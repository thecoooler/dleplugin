# Updating Docker tools

The static and test images embed their Dockerfile, `composer.json` and
`composer.lock`. PHP, Composer and MariaDB images are pinned by digest, PCOV
by release, and Composer installs the complete locked dependency graph.
Every embedded build-context input contributes to the image tag, so new
versions build a new image. After a pull or build, the two most recent versions
of the image repository (including the current one; registry aliases count as
one version) are kept and older tags not used by a container are removed. A
pulled image is tagged locally and its registry reference is removed again, so
pruning the local tag also releases the layers. `dleplugin cache clean
--docker` removes unused content-addressed tool images across both
repositories.

To update Composer tools, run `composer update` in the corresponding asset
directory using the pinned PHP/Composer environment, review both JSON/lock
files, then build the image and check PHPStan/Pest. Do not run `composer require`
at image build time. To change PHP, Composer, MariaDB or PCOV, update their
pin, build and smoke-test the affected image, and run `make pipeline`.

Manual builds:

```sh
docker build -t dleplugin-static:dev internal/modules/static/internal/static/assets
docker run --rm dleplugin-static:dev vendor/bin/phpstan --version
docker build -t dleplugin-test:dev internal/modules/test/internal/dletest/assets
docker run --rm dleplugin-test:dev vendor/bin/pest --version
```

## Prebuilt images

`static` and `test` first look for the image locally, then pull
`ghcr.io/thecoooler/<name>:<hash>` and tag it as `<name>:<hash>`, and build
locally only when the pull fails (no network, unpublished tag, unsupported
platform, timeout after 10 minutes). `DLEPLUGIN_IMAGE_REGISTRY` sets another
registry prefix, for example a mirror; an empty value disables pulling, which
is useful while changing the assets.

The Images workflow (`.github/workflows/images.yml`) publishes them. It runs
on pushes to main that change the assets or `internal/dockerimg`, on manual
dispatch, and before packaging a release. The hidden `dleplugin tool-images`
command prints the tags and, with `--context DIR`, writes the exact build
contexts, so published tags always match the embedded assets. Tags already in
the registry are skipped. Missing ones are built and smoke-tested on native
amd64 and arm64 runners, pushed as `<hash>-amd64`/`<hash>-arm64`, and combined
into a multi-platform `<hash>` tag.

GHCR creates new packages as private. After the first publication, set the
visibility of the `dleplugin-static` and `dleplugin-test` packages to public
in their package settings; otherwise anonymous pulls fail and every user
builds locally again.

Runtime builds use the same assets in a deterministic tar context from stdin;
no project files or credentials enter that build context. Apt package mirrors
still receive updates: these pins make PHP/Composer tool versions predictable,
but do not promise byte-identical Docker layers across rebuilds.
