package systemtest_test

import (
	"slices"
	"testing"

	"github.com/worldtree-industries/mimir/internal/systemtest"
	"github.com/worldtree-industries/mimir/models"
)

const shopConfig = "example.com/shop.CheckoutConfig"

func shopDevNamespace() models.Namespace {
	return models.Namespace{Config: shopConfig, Deployable: "checkout", Environment: "dev"}
}

func shopItem(name string, typ models.Type, value string) models.Item {
	return models.Item{Name: name, Type: typ, Description: name + " item", Value: value}
}

// shopRegistration is a version of checkout, in dev, declaring items.
func shopRegistration(items ...models.Item) models.Registration {
	return models.Registration{Namespace: shopDevNamespace(), Items: items}
}

func shopOverride(item string, typ models.Type, value string) models.Override {
	return models.Override{
		Scope: models.Scope{Config: shopConfig},
		Item:  models.ItemKey{Name: item, Type: typ},
		Value: value,
	}
}

// itemKeys is the names and types of the Items the server knows of the shop
// Config.
func itemKeys(t *testing.T, h *systemtest.Harness) []models.ItemKey {
	t.Helper()

	var keys []models.ItemKey
	for _, item := range h.Items(t, shopConfig) {
		keys = append(keys, item.ItemKey)
	}

	return keys
}

func TestRegisteringAVersionThatDropsAnItemLeavesItAndItsOverrides(t *testing.T) {
	h := systemtest.New(t)
	h.Register(t, shopRegistration(
		shopItem("timeout", models.TypeInt, "30"),
		shopItem("banner", models.TypeString, "hi"),
	))
	h.CreateOverride(t, shopOverride("banner", models.TypeString, "sale"))

	// The next version no longer declares banner.
	h.Register(t, shopRegistration(shopItem("timeout", models.TypeInt, "30")))

	want := []models.ItemKey{
		{Name: "banner", Type: models.TypeString},
		{Name: "timeout", Type: models.TypeInt},
	}
	if got := itemKeys(t, h); !slices.Equal(got, want) {
		t.Errorf("Items after the dropping version registered = %v, want %v", got, want)
	}
	snap, _ := h.Snapshot(t, shopDevNamespace())
	if got, ok := snap.Value(models.ItemKey{Name: "banner", Type: models.TypeString}); !ok || got != "sale" {
		t.Errorf("Snapshot value of banner = %q (present %t), want the Override %q kept", got, ok, "sale")
	}
	if entries := h.History(t, models.Scope{Config: shopConfig}); len(entries) != 1 {
		t.Errorf("history = %+v, want only the Override's creation", entries)
	}
}

func TestAnItemWhoseTypeChangedRegistersAsANewItem(t *testing.T) {
	h := systemtest.New(t)
	h.Register(t, shopRegistration(shopItem("limit", models.TypeInt, "5")))
	h.CreateOverride(t, shopOverride("limit", models.TypeInt, "9"))

	h.Register(t, shopRegistration(shopItem("limit", models.TypeString, "five")))

	want := []models.ItemKey{
		{Name: "limit", Type: models.TypeInt},
		{Name: "limit", Type: models.TypeString},
	}
	if got := itemKeys(t, h); !slices.Equal(got, want) {
		t.Errorf("Items after the type changed = %v, want both %v", got, want)
	}

	// The string Item is its own Item, so it takes its own Override beside
	// the int Item's.
	if status, _ := h.Post(t, "/v1/overrides", shopOverride("limit", models.TypeString, "ten")); status != 201 {
		t.Errorf("Override of the new string Item: status %d, want 201", status)
	}
	snap, _ := h.Snapshot(t, shopDevNamespace())
	if len(snap.Values) != 2 {
		t.Errorf("Snapshot = %+v, want one value for each of the two Items", snap.Values)
	}
}
