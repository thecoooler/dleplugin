# Contributing

Use Go 1.27.3 or newer and keep changes focused. Build from a clone with
`make build`; verify changes with `make pipeline` and `go test -race ./...`.
Docker is required only for plugin PHP analysis/tests and tool-image checks.
No PHP installation is needed for the Go test suite.

Follow [AGENTS.md](AGENTS.md) and [architecture.md](docs/architecture.md).
Services return errors/data and use injected output sinks; CLI/UI packages
format terminal output. Wrap underlying errors with `%w`, format changed Go
files with gofmt, and keep public command names stable.

For generated output changes, follow [testdata/README.md](testdata/README.md).
Use `make golden` only after deciding the intended output; review its diff and
retain byte-exact XML/YAML checks. Tool versions and image cache invalidation
are described in [tool-images.md](docs/tool-images.md).

Open an issue for substantial behavior changes. Submit focused pull requests
with Conventional Commit titles, the reason for the change, CLI behavior
changes, and verification commands. Never commit `.env` or saved deployment
site files; use fictional values in examples. See [SECURITY.md](SECURITY.md)
for vulnerability reports.
