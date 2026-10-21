# CLI commands and tool configuration

Use `dleplugin --help` and `<command> --help` to check the installed version. This reference was checked against the Go CLI source at commit `a738aa05966a6d037a46e408dbe110745bde8652` (2026-10-21), especially `docs/commands.md`, `README.md`, `internal/modules/`, and `internal/config/`. Older Python-era configuration features are not necessarily supported.

## Commands and flags

Global: `-v, --verbose`. Root `dleplugin --version` prints the CLI version (distinct from the build/upload version option). `-h, --help` is available on commands. There is no global `--config` flag.

| Command | Arguments and command-specific flags |
|---|---|
| `init [DIR]` | `--slug`, `--name`, `--description`, `--namespace` |
| `build` | `--version V`, `--upgrade-url U` |
| `convert XML_PATH [PLUGIN_PATH]` | `--namespace` |
| `diff SOURCE1 SOURCE2` | no command-specific flags |
| `static [FILES...]` | `--json`, `--count`, `-t/--tool-verbose`, `--php-version`, repeatable `--dependency-root` |
| `test [FILES...]` | `-f/--filter`, `-u/--update-snapshots`, `--coverage`, `--coverage-min`, `--coverage-html`, `--refresh-dle` |
| `check DLE_DIR...` | `-f/--file` |
| `deploy upload [HOST]` | `-f/--file`, `--version`, `--upgrade-url`, `-u/--login`, `--admin-path`, `--password-env`, `--password-stdin`, `--new`, `--dry-run`, `--insecure` |
| `deploy sites list` | no command-specific flags |
| `deploy sites show HOST` | no command-specific flags |
| `deploy sites add [HOST]`, `edit [HOST]` | `--global`, `-u/--login`, `--admin-path`, `--password-env`, `--password-stdin`, `--insecure` |
| `deploy sites remove HOST` | `--global` |
| `skills list` | lists both bundled skills and CLI version |
| `skills install` | `--agent codex\|claude` (required), `--global`, `--force` |
| `skills uninstall` | `--agent codex\|claude` (required), `--global` |

`deploy upload --file` is mutually exclusive with `--version` and `--upgrade-url`.

`init` writes only the manifest, creating DIR if needed; create `src/` yourself. Supply metadata flags to avoid prompts in automation. Namespace defaults to slug when it is a valid PHP namespace; a hyphenated slug needs an explicit namespace. Existing manifests are not overwritten.

```sh
dleplugin init Demo --slug demo --name 'Demo Plugin' --description 'Example' --namespace 'Acme\Demo'
```

`convert` extracts XML create operations into `src/` and writes mount/alias metadata. It refuses an existing `dleplugin.yml` or `src/` destination. `--namespace` controls the mount; otherwise file paths are used for inference.

`diff` accepts XML, ZIP (an XML entry named after the archive), or project directories/subdirectories. Projects build in memory with split mode respected. Only XML is compared, excluding version/slug/mount metadata and separately packaged source files. Differences do not fail the command. Requires the system `diff` tool.

## Bundled agent skills

This CLI ships `dleplugin` and `dle-admin-ui` together. `skills install` copies both
into the current directory's `.agents/skills/` for Codex or `.claude/skills/` for
Claude Code. `--global` uses those paths under the user's home. Repeat install
after a CLI update. No network or configuration is required. Modified/unmanaged
copies require `install --force`, which discards their contents. Uninstall only
removes unchanged copies managed by the CLI; it has no force flag. `init` does not
install skills implicitly.

```sh
dleplugin skills list
dleplugin skills install --agent codex
dleplugin skills install --agent claude --global
dleplugin skills uninstall --agent claude --global
```

## Global versus plugin configuration

Global config is `~/.config/dleplugin/config.yml`. Missing config is allowed by static/test; `host_dir` is not required by the current public commands. Do not confuse this file with `dleplugin.yml`.

```yaml
dependency_roots:
  - /path/to/Modules
static:
  dependencies:
    DLEPlugin: /path/to/Modules/DLEPlugin
  # Optional override of top-level dependency_roots:
  dependency_roots:
    - /path/to/Modules
test:
  dependency_roots:
    - /path/to/Modules
  trial_url: https://dle-news.ru/files/dle_trial.zip
```

Static and test have different dependency lookup rules; see below and [tests.md](tests.md). Tool caches use an absolute `XDG_CACHE_HOME` when set, otherwise the platform cache directory under `dleplugin/`. Tool images and their dependencies are CLI-managed.

## Static analysis

Runs PHPStan level max with bleeding edge in Docker, targeting the image's PHP 8.4 by default. `--php-version 8.1` changes analysis assumptions, not the container runtime. No local PHP or DLE installation is required; DLE stubs are bundled. Source/include/vendor mounts are read-only.

```sh
dleplugin static
dleplugin static src/Core/App.php --php-version 8.1
dleplugin static --json
dleplugin static --count
dleplugin static --dependency-root /path/to/Modules
```

Files default to `src`; explicit relative paths are resolved from the invocation directory. Plugin-local `dlestatic.yml`:

```yaml
dependencies: [DLEPlugin]
excludePaths:
  analyse: [src/Legacy]
strict: true
ignoreErrors:
  - 'Call to an undefined method .*::legacy\(\)'
```

Dependencies here are independent of `dleplugin.yml.dependencies`. Each name resolves first in global `static.dependencies`, then as `<dependency-root>/<name>`; its `src/` becomes an include path. Missing dependencies produce warnings. `--dependency-root` replaces configured roots. `excludePaths` requires `analyse`. `ignoreErrors` contains Go regular expressions (RE2 syntax; lookarounds are not supported); `strict` enables PHPStan strict rules.

Exit 1 on findings or analyser failure. `--json` contains `messages`, `errors`, `warnings`, `count`; messages/count group by distinct file/line and JSON keeps the first message there. `--count` takes precedence when both output flags are given; a provider error can yield count 0 but still exit 1. Static reads slug without full manifest validation; use build for that.

## Patch compatibility check

```sh
dleplugin check /path/to/DLE
# Ready artifact instead of the current manifest:
dleplugin check --file dist/demo.xml /path/to/DLE /path/to/another-DLE
```

Positional arguments are DLE directories, never the plugin artifact. Reads manifest patches including generated extras/extra configs or `--file` XML/ZIP. Checks search text against the supplied DLE tree, ignoring whitespace/case. Only `before`, `after` and `replace` operations are searched; `create` and other action names are skipped. Missing files/searches and empty searches fail; duplicate matches warn. Create operations are not checked against existing files. Exit 1 if any DLE directory fails. This is a compatibility check, not PHP/SQL execution or proof of runtime behavior.

## Deployment

```sh
dleplugin deploy sites add example.com --login admin --password-env EXAMPLE_ADMIN_PASSWORD
dleplugin deploy sites list
dleplugin deploy sites show example.com
dleplugin deploy sites edit example.com --admin-path panel.php
dleplugin deploy upload example.com --dry-run
dleplugin deploy upload example.com --new
dleplugin deploy upload example.com --file dist/demo.zip
dleplugin deploy sites remove example.com
```

Upload logs into DLE 13+ administration, matches the installed plugin by XML `name`, and updates it. Without `--file`, it builds first (replacing `dist/`), applying manifest version or `--version`, and `upgrade_url` or `--upgrade-url`. `--file` is mutually exclusive with `--version` and `--upgrade-url`. `--new` permits first installation; interactive upload can ask when missing. `--dry-run` still builds and authenticates/looks up the plugin but does not upload.

Project registry is `dleplugin-deploy.yaml` next to the manifest; global registry is `~/.config/dleplugin/dleplugin-deploy.yaml`. Add/edit/remove target project by default; `--global` works outside a project. List/show use effective records. Project records replace whole global records for the same lowercase host (including port); fields are not merged.

```yaml
sites:
  example.com:
    url: https://example.com
    admin_path: panel.php
    login: admin
    password_env: EXAMPLE_ADMIN_PASSWORD
```

Host is the key, not a profile name. HTTPS defaults except for `localhost` and `*.localhost` (HTTP); script defaults to `admin.php`. An explicit URL bypasses saved connection settings. Add/edit authenticate before saving. Passwords are not written into YAML: stdin, selected env variable (default `DLEPLUGIN_PASSWORD`), saved-site OS credential store, then secret prompt, in that order. An empty explicitly selected variable does not fall back to the default variable. Prompted saved-site passwords and passwords piped to add/edit are remembered in the OS store; env passwords are not. `--password-stdin` requires HOST and a flag/saved login for upload. There is no `--password` flag. `--new`/`--insecure` are not persisted. Redirects stay on the same origin; use the final admin URL for cross-origin redirects.
