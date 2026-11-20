package service

import (
	"context"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// ScopeFilter narrows a listing of Overrides. A nil Deployable or Environment
// matches any; otherwise the Override's Scope must have exactly that value,
// where "" means not narrowed.
type ScopeFilter struct {
	Config      string
	Deployable  *string
	Environment *string
}

// ListOverrides returns the Overrides of filter's Config that match it,
// oldest first.
func (s *Service) ListOverrides(ctx context.Context, filter ScopeFilter) ([]models.Override, error) {
	if filter.Config == "" {
		return nil, fmt.Errorf("%w: a Config is required", ErrInvalid)
	}

	all, err := s.store.Overrides(ctx, filter.Config)
	if err != nil {
		return nil, err
	}

	matching := make([]models.Override, 0, len(all))
	for _, o := range all {
		if (filter.Deployable == nil || *filter.Deployable == o.Scope.Deployable) &&
			(filter.Environment == nil || *filter.Environment == o.Scope.Environment) {
			matching = append(matching, o)
		}
	}

	return matching, nil
}

// Explain answers why ns has the values it has: each Item's effective value
// and the Override (or Default) it comes from, by the same Precedence the
// Snapshot is built with.
func (s *Service) Explain(ctx context.Context, ns models.Namespace) (models.Explanation, error) {
	if ns.Config == "" || ns.Deployable == "" || ns.Environment == "" {
		return models.Explanation{}, fmt.Errorf(
			"%w: a Namespace needs a Config, a Deployable and an Environment",
			ErrInvalid,
		)
	}

	known, err := s.store.HasNamespace(ctx, ns)
	if err != nil {
		return models.Explanation{}, err
	}
	if !known {
		return models.Explanation{}, ErrUnknownNamespace
	}

	items, err := s.store.Items(ctx, ns.Config)
	if err != nil {
		return models.Explanation{}, err
	}
	overrides, err := s.store.Overrides(ctx, ns.Config)
	if err != nil {
		return models.Explanation{}, err
	}

	return models.Explain(ns, items, overrides), nil
}
