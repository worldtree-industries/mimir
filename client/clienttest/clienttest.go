// Package clienttest helps tests run a program's Configs against a
// mimir-server the test starts itself, such as an [net/http/httptest]
// server.
package clienttest

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/worldtree-industries/mimir/client"
)

// timeout bounds each request of a routed client, as the client's own
// default does.
const timeout = 10 * time.Second

// RouteDefaultServer returns an HTTP client, for [client.WithHTTPClient],
// that sends requests for mimir-server at its Default URL to the server at
// to, standing in for the cluster's DNS. Requests for any other URL, as a
// server URL Override names, go unchanged.
func RouteDefaultServer(to string) (*http.Client, error) {
	target, err := url.Parse(to)
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("route the Default server URL to %q: not an absolute URL", to)
	}
	def, err := url.Parse(client.DefaultServerURL)
	if err != nil {
		return nil, fmt.Errorf("parse the Default server URL: %w", err)
	}

	return &http.Client{Timeout: timeout, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == def.Host {
			req = req.Clone(req.Context())
			req.URL.Scheme, req.URL.Host, req.Host = target.Scheme, target.Host, target.Host
		}
		return http.DefaultTransport.RoundTrip(req)
	})}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
