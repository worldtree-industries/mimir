package service

import (
	"context"
	"fmt"

	"github.com/worldtree-industries/mimir/models"
)

// ListOverrides returns the Overrides whose Scope filter matches, oldest
// first. filter must name a Config.
func (s *Service) ListOverrides(ctx context.Context, filter models.ScopeFilter) ([]models.Override, error) {
	if filter.Config == "" {
		return nil, fmt.Errorf("%w: a Config is required", ErrInvalid)
	}

	all, err := s.store.Overrides(ctx, filter.Config)
	if err != nil {
		return nil, err
	}

	matching := make([]models.Override, 0, len(all))
	for _, o := range all {
		if filter.Matches(o.Scope) {
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
