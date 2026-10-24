# Frontend routes and legacy extras.routes

The current Go CLI ignores `extras.routes`. It does not turn this YAML into `Router::add()` calls. Preserve legacy entries only when another verified project tool consumes them; do not promise working dispatch by editing ignored YAML.

## Register a route with the PHP framework

```php
use iCooLER\DLEPlugin\Core\Router;
use Acme\Demo\Site\ProfileController;

Router::add('/profile/{int:user_id}', [ProfileController::class, 'show'], ['do' => 'demo']);
```

Use the actual manifest namespace, existing plugin `do` convention, and a real public controller method. The controller receives a route-argument array, not one positional argument per placeholder:

```php
public function show(array $args): \iCooLER\DLEPlugin\Core\SiteResponse
{
    $userId = (int) $args['user_id'];
    // Render content and construct SiteResponse using the project's conventions.
}
```

This is a signature illustration; supply the response implementation before shipping. See [framework.md](framework.md) for SiteResponse/SEO/breadcrumb examples.

## Ensure registration actually executes

Put registration in an existing executed bootstrap, or package `src/routes.php` and include it through an explicit manifest `files` patch at a verified DLE hook. A new PHP file under `src/` is copied, not automatically run.

The supplied DLEPlugin framework's own manifest patches `engine/init.php` to read/build cached `dleplugin_routes` using `Router::toArray()`, then patches `engine/engine.php` to dispatch and apply `SiteResponse`. Register routes before that cache is built. Invalidate the existing route cache when registrations change. Inspect the installed framework patches and DLE version when choosing the hook; patch order matters. Build plus `check DLE_DIR` confirms patch matching, while runtime tests confirm registration timing and dispatch.

The framework's DLE integration resolves frontend controllers with `DLEPlugin::make()` and applies content/SEO/breadcrumbs from `SiteResponse`. The standalone `Router::exec()` instead creates controllers with `new` and returns their method result. Do not assume those paths have identical DI/response behavior. See [di-bindings.md](di-bindings.md).

## Supported path patterns

Confirmed in `src/Core/Router.php`:

| Pattern | Matcher |
|---|---|
| `/brands` | Exact static path |
| `{int:id}` | `[0-9]+` |
| `{str:name}` | `\w+` (not arbitrary punctuation or hyphenated slugs) |
| `{path:value}` | `[^.]+` (can include slashes, excludes dots) |
| `{config:plugin.key}` | Value from the plugin App config; absent/falsy becomes `__EMPTY__` (do not rely on route suppression) |

Use leading `/` paths and unique routes, and verify regex-sensitive literals and matching order against existing project routes. Match named arguments to the controller's argument-array keys.

## Migrating legacy YAML

An old project may contain this metadata:

```yaml
extras:
  routes:
    /profile/{int:user_id}:
      controller: \Acme\Demo\Site\ProfileController
      method: show
```

For this CLI, replace the assumed generation with the PHP registration shown above and an executed bootstrap. Do not edit generated XML as the primary source. Preserve legacy config when a separate consumer needs it, and keep one `extras` block.

Public routes are distinct from admin routes (`$admin->add()`, an entry aliased to `engine/inc/...`) and AJAX endpoints (entry aliased to `engine/ajax/...`, invoked through `controller.php?mod=...`). Do not expose admin handlers as public routes to work around missing bootstrap wiring.

Verification: confirm the class/public method exists, bootstrap and framework dependency are installed, registration occurs before route-cache construction, route cache is refreshed, and dispatch returns the intended page. Bind interfaces needed by controllers separately; a route alone does not require an explicit controller binding.

Evidence: CLI `internal/manifest/types.go`, `parse.go`; framework `src/Core/Router.php` and `dleplugin.yml` frontend integration patches.
