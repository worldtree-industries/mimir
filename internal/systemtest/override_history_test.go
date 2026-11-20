package systemtest_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// putWithCaller updates an Override through the admin API as caller.
func (f *fleet) putWithCaller(t *testing.T, o models.Override, value, caller string) (int, []byte) {
	t.Helper()

	return f.h.Send(t, http.MethodPut, overridePath(o), map[string]string{"value": value}, "X-Mimir-Caller", caller)
}

func TestUpdatingAnOverrideIsRecordedInHistory(t *testing.T) {
	f := newFleet(t)
	o := f.h.CreateOverride(t, scopedOverride(deployableA, envProd, "before"))
	f.h.Clock.Advance(time.Hour)

	if status, body := f.putWithCaller(t, o, "after", "alice"); status != http.StatusOK {
		t.Fatalf("update Override: status %d, body %s", status, body)
	}

	got := f.h.History(t, models.Scope{Config: foo.FooConfigName})
	if len(got) != 2 {
		t.Fatalf("history after a create and an update has %d entries, want 2: %+v", len(got), got)
	}
	e := got[1]
	if e.Action != models.HistoryUpdated {
		t.Errorf("Action = %q, want %q", e.Action, models.HistoryUpdated)
	}
	if e.Scope != o.Scope || e.Item != o.Item {
		t.Errorf("entry is for %+v %+v, want %+v %+v", e.Scope, e.Item, o.Scope, o.Item)
	}
	if e.OldValue == nil || *e.OldValue != "before" || e.NewValue == nil || *e.NewValue != "after" {
		t.Errorf("values = %v -> %v, want %q -> %q", e.OldValue, e.NewValue, "before", "after")
	}
	if want := systemtest.Epoch.Add(time.Hour); !e.Time.Equal(want) {
		t.Errorf("Time = %v, want the server clock's %v", e.Time, want)
	}
	if e.Caller != "alice" {
		t.Errorf("Caller = %q, want %q", e.Caller, "alice")
	}
}

func TestDeletingAnOverrideIsRecordedInHistory(t *testing.T) {
	f := newFleet(t)
	o := f.h.CreateOverride(t, scopedOverride("", envProd, "doomed"))
	f.h.Clock.Advance(time.Hour)

	status, body := f.h.Send(t, http.MethodDelete, overridePath(o), nil, "X-Mimir-Caller", "bob")
	if status != http.StatusNoContent {
		t.Fatalf("delete Override: status %d, body %s", status, body)
	}

	got := f.h.History(t, models.Scope{Config: foo.FooConfigName})
	if len(got) != 2 {
		t.Fatalf("history after a create and a delete has %d entries, want 2: %+v", len(got), got)
	}
	e := got[1]
	if e.Action != models.HistoryDeleted {
		t.Errorf("Action = %q, want %q", e.Action, models.HistoryDeleted)
	}
	if e.Scope != o.Scope || e.Item != o.Item {
		t.Errorf("entry is for %+v %+v, want %+v %+v", e.Scope, e.Item, o.Scope, o.Item)
	}
	if e.OldValue == nil || *e.OldValue != "doomed" || e.NewValue != nil {
		t.Errorf("values = %v -> %v, want %q -> none", e.OldValue, e.NewValue, "doomed")
	}
	if want := systemtest.Epoch.Add(time.Hour); !e.Time.Equal(want) {
		t.Errorf("Time = %v, want the server clock's %v", e.Time, want)
	}
	if e.Caller != "bob" {
		t.Errorf("Caller = %q, want %q", e.Caller, "bob")
	}
}

func TestUpdateValueMustParseAsTheItemsType(t *testing.T) {
	f := newFleet(t)
	bar := f.h.CreateOverride(t, models.Override{
		Scope: models.Scope{Config: foo.FooConfigName, Deployable: deployableA},
		Item:  models.ItemKey{Name: "bar", Type: models.TypeInt},
		Value: "7",
	})

	status, body := f.putWithCaller(t, bar, "forty-two", "alice")
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("update bar to a non-integer: status %d, want 422; body %s", status, body)
	}

	listed := f.listOverrides(t, url.Values{"config": {foo.FooConfigName}})
	if len(listed) != 1 || listed[0].Value != "7" {
		t.Errorf("Overrides after the rejected update = %+v, want bar still 7", listed)
	}
	if got := f.h.History(t, models.Scope{Config: foo.FooConfigName}); len(got) != 1 {
		t.Errorf("history has %d entries after a rejected update, want only the create: %+v", len(got), got)
	}
}

func TestUpdatingAnUnknownOverrideLeavesNoHistory(t *testing.T) {
	f := newFleet(t)

	status, _ := f.h.Request(t, http.MethodPut, "/v1/overrides/9999", map[string]string{"value": "x"})
	if status != http.StatusNotFound {
		t.Fatalf("status %d, want 404", status)
	}
	if got := f.h.History(t, models.Scope{}); len(got) != 0 {
		t.Errorf("history = %+v, want none", got)
	}
}
