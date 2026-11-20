# Items are keyed by name and type, and expire when unregistered

An Item is identified by its name and type together, Registration only ever adds Items, and the server removes an Item that no Deployable has registered for 60 days. This makes mixed-version rollouts safe without coordination: old and new versions each register what they declare, nothing is removed on deploy, and a type change is simply a new Item rather than a conflict to reject. Items that are really gone disappear on their own instead of needing an admin to retire them.

## Considered Options

- **Registration replaces the schema**: removes Items still used by old versions mid-rollout.
- **Registration only adds, admins retire Items by hand, type changes are rejected**: safe, but leaves clean-up to people and turns type changes into failed Registrations.
