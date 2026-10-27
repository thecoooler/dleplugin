# DLE hosts (`dleplugin hosts`)

`dleplugin hosts` runs DLE sites in a local Docker stack: Apache, PHP-FPM 8.5
and 7.4, MariaDB. The stack files are embedded in the binary; only Docker with
Compose and clean DLE distributions are needed. The module was moved here from
the standalone `dlehost` CLI and keeps its config, stack and data formats, so
an existing dlehost stack works without changes (see
[Moving from dlehost](#moving-from-dlehost)).

```sh
dleplugin hosts up                                 # build and start the stack

dleplugin hosts config set dists_dir ~/dle/dists
dleplugin hosts config set default_dle 21.0
dleplugin hosts config show

dleplugin hosts versions                           # DLE (from dists_dir) and PHP versions
dleplugin hosts add shop --dle 21.0                # http://shop.localhost/
dleplugin hosts add shop2 --admin-login owner --admin-password secret
dleplugin hosts add public --site-offline off       # create an open site
dleplugin hosts get shop site_offline               # on: closed, off: open
dleplugin hosts set shop site_offline off           # open an existing site
dleplugin hosts set shop site_offline on            # close an existing site
dleplugin hosts list
dleplugin hosts path shop                          # host files: <stack>/www/shop
dleplugin hosts path shop --container              # the same inside the containers: /var/www/shop
dleplugin hosts remove shop
dleplugin hosts port 80                            # Apache port of all hosts
dleplugin hosts path                               # stack directory
dleplugin hosts compose logs -f apache             # any docker compose command
dleplugin hosts down                               # stop the stack
```

The `hosts` commands do not need `dleplugin.yml` or the dleplugin config and
work from any directory.

> [!WARNING]
> **PHP 7.4 is vulnerable to CVE-2026-109618 / CVE-2026-109619** (remote code
> execution in PHP-FPM, October 2026). 7.4 is no longer supported and has no
> fixed official image; the `php74` service is built from the unfixed
> `php:7.4-fpm`. PHP 8.5 is pinned to the fixed 8.5.12.
>
> The 7.4 image has partial mitigations (`docker/php/Dockerfile`):
> `session.upload_progress` is off, the request body is limited to 32M
> (`post_max_size`, `upload_max_filesize`), and an FPM worker serves a single
> request (`pm.max_requests = 1`). They narrow the known attack paths but do not
> replace a fix.
>
> Through external domains php74 is reachable for 7.4 sites. Anyone who can
> reach the Apache port directly can always reach php74: the
> `*.php74.localhost` auto host is selected by the `Host` header even when no
> site uses 7.4. Keep `BIND_ADDRESS=127.0.0.1`, do not
> publish 7.4 sites through external domains, or move them to 8.5. An
> unofficial 7.4 image with the official php-src patch:
> [mpyw/php-eol-CVE-2026-109618-109619](https://github.com/mpyw/php-eol-CVE-2026-109618-109619).

## Where things are stored

- Hosts config: `~/.config/dlehost/config.yml` (overridden by `DLEHOST_CONFIG`).
  Keys: `dists_dir`, `default_dle`, `default_php`, `default_site_offline`, `default_admin_path`, `php_versions`,
  `project_dir`, `base_domain`, `external_origin` (see `dleplugin hosts config --help`).
- Stack: `~/.local/share/dlehost` (`$XDG_DATA_HOME/dlehost`, or `project_dir`,
  or `DLEHOST_DIR`, which takes precedence over `project_dir`):

  ```text
  .env                 database ports and passwords, UID/GID; created once
  docker-compose.yml   ┐
  config/, docker/     ┘ service files from the binary
  www/<name>/          site files
  data/mariadb/        databases
  data/hosts.json      hosts registry
  data/staging/        temporary files of add before publishing to www/
  logs/apache/         Apache logs
  ```

`.env` is created on first run. Any of its keys can be preset with
`DLEHOST_<KEY>`: `COMPOSE_PROJECT_NAME` (docker compose project name and
container name prefix, default `dle`), `BIND_ADDRESS` (port address, default
`0.0.0.0`), `HTTP_PORT`, `DB_PORT`, database passwords, `UID`/`GID`. Values are
written literally (no `${…}` interpolation); single quotes and line breaks are
rejected. The file mode is `0600`. When run as root, `UID`/`GID` default to
`1000`: php-fpm does not run as root. This way you can run a second, isolated
stack next to the main one, for example for tests:

```sh
export DLEHOST_CONFIG=/tmp/hosts-test/config.yml DLEHOST_DIR=/tmp/hosts-test/stack
export DLEHOST_COMPOSE_PROJECT_NAME=hosts-test DLEHOST_BIND_ADDRESS=127.0.0.1
export DLEHOST_HTTP_PORT=18080 DLEHOST_DB_PORT=13307
dleplugin hosts up
```

The stack directory is created by the first command that needs it. Service
files are overwritten when a new binary embeds different ones, so edits in
them are lost: configure the stack through `.env`. Put your own vhosts in
`config/apache/sites/` under names that the embedded stack does not use; such
files are kept. For example, `70-mysite.conf` for your own domain on PHP 7.4:

```apache
<VirtualHost *:80>
    ServerName mysite.test
    DocumentRoot /var/www/mysite
    Use PHP 74
</VirtualHost>
```

Then run `dleplugin hosts compose restart apache`; for domains outside
`.localhost` add `127.0.0.1 mysite.test` to `/etc/hosts`. After updating the
binary run `dleplugin hosts up`; the CLI reminds you. Configs are mounted into
the containers and docker compose does not notice their changes, so `up` after
a stack file update recreates the containers (`--force-recreate`); `data/` is
kept.

## Creating a host (`add`)

`add` installs the original clean DLE 11.0+ distributions `<version>.zip`. The
site directory inside the archive is found by `install.php` (files may be at
the root, in `upload/` or in a nested build directory). The DLE version is
read from the archive (`version_id` in `install.php`). A distribution must
have `install.php` and `index.php` and must not have the files of an installed
site (`engine/data/config.php`, `dbconfig.php`). Old dumps and unpacked builds
with nested ZIPs fail these checks and are not listed by `versions`.

With `--dle VERSION` (or `default_dle`) the CLI looks for exactly
`VERSION.zip` in `dists_dir` and checks the version inside. `--dist PATH`
takes an archive directly; its version is detected:

```sh
dleplugin hosts add shop --dle 21.0
dleplugin hosts add shop --dist /path/to/dle-21.0.zip
```

The CLI unpacks the files into `data/staging/`, creates an empty database and
user, starts a temporary PHP container from the image of the chosen stack
service and walks the standard `install.php` with an HTTP client. The
container port is bound to `127.0.0.1` only and picked by Docker. The DLE
installer creates the tables, administrator, auth key, configuration,
templates and demo data. Before publishing, the configs, the chosen template
and the removal of `install.php` are checked; the container is removed. Apache
gets an already installed site. The domain and scheme are passed to the
installer, so URLs in the config and demo banners match the final address. After installation `allow_alt_url` is enabled for
Apache. New sites are closed using DLE's maintenance mode before they are
published (see below). DLE 11.0–14.2 do not remove `install.php` themselves,
so the CLI does;
14.3+ remove it themselves. The remaining template archive is removed by the
CLI. The administrator email is `admin@example.com`. The stack must be running
(`dleplugin hosts up`).

The PHP version is chosen by the DLE version: PHP 7.4 for 11.0–16.0, PHP 8.5
for 16.1+. `default_php` defaults to `auto`; a saved numeric value is used as
the default even if it may be incompatible with DLE. `--php auto` uses the
standard choice regardless of the config; `--php VERSION` is a manual override.
A PHP version outside the automatic runtime family triggers a compatibility
warning. PHP 7.4 also triggers a
security warning, including when selected automatically. These family checks
do not guarantee compatibility with every DLE release or plugin. A PHP version
missing from `php_versions` is an error. The compatibility warning accounts for
the installer minimum (16.1+ needs PHP 8.0+, 16.0 needs 7.4+, 15.x
needs 7.2+, 14.3 needs 7.0+, 12.1–14.2 need 5.4+, 11.2–12.0 need 5.3.7+,
11.0–11.1 need 5.3+). A choice below this minimum is not blocked, but the DLE
installer may fail. Running old DLE on a new PHP is not supported.

On error everything created is rolled back.

### Site maintenance mode and host settings

New hosts default to `site_offline = 1` in `engine/data/config.php`. Guests
see DLE's maintenance page; administrators and groups with DLE's offline
access permission retain access.

```sh
dleplugin hosts config get default_site_offline    # on by default (site closed)
dleplugin hosts config set default_site_offline off # create open sites by default
dleplugin hosts config unset default_site_offline  # restore the default: on
dleplugin hosts add private --site-offline on       # override the default: closed
dleplugin hosts add public --site-offline off       # override the default: open

dleplugin hosts get shop site_offline               # read the current DLE value
dleplugin hosts set shop site_offline on            # close the site
dleplugin hosts set shop site_offline off           # open the site
```

`default_site_offline` affects new hosts only, including when an existing
hosts config does not yet contain this key. `--site-offline on|off` takes
precedence over the configured default.

`hosts get NAME KEY` and `hosts set NAME KEY VALUE` read and update the host's
DLE configuration directly, so changes from the DLE admin UI are visible.
They also work for DLE directories under `www/` that were not created by
`hosts add`. Supported keys are `site_offline`, with values `on` (closed) and
`off` (open), and `admin_path` (the administrator PHP filename). Unknown keys
and values are rejected. No Docker restart is required. Missing, invalid or
duplicate settings are reported as errors without rewriting the configuration.

### Administrator address

```bash
dleplugin hosts get shop admin_path                # e.g. admin.php
dleplugin hosts set shop admin_path control.php    # rename the current entry point
dleplugin hosts set shop admin_path admin.php      # restore the default
```

Set the administrator address for future `hosts add` commands globally:

```bash
dleplugin hosts config set default_admin_path control.php
dleplugin hosts config get default_admin_path       # control.php
dleplugin hosts config unset default_admin_path     # restore admin.php for new hosts
```

`default_admin_path` defaults to `admin.php`, including in older configs, and
uses the same filename validation as `admin_path`. It only affects new hosts;
existing hosts retain their address. Creation renames the entry point before
publishing the site. A collision or config error rolls back creation, including
the new files and database. `hosts add` prints the resulting administrator URL.

`admin_path` matches the DLE configuration key used by DLE 11.0–21.0. The
command reads the current filename from `engine/data/config.php`, including
manual changes, and moves that file while updating only the setting's value.
Use a filename ending in lowercase `.php`, starting with an ASCII letter or
digit; the remaining stem may contain ASCII letters, digits, `_` and `-`
(maximum 255 bytes including `.php`). Paths, URLs and query strings are rejected.

The current entry point and config must be regular files, not symlinks. Any
existing destination (including directories and dangling symlinks) is a collision;
for example, `index.php` is never overwritten. Setting the current filename again
is a no-op after checking the source. The PHP contents, ownership and permissions
are preserved; config quoting, line endings, unrelated values and permissions
are preserved too. `site_offline` is unchanged, as are all other hosts.

The complete config is staged before changing files and replaced atomically;
if committing it fails, the old entry point is restored. The filesystem must
support hard links within the host directory. This is a two-file operation:
avoid concurrent config edits or requests during the brief change, and do not
interrupt it. A process/system crash between file operations is not transactional.
No redirect or alias remains at the old address after success.

### Administrator credentials

The administrator login and password come from `--admin-login`,
`--admin-password`, or `DLEHOST_ADMIN_LOGIN`, `DLEHOST_ADMIN_PASSWORD`. For
each value: flag, then environment variable, then `admin`. The variables are
read from the process environment, not from the stack `.env`. Empty values are
rejected. The login is up to 40 bytes (letters, digits, `_`, `.`, `-`), the
password up to 72 bytes. DLE itself creates the user and hash through its
installer. The password is not printed. Existing hosts do not change.

## Other commands

`port N` changes `HTTP_PORT` in `.env`, recreates the Apache container,
updates `http_home_url` and clears the page cache (`engine/cache/*.tmp`) of the
hosts without a saved external origin. External URLs, DLE configs and caches of
public hosts do not change with the internal port; update the backend address
of your reverse proxy separately. When the port is busy, the previous port is
restored. `remove` only removes hosts created by `add` (the registry is
`data/hosts.json`).

`path NAME` prints only the path, so it is handy in other commands:
`cd "$(dleplugin hosts path shop)"`. It works for any directory in `www/`.
`www/` is mounted into the apache and php containers as `/var/www`.

## Public hosts behind a reverse proxy

With `base_domain` and `external_origin` new hosts get public addresses:

```sh
dleplugin hosts config set base_domain dle.example.com
dleplugin hosts config set external_origin https://dle.example.com
dleplugin hosts add shop          # https://shop.dle.example.com/
```

`base_domain` is a lower-case DNS domain without scheme, port or `*.`; empty
means `localhost`. `external_origin` is the origin of the **base** domain with
`http://` or `https://` and an optional external port, without a path, query
or fragment, and its domain must equal `base_domain`. A non-empty origin
replaces the backend scheme and port entirely. The domain and origin are saved
per host in `data/hosts.json` at `add`; later config changes apply to new hosts
only.

The proxy needs a wildcard DNS record and certificate for
`*.dle.example.com`, must pass the original `Host` header and send
`X-Forwarded-Proto`. HTTPS vhosts translate `X-Forwarded-Proto: https` into
FastCGI `HTTPS=on` and `REQUEST_SCHEME=https`; this trusts the proxy, so keep
the backend private (`BIND_ADDRESS=127.0.0.1`). Unknown domains get
`403 Unknown host` from `05-fallback.conf` without running PHP. Do not put your
own vhosts before `05-fallback.conf`.

## Moving from dlehost

`dleplugin hosts` replaces the `dlehost` binary and uses the same config file,
stack directory, `DLEHOST_*` variables, registry and password store, so
existing sites, databases and settings are picked up as they are. The embedded
stack files were translated to English, so the first command reports that the
stack files were updated; run `dleplugin hosts up` once to apply them. Sites
and data are not touched.

## Development checks

`go test ./...` does not touch running containers. The Docker tests use their
own temporary stacks and are skipped by default:

```sh
DLEHOST_TEST_DLE21=/absolute/path/to/21.0.zip \
  go test ./internal/modules/hosts/internal/host -run '^TestInstallerDocker$' -v -timeout 20m
DLEHOST_TEST_DISTS=/path/to/dists \
  go test ./internal/modules/hosts/internal/host -run '^TestInstallerMatrix$' -v -timeout 20m
```
