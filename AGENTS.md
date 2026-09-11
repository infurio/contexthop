# Repository privacy requirements

Read `docs/architecture.md` before changing application behavior.

Never copy or derive repository content from a developer's real contexts or local
configuration. This includes context and cluster names, accounts, projects,
namespaces, endpoints, catalog entries, paths, and identifying fragments of them.
The rule applies to code, tests, fixtures, snapshots, documentation, media, commit
messages, PR text, logs, and release artifacts. Identifiers are private even when
they are not credentials.

Construct fictional examples independently. Use `internal/testenv` and its Acme
scenario for tests and recordings; do not capture the developer's live terminal
or run discovery against their configuration to produce examples. Do not copy
live configuration and sanitize it afterward. Real-account verification, when
explicitly requested, must keep identifying output out of the repository and
publication channels.

Before committing or publishing, review all changed text and every distinct
media frame for private identifiers and verify fixture provenance. A passing
secret scanner does not establish that context names or organizational details
are safe. If provenance is uncertain, stop publication and replace the content
with independently constructed fictional data.

For privacy audits, keep private comparison inputs and reports outside the
repository and suppress matched values in output. Never add a real identifier to
a regression test, denylist, or scanner configuration, even to prevent recurrence.
