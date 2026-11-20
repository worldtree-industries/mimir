# Mimir

Mimir manages typed configuration for Deployables: each Config is defined in code, and its values can be tuned per Deployable and Environment without a redeploy.

## Language

### Definitions

**Config**:
A named, typed set of Items, defined in a config file in the code that reads it. A library may define a Config that several Deployables use.
_Avoid_: Schema, settings, config file (that's where a Config is defined)

**Item**:
One value in a Config, identified by its name and type together, with a description and a Default. Changing an Item's type makes it a different Item.
_Avoid_: Key, field, setting, parameter

**Default**:
The value an Item has when no Override applies, set in the Config's definition.
_Avoid_: Fallback, initial value

**Registration**:
A running Deployable telling Mimir which Items its Configs declare. Registration only adds; an Item that goes unregistered for long enough expires.
_Avoid_: Sync, publish, schema push

### Where values apply

**Deployable**:
A binary that's deployed into an Environment and reads one or more Configs.
_Avoid_: Service, app, consumer

**Environment**:
A deployment stage, such as dev, staging or prod, from the fixed set defined in mjolnir.
_Avoid_: Stage, tier, cluster

**Namespace**:
One concrete use of a Config: the Config, the Deployable using it, and the Environment it runs in. The same library Config used by two Deployables, or in two Environments, is distinct Namespaces with independent values.
_Avoid_: Config (for this narrower meaning), app, service

**Scope**:
The set of Namespaces an Override applies to: always one Config, optionally narrowed to one Deployable, one Environment, or both.
_Avoid_: Target, selector, level

### Values

**Override**:
A value set through Mimir for one Item within a Scope, replacing the Item's Default in every Namespace the Scope covers.
_Avoid_: Value (on its own), setting, patch

**Precedence**:
The rule that picks one Override when several apply to the same Item in a Namespace: the most specific Scope wins.
_Avoid_: Priority, ordering, resolution order

**History**:
The append-only record of every change to an Override: each create, update, delete and expiry, with the old and new values, when it happened and its Caller.
_Avoid_: Audit log, changelog, events

**Caller**:
Whoever made a change recorded in the History: the person or system named by the request, or Mimir itself for an expiry.
_Avoid_: User, actor, author

**Catalogue**:
What Mimir knows has been registered: every Config, its Items and its Namespaces, with when each was last registered.
_Avoid_: Registry, inventory, schema

**Snapshot**:
The published values for one Namespace, with Precedence already applied, which its clients read to learn their values.
_Avoid_: Cache, export, dump
