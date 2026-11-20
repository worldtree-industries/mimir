package systemtest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/worldtree-industries/mimir/models"
)

// Configs lists the Configs the server knows through the admin API.
func (h *Harness) Configs(t *testing.T) []models.ConfigInfo {
	t.Helper()

	var configs []models.ConfigInfo
	h.list(t, "/v1/configs", &configs)

	return configs
}

// Items lists the Items the server knows through the admin API. An empty
// config lists every Config's Items.
func (h *Harness) Items(t *testing.T, config string) []models.RegisteredItem {
	t.Helper()

	var items []models.RegisteredItem
	h.list(t, "/v1/items"+configQuery(config), &items)

	return items
}

// Namespaces lists the Namespaces the server knows through the admin API. An
// empty config lists every Config's Namespaces.
func (h *Harness) Namespaces(t *testing.T, config string) []models.RegisteredNamespace {
	t.Helper()

	var namespaces []models.RegisteredNamespace
	h.list(t, "/v1/namespaces"+configQuery(config), &namespaces)

	return namespaces
}

// Register sends reg to the server as a client would, and fails the test
// unless it is accepted. It stands in for a Deployable of some version
// declaring exactly reg's Items.
func (h *Harness) Register(t *testing.T, reg models.Registration) {
	t.Helper()

	if status, body := h.Post(t, "/v1/registrations", reg); status != http.StatusNoContent {
		t.Fatalf("register: status %d, body %s", status, body)
	}
}

func configQuery(config string) string {
	if config == "" {
		return ""
	}

	return "?" + url.Values{"config": {config}}.Encode()
}

func (h *Harness) list(t *testing.T, path string, into any) {
	t.Helper()

	status, body := h.Get(t, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", path, status, body)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("decode response to GET %s %s: %v", path, body, err)
	}
}
