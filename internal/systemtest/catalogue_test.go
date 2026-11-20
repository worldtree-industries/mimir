package systemtest_test

import (
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/foo"
	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

func TestAdminAPIListsConfigsItemsAndNamespaces(t *testing.T) {
	h := systemtest.New(t)
	dev := h.NewDeployable(t, systemtest.Deployable{Name: "foo-consumer", Environment: "dev"})
	prod := h.NewDeployable(t, systemtest.Deployable{Name: "bar-consumer", Environment: "prod"})
	for _, d := range []*systemtest.RunningDeployable{dev, prod} {
		foo.NewFooConfig(t.Context(), d.ClientOptions()...)
		if err := d.AwaitRegistration(t); err != nil {
			t.Fatalf("Registration of %s failed: %v", d.Name, err)
		}
	}

	configs := h.Configs(t)
	if len(configs) != 1 || configs[0].Name != foo.FooConfigName {
		t.Fatalf("Configs = %+v, want only %s", configs, foo.FooConfigName)
	}
	if !configs[0].LastRegisteredAt.Equal(systemtest.Epoch) {
		t.Errorf("Config last registered at %v, want %v", configs[0].LastRegisteredAt, systemtest.Epoch)
	}

	items := h.Items(t, foo.FooConfigName)
	if len(items) != 8 {
		t.Fatalf("Items = %d, want the 8 foo declares: %+v", len(items), items)
	}
	var got models.RegisteredItem
	for _, item := range items {
		if item.Name == "bar" {
			got = item
		}
	}
	want := models.RegisteredItem{
		Config:            foo.FooConfigName,
		Description:       "An int",
		Default:           "55555",
		FirstRegisteredAt: systemtest.Epoch,
		LastRegisteredAt:  systemtest.Epoch,
	}
	want.Name, want.Type = "bar", models.TypeInt
	if got != want {
		t.Errorf("Item bar = %+v, want %+v", got, want)
	}

	namespaces := h.Namespaces(t, foo.FooConfigName)
	if len(namespaces) != 2 {
		t.Fatalf("Namespaces = %+v, want foo-consumer in dev and bar-consumer in prod", namespaces)
	}
	for _, ns := range namespaces {
		if !ns.LastRegisteredAt.Equal(systemtest.Epoch) {
			t.Errorf("Namespace %+v last registered at %v, want %v",
				ns.Namespace, ns.LastRegisteredAt, systemtest.Epoch)
		}
	}

	if other := h.Items(t, "example.com/other.Config"); len(other) != 0 {
		t.Errorf("Items of another Config = %+v, want none", other)
	}
}
