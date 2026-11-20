package models

import "time"

// HistoryAction is what a history entry records being done to an Override.
type HistoryAction string

// The actions History records.
const (
	HistoryCreated HistoryAction = "created"
	HistoryUpdated HistoryAction = "updated"
	HistoryDeleted HistoryAction = "deleted"
	// HistoryExpired is an Override removed because its Item expired.
	HistoryExpired HistoryAction = "expired"
)

// HistoryEntry is one append-only record of a change to an Override. It holds
// the Scope and Item by value, not by reference, so it outlives the Override
// and the Item. OldValue is nil for a create and NewValue is nil for a delete
// or an expiry.
// Caller is who made the change, as best known: the name the caller gave, or
// else the address the request came from.
type HistoryEntry struct {
	ID       int64         `json:"id"`
	Time     time.Time     `json:"time"`
	Action   HistoryAction `json:"action"`
	Scope    Scope         `json:"scope"`
	Item     ItemRef       `json:"item"`
	OldValue *string       `json:"old_value"`
	NewValue *string       `json:"new_value"`
	Caller   string        `json:"caller"`
}
