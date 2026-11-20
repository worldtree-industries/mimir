# Clients poll snapshots in object storage

Mimir's server publishes each change to Overrides as snapshots in object storage, and clients poll those snapshots with conditional GETs (about every 30 seconds) instead of talking to the server. This keeps the server and Postgres off the read path: a Deployable that starts while the server is down still gets its real values, not just Defaults. Object storage is reached through its S3 API first (Rook/Ceph RGW), with Google Cloud Storage to follow.

## Considered Options

- **Clients poll the server**: simpler, but a pod started during a server outage runs on Defaults.
- **Push over SSE, WebSockets or streams**: lower latency than needed, long-lived connection state, and fan-out across server replicas.
- **Kubernetes ConfigMaps mounted as files**: no runtime dependency, but ties Mimir to Kubernetes and needs write access to every target namespace.
