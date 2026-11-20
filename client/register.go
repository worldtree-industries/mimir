package client

import (
	"bytes"
	"context"
	"encoding/json"
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

// register tells mimir-server about the Namespace and the Config's Items. A
// failure is logged and reported; it never affects the values the program
// reads.
func (c *Client) register(ctx context.Context) {
	err := c.sendRegistration(ctx)
	if err != nil {
		c.logger.ErrorContext(ctx, "Registration failed; values are unaffected", "error", err)
	}
	c.emit(EventRegistered, err)
}

func (c *Client) sendRegistration(ctx context.Context) error {
	reg := models.Registration{Namespace: c.ns, DisplayName: c.def.DisplayName}
	for _, item := range c.def.Items {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL+registrationPath, bytes.NewReader(body))
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
