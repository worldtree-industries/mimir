// Package systemtest is the harness for Mimir's system-seam tests: a real
// mimir-server handler running in-process on a throwaway Postgres database,
// real clients, one in-memory object store they share, and a fake clock, so
// tests drive the whole path through the admin API and Registration and
// observe it through generated getters, without sleeping.
//
// Tests skip when DATABASE_URL is unset.
package systemtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dfryer1193/mjolnir/utils/db"
	"github.com/dfryer1193/mjolnir/utils/log"

	"github.com/worldtree-industries/mimir/client"
	"github.com/worldtree-industries/mimir/client/clienttest"
	"github.com/worldtree-industries/mimir/internal/clock"
	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/internal/server"
	"github.com/worldtree-industries/mimir/models"
)

// awaitTimeout bounds every wait for something the system under test does in
// the background. It's a guard against hangs, never a delay: waits end as
// soon as the awaited event happens.
const awaitTimeout = 10 * time.Second

// maxConns sizes each test's Postgres pool.
const maxConns = 4

// eventBuffer is how many unawaited client events a RunningDeployable holds.
const eventBuffer = 16

// Epoch is where every Harness's clock starts.
//
//nolint:gochecknoglobals // a fixed, read-only test start time
var Epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// Harness is one running mimir-server and the shared world around it.
type Harness struct {
	// Store is the object storage mimir-server publishes to and clients poll.
	Store *objectstore.Memory
	// Clock drives mimir-server and every client.
	Clock *clock.Fake
	// URL is the in-process mimir-server's base URL.
	URL string

	served *faultyStore
	dsn    string
	pool   *db.Pool
	// serverEnv, when set, is the environment the servers are configured
	// from, as a real mimir-server is.
	serverEnv map[string]string

	rejectRegistrations atomic.Bool

	mu          sync.Mutex
	deployables []*RunningDeployable
	replicas    []*Replica
	endpoints   map[string]*objectstore.Memory
	// registrations counts the Registrations sent, by Config.
	registrations map[string]int
}

// Option configures a Harness.
type Option func(*Harness)

// WithServerEnv configures the servers from the environment env, as a real
// mimir-server is configured (server.ConfigFromEnv), so they publish to the
// object storage env names rather than to Store.
func WithServerEnv(env map[string]string) Option {
	return func(h *Harness) { h.serverEnv = env }
}

// New starts mimir-server in-process against a fresh database, and stops it
// when the test ends. It skips the test when DATABASE_URL is unset.
func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()

	dsn := throwawayDatabase(t)
	store := objectstore.NewMemory()
	h := &Harness{
		Store:  store,
		Clock:  clock.NewFake(Epoch),
		served: &faultyStore{ObjectStore: store},
		dsn:    dsn,
	}
	for _, opt := range opts {
		opt(h)
	}

	primary := h.startServer(t)
	h.URL = primary.URL
	h.pool = primary.pool

	return h
}

// Replica is another mimir-server instance on the Harness's database, object
// storage and clock, as when several replicas run.
type Replica struct {
	// URL is the replica's base URL.
	URL string

	h          *Harness
	pool       *db.Pool
	reconciled chan struct{}
	expired    chan struct{}
}

// NewReplica starts one more mimir-server on the same database and object
// storage.
func (h *Harness) NewReplica(t *testing.T) *Replica {
	t.Helper()

	return h.startServer(t)
}

func (h *Harness) startServer(t *testing.T) *Replica {
	t.Helper()

	pool, err := db.Connect(t.Context(), h.dsn, db.Config{MinConns: 1, MaxConns: maxConns, ConnTimeout: awaitTimeout})
	if err != nil {
		t.Fatalf("connect to the throwaway database: %v", err)
	}
	t.Cleanup(pool.Close)

	r := &Replica{
		h:          h,
		pool:       pool,
		reconciled: make(chan struct{}, eventBuffer),
		expired:    make(chan struct{}, eventBuffer),
	}

	cfg := server.Config{ObjectStore: h.served}
	if h.serverEnv != nil {
		if cfg, err = server.ConfigFromEnv(
			t.Context(),
			func(key string) string { return h.serverEnv[key] },
		); err != nil {
			t.Fatalf("configure mimir-server from its environment: %v", err)
		}
	}
	cfg.Pool, cfg.Clock, cfg.Logger = pool, h.Clock, log.GetLoggerTo("mimir-server", t.Output())
	cfg.TestHooks = server.TestHooks{
		ReconcileFinished: func() {
			select {
			case r.reconciled <- struct{}{}:
			default:
			}
		},
		ExpiryFinished: func() {
			select {
			case r.expired <- struct{}{}:
			default:
			}
		},
	}
	handler, err := server.New(t.Context(), cfg)
	if err != nil {
		t.Fatalf("start mimir-server: %v", err)
	}
	srv := httptest.NewServer(h.gateRegistrations(handler))
	t.Cleanup(srv.Close)
	r.URL = srv.URL

	h.mu.Lock()
	h.replicas = append(h.replicas, r)
	h.mu.Unlock()

	return r
}

// Deployable describes a program that reads a Config through the client.
type Deployable struct {
	// Name is MIMIR_DEPLOYABLE; empty leaves it unset.
	Name string
	// Environment is MIMIR_ENVIRONMENT.
	Environment string
	// BuildPath is the main package path the Deployable name is derived from
	// when Name is empty.
	BuildPath string
	// ServerDown makes the Default server URL reach a mimir-server that isn't
	// running.
	ServerDown bool
	// Env holds further environment variables, such as the AWS ones.
	Env map[string]string
	// NoObjectStore gives the client no object storage at all, as when no S3
	// variables are set.
	NoObjectStore bool
	// ObjectStoreDown gives the client object storage it can never open.
	ObjectStoreDown bool
}

// RunningDeployable is a Deployable placed in a Harness, recording what its
// client does. It is one process: every Config it runs shares one client
// Runtime.
type RunningDeployable struct {
	Deployable

	h          *Harness
	runtime    *client.Runtime
	logs       lockedBuffer
	registered chan error
	polled     chan error

	mu sync.Mutex
	// configs is how many Configs the Deployable runs: one per call to
	// ClientOptions.
	configs          int
	valueEvents      []client.Event
	locationSwitches []error
	lastPollErr      error
}

// NewDeployable places d in h. Pass its ClientOptions to the generated
// Config's constructor.
func (h *Harness) NewDeployable(t *testing.T, d Deployable) *RunningDeployable {
	t.Helper()

	running := &RunningDeployable{
		Deployable: d,
		h:          h,
		registered: make(chan error, eventBuffer),
		polled:     make(chan error, eventBuffer),
	}

	running.runtime = client.NewRuntime(t.Context(), running.processOptions()...)

	h.mu.Lock()
	h.deployables = append(h.deployables, running)
	h.mu.Unlock()

	return running
}

// ClientOptions are the client options that place a Config in the
// Deployable, with event capture. Call it once for each Config the
// Deployable runs.
func (d *RunningDeployable) ClientOptions() []client.Option {
	d.mu.Lock()
	d.configs++
	d.mu.Unlock()

	return []client.Option{client.WithRuntime(d.runtime), client.WithEventHandler(d.record)}
}

// processOptions place the Deployable's process in the harness: its
// environment, the shared store and clock, and its log.
func (d *RunningDeployable) processOptions() []client.Option {
	serverURL := d.h.URL
	if d.ServerDown {
		serverURL = "http://127.0.0.1:1"
	}
	env := map[string]string{
		"MIMIR_DEPLOYABLE":  d.Name,
		"MIMIR_ENVIRONMENT": d.Environment,
	}
	maps.Copy(env, d.Env)
	routed, err := clienttest.RouteDefaultServer(serverURL)
	if err != nil {
		panic(err)
	}

	opts := []client.Option{
		client.WithGetenv(func(key string) string { return env[key] }),
		client.WithHTTPClient(routed),
		client.WithClock(d.h.Clock),
		client.WithLogger(log.GetLoggerTo("mimir-client", &d.logs)),
	}
	switch {
	case d.ObjectStoreDown:
		opts = append(
			opts,
			client.WithObjectStores(func(_ context.Context, endpoint string) (models.ObjectReader, error) {
				return nil, fmt.Errorf("dial %q: %w", endpoint, errUnreachable)
			}),
		)
	case !d.NoObjectStore:
		opts = append(opts, client.WithObjectStores(d.h.openEndpoint))
	}
	if d.BuildPath != "" {
		opts = append(opts, client.WithBuildPath(d.BuildPath))
	}

	return opts
}

func (d *RunningDeployable) record(e client.Event) {
	// Events nobody awaits are dropped rather than left to block the client.
	switch e.Kind {
	case client.EventRegistered:
		select {
		case d.registered <- e.Err:
		default:
		}
	case client.EventPolled:
		select {
		case d.polled <- e.Err:
		default:
		}
	case client.EventValueChanged, client.EventValueRejected:
		d.mu.Lock()
		d.valueEvents = append(d.valueEvents, e)
		d.mu.Unlock()
	case client.EventLocationSwitch:
		d.mu.Lock()
		d.locationSwitches = append(d.locationSwitches, e.Err)
		d.mu.Unlock()
	}
}

// TakeValueEvents returns the value events (changes and rejections) the
// client has reported since the last call, in order. A poll's value events
// are all reported before NextPoll returns.
func (d *RunningDeployable) TakeValueEvents() []client.Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	events := d.valueEvents
	d.valueEvents = nil

	return events
}

// AwaitRegistration waits for the client's next Registration attempt (at
// startup, a daily re-Registration or a retry) to finish and returns its
// error.
func (d *RunningDeployable) AwaitRegistration(t *testing.T) error {
	t.Helper()

	return await(t, d.registered, "Registration")
}

// Logs returns everything the Deployable's client has logged.
func (d *RunningDeployable) Logs() string {
	return d.logs.String()
}

// NextPoll moves the clock one poll interval on, once every Deployable's
// client is waiting for it, and returns when each has finished that poll.
func (h *Harness) NextPoll(t *testing.T) {
	t.Helper()

	h.NextPollAfter(t, client.DefaultPollInterval)
}

// NextPollAfter is NextPoll for clients whose poll interval is interval
// rather than the Default. Every Deployable's client must poll by then.
func (h *Harness) NextPollAfter(t *testing.T, interval time.Duration) {
	t.Helper()

	h.mu.Lock()
	deployables := h.deployables
	waiters := h.waiters()
	h.mu.Unlock()

	for _, d := range deployables {
		drain(d.polled)
	}

	ctx, cancel := context.WithTimeout(t.Context(), awaitTimeout)
	defer cancel()
	if err := h.Clock.WaitForWaiters(ctx, waiters); err != nil {
		t.Fatalf("clients never waited for their next poll: %v", err)
	}
	h.Clock.Advance(interval)

	for _, d := range deployables {
		err := d.awaitPolls(t)
		d.mu.Lock()
		d.lastPollErr = err
		d.mu.Unlock()
	}
}

// awaitPolls waits for the poll of each of the Deployable's Configs, and
// returns their errors joined.
func (d *RunningDeployable) awaitPolls(t *testing.T) error {
	t.Helper()

	d.mu.Lock()
	configs := d.configs
	d.mu.Unlock()

	var errs []error
	for range configs {
		if err := await(t, d.polled, "poll"); err != nil {
			t.Logf("poll by %s finished with: %v", d.Name, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// LastPollError is the error the Deployable's client reported for the poll
// NextPoll last drove, or nil if that poll succeeded.
func (d *RunningDeployable) LastPollError() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.lastPollErr
}

// Reconcile moves the clock one reconcile interval on, once every server is
// waiting for it, and returns when each has finished reconciling.
func (h *Harness) Reconcile(t *testing.T) {
	t.Helper()

	h.mu.Lock()
	replicas := slices.Clone(h.replicas)
	waiters := h.waiters()
	h.mu.Unlock()

	for _, r := range replicas {
		for len(r.reconciled) > 0 {
			<-r.reconciled
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), awaitTimeout)
	defer cancel()
	if err := h.Clock.WaitForWaiters(ctx, waiters); err != nil {
		t.Fatalf("servers never waited for their next reconcile: %v", err)
	}
	h.Clock.Advance(server.DefaultReconcileInterval)

	for _, r := range replicas {
		select {
		case <-r.reconciled:
		case <-time.After(awaitTimeout):
			t.Fatal("timed out waiting for a server to reconcile")
		}
	}
}

// FailUploads makes every Snapshot upload from the servers fail, or stops
// that.
func (h *Harness) FailUploads(fail bool) { h.served.failUploads.Store(fail) }

// FailObjectStore makes the servers' every object storage call fail, or stops
// that.
func (h *Harness) FailObjectStore(fail bool) { h.served.failAll.Store(fail) }

// StopPostgres closes the primary server's database connections for good.
func (h *Harness) StopPostgres() { h.pool.Close() }

// StoreCalls counts the calls the servers have made to object storage.
func (h *Harness) StoreCalls() int64 { return h.served.calls.Load() }

// AdvancePastReadinessCache moves the clock far enough that the next /readyz
// checks the dependencies afresh.
func (h *Harness) AdvancePastReadinessCache() { h.Clock.Advance(server.ReadinessCacheTTL) }

// Ready returns the status of the primary server's /readyz.
func (h *Harness) Ready(t *testing.T) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.URL+"/readyz", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

// DeleteSnapshot removes ns's Snapshot from object storage.
func (h *Harness) DeleteSnapshot(t *testing.T, ns models.Namespace) {
	t.Helper()

	loc := models.SnapshotLocationFor(ns)
	if err := h.Store.Delete(t.Context(), loc.Bucket, loc.Key); err != nil {
		t.Fatalf("delete Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}
}

// SnapshotETag returns the ETag of ns's published Snapshot.
func (h *Harness) SnapshotETag(t *testing.T, ns models.Namespace) string {
	t.Helper()

	loc := models.SnapshotLocationFor(ns)
	obj, err := h.Store.Get(t.Context(), loc.Bucket, loc.Key, "")
	if err != nil {
		t.Fatalf("read Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}

	return obj.ETag
}

// CreateOverride creates o through the admin API and returns it as created.
func (h *Harness) CreateOverride(t *testing.T, o models.Override) models.Override {
	t.Helper()

	return createOverride(t, h.URL, o)
}

// CreateOverride creates o through this replica's admin API.
func (r *Replica) CreateOverride(t *testing.T, o models.Override) models.Override {
	t.Helper()

	return createOverride(t, r.URL, o)
}

func createOverride(t *testing.T, baseURL string, o models.Override) models.Override {
	t.Helper()

	status, body := post(t, baseURL, "/v1/overrides", o)
	if status != http.StatusCreated {
		t.Fatalf("create Override: status %d, body %s", status, body)
	}

	var created models.Override
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode created Override %s: %v", body, err)
	}

	return created
}

// Post sends payload as JSON to the admin API path and returns the response's
// status and body.
func (h *Harness) Post(t *testing.T, path string, payload any) (int, []byte) {
	t.Helper()

	return post(t, h.URL, path, payload)
}

func post(t *testing.T, baseURL, path string, payload any) (int, []byte) {
	t.Helper()

	reqBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL+path, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response to POST %s: %v", path, err)
	}

	return resp.StatusCode, body
}

// Snapshot reads ns's Snapshot from the store at its constructed location.
// It reports false when there is none.
func (h *Harness) Snapshot(t *testing.T, ns models.Namespace) (models.Snapshot, bool) {
	t.Helper()

	loc := models.SnapshotLocationFor(ns)
	obj, err := h.Store.Get(t.Context(), loc.Bucket, loc.Key, "")
	if errors.Is(err, models.ErrNotFound) {
		return models.Snapshot{}, false
	}
	if err != nil {
		t.Fatalf("read Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}

	var snap models.Snapshot
	if err = json.Unmarshal(obj.Body, &snap); err != nil {
		t.Fatalf("decode Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}

	return snap, true
}

// PutSnapshot writes snap to the store as ns's Snapshot, standing in for
// whatever mimir-server, or anything else with write access, might publish.
func (h *Harness) PutSnapshot(t *testing.T, ns models.Namespace, snap models.Snapshot) {
	t.Helper()

	h.PutSnapshotAt(t, h.Store, ns, snap)
}

func await(t *testing.T, ch <-chan error, what string) error {
	t.Helper()

	select {
	case err := <-ch:
		return err
	case <-time.After(awaitTimeout):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}

func drain(ch <-chan error) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// lockedBuffer is a [bytes.Buffer] safe for the client's concurrent logging.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// Request sends a request with the given method to the admin API path and
// returns the response's status and body. A nil payload sends no body;
// otherwise it is sent as JSON.
func (h *Harness) Request(t *testing.T, method, path string, payload any) (int, []byte) {
	t.Helper()

	return h.Send(t, method, path, payload)
}

// Send is Request with header pairs (name, value, ...) sent as well.
func (h *Harness) Send(t *testing.T, method, path string, payload any, header ...string) (int, []byte) {
	t.Helper()

	var reqBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		reqBody = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, h.URL+path, reqBody)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}

	return h.send(t, req)
}
