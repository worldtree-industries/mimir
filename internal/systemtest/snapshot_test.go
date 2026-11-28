package systemtest_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/objectstore/objectstoretest"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

func TestFirstRegistrationPublishesSnapshotAtConstructedLocation(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	ns := models.Namespace{Config: foo.FooConfigName, Deployable: "foo-consumer", Environment: "dev"}

	if _, ok := h.Snapshot(t, ns); ok {
		t.Fatal("a Snapshot exists before the Namespace ever registered")
	}

	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	snap, ok := h.Snapshot(t, ns)
	if !ok {
		t.Fatalf("no Snapshot at bucket mimir-dev, object %s/foo-consumer.json after first Registration", ns.Config)
	}
	if len(snap.Values) != 0 {
		t.Errorf("Snapshot with no Overrides holds %v, want nothing", snap.Values)
	}
}

func TestSnapshotHoldsOnlyItemsWithAnOverride(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	h.CreateOverride(t, fooOverride("tuned"))

	snap, _ := h.Snapshot(
		t,
		models.Namespace{Config: foo.FooConfigName, Deployable: "foo-consumer", Environment: "dev"},
	)
	want := []models.SnapshotValue{{Item: models.ItemRef{Name: "foo", Type: models.TypeString}, Value: "tuned"}}
	if !reflect.DeepEqual(snap.Values, want) {
		t.Errorf("Snapshot values = %+v, want %+v", snap.Values, want)
	}
}

func TestOverrideReachesEveryNamespaceOfItsConfig(t *testing.T) {
	h := systemtest.New(t)
	devFoo := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	prodBar := h.NewDeployable(t, systemtest.Deployable{Name: "bar-consumer", Environment: "prod"})

	devFooCfg := foo.NewFooConfig(t.Context(), devFoo.ClientOptions()...)
	prodBarCfg := foo.NewFooConfig(t.Context(), prodBar.ClientOptions()...)
	for _, d := range []*systemtest.RunningDeployable{devFoo, prodBar} {
		if err := d.AwaitRegistration(t); err != nil {
			t.Fatalf("Registration of %s failed: %v", d.Name, err)
		}
	}

	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)

	if got := devFooCfg.GetFoo(); got != "tuned" {
		t.Errorf("foo-consumer in dev: GetFoo() = %q, want %q", got, "tuned")
	}
	if got := prodBarCfg.GetFoo(); got != "tuned" {
		t.Errorf("bar-consumer in prod: GetFoo() = %q, want %q", got, "tuned")
	}
}

func TestOverrideOfUnregisteredItemIsRejected(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	// foo is registered as a string, so as an int it is a different Item.
	o := fooOverride("7")
	o.Item.Type = models.TypeInt

	if status, body := h.Post(t, "/v1/overrides", o); status != http.StatusUnprocessableEntity {
		t.Errorf("create Override of an unregistered Item: status %d, want 422; body %s", status, body)
	}
}

func TestServerPublishesToMimirBucketsWhateverTheEnvironment(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	s3 := objectstoretest.NewFakeS3(t)

	// MIMIR_BUCKET_PREFIX once set the prefix (ADR 0007).
	h := systemtest.New(t, systemtest.WithServerEnv(map[string]string{
		"AWS_ENDPOINT_URL_S3": s3.URL,
		"MIMIR_BUCKET_PREFIX": "acme-config",
	}))
	d := h.NewDeployable(t, systemtest.Deployable{
		Name:          "foo-consumer",
		Environment:   "dev",
		NoObjectStore: true,
		Env:           map[string]string{"AWS_ENDPOINT_URL_S3": s3.URL},
	})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)

	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want %q read from mimir-dev", got, "tuned")
	}
	key := models.SnapshotLocationFor(fooConsumerDev).Key
	if !s3.Has("mimir-dev", key) {
		t.Errorf("no Snapshot published to mimir-dev/%s", key)
	}
	if s3.Has("acme-config-dev", key) {
		t.Errorf("a Snapshot was published to acme-config-dev/%s", key)
	}
}
