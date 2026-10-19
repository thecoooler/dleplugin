# Manifest and build behavior

Checked against `internal/manifest/`, `internal/plugin/`, `internal/build/`, `docs/dleplugin-yml.md`, and golden fixtures in the Go CLI at `d9bce4fdf75ebc5dcff2380641db2cb98eb663eb`.

## Fields and defaults

| Field | Shape / behavior |
|---|---|
| `slug`, `name` | Required strings; slug is a nonempty single filename component |
| `description`, `version` | Optional strings, empty by default |
| `namespace` | PHP namespace without leading/trailing backslashes |
| `mount` | Directory relative to DLE root for default `src/` targets |
| `dependencies` | List of other plugins' display names; emitted comma-separated into DLE `needplugin`; commas in names rejected |
| `build.split` | Boolean, false by default |
| `extras.admin_section` | Boolean, false by default; generates enable/disable/delete SQL |
| `extras.autoloader` | Boolean or list controlling two DLE injection targets; off by default |
| `extras.configs` | List of paths relative to project root, without `.yml`; merges only their `files` actions, appended in order |
| `alias` | Mapping from path relative to `src/` to final DLE path |
| `files` | Mapping of DLE target paths to ordered patch operations |
| `mysql`, `php` | Mappings of lifecycle hook names to string bodies |

Unknown fields are ignored, including `di` and `extras.routes`; valid YAML is not proof that a directive does anything. See [di-bindings.md](di-bindings.md) and [extras-routes.md](extras-routes.md) for PHP registration instead.

## Namespace, mount, alias

```yaml
slug: demo
name: Demo
namespace: Acme\Demo
mount: engine/Acme/Demo
extras:
  autoloader: [true, true]
alias:
  admin.php: engine/inc/demo.php
  ajax.php: engine/ajax/demo_ajax.php
```

`src/Service/Importer.php` declares `namespace Acme\Demo\Service;` and installs to `engine/Acme/Demo/Service/Importer.php`. `src/admin.php` installs to `engine/inc/demo.php`; alias targets do not get mount prepended. `src/` is recursively packaged (all regular files, not only PHP; `.DS_Store` skipped). A missing `src/` yields no source files.

An explicit namespace supplies an omitted mount as `engine/<namespace path>`. An omitted namespace derives from a valid `engine/...` mount, otherwise from a valid PHP-name slug. With neither declared, mount stays empty even if namespace can fall back to slug. Set both explicitly in shared examples. `init` defaults namespace to slug and writes the corresponding mount; there is no implicit `iCooLER` prefix.

Autoloader injection happens before `function dle_session( $sid = false ) {` in:

1. `engine/inc/include/functions.inc.php`
2. `engine/modules/functions.php`

`true` enables both; `[true, false]` only the first. The generated loader checks `<namespace>\` and includes `ENGINE_DIR/<full class name as path>.php` via `DLEPlugins::Check`. Namespace is required when enabled; any nonempty mount must match `engine/<namespace path>`. Aliasing classes away from those paths breaks this loader.

Targets (`mount`, alias values, patched filenames) are relative slash paths within DLE: no absolute paths, `..`, backslashes or colons. Alias keys also have to be safe relative paths. Do not use mount to point outside the CMS root.

## File patches and extra configs

```yaml
files:
  engine/modules/main.php:
    - before:
        search: |
          echo $tpl->result['main'];
        replace: |
          require_once \DLEPlugins::Check(ENGINE_DIR . '/Acme/Demo/bootstrap.php');
    - after:
        search: |
          echo $tpl->result['main'];
        replace: |
          // PHP after the matched code.
  engine/inc/demo_helper.php:
    - create: |
        <?php
        // Explicitly created file.
extras:
  configs: [patches/admin]
```

Extra file `patches/admin.yml` contains a `files:` mapping in the same format. Only its file actions merge, not metadata, aliases, lifecycle hooks or extras. Existing target actions come first. Non-create operations require `search` and `replace`; `create` takes a scalar body, not a nested mapping. Action names pass through to XML, so use names supported by DLE (commonly before/after/replace/create). Code is trimmed of outer CR/LF characters.

## SQL and PHP lifecycle hooks

```yaml
mysql:
  install: |
    CREATE TABLE `{prefix}_demo_items` (`id` int NOT NULL PRIMARY KEY);
  upgrade: |
    ALTER TABLE `{prefix}_demo_items` ADD COLUMN `title` varchar(255) NOT NULL DEFAULT '';
  enable: ''
  disable: ''
  delete: ''
php:
  enable: |
    clear_cache();
```

Both sections support `install`, `upgrade`, `enable`, `disable`, `delete`. Bodies are strings; use one literal block for multiple statements. Missing hooks become empty. DLE receives `{prefix}` unchanged. Keep install/upgrade schema changes in these hooks rather than hiding them in entry files. `extras.admin_section` appends generated SQL without replacing existing bodies; the section name is lowercase slug.

## Build artifacts

```sh
dleplugin build
dleplugin build --version 1.2.0
```

The command rebuilds `dist/`. Default output is `dist/<slug>.xml` with `src/` files embedded as create operations, explicit patches, hooks and generated extras. Manifest version is used unless a nonempty `--version` overrides it; without either, XML version is empty. Root `README.md`, when present, becomes `dist/README.html`.

With `build.split: true`, `src/.htaccess` is required. Output is `dist/<slug>.zip`: XML named `<slug>.xml` plus source files at their resolved mount/alias targets. Source files are omitted from XML; explicit `files` and extras remain there. The standalone XML is removed from `dist/` after archiving. This is packaged source distribution, not a build that drops source files entirely.

`check --file dist/<slug>.xml DLE_DIR` checks a ready artifact (use `.zip` for split builds). Bare `check dist/<slug>.xml` is wrong: positional arguments are DLE directories. `check` does not execute PHP/SQL or validate runtime DI/routes.
