# DLE Admin Panel UI

## Contents

- [Core rules](#core-rules)
- [Inline code and scoped CSS](#inline-code-and-scoped-css)
- [Page shell (DLEPlugin vs raw)](#page-shell-dleplugin-vs-raw)
- [Plugin home: section directory](#plugin-home-section-directory)
- [Two form layouts](#two-form-layouts)
- [Controls catalog](#controls-catalog)
- [Buttons](#buttons)
- [Modals](#modals)
- [Panels](#panels)
- [Alerts / notices](#alerts--notices)
- [List / CRUD table](#list--crud-table)
- [Tabs and section toolbars](#tabs-and-section-toolbars)
- [Icons](#icons)
- [DLEPlugin-first workflow for plugin admin UI](#dleplugin-first-workflow-for-plugin-admin-ui)
- [Conditional fields](#conditional-fields)
- [Accessibility / i18n notes](#accessibility--i18n-notes)
- [Anti-patterns](#anti-patterns)
- [Quick checklist before finishing admin UI work](#quick-checklist-before-finishing-admin-ui-work)

Use this reference when building or reviewing **admin UI** for DLE plugins or classic `engine/inc` modules. Native DLE admin (Limitless/Bootstrap stack in `engine/skins`) is the visual source of truth. Match its HTML/CSS/JS patterns so plugin screens look native.

Reference sources in the CMS tree:

| Path | Role |
|---|---|
| `engine/inc/*.php` | Real admin screens: settings rows, lists, forms, buttons |
| `engine/inc/include/functions.inc.php` | `echoheader()`, `echofooter()`, `msg()`, `get_groups()` |
| `engine/skins/default.skin.php` | Shell layout (do not reimplement) |
| `public/adminpanel/stylesheets/application.css` | Admin theme styles |
| `public/adminpanel/javascripts/application.js` | `uniform`, switch, chosen init, `DLEconfirm`, `DLEconfirmDelete`, `DLEPush`, calendars |
| `engine/iCooLER/DLEPlugin/Core/AdminHTML.php` | Framework HTML helpers with DLE classes |
| `engine/iCooLER/DLEPlugin/Views/` | Built-in form/list views (`@form`, `@model`, `@model/main`) |


## Core rules

1. **Reuse DLE classes** (`panel`, `form-control`, `btn bg-teal`, `switch`, `uniform`). Do not invent a parallel design system.
2. **Escape output**: titles, labels, option text, and user data go through `htmlspecialchars(..., ENT_QUOTES, 'UTF-8')` (or equivalent) before echo.
3. **Always include CSRF** on POST forms and mutating GET links:
   ```html
   <input type="hidden" name="user_hash" value="{$dle_login_hash}" />
   ```
   In PHP verify: `$_REQUEST['user_hash'] === $dle_login_hash`.
4. **Prefer DLEPlugin views/helpers** when the project already uses the framework; fall back to raw HTML only when you need a custom layout.
5. **Do not load extra CSS/JS frameworks** unless the user explicitly asks. Admin already loads Font Awesome, Bootstrap-style panels, Selectpicker (via `uniform` class), Switchery (`.switch`), Chosen, datetimepicker, popovers.

## Inline code and scoped CSS

Do not assume `<code>` renders inline in the installed admin theme. The inspected `public/adminpanel/stylesheets/application.css` contains `code,pre{display:block; ... white-space:pre; ...}`, so a template tag or short example inside a sentence starts a separate block. Inspect the actual theme and cascade before changing styles.

For DLETemplateVars, scope the correction to its own content:

```html
<style>
  #dtv-admin code { display: inline; }
</style>
<div id="dtv-admin">
  <p>Use <code>{template-var}</code> inside the template.</p>
  <pre><code>First line
Second line</code></pre>
</div>
```

Use this wrapper around plugin-owned content on each rendered screen: the list, help, and the separate form page. Keep the wrapper inside the existing DLE shell: wrap the view content returned in `AdminResponse`, or content between `echoheader()` and `echofooter()` in a classic module. Do not wrap or restyle admin headers, navigation, breadcrumbs, footer, or other modules. Use a plugin-specific wrapper for other plugins; never fix this with global `code` / `pre` rules or edits to the shared theme stylesheet.

Short inline examples belong in `<code>` within the surrounding sentence. Intentional multiline examples belong in `<pre><code>…</code></pre>`: the `<pre>` stays block-level and preserves line breaks even though its child `<code>` is inline. If existing markup deliberately uses a standalone `<code>` as a multiline block, convert it to `<pre><code>` or give it a dedicated plugin-scoped block class. The inline correction only changes `display`; inspect inherited/theme `white-space`, padding, and overflow separately if wrapping or spacing still needs adjustment.

Verify on the rendered admin page with the actual theme and plugin styles loaded, not just from the HTML tag or a CSS source search. In DevTools select an inline example and check **Computed → display**, or run `getComputedStyle(document.querySelector('#dtv-admin p code')).display` with a selector matching the real example; expect `inline`. Inspect matched rules to confirm the scoped override wins over the theme. Check the list, help, and separate form page, including any later-loaded styles. Confirm sentences flow naturally, multiline examples retain line breaks with a block-level `<pre>`, and shell/other-module code outside `#dtv-admin` retains its previous computed style.

## Page shell (DLEPlugin vs raw)

### With DLEPlugin (preferred for plugins)

Controller returns `AdminResponse` or a string; the framework wraps with `echoheader()` / `echofooter()`.

```php
return new AdminResponse($html, [
    ['label' => 'Settings', 'url' => $this->admin->url('/settings')],
]);
```

Header title/icon is set by the admin section / response breadcrumbs. Build links with `$this->admin->url('/path')`, never hard-code `?mod=...` unless matching an existing DLE mod.

### Raw DLE `engine/inc` style (only if writing classic mod pages)

```php
echoheader(
    '<i class="fa fa-cogs position-left"></i><span class="text-semibold">Plugin name</span>',
    $lang['page_subtitle'] // or breadcrumb array: ['?mod=x' => 'Parent', '' => 'Current']
);

// ... page HTML ...

echofooter();
```

Success/error full-page message:

```php
msg('success', $lang['opt_sysok'], $lang['opt_sysok_1'], '?mod=myplugin');
msg('error', $lang['index_denied'], $lang['index_denied']);
// types: error | warning | success | info (default)
// $back: URL string or [url => label, ...] for multiple buttons
```

## Plugin home: section directory

For a plugin with several independent admin sections, the home page is **only a directory of section links**. Settings forms, CRUD tables, statistics, and their actions belong inside the selected section. Do not put them below the directory, preselect a section on the home page, or replace the directory with tabs. Tabs and options-style filters remain appropriate **within a page/section**, for example two groups of settings saved by one form.

Use the native hierarchy exactly: `panel panel-default` → `panel-heading` + `list-bordered` → `row box-section` → `col-sm-6 media-list media-list-linked` → `a.media-link` → `media-left` + `media-body`. The image uses `img-lg section_icon`. Each row holds at most two sections; close the row after the second item and close a final one-item row normally, without an empty placeholder column. No custom CSS, card system, or extra JS is needed.

### Complete markup (three sections, two rows)

This classic-module example uses `myplugin` as a placeholder for the actual mod name, not a dependency on a particular plugin. At `admin.php?mod=myplugin`, render only this directory inside the existing DLE shell:

```html
<div class="panel panel-default">
  <div class="panel-heading">Разделы плагина</div>
  <div class="list-bordered">
    <div class="row box-section">
      <div class="col-sm-6 media-list media-list-linked">
        <a class="media-link" href="?mod=myplugin&amp;section=settings">
          <div class="media-left"><img src="public/adminpanel/images/default_icon.png" class="img-lg section_icon" alt=""></div>
          <div class="media-body">
            <h6 class="media-heading text-semibold">Настройки</h6>
            <span class="text-muted">Группы пользователей и гостей, которым разрешено ставить реакции.</span>
          </div>
        </a>
      </div>
      <div class="col-sm-6 media-list media-list-linked">
        <a class="media-link" href="?mod=myplugin&amp;section=reactions">
          <div class="media-left"><img src="public/adminpanel/images/default_icon.png" class="img-lg section_icon" alt=""></div>
          <div class="media-body">
            <h6 class="media-heading text-semibold">Реакции</h6>
            <span class="text-muted">Добавление, редактирование и удаление доступных реакций.</span>
          </div>
        </a>
      </div>
    </div>
    <div class="row box-section">
      <div class="col-sm-6 media-list media-list-linked">
        <a class="media-link" href="?mod=myplugin&amp;section=stats">
          <div class="media-left"><img src="public/adminpanel/images/default_icon.png" class="img-lg section_icon" alt=""></div>
          <div class="media-body">
            <h6 class="media-heading text-semibold">Статистика</h6>
            <span class="text-muted">Количество реакций и популярные материалы за выбранный период.</span>
          </div>
        </a>
      </div>
    </div>
  </div>
</div>
```

### PHP generation and classic-module routing

Keep the existing DLE access checks and shell. The example below runs after authorization in the classic `engine/inc/myplugin.php` entry. The three fixed view files contain the actual settings form, reactions CRUD, and statistics respectively; adapt their paths to the project. They output section content only, without another header/footer. Never derive an include path from request data.

```php
$escape = static function (string $value): string {
    return htmlspecialchars($value, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
};
$url = static function (string $section = ''): string {
    $query = ['mod' => 'myplugin']; // Use the project's actual mod name.
    if ($section !== '') {
        $query['section'] = $section;
    }
    return '?' . http_build_query($query, '', '&', PHP_QUERY_RFC3986);
};
$sections = [
    'settings' => ['Настройки', 'Группы пользователей и гостей, которым разрешено ставить реакции.'],
    'reactions' => ['Реакции', 'Добавление, редактирование и удаление доступных реакций.'],
    'stats' => ['Статистика', 'Количество реакций и популярные материалы за выбранный период.'],
];
$section = $_GET['section'] ?? '';
if (!is_string($section) || ($section !== '' && !isset($sections[$section]))) {
    msg('error', 'Ошибка', 'Неизвестный раздел', $url());
    return;
}

echoheader('Мой плагин', '');
if ($section === '') {
    echo '<div class="panel panel-default"><div class="panel-heading">Разделы плагина</div>'
        . '<div class="list-bordered">';
    foreach (array_chunk($sections, 2, true) as $row) {
        echo '<div class="row box-section">';
        foreach ($row as $key => [$title, $description]) {
            echo '<div class="col-sm-6 media-list media-list-linked">'
                . '<a class="media-link" href="' . $escape($url($key)) . '">'
                . '<div class="media-left"><img src="public/adminpanel/images/default_icon.png"'
                . ' class="img-lg section_icon" alt=""></div><div class="media-body">'
                . '<h6 class="media-heading text-semibold">' . $escape($title) . '</h6>'
                . '<span class="text-muted">' . $escape($description) . '</span>'
                . '</div></a></div>';
        }
        echo '</div>'; // Also closes the final incomplete row.
    }
    echo '</div></div>';
} else {
    echo '<a href="' . $escape($url()) . '">К разделам плагина</a>';
    switch ($section) {
        case 'settings': require __DIR__ . '/myplugin/views/settings.php'; break;
        case 'reactions': require __DIR__ . '/myplugin/views/reactions.php'; break;
        case 'stats': require __DIR__ . '/myplugin/views/stats.php'; break;
    }
}
echofooter();
```

Construct URLs first, then escape once at the HTML output boundary: dynamic titles and descriptions are text, and URLs in quoted `href`, image paths in `src`, and any nonempty `alt` are attributes, all escaped with `htmlspecialchars(..., ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8')`. `http_build_query()` encodes query values; it does not replace HTML escaping (`&` must become `&amp;` in markup). Use trusted internal URL builders/allowlisted destinations, not arbitrary user-supplied schemes. The standard image above is decorative beside a visible title, so `alt=""` is intentional; escape a translated/dynamic alt if a meaningful image needs one. Use the project's language strings for the heading, titles, descriptions, error, and back link, escaping them on output too. These directory links only navigate; mutating operations inside sections still require server-verified `user_hash`.

### DLEPlugin framework integration

Reuse the same directory markup/generation in a PHP view (for example `directory.php`), with the metadata and `$escape` above. Supply its URL callable from the controller via `$admin->url()`; do not copy the classic query builder or `echoheader()` / `echofooter()` into a framework controller/view. Register the home and three section routes in the executed admin bootstrap, following the existing namespace and controller conventions:

```php
$admin->add('/', [CatalogController::class, 'index']);
$admin->add('/settings', [SettingsController::class, 'index']);
$admin->add('/reactions', [ReactionsController::class, 'index']);
$admin->add('/stats', [StatsController::class, 'index']);
$admin->run();
```

Inside `CatalogController` extending `iCooLER\DLEPlugin\Core\Controller`, import `iCooLER\DLEPlugin\Core\AdminResponse` and return only the directory view:

```php
public function index(array $args)
{
    $url = function (string $section = ''): string {
        return $this->admin->url($section === '' ? '/' : '/' . $section);
    };
    return new AdminResponse($this->admin->render('directory', ['url' => $url]));
}
```

The directory view uses the `$sections` array and the home branch's rendering loop above, with no request dispatch or section includes. Each section controller renders its own content and supplies `backUrl => $this->admin->url('/')` to its view; render `<a href="<?= $escape($backUrl) ?>">К разделам плагина</a>` there, with the same escaping helper in scope. The framework supplies the page shell around `AdminResponse`.

Classic modules use `section=settings|reactions|stats`; the existing framework router matches **`endpoint`**, so its equivalent URLs come from `$admin->url('/settings')`, `$admin->url('/reactions')`, and `$admin->url('/stats')`. Do not pretend `section` drives that router. If a project explicitly requires public `section=...` URLs with the framework, inspect and implement an explicit allowlisted adapter in its entrypoint; keep internal links through `$admin->url()` according to the project's routing integration. For normal framework projects, preserve native endpoint routing.

## Two form layouts

Pick one layout per screen and stick to it.

### A) Settings table (most system options pages)

Used in `social.php`, `videoconfig.php`, `options.php`, `storage.php` edit form.

Structure:

```html
<form method="post" action="...">
  <div class="panel panel-default">
    <div class="panel-heading">Section title</div>
    <div class="table-responsive">
      <table class="table table-striped">
        <thead>
          <tr>
            <th>Option</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <!-- rows -->
        </tbody>
      </table>
    </div>
    <div class="panel-footer">
      <button type="submit" class="btn bg-teal btn-sm btn-raised position-left">
        <i class="fa fa-floppy-o position-left"></i>Save
      </button>
    </div>
  </div>
  <input type="hidden" name="user_hash" value="..." />
</form>
```

**Row helper** (copy into plugin admin code or view; DLE redefines this per file):

```php
function showRow(string $title = '', string $description = '', string $field = '', string $class = ''): void
{
    $trClass = $class !== '' ? ' class="'.htmlspecialchars($class, ENT_QUOTES, 'UTF-8').'"' : '';
    echo '<tr'.$trClass.'>
        <td class="col-xs-6 col-sm-6 col-md-7">
            <div class="media-heading text-semibold">'.$title.'</div>
            <span class="text-muted text-size-small hidden-xs">'.$description.'</span>
        </td>
        <td class="col-xs-6 col-sm-6 col-md-5">'.$field.'</td>
    </tr>';
}
```

Use `$class` on `<tr>` for show/hide groups (`ftp-server`, `ai-params`, …) and toggle with jQuery.

### B) Horizontal form (add/edit entity screens)

Used in `editvote.php` and similar:

```html
<form class="form-horizontal" method="post" action="...">
  <div class="panel panel-default">
    <div class="panel-heading">Title</div>
    <div class="panel-body">
      <div class="form-group">
        <label class="control-label col-md-2 col-sm-3">Label</label>
        <div class="col-md-10 col-sm-9">
          <input type="text" dir="auto" name="title" class="form-control width-500" value="...">
          <i class="help-button visible-lg-inline-block text-primary-600 fa fa-question-circle position-right position-left"
             data-rel="popover" data-trigger="hover" data-placement="auto right"
             data-content="Hint text"></i>
        </div>
      </div>
    </div>
    <div class="panel-footer">
      <button type="submit" class="btn bg-teal btn-sm btn-raised">Save</button>
    </div>
  </div>
  <input type="hidden" name="user_hash" value="..." />
</form>
```

DLEPlugin `AdminHTML` mirrors this with `formGroup` / `formGroupInfo` / `formGroupField` and `col-sm-3` + `col-sm-8`.

## Controls catalog

### Text input

```html
<input type="text" dir="auto" class="form-control" name="save_con[key]" value="...">
```

Narrow / centered number-like:

```html
<input type="text" dir="auto" class="form-control" style="max-width:150px; text-align:center;" name="..." value="...">
```

Width utilities used in core: `width-350`, `width-500`, `width-550`, `position-left`, `position-right`, `mb-10`.

Always set `dir="auto"` on free-text fields (core convention).

### Textarea

```html
<textarea dir="auto" class="form-control" style="height:150px;" name="body">...</textarea>
```

Older style still present: `class="classic width-500"`.

### Checkbox switch (boolean)

Native DLE switch (Switchery). Class **`switch`**, value `1`:

```php
function makeCheckBox(string $name, $selected, string $optional = ''): string
{
    $checked = $selected ? 'checked' : '';

    return '<input class="switch" type="checkbox" name="'.htmlspecialchars($name, ENT_QUOTES, 'UTF-8').'" value="1" '.$checked.' '.$optional.'>';
}
```

On save, unchecked boxes are absent from POST — default to `0`:

```php
$enabled = isset($_POST['enabled']) ? (int) $_POST['enabled'] : 0;
```

DLEPlugin boolean fields often use hidden + switch so unchecked still posts `0` (see framework `@model` view). Prefer that pattern for Model/Form.

Do **not** use plain Bootstrap checkboxes for settings unless matching a special screen.

### Single-select (`uniform` / `form-control`)

A single-value `<select>` uses one of two class strategies:

- **`class="uniform"`** — the native DLE look. `application.js` runs `$('select.uniform').selectpicker()` on load, turning it into a Bootstrap-Select (Selectpicker) widget. Use this by default.
- **`class="uniform form-control"`** — same Selectpicker widget, but the select also carries Bootstrap's `form-control` sizing. Use it when the select must line up with adjacent `form-control` inputs, e.g. inside a `form-horizontal` grid (`editusers.php` order selects) or an `input-group-btn` (`editnews.php` / `static.php` search).
- **`class="form-control"`** alone — plain Bootstrap styling with **no** Selectpicker. Only when you deliberately skip the themed widget (framework-generated selects); the native DLE admin convention is `uniform`.

Selectpicker sizes to content by default, so set a width explicitly:

- `data-width="100%"` to fill the column (most common, `editnews.php`, `categories.php`).
- or a fixed `data-width="350"` / inline `style="width:100%;max-width:350px;"`.

Other Selectpicker attributes used in core:

| Attribute | Purpose |
|---|---|
| `data-live-search="true"` | search box for long lists (categories, timezones) |
| `data-none-results-text="..."` | no-match message |
| `data-dropdown-align-right="true"` (or `"auto"`) | align the menu to the right |
| `position-left` / `position-right` | spacing for inline selects next to a button |

Builder helper:

```php
function makeDropDown(array $options, string $name, $selected, string $attrs = ''): string
{
    $html = '<select class="uniform" name="'.htmlspecialchars($name, ENT_QUOTES, 'UTF-8').'" '.$attrs.'>';
    foreach ($options as $value => $label) {
        $sel = ((string) $selected === (string) $value) ? ' selected' : '';
        $html .= '<option value="'.htmlspecialchars((string) $value, ENT_QUOTES, 'UTF-8').'"'.$sel.'>'
            .htmlspecialchars((string) $label, ENT_QUOTES, 'UTF-8').'</option>';
    }

    return $html.'</select>';
}
```

Inline action select (mass action paired with a button):

```html
<select class="uniform position-left" name="action" data-dropdown-align-right="auto">
  <option value="">— action —</option>
  <option value="mass_delete">Delete</option>
</select>
```

When options change after load, **refresh Selectpicker** — do not use `$.uniform.update()` here (that re-renders `.icheck` radios/checkboxes, not selects):

```js
$('select.uniform').selectpicker('refresh');   // or .selectpicker() to re-init a fresh element
```

### Multi-select (Chosen)

Multi-value selects use the **Chosen** plugin, not `uniform`/Selectpicker. Requirements:

- `multiple` attribute and a `name` ending in `[]`.
- A page-specific class that Chosen binds to — core uses `cat_select`, `categoryselect`, `groupselect`, `empfangerselect`.
- `data-placeholder="..."` for the empty hint.
- Explicit initialization in a small inline script (Chosen is **not** auto-initialized by `application.js`).

```html
<select name="groups[]" class="cat_select" data-placeholder="Choose groups" style="width:100%; max-width:350px;" multiple>
  <option value="all">All</option>
  <option value="1">Editors</option>
  <option value="2">Authors</option>
</select>
<script>
$(function () {
  $('.cat_select').chosen({
    allow_single_deselect: true,
    no_results_text: 'Nothing found'
  });
});
</script>
```

Do **not** add `uniform` or `form-control` to a `multiple` select — that is the Selectpicker path and is not the DLE convention for multi-value fields. After changing options, fire `chosen:updated`:

```js
$('.cat_select').trigger('chosen:updated');
```

On pages with several multi-selects, initialize every hook class the markup uses (e.g. both `.cat_select` and `.categoryselect`) so none render as a raw list.

### Date / datetime

```html
<input data-rel="calendar" type="text" dir="auto" name="start_date"
       class="form-control" style="width:190px;" value="..." autocomplete="off">
```

Other `data-rel` values initialized in `application.js`:

| Attribute | Purpose |
|---|---|
| `calendar` | datetime |
| `calendardate` | date only |
| `calendartime` | time only |
| `calendardatetime` | full datetime |

### Help popover

```html
<i class="help-button visible-lg-inline-block text-primary-600 fa fa-question-circle position-right"
   data-rel="popover" data-trigger="hover" data-placement="auto right"
   data-content="Explanation"></i>
```

### Status icons (list columns)

```html
<span title="On" class="text-success tip"><b><i class="fa fa-check-circle"></i></b></span>
<span title="Off" class="text-danger tip"><b><i class="fa fa-exclamation-circle"></i></b></span>
```

## Buttons

**Hard rule for plugin admin UI: every action button must carry `btn` + exactly one `bg-*` color + `btn-sm` + `btn-raised`.** Never emit a bare `btn` or a button with no background color — it looks broken next to the core. Native modal `.close` dismiss controls retain their stock markup and are exempt. This applies to action `<button>`, `<input type="submit|button|reset">`, and link-as-button `<a>`.

Primary pattern:

```html
<button type="submit" class="btn bg-teal btn-sm btn-raised position-left">
  <i class="fa fa-floppy-o position-left"></i>Save
</button>
```

> Core note: a few legacy core pages omit `btn-sm` (e.g. `options.php`, `social.php`). Do **not** copy that — plugin screens must always include `btn-sm`.

Common color classes (Limitless palette used in core):

| Class | Typical use | Real example |
|---|---|---|
| `bg-teal` | Primary save / confirm / add / search | `editnews.php` save, `blockip.php` modal save |
| `bg-danger` | Delete / clear / reset / destructive | `editnews.php` clear search, delete |
| `bg-grey-400` | Cancel / reset / neutral dismiss | `blockip.php`, `categories.php` modal cancel |
| `bg-slate-600` | Secondary (preview, back, alternate action) | `editnews.php` preview, `templates.php` select |
| `bg-slate-400` | Default DLEPlugin `AdminHTML::button` | framework default |
| `bg-primary-600` | Alternative primary (auto-fill, extra actions) | `editnews.php` auto keywords |
| `bg-info-800` | Find / info side actions | `plugins.php` check update |
| `bg-brown-600` | Mass actions / spam-like / flush | mass action "Go", `comments.php` spam |

Sizes/modifiers: `btn-sm` (always), `btn-raised` (always in core), `position-left` / `position-right`, plus spacing helpers `mt-5`, `mb-5`, `mt-15`.

Icon placement: `<i class="fa fa-* position-left"></i>` inside the button, text after it.

Link-as-button (same classes):

```html
<a href="..." class="btn bg-teal btn-sm btn-raised position-left">
  <i class="fa fa-plus-circle position-left"></i>Add
</a>
```

Buttons that open a modal use either `data-toggle="modal"` (see [Modals](#modals)) or a JS call such as `onclick="$('#newcats').modal({backdrop: 'static',keyboard: false}); return false;"` (`categories.php`).

## Modals

DLE admin uses **Bootstrap 3 modals**: `modal fade` → `modal-dialog` (add `modal-lg` for wide content) → `modal-content`. Modals are emitted once (usually at the end of the page output), hidden by default, and opened by a trigger elsewhere on the page. Real references: `engine/inc/editnews.php` (advanced search, lines 429–585), `blockip.php` (add form, lines 175–218), `categories.php`, `wordfilter.php`, `plugins.php`, `editusers.php`.

### Trigger

```html
<!-- link trigger, e.g. in a panel heading-elements list -->
<li>
  <a href="#" data-toggle="modal" data-target="#advancedsearch">
    <i class="fa fa-search position-left"></i>Advanced search
  </a>
</li>

<!-- button trigger, e.g. in a panel-footer / toolbar -->
<button type="button" data-toggle="modal" data-target="#advancedadd"
        class="btn bg-teal btn-sm btn-raised position-left">
  <i class="fa fa-plus position-left"></i>Add
</button>
```

Rules:

- `data-target` must equal the modal root `id` (with `#`). Keep ids unique per page.
- No custom JS is needed for open/close — only `data-toggle="modal"` / `data-dismiss="modal"`.
- Optionally open from JS: `$('#newcats').modal({backdrop: 'static', keyboard: false})`.

### Skeleton (read-only / info modal)

```html
<div class="modal fade" id="advancedsearch" role="dialog" aria-labelledby="advancedsearchLabel">
  <div class="modal-dialog modal-lg" role="document">
    <div class="modal-content">
      <div class="modal-header ui-dialog-titlebar">
        <button type="button" class="close" data-dismiss="modal" aria-label="Close">
          <span aria-hidden="true">&times;</span>
        </button>
        <span class="ui-dialog-title" id="advancedsearchLabel">Title</span>
      </div>
      <div class="modal-body">
        <!-- content -->
      </div>
      <div class="modal-footer">
        <button type="button" class="btn bg-grey-400 btn-sm btn-raised" data-dismiss="modal">Cancel</button>
        <button type="button" class="btn bg-teal btn-sm btn-raised position-left">
          <i class="fa fa-search position-left"></i>Apply
        </button>
      </div>
    </div>
  </div>
</div>
```

### Form modal (add / edit / upload)

The `<form>` wraps header, body **and** footer so all inputs submit together. Add `user_hash` for POST. When the form uses `form-horizontal`, core adds `style="margin-top:-20px;"` to the footer.

```html
<div class="modal fade" id="newblock" tabindex="-1" role="dialog" aria-labelledby="newblockLabel">
  <div class="modal-dialog modal-lg" role="document">
    <div class="modal-content">
      <form method="post" action="" class="form-horizontal">
        <input type="hidden" name="mod" value="blockip">
        <input type="hidden" name="action" value="add">
        <input type="hidden" name="user_hash" value="{$dle_login_hash}">

        <div class="modal-header ui-dialog-titlebar">
          <button type="button" class="close" data-dismiss="modal" aria-label="Close">
            <span aria-hidden="true">&times;</span>
          </button>
          <span class="ui-dialog-title" id="newblockLabel">Add record</span>
        </div>

        <div class="modal-body">
          <div class="form-group">
            <label class="control-label col-sm-4">IP</label>
            <div class="col-sm-8">
              <textarea dir="auto" class="classic" style="width:100%" rows="5" name="ip_add"></textarea>
            </div>
          </div>
        </div>

        <div class="modal-footer" style="margin-top:-20px;">
          <button type="button" class="btn bg-grey-400 btn-sm btn-raised" data-dismiss="modal">Cancel</button>
          <button type="submit" class="btn bg-teal btn-sm btn-raised position-left">
            <i class="fa fa-floppy-o position-left"></i>Save
          </button>
        </div>
      </form>
    </div>
  </div>
</div>
```

Modal footer button rules:

- **Cancel / dismiss**: `btn bg-grey-400 btn-sm btn-raised` with `data-dismiss="modal"`, placed first.
- **Primary action**: `btn bg-teal btn-sm btn-raised position-left` with an icon.
- Destructive confirm inside a modal: `bg-danger`.

Modal body field layout: either `control-label col-sm-4` + `col-sm-8` (form modal), or `form-group > .row > .col-sm-6` / `.col-sm-12` for two-column layouts (`editnews.php` advanced search). Use the same controls as elsewhere (`form-control`, `uniform`, `data-rel="calendar"`, Chosen).

### Confirm dialogs

Destructive actions in core do **not** build a modal by hand — they use jQuery UI helpers (`public/adminpanel/javascripts/application.js`):

```js
DLEconfirmDelete('Delete this item?', 'Confirm', function () {
  document.location = url; // include user_hash in the URL
});

DLEconfirm('Are you sure?', 'Confirm', function () { /* ... */ });
```

For a raw button, call the helper in `onclick` and `return false;`. The delete variant renders a red `ui-button-delete` button automatically.

### Modals checklist

- Trigger `data-target` matches the modal root `id`; ids unique on the page.
- Modal root has `modal fade` + `role="dialog"` and a fully structured `modal-dialog > modal-content`.
- Header uses `modal-header ui-dialog-titlebar` with a `.close` dismiss button and a `ui-dialog-title` heading.
- Form modals wrap all three sections in one `<form>` and include `user_hash`.
- Footer buttons follow the `btn … btn-sm btn-raised` rule (`bg-grey-400` cancel + `bg-teal` primary).
- Prefer `DLEconfirm` / `DLEconfirmDelete` over hand-built confirm modals.

## Panels

Standard card:

```html
<div class="panel panel-default">
  <div class="panel-heading">...</div>
  <div class="panel-body">...</div>          <!-- free content / form-horizontal -->
  <div class="table-responsive">...</div>  <!-- often instead of panel-body for tables -->
  <div class="panel-footer">...</div>
</div>
```

Settings sections with internal nav: `panel panel-flat` + optional `panel-body border-bottom` as subsection title (see `options.php`).

DLEPlugin helpers:

```php
use iCooLER\DLEPlugin\Core\AdminHTML as HTML;

echo HTML::panel([
    HTML::panelHeading('Settings'),
    HTML::panelBody($fieldsHtml),
    HTML::panelFooter([
        '<button type="submit" class="btn btn-sm bg-teal btn-raised">Save</button>',
    ]),
]);
```

Default `AdminHTML` classes: `form` → `form-horizontal`, `input` → `form-control`, `button` → `btn btn-sm bg-slate-400`, `select` → `uniform`. The helper **appends** these defaults to any supplied class; passing `bg-teal` does not remove `bg-slate-400`. Use raw button HTML (as above) or base `Core\HTML` for exactly one color. The preferred one-color/raised-button style is a UI convention, not something enforced by the CLI.

## Alerts / notices

```html
<div class="alert alert-info alert-styled-left alert-arrow-left alert-component">Info text</div>
<div class="alert alert-warning alert-styled-left alert-arrow-left alert-component">Warning</div>
<div class="alert alert-danger alert-styled-left alert-arrow-left alert-component">Error</div>
<div class="alert alert-success alert-styled-left alert-arrow-left alert-component">Success</div>
```

In-page toast (no full reload):

```js
DLEPush.info('Saved');
DLEPush.warning('Check fields');
DLEPush.error('Failed');
```

## List / CRUD table

Pattern from `editvote.php`, `storage.php`, `plugins.php`:

```html
<div class="panel panel-default">
  <div class="panel-heading">Items</div>
  <div class="table-responsive">
    <table class="table table-xs table-hover">
      <thead>
        <tr>
          <th>Title</th>
          <th class="text-center hidden-xs">Meta</th>
          <th class="text-center">Status</th>
          <th style="width: 4.375rem">&nbsp;</th>
        </tr>
      </thead>
      <tbody>
        <tr>
          <td class="cursor-pointer" onclick="document.location='...';return false;">
            <a href="...">Title</a>
          </td>
          <td class="text-center hidden-xs">...</td>
          <td class="text-center">...</td>
          <td class="text-center"><!-- actions menu --></td>
        </tr>
      </tbody>
    </table>
  </div>
  <div class="panel-footer">
    <a href="..." class="btn bg-teal btn-sm btn-raised">
      <i class="fa fa-plus-circle position-left"></i>Add
    </a>
  </div>
</div>
```

Empty state: one row with `colspan` and centered text.

### Row action dropdown

```html
<div class="btn-group">
  <a href="#" class="dropdown-toggle nocolor" data-toggle="dropdown" aria-expanded="true">
    <i class="fa fa-bars"></i><span class="caret"></span>
  </a>
  <ul class="dropdown-menu text-left pull-right">
    <li><a href="..."><i class="fa fa-pencil-square-o position-left"></i>Edit</a></li>
    <li><a href="..."><i class="fa fa-eye position-left"></i>View</a></li>
    <li class="divider"></li>
    <li>
      <a href="#" onclick="confirmDelete('...');return false;">
        <i class="fa fa-trash-o position-left text-danger"></i>Delete
      </a>
    </li>
  </ul>
</div>
```

### Confirm dialogs

```js
function confirmDelete(url) {
  DLEconfirmDelete('Delete this item?', 'Confirm', function () {
    document.location = url; // include user_hash in URL
  });
}

// Non-delete confirm:
DLEconfirm('Are you sure?', 'Confirm', function () { ... });
```

### Mass actions footer

```html
<select class="uniform position-left" name="action">
  <option value="">— action —</option>
  <option value="mass_enable">Enable</option>
  <option value="mass_delete">Delete</option>
</select>
<input class="btn bg-brown-600 btn-sm btn-raised" type="submit" value="Go">
```

## Tabs and section toolbars

Use these patterns within a page or section. They do not replace the [section directory on the plugin home page](#plugin-home-section-directory).

### Bootstrap tabs (in panel heading)

```html
<div class="panel-heading">
  <ul class="nav nav-tabs nav-tabs-solid">
    <li class="active"><a href="#tab1" data-toggle="tab">Tab 1</a></li>
    <li><a href="#tab2" data-toggle="tab">Tab 2</a></li>
  </ul>
</div>
<div class="panel-tab-content tab-content">
  <div class="tab-pane active" id="tab1">...</div>
  <div class="tab-pane" id="tab2">...</div>
</div>
```

Wrap the heading and tab content in `.panel.panel-default`. For a tabbed form, one `<form>` must wrap all panes so their inputs submit together. Exactly one nav item and its pane are active; every `href="#id"` matches a unique pane id. Use `.panel-body` inside panes and the standard horizontal form grid for fields. Bootstrap's `data-toggle="tab"` handles switching without custom JavaScript. The optional `heading-elements` / `panel-fullscreen` block may be added to the heading.

### Options-style navbar filter (multi-section settings)

```html
<div class="navbar navbar-default navbar-component navbar-xs">
  <div class="navbar-collapse collapse" id="navbar-filter">
    <ul class="nav navbar-nav">
      <li class="active"><a href="#" onclick="ChangeOption(this,'general');return false;">
        <i class="fa fa-cog"></i> General
      </a></li>
      <li><a href="#" onclick="ChangeOption(this,'advanced');return false;">
        <i class="fa fa-sliders"></i> Advanced
      </a></li>
    </ul>
  </div>
</div>
<div id="general" class="panel panel-flat">...</div>
<div id="advanced" class="panel panel-flat" style="display:none">...</div>
```

Show/hide panels with small JS (pattern from `options.php` `ChangeOption`).

## Icons

Font Awesome 4-style classes used across admin: `fa fa-*`. Place icons with `position-left` next to labels/buttons.

Header examples:

```html
<i class="fa fa-cogs position-left"></i>
<i class="fa fa-facebook-official position-left"></i>
<i class="fa fa-hdd-o position-left"></i>
```

## DLEPlugin-first workflow for plugin admin UI

1. Ensure `extras.admin_section: true`, `src/admin.php`, and an alias to `engine/inc/<lowercase-slug>.php` when working in a dleplugin project. Use the companion `dleplugin` skill for manifest and packaging details when available; classic `engine/inc` modules do not need this manifest setup.
2. Register routes on `$app->admin`; implement controllers extending `Core\Controller`.
3. Prefer:
   - **Settings**: Model + `Form` + a custom view containing the escaped CSRF token (stock `@form` requires adaptation)
   - **CRUD list**: adapt `Admin\ModelController` with custom form, confirmation and list views. Stock `save()`/`delete()` do not check `user_hash`, stock views do not add it, and `getValue()` emits raw strings/item labels into `@model/main`. Verify tokens before mutation, add escaped tokens to forms/mutating links, and escape text and URL attributes in the custom views; keep trusted icon HTML separate. See [CRUD security adaptations](../../dleplugin/references/framework.md#adapting-modelcontroller-crud-safely)
   - **Custom screen**: PHP view under plugin `Views/` using `AdminHTML` or the HTML patterns above
4. Return `AdminResponse` for pages; `Response::redirect()` after POST saves; use `$this->admin->addMessage()` for flash-style notices when available.
5. Keep markup in views, not giant strings in controllers, unless the project already uses string HTML.

Minimal custom settings view sketch:

```php
// src/Views/settings.php
use iCooLER\DLEPlugin\Core\AdminHTML as HTML;

/** @var string $saveUrl */
/** @var array $config */

echo HTML::form(['method' => 'post', 'action' => $saveUrl], [
    HTML::panel([
        HTML::panelHeading('My plugin'),
        // Prefer table+showRow HTML if matching system settings look,
        // or formGroup rows for entity-style forms.
        HTML::panelFooter([
            '<button type="submit" class="btn bg-teal btn-sm btn-raised position-left"><i class="fa fa-floppy-o position-left"></i>Save</button>',
        ]),
    ]),
    '<input type="hidden" name="user_hash" value="'
        . htmlspecialchars((string) $GLOBALS['dle_login_hash'], ENT_QUOTES, 'UTF-8') . '">',
]);
```

The framework does not inject this token or validate it. Validate the POST token before saving; `Form::isSubmitAndValid()` alone does not do that. With a nonstandard mount, call `$admin->addViewsFolder()` with the installed absolute path to the plugin Views directory before rendering. The companion `dleplugin` skill covers framework controllers, persistence, and CLI checks when available.

When emitting raw HTML in views, use the **exact class names** from this reference so skins stay consistent after DLE updates.

## Conditional fields

```php
showRow('FTP host', 'Remote host', $input, 'ftp-server');
```

```js
function showRemote(type) {
  $('.ftp-server').toggle(type === '1');
}
$('select[name="storage[type]"]').on('change', function () {
  showRemote(this.value);
});
```

## Accessibility / i18n notes

- Put UI strings in plugin language files when the project has them; otherwise Russian labels are fine if the rest of the plugin is Russian.
- Prefer `$lang[...]` when extending core pages; plugins often use their own lang arrays.
- Use `text-muted`, `text-semibold`, `text-size-small`, `hidden-xs` for hierarchy like core.

## Anti-patterns

- Forms, CRUD tables, statistics, or tabs replacing the section directory on a multi-section plugin home page.
- Custom Bootstrap 5 / Tailwind admin skins that clash with Limitless.
- Buttons without `btn-sm` or without a `bg-*` color (a bare `btn`, or `btn btn-default`).
- Hand-built confirmation modals instead of `DLEconfirm` / `DLEconfirmDelete`.
- Modal trigger `data-target` that does not match the modal root `id`, or duplicate ids.
- A `<form>` that wraps only `.modal-body` while the footer buttons sit outside it.
- Checkbox without `class="switch"` on settings pages (looks broken next to core).
- Single-select without `uniform` (unstyled next to themed selects).
- A `multiple` select styled with `uniform`/`form-control` instead of a Chosen class, or a multi-select whose class is never passed to `.chosen()` (renders as a raw list).
- Using `$.uniform.update()` to refresh a Selectpicker (it only re-renders `.icheck`; use `.selectpicker('refresh')`).
- Missing `user_hash` on save/delete.
- `echoheader`/`echofooter` inside DLEPlugin controllers that already wrap `AdminResponse` (double chrome).
- Inline styles for colors when a `bg-*` / `text-*` class exists.
- Loading jQuery again (already in admin).

## Quick checklist before finishing admin UI work

- [ ] Multi-section plugin home shows only the section directory; links open separate content and each section links back
- [ ] Directory uses native media links, two columns per row, and a correctly closed final incomplete row
- [ ] Forms inside sections match either **settings table** or **form-horizontal**; tabs stay within a page/section
- [ ] Panels use `panel panel-default` with the appropriate directory, body, table, or footer structure
- [ ] Booleans use `class="switch"`; single-selects use `uniform` (`uniform form-control` to align in grids); multi-selects use a Chosen class + `multiple` + `.chosen()` init
- [ ] **Every action** button is `btn bg-* btn-sm btn-raised` (native modal `.close` controls are exempt) (no bare `btn`, no missing `btn-sm`)
- [ ] Primary actions use `bg-teal`; cancel uses `bg-grey-400`; destructive uses `bg-danger`
- [ ] Modals follow `modal fade > modal-dialog > modal-content`, triggers match ids, form wraps all sections
- [ ] Destructive actions use `DLEconfirmDelete`, not a hand-built confirm modal
- [ ] POST includes `user_hash`; server verifies it
- [ ] User-facing strings escaped
- [ ] Template tags/short `<code>` examples have computed `display: inline` within sentences on list, help, and separate form pages; any override is scoped to plugin content, multiline examples stay block-level, and shell/other modules are unaffected
- [ ] Links built via `$admin->url()` in DLEPlugin projects
- [ ] No extra CSS framework unless requested
- [ ] Visual parity with a nearby core screen (`options`, `social`, `editvote`, `storage`)
