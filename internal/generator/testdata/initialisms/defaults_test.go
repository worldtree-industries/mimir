package initialisms_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator/testdata/initialisms"
)

// itemGetters is the kind of interface a Deployable declares over a Config,
// so it can hand-write a fake whose method names pass lint.
type itemGetters interface {
	GetOpnsenseURL() string
	GetVictorialogsURLs() []string
}

type fakeItemGetters struct{}

func (fakeItemGetters) GetOpnsenseURL() string        { return "https://fake.example" }
func (fakeItemGetters) GetVictorialogsURLs() []string { return nil }

var (
	_ itemGetters = (*initialisms.InitialismsConfig)(nil)
	_ itemGetters = fakeItemGetters{}
)

func TestGettersReturnDefaults(t *testing.T) {
	cfg := initialisms.NewInitialismsConfig(context.Background())

	if got, want := cfg.GetOpnsenseURL(), "https://opnsense.example"; got != want {
		t.Errorf("opnsense_url = %q, want %q", got, want)
	}
	if got, want := cfg.GetVictorialogsURLs(), []string{"https://a.example", "https://b.example"}; !reflect.DeepEqual(
		got, want) {
		t.Errorf("victorialogs_urls = %v, want %v", got, want)
	}
	if got, want := cfg.GetRequestTTL(), int64(30); got != want {
		t.Errorf("request_ttl = %d, want %d", got, want)
	}
	if got := cfg.GetUserIDs().Len(); got != 2 {
		t.Errorf("user_ids has %d members, want 2", got)
	}
}
