# Development and architecture

[Documentation](README.md) · [Release guide](../RELEASE.md)

## Local development

From a checkout on a supported Mac with Go installed:

```sh
./scripts/dev
```

This builds `bin/chop` and opens a Zsh development shell using it, with current-shell
integration. Run `make build` after changes and `exit` to return. Homebrew and
startup files stay unchanged; the development shell uses your existing catalog
and credentials. Nested/login Zsh shells retain the checkout binary. Check with
`print -r -- "$CONTEXTHOP_BINARY"`.

For a manual build, use `make check` and add `$PWD/bin` to PATH.
[.go-version](../.go-version) is the toolchain source of truth; `TOOLCHAIN`
can override it locally.

## Ownership

ContextHop separates the saved catalog, session-only observations, the prepared
selection, and the active shell. Discovery does not activate a context.

| Layer | Responsibility |
| --- | --- |
| `cmd/contexthop/main.go`, `commands_*.go` | CLI dispatch and command execution |
| `interactive.go`, `controller.go` | TUI startup, workflow preparation, persistence, result acceptance, activation handoff |
| `*_flow.go`, editor modules | Launch, resource selection, project search, catalog and provider workflows |
| `internal/appstate` | Saved catalog and discovery overlay |
| `internal/selection` | Typed request, staging dependencies, ADC policy, launch/save resolution |
| `internal/ui` | Cursor projection, keyboard input, navigation, rendering and worker messages |
| `internal/catalog`, `internal/config` | Validated mutation plans, stale-write protection, snapshots and atomic writes |
| `internal/destination`, `internal/resolver` | Shared destination capabilities and dependency validation |
| `internal/session` | Isolated materialization and shell activation |

`selection.Request` contains resources, workspace provenance and ADC override,
without screen names, provider calls or persistence. `Components`, `Resolve`,
and `ResolveForSave` share identity/default and ADC policy. Workspace resolution
retains its saved dependency contract without mutating the catalog.

`ui.Draft` remains a dialog transport. `ContextSelection` extracts only resource
state; writing it back preserves unrelated dialog inputs.
`selection_request.go` translates discovery sentinels at the application boundary.
The controller projects a compatible cursor row plus staging; the UI rejects a
projection that replaces staged components. Explicit Space uses the replacement
projection. Launch and save consume the protected Selected preview.

## Terminal modules

`tabs.go` owns tab order, navigation keys, labels, capabilities and table modules.
`navigation.go` and `transitions.go` own the shared dialog stack and browsing state.
`layout.go` allocates rows and owns context-header visibility and width budgets.
`context_display.go` projects Active / Shared / Selected values, ADC badges and
selection state using typed component keys without changing the draft. It also
projects the legacy Pending comparison independently of rendering. Compact field
widths and omissions are allocated before styling; `window_header.go` and
`context_columns.go` render that data in compact and column layouts.
Table modules supply typed cells so selection styling preserves tag colors.
Ordinary lists share a visible-range calculation in `layout.go`; multi-line
action menus retain their own scrolling. `header_notice.go` carries notice
severity, priority and Details availability. Legacy descriptions without an
explicit status kind retain their severity inference at that boundary.

`browser_commands.go` and `context_commands.go` define available actions, keys,
aliases, search availability and footer labels. Keyboard dispatch, Options, Help
and primary hints consume them. `resource_footer.go` plans hint order, reserved
controls and branding before fitting the two footer rows. Small-terminal rendering
may abbreviate labels and glyphs without changing mappings. Domain actions go
through the controller; text editing and navigation stay in the UI.

Workspaces is the root; there is no Home renderer. Generic picker mode remains
for the CLI's identity chooser. Workflow routing delegates to launch,
resource-selection, project-search and catalog handlers, with separate provider
and editor handlers.

## Background work and persistence

Each controller operation uses independent catalog/editor snapshots. Discovery
updates that disposable view; `Catalog.Observe` extracts its session-only overlay.

Workers return an opaque `applicationResult` with a navigation transition.
`AcceptResult` publishes copies on the event loop before navigation/rendering.
Browser callbacks read only accepted state. Authentication continuations use the
same path. Never mutate accepted controller state from a worker.

The loading overlay retains a rendered background while resize and cancellation
remain responsive. Interactive provider commands yield the terminal through
`ui.Process`. Provider timeouts remain in the command layer.

Validated catalog plans apply in the background. `Catalog.SavedChange` installs
the saved snapshot and retains unrelated observations while preventing deleted
resources or mappings from being resurrected. Refreshed pickers return to the
existing browser. Failed writes preserve the workflow; final activation performs
fresh catalog and Kubernetes source checks after the TUI returns an outcome.

## Behavior invariants

The [user guide](usage.md) owns interaction details. Preserve these cross-layer
contracts when extending or refactoring the code:

- Identity precedence is explicit choice, workspace, eligible access-profile
  preference, project preference, sole eligible identity, then a chooser.
  Recency must not resolve ambiguity or reorder entity lists.
- A physical Kubernetes cluster, access profile, and context alias are distinct.
  Exact access definitions reconcile into one profile; credentials, endpoint or
  TLS differences remain distinct. Namespace-only variants are aliases.
  Conflicting same-name contexts retain provenance and require a specific source.
- Identity-only, Docker and standalone Kubernetes selections do not add unrelated
  cloud components. Missing bindings cannot fall back to ambient credentials.
  Provider observations are scoped to the identity that obtained them.
- Preview, launch and workspace saving agree on components and effective ADC.
  ADC is off without an identity. Cancellation preserves intended context;
  recoverable launch failures retain the draft, and changed dependencies need review.
- Activation validates reviewed catalog entries, local source revisions,
  credential principals, and canonical endpoint/decoded CA fingerprints before
  commit. Do not add remote reachability preflights or silently adopt drift.
  Preserve source namespaces; use `default` only when the source has no namespace.
- Failed/canceled preparation leaves the active context unchanged and cleans only
  abandoned staging. Subshells always create children and preserve exit status;
  equivalent current-shell switches may be no-ops after required ADC verification.
  Session reuse copies materialized state independently and rechecks requested ADC.
- Catalog writes validate references/providers, reject stale revisions, back up
  valid state, and commit atomically. Names are unique within a kind and exclude
  control characters. Preserve user-owned names, tags, visibility and access data.
- Discovery is deterministic and idempotent. Complete refreshes replace only the
  selected identity/scope; partial failures retain prior observations as stale.
  Missing observations do not delete saved resources. Browsing never starts discovery.
- Deletion affects local catalog records, not external resources. Shared-tag
  deletion reviews and removes all assignments atomically. Tags and legacy risk
  fields do not change activation policy; production tags may mark the prompt.
- Store credential references, never credential contents. Diagnostics must not
  leak secrets or authentication callbacks. Noninteractive failure gives recovery
  instructions instead of opening a browser.
- Shell integration must coexist with terminal and theme hooks: preserve shell
  options, terminal markers, secondary/right prompts, and later theme edits.
  Prefer a stable prompt reference over rewriting the prompt on each refresh.
- Keep state understandable without color, tag colors intact when highlighted,
  and Help usable on narrow/resized terminals. Completed background results must
  survive cancellation and errors.

## Testing and diagnostics

```sh
make check
make ci
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
git diff --check
```

`make check` tests, vets, and builds. `make ci` additionally requires macOS/zsh,
checks formatting, and runs race tests. Use focused regressions for changed
behavior; do not treat historical audit results as current verification.

For fictional controller/UI captures:

```sh
CONTEXTHOP_UI_AUDIT_DIR=/tmp/contexthop-ui-captures \
  GOTOOLCHAIN="go$(cat .go-version)" GOCACHE="$PWD/.gocache" \
  go test ./cmd/contexthop -run '^TestUIWalkthrough$' -count=1
```

`chop debug [i|p|k|d|w|<workspace>]` traces switch steps and cache decisions,
buffering diagnostics until the selector yields the terminal.
See [demo recording](demos/README.md) for VHS fixtures.

`internal/testenv` creates disposable test and recording environments. Command
integration tests start with an isolated home, catalog/cache paths and rejecting
provider stubs; each test explicitly constructs any managed session it needs.
Exact subprocess-helper invocations retain their parent test's prepared session.
Use `testenv.New(t, options)` for independent scenarios and test-cleanup restoration,
or `Environment.Environ()` for subprocesses. Environment-changing helpers require
serial tests. The `acme` scenario is shared with VHS; authentication tests may
replace the generated provider stubs. The production app does not import this
package.

Tests cover resolution, staging, cancellation, snapshot isolation, persistence,
stale writes, authentication result delivery and asynchronous responsiveness.
Provider stubs and viewport checks do not prove live-account behavior.
Record manual integration evidence with each [release](../RELEASE.md#release-verification).

## Potential follow-up work

These are ideas, not supported features or release commitments: diagnosis/repair
commands; init-time reconciliation of existing catalogs; safe credential-profile
equivalence; service accounts, impersonation and federation; large-catalog and
accessible line-oriented flows; verified SSH/tmux/remote development; AWS/EKS,
other providers and Bash/Fish; dedicated agent workflows if `chop exec` is insufficient.
Keep changes tied to a concrete need and preserve the invariants above.

## Shared configuration

The TUI's Update shared action verifies the selection, publishes an immutable
snapshot under chop's configuration directory, and asks the launching terminal
to follow it. A locked, atomic pointer update coordinates publishers and readers.
Zsh checks for a new revision before commands and at prompts; each follower gets
an independent session copy. Native tool configuration files are not rewritten.

Apply here and subshells pin their selection. Join shared only changes
following mode; it does not resolve or publish Selected. The TUI reads the shared
manifest through a local callback every two seconds to preview it while pinned.
That read neither copies a session nor contacts a provider. Missing and unreadable
shared configuration are shown separately. The historical `shared-default` disk
path and `CONTEXTHOP_SHARED_REVISION` variable remain compatible.
