# Updating Docker tools

The static and test images embed their Dockerfile, `composer.json` and
`composer.lock`. PHP, Composer and MariaDB images are pinned by digest, PCOV
by release, and Composer installs the complete locked dependency graph.
Every embedded build-context input contributes to the image tag, so new
versions build a new image.

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

Runtime builds use the same assets in a deterministic tar context from stdin;
no project files or credentials enter that build context. Apt package mirrors
still receive updates: these pins make PHP/Composer tool versions predictable,
but do not promise byte-identical Docker layers across rebuilds.
