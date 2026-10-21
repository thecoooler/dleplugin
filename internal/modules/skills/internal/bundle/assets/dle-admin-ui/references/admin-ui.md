# DLE Admin Panel UI

## Contents

- [Core rules](#core-rules)
- [Page shell (DLEPlugin vs raw)](#page-shell-dleplugin-vs-raw)
- [Native breadcrumbs for list, add, and edit pages](#native-breadcrumbs-for-list-add-and-edit-pages)
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
3. **Include and validate CSRF** on POST forms and mutating GET links. For example, render a hidden token in a PHP view:
   ```php
   echo '<input type="hidden" name="user_hash" value="'
       . htmlspecialchars((string) $dle_login_hash, ENT_QUOTES, 'UTF-8') . '">';
   ```
   Before any mutation, reject a missing, non-string, empty or mismatched token:
   ```php
   $token = $_POST['user_hash'] ?? null; // use $_GET for a mutating GET handler
   if (!is_string($token) || !is_string($dle_login_hash)
       || $dle_login_hash === '' || !hash_equals($dle_login_hash, $token)) {
       throw new \RuntimeException('Invalid CSRF token');
   }
   ```
   A confirmation dialog and `Form::isSubmitAndValid()` do not replace this server check.
4. **Prefer DLEPlugin views/helpers** when the project already uses the framework; fall back to raw HTML only when you need a custom layout.
5. **Do not load extra CSS/JS frameworks** unless the user explicitly asks. Admin already loads Font Awesome, Bootstrap-style panels, Selectpicker (via `uniform` class), Switchery (`.switch`), Chosen, datetimepicker, popovers.

## Page shell (DLEPlugin vs raw)

### With DLEPlugin (preferred for plugins)

Controller returns `AdminResponse` or a string; the framework wraps with `echoheader()` / `echofooter()`. Views contain only the page body. Configure breadcrumbs through the installed framework's response/controller API: inspect `Core/AdminResponse.php` and the response handling in `Core/Admin.php` for the accepted shape and escaping behavior. Do not assume it accepts the raw DLE URL-to-label array below.

Build parent and action links with `$this->admin->url('/path')`, never hard-code `?mod=...` unless matching an existing DLE mod. Escape each dynamic value once at the HTML output boundary, accounting for any escaping the installed wrapper already performs.

### Raw DLE `engine/inc` style (only if writing classic mod pages)

```php
echoheader(
    '<i class="fa fa-cogs position-left"></i><span class="text-semibold">Plugin name</span>',
    ['' => htmlspecialchars($lang['page_subtitle'], ENT_QUOTES, 'UTF-8')]
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

## Native breadcrumbs for list, add, and edit pages

**Required for nested admin pages:** use the native shell's breadcrumbs to express the page hierarchy. Moving add/edit to a separate page must preserve the linked parent list and an active, unlinked current item. A separate “Back to list” link is not a substitute; a cancel action may still be useful in a form.

Verified in DLE's `engine/inc/include/functions.inc.php`, `echoheader($header_title, $header_subtitle)`:

- The **second argument** accepts an associative array, in display order: `['?mod=x' => 'Parent', '' => 'Current']`.
- Each nonempty URL key becomes an `<a href="...">`; the empty key becomes `<li class="active">`. Put the single empty key last.
- A string is converted to `['' => $string]`; it cannot describe a parent/current hierarchy.
- DLE prepends **Home** (`?mod=main`, localized as “Главная”) automatically. Do not add it yourself.
- DLE inserts keys and labels directly into HTML. Escape dynamic labels and URL attributes with `htmlspecialchars(..., ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8')`. Encode query parameter values when constructing URLs, then HTML-escape the complete URL. Keep trusted title/icon markup separate from dynamic text.

`engine/inc/storage.php` also demonstrates a linked parent plus an active add/edit title. Check these functions and the installed skin when targeting another DLE version.

The following are three alternative classic-module page shells. `$listHtml` / `$formHtml` denote the rendered body; on edit, `$variableName` is the loaded record's unescaped name. Each page calls header/footer once.

**List — Home → Variables (active):**

```php
$title = '<i class="fa fa-cogs position-left"></i><span class="text-semibold">Template variables</span>';
echoheader($title, ['' => 'Variables']);
echo $listHtml;
echofooter();
```

**Add — Home → Variables (link) → Add variable (active):**

```php
$title = '<i class="fa fa-cogs position-left"></i><span class="text-semibold">Template variables</span>';
echoheader($title, [
    '?mod=templatevars' => 'Variables',
    '' => 'Add variable',
]);
echo $formHtml;
echofooter();
```

**Edit — Home → Variables (link) → Edit: name (active):**

```php
$title = '<i class="fa fa-cogs position-left"></i><span class="text-semibold">Template variables</span>';
$current = htmlspecialchars('Edit: ' . $variableName, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
echoheader($title, [
    '?mod=templatevars' => 'Variables',
    '' => $current,
]);
echo $formHtml;
echofooter();
```

`templatevars` is illustrative; use the real module and project language strings. For DLEPlugin, implement the same hierarchy through its verified breadcrumb API, with parent URLs from `$admin->url()`. Do not copy these `echoheader()` / `echofooter()` calls into an `AdminResponse` page.

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

`showRow()` deliberately accepts HTML in `$title`, `$description`, and `$field`; it only escapes `$class`. Escape dynamic text before composing those slots, while preserving trusted field/icon markup.

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

Initial page setup does not initialize controls inserted later by AJAX. Initialize new single selects with `.selectpicker()`, new switches with `new Switchery(element, options)`, and new Chosen fields with `.chosen()`, scoped to the inserted nodes; avoid initializing an existing instance twice. Refresh an existing widget after changing its options. This follows the document-ready initialization in DLE's `application.js`.

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

**Skill convention for action buttons (not an invariant of every core page): every action button must carry `btn` + exactly one `bg-*` color + `btn-sm` + `btn-raised`.** Never emit a bare `btn` or a button with no background color — it looks broken next to the core. Native modal `.close` dismiss controls retain their stock markup and are exempt. This applies to action `<button>`, `<input type="submit|button|reset">`, and link-as-button `<a>`.

Primary pattern:

```html
<button type="submit" class="btn bg-teal btn-sm btn-raised position-left">
  <i class="fa fa-floppy-o position-left"></i>Save
</button>
```

> Core note: some core pages omit `btn-sm` (e.g. `options.php`, `social.php`). This skill standardizes plugin action buttons on `btn-sm`; the choice of color below is a recommendation by action role.

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

Sizes/modifiers: `btn-sm` (always), `btn-raised` (required by this skill for action buttons), `position-left` / `position-right`, plus spacing helpers `mt-5`, `mb-5`, `mt-15`.

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
        <input type="hidden" name="user_hash" value="<?= htmlspecialchars((string) $dle_login_hash, ENT_QUOTES, 'UTF-8') ?>">

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

Recommended modal footer arrangement (button classes still follow the action-button convention):

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
- A standalone “Back to list” link replacing native parent/current breadcrumbs on add/edit pages.
- `echoheader`/`echofooter` inside DLEPlugin controllers that already wrap `AdminResponse` (double chrome).
- Inline styles for colors when a `bg-*` / `text-*` class exists.
- Loading jQuery again (already in admin).

## Quick checklist before finishing admin UI work

- [ ] List/add/edit hierarchy uses native breadcrumbs: one automatic Home, linked parent, active unlinked last item; header/footer appear once
- [ ] Layout matches either **settings table** or **form-horizontal**
- [ ] Panels use `panel panel-default` + heading/body|table/footer
- [ ] Booleans use `class="switch"`; single-selects use `uniform` (`uniform form-control` to align in grids); multi-selects use a Chosen class + `multiple` + `.chosen()` init
- [ ] **Every action** button is `btn bg-* btn-sm btn-raised` (native modal `.close` controls are exempt) (no bare `btn`, no missing `btn-sm`)
- [ ] Recommended role colors: `bg-teal` primary, `bg-grey-400` cancel, `bg-danger` destructive
- [ ] Modals follow `modal fade > modal-dialog > modal-content`, triggers match ids, form wraps all sections
- [ ] Destructive actions use `DLEconfirmDelete`, not a hand-built confirm modal
- [ ] Save/delete include an escaped `user_hash`; server rejects missing/invalid tokens before mutation
- [ ] User-facing strings escaped
- [ ] Links built via `$admin->url()` in DLEPlugin projects
- [ ] No extra CSS framework unless requested
- [ ] Visual parity with a nearby core screen (`options`, `social`, `editvote`, `storage`)
