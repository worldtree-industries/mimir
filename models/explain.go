package models

import "slices"

// SourceKind is what kind of Source an Item's effective value comes from.
type SourceKind string

// The kinds of Source.
const (
	// SourceDefault means no Override applies, so the Item has its Default.
	SourceDefault SourceKind = "default"
	// SourceOverride means an Override won by Precedence.
	SourceOverride SourceKind = "override"
)

// Source says where an Item's effective value comes from: the Default, or the
// Override that won by Precedence. Override is set only for SourceOverride.
type Source struct {
	Kind     SourceKind `json:"kind"`
	Override *Override  `json:"override,omitempty"`
}

// ExplainedItem is one Item's effective value in a Namespace and its Source.
// Default is always the Item's Default, so an Override's effect is visible.
type ExplainedItem struct {
	Item        ItemRef `json:"item"`
	Description string  `json:"description"`
	Default     string  `json:"default"`
	Value       string  `json:"value"`
	Source      Source  `json:"source"`
}

// Explanation answers "why does this Namespace have these values?": every
// Item of the Namespace's Config, ordered by name then type, with its
// effective value and Source.
type Explanation struct {
	Namespace Namespace       `json:"namespace"`
	Items     []ExplainedItem `json:"items"`
}

// Explain applies Precedence to overrides for ns and reports, for each of
// items (the Config's Items), its effective value
// and Source.
func Explain(ns Namespace, items []Item, overrides []Override) Explanation {
	winners := Precedence(ns, overrides)

	explained := make([]ExplainedItem, 0, len(items))
	for _, item := range items {
		e := ExplainedItem{
			Item:        item.Ref(),
			Description: item.Description,
			Default:     item.Default,
			Value:       item.Default,
			Source:      Source{Kind: SourceDefault},
		}
		if o, ok := winners[item.Ref()]; ok {
			e.Value = o.Value
			e.Source = Source{Kind: SourceOverride, Override: &o}
		}
		explained = append(explained, e)
	}
	slices.SortFunc(explained, func(a, b ExplainedItem) int { return a.Item.Compare(b.Item) })

	return Explanation{Namespace: ns, Items: explained}
}
