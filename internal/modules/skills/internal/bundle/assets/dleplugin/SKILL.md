---
name: dleplugin
description: >
  Develop, configure, build, and test DataLife Engine (DLE) plugins using the
  dleplugin CLI and iCooLER DLEPlugin PHP framework. Use for dleplugin.yml,
  plugin source, runtime bindings/routes, lifecycle SQL, and CLI verification or
  deployment. For native admin markup and controls, use dle-admin-ui alongside
  this skill when working in a dleplugin project.
---

# DLE Plugin Development

Use `dleplugin.yml` and the actual CLI implementation as the source of truth for builds. Inspect the manifest and existing source before editing. The PHP framework is a separate dependency: framework APIs do not imply support for similarly named manifest directives.

## Workflow

1. Find the plugin root (`dleplugin.yml`); commands discover it by walking upward from the working directory.
2. Read `slug`, `name`, `namespace`, `mount`, `alias`, `dependencies`, `build`, and `extras`. Namespace is independent of slug. Preserve working project conventions.
3. Inspect `src/`, tests, and any bootstrap that registers runtime bindings/routes. Entry files intended for packaging belong under `src/`, including `src/admin.php` and `src/ajax.php`.
4. Change project source/config, then regenerate artifacts when needed. `build` replaces `dist/`; do not store hand-written sources there.
5. Run `dleplugin static [FILES...]` for PHP changes and relevant `dleplugin test [FILES...]`. Use `build` to validate manifest/build behavior and `check DLE_DIR...` to check patch compatibility. Static analysis alone does not validate the complete manifest.
6. Report verification results and any missing Docker, dependencies, or DLE fixtures. Do not claim skipped checks passed.

Prefer CLI-managed tools over local PHP/Composer setup for plugin checks. Do not install packages or run project Composer scripts just to use static/test: the CLI manages its own tool dependencies. Respect explicit project dependency policies; the CLI does not forbid Composer or external libraries in every plugin.

## Read the relevant reference

| Task | Reference |
|---|---|
| All commands/flags, global config, static/check/deploy | [references/cli.md](references/cli.md) |
| Manifest fields, build/split, namespace/mount/alias, lifecycle hooks | [references/manifest.md](references/manifest.md) |
| Pest runner, dependency resolution, test bootstrap | [references/tests.md](references/tests.md) |
| Framework container and legacy `di.bindings` migration | [references/di-bindings.md](references/di-bindings.md) |
| Frontend Router API and legacy `extras.routes` migration | [references/extras-routes.md](references/extras-routes.md) |
| App, controllers, responses, templates, HTTP, config/cache/logs, DB, Model/Form | [references/framework.md](references/framework.md) |

## Manifest and source essentials

A small explicit manifest:

```yaml
slug: usercollections
name: User Collections
description: Collections for DLE
namespace: iCooLER\usercollections
mount: engine/iCooLER/usercollections
files: {}
build:
  split: false
extras:
  autoloader: true
  admin_section: true
alias:
  admin.php: engine/inc/usercollections.php
  ajax.php: engine/ajax/usercollections_ajax.php
mysql:
  install: |
    CREATE TABLE IF NOT EXISTS `{prefix}_plugin_items` (
      `id` INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- This namespace is an example, not a required vendor prefix. For `namespace: Acme\Collections`, `src/Core/App.php` declares `namespace Acme\Collections\Core;`.
- A source target is `<mount>/<path relative to src/>`; alias keys are relative to `src/`, and alias values are full target paths relative to DLE root. Root-level `admin.php` is not automatically packaged.
- `namespace` defaults from an `engine/...` mount, then from a valid PHP-name slug. An explicitly declared namespace supplies an omitted mount as `engine/<namespace as path>`. Neither field means there is no automatic `engine/iCooLER/<slug>` mount. Prefer explicit values.
- The generated autoloader resolves the full class name below `ENGINE_DIR`, using `DLEPlugins::Check()`. With it enabled, a nonempty mount must equal `engine/<namespace as path>`. Do not alias autoloaded classes away from their expected paths.
- `extras.autoloader: true` enables injection in both DLE function files; `[true, false]` enables only the first. It defaults off.
- `extras.admin_section` adds enable/disable/delete SQL for an admin section whose name is lowercase slug. It does not create an entry file: map `src/admin.php` to `engine/inc/<lowercase-slug>.php` to serve that section.
- AJAX example above uses `/engine/ajax/controller.php?mod=usercollections_ajax`: derive `mod` from the target filename without `.php`, not the alias key. Cron may call that HTTP endpoint when the project intends it; there is no CLI cron generator.
- Lifecycle hooks `install`, `upgrade`, `enable`, `disable`, `delete` under `mysql` and `php` are strings, usually YAML literal blocks (`|`), never lists of queries. Preserve `{prefix}` for DLE substitution.
- Unknown fields are ignored. In particular, this Go CLI does not compile `di.bindings` or `extras.routes`. Preserve legacy metadata when needed, but register bindings/routes in a PHP bootstrap or explicit `files` patches and verify that the bootstrap actually runs.

Keep one top-level section per key. Consult the manifest reference for patch syntax, extra configs, version/dependencies and split output requirements.

## DLE inclusion and paths

When loading DLE PHP through its plugin system, pass the path through `DLEPlugins::Check` **and include the returned path**:

```php
require_once \DLEPlugins::Check(ENGINE_DIR . '/modules/example.php');
```

A bare `DLEPlugins::Check(...)` call does not execute the file. Use `ENGINE_DIR`, `ROOT_DIR`, and configured mount paths when addressing DLE files. `__DIR__` is not prohibited by the CLI; use it only when a source-relative path remains correct in the installed/patched execution context.

## Admin integration

Set `extras.admin_section: true`, package `src/admin.php` with the matching `engine/inc/<lowercase-slug>.php` alias, and keep reusable classes in `src/` under the manifest namespace. Use the framework's admin router/bootstrap when the project depends on DLEPlugin; see [framework.md](references/framework.md).

For admin markup, forms, controls, tabs, and modals, also use the separate `dle-admin-ui` skill (installed alongside this skill: [dle-admin-ui/SKILL.md](../dle-admin-ui/SKILL.md)). Keep manifest, entrypoint, runtime routing, persistence, and CLI checks in this workflow; the UI skill supplies the native DLE presentation patterns. Load it for tasks that render or change an admin screen.

## Completion checklist

- Manifest examples use supported field types; namespaces and source targets agree.
- Entry aliases point at actual files under `src/` and correct DLE targets.
- Runtime DI/routes are registered by executed PHP, not assumed from ignored YAML.
- Relevant static/test/build/check results are reported accurately.
- Generated artifacts are regenerated from source when required.
