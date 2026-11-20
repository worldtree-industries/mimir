package service

import (
	"context"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// Register records reg: its Config, Items and Namespace (ADR 0004).
// Registration only adds. A Namespace registering for the first time gets its
// Snapshot published, so broad Overrides reach it within one poll interval.
func (s *Service) Register(ctx context.Context, reg models.Registration) error {
	if err := validateRegistration(reg); err != nil {
		return err
	}

	if err := s.checkEnvironment(ctx, reg.Namespace.Environment); err != nil {
		return err
	}

	var isNew bool
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		var recordErr error
		isNew, recordErr = s.store.RecordRegistration(ctx, reg, s.clock.Now())
		return recordErr
	})
	if err != nil {
		return fmt.Errorf("record Registration: %w", err)
	}

	if isNew {
		s.publishLogged(ctx, reg.Namespace)
	}

	return nil
}

func validateRegistration(reg models.Registration) error {
	ns := reg.Namespace
	switch {
	case ns.Config == "":
		return fmt.Errorf("%w: the Namespace has no Config", ErrInvalid)
	case ns.Deployable == "":
		return fmt.Errorf("%w: the Namespace has no Deployable", ErrInvalid)
	case ns.Environment == "":
		return fmt.Errorf("%w: the Namespace has no Environment", ErrInvalid)
	}
	for _, item := range reg.Items {
		if item.Name == "" || item.Type == "" {
			return fmt.Errorf("%w: every Item needs a name and a type", ErrInvalid)
		}
	}

	return nil
}
