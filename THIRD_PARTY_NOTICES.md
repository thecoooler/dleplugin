# Third-party software

The dleplugin source is licensed under the MIT License (see LICENSE).
Go libraries and the Go standard library retain their respective licenses.
Release archives include their license and notice texts in `licenses/`.
The module versions are recorded in `go.mod`, `go.sum` and the binary's
`go version -m` metadata. The license bundle includes dependencies for all
supported platforms and may also include development-only dependencies.

Notable dependencies include the MySQL driver (MPL-2.0), Cobra (Apache-2.0),
Goldmark and go-keyring (MIT), and Go's supplemental libraries (BSD-style).
The desktop app also includes MyGo and purego (MIT and Apache-2.0) and
go-text/typesetting (BSD-style or Unlicense).
Consult the included license texts for the exact terms and copyright notices.

Docker tool images contain PHP, Composer, PHPStan, Pest and their dependencies;
these keep their own licenses. DataLife Engine and its trial are separate
software and are not included in this repository or its release archives.
