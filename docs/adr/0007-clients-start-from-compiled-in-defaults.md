---
status: accepted, not yet implemented (#26). Until #26 lands, MimirClient still starts from its MIMIR_* environment variables and still has a bucket prefix Item.
---

# Clients start from compiled-in Defaults only

A client finds its first Snapshot and mimir-server from compiled-in Defaults alone: buckets `mimir-<environment>` (ADR 0006) and mimir-server at `http://mimir-server.mimir-config`. mimir-server publishes to the same fixed prefix. Mimir exists to move configuration out of environment variables, so the client reads none of its own. The only exceptions are the variables that identify the Namespace (`MIMIR_ENVIRONMENT`, `MIMIR_DEPLOYABLE`) and the object-storage settings and credentials, which stay in the runtime environment (ADR 0005). Once a client has read MimirClient's Snapshot, Overrides tune it as they do any other Config. When running from a checkout of the repository, Local Values do the same.

## Considered Options

- **Environment variables as start values** (the previous `MIMIR_*` layer): these let each installation pick its own server URL and bucket prefix, but they were exactly the kind of configuration Mimir is meant to replace.
- **Installation-wide properties defined in mjolnir**: rejected as a leaky abstraction. mjolnir would have to know where Mimir lives.

## Consequences

- An installation must run mimir-server at the Default URL and name its buckets `mimir-<environment>`, or else fork mimir. That suits on-cluster S3-compatible storage (ADR 0002), but not providers with a global bucket namespace such as AWS S3, where `mimir-prod` is likely taken.
- MimirClient has no bucket prefix Item: the server never publishes anywhere else, so an Override could only point clients at empty buckets.
