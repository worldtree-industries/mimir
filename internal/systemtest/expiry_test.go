package systemtest_test

import (
	"slices"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// expiryAge is how long an Item or Namespace can go unregistered before it
// expires (ADR 0004).
const expiryAge = 60 * systemtest.Day

func bannerKey() models.ItemKey  { return models.ItemKey{Name: "banner", Type: models.TypeString} }
func timeoutKey() models.ItemKey { return models.ItemKey{Name: "timeout", Type: models.TypeInt} }
func themeKey() models.ItemKey   { return models.ItemKey{Name: "theme", Type: models.TypeString} }

// registerEachDay moves the clock on a day at a time, n times, and has every
// reg in regs register again after each, as running pods of those versions
// would.
func registerEachDay(t *testing.T, h *systemtest.Harness, n int, regs ...models.Registration) {
	t.Helper()

	for range n {
		h.Elapse(t, systemtest.Day)
		for _, reg := range regs {
			h.Register(t, reg)
		}
	}
}

func TestItemUnregisteredForSixtyDaysExpiresWithItsOverrides(t *testing.T) {
	h := systemtest.New(t)
	h.Register(t, shopRegistration(
		shopItem("timeout", models.TypeInt, "30"),
		shopItem("banner", models.TypeString, "hi"),
	))
	h.CreateOverride(t, shopOverride("banner", models.TypeString, "sale"))
	h.CreateOverride(t, shopOverride("timeout", models.TypeInt, "45"))

	// From now on only a version that doesn't declare banner registers.
	h.Elapse(t, expiryAge-time.Hour)
	h.Register(t, shopRegistration(shopItem("timeout", models.TypeInt, "30")))
	h.Elapse(t, time.Hour)

	// banner was last registered exactly sixty days ago, and so expires.
	if got, want := itemKeys(t, h), []models.ItemKey{timeoutKey()}; !slices.Equal(got, want) {
		t.Errorf("Items after sixty days = %v, want only %v", got, want)
	}

	snap, ok := h.Snapshot(t, shopDevNamespace())
	if !ok {
		t.Fatal("the Namespace's Snapshot is gone")
	}
	if _, found := snap.Value(bannerKey()); found {
		t.Errorf("Snapshot %+v still holds the expired Item's Override", snap.Values)
	}
	if got, found := snap.Value(timeoutKey()); !found || got != "45" {
		t.Errorf("Snapshot value of timeout = %q (present %t), want its Override %q kept", got, found, "45")
	}

	history := h.History(t, models.Scope{Config: shopConfig})
	if len(history) != 3 {
		t.Fatalf("history = %+v, want two creations and the expiry of banner's Override", history)
	}
	expired := history[2]
	if expired.Action != models.HistoryDeleted || expired.Item != bannerKey() || expired.Scope.Config != shopConfig {
		t.Errorf("last history entry = %+v, want banner's Override deleted", expired)
	}
	if expired.OldValue == nil || *expired.OldValue != "sale" || expired.NewValue != nil {
		t.Errorf("expiry recorded old/new = %v/%v, want sale/none", expired.OldValue, expired.NewValue)
	}
	if want := systemtest.Epoch.Add(expiryAge); !expired.Time.Equal(want) {
		t.Errorf("expiry recorded at %v, want %v", expired.Time, want)
	}
	if expired.Caller != "mimir-server:expiry" {
		t.Errorf("expiry's caller = %q, want mimir-server:expiry", expired.Caller)
	}
}

func TestItemRegisteredWithinSixtyDaysDoesNotExpire(t *testing.T) {
	h := systemtest.New(t)
	h.Register(t, shopRegistration(shopItem("banner", models.TypeString, "hi")))
	h.CreateOverride(t, shopOverride("banner", models.TypeString, "sale"))

	h.Elapse(t, expiryAge-time.Hour)

	if got, want := itemKeys(t, h), []models.ItemKey{bannerKey()}; !slices.Equal(got, want) {
		t.Errorf("Items a little under sixty days on = %v, want %v still", got, want)
	}
	if snap, _ := h.Snapshot(t, shopDevNamespace()); len(snap.Values) != 1 {
		t.Errorf("Snapshot = %+v, want the Override kept", snap.Values)
	}
	if entries := h.History(t, models.Scope{Config: shopConfig}); len(entries) != 1 {
		t.Errorf("history = %+v, want only the creation", entries)
	}
}

func TestMixedVersionRolloutKeepsEveryItemEitherVersionRegisters(t *testing.T) {
	h := systemtest.New(t)
	oldVersion := shopRegistration(
		shopItem("timeout", models.TypeInt, "30"),
		shopItem("banner", models.TypeString, "hi"),
	)
	newVersion := shopRegistration(
		shopItem("timeout", models.TypeInt, "30"),
		shopItem("theme", models.TypeString, "dark"),
	)
	h.Register(t, oldVersion)
	h.CreateOverride(t, shopOverride("banner", models.TypeString, "sale"))

	// Both versions run side by side for a while, each registering what it
	// declares, so neither's Items are removed nor does the new one's conflict.
	registerEachDay(t, h, 10, oldVersion, newVersion)
	want := []models.ItemKey{bannerKey(), themeKey(), timeoutKey()}
	if got := itemKeys(t, h); !slices.Equal(got, want) {
		t.Fatalf("Items during the rollout = %v, want %v", got, want)
	}

	// The old version is gone. Its last pod registered 59 days from now: banner
	// is still there; a day later it expires, and the new version's Items stay.
	registerEachDay(t, h, 59, newVersion)
	if got := itemKeys(t, h); !slices.Equal(got, want) {
		t.Fatalf("Items 59 days after the old version left = %v, want %v still", got, want)
	}
	registerEachDay(t, h, 1, newVersion)

	if got, remaining := itemKeys(t, h), []models.ItemKey{themeKey(), timeoutKey()}; !slices.Equal(got, remaining) {
		t.Errorf("Items 60 days after the old version left = %v, want %v", got, remaining)
	}
}

func TestNamespaceUnregisteredForSixtyDaysExpiresAndItsSnapshotIsDeleted(t *testing.T) {
	h := systemtest.New(t)
	kept := shopDevNamespace()
	retired := models.Namespace{Config: shopConfig, Deployable: "checkout", Environment: "staging"}
	items := []models.Item{shopItem("banner", models.TypeString, "hi")}
	keptReg := models.Registration{Namespace: kept, Items: items}
	h.Register(t, keptReg)
	h.Register(t, models.Registration{Namespace: retired, Items: items})
	h.CreateOverride(t, shopOverride("banner", models.TypeString, "sale"))
	if _, ok := h.Snapshot(t, retired); !ok {
		t.Fatal("the retired Namespace has no Snapshot to begin with")
	}

	registerEachDay(t, h, 59, keptReg)
	if _, ok := h.Snapshot(t, retired); !ok {
		t.Fatal("the Snapshot was deleted before the Namespace was unregistered for sixty days")
	}
	registerEachDay(t, h, 1, keptReg)

	if _, ok := h.Snapshot(t, retired); ok {
		t.Error("the expired Namespace's Snapshot is still in object storage")
	}
	if _, ok := h.Snapshot(t, kept); !ok {
		t.Error("the registered Namespace's Snapshot was deleted")
	}
	namespaces := h.Namespaces(t, shopConfig)
	if len(namespaces) != 1 || namespaces[0].Namespace != kept {
		t.Errorf("Namespaces = %+v, want only %+v", namespaces, kept)
	}
	if got, want := itemKeys(t, h), []models.ItemKey{bannerKey()}; !slices.Equal(got, want) {
		t.Errorf("Items = %v, want %v: a Namespace expiring doesn't expire Items", got, want)
	}
}

func TestRunningClientKeepsItsItemsAndNamespaceFromExpiring(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	h.CreateOverride(t, fooOverride("tuned"))

	for day := 1; day <= 61; day++ {
		h.Day(t)
		if err := d.AwaitRegistration(t); err != nil {
			t.Fatalf("re-Registration on day %d failed: %v", day, err)
		}
	}

	if got := len(h.Items(t, foo.FooConfigName)); got != 8 {
		t.Errorf("Items after 61 days of a running client = %d, want all 8", got)
	}
	if got := len(h.Namespaces(t, foo.FooConfigName)); got != 1 {
		t.Errorf("Namespaces after 61 days of a running client = %d, want 1", got)
	}
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want the Override %q kept", got, "tuned")
	}
}

func TestReplicasExpireEachItemOnce(t *testing.T) {
	h := systemtest.New(t)
	h.NewReplica(t)
	h.NewReplica(t)
	h.Register(t, shopRegistration(shopItem("banner", models.TypeString, "hi")))
	h.CreateOverride(t, shopOverride("banner", models.TypeString, "sale"))

	h.Elapse(t, expiryAge)

	if got := itemKeys(t, h); len(got) != 0 {
		t.Errorf("Items after sixty days = %v, want none", got)
	}
	entries := h.History(t, models.Scope{Config: shopConfig})
	if len(entries) != 2 || entries[1].Action != models.HistoryDeleted {
		t.Errorf("history = %+v, want the creation and one expiry", entries)
	}
}

func TestSnapshotRepublishedByAReconcileRacingExpiryIsDeleted(t *testing.T) {
	h := systemtest.New(t)
	kept := shopDevNamespace()
	retired := models.Namespace{Config: shopConfig, Deployable: "checkout", Environment: "staging"}
	items := []models.Item{shopItem("banner", models.TypeString, "hi")}
	keptReg := models.Registration{Namespace: kept, Items: items}
	h.Register(t, keptReg)
	h.Register(t, models.Registration{Namespace: retired, Items: items})
	registerEachDay(t, h, 59, keptReg)

	// The reconcile that starts alongside expiry has read the retired
	// Namespace, and uploads its Snapshot only after expiry deleted it.
	release := h.HoldUploads(t)
	h.Advance(t, systemtest.Day)
	h.AwaitExpiry(t)
	release()
	h.AwaitReconcile(t)

	if _, ok := h.Snapshot(t, retired); ok {
		t.Error("the expired Namespace's Snapshot was published again by a reconcile that raced expiry")
	}
}
