package systemtest_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dfryer1193/mjolnir/utils/environment"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
)

func TestClientWithoutAValidEnvironmentRunsOnDefaultsOnly(t *testing.T) {
	cases := map[string]string{
		"missing": "",
		"unknown": "qa",
		"case":    "Dev", // mjolnir's match is exact
	}

	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			h := systemtest.New(t)
			d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: env})

			cfg := foo.NewFooConfig(t.Context(), d.ClientOptions()...)

			if !strings.Contains(d.Logs(), "MIMIR_ENVIRONMENT") {
				t.Errorf("the bad Environment wasn't logged; logs:\n%s", d.Logs())
			}
			if got := cfg.GetFoo(); got != "default" {
				t.Errorf("GetFoo() = %q, want the Default", got)
			}
			// Start launches no background work for a client like this, so
			// the absence of a Registration is already settled.
			status, _ := h.Get(t, "/v1/explain?config="+foo.FooConfigName+"&deployable=foo-consumer&environment="+env)
			if status == http.StatusOK {
				t.Error("a client without a valid Environment registered")
			}
		})
	}
}

func TestClientWithAValidEnvironmentRegisters(t *testing.T) {
	for _, env := range environment.List() {
		t.Run(env.String(), func(t *testing.T) {
			h := systemtest.New(t)
			d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: env.String()})

			foo.NewFooConfig(t.Context(), d.ClientOptions()...)

			if err := d.AwaitRegistration(t); err != nil {
				t.Fatalf("Registration failed: %v", err)
			}
		})
	}
}

func environmentNames(t *testing.T, h *systemtest.Harness) []string {
	t.Helper()

	status, body := h.Get(t, "/v1/environments")
	if status != http.StatusOK {
		t.Fatalf("GET /v1/environments = %d: %s", status, body)
	}
	var resp struct {
		Environments []string `json:"environments"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode environments: %v", err)
	}
	slices.Sort(resp.Environments)

	return resp.Environments
}

func TestServerCreatesEveryEnvironmentFromMjolnirAtStartup(t *testing.T) {
	h := systemtest.New(t)

	var want []string
	for _, e := range environment.List() {
		want = append(want, e.String())
	}
	slices.Sort(want)

	if got := environmentNames(t, h); !slices.Equal(got, want) {
		t.Errorf("Environments = %v, want %v", got, want)
	}

	// A second replica starting against the same database changes nothing.
	h.NewReplica(t)
	if got := environmentNames(t, h); !slices.Equal(got, want) {
		t.Errorf("Environments after another replica started = %v, want %v", got, want)
	}
}

func TestOverrideScopedToAnUnknownEnvironmentIsRejected(t *testing.T) {
	h := systemtest.New(t)
	d := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), d.ClientOptions()...)
	if err := d.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	o := fooOverride("tuned")
	o.Scope.Environment = "qa"
	if status, body := h.Post(t, "/v1/overrides", o); status != http.StatusUnprocessableEntity {
		t.Errorf("Override scoped to unknown Environment: status %d, want 422; body %s", status, body)
	}

	o.Scope.Environment = "dev"
	if status, body := h.Post(t, "/v1/overrides", o); status != http.StatusCreated {
		t.Errorf("Override scoped to a known Environment: status %d, want 201; body %s", status, body)
	}
}
