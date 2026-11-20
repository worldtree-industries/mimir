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

	if err := validateValue(o); err != nil {
		return models.Override{}, err
	}

	if o.Scope.Environment != "" {
		if err := s.checkEnvironment(ctx, o.Scope.Environment); err != nil {
			return models.Override{}, err
		}
	}

	var created models.Override
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		var createErr error
		created, createErr = s.store.CreateOverride(ctx, o, s.clock.Now())
		if createErr != nil {
			return createErr
		}

		return s.recordHistory(ctx, models.HistoryCreated, o.Scope, o.Item, nil, &o.Value)
	})
	switch {
	case errors.Is(err, data.ErrUnknownItem):
		return models.Override{}, ErrUnknownItem
	case errors.Is(err, data.ErrOverrideExists):
		return models.Override{}, ErrOverrideExists
	case err != nil:
		return models.Override{}, err
	}

	s.republishScope(ctx, o.Scope)

	return created, nil
}

// UpdateOverride sets the value of Override id, publishes the Snapshot of
// every Namespace its Scope covers, and records the change in history. The
// value must suit the Item's type. Only the value can change: a different
// Scope or Item is a different Override.
func (s *Service) UpdateOverride(ctx context.Context, id int64, value string) (models.Override, error) {
	var updated models.Override
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		current, err := s.store.Override(ctx, id)
		if err != nil {
			return err
		}
		if err = validateValue(models.Override{Item: current.Item, Value: value}); err != nil {
			return err
		}
		if updated, err = s.store.UpdateOverride(ctx, id, value, s.clock.Now()); err != nil {
			return err
		}

		return s.recordHistory(ctx, models.HistoryUpdated, updated.Scope, updated.Item, &current.Value, &updated.Value)
	})
	if errors.Is(err, data.ErrOverrideNotFound) {
		return models.Override{}, ErrOverrideNotFound
	}
	if err != nil {
		return models.Override{}, err
	}

	s.republishScope(ctx, updated.Scope)

	return updated, nil
}

// DeleteOverride removes Override id, publishes the Snapshot of every
// Namespace its Scope covered, so the next most specific Override, or the
// Default, takes effect there, and records the change in history.
func (s *Service) DeleteOverride(ctx context.Context, id int64) error {
	var deleted models.Override
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		var err error
		if deleted, err = s.store.DeleteOverride(ctx, id); err != nil {
			return err
		}

		return s.recordHistory(ctx, models.HistoryDeleted, deleted.Scope, deleted.Item, &deleted.Value, nil)
	})
	if errors.Is(err, data.ErrOverrideNotFound) {
		return ErrOverrideNotFound
	}
	if err != nil {
		return err
	}

	s.republishScope(ctx, deleted.Scope)

	return nil
}

// validateValue rejects an Override whose value doesn't parse as its Item's
// type. Every write path of an Override runs it.
func validateValue(o models.Override) error {
	if err := models.ValidateValue(o.Item.Type, o.Value); err != nil {
		return fmt.Errorf("%w: the value %w", ErrInvalid, err)
	}

	return nil
}
