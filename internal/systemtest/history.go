package systemtest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/worldtree-industries/mimir/models"
)

// Get sends a GET to the admin API path and returns the response's status and
// body. Header pairs (name, value, ...) are sent with the request.
func (h *Harness) Get(t *testing.T, path string, header ...string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}

	return h.send(t, req)
}

// History lists the history entries matching scope through the admin API,
// oldest first. An empty field of scope doesn't filter on it.
func (h *Harness) History(t *testing.T, scope models.Scope) []models.HistoryEntry {
	t.Helper()

	query := url.Values{}
	for name, value := range map[string]string{
		"config":      scope.Config,
		"deployable":  scope.Deployable,
		"environment": scope.Environment,
	} {
		if value != "" {
			query.Set(name, value)
		}
	}

	status, body := h.Get(t, "/v1/history?"+query.Encode())
	if status != http.StatusOK {
		t.Fatalf("list history: status %d, body %s", status, body)
	}

	var entries []models.HistoryEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("decode history %s: %v", body, err)
	}

	return entries
}

// PostWithHeader is Post with one request header.
func (h *Harness) PostWithHeader(t *testing.T, path string, payload any, name, value string) (int, []byte) {
	t.Helper()

	reqBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.URL+path, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(name, value)

	return h.send(t, req)
}

// Do sends a body-less request with method to the admin API path.
func (h *Harness) Do(t *testing.T, method, path string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, h.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}

	return h.send(t, req)
}

func (h *Harness) send(t *testing.T, req *http.Request) (int, []byte) {
	t.Helper()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response to %s %s: %v", req.Method, req.URL.Path, err)
	}

	return resp.StatusCode, body
}
