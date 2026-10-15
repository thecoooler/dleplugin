# Golden fixtures

Inputs live in `golden/plugin/*/input/` and `golden/convert/*/*.xml`. To add a
case, add its input and register it in the relevant golden test. Update
expectations with `make golden`, review `git diff -- testdata/golden`, then run
`make pipeline`. Updates are explicit; normal test runs never write fixtures.

`make golden` runs plugin/init updates first, XML generation second, and
conversion third so changes cannot race with consumers of generated metadata.
JSON formatting is preserved when values are unchanged. XML, YAML and extracted
source bytes remain exact comparisons. Golden input files preserve CRLF.

`plugin_errors.json` holds manifests and the exact loading errors they produce;
the plugin directory is written as `<dir>`. `readme/README.html` is the
README.html page wrapper; the Markdown renderer's body is tested separately.
Regression cases for quoting, paths and SQL live in the normal Go test suites.
