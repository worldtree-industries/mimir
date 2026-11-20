package models

// ScopeFilter selects Scopes, to narrow a listing of Overrides or of History.
// An empty Config matches any Config. A nil Deployable or Environment matches
// any; otherwise the Scope must have exactly that value, where "" means not
// narrowed.
type ScopeFilter struct {
	Config      string
	Deployable  *string
	Environment *string
}

// Matches reports whether s is one of the Scopes f selects.
func (f ScopeFilter) Matches(s Scope) bool {
	return (f.Config == "" || f.Config == s.Config) &&
		(f.Deployable == nil || *f.Deployable == s.Deployable) &&
		(f.Environment == nil || *f.Environment == s.Environment)
}
