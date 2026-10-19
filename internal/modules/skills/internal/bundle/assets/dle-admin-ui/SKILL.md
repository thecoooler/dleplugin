---
name: dle-admin-ui
description: >
  Build and review native DataLife Engine (DLE) admin interfaces: settings forms,
  CRUD tables, tabs, buttons, selects, and modals matching the bundled admin theme.
  Use for plugin admin views and classic engine/inc modules, with or without the
  DLEPlugin PHP framework. Covers admin presentation and form integration;
  dleplugin CLI, manifests, and backend framework setup belong to dleplugin.
---

# DLE Admin UI

Create admin screens that match the installed DLE theme. This skill supports both DLEPlugin views and classic `engine/inc` modules; it does not require the dleplugin CLI or manifest.

## Workflow

1. Inspect the existing screen and a nearby core page (`options.php`, `social.php`, `editvote.php`, or `storage.php`). Follow the installed theme's markup and the project's existing integration.
2. Read the relevant sections of [references/admin-ui.md](references/admin-ui.md) before changing UI. It contains page shells, settings tables, horizontal forms, controls, buttons, modals, CRUD lists, tabs, and DLEPlugin helper caveats.
3. Use the existing page shell: DLEPlugin controllers return `AdminResponse` and already receive header/footer wrapping; classic modules use `echoheader()` / `echofooter()`.
4. Keep markup in views where the project uses them. Preserve field names, action URLs, and backend behavior unless the task changes them. For dleplugin manifest, routing, persistence, or CLI work, also use the companion `dleplugin` skill when available.
5. Check rendered behavior when the app can run: controls initialize, tabs and modals target the right elements, all fields submit, and validation or success messages appear. Report which checks ran and any unavailable runtime.

## Essential conventions

- Use DLE's native panels and settings-table or horizontal-form layouts. Admin already provides jQuery, Bootstrap 3 patterns, Font Awesome, Switchery, Selectpicker, and Chosen.
- Action buttons use `btn` + one `bg-*` color + `btn-sm` + `btn-raised`. Modal `.close` controls retain their native markup. See the reference for `AdminHTML::button()` defaults that append an extra color.
- Booleans use `.switch`; single selects use `.uniform` (add `.form-control` for grid alignment); multiple selects use Chosen and explicit initialization.
- Tabs use `data-toggle="tab"`; one form wraps all tab panes. Bootstrap modals use matching triggers/ids and one form around header, body, and footer. Confirm destructive actions with `DLEconfirmDelete` / `DLEconfirm`.
- Escape dynamic output. Include an escaped `user_hash` on forms and mutating links, and verify the token on the server before saving. Framework forms do not add or validate it automatically.
- Use `$admin->url()` in DLEPlugin screens and existing module URL conventions in classic modules. Keep strings in the project's language files where present.
