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
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dfryer1193/mjolnir/utils/db"
	"github.com/dfryer1193/mjolnir/utils/log"

	"github.com/worldtree-industries/mimir/client"
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

	mu          sync.Mutex
	deployables []*RunningDeployable
}

// New starts mimir-server in-process against a fresh database, and stops it
// when the test ends. It skips the test when DATABASE_URL is unset.
func New(t *testing.T) *Harness {
	t.Helper()

	dsn := throwawayDatabase(t)
	pool, err := db.Connect(t.Context(), dsn, db.Config{MinConns: 1, MaxConns: maxConns, ConnTimeout: awaitTimeout})
	if err != nil {
		t.Fatalf("connect to the throwaway database: %v", err)
	}
	t.Cleanup(pool.Close)

	h := &Harness{Store: objectstore.NewMemory(), Clock: clock.NewFake(Epoch)}

	handler, err := server.New(t.Context(), server.Config{
		Pool:        pool,
		ObjectStore: h.Store,
		Clock:       h.Clock,
		Logger:      log.GetLoggerTo("mimir-server", t.Output()),
	})
	if err != nil {
		t.Fatalf("start mimir-server: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	h.URL = srv.URL

	return h
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
	// ServerDown points the client at a mimir-server that isn't running.
	ServerDown bool
}

// RunningDeployable is a Deployable placed in a Harness, recording what its
// client does.
type RunningDeployable struct {
	Deployable

	h          *Harness
	logs       lockedBuffer
	registered chan error
	polled     chan error

	mu          sync.Mutex
	valueEvents []client.Event
	lastPollErr error
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

	h.mu.Lock()
	h.deployables = append(h.deployables, running)
	h.mu.Unlock()

	return running
}

// ClientOptions are the client options that place the Deployable in the
// harness: its environment, the shared store and clock, and event capture.
func (d *RunningDeployable) ClientOptions() []client.Option {
	serverURL := d.h.URL
	if d.ServerDown {
		serverURL = "http://127.0.0.1:1"
	}
	env := map[string]string{
		"MIMIR_DEPLOYABLE":  d.Name,
		"MIMIR_ENVIRONMENT": d.Environment,
		"MIMIR_SERVER_URL":  serverURL,
	}

	opts := []client.Option{
		client.WithGetenv(func(key string) string { return env[key] }),
		client.WithObjectStore(d.h.Store),
		client.WithClock(d.h.Clock),
		client.WithLogger(log.GetLoggerTo("mimir-client", &d.logs)),
		client.WithEventHandler(d.record),
	}
	if d.BuildPath != "" {
		opts = append(opts, client.WithBuildPath(d.BuildPath))
	}

	return opts
}

func (d *RunningDeployable) record(e client.Event) {
	switch e.Kind {
	case client.EventRegistered:
		d.registered <- e.Err
	case client.EventPolled:
		d.polled <- e.Err
	case client.EventValueChanged, client.EventValueRejected:
		d.mu.Lock()
		d.valueEvents = append(d.valueEvents, e)
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

// AwaitRegistration waits for the client's startup Registration to finish
// and returns its error.
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

	h.mu.Lock()
	deployables := h.deployables
	h.mu.Unlock()

	for _, d := range deployables {
		drain(d.polled)
	}

	ctx, cancel := context.WithTimeout(t.Context(), awaitTimeout)
	defer cancel()
	if err := h.Clock.WaitForWaiters(ctx, len(deployables)); err != nil {
		t.Fatalf("clients never waited for their next poll: %v", err)
	}
	h.Clock.Advance(client.DefaultPollInterval)

	for _, d := range deployables {
		err := await(t, d.polled, "poll")
		if err != nil {
			t.Logf("poll by %s finished with: %v", d.Name, err)
		}
		d.mu.Lock()
		d.lastPollErr = err
		d.mu.Unlock()
	}
}

// LastPollError is the error the Deployable's client reported for the poll
// NextPoll last drove, or nil if that poll succeeded.
func (d *RunningDeployable) LastPollError() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.lastPollErr
}

// CreateOverride creates o through the admin API and returns it as created.
func (h *Harness) CreateOverride(t *testing.T, o models.Override) models.Override {
	t.Helper()

	status, body := h.Post(t, "/v1/overrides", o)
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

	reqBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.URL+path, bytes.NewReader(reqBody))
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

	loc := models.SnapshotLocationFor(models.DefaultBucketPrefix, ns)
	obj, err := h.Store.Get(t.Context(), loc.Bucket, loc.Key, "")
	if errors.Is(err, objectstore.ErrNotFound) {
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

	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	loc := models.SnapshotLocationFor(models.DefaultBucketPrefix, ns)
	if err = h.Store.Put(t.Context(), loc.Bucket, loc.Key, body); err != nil {
		t.Fatalf("write Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}
}

// DeleteSnapshot removes ns's Snapshot from the store, as expiring the
// Namespace does.
func (h *Harness) DeleteSnapshot(t *testing.T, ns models.Namespace) {
	t.Helper()

	loc := models.SnapshotLocationFor(models.DefaultBucketPrefix, ns)
	if err := h.Store.Delete(t.Context(), loc.Bucket, loc.Key); err != nil {
		t.Fatalf("delete Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}
}

// SnapshotETag is the ETag of ns's Snapshot in the store.
func (h *Harness) SnapshotETag(t *testing.T, ns models.Namespace) string {
	t.Helper()

	loc := models.SnapshotLocationFor(models.DefaultBucketPrefix, ns)
	obj, err := h.Store.Get(t.Context(), loc.Bucket, loc.Key, "")
	if err != nil {
		t.Fatalf("read Snapshot %s/%s: %v", loc.Bucket, loc.Key, err)
	}

	return obj.ETag
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
