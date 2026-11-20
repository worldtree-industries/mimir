package models

// Covers reports whether the Scope covers ns: the same Config, and the same
// Deployable and Environment wherever the Scope names one.
func (s Scope) Covers(ns Namespace) bool {
	return s.Config == ns.Config &&
		(s.Deployable == "" || s.Deployable == ns.Deployable) &&
		(s.Environment == "" || s.Environment == ns.Environment)
}

// Specificity ranks the Scope by Precedence (ADR 0003): the higher, the more
// specific. Deployable and Environment beats Deployable, which beats
// Environment, which beats the whole Config.
func (s Scope) Specificity() int {
	switch {
	case s.Deployable != "" && s.Environment != "":
		return specificityDeployableEnvironment
	case s.Deployable != "":
		return specificityDeployable
	case s.Environment != "":
		return specificityEnvironment
	default:
		return specificityConfig
	}
}

const (
	specificityConfig = iota
	specificityEnvironment
	specificityDeployable
	specificityDeployableEnvironment
)

// Precedence picks, for each Item, the Override that applies in ns: of the
// overrides whose Scope covers ns, the one with the most specific Scope. Items
// with no covering Override are absent, so they have their Default. Two
// Overrides of one Item never share a Scope, so the winner is unique.
func Precedence(ns Namespace, overrides []Override) map[ItemRef]Override {
	winners := map[ItemRef]Override{}
	for _, o := range overrides {
		if !o.Scope.Covers(ns) {
			continue
		}
		if best, ok := winners[o.Item]; !ok || o.Scope.Specificity() > best.Scope.Specificity() {
			winners[o.Item] = o
		}
	}

	return winners
}
