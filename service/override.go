package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/worldtree-industries/mimir/data"
	"github.com/worldtree-industries/mimir/models"
)

// CreateOverride stores o and publishes the Snapshot of every Namespace its
// Scope covers. The Item must be registered.
func (s *Service) CreateOverride(ctx context.Context, o models.Override) (models.Override, error) {
	switch {
	case o.Scope.Config == "":
		return models.Override{}, fmt.Errorf("%w: the Scope has no Config", ErrInvalid)
	case o.Item.Name == "" || o.Item.Type == "":
		return models.Override{}, fmt.Errorf("%w: the Item needs a name and a type", ErrInvalid)
	}

	created, err := s.store.CreateOverride(ctx, o, s.clock.Now())
	switch {
	case errors.Is(err, data.ErrUnknownItem):
		return models.Override{}, ErrUnknownItem
	case errors.Is(err, data.ErrOverrideExists):
		return models.Override{}, ErrOverrideExists
	case err != nil:
		return models.Override{}, err
	}

	s.publishScopeLogged(ctx, o.Scope)

	return created, nil
}

// UpdateOverride sets the value of Override id and publishes the Snapshot of
// every Namespace its Scope covers. Only the value can change: a different
// Scope or Item is a different Override.
func (s *Service) UpdateOverride(ctx context.Context, id int64, value string) (models.Override, error) {
	updated, err := s.store.UpdateOverride(ctx, id, value, s.clock.Now())
	if errors.Is(err, data.ErrOverrideNotFound) {
		return models.Override{}, ErrOverrideNotFound
	}
	if err != nil {
		return models.Override{}, err
	}

	s.publishScopeLogged(ctx, updated.Scope)

	return updated, nil
}

// DeleteOverride removes Override id and publishes the Snapshot of every
// Namespace its Scope covered, so the next most specific Override, or the
// Default, takes effect there.
func (s *Service) DeleteOverride(ctx context.Context, id int64) error {
	deleted, err := s.store.DeleteOverride(ctx, id)
	if errors.Is(err, data.ErrOverrideNotFound) {
		return ErrOverrideNotFound
	}
	if err != nil {
		return err
	}

	s.publishScopeLogged(ctx, deleted.Scope)

	return nil
}
