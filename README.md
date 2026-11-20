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

1. determines its Namespace: the Config's derived name, the Deployable (`MIMIR_DEPLOYABLE`, or else the last element of the binary's main package path, so `cmd/checkout` is `checkout`), and the Environment (`MIMIR_ENVIRONMENT`). Without an Environment it logs an error and runs on Defaults only.
2. reads its Snapshot from object storage at a location it builds itself: bucket `mimir-<environment>`, object `<config>/<deployable>.json`. A missing Snapshot means every Item uses its Default.
3. registers the Config's Items with `mimir-server` (`MIMIR_SERVER_URL`, default `http://mimir-server:8080`) in the background. A failed Registration is logged and never affects the values.
4. polls the Snapshot every 30 seconds until the constructor's context is done.

Values are never read from `mimir-server`, so a program that starts while it is down still gets its real values.

## mimir-server

`mimir-server` stores Registrations and Overrides in Postgres (`DATABASE_URL`; it migrates the schema at startup) and publishes each Namespace's Snapshot to object storage: after the Namespace's first Registration, and after every Override of its Config. Its API, for the private network only (there is no authentication yet):

- `POST /v1/registrations`: Registration, sent by clients.
- `POST /v1/overrides`: create an Override, for example a Config-wide one:
  ```json
  {"scope": {"config": "github.com/acme/kafka.KafkaConfig"}, "item": {"name": "batch_size", "type": "int"}, "value": "500"}
  ```
  The Item must be registered. Errors are RFC 9457 problem documents.

Object storage over the S3 API is not wired up yet, so for now the server keeps Snapshots in memory.

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

## License

MIT License - see [LICENSE](LICENSE) for details
