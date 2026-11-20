package alltypes_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/alltypes"
)

func TestEveryItemTypeReturnsItsDefault(t *testing.T) {
	cfg := alltypes.NewAllTypesConfig(context.Background())

	checks := []struct {
		name      string
		got, want any
	}{
		{"s", cfg.GetS(), "hi"},
		{"i", cfg.GetI(), int64(-7)},
		{"b", cfg.GetB(), true},
		{"f", cfg.GetF(), 2.5},
		{"s_arr", cfg.GetSArr(), []string{"a", "b", "a"}},
		{"i_arr", cfg.GetIArr(), []int64{3, 1, 3}},
		{"b_arr", cfg.GetBArr(), []bool{true, false, true}},
		{"f_arr", cfg.GetFArr(), []float64{1.5, 2.5}},
		{"s_set", cfg.GetSSet().Len(), 2},
		{"i_set", cfg.GetISet().Len(), 2},
		{"b_set", cfg.GetBSet().Len(), 2},
		{"f_set", cfg.GetFSet().Len(), 1},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestConfigNameIsDerivedFromPackagePathAndType(t *testing.T) {
	want := "github.com/worldtree-industries/mimir/internal/generator/testdata/alltypes.AllTypesConfig"
	if alltypes.AllTypesConfigName != want {
		t.Errorf("name = %q, want %q", alltypes.AllTypesConfigName, want)
	}
}

func TestDisplayNameIsExposed(t *testing.T) {
	if alltypes.AllTypesConfigDisplayName != "Every type" {
		t.Errorf("display name = %q", alltypes.AllTypesConfigDisplayName)
	}
}
