package systemtest_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// The four Namespaces of the fleet most precedence tests run against: two
// Deployables, each in dev and prod.
const (
	deployableA = "service-a"
	deployableB = "service-b"
	envDev      = "dev"
	envProd     = "prod"
)

// fleet is every Deployable/Environment combination running foo's Config.
type fleet struct {
	h    *systemtest.Harness
	cfgs map[models.Namespace]*foo.FooConfig
}

func newFleet(t *testing.T) *fleet {
	t.Helper()

	f := &fleet{h: systemtest.New(t), cfgs: map[models.Namespace]*foo.FooConfig{}}
	for _, d := range []string{deployableA, deployableB} {
		for _, env := range []string{envDev, envProd} {
			running := f.h.NewDeployable(t, systemtest.Deployable{Name: d, Environment: env})
			f.cfgs[fleetNamespace(d, env)] = foo.NewFooConfig(t.Context(), running.ClientOptions()...)
			if err := running.AwaitRegistration(t); err != nil {
				t.Fatalf("Registration of %s in %s failed: %v", d, env, err)
			}
		}
	}

	return f
}

func fleetNamespace(deployable, environment string) models.Namespace {
	return models.Namespace{Config: foo.FooConfigName, Deployable: deployable, Environment: environment}
}

// scopedOverride is an Override of foo's string Item "foo" in the Scope that
// narrows foo's Config to deployable and environment ("" meaning any).
func scopedOverride(deployable, environment, value string) models.Override {
	o := fooOverride(value)
	o.Scope.Deployable = deployable
	o.Scope.Environment = environment

	return o
}

// foos is what every Namespace in the fleet reads foo as, keyed by
// "<deployable>/<environment>".
func (f *fleet) foos() map[string]string {
	got := map[string]string{}
	for ns, cfg := range f.cfgs {
		got[ns.Deployable+"/"+ns.Environment] = cfg.GetFoo()
	}

	return got
}

func assertFoos(t *testing.T, got, want map[string]string) {
	t.Helper()

	for ns, w := range want {
		if got[ns] != w {
			t.Errorf("%s reads foo as %q, want %q (all: %v)", ns, got[ns], w, got)
		}
	}
}

func TestOverrideAppliesToEveryNamespaceItsScopeCovers(t *testing.T) {
	const def, set = "default", "tuned"

	tests := []struct {
		name        string
		deployable  string
		environment string
		want        map[string]string
	}{
		{
			name: "Config-wide covers every Namespace",
			want: map[string]string{
				"service-a/dev":  set,
				"service-a/prod": set,
				"service-b/dev":  set,
				"service-b/prod": set,
			},
		},
		{
			name:       "one Deployable covers it in every Environment",
			deployable: deployableA,
			want: map[string]string{
				"service-a/dev":  set,
				"service-a/prod": set,
				"service-b/dev":  def,
				"service-b/prod": def,
			},
		},
		{
			name:        "one Environment covers every Deployable in it",
			environment: envProd,
			want: map[string]string{
				"service-a/dev":  def,
				"service-a/prod": set,
				"service-b/dev":  def,
				"service-b/prod": set,
			},
		},
		{
			name:        "one Deployable in one Environment covers just that Namespace",
			deployable:  deployableA,
			environment: envProd,
			want: map[string]string{
				"service-a/dev":  def,
				"service-a/prod": set,
				"service-b/dev":  def,
				"service-b/prod": def,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFleet(t)

			f.h.CreateOverride(t, scopedOverride(tt.deployable, tt.environment, set))
			f.h.NextPoll(t)

			assertFoos(t, f.foos(), tt.want)
		})
	}
}

// scopeShape is one of the four ways an Override's Scope can be narrowed,
// aimed at service-a in prod so that every shape covers that Namespace.
type scopeShape struct {
	name        string
	deployable  string
	environment string
}

//nolint:gochecknoglobals // fixed, read-only test data
var (
	shapeConfig                = scopeShape{name: "Config-wide"}
	shapeEnvironment           = scopeShape{name: "Environment", environment: envProd}
	shapeDeployable            = scopeShape{name: "Deployable", deployable: deployableA}
	shapeDeployableEnvironment = scopeShape{
		name:        "Deployable and Environment",
		deployable:  deployableA,
		environment: envProd,
	}
)

func TestMostSpecificScopeWinsForEveryPairing(t *testing.T) {
	// Each pair is (winner, loser) in ADR 0003's order: Deployable and
	// Environment, then Deployable, then Environment, then Config-wide.
	pairs := []struct{ winner, loser scopeShape }{
		{shapeDeployableEnvironment, shapeDeployable},
		{shapeDeployableEnvironment, shapeEnvironment},
		{shapeDeployableEnvironment, shapeConfig},
		// "service-a in any Environment" beats "any Deployable in prod":
		// whoever tuned service-a meant service-a.
		{shapeDeployable, shapeEnvironment},
		{shapeDeployable, shapeConfig},
		{shapeEnvironment, shapeConfig},
	}

	for _, p := range pairs {
		for _, loserFirst := range []bool{true, false} {
			order := "loser created first"
			if !loserFirst {
				order = "winner created first"
			}

			t.Run(p.winner.name+" beats "+p.loser.name+", "+order, func(t *testing.T) {
				f := newFleet(t)
				win := scopedOverride(p.winner.deployable, p.winner.environment, "winner")
				lose := scopedOverride(p.loser.deployable, p.loser.environment, "loser")

				first, second := lose, win
				if !loserFirst {
					first, second = win, lose
				}
				f.h.CreateOverride(t, first)
				f.h.CreateOverride(t, second)
				f.h.NextPoll(t)

				assertFoos(t, f.foos(), map[string]string{"service-a/prod": "winner"})
			})
		}
	}
}

func TestEachNamespaceReadsItsMostSpecificOverride(t *testing.T) {
	f := newFleet(t)

	f.h.CreateOverride(t, scopedOverride("", "", "config"))
	f.h.CreateOverride(t, scopedOverride("", envProd, "environment"))
	f.h.CreateOverride(t, scopedOverride(deployableA, "", "deployable"))
	f.h.CreateOverride(t, scopedOverride(deployableA, envProd, "deployable-and-environment"))
	f.h.NextPoll(t)

	assertFoos(t, f.foos(), map[string]string{
		"service-a/prod": "deployable-and-environment",
		"service-a/dev":  "deployable",
		"service-b/prod": "environment",
		"service-b/dev":  "config",
	})
}

func TestPrecedenceIsPerItem(t *testing.T) {
	f := newFleet(t)

	// foo is overridden Config-wide and bar only in prod: neither hides the
	// other, though prod's Scope is narrower.
	f.h.CreateOverride(t, scopedOverride("", "", "tuned"))
	f.h.CreateOverride(t, models.Override{
		Scope: models.Scope{Config: foo.FooConfigName, Environment: envProd},
		Item:  models.ItemKey{Name: "bar", Type: models.TypeInt},
		Value: "7",
	})
	f.h.NextPoll(t)

	cfg := f.cfgs[fleetNamespace(deployableA, envProd)]
	if got := cfg.GetFoo(); got != "tuned" {
		t.Errorf("foo = %q, want the Config-wide Override %q", got, "tuned")
	}
	if got := cfg.GetBar(); got != 7 {
		t.Errorf("bar = %d, want the Environment Override 7", got)
	}
	if got := f.cfgs[fleetNamespace(deployableA, envDev)].GetBar(); got != 55555 {
		t.Errorf("bar in dev = %d, want its Default 55555", got)
	}
}

// overridePath is the admin API path of one Override.
func overridePath(o models.Override) string {
	return "/v1/overrides/" + strconv.FormatInt(o.ID, 10)
}

func TestUpdatingAnOverrideReachesCoveredNamespaces(t *testing.T) {
	f := newFleet(t)
	o := f.h.CreateOverride(t, scopedOverride(deployableA, "", "before"))
	f.h.NextPoll(t)
	assertFoos(
		t,
		f.foos(),
		map[string]string{"service-a/dev": "before", "service-a/prod": "before", "service-b/dev": "default"},
	)

	status, body := f.h.Request(t, http.MethodPut, overridePath(o), map[string]string{"value": "after"})
	if status != http.StatusOK {
		t.Fatalf("update Override: status %d, body %s", status, body)
	}
	var updated models.Override
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode updated Override %s: %v", body, err)
	}
	if updated.ID != o.ID || updated.Value != "after" || updated.Scope != o.Scope || updated.Item != o.Item {
		t.Errorf("updated Override = %+v, want %+v with value %q", updated, o, "after")
	}
	f.h.NextPoll(t)

	assertFoos(
		t,
		f.foos(),
		map[string]string{"service-a/dev": "after", "service-a/prod": "after", "service-b/dev": "default"},
	)
}

func TestUpdatingAnUnknownOverrideIsNotFound(t *testing.T) {
	f := newFleet(t)

	status, body := f.h.Request(t, http.MethodPut, "/v1/overrides/9999", map[string]string{"value": "x"})
	if status != http.StatusNotFound {
		t.Errorf("update unknown Override: status %d, want 404; body %s", status, body)
	}
}

func TestDeletingTheWinningOverrideLetsTheNextMostSpecificTakeEffect(t *testing.T) {
	f := newFleet(t)
	f.h.CreateOverride(t, scopedOverride("", "", "config"))
	f.h.CreateOverride(t, scopedOverride("", envProd, "environment"))
	winning := f.h.CreateOverride(t, scopedOverride(deployableA, envProd, "deployable-and-environment"))
	f.h.NextPoll(t)
	assertFoos(t, f.foos(), map[string]string{"service-a/prod": "deployable-and-environment"})

	if status, body := f.h.Request(t, http.MethodDelete, overridePath(winning), nil); status != http.StatusNoContent {
		t.Fatalf("delete Override: status %d, body %s", status, body)
	}
	f.h.NextPoll(t)

	assertFoos(t, f.foos(), map[string]string{
		"service-a/prod": "environment",
		"service-a/dev":  "config",
		"service-b/prod": "environment",
		"service-b/dev":  "config",
	})
}

func TestDeletingTheLastOverrideRestoresTheDefault(t *testing.T) {
	f := newFleet(t)
	o := f.h.CreateOverride(t, scopedOverride(deployableA, "", "tuned"))
	f.h.NextPoll(t)
	assertFoos(t, f.foos(), map[string]string{"service-a/dev": "tuned"})

	if status, body := f.h.Request(t, http.MethodDelete, overridePath(o), nil); status != http.StatusNoContent {
		t.Fatalf("delete Override: status %d, body %s", status, body)
	}
	f.h.NextPoll(t)

	assertFoos(t, f.foos(), map[string]string{
		"service-a/dev":  "default",
		"service-a/prod": "default",
		"service-b/dev":  "default",
		"service-b/prod": "default",
	})
}

func TestDeletingAnUnknownOverrideIsNotFound(t *testing.T) {
	f := newFleet(t)

	if status, body := f.h.Request(t, http.MethodDelete, "/v1/overrides/9999", nil); status != http.StatusNotFound {
		t.Errorf("delete unknown Override: status %d, want 404; body %s", status, body)
	}
}

// publishedAfter clears every Snapshot in the fleet, runs change, and returns
// the "<deployable>/<environment>" of each Namespace whose Snapshot exists
// afterwards: exactly those the change republished.
func (f *fleet) publishedAfter(t *testing.T, change func()) []string {
	t.Helper()

	for ns := range f.cfgs {
		f.h.DeleteSnapshot(t, ns)
	}
	change()

	var published []string
	for ns := range f.cfgs {
		if _, ok := f.h.Snapshot(t, ns); ok {
			published = append(published, ns.Deployable+"/"+ns.Environment)
		}
	}
	slices.Sort(published)

	return published
}

func TestEachChangeRepublishesExactlyTheNamespacesItsScopeCovers(t *testing.T) {
	all := []string{"service-a/dev", "service-a/prod", "service-b/dev", "service-b/prod"}
	shapes := []struct {
		scopeShape

		covered []string
	}{
		{shapeConfig, all},
		{shapeDeployable, []string{"service-a/dev", "service-a/prod"}},
		{shapeEnvironment, []string{"service-a/prod", "service-b/prod"}},
		{shapeDeployableEnvironment, []string{"service-a/prod"}},
	}

	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			f := newFleet(t)
			var o models.Override

			created := f.publishedAfter(t, func() {
				o = f.h.CreateOverride(t, scopedOverride(s.deployable, s.environment, "one"))
			})
			if !slices.Equal(created, s.covered) {
				t.Errorf("creating republished %v, want %v", created, s.covered)
			}

			updated := f.publishedAfter(t, func() {
				if status, body := f.h.Request(
					t,
					http.MethodPut,
					overridePath(o),
					map[string]string{"value": "two"},
				); status != http.StatusOK {
					t.Fatalf("update: status %d, body %s", status, body)
				}
			})
			if !slices.Equal(updated, s.covered) {
				t.Errorf("updating republished %v, want %v", updated, s.covered)
			}

			deleted := f.publishedAfter(t, func() {
				if status, body := f.h.Request(
					t,
					http.MethodDelete,
					overridePath(o),
					nil,
				); status != http.StatusNoContent {
					t.Fatalf("delete: status %d, body %s", status, body)
				}
			})
			if !slices.Equal(deleted, s.covered) {
				t.Errorf("deleting republished %v, want %v", deleted, s.covered)
			}
		})
	}
}

func TestChangeToOneConfigLeavesAnotherConfigsSnapshotsAlone(t *testing.T) {
	f := newFleet(t)
	other := models.Namespace{Config: "some/other.Config", Deployable: deployableA, Environment: envDev}
	status, body := f.h.Post(t, "/v1/registrations", models.Registration{
		Namespace: other,
		Items:     []models.Item{{Name: "foo", Type: models.TypeString, Description: "x", Value: "d"}},
	})
	if status != http.StatusNoContent {
		t.Fatalf("register other Config: status %d, body %s", status, body)
	}
	f.h.DeleteSnapshot(t, other)

	f.h.CreateOverride(t, scopedOverride("", "", "tuned"))

	if _, ok := f.h.Snapshot(t, other); ok {
		t.Error("an Override of one Config republished another Config's Snapshot")
	}
}

// listOverrides lists Overrides through the admin API with the given query.
func (f *fleet) listOverrides(t *testing.T, query url.Values) []models.Override {
	t.Helper()

	status, body := f.h.Request(t, http.MethodGet, "/v1/overrides?"+query.Encode(), nil)
	if status != http.StatusOK {
		t.Fatalf("list Overrides %v: status %d, body %s", query, status, body)
	}
	var listed struct {
		Overrides []models.Override `json:"overrides"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode Overrides %s: %v", body, err)
	}

	return listed.Overrides
}

func values(overrides []models.Override) []string {
	var got []string
	for _, o := range overrides {
		got = append(got, o.Value)
	}

	return got
}

func TestOverridesCanBeListedByConfigAndScope(t *testing.T) {
	f := newFleet(t)
	f.h.CreateOverride(t, scopedOverride("", "", "config"))
	f.h.CreateOverride(t, scopedOverride("", envProd, "environment"))
	f.h.CreateOverride(t, scopedOverride(deployableA, "", "deployable"))
	f.h.CreateOverride(t, scopedOverride(deployableA, envProd, "deployable-and-environment"))
	f.h.CreateOverride(t, scopedOverride(deployableB, "", "other deployable"))
	// An Override of another Config never shows up.
	status, body := f.h.Post(t, "/v1/registrations", models.Registration{
		Namespace: models.Namespace{Config: "some/other.Config", Deployable: deployableA, Environment: envDev},
		Items:     []models.Item{{Name: "foo", Type: models.TypeString, Description: "x", Value: "d"}},
	})
	if status != http.StatusNoContent {
		t.Fatalf("register other Config: status %d, body %s", status, body)
	}
	f.h.CreateOverride(t, models.Override{
		Scope: models.Scope{Config: "some/other.Config"},
		Item:  models.ItemKey{Name: "foo", Type: models.TypeString},
		Value: "elsewhere",
	})

	tests := []struct {
		name  string
		query url.Values
		want  []string
	}{
		{
			name:  "by Config alone lists every Scope",
			query: url.Values{"config": {foo.FooConfigName}},
			want:  []string{"config", "environment", "deployable", "deployable-and-environment", "other deployable"},
		},
		{
			name:  "by Deployable lists it in every Environment",
			query: url.Values{"config": {foo.FooConfigName}, "deployable": {deployableA}},
			want:  []string{"deployable", "deployable-and-environment"},
		},
		{
			name:  "by Environment lists it with every Deployable",
			query: url.Values{"config": {foo.FooConfigName}, "environment": {envProd}},
			want:  []string{"environment", "deployable-and-environment"},
		},
		{
			name:  "by Deployable and Environment lists exactly that Scope",
			query: url.Values{"config": {foo.FooConfigName}, "deployable": {deployableA}, "environment": {envProd}},
			want:  []string{"deployable-and-environment"},
		},
		{
			name:  "an empty Deployable and Environment lists the Config-wide Scope",
			query: url.Values{"config": {foo.FooConfigName}, "deployable": {""}, "environment": {""}},
			want:  []string{"config"},
		},
		{
			name:  "another Config lists only its own",
			query: url.Values{"config": {"some/other.Config"}},
			want:  []string{"elsewhere"},
		},
		{
			name:  "a Scope with no Overrides lists nothing",
			query: url.Values{"config": {foo.FooConfigName}, "deployable": {"nobody"}},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := values(f.listOverrides(t, tt.query)); !slices.Equal(got, tt.want) {
				t.Errorf("listed %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListingOverridesNeedsAConfig(t *testing.T) {
	f := newFleet(t)

	if status, body := f.h.Request(t, http.MethodGet, "/v1/overrides", nil); status != http.StatusUnprocessableEntity {
		t.Errorf("list without a Config: status %d, want 422; body %s", status, body)
	}
}

func (f *fleet) explain(t *testing.T, ns models.Namespace) (models.Explanation, int) {
	t.Helper()

	query := url.Values{"config": {ns.Config}, "deployable": {ns.Deployable}, "environment": {ns.Environment}}
	status, body := f.h.Request(t, http.MethodGet, "/v1/explain?"+query.Encode(), nil)
	var explained models.Explanation
	if status == http.StatusOK {
		if err := json.Unmarshal(body, &explained); err != nil {
			t.Fatalf("decode explanation %s: %v", body, err)
		}
	}

	return explained, status
}

// source finds the explained Item named name.
func source(t *testing.T, e models.Explanation, name string) models.ExplainedItem {
	t.Helper()

	for _, item := range e.Items {
		if item.Item.Name == name {
			return item
		}
	}
	t.Fatalf("explanation has no Item %q: %+v", name, e)

	return models.ExplainedItem{}
}

func TestExplainSaysWhereEachItemsValueComesFrom(t *testing.T) {
	f := newFleet(t)
	f.h.CreateOverride(t, scopedOverride("", "", "config"))
	deployable := f.h.CreateOverride(t, scopedOverride(deployableA, "", "deployable"))
	f.h.CreateOverride(t, models.Override{
		Scope: models.Scope{Config: foo.FooConfigName, Environment: envProd},
		Item:  models.ItemKey{Name: "bar", Type: models.TypeInt},
		Value: "7",
	})

	t.Run("an Override that won over a less specific one", func(t *testing.T) {
		e, status := f.explain(t, fleetNamespace(deployableA, envDev))
		if status != http.StatusOK {
			t.Fatalf("explain: status %d", status)
		}
		if e.Namespace != fleetNamespace(deployableA, envDev) {
			t.Errorf("explained Namespace = %+v", e.Namespace)
		}
		fooItem := source(t, e, "foo")
		if fooItem.Value != "deployable" || fooItem.Source.Kind != models.SourceOverride ||
			fooItem.Source.Override == nil || *fooItem.Source.Override != deployable {
			t.Errorf("foo = %+v, want value %q from %+v", fooItem, "deployable", deployable)
		}
	})

	t.Run("an Item with no applicable Override has its Default", func(t *testing.T) {
		e, _ := f.explain(t, fleetNamespace(deployableA, envDev))
		bar := source(t, e, "bar")
		if bar.Value != "55555" || bar.Default != "55555" || bar.Source.Kind != models.SourceDefault ||
			bar.Source.Override != nil {
			t.Errorf("bar in dev = %+v, want its Default 55555 from the Default", bar)
		}
	})

	t.Run("an Override that covers only some Namespaces", func(t *testing.T) {
		e, _ := f.explain(t, fleetNamespace(deployableB, envProd))
		bar := source(t, e, "bar")
		if bar.Value != "7" || bar.Default != "55555" || bar.Source.Kind != models.SourceOverride {
			t.Errorf("bar in prod = %+v, want 7 from an Override, with Default 55555", bar)
		}
		if got := source(t, e, "foo"); got.Value != "config" {
			t.Errorf("foo in service-b/prod = %q, want the Config-wide %q", got.Value, "config")
		}
	})

	t.Run("it lists every Item of the Config", func(t *testing.T) {
		e, _ := f.explain(t, fleetNamespace(deployableA, envDev))
		var names []string
		for _, item := range e.Items {
			names = append(names, item.Item.Name)
		}
		want := []string{"bar", "baz", "corge", "foo", "grault", "quux", "quuz", "qux"}
		if !slices.Equal(names, want) {
			t.Errorf("explained Items %v, want %v", names, want)
		}
	})
}

func TestExplainAgreesWithThePublishedSnapshot(t *testing.T) {
	f := newFleet(t)
	f.h.CreateOverride(t, scopedOverride("", envProd, "environment"))
	f.h.CreateOverride(t, scopedOverride(deployableA, "", "deployable"))
	f.h.NextPoll(t)

	for ns, cfg := range f.cfgs {
		e, _ := f.explain(t, ns)
		if got := source(t, e, "foo").Value; got != cfg.GetFoo() {
			t.Errorf("%+v: explain says foo is %q, the running Config reads %q", ns, got, cfg.GetFoo())
		}
	}
}

func TestExplainOfAnUnknownNamespaceIsNotFound(t *testing.T) {
	f := newFleet(t)

	if _, status := f.explain(t, fleetNamespace("nobody", envDev)); status != http.StatusNotFound {
		t.Errorf("explain unknown Namespace: status %d, want 404", status)
	}
}

func TestExplainNeedsAWholeNamespace(t *testing.T) {
	f := newFleet(t)

	status, body := f.h.Request(
		t,
		http.MethodGet,
		"/v1/explain?"+url.Values{"config": {foo.FooConfigName}}.Encode(),
		nil,
	)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("explain without a Deployable and Environment: status %d, want 422; body %s", status, body)
	}
}
