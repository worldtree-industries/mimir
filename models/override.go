package models

import "log/slog"

// Scope is the set of Namespaces an Override applies to: always one Config,
// optionally narrowed to one Deployable, one Environment, or both. An empty
// Deployable or Environment means any.
type Scope struct {
	Config      string `json:"config"`
	Deployable  string `json:"deployable,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// LogValue logs the Scope as its parts, leaving out those not narrowed.
func (s Scope) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("config", s.Config)}
	if s.Deployable != "" {
		attrs = append(attrs, slog.String("deployable", s.Deployable))
	}
	if s.Environment != "" {
		attrs = append(attrs, slog.String("environment", s.Environment))
	}

	return slog.GroupValue(attrs...)
}

// ConfigWide reports whether the Scope covers every Namespace of its Config.
func (s Scope) ConfigWide() bool {
	return s.Deployable == "" && s.Environment == ""
}

// Override is a value set through Mimir for one Item within a Scope. ID is
// assigned by mimir-server.
type Override struct {
	ID    int64   `json:"id,omitempty"`
	Scope Scope   `json:"scope"`
	Item  ItemRef `json:"item"`
	Value string  `json:"value"`
}
