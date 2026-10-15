# Security

Use the latest dleplugin release and a patched Go toolchain when building it.
Only the latest release is maintained for security fixes.

Report vulnerabilities through [GitHub private vulnerability reporting](https://github.com/thecoooler/dleplugin/security/advisories/new)
when enabled. If it is unavailable, open an issue requesting a private contact
channel without exploit details, passwords, tokens or private site addresses.
Include the affected version, a minimal reproduction with fictional data,
the expected behavior and the impact. Allow time for a fix before publishing
exploit details.

Deployment credentials stay in environment variables or the OS credential
store. `--insecure` disables certificate verification for the current command;
use it only with trusted local/self-signed endpoints. Admin redirects cannot
cross origins. Plugin manifests and sources can contain executable PHP/SQL;
review third-party plugins before building or running their tests.
