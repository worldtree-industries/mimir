package service

import (
	"context"

	"github.com/worldtree-industries/mimir/models"
)

type callerKey struct{}

// WithCaller returns a ctx that names who is calling the Service, as best the
// caller of the Service knows. History records it.
func WithCaller(ctx context.Context, caller string) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

func callerFrom(ctx context.Context) string {
	caller, _ := ctx.Value(callerKey{}).(string)
	if caller == "" {
		return "unknown"
	}

	return caller
}

// recordHistory appends an entry for a change to the Override of item in
// scope, from oldValue to newValue (nil for none), stamped with the clock and
// the caller in ctx. Every write path of an Override calls it in the same
// transaction as the write, so a change and its history stand or fall
// together. Other writers, such as expiry, record with their own caller.
func (s *Service) recordHistory(
	ctx context.Context, action models.HistoryAction, scope models.Scope, item models.ItemKey,
	oldValue, newValue *string,
) error {
	_, err := s.store.AppendHistory(ctx, models.HistoryEntry{
		Time:     s.clock.Now(),
		Action:   action,
		Scope:    scope,
		Item:     item,
		OldValue: oldValue,
		NewValue: newValue,
		Caller:   callerFrom(ctx),
	})

	return err
}

// History returns the History entries whose Scope filter matches, oldest
// first.
func (s *Service) History(ctx context.Context, filter models.ScopeFilter) ([]models.HistoryEntry, error) {
	return s.store.History(ctx, filter)
}
