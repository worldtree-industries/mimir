package client_test

import (
	"testing"

	"github.com/worldtree-industries/mimir/client"
)

func TestServerURLDefaultsToMimirServerInMimirConfig(t *testing.T) {
	const want = "http://mimir-server.mimir-config"
	if client.DefaultServerURL != want {
		t.Errorf("server URL Default = %q, want %q", client.DefaultServerURL, want)
	}
}
