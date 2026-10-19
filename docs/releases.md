# Releases

The public module path is `github.com/thecoooler/dleplugin` and release tags
use semantic versions such as `v0.1.0`. Build with Go 1.27.2+ and keep the
minimum patch version in `go.mod` current. CI uses that version and scans both
sources and built binaries with the pinned govulncheck version.

Releases are verified and built by GitHub Actions; no local checks are
required. To release, make sure CI is green on main, then tag a commit on main
and push the tag:

```sh
git tag -a v0.1.0 -m v0.1.0
git push origin v0.1.0
gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
```

The Release workflow rejects tags that are not on main, runs the full CI
(tests, race tests, vulnerability scans, cross builds, image smoke checks),
then packages with `scripts/release.py`. Packaging builds Linux, macOS and
Windows for amd64 and arm64, disables CGO, scans every resulting binary, and
includes README, MIT LICENSE, third-party notices, license texts and
SHA256SUMS. Archive directory names contain the version and target platform.

The workflow creates a **draft** GitHub release. Inspect its assets and notes
before publishing the draft. Retries update only an existing draft; published
assets are not overwritten. To debug packaging, `scripts/release.py` can also
run locally, for example
`python3 scripts/release.py v0.0.0-check --platform linux/amd64 --output /tmp/dleplugin-release-check`.

When the repository is created, enable private vulnerability reporting and
make the CI test/security/build/image jobs required checks for main. Review
Dependabot updates for Go, Composer and Actions. Update base image digests,
PCOV and the pinned govulncheck version explicitly with their smoke checks.
No workflow bundles or redistributes the DataLife Engine trial.

## Bundled agent skills

The binary embeds both skill directories from
`internal/modules/skills/internal/bundle/assets/`; no separate download or release
asset is needed. `skills list` and installation receipts use the same CLI version
set by ldflags. Update the vendored files and source revision together, preserve
the documented local fixes, and run `make pipeline` when refreshing them. Existing
installations update only when the user runs `skills install` again. See
[agent skills](agent-skills.md) for source provenance and verification limits.
