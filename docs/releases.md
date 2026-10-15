# Releases

The public module path is `github.com/thecoooler/dleplugin` and release tags
use semantic versions such as `v0.1.0`. Build with Go 1.27.2+ and keep the
minimum patch version in `go.mod` current. CI uses that version and scans both
sources and built binaries with the pinned govulncheck version.

Before a release, run `make pipeline`, `go test -race -count=1 ./...`, image
smoke checks and the vulnerability scans. Build archives locally with:

```sh
python3 scripts/release.py v0.1.0
# One platform for a local packaging check:
python3 scripts/release.py v0.0.0-check --platform linux/amd64 --output /tmp/dleplugin-release-check
```

Packaging requires Go, Python 3, and network access for vulnerability data and
uncached modules. It builds Linux, macOS and Windows for amd64 and arm64,
disables CGO, scans every resulting binary, and includes README, MIT LICENSE,
third-party notices, license texts and SHA256SUMS. Archive directory names
contain the version and target platform. Packaging does not create a Git tag,
push changes, or publish a release.

After reviewing the final commit and artifacts, a maintainer creates and
pushes a version tag. The Release workflow runs CI first and creates a **draft**
GitHub release. Inspect its assets and notes before publishing the draft.
Retries update only an existing draft; published assets are not overwritten.

When the repository is created, enable private vulnerability reporting and
make the CI test/security/build/image jobs required checks for main. Review
Dependabot updates for Go, Composer and Actions. Update base image digests,
PCOV and the pinned govulncheck version explicitly with their smoke checks.
No workflow bundles or redistributes the DataLife Engine trial.
