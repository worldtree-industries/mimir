package systemtest_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

func fooNamespace() models.Namespace {
	return models.Namespace{Config: foo.FooConfigName, Deployable: "foo-consumer", Environment: "dev"}
}

func tunedValues(value string) []models.SnapshotValue {
	return []models.SnapshotValue{{Item: models.ItemKey{Name: "foo", Type: models.TypeString}, Value: value}}
}

func registerFooConsumer(t *testing.T, h *systemtest.Harness) *foo.FooConfig {
	t.Helper()

	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	return cfg
}

func TestReconcileHealsSnapshotStaleFromFailedUpload(t *testing.T) {
	h := systemtest.New(t)
	cfg := registerFooConsumer(t, h)

	h.FailUploads(true)
	h.CreateOverride(t, fooOverride("tuned"))
	h.FailUploads(false)

	if snap, _ := h.Snapshot(t, fooNamespace()); len(snap.Values) != 0 {
		t.Fatalf("Snapshot holds %+v although its upload failed", snap.Values)
	}

	h.Reconcile(t)

	snap, _ := h.Snapshot(t, fooNamespace())
	if !reflect.DeepEqual(snap.Values, tunedValues("tuned")) {
		t.Errorf("after reconcile Snapshot values = %+v, want %+v", snap.Values, tunedValues("tuned"))
	}

	h.NextPoll(t)
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("after reconcile GetFoo() = %q, want %q", got, "tuned")
	}
}

func TestReconcileRestoresMissingSnapshot(t *testing.T) {
	h := systemtest.New(t)
	registerFooConsumer(t, h)
	h.CreateOverride(t, fooOverride("tuned"))
	h.DeleteSnapshot(t, fooNamespace())

	if _, ok := h.Snapshot(t, fooNamespace()); ok {
		t.Fatal("Snapshot still exists after being deleted")
	}

	h.Reconcile(t)

	snap, ok := h.Snapshot(t, fooNamespace())
	if !ok || !reflect.DeepEqual(snap.Values, tunedValues("tuned")) {
		t.Errorf("after reconcile Snapshot = %+v (exists %t), want values %+v", snap.Values, ok, tunedValues("tuned"))
	}
}

func TestReconcileRunsAgainEveryInterval(t *testing.T) {
	h := systemtest.New(t)
	registerFooConsumer(t, h)

	h.Reconcile(t)
	h.DeleteSnapshot(t, fooNamespace())
	h.Reconcile(t)

	if _, ok := h.Snapshot(t, fooNamespace()); !ok {
		t.Error("second reconcile didn't republish the Snapshot")
	}
}

func TestServersSharingADatabaseAndStoreConverge(t *testing.T) {
	h := systemtest.New(t)
	other := h.NewReplica(t)
	registerFooConsumer(t, h)

	// The Override lands on one replica whose upload fails; the other
	// replica, with the same database, heals it.
	h.FailUploads(true)
	other.CreateOverride(t, fooOverride("from-other"))
	h.FailUploads(false)
	if snap, _ := h.Snapshot(t, fooNamespace()); len(snap.Values) != 0 {
		t.Fatalf("Snapshot holds %+v although its upload failed", snap.Values)
	}

	h.Reconcile(t)

	first, ok := h.Snapshot(t, fooNamespace())
	if !ok || !reflect.DeepEqual(first.Values, tunedValues("from-other")) {
		t.Fatalf("replicas reconciled to %+v (exists %t), want values %+v", first.Values, ok, tunedValues("from-other"))
	}
	etag := h.SnapshotETag(t, fooNamespace())

	// Every replica publishes the same bytes, whoever writes last.
	h.Reconcile(t)
	h.Reconcile(t)

	if got := h.SnapshotETag(t, fooNamespace()); got != etag {
		t.Errorf("Snapshot ETag changed from %s to %s though nothing did", etag, got)
	}
}

func TestReadyzReportsPostgresAndObjectStorage(t *testing.T) {
	h := systemtest.New(t)
	registerFooConsumer(t, h)

	if got := h.Ready(t); got != http.StatusOK {
		t.Fatalf("/readyz with everything up: %d, want 200", got)
	}

	h.FailObjectStore(true)
	h.AdvancePastReadinessCache()
	if got := h.Ready(t); got != http.StatusServiceUnavailable {
		t.Errorf("/readyz with object storage down: %d, want 503", got)
	}

	h.FailObjectStore(false)
	h.AdvancePastReadinessCache()
	if got := h.Ready(t); got != http.StatusOK {
		t.Errorf("/readyz after object storage returned: %d, want 200", got)
	}

	h.StopPostgres()
	h.AdvancePastReadinessCache()
	if got := h.Ready(t); got != http.StatusServiceUnavailable {
		t.Errorf("/readyz with Postgres down: %d, want 503", got)
	}
}

func TestReadyzChecksDependenciesAtMostOncePerCacheWindow(t *testing.T) {
	h := systemtest.New(t)
	registerFooConsumer(t, h)
	h.Ready(t)
	before := h.StoreCalls()

	for range 20 {
		h.Ready(t)
	}

	if after := h.StoreCalls(); after != before {
		t.Errorf("20 probes made %d object storage calls, want none inside the cache window", after-before)
	}
}
