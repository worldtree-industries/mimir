package models

// Namespace is one concrete use of a Config: the Config, the Deployable using
// it, and the Environment it runs in.
type Namespace struct {
	Config      string `json:"config"`
	Deployable  string `json:"deployable"`
	Environment string `json:"environment"`
}

// ItemKey identifies an Item within a Config: its name and type together.
// Changing an Item's type makes it a different Item (ADR 0004).
type ItemKey struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
}

// Key returns the Item's identity.
func (i Item) Key() ItemKey {
	return ItemKey{Name: i.Name, Type: i.Type}
}

// Registration is a running Deployable telling Mimir which Items a Config it
// reads declares, in the Namespace it runs in. Each Item's Value is its
// Default. DisplayName is the Config's optional short display name.
type Registration struct {
	Namespace   Namespace `json:"namespace"`
	DisplayName string    `json:"display_name,omitempty"`
	Items       []Item    `json:"items"`
}
