package noscalars_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/noscalars"
)

// A Config with no int, bool or float Item parses nothing with strconv, so
// its generated code must build without importing it.
func TestGettersReturnDefaults(t *testing.T) {
	cfg := noscalars.NewNoScalarsConfig(context.Background())

	if got, want := cfg.GetName(), "hi"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if got, want := cfg.GetTags(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
	if got := cfg.GetPorts().Len(); got != 2 {
		t.Errorf("ports has %d members, want 2", got)
	}
}
