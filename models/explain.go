package models

import (
	"cmp"
	"slices"
)

// The kinds of Source an Item's effective value can come from.
const (
	// SourceDefault means no Override applies, so the Item has its Default.
	SourceDefault = "default"
	// SourceOverride means an Override won by Precedence.
	SourceOverride = "override"
)

// Source says where an Item's effective value comes from: the Default, or the
// Override that won by Precedence. Override is set only for SourceOverride.
type Source struct {
	Kind     string    `json:"kind"`
	Override *Override `json:"override,omitempty"`
}

// ExplainedItem is one Item's effective value in a Namespace and its Source.
// Default is always the Item's Default, so an Override's effect is visible.
type ExplainedItem struct {
	Item        ItemKey `json:"item"`
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
// items (the Config's Items, with Value as the Default), its effective value
// and Source.
func Explain(ns Namespace, items []Item, overrides []Override) Explanation {
	winners := Precedence(ns, overrides)

	explained := make([]ExplainedItem, 0, len(items))
	for _, item := range items {
		e := ExplainedItem{
			Item:        item.Key(),
			Description: item.Description,
			Default:     item.Value,
			Value:       item.Value,
			Source:      Source{Kind: SourceDefault},
		}
		if o, ok := winners[item.Key()]; ok {
			e.Value = o.Value
			e.Source = Source{Kind: SourceOverride, Override: &o}
		}
		explained = append(explained, e)
	}
	slices.SortFunc(explained, func(a, b ExplainedItem) int {
		return cmp.Or(cmp.Compare(a.Item.Name, b.Item.Name), cmp.Compare(a.Item.Type, b.Item.Type))
	})

	return Explanation{Namespace: ns, Items: explained}
}
