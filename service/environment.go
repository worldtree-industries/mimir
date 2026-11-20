package service

import (
	"context"
	"fmt"
)

// EnsureEnvironments creates the named Environments that are missing. The
// server calls it at startup with mjolnir's Environments.
func (s *Service) EnsureEnvironments(ctx context.Context, names []string) error {
	return s.store.EnsureEnvironments(ctx, names)
}

// Environments lists the Environments Mimir knows, in name order.
func (s *Service) Environments(ctx context.Context) ([]string, error) {
	return s.store.Environments(ctx)
}

// checkEnvironment rejects a name that isn't a known Environment.
func (s *Service) checkEnvironment(ctx context.Context, name string) error {
	ok, err := s.store.HasEnvironment(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %q is not a known Environment", ErrInvalid, name)
	}

	return nil
}
