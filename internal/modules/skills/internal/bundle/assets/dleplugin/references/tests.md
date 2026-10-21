# DLE Plugin Tests

Use this reference when working with tests for CMS DataLife Engine (DLE) plugins built with `dleplugin`.

## Core rules

- Write tests with PestPHP syntax.
- Put tests under the project `tests/` directory.
- Run tests with `dleplugin test` from the plugin project root.
- Prefer fast unit/application tests that exercise plugin classes directly.
- Keep individual tests focused, but account for the runner: `dleplugin test` always bootstraps a real DLE trial in Docker; it is not a standalone unit-test mode.
- Keep tests deterministic: use fixed dates, in-memory repositories, explicit request/input state, and isolated fixtures.
- Follow any repository instruction files before editing tests.

## Before writing tests

1. Inspect the existing `tests/` directory for local patterns.
2. Inspect nearby source classes for namespace, constructor dependencies, and expected behavior.
3. Check whether the project already provides test doubles under `tests/Unit/` or another test helper directory.
4. If no tests exist, create the smallest PestPHP test file that proves the requested behavior.

## PestPHP style

Use Pest's global `it()` and `expect()` helpers:

```php
<?php

declare(strict_types=1);

use iCooLER\myplugin\Domain\Entity\Thing;

it('does the expected behavior', function () {
    $thing = Thing::create('value');

    expect($thing->value())->toBe('value');
});
```

For exception behavior:

```php
expect(fn () => $subject->run($invalidInput))->toThrow(ExpectedException::class);
```

For arrays and controller-style responses:

```php
expect($response['success'])->toBeTrue();
expect($response['data']['id'])->toBe(1);
```

## Existing example locations

When the target plugin repository has a `tests/` directory, use it as the primary source of examples. Common patterns include:

- `tests/Domain/*Test.php` for value objects and domain entities.
- `tests/Application/*Test.php` for use cases and application services.
- `tests/Infrastructure/*Test.php` for repository or adapter behavior.
- `tests/Presentation/*Test.php` for controllers and request/response behavior.
- `tests/Site/*Test.php` for public page rendering behavior.
- `tests/Unit/*` for fixed clocks, in-memory repositories, and other test doubles.

Illustrative example patterns to use only when matching files exist in the target project:

- `tests/Domain/CollectionTest.php` creates domain entities, checks normalized values, and asserts invalid input throws domain exceptions.
- `tests/Presentation/CollectionApiControllerTest.php` composes a controller with in-memory repositories and sets `$_REQUEST` explicitly before calling controller methods.
- `tests/Site/SitePageRenderTest.php` renders HTML and asserts expected text and links with `toContain()`.

## Writing new tests

1. Choose the test directory by behavior layer:
   - Domain rules: `tests/Domain/`
   - Use cases/services: `tests/Application/`
   - Persistence/adapters: `tests/Infrastructure/`
   - Controllers/API: `tests/Presentation/`
   - Site/frontend rendering: `tests/Site/`
   - Shared test doubles: `tests/Unit/`
2. Name files as `<Subject>Test.php`.
3. Start each PHP test with:

```php
<?php

declare(strict_types=1);
```

4. Import only the classes used by the test.
5. Build dependencies explicitly in the test or a small local helper function.
6. Prefer one behavior per `it()` block.
7. Assert observable behavior, not implementation details.

## Running tests

Run all tests from the plugin root:

```bash
dleplugin test
```

The CLI supports file/directory arguments and a name filter. Paths are relative to the invocation directory and normalized to the discovered plugin root:

```bash
dleplugin test tests/Domain/CollectionTest.php
```

```bash
dleplugin test tests/Domain --filter 'creates a collection'
dleplugin test --update-snapshots
dleplugin test --coverage-min 80
dleplugin test --coverage-html /tmp/plugin-coverage
dleplugin test --refresh-dle
```

`--coverage`, `--coverage-min`, and `--coverage-html` use PCOV; the latter two imply coverage. `--refresh-dle` downloads/installs the trial again. Snapshots use Pest's standard layout under `tests/.pest/snapshots/<test path>/`. The command returns Pest's exit code. With no tests it warns and succeeds; this is not evidence of test coverage.

## Runner, bootstrap and dependencies

Requires a working Docker engine, not a local PHP/Composer installation. The CLI downloads and caches the DLE trial installation, then creates a fresh copy/database for each run with MariaDB. Tool image uses PHP 8.4, Pest 5, Mockery and PCOV. It connects plugin files and patches like a real installation; prefer deterministic test doubles inside that environment rather than assuming the runner avoids DLE.

Runtime dependencies are `dependencies` in `dleplugin.yml` (other plugin display names, not filesystem paths). They are recursively connected before the plugin. Lookup uses configured `test.dependency_roots` or the top-level `dependency_roots`: first a matching directory by name, then plugin manifests directly inside those roots, matching name/slug. Missing dependencies and dependency cycles fail.

Test-only dependencies go in plugin-local `dletest.yml`:

```yaml
depends:
  - ../TestSupport
  - AnotherPlugin
```

An existing plugin path resolves relative to the project root; otherwise the value is looked up as a plugin name. This differs from static's independent `dlestatic.yml.dependencies` lookup; see [cli.md](cli.md). Unlike `static`, `test` has no `--dependency-root` flag; use `dletest.yml.depends` or configured `test.dependency_roots`.

Before bootstrap, the CLI links the plugin's `templates/` contents into the trial site's `templates/Default/`, and links plugin-root `.env` and `conftest.php` when present. Existing destination entries are kept rather than overwritten. The bootstrap loads `.env` and optional root `conftest.php`, defines DLE constants, initializes DLE/database/session/user/category state, and autoloads `Tests\` from `tests/`. Installed plugin classes still need working manifest autoloading/bootstrap. Inspect `conftest.php` before adding globals or doubles. DLE 19+ xfields compatibility helpers are supplied by the test bootstrap. Do not infer the installed trial's version from a fixed old example: the runner downloads the current trial.


## Fixing failures

When tests fail:

1. Read the failure output and identify the exact failing expectation or error.
2. Fix the smallest relevant production or test code issue.
3. Re-run `dleplugin test`.
4. Do not mark the task complete until tests pass, or clearly report why tests could not be run.

## Completion checklist

Before responding to the user:

- New or changed tests follow existing PestPHP style.
- Test names describe behavior.
- Test doubles are simple and deterministic.
- `dleplugin test` was run from the plugin root.
- The result is reported clearly, including any failures or inability to run the command.
