package client

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/dfryer1193/mimir/api/config"
	"github.com/r3labs/sse/v2"
)

var _ MimirClient = (*mimirClient)(nil)

// SetFunc is a function that sets a value. The value should be parseable into the type of the config value.
type SetFunc func(strVal string)

type MimirClient interface {
	Start(ctx context.Context, namespace string, cfg *config.Config) (*config.Config, error)
	Stop() error

	UpdateCh() <-chan *config.Update
	ErrorCh() <-chan error
}

type mimirClient struct {
	serverAddr string
	client     *sse.Client

	mu     sync.Mutex
	cancel context.CancelFunc

	updateCh chan *config.Update
	errorCh  chan error

	wg sync.WaitGroup
}

func NewClient(host string) (*mimirClient, error) {
	serverAddr := "http://" + host // TODO: make http/s configurable

	return &mimirClient{
		serverAddr: serverAddr,
		client:     sse.NewClient(serverAddr),

		updateCh: make(chan *config.Update, 100),
		errorCh:  make(chan error, 1),
	}, nil
}

func (c *mimirClient) Start(ctx context.Context, namespace string, cfg *config.Config) (*config.Config, error) {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()

	// TODO: Request config from the server, or post if it doesn't exist

	c.wg.Go(func() {
		err := c.client.SubscribeWithContext(ctx, namespace, c.handleEvent)
		if err != nil {
			c.errorCh <- err
		}
	})

	return cfg, nil
}

func (c *mimirClient) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	c.wg.Wait()

	close(c.updateCh)
	close(c.errorCh)

	return nil
}

func (c *mimirClient) UpdateCh() <-chan *config.Update {
	return c.updateCh
}

func (c *mimirClient) ErrorCh() <-chan error {
	return c.errorCh
}

func (c *mimirClient) handleEvent(msg *sse.Event) {
	update := &config.Update{}
	err := json.Unmarshal(msg.Data, update)
	if err != nil {
		c.errorCh <- err
		return
	}

	c.updateCh <- update
}
