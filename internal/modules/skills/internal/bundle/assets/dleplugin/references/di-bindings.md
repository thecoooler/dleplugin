# Framework dependency injection and legacy di.bindings

The current Go CLI ignores `di.bindings`: it does not generate bindings from this YAML. A legacy project may have a separate generator; inspect it before preserving that assumption. Valid YAML/build success does not prove a binding exists at runtime.

## Register bindings in PHP

Use the supplied framework API in a bootstrap that actually runs before services are resolved:

```php
use iCooLER\DLEPlugin\DLEPlugin;
use Acme\Demo\Domain\Repository\ItemRepositoryInterface;
use Acme\Demo\Infrastructure\Repository\ItemRepository;

DLEPlugin::bind(ItemRepositoryInterface::class, ItemRepository::class, true);
$repository = DLEPlugin::make(ItemRepositoryInterface::class);
```

Use the project's manifest namespace for plugin imports; `Acme\Demo` is illustrative. Keep the framework imports under `iCooLER\DLEPlugin`.

Inspect `dleplugin.yml` and existing bootstrap/patches first. Put shared registration in `src/bootstrap.php` or the project's existing initialization file and include it through an explicit `files` patch or entry point. Choose a verified DLE hook that runs early enough for both frontend and admin paths needing the binding. A file under `src/` is packaged but is not executed merely because it exists. See [manifest.md](manifest.md) for patch syntax.

## Lifetimes and resolution

- `DLEPlugin::bind($abstract, $implementation, $singleton = false)` returns the container. Pass `true` deliberately for shared services.
- `DLEPlugin::make($class)` resolves from the shared container; it enables autoSingleton for unbound concrete classes. Do not assume all concrete resolutions create fresh instances.
- Interfaces need bindings to concrete implementations; constructor dependencies are resolved recursively. Scalar parameters need usable defaults. Avoid unsupported union types.
- Prefer interface-to-implementation mappings for repositories, clocks, gateways and adapters. Share only services whose state is appropriate for the container lifetime.
- Construct entities, value objects and DTOs explicitly. Do not bind data arrays, SQL, templates or exceptions.
- The installed framework's frontend DLE patch resolves route controllers through `DLEPlugin::make()`. `Router::exec()` itself uses `new Controller()`; the admin dispatcher uses `new Controller($admin)`. Inspect which path invokes the controller before relying on constructor DI.

## Migrating legacy YAML

An old project may contain:

```yaml
di:
  bindings:
    Acme\Demo\Domain\Repository\ItemRepositoryInterface:
      target: Acme\Demo\Infrastructure\Repository\ItemRepository
      singleton: true
```

Treat this as metadata unless an actual project-specific consumer is found. In the current CLI, migrate its intent to the PHP `bind()` call above. Preserve existing YAML if other tools need it; do not add more ignored entries as a working fix.

Verify that the interface and implementation exist, the implementation satisfies the interface, the registration bootstrap executes before `make()`, and a relevant runtime test resolves the expected class. Run static analysis as well; it cannot prove that a bootstrap was executed.

Evidence: CLI `internal/manifest/types.go`, `parse.go`; PHP framework `src/DLEPlugin.php`, `src/Core/DI.php`, `src/Core/Admin.php`, `src/Core/Router.php`, and its `dleplugin.yml`.
