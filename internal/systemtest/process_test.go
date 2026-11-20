package systemtest_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/worldtree-industries/mimir/client"
	"github.com/worldtree-industries/mimir/internal/generator/testdata/alltypes"
	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// allTypesConsumerDev is foo-consumer's Namespace of the alltypes Config in
// dev, for a process reading two Configs.
//
//nolint:gochecknoglobals // a fixed, read-only Namespace
var allTypesConsumerDev = models.Namespace{
	Config: alltypes.AllTypesConfigName, Deployable: "foo-consumer", Environment: "dev",
}

// allTypesSnapshot is a Snapshot of the alltypes Config with its string
// Item set to value.
func allTypesSnapshot(value string) models.Snapshot {
	return models.NewSnapshot([]models.SnapshotValue{
		{Item: models.ItemKey{Name: "s", Type: models.TypeString}, Value: value},
	})
}

// recordGetKeys counts the Gets of each object of h.Store until the returned
// function is called, which returns the counts by key.
func recordGetKeys(h *systemtest.Harness) func() map[string]int {
	var mu sync.Mutex
	counts := map[string]int{}
	h.Store.SetGetFault(func(_, key, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		counts[key]++
		return nil
	})

	return func() map[string]int {
		h.Store.SetGetFault(nil)
		mu.Lock()
		defer mu.Unlock()
		return counts
	}
}

func TestProcessRunsOneMimirClientForAllItsConfigs(t *testing.T) {
	h := systemtest.New(t)
	h.PutSnapshot(t, allTypesConsumerDev, allTypesSnapshot("tuned at start"))

	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	fooCfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	allCfg := alltypes.NewAllTypesConfig(t.Context(), d.ClientOptions()...)
	for range 2 {
		if err := d.AwaitRegistration(t); err != nil {
			t.Fatalf("Registration failed: %v", err)
		}
	}

	// The second Config still starts on the values its Snapshot holds.
	if got := allCfg.GetS(); got != "tuned at start" {
		t.Errorf("second Config's GetS() at start = %q, want %q", got, "tuned at start")
	}
	if got := h.RegistrationsOf(client.MimirClientConfig); got != 1 {
		t.Errorf("MimirClient was registered %d times at start, want once for the process", got)
	}

	h.PutSnapshot(t, fooConsumerDev, fooSnapshot("tuned"))
	h.PutSnapshot(t, allTypesConsumerDev, allTypesSnapshot("polled"))
	gets := recordGetKeys(h)
	h.NextPoll(t)

	reads := gets()
	for _, ns := range []models.Namespace{mimirClientDev, fooConsumerDev, allTypesConsumerDev} {
		key := models.SnapshotLocationFor(models.DefaultBucketPrefix, ns).Key
		if reads[key] != 1 {
			t.Errorf("one poll read %s %d times, want once", key, reads[key])
		}
	}
	if got := fooCfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want %q", got, "tuned")
	}
	if got := allCfg.GetS(); got != "polled" {
		t.Errorf("GetS() = %q, want %q", got, "polled")
	}
}

func TestLaterConfigsDontWaitOnObjectStorageAlreadyFoundDown(t *testing.T) {
	h := systemtest.New(t)
	var reads atomic.Int32
	h.Store.SetGetFault(func(_, _, _ string) error {
		reads.Add(1)
		return storeFailures["5xx"]
	})

	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	before := reads.Load()
	if before == 0 {
		t.Fatal("the first Config never read object storage at start")
	}

	// Object storage didn't answer the process a moment ago, so the next
	// Config starts on its Defaults at once rather than waiting on it again.
	allCfg := alltypes.NewAllTypesConfig(t.Context(), d.ClientOptions()...)
	if after := reads.Load(); after != before {
		t.Errorf("the second Config read object storage %d times at start, want none", after-before)
	}
	for range 2 {
		if err := d.AwaitRegistration(t); err != nil {
			t.Fatalf("Registration failed: %v", err)
		}
	}

	// Once object storage answers, the next poll brings it its values.
	h.Store.SetGetFault(nil)
	h.PutSnapshot(t, allTypesConsumerDev, allTypesSnapshot("recovered"))
	h.NextPoll(t)
	if got := allCfg.GetS(); got != "recovered" {
		t.Errorf("after object storage recovered, GetS() = %q, want %q", got, "recovered")
	}
}
