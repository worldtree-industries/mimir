package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/worldtree-industries/mimir/data"
	"github.com/worldtree-industries/mimir/models"
)

// CreateOverride stores o and publishes the Snapshot of every Namespace of
// its Config. The Item must be registered. Only Config-wide Scopes are
// supported so far.
func (s *Service) CreateOverride(ctx context.Context, o models.Override) (models.Override, error) {
	switch {
	case o.Scope.Config == "":
		return models.Override{}, fmt.Errorf("%w: the Scope has no Config", ErrInvalid)
	case o.Item.Name == "" || o.Item.Type == "":
		return models.Override{}, fmt.Errorf("%w: the Item needs a name and a type", ErrInvalid)
	case !o.Scope.ConfigWide():
		// TODO(#11): narrowed Scopes and Precedence.
		return models.Override{}, fmt.Errorf("%w: only Config-wide Scopes are supported", ErrInvalid)
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

	s.publishConfigLogged(ctx, o.Scope.Config)

	return created, nil
}
