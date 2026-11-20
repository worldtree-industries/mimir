package systemtest_test

import (
	"net/http"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/alltypes"
	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

// registerAllTypes registers the Config that declares one Item of each type.
func registerAllTypes(t *testing.T, h *systemtest.Harness) {
	t.Helper()

	app := h.NewDeployable(t, systemtest.Deployable{Name: "all-types-consumer", Environment: "dev"})
	alltypes.NewAllTypesConfig(t.Context(), app.ClientOptions()...)
	if err := app.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}
}

func allTypesOverride(item string, typ models.Type, value string) models.Override {
	return models.Override{
		Scope: models.Scope{Config: alltypes.AllTypesConfigName},
		Item:  models.ItemRef{Name: item, Type: typ},
		Value: value,
	}
}

func TestOverrideValueMustParseAsItsItemType(t *testing.T) {
	cases := []struct {
		item    string
		typ     models.Type
		bad     string
		goodVal string
	}{
		{"s", models.TypeString, "", "anything"}, // a string has no bad value; checked as valid below
		{"i", models.TypeInt, "forty-two", "42"},
		{"b", models.TypeBool, "maybe", "false"},
		{"f", models.TypeFloat, "1.2.3", "0.25"},
		{"s_arr", models.TypeStringArray, `["a", 2]`, `["a", "b"]`},
		{"i_arr", models.TypeIntArray, `[1, 2.5]`, `[1, 2]`},
		{"b_arr", models.TypeBoolArray, `[true, "no"]`, `[true, false]`},
		{"f_arr", models.TypeFloatArray, `[1.5, "x"]`, `[1.5, 2]`},
		{"s_set", models.TypeStringSet, `not json`, `["a"]`},
		{"i_set", models.TypeIntSet, `{"a": 1}`, `[1, 2]`},
		{"b_set", models.TypeBoolSet, `[1]`, `[true]`},
		{"f_set", models.TypeFloatSet, `1.5`, `[1.5]`},
	}

	h := systemtest.New(t)
	registerAllTypes(t, h)

	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			if tc.bad != "" {
				status, body := h.Post(t, "/v1/overrides", allTypesOverride(tc.item, tc.typ, tc.bad))
				if status != http.StatusUnprocessableEntity {
					t.Errorf("Override %q for a %s Item: status %d, want 422; body %s", tc.bad, tc.typ, status, body)
				}
			}

			h.CreateOverride(t, allTypesOverride(tc.item, tc.typ, tc.goodVal))
		})
	}
}

func TestOverrideForAnItemNobodyRegisteredIsRejected(t *testing.T) {
	h := systemtest.New(t)
	app := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	foo.NewFooConfig(t.Context(), app.ClientOptions()...)
	if err := app.AwaitRegistration(t); err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	unknownName := models.Override{
		Scope: models.Scope{Config: foo.FooConfigName},
		Item:  models.ItemRef{Name: "nobody-declared-this", Type: models.TypeString},
		Value: "x",
	}
	// "foo" is registered as a string; as an int it is a different Item.
	otherType := fooOverride("1")
	otherType.Item.Type = models.TypeInt
	unknownConfig := fooOverride("x")
	unknownConfig.Scope.Config = "example.com/never.Registered"

	for name, o := range map[string]models.Override{
		"unknown name":   unknownName,
		"other type":     otherType,
		"unknown Config": unknownConfig,
	} {
		status, body := h.Post(t, "/v1/overrides", o)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422; body %s", name, status, body)
		}
	}
}
