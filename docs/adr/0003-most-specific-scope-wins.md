# The most specific Scope wins, Deployable before Environment

When several Overrides apply to an Item in a Namespace, the most specific Scope wins, in this fixed order: Deployable and Environment, then Deployable, then Environment, then the whole Config. So an Override for "foo in any Environment" beats one for "any Deployable in prod": whoever tuned foo meant foo. The server applies this rule when it publishes each Namespace's Snapshot; clients never apply it, so the rule (and new, narrower Scopes such as modules) can change without upgrading clients.

## Considered Options

- **Environment before Deployable**: lets prod-wide guardrails beat per-Deployable tuning; just as defensible, which is why this is recorded.
- **Explicit priorities on each Override**: flexible, but makes "why does foo have this value?" hard to answer.
- **Clients apply Precedence to a per-Config Snapshot**: lets a never-before-seen Deployable pick up broad Overrides on its first start, but freezes the rule into every client version.
