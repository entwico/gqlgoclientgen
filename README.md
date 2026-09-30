# gqlgoclientgen

A GraphQL Go client generator that produces type-safe Go clients from GraphQL schemas and operation documents.

## Features

- **Type-Safe Client Generation**: Generate a complete Go client (types, operations, subscriptions) from a schema and your `.gql`/`.graphql` documents
- **Schema Introspection**: Fetch GraphQL schemas from any endpoint, output as JSON or SDL
- **Subscriptions**: Generated subscription methods return Go iterators (`iter.Seq2`) with a strict termination contract over graphql-ws
- **Flexible Configuration**: Use command-line flags or configuration files
- **Authentication Support**: Pass arbitrary HTTP headers for introspection

## Installation

```bash
go install github.com/entwico/gqlgoclientgen/cmd/gqlgoclientgen@latest
```

Or build from source:

```bash
git clone https://github.com/entwico/gqlgoclientgen.git
cd gqlgoclientgen
task build
```

## Usage

### Generating a Client

The `generate` command produces a Go package with a type-safe client for your schema and operations.

```bash
# Generate from a local schema file
gqlgoclientgen generate --schema ./schema.graphql --documents ./queries \
  --pkg client --out ./generated

# Generate via introspection from an endpoint
gqlgoclientgen generate --url https://api.example.com/graphql --documents ./queries \
  --pkg api --out ./api/generated

# With authentication headers
gqlgoclientgen generate --url https://api.example.com/graphql \
  -H "Authorization: Bearer $TOKEN" \
  --documents ./queries --pkg api --out ./api/generated

# Using a config file
gqlgoclientgen generate --config ./config.json --documents ./queries \
  --pkg client --out ./generated
```

Flags:

```
  -c, --config string        Config file path (JSON format with url, headers, and/or schema path)
      --documents string     Path to directory containing .gql or .graphql files with operations (required)
  -H, --header stringArray   HTTP headers for introspection (format: 'Key: Value')
      --out string           Output directory for generated files (default "./generated")
      --pkg string           Name of the generated Go package (default "generated")
      --schema string        Path to GraphQL schema file (SDL or introspection JSON)
  -u, --url string           GraphQL endpoint URL for introspection
```

The schema can be provided as a local file (`--schema`, SDL or introspection JSON), via
introspection (`--url`), or through a config file (`--config`). Command-line flags take
priority over config file values.

### Configuration file

Instead of flags, a YAML (or JSON) config file can describe the whole generation,
including custom scalar mappings:

```yaml
# gqlgoclientgen.yml
schema: ./schema.graphql   # or url: https://api.example.com/graphql
documents: ./queries
package: apiclient
output: ./generated
omitDeprecated: false
scalars:
  BigInt: int64
  Decimal: github.com/shopspring/decimal.Decimal
headers:
  Authorization: Bearer your-token
```

```bash
gqlgoclientgen generate --config gqlgoclientgen.yml
```

Unmapped custom scalars default to a generated string type. Mapped scalars use the
configured Go type everywhere (types, operations, JSON Schemas).

### Keeping generated code fresh

Every generated file's header records the generator version and a checksum of the
inputs. `--check` re-renders in memory and fails when the committed output is stale —
put it in CI:

```bash
gqlgoclientgen generate --config gqlgoclientgen.yml --check
```

### Generated code requirements

The generated client depends on these packages, which the consuming module needs:

```bash
go get github.com/hasura/go-graphql-client@latest   # v0.16.0 or later
go get github.com/cenkalti/backoff/v5@latest
```

### Schema Introspection

The `introspect` command fetches the GraphQL schema from an endpoint and outputs it as JSON (default) or SDL.

```bash
# Introspect a public GraphQL endpoint
gqlgoclientgen introspect --url https://api.example.com/graphql

# Save schema to a file
gqlgoclientgen introspect --url https://api.example.com/graphql -o schema.json

# Output as SDL
gqlgoclientgen introspect --url https://api.example.com/graphql -f sdl -o schema.graphql
```

#### With Authentication

```bash
# Using Bearer token
gqlgoclientgen introspect --url https://api.example.com/graphql \
  -H "Authorization: Bearer your-token-here"

# Multiple headers
gqlgoclientgen introspect --url https://api.example.com/graphql \
  -H "Authorization: Bearer your-token" \
  -H "X-Custom-Header: value"
```

#### Using Configuration File

Create a configuration file `config.json`:

```json
{
  "url": "https://api.example.com/graphql",
  "headers": {
    "Authorization": "Bearer your-token-here"
  }
}
```

Then use it:

```bash
gqlgoclientgen introspect --config config.json -o schema.json
```

Flags:

```
  -c, --config string        Config file path (JSON format with url and headers)
  -f, --format string        Output format: json or sdl (default "json")
  -H, --header stringArray   HTTP headers to include in the request (format: 'Key: Value')
  -o, --output string        Output file path (default: stdout)
  -u, --url string           GraphQL endpoint URL
```

## Subscription semantics

Generated subscription methods return an `iter.Seq2[*Response, error]` iterator with a strict termination contract:

- **Iterator ends without yielding an error** — the server sent a GraphQL `complete`; the stream delivered all data.
- **Iterator yields a non-nil error (terminal)** — the stream was aborted and may be **partial**. Transport/connection failures satisfy `errors.Is(err, ErrSubscriptionTransport)`; GraphQL `error` payloads carry the server's error. Re-subscribing is the caller's responsibility.

Callback behavior: `OnSubscriptionComplete` fires only on a genuine server `complete`; `OnSubscriptionError` fires on transport failures and GraphQL errors. A GraphQL error on one subscription does not affect other subscriptions on the same connection.

Known limitation: the underlying library transparently restarts the session on plain read-EOF/connection-reset (its internal retry path bypasses `OnError`), which re-subscribes active subscriptions without an error signal — a bulk-loading consumer can then observe duplicate items (never silent truncation).

Minimum supported `github.com/hasura/go-graphql-client` version in consumers: **v0.16.0**.

## Development

### Prerequisites

- Go 1.27 or later
- [Task](https://taskfile.dev) (optional, for using the Taskfile)

### Building and testing

```bash
task build   # build binaries via goreleaser
task test    # run tests with race detection and coverage
task lint    # run golangci-lint
```

### Project Structure

```
gqlgoclientgen/
├── cmd/
│   └── gqlgoclientgen/       # CLI application
│       ├── cmd/              # Cobra commands
│       └── main.go           # Entry point
└── pkg/
    ├── clientgen/            # Client code generation
    └── schema/               # Schema introspection and SDL conversion
```

## License

[MIT](LICENSE)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
