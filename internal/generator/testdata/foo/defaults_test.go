package foo_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
)

func TestGettersReturnDefaults(t *testing.T) {
	cfg := foo.NewFooConfig(context.Background())

	checks := []struct {
		name      string
		got, want any
	}{
		{"foo", cfg.GetFoo(), "default"},
		{"bar", cfg.GetBar(), int64(55555)},
		{"baz", cfg.GetBaz(), true},
		{"qux", cfg.GetQux(), 3.14159},
		{"quux", cfg.GetQuux(), []string{"default", "values"}},
		{"quuz", cfg.GetQuuz(), []int64{1, 1, 2, 3, 5, 8}},
		{"corge", cfg.GetCorge().Len(), 3},
		{"grault", cfg.GetGrault().Len(), 3},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}
