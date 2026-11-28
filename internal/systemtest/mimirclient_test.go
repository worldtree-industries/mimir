package systemtest_test

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/client"
	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// mimirClientDev is foo-consumer's MimirClient Namespace in dev.
//
//nolint:gochecknoglobals // a fixed, read-only Namespace
var mimirClientDev = models.Namespace{Config: client.MimirClientConfig, Deployable: "foo-consumer", Environment: "dev"}

// fooSnapshot is a Snapshot of foo's Config with foo set to value.
func fooSnapshot(value string) models.Snapshot {
	return models.NewSnapshot([]models.SnapshotValue{{Item: fooRef, Value: value}})
}

// recordGets records the bucket of every Get from h.Store until the returned
// function is called, which returns them.
func recordGets(h *systemtest.Harness) func() []string {
	var mu sync.Mutex
	var buckets []string
	h.Store.SetGetFault(func(bucket, _, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		buckets = append(buckets, bucket)
		return nil
	})

	return func() []string {
		h.Store.SetGetFault(nil)
		mu.Lock()
		defer mu.Unlock()
		return slices.Compact(slices.Sorted(slices.Values(buckets)))
	}
}

// mimirClientOverride is a Config-wide Override of one of MimirClient's Items, all
// of which are strings.
func mimirClientOverride(item, value string) models.Override {
	return models.Override{
		Scope: models.Scope{Config: client.MimirClientConfig},
		Item:  models.ItemRef{Name: item, Type: models.TypeString},
		Value: value,
	}
}

func TestMimirClientRegistersItsThreeItems(t *testing.T) {
	h := systemtest.New(t)
	startFooConsumer(t, h)

	var got []string
	for _, item := range h.Items(t, client.MimirClientConfig) {
		got = append(got, item.Name)
	}
	slices.Sort(got)
	// No bucket prefix: mimir-server only ever publishes to mimir-<environment>.
	if want := []string{"object_store_endpoint", "poll_interval", "server_url"}; !slices.Equal(got, want) {
		t.Errorf("MimirClient's Items = %q, want %q", got, want)
	}
}

func TestPollIntervalOverrideTakesEffectLive(t *testing.T) {
	h := systemtest.New(t)
	cfg, _ := startFooConsumer(t, h)

	h.CreateOverride(t, mimirClientOverride(client.PollIntervalItem, "10s"))
	h.NextPoll(t)

	// The next poll comes 10s on, not the Default 30s.
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPollAfter(t, 10*time.Second)

	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("10s after the poll interval became 10s, GetFoo() = %q, want %q", got, "tuned")
	}
}

func TestEndpointOverrideIsAdoptedOnceSnapshotsAreReadThere(t *testing.T) {
	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)

	other := h.NewEndpoint("https://objects.example")
	h.CreateOverride(t, mimirClientOverride(client.ObjectStoreEndpointItem, "https://objects.example"))
	h.CopySnapshot(t, mimirClientDev, other)
	h.PutSnapshotAt(t, other, fooConsumerDev, fooSnapshot("moved"))
	h.NextPoll(t)

	if got := cfg.GetFoo(); got != "moved" {
		t.Errorf("after the endpoint changed, GetFoo() = %q, want %q from the new endpoint", got, "moved")
	}
	if got := d.TakeLocationSwitches(); len(got) != 1 || got[0] != nil {
		t.Errorf("location switches = %v, want one that succeeded", got)
	}

	gets := recordGets(h)
	h.NextPoll(t)
	if got := gets(); len(got) != 0 {
		t.Errorf("after the switch the client still read buckets %q at the old endpoint", got)
	}
}

func TestUnreachableLocationNeverStrandsTheClient(t *testing.T) {
	cases := map[string]struct {
		override models.Override
		// fault makes the new location fail; nil leaves it unreachable.
		fault func(h *systemtest.Harness)
	}{
		"endpoint that can't be reached": {
			override: mimirClientOverride(client.ObjectStoreEndpointItem, "https://nowhere.example"),
		},
		"endpoint whose buckets can't be read": {
			override: mimirClientOverride(client.ObjectStoreEndpointItem, "https://broken.example"),
			fault: func(h *systemtest.Harness) {
				h.NewEndpoint("https://broken.example").SetGetFault(func(string, string, string) error {
					return storeFailures["5xx"]
				})
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { checkLocationNeverStrands(t, tc.override, tc.fault) })
	}
}

// checkLocationNeverStrands moves a client to a location that fails, with
// override and fault, and checks it keeps reading the old one until fixed.
func checkLocationNeverStrands(t *testing.T, override models.Override, fault func(h *systemtest.Harness)) {
	t.Helper()

	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)
	if fault != nil {
		fault(h)
	}

	h.CreateOverride(t, override)
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)

	if got := d.TakeLocationSwitches(); len(got) != 1 || got[0] == nil {
		t.Fatalf("location switches = %v, want one that failed", got)
	}
	if !strings.Contains(d.Logs(), "keeping the current one") {
		t.Errorf("the failed switch wasn't logged; logs:\n%s", d.Logs())
	}
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want %q still read from the old location", got, "tuned")
	}

	// The client keeps trying, and keeps reading the old location,
	// where the fix arrives: the MimirClient Item goes back to its Default.
	h.PutSnapshot(t, mimirClientDev, models.NewSnapshot(nil))
	h.PutSnapshot(t, fooConsumerDev, fooSnapshot("fixed"))
	h.NextPoll(t)

	if got := cfg.GetFoo(); got != "fixed" {
		t.Errorf("after the fix, GetFoo() = %q, want %q", got, "fixed")
	}
	h.NextPoll(t)
	if got := d.TakeLocationSwitches(); len(got) != 0 {
		t.Errorf("after the fix the client still tried to switch: %v", got)
	}
}

func TestMimirClientStartsFromItsDefaultsWhateverTheEnvironment(t *testing.T) {
	h := systemtest.New(t)
	// Snapshots where the old variables would point: at their endpoint, and
	// in custom-dev at the Default one.
	other := h.NewEndpoint("https://objects.example")
	h.PutSnapshotAt(t, other, fooConsumerDev, fooSnapshot("at-the-ignored-endpoint"))
	ignored, err := json.Marshal(fooSnapshot("in-the-ignored-bucket"))
	if err != nil {
		t.Fatal(err)
	}
	key := models.SnapshotLocationFor(fooConsumerDev).Key
	if err = h.Store.Put(t.Context(), "custom-dev", key, ignored); err != nil {
		t.Fatal(err)
	}

	// These variables once set MimirClient's start values (ADR 0007).
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev", Env: map[string]string{
		"MIMIR_OBJECT_STORE_ENDPOINT": "https://objects.example",
		"MIMIR_BUCKET_PREFIX":         "custom",
		"MIMIR_POLL_INTERVAL":         "10s",
		"MIMIR_SERVER_URL":            "http://127.0.0.1:1",
	}})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err = d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration at the Default server URL failed: %v", err)
	}
	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("GetFoo() = %q, want the Default, nothing being published at mimir-dev", got)
	}

	// No poll 10s on; the poll at the Default 30s reads mimir-dev at the
	// Default endpoint.
	h.CreateOverride(t, fooOverride("tuned"))
	h.Advance(t, 10*time.Second)
	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("10s on, GetFoo() = %q, want no poll yet at the Default interval", got)
	}
	h.NextPollAfter(t, client.DefaultPollInterval-10*time.Second)
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("30s on, GetFoo() = %q, want %q from mimir-dev at the Default endpoint", got, "tuned")
	}
}

func TestBadMimirClientValueIsLoggedAndTheLastGoodOneKept(t *testing.T) {
	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)

	// mimir-server accepts any string, but the client rejects a value that
	// isn't a usable interval and keeps polling at the Default.
	h.CreateOverride(t, mimirClientOverride(client.PollIntervalItem, "0s"))
	h.NextPoll(t)
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)

	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want %q polled at the Default interval", got, "tuned")
	}
	if !strings.Contains(d.Logs(), `"value":"0s"`) {
		t.Errorf("the rejected poll interval wasn't logged; logs:\n%s", d.Logs())
	}
	if got := d.TakeValueEvents(); slices.ContainsFunc(got, func(e client.Event) bool {
		return e.Item.Name == client.PollIntervalItem
	}) {
		t.Errorf("the program was sent value events for the MimirClient Config: %+v", got)
	}
}

func TestRestartBeginsAgainFromTheStartingLocation(t *testing.T) {
	h := systemtest.New(t)
	_, first := startFooConsumer(t, h)
	next := h.NewEndpoint("https://next.example")
	h.CreateOverride(t, mimirClientOverride(client.ObjectStoreEndpointItem, "https://next.example"))
	h.CopySnapshot(t, mimirClientDev, next)
	h.PutSnapshotAt(t, next, fooConsumerDev, fooSnapshot("moved"))
	h.NextPoll(t)
	if got := first.TakeLocationSwitches(); len(got) != 1 || got[0] != nil {
		t.Fatalf("location switches = %v, want one that succeeded", got)
	}

	// The pod restarts while its starting location can't be read. It
	// doesn't remember the adopted endpoint, so it runs on Defaults rather
	// than reading next.example.
	h.Store.SetGetFault(func(bucket, _, _ string) error {
		if bucket == "mimir-dev" {
			return storeFailures["network error"]
		}
		return nil
	})
	restarted := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	cfg := foo.NewFooConfig(t.Context(), restarted.ClientOptions()...)
	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("after a restart with mimir-dev unreadable, GetFoo() = %q, want the Default %q", got, "default")
	}

	// Once the starting location reads again, the Override is read there
	// and adopted afresh.
	h.Store.SetGetFault(nil)
	h.NextPoll(t)
	if got := cfg.GetFoo(); got != "moved" {
		t.Errorf("once mimir-dev reads again, GetFoo() = %q, want %q from next.example", got, "moved")
	}
}

func TestServerURLOverrideTakesEffectLive(t *testing.T) {
	h := systemtest.New(t)
	startFooConsumer(t, h)

	// A pod whose Default server URL reaches nothing can't register.
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev", ServerDown: true})
	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err == nil {
		t.Fatal("Registration against a stopped server succeeded")
	}

	// The fix arrives as an Override, and the next attempt uses it.
	h.CreateOverride(t, mimirClientOverride(client.ServerURLItem, h.URL))
	h.NextPoll(t)
	h.Advance(t, client.RegistrationRetryInterval)
	if err := d.AwaitRegistration(t); err != nil {
		t.Errorf("Registration retried after the server URL Override failed: %v", err)
	}
}

func TestBadEndpointOverrideNeverStrandsOrFlaps(t *testing.T) {
	cases := map[string]func(h *systemtest.Harness){
		// Nothing was ever published at the endpoint: its buckets are empty.
		"endpoint with nothing published": func(h *systemtest.Harness) {
			h.NewEndpoint("https://nowhere.example")
		},
		// The Config's Snapshot is there, but not MimirClient's, so reading
		// there would set the endpoint back to its start value.
		"endpoint without MimirClient's Snapshot": func(h *systemtest.Harness) {
			stray := h.NewEndpoint("https://nowhere.example")
			h.PutSnapshotAt(t, stray, fooConsumerDev, fooSnapshot("stray"))
		},
	}
	for name, setUp := range cases {
		t.Run(name, func(t *testing.T) {
			h := systemtest.New(t)
			cfg, d := startFooConsumer(t, h)
			setUp(h)
			h.CreateOverride(t, fooOverride("tuned"))
			h.CreateOverride(t, mimirClientOverride(client.ObjectStoreEndpointItem, "https://nowhere.example"))

			for poll := range 3 {
				h.NextPoll(t)
				if got := cfg.GetFoo(); got != "tuned" {
					t.Fatalf("poll %d: GetFoo() = %q, want %q still read from the Default endpoint", poll, got, "tuned")
				}
				for _, err := range d.TakeLocationSwitches() {
					if err == nil {
						t.Fatalf(
							"poll %d: the client moved to an endpoint whose MimirClient Snapshot doesn't name it",
							poll,
						)
					}
				}
			}
		})
	}
}
