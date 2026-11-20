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

    // For now every getter returns the Item's Default.
}
```

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

## License

MIT License - see [LICENSE](LICENSE) for details
