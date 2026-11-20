package models

import "time"

// ConfigInfo is a Config as Mimir knows it from Registration.
type ConfigInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	// LastRegisteredAt is when a Deployable last registered the Config.
	LastRegisteredAt time.Time `json:"last_registered_at"`
}

// RegisteredItem is an Item as Mimir knows it from Registration. An Item no
// Deployable has registered for 60 days expires (ADR 0004).
type RegisteredItem struct {
	ItemKey

	Config      string `json:"config"`
	Description string `json:"description"`
	Default     string `json:"default"`
	// FirstRegisteredAt and LastRegisteredAt are the first and the most
	// recent time any Deployable registered the Item.
	FirstRegisteredAt time.Time `json:"first_registered_at"`
	LastRegisteredAt  time.Time `json:"last_registered_at"`
}

// RegisteredNamespace is a Namespace as Mimir knows it from Registration. A
// Namespace unregistered for 60 days expires.
type RegisteredNamespace struct {
	Namespace

	FirstRegisteredAt time.Time `json:"first_registered_at"`
	LastRegisteredAt  time.Time `json:"last_registered_at"`
}
