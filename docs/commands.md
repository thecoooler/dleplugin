# Public commands

Keep these commands and their names stable.

Core module:

- `dleplugin init [DIR]`
- `dleplugin build`
- `dleplugin convert`
- `dleplugin diff SOURCE1 SOURCE2`

Deploy module:

- `dleplugin deploy upload`
- `dleplugin deploy sites list/add/show/edit/remove`

Static module:

- `dleplugin static`

Test module:

- `dleplugin test`

Check module:

- `dleplugin check`

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
