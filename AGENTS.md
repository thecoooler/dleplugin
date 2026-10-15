# Repository Guidelines

## Project Structure & Module Organization

This is a Go module (`github.com/thecoooler/dleplugin`) with a single binary in `cmd/dleplugin`. All
code is under `internal/`. Shared packages are `app` (runtime, module registration, root command),
`ui`, `config`, `process`, `paths` (including `Resolve`), `manifest`, `plugin`,
`build`, `convert`, `diff`, `dlexml`, `dleadmin` (with `dleadmintest`),
`dockerimg`, and `goldentest`. Command modules live in
`internal/modules/<name>/` (`initcmd`, `buildcmd`, `convertcmd`, `diffcmd`, `deploy`,
`static`, `test`, `check`); `initcmd` provides the module and command named `init`.
`internal/modules/moduletest` holds shared command-test helpers. Module-private
services live in `internal/modules/<name>/internal/<pkg>`: test owns `dletest`
and `dle`, deploy owns `deploysites`, static owns `static` (with `assets/`),
and check owns `check`. Shared packages must not import module packages.
Services never import `app`, `ui` or cobra; only command packages talk to the
terminal. `internal/app/arch_test.go` enforces these import rules for production
code. Fixtures are in `testdata/golden/`. Design notes are in `docs/architecture.md`.

## Build, Test, and Development Commands

- `make build` builds `bin/dleplugin` with the version set via ldflags.
- `make test` runs `go test ./...`.
- `make lint` checks `gofmt` and runs `go vet`.
- `make pipeline` runs lint and tests.
- `make install` installs the binary with `go install`.
- `make golden` updates generated expectations from committed inputs using
  the Go tests; review the fixture diff before committing.

## Coding Style & Naming Conventions

Format with `gofmt` and keep `go vet` clean. Prefer small packages with
explicit errors (wrap with `%w`) and no terminal output in services. Keep
command names stable and hyphenated. Tests are table-driven
where it helps and use the standard `testing` package. Generated artifacts (XML,
YAML, zip) must match `testdata/golden` byte for byte.

## Adding a Module

1. Create `internal/modules/<name>/` implementing `app.Module`
   (`Name() string`, `Commands(rt *app.Runtime) []*cobra.Command`).
2. Put module-private services in `internal/modules/<name>/internal/<pkg>`.
   Keep reusable services in shared `internal/<pkg>` packages, which must not
   import module packages. Non-command packages must not import `app`, `ui`
   or cobra; `internal/app/arch_test.go` enforces both import rules.
3. Use `rt.UI`, `rt.Config`, `rt.Exec`, `rt.WorkDir()`; wrap config-dependent
   commands with `app.RequireConfig` or `app.OptionalConfig`. Set `GroupID`
   of top-level commands to an `app.Group*` help group.
4. Add it to the `modules()` list in `cmd/dleplugin/main.go`, alongside
   `initcmd.Module()`, `buildcmd.Module()`, `convertcmd.Module()`, `diffcmd.Module()`,
   `deploy.Module()`, `static.Module()`, `test.Module()`, and `check.Module()`.
   The entry point calls `app.Main(modules()...)`; no module must register first.
5. Add tests with `app.NewRuntime` and buffers or the shared helpers in
   `internal/modules/moduletest`; add `--help` checks.

## Commit & Pull Request Guidelines

Use Conventional Commit subjects, for example `feat(core): add convert` or
`fix(build): keep zip order`. Pull requests should describe the intent, list
verification commands that passed, and call out CLI behavior changes.

## Agent-Specific Instructions

Keep edits surgical and do not reformat untouched files. Keep public commands
stable as listed in `docs/commands.md`.
