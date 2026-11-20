package models

import (
	"cmp"
	"log/slog"
)

// Namespace is one concrete use of a Config: the Config, the Deployable using
// it, and the Environment it runs in.
type Namespace struct {
	Config      string `json:"config"`
	Deployable  string `json:"deployable"`
	Environment string `json:"environment"`
}

// ItemRef identifies an Item within a Config: its name and type together.
// Changing an Item's type makes it a different Item (ADR 0004).
type ItemRef struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
}

// Compare orders ItemRefs by name, then type: the order Snapshots and
// explanations list Items in. It returns -1, 0 or +1, as [cmp.Compare].
func (r ItemRef) Compare(other ItemRef) int {
	return cmp.Or(cmp.Compare(r.Name, other.Name), cmp.Compare(r.Type, other.Type))
}

// LogValue logs the Namespace as its three parts.
func (ns Namespace) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("config", ns.Config),
		slog.String("deployable", ns.Deployable),
		slog.String("environment", ns.Environment),
	)
}

// Ref returns the Item's identity.
func (i Item) Ref() ItemRef {
	return ItemRef{Name: i.Name, Type: i.Type}
}

// Registration is a running Deployable telling Mimir which Items a Config it
// reads declares, in the Namespace it runs in, each with its Default.
// DisplayName is the Config's optional short display name.
type Registration struct {
	Namespace   Namespace `json:"namespace"`
	DisplayName string    `json:"display_name,omitempty"`
	Items       []Item    `json:"items"`
}
