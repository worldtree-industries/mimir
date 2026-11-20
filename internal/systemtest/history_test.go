package systemtest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

func registerFoo(t *testing.T, h *systemtest.Harness) {
	t.Helper()

	app := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), app.ClientOptions()...)
	if err := app.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
}

func TestCreatingAnOverrideIsRecordedInHistory(t *testing.T) {
	h := systemtest.New(t)
	registerFoo(t, h)
	h.Clock.Advance(time.Hour)

	body := fooOverride("tuned")
	status, resp := h.PostWithHeader(t, "/v1/overrides", body, "X-Mimir-Caller", "alice")
	if status != http.StatusCreated {
		t.Fatalf("create Override: status %d, body %s", status, resp)
	}

	got := h.History(t, models.Scope{Config: foo.FooConfigName})
	if len(got) != 1 {
		t.Fatalf("history after a create has %d entries, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.Action != models.HistoryCreated {
		t.Errorf("Action = %q, want %q", e.Action, models.HistoryCreated)
	}
	if e.Scope != body.Scope || e.Item != body.Item {
		t.Errorf("entry is for %+v %+v, want %+v %+v", e.Scope, e.Item, body.Scope, body.Item)
	}
	if e.OldValue != nil {
		t.Errorf("OldValue = %q, want none for a create", *e.OldValue)
	}
	if e.NewValue == nil || *e.NewValue != "tuned" {
		t.Errorf("NewValue = %v, want %q", e.NewValue, "tuned")
	}
	if want := systemtest.Epoch.Add(time.Hour); !e.Time.Equal(want) {
		t.Errorf("Time = %v, want the server clock's %v", e.Time, want)
	}
	if e.Caller != "alice" {
		t.Errorf("Caller = %q, want %q", e.Caller, "alice")
	}
}

func TestCallerIsTheRemoteAddressWhenNobodyIdentifiesThemselves(t *testing.T) {
	h := systemtest.New(t)
	registerFoo(t, h)
	h.CreateOverride(t, fooOverride("tuned"))

	got := h.History(t, models.Scope{Config: foo.FooConfigName})
	if len(got) != 1 || got[0].Caller == "" {
		t.Fatalf("history = %+v, want one entry with a Caller", got)
	}
}

func TestRejectedOverridesLeaveNoHistory(t *testing.T) {
	h := systemtest.New(t)
	registerFoo(t, h)

	bad := fooOverride("x")
	bad.Item.Type = models.TypeInt
	if status, _ := h.Post(t, "/v1/overrides", bad); status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", status)
	}

	if got := h.History(t, models.Scope{Config: foo.FooConfigName}); len(got) != 0 {
		t.Errorf("history = %+v, want none", got)
	}
}

func TestHistoryIsFilteredByConfigAndScope(t *testing.T) {
	h := systemtest.New(t)
	registerFoo(t, h)
	registerAllTypes(t, h)

	h.CreateOverride(t, fooOverride("tuned"))
	h.CreateOverride(t, allTypesOverride("i", models.TypeInt, "5"))

	if got := h.History(t, models.Scope{Config: foo.FooConfigName}); len(got) != 1 || got[0].Item.Name != "foo" {
		t.Errorf("history of foo's Config = %+v, want just the foo Override", got)
	}
	if got := h.History(t, models.Scope{}); len(got) != 2 {
		t.Errorf("unfiltered history has %d entries, want 2", len(got))
	}
	if got := h.History(t, models.Scope{Config: foo.FooConfigName, Environment: "prod"}); len(got) != 0 {
		t.Errorf("history of foo in prod = %+v, want none: the Override is Config-wide", got)
	}
}

func TestHistoryHasNoRouteToChangeIt(t *testing.T) {
	h := systemtest.New(t)
	registerFoo(t, h)
	h.CreateOverride(t, fooOverride("tuned"))

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodPost} {
		status, _ := h.Do(t, method, "/v1/history")
		if status != http.StatusMethodNotAllowed && status != http.StatusNotFound {
			t.Errorf("%s /v1/history: status %d, want it refused", method, status)
		}
	}
	if got := h.History(t, models.Scope{}); len(got) != 1 {
		t.Errorf("history has %d entries after attempts to change it, want 1", len(got))
	}
}
