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

// registerDue registers, at the server URL MimirClient names now, MimirClient
// and every Config whose Registration is due: each is due when it starts, then
// DefaultRegistrationInterval after it registered, or RegistrationRetryInterval
// after an attempt failed. A Registration only falls due between polls, so
// it lands at the first poll after it is due. Each Config registered is sent
// one EventRegistered, whose Err joins its own failure with MimirClient's
// when MimirClient was registered with it. A failure is logged; it never
// affects the values the program reads.
func (rt *Runtime) registerDue(ctx context.Context) {
	now := rt.clock().Now()

	var selfErr error
	if !now.Before(rt.selfDue) {
		selfErr = rt.register(ctx, rt.self)
		rt.selfDue = nextRegistration(now, selfErr)
	}

	for _, c := range rt.live() {
		if now.Before(c.registrationDue) {
			continue
		}
		err := rt.register(ctx, c.poll)
		c.registrationDue = nextRegistration(now, err)
		c.emit(Event{Kind: EventRegistered, Err: errors.Join(selfErr, err)})
	}
}

func nextRegistration(now time.Time, err error) time.Time {
	if err != nil {
		return now.Add(RegistrationRetryInterval)
	}

	return now.Add(DefaultRegistrationInterval)
}

// register sends the Registration of p's Config, logging a failure.
func (rt *Runtime) register(ctx context.Context, p *configPoll) error {
	err := rt.sendRegistration(ctx, p)
	if err != nil {
		err = fmt.Errorf("register %s: %w", p.def.Config, err)
		p.logger.ErrorContext(ctx, "Registration failed; values are unaffected", "error", err)
	}

	return err
}

func (rt *Runtime) sendRegistration(ctx context.Context, p *configPoll) error {
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
	url := rt.values.currentServerURL() + registrationPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rt.http.Do(req)
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
