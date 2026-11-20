# Snapshot locations are constructed from the Namespace

Each Namespace's Snapshot lives at a location the client builds from its own Namespace, without asking the server: bucket `<prefix>-<environment>` (prefix `mimir` by default), object `<config>/<deployable>.json`, where the Config name is derived from its Go package path and type. A bucket per Environment keeps the layout identical across Environments and lets each Environment's credentials read only its own bucket. A missing object means there are no Overrides, so the client uses Defaults; an unreachable bucket means the client keeps its last good values.

## Consequences

- The server publishes one Snapshot per Namespace it learned about through Registration, so a Deployable's very first start runs on Defaults until its Snapshot is published (within about one poll interval of its first Registration, or until the server is back if it's down).
- A Namespace's Snapshot is deleted when the Namespace expires after 60 days without Registration.
