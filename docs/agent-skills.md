# Bundled AI agent skills

`dleplugin` ships the `dleplugin` workflow skill and the companion `dle-admin-ui`
UI skill inside the Go binary. Both complete trees are installed together; all
relative references and sibling links keep their layout. Installation needs no
network, source checkout, DLE installation, manifest or CLI configuration.

```sh
cd my-plugin
dleplugin skills list
dleplugin skills install --agent codex
dleplugin skills install --agent claude
# Install for all local projects instead:
dleplugin skills install --agent claude --global
# After upgrading the CLI, repeat the appropriate install command.
dleplugin skills install --agent claude --global
# Remove the managed copies in the selected scope:
dleplugin skills uninstall --agent claude --global
```

## Agent directories

| Agent | Project (current working directory) | User (`--global`) |
| --- | --- | --- |
| Codex | `.agents/skills/` | `~/.agents/skills/` |
| Claude Code | `.claude/skills/` | `~/.claude/skills/` |

These locations were checked against official
[Codex skill documentation](https://developers.openai.com/codex/skills/) and
[Claude Code skill documentation](https://code.claude.com/docs/en/skills).
Claude Code receives its own `.claude` copy. No shared `.agents` discovery is
assumed. Start/restart the agent in the project if it has not discovered the
installed skills. Agent settings and organization policies can restrict loading.

Project scope means the exact invocation directory, without searching for a Git
or `dleplugin.yml` root. Run from your intended project root. `init` never installs
skills implicitly. If both scopes are installed, agent-specific precedence rules
apply; uninstall the unwanted scope explicitly.

## Ownership, conflicts and updates

Copying keeps installation independent of symlink support, shared state and the
other agent's files. The cost is one small copy per agent and scope. The two
skill directories contain a `.dleplugin-managed.json` receipt recording owner,
schema, skill name, CLI version, upstream revision and SHA-256 hashes of files
(and directory entries). Do not edit or delete the receipt.

- `skills list` lists the binary's bundled skills and version, not installed state.
- Repeated install replaces unchanged managed copies with the current binary's
  contents and version, removing obsolete bundled files. The bundle version is
  the CLI version, including `dev` for a development build; upstream revision is
  provenance rather than an independent release version. Installing an older
  semantic release prints a downgrade warning for unchanged managed copies,
  including prerelease ordering. It still installs the selected binary's bundle.
  Development versions such as `dev` are not ordered.
- Modified, missing or extra files/directories inside an installed skill block
  install and uninstall. Missing/invalid ownership receipts and foreign skill
  directories also block both operations. Both skills are checked before either
  is changed.
- `install --force` explicitly replaces the entire conflicting skill directories,
  including user additions. Back up your changes first. It can adopt a foreign
  directory; the resulting copy is managed by the CLI.
- Uninstall has no force flag. Restore modified files or move the conflicting
  directory outside the skills folder and rerun uninstall. Missing skill
  directories are harmless. Other skill names and parent directories remain.
- Symlinked agent roots, skills roots and skill directories are refused, even
  with force. Nested symlinks are conflicts; force replaces their containing
  directory without following the links.

Updates stage both copies alongside the skills folder (`.agents/` or `.claude/`),
outside the directory scanned for skills, and use directory renames,
with rollback on ordinary rename failures. If rollback fails, the error names
retained backups. Cleanup failures also return an error naming remaining temporary
files; the copies may already have been installed or removed. Restore write
permissions and remove the named temporary directory after checking its contents.
After interruption, inspect any `.dleplugin-skills-*` directories left in the
selected `.agents/` or `.claude/` folder before removing them manually. The operation is not crash-atomic across both directories.
Do not run concurrent installers or edit the destination during an operation;
receipts protect local changes, not against hostile concurrent filesystem writes.

## Source and bundled fixes

Source: local `thecoooler/dleplugin-skill` commit
`9a24d6385200c32b9925ac27eb0af8b3ec148981`, directories `skills/dleplugin/` and
`skills/dle-admin-ui/`. The source repository is read-only for this integration;
vendored copies live in `internal/modules/skills/internal/bundle/assets/`.
No absolute source path is used at runtime.

Local adaptations relative to that revision:

- Native breadcrumbs are explicit workflow and completion requirements for
  list/add/edit admin pages, with complete classic-module shell examples.
  DLE's `echoheader()` implementation confirms URL-keyed parents, an empty-key
  active item, automatic Home and unescaped labels/URLs; `storage.php` demonstrates
  the hierarchy. DLEPlugin guidance keeps header/footer ownership in the wrapper
  and requires checking its installed breadcrumb contract rather than assuming
  the raw DLE array is an `AdminResponse` constructor format.
- The UI CSRF example now escapes the hidden token and rejects missing, empty,
  non-string and mismatched tokens before mutation. The reference calls out raw
  HTML slots in `showRow()` and initialization of controls inserted after page
  load, checked against DLE's `storage.php` and `application.js`. Button sizing is
  identified as a skill convention; role colors are recommendations.
- The HTTP example rejects `body === false` and status `0` before checking HTTP
  error statuses. Confirmed against local framework `Core/HTTP.php`.
- CRUD guidance explicitly requires save/delete CSRF validation, escaped tokens
  in custom form/confirmation views, and escaping strings/item labels in custom
  lists. Local `Admin/ModelController.php` and `Views/model/main.php` confirm that
  the stock controller/views do not supply these protections. Raw IDs/URLs and
  trusted icon markup require separate handling.
- Button guidance exempts native modal `.close` controls consistently.
- The CLI reference documents the new skills commands and current upgrade URL flags.
- Companion links describe the installed sibling layout, guaranteed by bundling
  both skills.

## Verification limits

Go tests exercise both agents and scopes with temporary HOME/project directories:
installation, version updates, changed/foreign files, force replacement, safe
removal, symlinks, unchanged unrelated skills and relative Markdown references.
The bundle update test compares both entire installed trees against the embedded
assets, including references and eval prompts.
Real user-global skills are never used by these tests.

`evals/evals.json` files include upstream and local regression scenario prompts
for navigation and dynamic controls. They have no
project fixtures and are not evidence that PHP tasks pass in a real DLE project.
This integration does not implement an eval harness, benchmark agent behavior or
run the PHP examples against a live DLE site.
