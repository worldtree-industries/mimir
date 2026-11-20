# Mimir never holds secrets

Mimir has no secret Item type, and credentials (including the client's own object storage credentials) stay in the runtime environment, such as Kubernetes Secrets. Every client in an Environment can read every Snapshot in it, and Overrides and their history are shown in plain text through the API and UI, so storing secrets would make Mimir an unsafe secret store.
