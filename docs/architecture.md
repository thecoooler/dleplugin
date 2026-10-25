# Architecture

`dleplugin` is a single statically linked Go binary: CLI tools for developing
DataLife Engine (DLE) plugins. Modules are compiled in, see below.

A plugin project is a source tree identified by `dleplugin.yml`; commands can
be invoked from any directory inside it. A test site is a copy of the installed
DLE trial with the plugin and its dependencies connected. Each test run gets
a fresh site copy and database.

## Layout

```text
cmd/dleplugin/main.go      entry point: builds the module list and runs the app
cmd/dleplugin-gui/main.go  desktop app entry point: the same module list, run
                           by the GUI
internal/app/              Runtime (shared services), Module contract, root
                           command, module registration, error rendering
internal/ui/               terminal output (✔/✖/⚠), colors, prompts, confirm
internal/config/           global config ~/.config/dleplugin/config.yml:
                           core section + typed per-module sections
internal/process/          external command runner (capture/stream, timing,
                           verbose logging, formatted failures)
internal/paths/            FindAncestorWith, CacheDir, Resolve, path helpers
internal/manifest/         dleplugin.yml model (types.go, ordered.go) and its
                           YAML decoding/normalization
internal/plugin/           plugin project: locate root, load manifest, merge
                           extra configs, Info() with generated extras,
                           source files, init, NormalizeFiles
internal/build/            XML generation and dist/ writer (README.html,
                           split zip)
internal/dlexml/           reading DLE plugin XML (file operations, tags)
internal/convert/          plugin XML -> dleplugin.yml text
internal/diff/             XML vs XML diff via convert + system `diff`
internal/deploysites/      deployment site files, scope resolution and saved
                           passwords (deploy module and GUI)
internal/gui/              desktop app (MyGo native UI): projects, forms,
                           console; runs commands through app.Execute
internal/dleadmin/         HTTP client for a DLE admin panel: login, plugin
                           list, plugin upload (dleadmintest: fake server)
internal/dleinstall/       HTTP client of the standard DLE 11.0+ web
                           installer (used by test and hosts)
internal/dockerimg/        tool images built from embedded Dockerfiles
internal/goldentest/       shared golden fixture update helpers
internal/modules/initcmd/  the init module: init
internal/modules/buildcmd/ the build module: build
internal/modules/convertcmd/
                           the convert module: convert
internal/modules/diffcmd/  the diff module: diff
internal/modules/moduletest/
                           shared command-test helpers
internal/modules/deploy/   the deploy module: deploy sites, deploy upload
internal/modules/static/   the static module: static
internal/modules/static/internal/static/
                           PHPStan in Docker (embedded Dockerfile and
                           bootstrap.php), dlestatic.yml, include paths
internal/modules/test/     the test module: test
internal/modules/test/internal/dle/
                           connects a plugin to a local DLE copy: file
                           symlinks, plugin registry in MySQL, cache clearing
internal/modules/test/internal/dletest/
                           plugin tests against the DLE trial: download,
                           install via dleinstall (MariaDB in Docker), run site,
                           bootstrap.php with the xfields shim, Pest command
internal/modules/check/    the check module: check
internal/modules/check/internal/check/
                           file patches vs DLE directories: whitespace- and
                           case-insensitive search, missing files, duplicates
internal/modules/hosts/    the hosts module: hosts up/down/compose/list/add/
                           remove/protect/path/port/versions/config
internal/modules/hosts/internal/stack/
                           Docker stack: embedded files, .env, docker compose,
                           temporary installer container
internal/modules/hosts/internal/host/
                           hosts registry, add/remove, vhosts, protect form
                           and passwords, HTTP port
internal/modules/hosts/internal/dist/
                           clean DLE distributions: inspect, resolve, extract
internal/modules/hosts/internal/hostconfig/
                           hosts config ~/.config/dlehost/config.yml
internal/modules/skills/   the skills module: skills list/install/uninstall
internal/modules/skills/internal/bundle/
                           embedded skill trees and managed copy lifecycle
testdata/golden/           golden fixtures (see testdata/README.md)
```

Dependency direction: `internal/modules/<name>` command packages -> shared
services (`plugin`, `build`, `convert`, ...) or module-private services ->
`manifest`, `paths`. Reusable services live in shared `internal/<pkg>` packages;
services owned by one module live in `internal/modules/<name>/internal/<pkg>`.
Go's `internal` visibility keeps those private services within their module.
Shared packages must not import `internal/modules/*`.

Non-command packages must not import `app`, `ui` or cobra; services return
errors and data. Only command packages talk to the terminal.
`internal/app/arch_test.go` enforces both import boundaries on production
imports; `_test.go` imports are excluded so service tests may use command-test
helpers. It treats `app`, `cmd/*`, and direct `internal/modules/<name>` packages
(including `moduletest`) as command packages; nested module packages are services.

## Desktop app

`cmd/dleplugin-gui` builds a second binary, `dleplugin-gui`, separately from
the CLI: `cmd/dleplugin` must not depend on `internal/gui` or MyGo, which
`internal/app/arch_test.go` checks. Only `internal/gui` and
`cmd/dleplugin-gui` import MyGo.

`internal/gui` is a frontend like the command packages: it may import `app`
(not cobra or `ui`) and never imports module packages, which its entry point
hands it. Actions run the CLI's commands in process through `app.Execute`
with a fresh `Runtime` whose working directory is the project, the arguments
the CLI documents, non-terminal input (so commands never prompt) and output
captured into the console; one command runs at a time and can be canceled.
A deploy to several sites runs one `deploy upload` per site in turn: a failed
site does not stop the rest, and canceling stops them all.
Passwords go through `--password-stdin`. Reading data the views show (the
manifest, `dist/`, saved sites) and creating a project use the shared
services directly (`plugin`, `deploysites`). The view runs on the main
thread; goroutines post their changes, which the next frame applies. Views
are tested without a window with MyGo's `ui.Tester`.

## Modules

A module is a compiled-in feature that contributes top-level commands.

```go
// internal/app
type Module interface {
	Name() string                           // "init", "build", "deploy", ...
	Commands(rt *Runtime) []*cobra.Command  // top-level commands
}
```

`cmd/dleplugin/main.go` lists modules explicitly:

```go
func main() {
	app.Main(modules()...)
}

func modules() []app.Module {
	return []app.Module{initcmd.Module(), buildcmd.Module(), convertcmd.Module(), diffcmd.Module(), deploy.Module(), static.Module(), test.Module(), check.Module(), hosts.Module(), skills.Module()}
}
```

Registration rules:

- modules register in the supplied order; no module is required to be first;
  `initcmd.Module()` has the module name `init`;
- duplicate module names: every duplicate is rejected and reported;
- a module whose command name collides with an already registered command or
  the reserved `help` command is rejected as a whole (none of its commands are
  added); the CLI keeps working;
- registration results are available via `rt.Modules()` / `rt.ModuleErrors()`;
  module tests must assert that a module registers without errors;
- top-level commands set `GroupID` to `app.GroupProject`, `app.GroupCheck`,
  `app.GroupRelease`, `app.GroupHosts` or `app.GroupAgents`; the root help lists these groups in that order and the
  commands of a group in registration order. Empty groups are omitted;
  commands without a known group appear under "Additional Commands".

### Runtime

`*app.Runtime` is the dependency container handed to modules:

| Field / method       | Purpose                                                    |
| -------------------- | ---------------------------------------------------------- |
| `UI *ui.UI`          | stdout/stderr/stdin, colors, verbose flag, prompts         |
| `Config *config.Store` | global config, lazy; core + typed module sections        |
| `Exec *process.Runner` | external commands (git, docker, composer, php, mysql)    |
| `WorkDir() string`   | invocation directory (overridable in tests)                |
| `Version string`     | binary version (ldflags)                                   |

Command helpers: `app.RequireConfig(rt, fn)` (a missing config file is created
and reported), `app.OptionalConfig(rt, fn)`. These load the global `core` config
section, available through `rt.CoreConfig()`.

### Shared services for modules

| Need                                                 | Provided by                                   |
| ---------------------------------------------------- | --------------------------------------------- |
| typed config sections with defaults + required keys | `config.Store.Section(name, &cfg)`             |
| own config files (deploy: `dleplugin-deploy.yaml`)     | `config.NewStore`, `config.SaveYAMLFile`   |
| plugin root lookup from any subdirectory            | `plugin.FindRoot`, `plugin.FromDir`            |
| manifest, generated XML (check)                     | `plugin.Plugin.Info()`, `build.XML`            |
| reading plugin XML / zip (check, deploy)            | `dlexml`                                       |
| running the DLE web installer (test, hosts)         | `dleinstall.Run`                               |
| running docker/composer/php/mysql CLIs              | `process.Runner`                               |
| cache directories, shared composer cache           | `paths.CacheDir`, `paths.ComposerCacheDir`     |
| relative file args (test, static)                   | `plugin.NormalizeFiles`                        |
| interactive prompts                                 | `ui.Prompt`, `ui.Confirm`, `ui.PromptSecret`,  |
|                                                     | `ui.Interactive`                               |
| cancellation (Ctrl-C) for long runs                 | `cmd.Context()` from `signal.NotifyContext`    |
| embedded assets (stubs, test plugin)                | module-local `embed.FS`                        |

The test module's private `dle.Host` connects plugins to a DLE copy;
`dletest` runs the trial-based tests. Other modules use their own private
services for PHPStan and patch checking; deployment sites are the shared
`deploysites`, which the GUI reads too.

## Behavior notes

- `build` does not require the global config or a DLE installation: building
  is a pure operation. `<version>` in the XML comes from the manifest
  `version` field; `build --version` and `deploy upload --version` override it.
  `<upgradeurl>` behaves the same from `upgrade_url`/`--upgrade-url`, but only
  an https link validates.
- Modules are compiled in; there is no runtime discovery.
- `skills` embeds both agent skills via `go:embed`, using the CLI runtime version.
  Its private service copies complete sibling directories, records SHA-256 hashes
  and ownership, and preflights both before updates/removal. See
  [agent skills](agent-skills.md) for paths, conflict rules and source provenance.
- `deploy` is a command group: `deploy sites` manages host-keyed connections
  and `deploy upload [HOST]` builds/uploads to one site. Global sites live
  next to the global config in `dleplugin-deploy.yaml`; project sites live
  next to `dleplugin.yml` and replace global records as a whole. Site writes
  check the admin login before saving. Passwords are kept out of the file: a
  site may reference an environment variable, and passwords checked by
  `sites add/edit` (or typed for a saved site on upload) go to the OS
  credential store via go-keyring. `DLEPLUGIN_CREDENTIAL_STORE` (`auto`,
  `keyring`, `file`) lets them fall back to, or go only to, a `0600`
  `credentials.yaml` next to the global config. TLS is verified unless `--insecure`; the
  AJAX URL is resolved next to the admin script: `engine/ajax/controller.php`,
  or `index.php?controller=ajax` (DLE 19+) when the first answers 403/404.
  Admin requests follow redirects only within their original origin.
- `static` runs PHPStan. Tool images use pinned base image digests, Composer
  lock files and a fixed PCOV version; their tags hash the full embedded build
  context. Image build progress goes to stderr so `--json`/`--count` output
  stays parseable. `-t/--tool-verbose` is the same as `-v`. `ignoreErrors` are
  Go regular expressions (RE2: no lookarounds). Findings or an analyser
  failure exit with code 1, without an extra error line. `--php-version`
  writes PHPStan's `phpVersion` instead of rebuilding the image for another
  PHP.
- `test` runs against the downloaded DLE trial, always the current release,
  with a fresh MariaDB database per run. Pest runs in Docker with `/dle` as
  the project root, so snapshots use Pest's standard layout
  `tests/.pest/snapshots/<test path>/`. The bootstrap emulates `xfieldsload()`
  and `xfieldsdataload()` (removed in DLE 19). Dependencies come from the
  `dependencies` field of `dleplugin.yml` (plugin names, written to
  `<needplugin>`). MariaDB runs with `sql_mode=NO_ENGINE_SUBSTITUTION`: DLE 21
  tables have NOT NULL columns without defaults that plugin registration does
  not fill.
- `check` takes the DLE directories to check as arguments (one or more; each
  is a DLE root with `engine/inc/include/init.php`). A ready plugin file is
  passed with `-f/--file` (also `.zip`). Every directory is reported as
  `DLE <version> (<path>): OK|ERROR` with its problems below it
  (`FILE_NOT_FOUND` with the path relative to the DLE root, duplicates as
  `[WARNING] DUPLICATE ... (N matches)`); a whitespace-only search is reported
  as `EMPTY_SEARCH`, and operations on a file created by the plugin are not
  checked. Exits with code 1 when a patch does not apply.
- Usage errors and command errors exit with code 1 and print
  `✖ Error: <message>`; `-v/--verbose` additionally prints the wrapped error
  chain.
- `convert` rejects unsafe paths before writing and stages extracted sources
  in a fresh directory. YAML metadata and keys are quoted when needed.
- Manifest slugs must be single filename components, and mount/alias/patch
  paths must stay within the DLE root. Local writes use `os.Root` to prevent
  symlink traversal outside their destination.
- Names/descriptions containing SQL special characters are encoded safely.
- Release builds require patched Go 1.27.3+ and disable CGO by default.
  The module path is `github.com/thecoooler/dleplugin`.
- `make golden` updates generated expectations from committed inputs through
  Go tests; see `testdata/README.md`.

Generated XML/YAML and zip layout are covered by golden comparisons.
Process services receive output sinks and a completion callback; terminal
formatting stays in `app`/`ui` rather than the service package.
