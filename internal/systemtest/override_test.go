package systemtest_test

import (
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// fooOverride is a Config-wide Override of foo's string Item "foo".
func fooOverride(value string) models.Override {
	return models.Override{
		Scope: models.Scope{Config: foo.FooConfigName},
		Item:  models.ItemRef{Name: "foo", Type: models.TypeString},
		Value: value,
	}
}

func TestConfigWideOverrideReachesRunningConfigAfterNextPoll(t *testing.T) {
	h := systemtest.New(t)
	app := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})

	cfg := foo.NewFooConfig(t.Context(), app.ClientOptions()...)
	if err := app.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
	if got := cfg.GetFoo(); got != "default" {
		t.Fatalf("before any Override, GetFoo() = %q, want the Default %q", got, "default")
	}

	h.CreateOverride(t, fooOverride("tuned"))
	if got := cfg.GetFoo(); got != "default" {
		t.Errorf("before the next poll, GetFoo() = %q, want %q still", got, "default")
	}

	h.NextPoll(t)

	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("after the next poll, GetFoo() = %q, want the Override %q", got, "tuned")
	}
	if got := cfg.GetBar(); got != 55555 {
		t.Errorf("GetBar() = %d, want its Default 55555", got)
	}
}
