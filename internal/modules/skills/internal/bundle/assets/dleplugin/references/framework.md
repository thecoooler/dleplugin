# DLEPlugin Microframework Usage

## Contents

- [Standard workflow](#standard-workflow)
- [Choosing the right framework class](#choosing-the-right-framework-class)
- [Bootstrap and dependency injection](#bootstrap-and-dependency-injection)
- [Admin router and UI](#admin-router-and-ui)
- [Frontend routes and SiteResponse](#frontend-routes-and-siteresponse)
- [Template and TemplateString](#template-and-templatestring)
- [HTTP client](#http-client)
- [Config, cache, and logs](#config-cache-and-logs)
- [Database helpers](#database-helpers)
- [Models and forms](#models-and-forms)
- [Common pitfalls](#common-pitfalls)
- [Completion checklist](#completion-checklist)

Use this reference when writing plugin PHP code that should use the `iCooLER\DLEPlugin` framework layer over DataLife Engine (DLE). Use the [main guide](../SKILL.md) for `dleplugin.yml`, build, aliases, migrations, and verification.

The framework namespace is always `iCooLER\DLEPlugin`. The plugin's own code uses the manifest's `namespace` (or its documented mount/slug fallback), not a fixed vendor prefix. Existing examples below use `iCooLER\myplugin` illustratively.

## Standard workflow

1. Open `dleplugin.yml` and read `slug`, `namespace`, and `mount` before writing namespaces, admin URLs, config keys, or route examples.
2. Inspect the existing `src/` tree and follow its controller/model/service layout.
3. Prefer DLEPlugin framework helpers over raw DLE globals when a helper exists.
4. Keep raw DLE access at boundaries: entry files, repositories, adapters, and rendering glue.
5. For public routes, return `SiteResponse`; for admin or AJAX responses, use strings or `Core\Response`.
6. Run `dleplugin static` and relevant `dleplugin test` checks from the plugin root using CLI-managed tooling.

## Choosing the right framework class

| Need | Use |
|---|---|
| Shared plugin services | `Core\App::getApp($slug)` |
| Dependency resolution | `DLEPlugin::make()`, `DLEPlugin::bind()`, `DLEPlugin::container()` |
| Admin routing and view rendering | `Core\Admin`, `Core\Controller`, `Core\AdminResponse` |
| Admin redirects, JSON, files | `Core\Response` |
| Frontend page content plus metadata | `Core\SiteResponse`, `Core\Seo\Seo`, `Core\Breadcrumbs\Breadcrumbs` |
| DLE template files or template strings | `Core\Template`, `Core\TemplateString` |
| HTTP requests | `Core\HTTP` or `Core\HTTP\Client` |
| Plugin config/cache/logs | `Core\Config`, `Core\Cache`, `Core\DLECache`, `Core\Logger` via `App` |
| Database access | `Core\DB` |
| Admin model forms and CRUD helpers | `Core\Model\Model`, `Core\Model\Form`, `Admin\ModelController` |
| Pagination | `Core\Nav` |

## Bootstrap and dependency injection

Use `App::getApp($slug)` for per-plugin services and `DLEPlugin::make()` for container-resolved services.

```php
use iCooLER\DLEPlugin\Core\App;
use iCooLER\DLEPlugin\DLEPlugin;
use iCooLER\myplugin\Application\ImportService;

$app = App::getApp('myplugin');

$enabled = (bool) $app->config->get('enabled', false);
$service = DLEPlugin::make(ImportService::class);
```

DI autowires concrete classes through constructors. Interfaces need runtime bindings via `DLEPlugin::bind()`; the current Go CLI ignores `di.bindings` YAML. See [di-bindings.md](di-bindings.md) for registration and migration. Avoid scalar constructor parameters without defaults because the container cannot infer them. Avoid union-typed constructor dependencies because the container does not support them.

Register `DLEPlugin::bind()` calls in an executed PHP bootstrap before resolving services. Explicit bindings default to transient unless the third argument is true; the shared container enables autoSingleton for unbound concrete classes.

## Admin router and UI

Package the admin entry under `src/` and alias it to `engine/inc/<lowercase-slug>.php`. It usually creates the plugin app, registers admin routes, and calls `run()`. The admin dispatcher constructs controllers with the admin object, rather than resolving them through DI. Custom views default to `ENGINE_DIR/iCooLER/<slug>/Views`; with a different mount, configure `$admin->addViewsFolder()` with the installed absolute views path.

```php
use iCooLER\DLEPlugin\Core\App;
use iCooLER\myplugin\Admin\DashboardController;
use iCooLER\myplugin\Admin\SettingsController;

$app = App::getApp('myplugin');
$admin = $app->admin;

$admin->add('/', [DashboardController::class, 'index']);
$admin->add('/settings', [SettingsController::class, 'index']);

$admin->run();
```

Admin controllers should extend `Core\Controller`. They receive the current `Admin` instance through `$this->admin`.

```php
namespace iCooLER\myplugin\Admin;

use iCooLER\DLEPlugin\Core\AdminResponse;
use iCooLER\DLEPlugin\Core\Controller;
use iCooLER\DLEPlugin\Core\Response;

final class SettingsController extends Controller
{
    public function index(array $args)
    {
        if ($_SERVER['REQUEST_METHOD'] === 'POST') {
            global $dle_login_hash;
            $token = $_POST['user_hash'] ?? null;
            if (!is_string($token) || !is_string($dle_login_hash)
                || $dle_login_hash === '' || !hash_equals($dle_login_hash, $token)) {
                throw new \RuntimeException('Invalid CSRF token');
            }
            // Validate and save settings here before reporting success.
            $this->admin->addMessage('success', 'Success', 'Settings saved');

            return Response::redirect($this->admin->url('/settings'));
        }

        $content = $this->admin->render('settings', [
            'saveUrl' => $this->admin->url('/settings'),
        ]);

        return new AdminResponse($content, [
            ['label' => 'Settings', 'url' => $this->admin->url('/settings')],
        ]);
    }
}
```

Admin route paths are matched against `$_REQUEST['endpoint']`. Build admin links with `$this->admin->url('/path')` instead of hard-coding `?mod=...&endpoint=...`.

`$admin->render('name', $params)` loads PHP views from the plugin `Views/` folder. A template name starting with `@` loads a shared framework view, for example `@form` or `@model/main`. These stock views require CSRF and output-escaping adaptations before use with user data; see [Models and forms](#models-and-forms).

Use these return types deliberately:

- return a string or `AdminResponse` for a full admin page wrapped by DLE's `echoheader()` and `echofooter()`;
- return `Core\Response::redirect()`, `json()`, `text()`, `file()`, or `image()` for direct responses that should send headers and exit;
- return `null` only when the route intentionally outputs nothing.

## Frontend routes and SiteResponse

For public site pages, register the URL with PHP `Router::add()` in an executed bootstrap and implement a controller that returns `SiteResponse`. The current CLI ignores `extras.routes`. See [extras-routes.md](extras-routes.md) for registration timing, cache and installed framework integration.

```php
namespace iCooLER\myplugin\Site;

use iCooLER\DLEPlugin\Core\Breadcrumbs\Breadcrumbs;
use iCooLER\DLEPlugin\Core\Seo\Seo;
use iCooLER\DLEPlugin\Core\SiteResponse;
use iCooLER\DLEPlugin\Core\TemplateString;

final class CatalogController
{
    public function show(array $args): SiteResponse
    {
        $id = (int) ($args['id'] ?? 0);

        $tpl = new TemplateString('<h1>{title}</h1><div>{content}</div>');
        $tpl->set('title', 'Catalog item '.$id);
        $tpl->set('content', 'Rendered content');

        return SiteResponse::html($tpl->get())
            ->withSeo(Seo::make()
                ->title('Catalog item '.$id)
                ->description('Catalog item page')
                ->build())
            ->withBreadcrumbs(Breadcrumbs::make()
                ->add('Home', '/')
                ->add('Catalog', '/catalog/')
                ->add('Item '.$id)
                ->build());
    }
}
```

DLEPlugin's route integration places `SiteResponse->content` into DLE content and applies SEO/breadcrumb metadata. Do not rely on plain strings for frontend route metadata; use `SiteResponse` when SEO or breadcrumbs matter.

## Template and TemplateString

`TemplateString` is a simple mutable renderer. `set()` uses `str_replace()` for `{name}` placeholders. Blocks and conditions are regex helpers layered on top; this is not Twig, Blade, or a full template language.

Common patterns:

```php
use iCooLER\DLEPlugin\Core\Template;
use iCooLER\DLEPlugin\Core\TemplateString;

$tpl = new TemplateString('[enabled]<b>{title}</b>[/enabled][not-enabled]Disabled[/not-enabled]');
$tpl->set('title', 'Hello', true);
$tpl->setCondition('enabled', true);
$html = $tpl->get();

$list = new TemplateString('<ul>[items]<li>{title}</li>[/items]</ul>');
$list->renderBlockWithTemplate('items', $items, function (TemplateString $row, int $key, array $item): void {
    $row->set('title', $item['title']);
});

$dleTpl = Template::newInstance();
$dleTpl->load('myplugin/catalog');
$dleTpl->set('items', $list->get());
$dleTpl->render();
$html = $dleTpl->getResult();
```

Important template rules:

- escape user-supplied strings before putting them into placeholders; `set()` does not escape HTML;
- do not invent unsupported template syntax;
- use `renderBlockWithTemplate()` for repeated `[block]...[/block]` sections;
- use `setText()` for `{name limit="120"}` trimming;
- use `setDate()` for `{date=...}` date formatting;
- use `Template::globalInstance()` only when intentionally modifying the current global DLE template.

## HTTP client

Use `Core\HTTP::request()` for practical GET/POST calls with query, JSON, form, multipart, proxy, timeout, cookies, or file saving options.

```php
use iCooLER\DLEPlugin\Core\HTTP;

$response = HTTP::request('POST', 'https://api.example.test/items', [
    'headers' => [
        'Authorization' => 'Bearer '.$token,
    ],
    'json' => [
        'title' => $title,
    ],
    'timeout' => 10,
    'connect_timeout' => 3,
    'verify' => true,
]);

if ($response['body'] === false || $response['http_code'] === 0) {
    throw new RuntimeException('API transport failed');
}
if ($response['http_code'] >= 400) {
    throw new RuntimeException('API request failed: '.$response['body']);
}

$data = json_decode((string) $response['body'], true);
```

Use `Core\HTTP\Client` with `Request` when a small object API is enough:

```php
use iCooLER\DLEPlugin\Core\HTTP\Client;
use iCooLER\DLEPlugin\Core\HTTP\Request;

$request = (new Request('https://api.example.test/status'))
    ->setHeaders(['Accept: application/json']);

$response = (new Client())->send($request);
$data = $response->json();
```

The object client currently supports URL, method, and headers only. Use `HTTP::request()` when the request needs a body.

## Config, cache, and logs

Use `App` services for plugin-scoped state.

```php
use iCooLER\DLEPlugin\Core\App;

$app = App::getApp('myplugin');

$apiKey = (string) $app->config->get(['api', 'key'], '');
$app->config->set(['api', 'key'], $newKey)->save();

$cached = $app->cache->get('catalog');
if ($cached === null) {
    $cached = loadCatalog();
    $app->cache->set('catalog', $cached, time() + 3600);
}

try {
    importCatalog();
} catch (Throwable $e) {
    $app->logger->error($e);
}
```

`Config` persists to `ENGINE_DIR/data/<app>.config.php`. `Cache` stores JSON payloads under `ENGINE_DIR/cache/<app>/`. `Cache::set($name, $value, $expires)` expects an absolute expiry timestamp; use `time() + seconds` or `0` for no expiration.

Use `DLECache` only when you specifically need DLE's global cache functions:

```php
use iCooLER\DLEPlugin\Core\DLECache;

$items = DLECache::useCallback('myplugin_items', fn () => buildItems());
```

## Database helpers

Use `Core\DB` for DLE database access with the project's table prefix placeholder `{}` and helper-generated conditions.

```php
use iCooLER\DLEPlugin\Core\DB;

$items = DB::select('{}_myplugin_items', [
    'columns' => ['id', 'title', 'active'],
    'where' => [
        ['active', '=', 1],
        ['owner_id', '=', $memberId],
    ],
    'order_by' => ['id' => 'DESC'],
    'limit' => 20,
]);

$id = DB::insert('{}_myplugin_items', [
    'title' => $title,
    'active' => 1,
]);

DB::update('{}_myplugin_items', ['title' => $title], [
    'where' => [['id', '=', $id]],
]);
```

Prefer placeholders and structured `where` arrays over concatenating request data into SQL. Do not pass user-controlled table names, column names, or order clauses; helpers escape values, not SQL identifiers chosen at runtime.

`DB::selectOne()` returns one row or `null`. `DB::executeOne()` and `DB::executeAll()` are useful for custom SQL with `?` placeholders. `DB::query()` only formats SQL; it does not execute it.

The `Core\DB\SelectQuery` and `InsertQuery` classes are placeholders in the current framework. Prefer `Core\DB` methods unless the project has extended those builders.

## Models and forms

Use `Core\Model\Model` to describe admin-editable data and `Core\Model\Form` to bind submitted POST data.

```php
namespace iCooLER\myplugin\Model;

use iCooLER\DLEPlugin\Core\Model\Model;

final class SettingsModel extends Model
{
    protected function init()
    {
        $this->string('api_key', 'API key');
        $this->boolean('enabled', 'Enabled', ['default' => true]);
        $this->integer('limit', 'Limit', ['default' => 20]);
    }
}
```

For a custom settings controller:

```php
use iCooLER\DLEPlugin\Core\App;
use iCooLER\DLEPlugin\Core\Model\Form;
use iCooLER\DLEPlugin\Core\Response;
use iCooLER\myplugin\Model\SettingsModel;

$form = new Form(SettingsModel::class);
$app = App::getApp($this->admin->slug);
$form->model->setData($app->config->get([]));

if ($form->isSubmitAndValid()) {
    global $dle_login_hash;
    $token = $_POST['user_hash'] ?? null;
    if (!is_string($token) || !is_string($dle_login_hash)
        || $dle_login_hash === '' || !hash_equals($dle_login_hash, $token)) {
        throw new \RuntimeException('Invalid CSRF token');
    }
    // Validate the model/input explicitly before persistence.
    $app->config->setArray($form->model->toArray())->save();

    return Response::redirect($this->admin->url('/settings'));
}

return $this->admin->render('settings', ['form' => $form]);
```

The supplied `Form::isSubmitAndValid()` binds POST data and returns true on submission; it does not validate CSRF or model rules. The stock `@form` view does not add `user_hash`. The `settings` view above must be supplied by the plugin and add an escaped hidden token (use the companion `dle-admin-ui` skill for native form markup when available) and apply explicit input/model validation. A save redirect alone does not enqueue a success message.

Use `Admin\ModelController` when the task is a straightforward CRUD admin table. Override `getUrl()`, `getTable()`, and `getModel()`, and adjust public properties like `$list_display`, `$order`, `$sort`, `$limit`, `$actions`, `$fields`, or `$exclude`.

### Adapting ModelController CRUD safely

`Admin\ModelController` and `@model/main` are starting points, not a secure CRUD implementation on their own. In the inspected framework, `save()` and `delete()` do not verify `user_hash`; stock `@form` and model/confirmation views do not supply a CSRF token. `getValue()` returns raw string values and item labels, and `@model/main` inserts them into HTML without escaping.

- Override/adapt save and delete handlers to verify a non-empty session token with `hash_equals()` before any mutation; reject missing, non-string or invalid tokens. Do not call parent persistence before that check. Validate input explicitly as well.
- Supply custom form and delete-confirmation views with an HTML-escaped hidden `user_hash`, and include the token in any mutating links. A client-side confirmation is not CSRF protection.
- Use a custom list view that escapes string values, item labels, field labels and URL attributes with `htmlspecialchars((string) $value, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8')`. Keep trusted framework boolean/icon markup separate from untrusted strings. Do not globally escape `getValue()` if it is also used for raw IDs or URLs; cast IDs and construct URLs separately.
- Allowlist sort fields/directions rather than passing request-controlled SQL identifiers through generic CRUD defaults.

Use the companion [dle-admin-ui](../../dle-admin-ui/SKILL.md) skill for the adapted native markup.

## Common pitfalls

- Do not treat `TemplateString` as a full parser. Its basic placeholder replacement is `str_replace()`.
- Do not expect template placeholders to escape output automatically.
- Do not return `Core\Response` from frontend site routes unless the surrounding entry point explicitly sends it. Use `SiteResponse` for frontend pages.
- Do not add DI bindings for value objects, DTOs, entities, templates, SQL strings, or config arrays.
- Do not use `Core\HTTP\Client` for POST bodies; use `Core\HTTP::request()`.
- Do not pass raw request values as SQL identifiers or `order_by` strings.
- Do not edit generated `dist/` output as the source of truth.
- Use CLI-managed static/test tooling; follow the project dependency policy rather than installing project Composer tools for those checks.


## Completion checklist

Before finishing framework-based plugin work, confirm:

- the plugin slug, namespace and mount were read/resolved from `dleplugin.yml`;
- framework classes are imported from `iCooLER\DLEPlugin`, not copied into plugin code;
- admin routes use `$admin->add()` and admin links use `$admin->url()`;
- frontend routes return `SiteResponse` when content, SEO, or breadcrumbs should be handled by DLEPlugin;
- template output containing user content is escaped by the plugin before replacement;
- SQL values use helper placeholders or structured `where` arrays;
- config writes call `save()`;
- relevant static analysis and tests were run or the reason they could not run is reported.
