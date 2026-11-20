package systemtest_test

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/worldtree-industries/mimir/client"
	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/objectstore"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// fooKey is foo's string Item "foo".
//
//nolint:gochecknoglobals // a fixed, read-only Item key
var fooKey = models.ItemKey{Name: "foo", Type: models.TypeString}

// fooConsumerDev is foo-consumer's Namespace in dev.
//
//nolint:gochecknoglobals // a fixed, read-only Namespace
var fooConsumerDev = models.Namespace{Config: foo.FooConfigName, Deployable: "foo-consumer", Environment: "dev"}

// startFooConsumer runs foo-consumer in dev and waits for its Registration.
func startFooConsumer(t *testing.T, h *systemtest.Harness) (*foo.FooConfig, *systemtest.RunningDeployable) {
	t.Helper()

	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	return cfg, d
}

func TestProgramIsToldWhichValuesChanged(t *testing.T) {
	h := systemtest.New(t)
	_, d := startFooConsumer(t, h)
	h.NextPoll(t)
	d.TakeValueEvents()

	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)

	want := []client.Event{{Kind: client.EventValueChanged, Item: fooKey, Value: "tuned"}}
	if got := d.TakeValueEvents(); !slices.Equal(got, want) {
		t.Errorf("value events = %+v, want %+v", got, want)
	}
}

func TestUnchangedSnapshotIsPolledConditionallyAndNotReapplied(t *testing.T) {
	h := systemtest.New(t)
	_, d := startFooConsumer(t, h)
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)
	d.TakeValueEvents()

	var sent []string
	h.Store.SetGetFault(func(_, _, etag string) error {
		sent = append(sent, etag)
		return nil
	})
	h.NextPoll(t)
	h.Store.SetGetFault(nil)

	current := h.SnapshotETag(t, fooConsumerDev)
	if len(sent) != 1 || sent[0] != current {
		t.Errorf("poll sent ETags %q, want one conditional GET with the Snapshot's ETag %q", sent, current)
	}
	if got := d.TakeValueEvents(); len(got) != 0 {
		t.Errorf("an unchanged Snapshot reapplied values: %+v", got)
	}
}

func TestMissingSnapshotMeansDefaults(t *testing.T) {
	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)
	if got := cfg.GetFoo(); got != "tuned" {
		t.Fatalf("GetFoo() = %q, want the Override %q", got, "tuned")
	}
	d.TakeValueEvents()

	// The Snapshot goes away, as when the Namespace expires: no Overrides.
	h.DeleteSnapshot(t, fooConsumerDev)
	h.NextPoll(t)

	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("without a Snapshot, GetFoo() = %q, want the Default %q", got, "default")
	}
	if err := d.LastPollError(); err != nil {
		t.Errorf("a missing Snapshot was reported as a failed poll: %v", err)
	}
	want := []client.Event{{Kind: client.EventValueChanged, Item: fooKey, Value: "default"}}
	if got := d.TakeValueEvents(); !slices.Equal(got, want) {
		t.Errorf("value events = %+v, want %+v", got, want)
	}

	h.NextPoll(t)
	if got := d.TakeValueEvents(); len(got) != 0 {
		t.Errorf("a still-missing Snapshot reapplied values: %+v", got)
	}
}

// storeFailures are the ways object storage can fail to answer.
//
//nolint:gochecknoglobals // fixed, read-only test cases
var storeFailures = map[string]error{
	"network error": &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
	"5xx":           errors.New("503 Service Unavailable"),
}

func failGets(err error) objectstore.GetFault {
	return func(_, _, _ string) error { return err }
}

func TestUnreachableStoreKeepsLastGoodValues(t *testing.T) {
	for name, failure := range storeFailures {
		t.Run(name, func(t *testing.T) {
			h := systemtest.New(t)
			cfg, d := startFooConsumer(t, h)
			h.CreateOverride(t, fooOverride("tuned"))
			h.NextPoll(t)
			d.TakeValueEvents()

			h.Store.SetGetFault(failGets(failure))
			h.NextPoll(t)

			if got := cfg.GetFoo(); got != "tuned" {
				t.Errorf("with the store unreachable, GetFoo() = %q, want the last good %q", got, "tuned")
			}
			if err := d.LastPollError(); !errors.Is(err, failure) {
				t.Errorf("poll reported %v, want %v", err, failure)
			}
			if !strings.Contains(d.Logs(), "keeping current values") {
				t.Errorf("the failed poll wasn't logged; logs:\n%s", d.Logs())
			}
			if got := d.TakeValueEvents(); len(got) != 0 {
				t.Errorf("a failed poll changed values: %+v", got)
			}
		})
	}
}

func TestColdStartWithStoreUnreachableUsesDefaultsUntilReachable(t *testing.T) {
	h := systemtest.New(t)
	startFooConsumer(t, h)
	h.CreateOverride(t, fooOverride("tuned"))

	// Another pod starts while object storage can't be reached.
	h.Store.SetGetFault(failGets(storeFailures["network error"]))
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("at a cold start with the store unreachable, GetFoo() = %q, want the Default %q", got, "default")
	}

	h.NextPoll(t)
	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("while the store stays unreachable, GetFoo() = %q, want the Default %q", got, "default")
	}

	h.Store.SetGetFault(nil)
	h.NextPoll(t)
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("once the store is reachable, GetFoo() = %q, want the Override %q", got, "tuned")
	}
}

func TestValueThatDoesNotParseKeepsLastGoodValue(t *testing.T) {
	h := systemtest.New(t)
	cfg, d := startFooConsumer(t, h)
	barKey := models.ItemKey{Name: "bar", Type: models.TypeInt}
	h.PutSnapshot(t, fooConsumerDev, models.NewSnapshot([]models.SnapshotValue{{Item: barKey, Value: "42"}}))
	h.NextPoll(t)
	d.TakeValueEvents()

	// A bad value reaches object storage alongside a good one.
	h.PutSnapshot(t, fooConsumerDev, models.NewSnapshot([]models.SnapshotValue{
		{Item: barKey, Value: "forty-two"},
		{Item: fooKey, Value: "tuned"},
	}))
	h.NextPoll(t)

	if got := cfg.GetBar(); got != 42 {
		t.Errorf("GetBar() = %d, want the last good 42", got)
	}
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want %q: one bad value mustn't hold back the rest", got, "tuned")
	}
	if !strings.Contains(d.Logs(), "forty-two") {
		t.Errorf("the bad value wasn't logged; logs:\n%s", d.Logs())
	}
	var rejected, changed []client.Event
	for _, e := range d.TakeValueEvents() {
		switch e.Kind {
		case client.EventValueRejected:
			rejected = append(rejected, e)
		case client.EventValueChanged:
			changed = append(changed, e)
		case client.EventRegistered, client.EventPolled:
		}
	}
	if len(rejected) != 1 || rejected[0].Item != barKey || rejected[0].Value != "forty-two" || rejected[0].Err == nil {
		t.Errorf("rejections = %+v, want bar's %q rejected with an error", rejected, "forty-two")
	}
	wantChanged := []client.Event{{Kind: client.EventValueChanged, Item: fooKey, Value: "tuned"}}
	if !slices.Equal(changed, wantChanged) {
		t.Errorf("changes = %+v, want %+v", changed, wantChanged)
	}

	// The bad value is reported once, not on every poll that still finds it.
	h.PutSnapshot(t, fooConsumerDev, models.NewSnapshot([]models.SnapshotValue{
		{Item: barKey, Value: "forty-two"},
		{Item: fooKey, Value: "retuned"},
	}))
	h.NextPoll(t)
	want := []client.Event{{Kind: client.EventValueChanged, Item: fooKey, Value: "retuned"}}
	if got := d.TakeValueEvents(); !slices.Equal(got, want) {
		t.Errorf("value events = %+v, want only %+v", got, want)
	}
}

func TestGettersAreSafeDuringPolling(t *testing.T) {
	h := systemtest.New(t)
	cfg, _ := startFooConsumer(t, h)

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_, _, _ = cfg.GetFoo(), cfg.GetBar(), cfg.GetQuux()
					_, _ = cfg.GetCorge(), cfg.GetGrault()
				}
			}
		})
	}

	for i := range 5 {
		h.PutSnapshot(t, fooConsumerDev, models.NewSnapshot([]models.SnapshotValue{
			{Item: fooKey, Value: fmt.Sprintf("v%d", i)},
			{Item: models.ItemKey{Name: "quux", Type: models.TypeStringArray}, Value: fmt.Sprintf(`["v%d"]`, i)},
			{Item: models.ItemKey{Name: "corge", Type: models.TypeStringSet}, Value: fmt.Sprintf(`["v%d"]`, i)},
		}))
		h.NextPoll(t)
	}
	close(stop)
	readers.Wait()

	if got := cfg.GetFoo(); got != "v4" {
		t.Errorf("GetFoo() = %q, want the last polled %q", got, "v4")
	}
}
