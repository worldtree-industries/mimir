package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/worldtree-industries/mimir/models"
)

// registrationTimeout bounds one Registration request.
const registrationTimeout = 10 * time.Second

// registrationPath is mimir-server's Registration endpoint.
const registrationPath = "/v1/registrations"

// DefaultRegistrationInterval is how often a running client registers again,
// so Items its Deployable still declares never expire (ADR 0004).
const DefaultRegistrationInterval = 24 * time.Hour

// RegistrationRetryInterval is how soon a client tries again after a
// Registration failed.
const RegistrationRetryInterval = 5 * time.Minute

// registerEvery registers at once and then again every
// DefaultRegistrationInterval, or after RegistrationRetryInterval when the
// last attempt failed, until ctx is done.
func (c *Client) registerEvery(ctx context.Context) {
	for {
		wait := DefaultRegistrationInterval
		if err := c.register(ctx); err != nil {
			wait = RegistrationRetryInterval
		}

		select {
		case <-ctx.Done():
			return
		case <-c.clock.After(wait):
		}
	}
}

// register tells mimir-server about the Namespace and the Config's Items,
// and then about MimirClient's, at the server URL the settings name now. A
// failure is logged and reported, in one EventRegistered for both; it never
// affects the values the program reads.
func (c *Client) register(ctx context.Context) error {
	var errs []error
	for _, p := range []*configPoll{c.user, c.self} {
		err := c.sendRegistration(ctx, p)
		if err != nil {
			err = fmt.Errorf("register %s: %w", p.def.Config, err)
			c.logger.ErrorContext(ctx, "Registration failed; values are unaffected", "error", err)
		}
		errs = append(errs, err)
	}
	err := errors.Join(errs...)
	c.emit(Event{Kind: EventRegistered, Err: err})

	return err
}

func (c *Client) sendRegistration(ctx context.Context, p *configPoll) error {
	reg := models.Registration{Namespace: p.ns, DisplayName: p.def.DisplayName}
	for _, item := range p.def.Items {
		reg.Items = append(reg.Items, models.Item{
			Name:        item.Name,
			Description: item.Description,
			Type:        item.Type,
			Value:       item.Default,
		})
	}

	body, err := json.Marshal(reg)
	if err != nil {
		return fmt.Errorf("encode Registration: %w", err)
	}
	url := c.settings.currentServerURL() + registrationPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send Registration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("mimir-server answered Registration with %s: %s", resp.Status, detail)
	}

	return nil
}

// maxErrorBody bounds how much of an error response is kept for the log.
const maxErrorBody = 4 << 10
