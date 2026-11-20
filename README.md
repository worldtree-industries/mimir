# mimir
A config system with live-update capability, written in Go

## Overview

Mimir is a configuration management system that provides:
- **Defaults in code**: Each Config and its Defaults are defined in a config file next to the code that reads it
- **Type Safety**: Strongly-typed configuration values with automatic parsing
- **Thread Safety**: All configuration access is protected by RWMutex
- **Code Generation**: Automatic generation of type-safe config structs from JSON definitions

## Components

### mimir-generate

A code generator that creates type-safe configuration structs from JSON definitions.

See [cmd/mimir-generate/README.md](cmd/mimir-generate/README.md) for detailed documentation.

#### Quick Start

1. Create a config file (e.g., `myapp_config.json`; the generator picks up files ending in `_config.json`):
```json
{
  "namespace": "MyAppConfig",
  "items": [
    {
      "name": "port",
      "description": "Server port",
      "type": "int",
      "value": "8080"
    },
    {
      "name": "debug",
      "description": "Enable debug mode",
      "type": "bool",
      "value": "false"
    }
  ]
}
```

2. Build and run the generator:
```bash
# Build the generator
go build -o mimir-generate ./cmd/mimir-generate

# Or install it to $GOPATH/bin
go install ./cmd/mimir-generate

# Run from project root
./mimir-generate
```

3. Use the generated config in your code:
```go
package main

import (
    "context"
    "myapp"
)

func main() {
    ctx := context.Background()
    cfg := myapp.NewMyAppConfig(ctx)

    port := cfg.GetPort()    // int64
    debug := cfg.GetDebug()  // bool
}
```

Getters start at the Items' Defaults. The Mimir client inside the generated Config then keeps them current: see [Running a Deployable](#running-a-deployable).

## Running a Deployable

A generated Config's constructor starts the Mimir client, which:

1. determines its Namespace: the Config's derived name, the Deployable (`MIMIR_DEPLOYABLE`, or else the last element of the binary's main package path, so `cmd/checkout` is `checkout`), and the Environment (`MIMIR_ENVIRONMENT`). The Environment must be one of mjolnir's (`utils/environment`); if it is missing or unknown the client logs an error, skips Registration and polling, and leaves every Item on its Default. mimir-server creates those Environments at startup, lists them at `GET /v1/environments`, and rejects an Override scoped to any other. Without a usable Namespace it logs an error and runs on Defaults only.
2. reads its Snapshot from object storage at a location it builds itself: bucket `<prefix>-<environment>` (prefix `mimir` by default), object `<config>/<deployable>.json`. A missing Snapshot means every Item uses its Default.
3. registers the Config's Items with `mimir-server` in the background, and again every 24 hours so what it still declares never expires. A failed Registration is logged, retried after 5 minutes, and never affects the values.
4. polls the Snapshot every poll interval (30 seconds by default) until the constructor's context is done, with conditional GETs, so an unchanged Snapshot costs almost nothing and changes no values.

### The client's own settings: `MimirClient`

The client's settings are a Config of their own, `MimirClient` (named `github.com/worldtree-industries/mimir/client.MimirClient`), which every client registers and polls alongside its Config, in the same Deployable and Environment. Tune them with Overrides like any other Config. Each starts from an environment variable if it's set, else its Default:

| Item (all `string`) | Environment variable | Default |
| --- | --- | --- |
| `poll_interval` (a Go duration, at least `1s`) | `MIMIR_POLL_INTERVAL` | `30s` |
| `server_url` | `MIMIR_SERVER_URL` | `http://mimir-server:8080` |
| `object_store_endpoint` (empty: the standard `AWS_ENDPOINT_URL_S3` / `AWS_ENDPOINT_URL`) | `MIMIR_OBJECT_STORE_ENDPOINT` | empty |
| `bucket_prefix` | `MIMIR_BUCKET_PREFIX` | `mimir` |

A new poll interval applies from the next wait, and a new server URL from the next Registration. A new endpoint or bucket prefix is adopted only once the client has read both its Snapshots (`MimirClient`'s and its Config's) there; until then it logs the failure, keeps polling the old location and tries again at each poll, so a bad Override can't strand it where it can't read the fix. A restarted client begins again from its environment variables or Defaults, and adopts an Override only once it reads it from there. A setting the client can't use (an environment variable or an Override) is logged and the last good one kept. `MIMIR_ENVIRONMENT`, `MIMIR_DEPLOYABLE` and object storage credentials always come from the environment, never from Mimir.

Values are never read from `mimir-server`, so a program that starts while it is down still gets its real values. Nothing object storage or a bad value does can break a running program:

- If object storage can't be reached (network errors, 5xx), the client logs the failure and keeps its last good values. At a cold start that means Defaults, and the next polls pick up the real values once it's reachable.
- A value that doesn't parse as its Item's type is logged and reported, and the Item keeps its last good value. The rest of the Snapshot still applies.
- Getters are safe to call from any goroutine while values update.

To react to changes and errors, pass `client.WithEventHandler` to the constructor. It's told about each Item whose value changes (`EventValueChanged`), each rejected value (`EventValueRejected`), each attempt to switch Snapshot location (`EventLocationSwitch`), and each poll and Registration, with `Err` set when one failed. Changes to `MimirClient`'s own Items are logged rather than reported.

## mimir-server

`mimir-server` stores Registrations and Overrides in Postgres (`DATABASE_URL`; it migrates the schema at startup) and publishes each Namespace's Snapshot to object storage: after the Namespace's first Registration, and after every change to an Override whose Scope covers it. Its API, for the private network only (there is no authentication yet):

- `POST /v1/registrations`: Registration, sent by clients.
- `POST /v1/overrides`: create an Override, for example a Config-wide one:
  ```json
  {"scope": {"config": "github.com/acme/kafka.KafkaConfig"}, "item": {"name": "batch_size", "type": "int"}, "value": "500"}
  ```
  The Item must be registered. Errors are RFC 9457 problem documents.
- `GET /v1/configs`, `GET /v1/items`, `GET /v1/namespaces`: what Registration taught the server, with when each Item and Namespace was first and last registered. `items` and `namespaces` take an optional `config` query parameter.

Registration only adds: a version that drops an Item leaves it and its Overrides in place. Once an hour the server expires what no Deployable has registered for 60 days. An Item goes with its Overrides (each recorded in the History as `expired`, with Caller `mimir-server:expiry`) and the Snapshots that held them are republished; a Namespace goes with its Snapshot in object storage.

  Narrow the Scope with `"deployable"`, `"environment"` or both. When several Overrides apply to an Item in a Namespace, the most specific Scope wins: Deployable and Environment, then Deployable, then Environment, then Config-wide (ADR 0003).
- `PUT /v1/overrides/{id}`: set an Override's value (`{"value": "600"}`). `DELETE /v1/overrides/{id}` removes it; the next most specific Override, or the Default, takes effect.
- `GET /v1/overrides?config=...`: list a Config's Overrides. Add `deployable=` and/or `environment=` to match that Scope field exactly (empty means not narrowed); an absent parameter matches anything.
- `GET /v1/explain?config=...&deployable=...&environment=...`: for a Namespace, every Item's effective value and its source: the winning Override, or the Default.

## Object storage

Snapshots live in S3-API object storage, such as Rook/Ceph RGW. `mimir-server` and every client read the standard AWS environment variables (Mimir never holds credentials, ADR 0005):

| Variable | Meaning |
| --- | --- |
| `AWS_ENDPOINT_URL_S3` (or `AWS_ENDPOINT_URL`) | The endpoint, e.g. `http://rook-ceph-rgw-store.rook-ceph.svc`. Leave unset for AWS itself. |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | Credentials. |
| `AWS_REGION` | Region; defaults to `us-east-1`, which Ceph ignores. |

Addressing is path-style, so no wildcard DNS is needed. Without any of these, the server keeps Snapshots in memory only (nothing a client can read) and clients run on Defaults.

Buckets are named `<prefix>-<environment>`; the prefix is `mimir`, so `mimir-dev`, `mimir-prod`. Create them yourself: Mimir doesn't. The server needs read, write and delete on them, and clients read-only.

## Config file format

A config file is JSON with one Config:

- `namespace` (required): the Go type name of the generated Config, e.g. `MyAppConfig`.
- `display_name` (optional): a short human-friendly name for the API and future UI.
- `items`: the Config's Items, each with `name`, `description`, `type` and `value` (its Default, as a string; collections are JSON arrays in a string).

Item names must be unique within the file: generation fails, naming the Item and writing no output, if two Items share a name.

### Derived Config name

Each Config gets a globally unique name derived from its Go package import path and type, `<import path>.<type name>`, for example `github.com/acme/kafka.KafkaConfig`. Two libraries that both call their Config "kafka" therefore never collide. The generated code exposes it as the constant `<Type>Name` (for example `KafkaConfigName`), and the display name as `<Type>DisplayName`, which is empty when the config file sets none.

## Supported Types

- `string` - String values
- `int` - 64-bit integers
- `bool` - Boolean values
- `float` - 64-bit floating point
- `string_array` - Array of strings
- `int_array` - Array of integers
- `bool_array` - Array of booleans
- `float_array` - Array of floats
- `string_set` - Set of unique strings (requires `github.com/dfryer1193/mjolnir/utils/set`)
- `int_set` - Set of unique integers
- `bool_set` - Set of unique booleans
- `float_set` - Set of unique floats

## Testing

`make test` runs everything. The system tests in `internal/systemtest` run `mimir-server` in-process against Postgres: set `DATABASE_URL` to a server where the role may create databases. Each test makes its own database and drops it afterwards. Without `DATABASE_URL` those tests are skipped.

The object storage contract also runs against a real S3-compatible endpoint when `MIMIR_TEST_S3_ENDPOINT` and `MIMIR_TEST_S3_BUCKET` (an existing bucket) are set, with credentials in the standard AWS variables; otherwise that test is skipped.

## License

MIT License - see [LICENSE](LICENSE) for details
