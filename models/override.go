package models

// Scope is the set of Namespaces an Override applies to: always one Config,
// optionally narrowed to one Deployable, one Environment, or both. An empty
// Deployable or Environment means any.
type Scope struct {
	Config      string `json:"config"`
	Deployable  string `json:"deployable,omitempty"`
	Environment string `json:"environment,omitempty"`
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
	Item  ItemKey `json:"item"`
	Value string  `json:"value"`
}
