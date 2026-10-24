---
name: dle-admin-ui
description: >
  Build and review native DataLife Engine (DLE) admin interfaces: settings
  forms, CRUD tables and add/edit pages, section directories, tabs, buttons,
  selects, modals, and breadcrumbs matching the bundled admin theme.
  Use for DLE plugin admin views and classic engine/inc modules, with or without
  the DLEPlugin PHP framework. Covers admin presentation, native navigation,
  form and user_hash CSRF integration; dleplugin CLI, manifests, and backend
  framework setup belong to the companion dleplugin skill.
---

# DLE Admin UI

Create admin screens that match the installed DLE theme. This skill supports both DLEPlugin views and classic `engine/inc` modules; it does not require the dleplugin CLI or manifest. The `dleplugin` skill is installed alongside this one for manifest, routing, persistence, and CLI work.

## Workflow

1. Inspect the existing screen and a nearby core page (`options.php`, `social.php`, `editvote.php`, or `storage.php`). Follow the installed theme's markup and the project's existing integration.
2. Read the relevant sections of [references/admin-ui.md](references/admin-ui.md) before changing UI. For a multi-section plugin home, read [Plugin home: section directory](references/admin-ui.md#plugin-home-section-directory): the home shows only links, while forms, CRUD, and statistics open inside sections. The reference contains page shells, settings tables, horizontal forms, controls, buttons, modals, CRUD lists, tabs, and DLEPlugin helper caveats.
3. Use the existing page shell: DLEPlugin controllers return `AdminResponse` and already receive header/footer wrapping; classic modules use `echoheader()` / `echofooter()`. When adding or separating list/add/edit pages, plan native breadcrumbs with a linked parent list and an active current page; read [page navigation](references/admin-ui.md#native-breadcrumbs-for-list-add-and-edit-pages). A standalone “Back to list” link does not replace breadcrumbs.
4. Keep markup in views where the project uses them. Preserve field names, action URLs, and backend behavior unless the task changes them. For dleplugin manifest, routing, persistence, or CLI work, use the installed companion `dleplugin` skill.
5. Check rendered behavior when the app can run: follow list → add/edit → parent breadcrumb; verify one shell, one automatic Home item, and an unlinked current item. Check controls initialize (including dynamically inserted controls), tabs and modals target the right elements, all fields submit, and validation or success messages appear. Verify save/delete handlers reject missing or invalid CSRF tokens. Report which checks ran and any unavailable runtime.

## Essential conventions

- Use DLE's native panels and settings-table or horizontal-form layouts. Admin already provides jQuery, Bootstrap 3 patterns, Font Awesome, Switchery, Selectpicker, and Chosen.
- Nested pages must use native breadcrumbs: raw `echoheader()` takes an ordered URL-to-label array as its second argument, ending with `'' => 'Current'`; DLE adds Home itself. Escape dynamic labels and URL attributes. In DLEPlugin, supply navigation through the installed response/controller API; never add a second `echoheader()` in the view.
- The admin theme can make `<code>` block-level. Keep template tags and short examples within sentences inline using CSS scoped to the plugin content wrapper; preserve intentional multiline blocks. See [inline code and scoped CSS](references/admin-ui.md#inline-code-and-scoped-css) for the DLETemplateVars example and computed-style checks.
- Action buttons use `btn` + one `bg-*` color + `btn-sm` + `btn-raised`. Modal `.close` controls retain their native markup. See the reference for `AdminHTML::button()` defaults that append an extra color.
- Booleans use `.switch`; single selects use `.uniform` (add `.form-control` for grid alignment); multiple selects use Chosen and explicit initialization.
- A multi-section plugin home uses the native section directory with two media links per row; tabs are for content within a page/section and never replace that directory.
- Tabs use `data-toggle="tab"`; one form wraps all tab panes. Bootstrap modals use matching triggers/ids and one form around header, body, and footer. Confirm destructive actions with `DLEconfirmDelete` / `DLEconfirm`.
- Escape dynamic output. Include an escaped `user_hash` on forms and mutating links, and verify the token on the server before saving. Framework forms do not add or validate it automatically; a confirmation dialog, `Form::isSubmitAndValid()`, or stock CRUD views do not replace the server check.
- Field descriptions use the native help popover inside `<label>` (escaped `data-content`, `sr-only` copy via `aria-describedby`), not `help-block` text under fields. Panel titles are `<div class="panel-title">`; auxiliary blocks such as template tags get their own panel below the form.
- After a successful save, store the notice in `$_SESSION['<slug>_notice']`, redirect 303 to the clean section URL, then read and `unset` it; do not use `&status=saved` URLs. DLE admin already started the session. See [notice after saving](references/admin-ui.md#notice-after-saving-flash).
- Use `$admin->url()` in DLEPlugin screens and existing module URL conventions in classic modules. Keep strings in the project's language files where present.
