package systemtest_test

import (
	"encoding/json"
	"net/http"
	"net/url"
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
	return models.Item{Name: name, Type: typ, Description: name + " item", Default: value}
}

// shopRegistration is a version of checkout, in dev, declaring items.
func shopRegistration(items ...models.Item) models.Registration {
	return models.Registration{Namespace: shopDevNamespace(), Items: items}
}

func shopOverride(item string, typ models.Type, value string) models.Override {
	return models.Override{
		Scope: models.Scope{Config: shopConfig},
		Item:  models.ItemRef{Name: item, Type: typ},
		Value: value,
	}
}

// itemRefs is the names and types of the Items the server knows of the shop
// Config.
func itemRefs(t *testing.T, h *systemtest.Harness) []models.ItemRef {
	t.Helper()

	var keys []models.ItemRef
	for _, item := range h.Items(t, shopConfig) {
		keys = append(keys, item.ItemRef)
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

	want := []models.ItemRef{
		{Name: "banner", Type: models.TypeString},
		{Name: "timeout", Type: models.TypeInt},
	}
	if got := itemRefs(t, h); !slices.Equal(got, want) {
		t.Errorf("Items after the dropping version registered = %v, want %v", got, want)
	}
	snap, _ := h.Snapshot(t, shopDevNamespace())
	if got, ok := snap.Value(models.ItemRef{Name: "banner", Type: models.TypeString}); !ok || got != "sale" {
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

	want := []models.ItemRef{
		{Name: "limit", Type: models.TypeInt},
		{Name: "limit", Type: models.TypeString},
	}
	if got := itemRefs(t, h); !slices.Equal(got, want) {
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

func TestDefaultShownIsTheMostRecentlyRegisteredOne(t *testing.T) {
	h := systemtest.New(t)
	oldVersion := shopRegistration(shopItem("timeout", models.TypeInt, "30"))
	newVersion := shopRegistration(shopItem("timeout", models.TypeInt, "45"))

	// During a rollout the versions register in turn; Mimir keeps one Default
	// per Item, the one registered last.
	for _, step := range []struct {
		reg  models.Registration
		want string
	}{
		{oldVersion, "30"},
		{newVersion, "45"},
		{oldVersion, "30"},
	} {
		h.Register(t, step.reg)

		items := h.Items(t, shopConfig)
		if len(items) != 1 || items[0].Default != step.want {
			t.Fatalf("catalogue Items = %+v, want timeout with Default %q", items, step.want)
		}
		ns := shopDevNamespace()
		status, body := h.Request(t, http.MethodGet, "/v1/explain?"+url.Values{
			"config": {ns.Config}, "deployable": {ns.Deployable}, "environment": {ns.Environment},
		}.Encode(), nil)
		var explained models.Explanation
		if err := json.Unmarshal(body, &explained); status != http.StatusOK || err != nil {
			t.Fatalf("explain: status %d, body %s, decode error %v", status, body, err)
		}
		if len(explained.Items) != 1 || explained.Items[0].Default != step.want {
			t.Fatalf("explained Items = %+v, want timeout with Default %q", explained.Items, step.want)
		}
	}
}
