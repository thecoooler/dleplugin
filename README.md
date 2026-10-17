# dleplugin

`dleplugin` is a command-line tool for developing DataLife Engine (DLE)
plugins. It ships as a single static binary. A plugin project is described by a `dleplugin.yml`
manifest (see [docs/dleplugin-yml.md](docs/dleplugin-yml.md)).

## Install / build

```sh
go install github.com/thecoooler/dleplugin/cmd/dleplugin@latest
```

Or build from a clone:

```sh
git clone https://github.com/thecoooler/dleplugin.git
cd dleplugin
make build      # bin/dleplugin
make install    # go install into $GOBIN
```

Requires Go 1.27.2+ (including the security fixes in that patch release).
Builds disable CGO by default; set `CGO_ENABLED=1` explicitly if needed.

## Quick start

```sh
dleplugin init Demo --slug Demo --name "Demo Plugin" --description "My first plugin" --namespace 'Acme\Demo'
cd Demo
mkdir -p src
printf '<?php echo "Demo";\n' > src/Main.php
dleplugin build --version 0.1.0
# Output: dist/Demo.xml
```

See [the manifest reference](docs/dleplugin-yml.md) for aliases, dependencies,
patches, the autoloader and the admin section. To deploy, save a site and upload:

```sh
dleplugin deploy sites add example.com -u admin  # asks for the password
dleplugin deploy upload example.com --new
```

The CLI builds for Linux, macOS and Windows (amd64/arm64). Core commands and
HTTP deployment use the Go binary; `diff` also needs the system `diff` tool.
`static` and `test` require a working Docker engine. They use pinned PHP 8.4
tool images; no local PHP is needed. Deployment requires DLE 13+ and plugin
administration permissions; the test runner uses the downloaded current trial.
The Go tests and image smoke checks run on Linux/macOS in CI; Windows binaries
are cross-built, and Windows Docker workflows are not covered by integration
checks. See [issues](https://github.com/thecoooler/dleplugin/issues) for reports.

## Commands

| Command                                 | Description                                   |
| --------------------------------------- | --------------------------------------------- |
| `init [DIR]`                            | Create `dleplugin.yml` in DIR (default: `.`)  |
| `convert XML_PATH [PLUGIN_PATH]`        | Convert a plugin XML file to `dleplugin.yml`  |
| `static [FILES...]`                     | Run PHPStan for the plugin in Docker          |
| `test [FILES...]`                       | Run the plugin's Pest tests against DLE       |
| `check DLE_DIR...`                      | Check the manifest's patches against DLE dirs |
| `build [--version V] [--upgrade-url U]` | Build the plugin into `dist/`                 |
| `diff SOURCE1 SOURCE2`                  | Compare plugin XML files and projects         |
| `deploy upload [HOST]`                  | Upload the plugin to a DLE site               |
| `deploy sites`                          | Manage saved deployment sites                 |

Global flag: `-v/--verbose`.

### convert

`convert XML_PATH [PLUGIN_PATH]` extracts the files the XML creates into
`src/`: files inside the mount (from `--namespace` or inferred from the file
paths) relative to it, other files under their full DLE paths with matching
`alias` entries. Two files that map to the same `src/` path are an error.

### diff

`diff SOURCE1 SOURCE2` compares plugin XML files, zip archives and project
directories (or their subdirectories) in any combination. Projects are built in
memory like `build` does; only XML content is compared, and the version, slug
and mount are ignored. Differences do not fail the command.

### deploy

`deploy` groups site management and plugin uploads. Running `deploy` without
a subcommand prints help.

Save a site by host, using the same connection options as upload. Missing HOST,
login and password are asked interactively. The admin login is checked before
saving; no plugin is built or uploaded by site management commands.

```sh
dleplugin deploy sites add example.com              # project site, prompts for credentials
dleplugin deploy sites add example.com --global -u admin
dleplugin deploy sites add example.com --admin-path panel.php -u admin \
  --password-env EXAMPLE_ADMIN_PASSWORD
dleplugin deploy sites list
dleplugin deploy sites show example.com
dleplugin deploy sites edit example.com --admin-path new-panel.php
dleplugin deploy sites remove example.com --global
```

Sites are keyed by their lowercase host (including a port when specified),
without separate profile names. A host identifies one DLE installation.
`sites add` rejects an existing host in the selected config; use `sites edit`
to change its settings. Interactive editing offers the saved login and admin
path as defaults. Flags also allow non-interactive editing.

Configuration files:

- Project: `dleplugin-deploy.yaml` next to the plugin's `dleplugin.yml`, found
  even when invoked from a nested directory.
- Global: `~/.config/dleplugin/dleplugin-deploy.yaml`, next to the global config.

`add`, `edit` and `remove` operate on the project file by default; `--global`
selects the global file and works outside a plugin project. `list` and `show`
display the effective configuration and its source. For the same host, the
project record replaces the entire global record; fields are never merged.
Removing the project record makes the global record visible again.

```yaml
sites:
  example.com:
    url: https://example.com
    admin_path: panel.php
    login: admin
    password_env: EXAMPLE_ADMIN_PASSWORD
```

`url` and `admin_path` are optional when editing YAML directly: the host key
provides the URL, HTTPS is assumed (`*.localhost` uses HTTP), and `admin.php`
is the default script. Saved files have permissions `0600`. Passwords are
never written to the file: `--password-env` stores only an environment variable
name. With no password variable configured, `DLEPLUGIN_PASSWORD` or a secret
prompt is used. A password typed or piped during `add`/`edit` is saved in the
OS credential store (macOS Keychain, Windows Credential Manager, Secret Service
on Linux) under the service `dleplugin` and the account `LOGIN@HOST`; `remove`
deletes it. A password from an environment variable is not saved. If the store
is unavailable, a warning is shown and the password is not remembered.

```sh
dleplugin deploy upload                            # choose a saved site or enter a URL
dleplugin deploy upload example.com                # use the effective saved site
dleplugin deploy upload example.com -f dist/my_plugin.xml
dleplugin deploy upload example.com --dry-run
echo "$PASS" | dleplugin deploy upload example.com --password-stdin

# One-off upload: an explicit URL bypasses saved connection settings.
dleplugin deploy upload https://example.com/panel.php -u admin --new
```

`upload` builds the current plugin and uploads its `dist/` artifact (`.xml`,
or `.zip` in split mode) through the admin panel, updating the installed plugin
with the same `<name>`. `--file` uploads a ready artifact without building.
The upgrade URL (see `upgrade_url` in `docs/dleplugin-yml.md`) is written to
the built XML `<upgradeurl>`; only https links are accepted. An unsaved host
is also accepted for a one-off connection. Explicit flags override saved
settings. Without a terminal, HOST and credentials are required; a login from
a saved site satisfies the `--password-stdin` login requirement.

| Flag                  | Description                                                   |
| --------------------- | ------------------------------------------------------------- |
| `HOST`                | saved host, domain, or full admin URL                         |
| `-f, --file PATH`     | upload a ready `.xml`/`.zip` instead of building              |
| `--version V`         | version for the built XML (not with `--file`)                 |
| `--upgrade-url U`     | upgrade URL for the built XML; https only (not with `--file`) |
| `-u, --login NAME`    | admin login                                                   |
| `--admin-path PATH`   | admin script, default `admin.php` (not with a URL path)       |
| `--password-env NAME` | environment variable holding the admin password               |
| `--password-stdin`    | read the admin password from stdin                            |
| `--new`               | install the plugin when it is not on the site yet             |
| `--dry-run`           | log in and look up the plugin, upload nothing                 |
| `--insecure`          | skip TLS certificate verification for this invocation         |

Password precedence: stdin, the selected environment variable, the credential
store (saved sites only), then a secret prompt. A password typed for a saved
site is remembered; a saved password the site rejects is asked for again on a
terminal and replaced. If a site or flag specifies a variable, an empty value does not fall
back to `DLEPLUGIN_PASSWORD`; without a terminal it is an error. There is no
`--password` flag (shell history, `ps`). `--new` and `--insecure` are never saved.
Admin requests follow redirects only within the same scheme, hostname and port.
Use the final admin URL when a site redirects to another origin (for example,
from `example.com` to `www.example.com`); credentials are never forwarded there.

### static

`static` runs PHPStan (level max, bleeding edge) inside the `dleplugin-static`
Docker image, which is built from embedded assets on first use; no local
PHP is needed. Plugin sources, include paths and the shared test Composer
vendor directory are mounted read-only. Flags: `--json`, `--count`,
`-t/--tool-verbose`, `--dependency-root` (repeatable),
`--php-version` (e.g. `8.1`; sets PHPStan's `phpVersion`, by default the
image's PHP 8.4 is assumed). The exit code is 1 when there are findings (after `ignoreErrors`) or PHPStan
failed, so it can gate CI.

DLE classes and functions used by the analyser are described in the bundled
`bootstrap.php`; static analysis does not download or mount a DLE installation.

It reads the plugin's `dlestatic.yml` (`dependencies`,
`excludePaths.analyse`, `strict`, `ignoreErrors`) and the `static` section of
the global config:

```yaml
static:
  dependencies:            # explicit dependency directories
    ExampleDependency: /path/to/ExampleDependency
  dependency_roots: []     # <root>/<name> lookup; falls back to the top-level key
```

### test

Runs the plugin's Pest tests (`tests/`) in Docker against a real DataLife
Engine: the free DLE trial is downloaded from dle-news.ru and installed once
per release (MariaDB in Docker, cached in `dleplugin/test` under the user cache
directory). All caches use an absolute `XDG_CACHE_HOME` when set; otherwise,
they use the platform's default user cache directory. Every run
copies the installation, starts a fresh database, connects the dependencies
and the plugin (symlinks, registered in `dle_plugins`, so DLE applies the
plugin's file patches) and runs Pest in the `dleplugin-test` image (PHP 8.4,
Pest 4, Mockery, PCOV). Nothing has to be installed locally except Docker.

```sh
dleplugin test                      # all tests
dleplugin test tests/Service -f dup # files and a filter
dleplugin test -u                   # update snapshots
dleplugin test --coverage-min 80    # coverage (also --coverage, --coverage-html DIR)
dleplugin test --refresh-dle        # download and install the trial again
```

Dependencies are connected first, each after its own: `dependencies` of
`dleplugin.yml` (plugin names, e.g. `ExampleDependency`, looked up in
`dependency_roots`; see [docs/dleplugin-yml.md](docs/dleplugin-yml.md)) and
`depends` of the plugin's `dletest.yml` (plugins needed only by the tests:
paths relative to the plugin, or names). `.env` and `conftest.php` of the
plugin are loaded by the bootstrap. The exit code is Pest's.

```yaml
dependency_roots: [/path/to/Modules, /path/to/clients]   # or test.dependency_roots
test:
  trial_url: https://dle-news.ru/files/dle_trial.zip     # optional override
```

The trial is always the current DLE release (21.0 at the time of writing).
DLE 19 replaced `xfields.txt` with `xfields.json`; the bootstrap defines
`xfieldsload()` (rows `name, description, category, type, default`) and
`xfieldsdataload()` for plugins written for older versions.

### check

Checks the file patches of `dleplugin.yml` (including generated ones and extra
configs) against the DLE directories you give: an installation or an unpacked
distribution, one or several. Every patched file must exist and the code
searched by `before`/`after`/`replace` must be found in it; whitespace and
case are ignored. Code found more than once is a warning.

```sh
dleplugin check ~/dle/18.1                         # one DLE directory
dleplugin check ~/dists/17.0 ~/dists/18.1          # several
dleplugin check ~/dists/*                          # every unpacked release
dleplugin check -f dist/my_plugin.xml ~/dle/18.1   # a ready .xml/.zip
```

```text
DLE 18.1 (/home/me/dists/18.1): OK
DLE 17.0 (/home/me/dists/17.0): ERROR
  TEXT_NOT_FOUND in engine/modules/main.php: "missing code"
  FILE_NOT_FOUND: engine/modules/extra.php
```

The exit code is 1 when a patch does not apply to some directory.

## Development

```sh
make test       # go test ./...
make pipeline   # gofmt check + go vet + tests
```

To run the real Docker integration test (downloads the official DLE trial,
builds the test image, and starts temporary PHP/MariaDB containers):

```sh
DLEPLUGIN_DOCKER_TEST=1 go test -count=1 -timeout=20m -run '^TestDockerIntegration$' -v ./internal/modules/test/internal/dletest
```

It checks plugin file patches, install SQL, fresh databases on repeated runs,
coverage reporting, and container/network cleanup after success and failure.
The trial is kept in a temporary cache outside the repository and removed
after the test.

See [CONTRIBUTING.md](CONTRIBUTING.md), [AGENTS.md](AGENTS.md)
and [docs/architecture.md](docs/architecture.md).
Fixtures can be updated with `make golden` using only Go; see
[testdata/README.md](testdata/README.md). Tool-image updates are documented in
[docs/tool-images.md](docs/tool-images.md); packaging and draft releases in
[docs/releases.md](docs/releases.md).

## License

MIT. See [LICENSE](LICENSE) and [third-party notices](THIRD_PARTY_NOTICES.md).
The DLE trial and Docker tools retain their own licenses.
