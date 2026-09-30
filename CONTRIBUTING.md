# Contributing

Thanks for your interest in contributing!

## Development setup

Prerequisites:

- Go 1.27 or later
- [Task](https://taskfile.dev) (optional, for the Taskfile targets)
- [golangci-lint](https://golangci-lint.run) v2

## Workflow

1. Fork and clone the repository.
2. Make your change, following the existing code style.
3. Run the checks before opening a pull request:

```bash
go build ./...
go vet ./...
task test             # tests with race detection and coverage
task lint             # golangci-lint
task test-generated   # generate a client from the fixture and compile it
task test-integration # run the generated client against a fake graphql-ws server
```

The full generator output is locked in by golden files. After an intentional
change to the generated code, refresh them and review the diff:

```bash
go test ./pkg/clientgen -run TestGenerate_MatchesGolden -update
git diff pkg/clientgen/testdata/determinism/golden
```

## Guidelines

- Generated output must be deterministic: two runs over the same input must be
  byte-identical. `TestGenerate_OutputIsDeterministic` guards this.
- Generated output must compile. If your change affects the emitted code, run
  `task test-generated` and add or extend a test case under
  `pkg/clientgen/testdata/`.
- Prefer returning errors over panics in library code.
- Keep commits focused; use conventional commit messages (`feat:`, `fix:`,
  `docs:`, ...).

## Reporting issues

Please include the schema and operation documents that reproduce the problem
(or a minimized version) plus the exact command you ran.
