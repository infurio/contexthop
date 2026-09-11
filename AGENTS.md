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

# Keep changes and releases simple

- Implement the smallest complete solution. Do not add compatibility layers,
  new abstractions, dependencies, or approval steps without a concrete need.
  Before v1, remove superseded commands and settings rather than retaining aliases.
- A release request is publication of the prepared changes, not a fresh development
  cycle. Do not expand its scope except to fix a demonstrated release blocker.
- Reuse successful checks while the code and relevant inputs are unchanged.
  Do not rerun a local full or race suite merely because a release was requested;
  required PR CI runs those checks. Run missing targeted checks only when needed.
- Review publication content before pushing. Preserve privacy review, required CI,
  exact merged-commit verification, and the fresh Homebrew installation check.
- Use authenticated HTTPS for GitHub operations. Never print credentials or
  change credentials speculatively after a transport failure.
- Use one watcher per CI/release run with a 30-second interval. Avoid overlapping
  watches, separate repeated status queries, or additional sleep/poll loops.
- Measure release time from the user's request through verified completion.
  Report that total, including local work and waits. If it exceeds ten minutes,
  identify the current delay and next action; do not present publication-only
  time as the release duration. Ten minutes is an investigation threshold, not
  a guarantee about external runner availability.
