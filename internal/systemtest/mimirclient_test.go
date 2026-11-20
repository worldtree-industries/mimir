package systemtest_test

import (
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
	return models.NewSnapshot([]models.SnapshotValue{{Item: fooKey, Value: value}})
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

// clientSetting is a Config-wide Override of one of MimirClient's Items, all
// of which are strings.
func clientSetting(item, value string) models.Override {
	return models.Override{
		Scope: models.Scope{Config: client.MimirClientConfig},
		Item:  models.ItemKey{Name: item, Type: models.TypeString},
		Value: value,
	}
}

func TestPollIntervalOverrideTakesEffectLive(t *testing.T) {
	h := systemtest.New(t)
	cfg, _ := startFooConsumer(t, h)

	h.CreateOverride(t, clientSetting(client.PollIntervalItem, "10s"))
	h.NextPoll(t)

	// The next poll comes 10s on, not the Default 30s.
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPollAfter(t, 10*time.Second)

	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("10s after the poll interval became 10s, GetFoo() = %q, want %q", got, "tuned")
	}
}

func TestBucketPrefixOverrideIsAdoptedOnceSnapshotsAreReadThere(t *testing.T) {
	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)

	// The Snapshots are mirrored to buckets with a new prefix, where foo
	// differs so the test can tell which location values come from.
	h.CreateOverride(t, clientSetting(client.BucketPrefixItem, "mimir-next"))
	h.CopySnapshot(t, mimirClientDev, h.Store, "mimir-next")
	h.PutSnapshotAt(t, h.Store, "mimir-next", fooConsumerDev, fooSnapshot("moved"))
	h.NextPoll(t)

	if got := cfg.GetFoo(); got != "moved" {
		t.Errorf("after the bucket prefix became mimir-next, GetFoo() = %q, want %q from mimir-next-dev", got, "moved")
	}
	if got := d.TakeLocationSwitches(); len(got) != 1 || got[0] != nil {
		t.Errorf("location switches = %v, want one that succeeded", got)
	}

	gets := recordGets(h)
	h.NextPoll(t)
	if got, want := gets(), []string{"mimir-next-dev"}; !slices.Equal(got, want) {
		t.Errorf("after the switch the client read buckets %q, want only %q", got, want)
	}
}

func TestEndpointOverrideIsAdoptedOnceSnapshotsAreReadThere(t *testing.T) {
	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)

	other := h.NewEndpoint("https://objects.example")
	h.CreateOverride(t, clientSetting(client.ObjectStoreEndpointItem, "https://objects.example"))
	h.CopySnapshot(t, mimirClientDev, other, models.DefaultBucketPrefix)
	h.PutSnapshotAt(t, other, models.DefaultBucketPrefix, fooConsumerDev, fooSnapshot("moved"))
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
		setting models.Override
		// fault makes the new location fail; nil leaves it unreachable.
		fault func(h *systemtest.Harness)
	}{
		"endpoint that can't be reached": {
			setting: clientSetting(client.ObjectStoreEndpointItem, "https://nowhere.example"),
		},
		"bucket that can't be read": {
			setting: clientSetting(client.BucketPrefixItem, "broken"),
			fault: func(h *systemtest.Harness) {
				h.Store.SetGetFault(func(bucket, _, _ string) error {
					if bucket == "broken-dev" {
						return storeFailures["5xx"]
					}
					return nil
				})
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { checkLocationNeverStrands(t, tc.setting, tc.fault) })
	}
}

// checkLocationNeverStrands moves a client to a location that fails, with
// setting and fault, and checks it keeps reading the old one until fixed.
func checkLocationNeverStrands(t *testing.T, setting models.Override, fault func(h *systemtest.Harness)) {
	t.Helper()

	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)
	if fault != nil {
		fault(h)
	}

	h.CreateOverride(t, setting)
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
	// where the fix arrives: the setting goes back to its Default.
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

func TestClientSettingsStartFromEnvironmentVariables(t *testing.T) {
	h := systemtest.New(t)
	other := h.NewEndpoint("https://objects.example")
	h.PutSnapshotAt(t, other, "custom", fooConsumerDev, fooSnapshot("from-env-location"))

	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev", Env: map[string]string{
		"MIMIR_OBJECT_STORE_ENDPOINT": "https://objects.example",
		"MIMIR_BUCKET_PREFIX":         "custom",
		"MIMIR_POLL_INTERVAL":         "10s",
	}})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	if got := cfg.GetFoo(); got != "from-env-location" {
		t.Errorf("GetFoo() = %q, want %q from custom-dev at the endpoint in the environment", got, "from-env-location")
	}

	h.PutSnapshotAt(t, other, "custom", fooConsumerDev, fooSnapshot("polled"))
	h.NextPollAfter(t, 10*time.Second)
	if got := cfg.GetFoo(); got != "polled" {
		t.Errorf("10s on, with MIMIR_POLL_INTERVAL=10s, GetFoo() = %q, want %q", got, "polled")
	}
}

func TestBadClientSettingIsLoggedAndTheLastGoodOneKept(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{
		Name:        "foo-consumer",
		Environment: "dev",
		Env:         map[string]string{"MIMIR_POLL_INTERVAL": "soon"},
	})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	if !strings.Contains(d.Logs(), "MIMIR_POLL_INTERVAL") {
		t.Errorf("the bad environment variable wasn't logged; logs:\n%s", d.Logs())
	}

	// mimir-server accepts any string, but the client rejects a value that
	// isn't a usable interval and keeps polling at the Default.
	h.CreateOverride(t, clientSetting(client.PollIntervalItem, "0s"))
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
		t.Errorf("the program was sent value events for the client's own settings: %+v", got)
	}
}

func TestRestartBeginsAgainFromTheStartingLocation(t *testing.T) {
	h := systemtest.New(t)
	_, first := startFooConsumer(t, h)
	h.CreateOverride(t, clientSetting(client.BucketPrefixItem, "mimir-next"))
	h.CopySnapshot(t, mimirClientDev, h.Store, "mimir-next")
	h.PutSnapshotAt(t, h.Store, "mimir-next", fooConsumerDev, fooSnapshot("moved"))
	h.NextPoll(t)
	if got := first.TakeLocationSwitches(); len(got) != 1 || got[0] != nil {
		t.Fatalf("location switches = %v, want one that succeeded", got)
	}

	// The pod restarts while its starting location can't be read. It
	// doesn't remember the adopted prefix, so it runs on Defaults rather
	// than reading mimir-next-dev.
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
		t.Errorf("once mimir-dev reads again, GetFoo() = %q, want %q from mimir-next-dev", got, "moved")
	}
}

func TestServerURLOverrideIsUsedForRegistration(t *testing.T) {
	h := systemtest.New(t)
	startFooConsumer(t, h)
	h.CreateOverride(t, clientSetting(client.ServerURLItem, h.URL))

	// A pod whose MIMIR_SERVER_URL points nowhere reads the server URL from
	// its settings before registering.
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev", ServerDown: true})
	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Errorf("Registration with the server URL from the Override failed: %v", err)
	}
}
