package systemtest_test

import (
	"strings"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/client"
	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
)

// fooItemLastRegistered is when the Item of the foo Config named name was last
// registered.
func fooItemLastRegistered(t *testing.T, h *systemtest.Harness, name string) time.Time {
	t.Helper()

	for _, item := range h.Items(t, foo.FooConfigName) {
		if item.Name == name {
			return item.LastRegisteredAt
		}
	}
	t.Fatalf("Item %s isn't registered", name)

	return time.Time{}
}

func TestClientsReRegisterAboutDaily(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	h.Day(t)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("re-Registration failed: %v", err)
	}

	reregistered := systemtest.Epoch.Add(client.DefaultRegistrationInterval)
	if got := fooItemLastRegistered(t, h, "foo"); !got.Equal(reregistered) {
		t.Errorf("Item last registered at %v, want the re-Registration at %v", got, reregistered)
	}
	if got, want := h.Namespaces(t, foo.FooConfigName)[0].LastRegisteredAt, reregistered; !got.Equal(want) {
		t.Errorf("Namespace last registered at %v, want %v", got, want)
	}
}

func TestFailedReRegistrationIsLoggedRetriedAndNeverAffectsValues(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	h.CreateOverride(t, fooOverride("tuned"))
	h.NextPoll(t)

	h.RejectRegistrations(true)
	h.Day(t)
	if err := d.AwaitRegistration(t); err == nil {
		t.Fatal("re-Registration succeeded although the server rejected it")
	}
	if !strings.Contains(d.Logs(), "Registration failed") {
		t.Errorf("the failed re-Registration wasn't logged; logs:\n%s", d.Logs())
	}
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("GetFoo() = %q after the failure, want %q unaffected", got, "tuned")
	}

	// The retry comes well before the next daily one.
	h.RejectRegistrations(false)
	h.Advance(t, client.RegistrationRetryInterval)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("the retried Registration failed: %v", err)
	}

	if got, want := fooItemLastRegistered(t, h, "foo"), h.Clock.Now(); !got.Equal(want) {
		t.Errorf("Item last registered at %v, want the retry's time %v", got, want)
	}
}
