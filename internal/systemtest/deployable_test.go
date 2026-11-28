package systemtest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/objectstore/objectstoretest"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

func TestDeployableNameComesFromBuildInfo(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{
		BuildPath:   "example.com/acme/shop/cmd/checkout",
		Environment: "dev",
	})

	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	if _, ok := h.Snapshot(
		t,
		models.Namespace{Config: foo.FooConfigName, Deployable: "checkout", Environment: "dev"},
	); !ok {
		t.Error("no Snapshot for Deployable checkout, the name in cmd/checkout")
	}
}

func TestMIMIRDEPLOYABLEOverridesBuildInfo(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{
		Name:        "checkout-canary",
		BuildPath:   "example.com/acme/shop/cmd/checkout",
		Environment: "dev",
	})

	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)

	if _, ok := h.Snapshot(
		t,
		models.Namespace{Config: foo.FooConfigName, Deployable: "checkout", Environment: "dev"},
	); ok {
		t.Error("a Snapshot was published for checkout, the build info name, despite MIMIR_DEPLOYABLE")
	}
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want %q read from checkout-canary's Snapshot", got, "tuned")
	}
}

func TestRegistrationFailureDoesNotStopReadingSnapshotValues(t *testing.T) {
	h := systemtest.New(t)
	first := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), first.ClientOptions()...)
	if err := first.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	h.CreateOverride(t, fooOverride("tuned"))

	// A second pod of the same Deployable starts while mimir-server is
	// unreachable: it still reads its real values from object storage.
	second := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev", ServerDown: true})
	cfg := foo.NewFooConfig(t.Context(), second.ClientOptions()...)

	if err := second.AwaitRegistration(t); err == nil {
		t.Fatal("Registration against a stopped server succeeded")
	}
	if !strings.Contains(second.Logs(), "Registration failed") {
		t.Errorf("Registration failure wasn't logged; logs:\n%s", second.Logs())
	}
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q, want the Snapshot's %q", got, "tuned")
	}
	if got := cfg.GetBar(); got != 55555 {
		t.Errorf("GetBar() = %d, want its Default 55555", got)
	}
}

func TestRegistrationFailureLeavesDefaults(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev", ServerDown: true})

	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err == nil {
		t.Fatal("Registration against a stopped server succeeded")
	}

	h.NextPoll(t)
	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("GetFoo() = %q, want the Default %q", got, "default")
	}
}

func TestClientRegistersEvenWithoutObjectStorage(t *testing.T) {
	cases := map[string]systemtest.Deployable{
		"none configured":          {Name: "foo-consumer", Environment: "dev", NoObjectStore: true},
		"one that can't be opened": {Name: "foo-consumer", Environment: "dev", ObjectStoreDown: true},
	}
	for name, deployable := range cases {
		t.Run(name, func(t *testing.T) {
			h := systemtest.New(t)
			d := h.NewDeployable(t, deployable)

			cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
			if got := cfg.GetFoo(); got != "default" {
				t.Errorf("GetFoo() = %q, want the Default %q", got, "default")
			}
			if err := d.AwaitRegistration(t); err != nil {
				t.Fatalf("Registration failed: %v", err)
			}
			if got := h.Namespaces(t, foo.FooConfigName); len(got) != 1 || got[0].Namespace != fooConsumerDev {
				t.Errorf("registered Namespaces = %+v, want only %+v", got, fooConsumerDev)
			}
		})
	}
}

func TestClientReadsS3FromTheStandardAWSVariables(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	s3 := objectstoretest.NewFakeS3(t)
	body, err := json.Marshal(fooSnapshot("from-s3"))
	if err != nil {
		t.Fatal(err)
	}
	loc := models.SnapshotLocationFor(fooConsumerDev)
	s3.Put(loc.Bucket, loc.Key, body)

	// No object storage is passed in: the client opens S3 from the
	// environment, as it does in production.
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{
		Name:          "foo-consumer",
		Environment:   "dev",
		NoObjectStore: true,
		Env:           map[string]string{"AWS_ENDPOINT_URL_S3": s3.URL},
	})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)

	if got := cfg.GetFoo(); got != "from-s3" {
		t.Errorf("GetFoo() = %q, want %q read from S3", got, "from-s3")
	}
}
