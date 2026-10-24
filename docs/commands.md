# Public commands

Keep these commands and their names stable.

Core module:

- `dleplugin init [DIR]`
- `dleplugin build [--version V] [--upgrade-url U]`
- `dleplugin convert`
- `dleplugin diff SOURCE1 SOURCE2`

Deploy module:

- `dleplugin deploy upload [HOST]` (with `--version`, `--upgrade-url` and
  `--file`)
- `dleplugin deploy sites list/add/show/edit/remove`

Static module:

- `dleplugin static`

Test module:

- `dleplugin test`

Check module:

- `dleplugin check`

Hosts module (root help group: `DLE hosts`; see [DLE hosts](hosts.md)):

- `dleplugin hosts up/down`
- `dleplugin hosts compose [ARGS...]`
- `dleplugin hosts list/add/remove/path/port/versions`
- `dleplugin hosts protect`
- `dleplugin hosts config show/get/set/unset/path`

AI agents module (root help group: `AI agents`):

- `dleplugin skills list`
- `dleplugin skills install --agent codex|claude [--global] [--force]`
- `dleplugin skills uninstall --agent codex|claude [--global]`

Both skills are always installed together, offline, into the current directory's
agent skills folder or the user's home with `--global`. No manifest or CLI config
is required. See [agent skills](agent-skills.md) for conflict and removal rules.

## Validation Commands

```bash
make test
make lint
make pipeline
```

Help smoke checks:

```bash
dleplugin --help
dleplugin init --help
dleplugin build --help
dleplugin convert --help
dleplugin diff --help
dleplugin deploy --help
dleplugin deploy upload --help
dleplugin deploy sites --help
dleplugin deploy sites add --help
dleplugin static --help
dleplugin test --help
dleplugin check --help
dleplugin skills --help
dleplugin skills list --help
dleplugin skills install --help
dleplugin skills uninstall --help
```

## Golden fixtures

Verified against `testdata/golden`:

- `dleplugin.yml` loading and normalization (`testdata/golden/plugin`).
- XML generation content and plugin file mount paths (`build.xml`,
  `build_nocommit.xml`).
- Split build zip contents and XML cleanup behavior (`quirks_files`).
- README HTML generation when `README.md` exists (`testdata/golden/readme`).
- `init` output (`testdata/golden/init`) and `convert` output
  (`testdata/golden/convert`).
- Plugin loading error messages (`testdata/golden/plugin_errors.json`).
