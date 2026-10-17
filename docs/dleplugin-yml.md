# dleplugin.yml configuration specification

`dleplugin.yml` is the plugin manifest read by `dleplugin build`, `test`,
`deploy`, and related commands. The file lives in the plugin root,
next to the `src/` directory. When a command is started inside a subdirectory,
`dleplugin` walks upward until it finds this file.

The manifest describes plugin metadata, generated DLE XML operations, optional
source-file mounting rules, lifecycle SQL/PHP hooks, and a small set of helper
features.

## Minimal manifest

```yaml
slug: ExamplePlugin
name: ExamplePlugin
description: Example plugin
namespace: Acme\ExamplePlugin
mount: engine/Acme/ExamplePlugin
files: {}
```

Only `slug` and `name` are required by validation, but real plugins should also
set `description` and `namespace` (or `mount`). If neither `namespace` nor
`mount` is set, generated `src/` file targets are resolved relative to an
empty path. `dleplugin init` writes both fields.

## Top-level fields

| Field         | Type    | Default          | Purpose                                                        |
| ------------- | ------- | ---------------- | -------------------------------------------------------------- |
| `slug`        | string  | required         | Internal plugin slug. Used in generated paths and helper code. |
| `name`        | string  | required         | Display name written to the DLE plugin XML.                    |
| `description` | string  | `""`             | Display description written to the XML.                        |
| `version`     | string  | `""`             | Plugin version written to the XML `<version>`, see below.      |
| `upgrade_url` | string  | `""`             | Plugin upgrade URL written to the XML `<upgradeurl>`, see below. |
| `namespace`   | string  | see below        | PHP namespace of the plugin classes (autoloader prefix).       |
| `mount`       | path    | see below        | Target directory for files copied from `src/`.                 |
| `dependencies`| list    | `[]`             | Names of required plugins (DLE `needplugin`), see below.       |
| `build`       | mapping | `{split: false}` | Build options.                                                 |
| `extras`      | mapping | defaults below   | Generated helper edits and extra config files.                 |
| `alias`       | mapping | `{}`             | Per-file target overrides for files under `src/`.              |
| `files`       | mapping | `{}`             | DLE file patch operations.                                     |
| `mysql`       | mapping | empty hooks      | SQL lifecycle hooks.                                           |
| `php`         | mapping | empty hooks      | PHP lifecycle hooks.                                           |

Unknown top-level fields are ignored by the current parser.

### namespace and mount

```yaml
namespace: Acme\ExamplePlugin
mount: engine/Acme/ExamplePlugin
```

`namespace` is the PHP namespace of the plugin classes, written without leading
or trailing backslashes. The generated `autoloader` loads classes from
`ENGINE_DIR/<namespace as a path>.php`, so the plugin classes must be mounted at
`engine/<namespace as a path>`.

Each field defaults from the other:

- without `mount`, a declared `namespace` sets it to `engine/<namespace as a
  path>`;
- without `namespace`, it is derived from an `engine/<A>/<B>` mount (`A\B`),
  otherwise it is the slug when the slug is a valid PHP name.

With `extras.autoloader` enabled, a namespace is required and a set `mount`
must equal `engine/<namespace as a path>`; otherwise loading fails.

### version

```yaml
version: 1.2.0
```

Written to the XML `<version>` by `build` and `deploy upload`. The
`--version` flag of these commands overrides it for one build (for example in
CI releases). Without either the version is empty.

### upgrade_url

```yaml
upgrade_url: https://example.com/downloads/myplugin.dleplugin.xml
```

URL DLE requests to upgrade the plugin, written to the XML `<upgradeurl>` by
`build` and `deploy upload`. Leading and trailing whitespace is trimmed. Only
https links with a host and without user info are accepted; a relative, http
or malformed value fails manifest loading and building, and an existing
`dist/` is left unchanged.

The `--upgrade-url` flag of these commands overrides the field for one build
and is trimmed and validated the same way. The manifest is still loaded, so an
invalid `upgrade_url` fails even when a valid flag is given. The flag cannot
clear the URL: `--upgrade-url ""` (or only whitespace) is an error; remove the
field from `dleplugin.yml` to build without an upgrade URL.

### dependencies

```yaml
dependencies:
  - ExampleDependency
```

Plugin names (`name` of the other plugin) this plugin requires. `build` writes
them comma-separated into the XML `<needplugin>`, so DLE refuses to install
the plugin without them; a name must not contain a comma. `convert` reads
`<needplugin>` back. `dleplugin test` connects the dependencies (recursively)
before the plugin, looking them up in `dependency_roots` by directory name or
by `name`/`slug`.

## File generation from src

During build, every regular file under `src/` is turned into a DLE `create`
operation unless `build.split` is enabled. `.DS_Store` is skipped.

By default, the target path is:

```text
<mount>/<relative path under src>
```

For example, with `mount: engine/Acme/ExamplePlugin`, this source file:

```text
src/Admin/ConfigController.php
```

is emitted as:

```text
engine/Acme/ExamplePlugin/Admin/ConfigController.php
```

Use `alias` to override specific files:

```yaml
alias:
  admin.php: engine/inc/example.php
  ajax.php: engine/ajax/example.php
```

The alias key is the path relative to `src/`; the value is the final DLE target
path.

## build

```yaml
build:
  split: false
```

`split` controls whether source files from `src/` are embedded into the built
XML:

- `false`: embed all `src/` files as `create` operations.
- `true`: do not embed `src/` files. The build contains only explicit `files`,
  lifecycle hooks, and generated `extras` operations.

Use `split: true` when the plugin XML should patch DLE but source files are
distributed or installed by another mechanism.

## files

`files` maps DLE target files to ordered patch operations:

```yaml
files:
  engine/modules/main.php:
    - before:
        search: |
          $tpl->set ( '{AJAX}', $ajax );
        replace: |
          require_once \DLEPlugins::Check(ENGINE_DIR . "/Acme/Example/js.php");

    - after:
        search: |
          echo $tpl->result['main'];
        replace: |
          (new \Acme\Example\Widget())->render($tpl->result['main']);
```

Each operation is emitted into the DLE XML as:

- `operation action="<operation name>"`
- `searchcode` from `search`
- `replacecode` from `replace`
- `enabled` set to `1`

Common operation names are `before`, `after`, `replace`, and `create`. The tool
passes the action name through to the XML, so keep it compatible with DLE plugin
operations.

`search` and `replace` are trimmed of leading and trailing newlines. Use YAML
literal blocks (`|`) for multi-line PHP snippets so indentation and quotes stay
readable.

### create operation

For explicit file creation inside `files`, use the compact syntax:

```yaml
files:
  engine/inc/example.php:
    - create: |
        <?php
        echo "Example";
```

The compact `create` form does not need `search`; it becomes `replacecode` in
the XML. Do not use the expanded `search`/`replace` mapping for `create`.

## mysql and php hooks

`mysql` and `php` accept the same lifecycle hook names:

```yaml
mysql:
  install: |
    CREATE TABLE `{prefix}_example_items` (
      `id` int NOT NULL AUTO_INCREMENT PRIMARY KEY
    );
  upgrade: ""
  enable: ""
  disable: ""
  delete: ""

php:
  install: |
    clear_cache();
  upgrade: ""
  enable: ""
  disable: ""
  delete: ""
```

Supported hook keys:

- `install`
- `upgrade`
- `enable`
- `disable`
- `delete`

Missing hooks default to an empty string. Hook text is trimmed of leading and
trailing newlines before XML generation.

Use `{prefix}` in SQL when the query should use the DLE database prefix. The
placeholder is written into the XML unchanged for DLE to process.

## extras

```yaml
extras:
  admin_section: false
  autoloader: false
  configs: []
```

### admin_section

When `admin_section: true`, `dleplugin` appends generated SQL to lifecycle
hooks:

- `mysql.enable`: inserts a row into `{prefix}_admin_sections`.
- `mysql.disable`: deletes that row.
- `mysql.delete`: deletes that row.

The generated section name is `slug.lower()`. The generated title and
description come from `name` and `description`.

### autoloader

`autoloader` injects an autoloader for classes in `<namespace>\` before
`function dle_session( $sid = false ) {` in DLE files.

Accepted forms:

```yaml
extras:
  autoloader: true
```

```yaml
extras:
  autoloader: [true, false]
```

`true` enables both supported injection targets. A list controls them by index:

| Index | DLE file                               |
| ----- | -------------------------------------- |
| `0`   | `engine/inc/include/functions.inc.php` |
| `1`   | `engine/modules/functions.php`         |

Values after index `1` are ignored. False-like values disable that target.

### configs

`configs` is a list of extra YAML files to merge:

```yaml
extras:
  configs:
    - patches/admin
    - patches/site
```

Each entry is resolved relative to the plugin root and gets `.yml` appended.
The example above reads:

```text
patches/admin.yml
patches/site.yml
```

Extra config files are parsed as file-patch fragments. Only their `files`
section is merged. If a target file already exists in the main manifest, extra
actions are appended after existing actions.

Example extra config:

```yaml
files:
  engine/modules/main.php:
    - before:
        search: |
          echo $tpl->result['main'];
        replace: |
          require \DLEPlugins::Check(ENGINE_DIR . "/Acme/Example/main.php");
```

## Complete example

```yaml
slug: ExamplePlugin
name: ExamplePlugin
description: Example DLE plugin
mount: engine/Acme/ExamplePlugin

build:
  split: false

extras:
  admin_section: true
  autoloader: [true, true]
  configs:
    - patches/extra

alias:
  admin.php: engine/inc/example.php
  ajax.php: engine/ajax/example.php

files:
  engine/init.php:
    - after:
        search: |
          require_once (DLEPlugins::Check(ENGINE_DIR . '/modules/functions.php'));
        replace: |
          define("EXAMPLE_PLUGIN", true);

  engine/modules/main.php:
    - before:
        search: |
          $tpl->set ( '{AJAX}', $ajax );
        replace: |
          require_once \DLEPlugins::Check(ENGINE_DIR . "/Acme/ExamplePlugin/js.php");

mysql:
  install: |
    CREATE TABLE `{prefix}_example_items` (
      `id` int NOT NULL AUTO_INCREMENT PRIMARY KEY
    );

php:
  enable: |
    clear_cache();
```

## Validation and build behavior

Use these commands from the plugin root while editing a manifest:

```sh
dleplugin build
dleplugin check dist/<slug>.xml
```

The parser reports YAML syntax errors as `dleplugin.yml syntax error` and schema
errors as `dleplugin.yml validation error`. The validation layer currently
requires only `slug` and `name`, so a syntactically valid but incomplete
manifest can still build into an unusable plugin. Prefer explicit defaults in
shared examples when the intent matters.

## Filesystem path safety

`slug` must be a nonempty single filename component. `mount`, alias targets
and patched file names must be relative slash paths within the DLE root.
Absolute paths, `..` components, backslashes and colons are rejected.
Local file writes also reject parent symlinks that leave the destination root.
Unsafe manifests are rejected before `dist/` is cleared or files are connected.
