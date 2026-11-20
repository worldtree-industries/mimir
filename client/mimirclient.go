package client

import (
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/worldtree-industries/mimir/models"
)

// MimirClientConfig is the name of MimirClient, the Config holding the
// client's own poll interval, server URL, object storage endpoint and bucket
// prefix. Like a generated Config's name it is derived from
// its Go package path and type. Every client registers it alongside the
// Config it reads, in the same Deployable and Environment, so operators tune
// it with Overrides and Precedence like any other Config.
const MimirClientConfig = "github.com/worldtree-industries/mimir/client.MimirClient"

// mimirClientDisplayName is MimirClient's short display name.
const mimirClientDisplayName = "MimirClient"

// MimirClient's Items. All are strings.
const (
	// PollIntervalItem is how often the Snapshot is polled, as a Go
	// duration such as "30s". It starts from MIMIR_POLL_INTERVAL.
	PollIntervalItem = "poll_interval"
	// ServerURLItem is mimir-server's base URL, used for Registration. It
	// starts from MIMIR_SERVER_URL.
	ServerURLItem = "server_url"
	// ObjectStoreEndpointItem is the object storage endpoint Snapshots are
	// read from; empty means the standard S3 settings (AWS_ENDPOINT_URL_S3
	// or AWS_ENDPOINT_URL). It starts from MIMIR_OBJECT_STORE_ENDPOINT.
	ObjectStoreEndpointItem = "object_store_endpoint"
	// BucketPrefixItem is the prefix of each Environment's bucket,
	// "<prefix>-<environment>" (ADR 0006). It starts from
	// MIMIR_BUCKET_PREFIX.
	BucketPrefixItem = "bucket_prefix"
)

// The environment variables each MimirClient Item starts from. This layer
// between the Defaults and the Overrides exists for MimirClient only, so a
// client can find its Snapshots before it has read any.
const (
	envPollInterval = "MIMIR_POLL_INTERVAL"
	envServerURL    = "MIMIR_SERVER_URL"
	envEndpoint     = "MIMIR_OBJECT_STORE_ENDPOINT"
	envBucketPrefix = "MIMIR_BUCKET_PREFIX"
)

// minPollInterval is the shortest poll interval accepted, so a slip in an
// Override can't have every pod hammer object storage.
const minPollInterval = time.Second

var (
	errPollInterval = errors.New("poll interval must be a Go duration of at least " + minPollInterval.String())
	errURL          = errors.New("must be an absolute http or https URL")
)

// location is where a client reads Snapshots: an object storage endpoint
// and a bucket prefix.
type location struct {
	endpoint string
	prefix   string
}

func (l location) String() string {
	endpoint := l.endpoint
	if endpoint == "" {
		endpoint = "the default endpoint"
	}

	return fmt.Sprintf("bucket prefix %q at %s", l.prefix, endpoint)
}

// mimirClientValues are the current values of MimirClient's Items. Its
// setters run in the Runtime's loop, and also at boot, so it is guarded.
type mimirClientValues struct {
	mu           sync.Mutex
	pollInterval time.Duration
	serverURL    string
	wanted       location
}

// definition is MimirClient as a Definition whose setters fill s.
func (s *mimirClientValues) definition() Definition {
	return Definition{
		Config:      MimirClientConfig,
		DisplayName: mimirClientDisplayName,
		Items: []ItemDefinition{
			{
				Name:        PollIntervalItem,
				Type:        models.TypeString,
				Description: "How often the client polls its Snapshots, as a Go duration (at least 1s).",
				Default:     DefaultPollInterval.String(),
				Set:         s.setPollInterval,
			},
			{
				Name:        ServerURLItem,
				Type:        models.TypeString,
				Description: "mimir-server's base URL, used for Registration.",
				Default:     defaultServerURL,
				Set:         s.setServerURL,
			},
			{
				Name: ObjectStoreEndpointItem,
				Type: models.TypeString,
				Description: "The object storage endpoint Snapshots are read from; empty means the standard S3 settings. " +
					"A new endpoint is used only once a Snapshot has been read from it.",
				Default: "",
				Set:     s.setEndpoint,
			},
			{
				Name: BucketPrefixItem,
				Type: models.TypeString,
				Description: "The prefix of each Environment's bucket, <prefix>-<environment>. " +
					"A new prefix is used only once a Snapshot has been read with it.",
				Default: models.DefaultBucketPrefix,
				Set:     s.setBucketPrefix,
			},
		},
	}
}

// envVar is the environment variable an Item of MimirClient starts from.
func envVar(item string) string {
	switch item {
	case PollIntervalItem:
		return envPollInterval
	case ServerURLItem:
		return envServerURL
	case ObjectStoreEndpointItem:
		return envEndpoint
	case BucketPrefixItem:
		return envBucketPrefix
	}

	return ""
}

func (s *mimirClientValues) setPollInterval(value string) error {
	d, err := time.ParseDuration(value)
	if err != nil || d < minPollInterval {
		return errPollInterval
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pollInterval = d

	return nil
}

func (s *mimirClientValues) setServerURL(value string) error {
	if err := checkURL(value); err != nil {
		return fmt.Errorf("server URL: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverURL = value

	return nil
}

func (s *mimirClientValues) setEndpoint(value string) error {
	if value != "" {
		if err := checkURL(value); err != nil {
			return fmt.Errorf("object storage endpoint: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.wanted.endpoint = value

	return nil
}

func (s *mimirClientValues) setBucketPrefix(value string) error {
	if err := models.CheckBucketPrefix(value); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.wanted.prefix = value

	return nil
}

// namedLocation is the location MimirClient names once snap is applied over
// its start values. A value MimirClient would reject leaves that part of
// current, as applying it would.
func namedLocation(snap models.Snapshot, start map[models.ItemRef]string, current location) location {
	value := func(item string) string {
		key := models.ItemRef{Name: item, Type: models.TypeString}
		if v, ok := snap.Value(key); ok {
			return v
		}

		return start[key]
	}

	named := current
	if endpoint := value(ObjectStoreEndpointItem); endpoint == "" || checkURL(endpoint) == nil {
		named.endpoint = endpoint
	}
	if prefix := value(BucketPrefixItem); models.CheckBucketPrefix(prefix) == nil {
		named.prefix = prefix
	}

	return named
}

func checkURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errURL
	}

	return nil
}

func (s *mimirClientValues) currentPollInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.pollInterval
}

func (s *mimirClientValues) currentServerURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.serverURL
}

// wantedLocation is the location the MimirClient Config names. The client reads from it
// only once it has fetched its Snapshots there.
func (s *mimirClientValues) wantedLocation() location {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.wanted
}
