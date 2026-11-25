package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/client"
	"github.com/worldtree-industries/mimir/models"
)

// waitTimeout bounds every wait for the client's background work. It is a
// guard against hangs, never a delay.
const waitTimeout = 5 * time.Second

// The Config the tests read: two Items, foo and bar.
const (
	fooConfig  = "example.com/foo.FooConfig"
	fooDefault = "default"
	barDefault = 55555
)

//nolint:gochecknoglobals // fixed, read-only test values
var (
	fooRef = models.ItemRef{Name: "foo", Type: models.TypeString}
	barRef = models.ItemRef{Name: "bar", Type: models.TypeInt}
	fooDev = models.Namespace{Config: fooConfig, Deployable: "foo-consumer", Environment: "dev"}
)

// fooValues are the values of the test Config, as a generated Config holds
// them.
type fooValues struct {
	mu  sync.Mutex
	foo string
	bar int64
}

func (v *fooValues) definition() client.Definition {
	return client.Definition{
		Config: fooConfig,
		Items: []client.ItemDefinition{
			{Name: fooRef.Name, Type: fooRef.Type, Default: fooDefault, Set: func(s string) error {
				v.mu.Lock()
				defer v.mu.Unlock()
				v.foo = s
				return nil
			}},
			{Name: barRef.Name, Type: barRef.Type, Default: strconv.Itoa(barDefault), Set: func(s string) error {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return fmt.Errorf("bar: %w", err)
				}
				v.mu.Lock()
				defer v.mu.Unlock()
				v.bar = n
				return nil
			}},
		},
	}
}

func (v *fooValues) Foo() string {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.foo
}

func (v *fooValues) Bar() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.bar
}

// localLine is one line of a local config file.
func localLine(config, item, value string) string {
	line, err := json.Marshal(map[string]string{"config": config, "item": item, "value": value})
	if err != nil {
		panic(err)
	}

	return string(line)
}

// newRepository makes a repository checkout whose local config file holds
// lines, and returns its root.
func newRepository(t *testing.T, lines ...string) string {
	t.Helper()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeLocalConfig(t, root, strings.Join(lines, "\n")+"\n")

	return root
}

func writeLocalConfig(t *testing.T, dir, contents string) {
	t.Helper()

	path := filepath.Join(dir, client.LocalConfigFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

// world says where a process runs: its working directory and environment,
// and whether it has object storage.
type world struct {
	workDir string
	env     map[string]string
	noStore bool
}

// process is one process reading Configs through a client Runtime, in a
// world the test controls.
type process struct {
	rt    *client.Runtime
	store *memStore
	clock *fakeClock
	logs  lockedBuffer

	polled     chan error
	registered chan error

	mu            sync.Mutex
	registrations []models.Registration
}

func newProcess(t *testing.T, w world) *process {
	t.Helper()

	p := &process{
		store:      &memStore{objects: map[string][]byte{}},
		clock:      &fakeClock{waits: make(chan wait, 16)},
		polled:     make(chan error, 16),
		registered: make(chan error, 16),
	}
	srv := httptest.NewServer(http.HandlerFunc(p.serveRegistration))
	t.Cleanup(srv.Close)

	env := maps.Clone(w.env)
	opts := []client.Option{
		client.WithGetenv(func(key string) string { return env[key] }),
		client.WithHTTPClient(routeDefaultServerTo(t, srv.URL)),
		client.WithWorkingDir(w.workDir),
		client.WithClock(p.clock),
		client.WithLogger(slog.New(slog.NewJSONHandler(&p.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))),
		client.WithBuildPath("example.com/cmd/foo-consumer"),
	}
	if !w.noStore {
		opts = append(opts, client.WithObjectStores(func(context.Context, string) (models.ObjectReader, error) {
			return p.store, nil
		}))
	}
	p.rt = client.NewRuntime(t.Context(), opts...)

	return p
}

// routeDefaultServerTo is an HTTP client that sends requests for
// mimir-server at its Default URL to the server at to.
func routeDefaultServerTo(t *testing.T, to string) *http.Client {
	t.Helper()

	target, err := url.Parse(to)
	if err != nil {
		t.Fatal(err)
	}
	def, err := url.Parse(client.DefaultServerURL)
	if err != nil {
		t.Fatal(err)
	}

	return &http.Client{Timeout: waitTimeout, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == def.Host {
			req = req.Clone(req.Context())
			req.URL.Scheme, req.URL.Host, req.Host = target.Scheme, target.Host, target.Host
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// startFoo starts the test Config in the process.
func (p *process) startFoo(t *testing.T) *fooValues {
	t.Helper()

	values := &fooValues{}
	client.Start(t.Context(), values.definition(), client.WithRuntime(p.rt), client.WithEventHandler(p.record))

	return values
}

func (p *process) record(e client.Event) {
	var ch chan error
	switch e.Kind {
	case client.EventPolled:
		ch = p.polled
	case client.EventRegistered:
		ch = p.registered
	case client.EventValueChanged, client.EventValueRejected, client.EventLocationSwitch:
		return
	}
	select {
	case ch <- e.Err:
	default:
	}
}

func (p *process) serveRegistration(w http.ResponseWriter, r *http.Request) {
	var reg models.Registration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.registrations = append(p.registrations, reg)
	p.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// publish writes values as the Snapshot of ns.
func (p *process) publish(t *testing.T, ns models.Namespace, values ...models.SnapshotValue) {
	t.Helper()

	body, err := json.Marshal(models.NewSnapshot(values))
	if err != nil {
		t.Fatal(err)
	}
	loc := models.SnapshotLocationFor(ns)
	p.store.put(loc.Bucket+"/"+loc.Key, body)
}

// nextPoll waits for the poll loop to wait, checks it waits for interval,
// ends the wait, and waits for the test Config's poll to finish.
func (p *process) nextPoll(t *testing.T, interval time.Duration) {
	t.Helper()

	for len(p.polled) > 0 {
		<-p.polled
	}
	select {
	case w := <-p.clock.waits:
		if w.d != interval {
			t.Fatalf("the client waits %v for its next poll, want %v", w.d, interval)
		}
		w.ch <- time.Time{}
	case <-time.After(waitTimeout):
		t.Fatal("the client never waited for its next poll")
	}
	await(t, p.polled, "poll")
}

func await(t *testing.T, ch <-chan error, what string) {
	t.Helper()

	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("%s failed: %v", what, err)
		}
	case <-time.After(waitTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestLocalValueReplacesTheDefaultWithoutANamespace(t *testing.T) {
	p := newProcess(t, world{workDir: newRepository(t, localLine(fooConfig, "bar", "42"))})

	cfg := p.startFoo(t)

	if got := cfg.Bar(); got != 42 {
		t.Errorf("bar = %d, want the Local Value 42", got)
	}
	if got := cfg.Foo(); got != fooDefault {
		t.Errorf("foo = %q, want its Default, having no Local Value", got)
	}
	if !strings.Contains(p.logs.String(), `"level":"INFO","msg":"Can't determine the Namespace`) {
		t.Errorf("running without a Namespace from a checkout wasn't logged at Info; logs:\n%s", p.logs.String())
	}
}

func TestLocalValuesAreFoundFromASubdirectoryOfTheRepository(t *testing.T) {
	root := newRepository(t, localLine(fooConfig, "bar", "42"))
	sub := filepath.Join(root, "internal", "e2e")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	p := newProcess(t, world{workDir: sub})

	if got := p.startFoo(t).Bar(); got != 42 {
		t.Errorf("bar = %d, want the Local Value 42", got)
	}
}

func TestLocalValuesAreReadOnlyOutsideStagingAndProd(t *testing.T) {
	cases := map[string]struct {
		environment string
		want        int64
	}{
		"unset":   {"", 42},
		"dev":     {"dev", 42},
		"staging": {"staging", barDefault},
		"prod":    {"prod", barDefault},
		"unknown": {"prdo", barDefault},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := newProcess(t, world{
				workDir: newRepository(t, localLine(fooConfig, "bar", "42")),
				env:     map[string]string{"MIMIR_ENVIRONMENT": tc.environment},
				noStore: true,
			})

			if got := p.startFoo(t).Bar(); got != tc.want {
				t.Errorf("with MIMIR_ENVIRONMENT=%q, bar = %d, want %d", tc.environment, got, tc.want)
			}
		})
	}
}

func TestLocalValuesAreReadOnlyFromARepositoryRoot(t *testing.T) {
	// A deployed image has no .git, even if the file was copied into it.
	dir := t.TempDir()
	writeLocalConfig(t, dir, localLine(fooConfig, "bar", "42")+"\n")
	p := newProcess(t, world{workDir: dir})

	if got := p.startFoo(t).Bar(); got != barDefault {
		t.Errorf("outside a repository, bar = %d, want the Default %d", got, barDefault)
	}
	if !strings.Contains(p.logs.String(), `"level":"ERROR","msg":"Can't determine the Namespace`) {
		t.Errorf("running without a Namespace outside a checkout wasn't logged as an error; logs:\n%s", p.logs.String())
	}
}

func TestLocalValueBeatsAnOverrideAtStartAndOnEveryPoll(t *testing.T) {
	p := newProcess(t, world{
		workDir: newRepository(t, localLine(fooConfig, "bar", "42")),
		env:     map[string]string{"MIMIR_ENVIRONMENT": "dev"},
	})
	p.publish(
		t,
		fooDev,
		models.SnapshotValue{Item: barRef, Value: "7"},
		models.SnapshotValue{Item: fooRef, Value: "tuned"},
	)

	cfg := p.startFoo(t)
	await(t, p.polled, "startup poll")
	if got := cfg.Bar(); got != 42 {
		t.Errorf("at start, bar = %d, want the Local Value 42 over the Override 7", got)
	}
	if got := cfg.Foo(); got != "tuned" {
		t.Errorf("at start, foo = %q, want its Override %q, having no Local Value", got, "tuned")
	}

	p.publish(
		t,
		fooDev,
		models.SnapshotValue{Item: barRef, Value: "8"},
		models.SnapshotValue{Item: fooRef, Value: "retuned"},
	)
	p.nextPoll(t, client.DefaultPollInterval)
	if got := cfg.Bar(); got != 42 {
		t.Errorf("after a poll, bar = %d, want the Local Value 42 over the Override 8", got)
	}
	if got := cfg.Foo(); got != "retuned" {
		t.Errorf("after a poll, foo = %q, want its new Override %q", got, "retuned")
	}
}

func TestMimirClientLocalValueReplacesItsDefault(t *testing.T) {
	p := newProcess(t, world{
		workDir: newRepository(t, localLine(client.MimirClientConfig, client.PollIntervalItem, "10s")),
		env:     map[string]string{"MIMIR_ENVIRONMENT": "dev"},
	})

	p.startFoo(t)
	await(t, p.polled, "startup poll")
	p.nextPoll(t, 10*time.Second)
}

func TestRegistrationPublishesDefaultsNotLocalValues(t *testing.T) {
	p := newProcess(t, world{
		workDir: newRepository(t, localLine(fooConfig, "bar", "42")),
		env:     map[string]string{"MIMIR_ENVIRONMENT": "dev"},
	})

	p.startFoo(t)
	await(t, p.registered, "Registration")

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, reg := range p.registrations {
		if reg.Namespace.Config != fooConfig {
			continue
		}
		for _, item := range reg.Items {
			if item.Name == barRef.Name && item.Default != strconv.Itoa(barDefault) {
				t.Errorf("bar registered with Default %q, want the compiled-in %d", item.Default, barDefault)
			}
		}
		return
	}
	t.Fatalf("the Config was never registered: %+v", p.registrations)
}

func TestBadLocalConfigLinesAreLoggedAndTheRestApply(t *testing.T) {
	p := newProcess(t, world{workDir: newRepository(t,
		`not json`,
		`{"config":"`+fooConfig+`","item":"foo"}`,
		localLine(fooConfig, "bar", "not-a-number"),
		localLine(fooConfig, "nonesuch", "1"),
		localLine(fooConfig, "foo", "first"),
		``,
		localLine(fooConfig, "foo", "local"),
	)})

	cfg := p.startFoo(t)

	if got := cfg.Foo(); got != "local" {
		t.Errorf("foo = %q, want %q from the last line setting it", got, "local")
	}
	if got := cfg.Bar(); got != barDefault {
		t.Errorf("bar = %d, want the Default %d, its Local Value being bad", got, barDefault)
	}
	logs := p.logs.String()
	for _, want := range []string{
		`"msg":"Skipping a bad line of the local config file"`,
		`"line":1,"error"`,
		`"line":2,"error"`,
		`"msg":"Ignoring a bad Local Value; keeping the start value"`,
		`"msg":"The local config file sets an Item this Config doesn't declare"`,
		`"msg":"Item set twice in the local config file; the last line wins"`,
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s; logs:\n%s", want, logs)
		}
	}
}

func TestAnOverlongLocalConfigLineIsSkippedAndTheRestApply(t *testing.T) {
	p := newProcess(t, world{workDir: newRepository(t,
		localLine(fooConfig, "foo", strings.Repeat("x", 2<<20)+`"`),
		localLine(fooConfig, "bar", "42"),
	)})

	if got := p.startFoo(t).Bar(); got != 42 {
		t.Errorf("bar = %d, want the Local Value 42 on the line after a 2 MiB one", got)
	}
}

func TestLocalValuesInUseAreLoggedOncePerConfig(t *testing.T) {
	p := newProcess(t, world{workDir: newRepository(t,
		localLine(fooConfig, "foo", "local"),
		localLine(fooConfig, "bar", "42"),
	)})

	p.startFoo(t)

	logs := p.logs.String()
	if n := strings.Count(logs, "Using Local Values"); n != 1 {
		t.Errorf("Local Values in use were logged %d times, want once; logs:\n%s", n, logs)
	}
	if !strings.Contains(logs, `"values":{"foo":"local","bar":"42"}`) {
		t.Errorf("the log of Local Values in use doesn't list them; logs:\n%s", logs)
	}
}

// memStore is object storage in memory.
type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *memStore) put(path string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[path] = body
}

func (s *memStore) Get(_ context.Context, bucket, key, etag string) (models.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	body, ok := s.objects[bucket+"/"+key]
	if !ok {
		return models.Object{}, models.ErrNotFound
	}
	tag := strconv.Quote(string(body))
	if tag == etag {
		return models.Object{}, models.ErrNotModified
	}

	return models.Object{Body: body, ETag: tag}, nil
}

// wait is one call to fakeClock.After.
type wait struct {
	d  time.Duration
	ch chan time.Time
}

// fakeClock is a clock whose waits end only when the test ends them.
type fakeClock struct {
	waits chan wait
}

func (*fakeClock) Now() time.Time { return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC) }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.waits <- wait{d: d, ch: ch}

	return ch
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
