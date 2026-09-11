# Catalog management and recovery

[Documentation](README.md) · [Keyboard reference](usage.md#keyboard-reference)

The catalog stores identities, projects, Kubernetes access profiles, Docker
targets, named workspaces, tags, and relationships. It contains credential
references, not credential contents. See [config.example.yaml](../config.example.yaml)
for the YAML format.

## Discovery

Run discovery from an **unmanaged terminal**, so imports can see your full local
configuration. Discovery is disabled inside managed ContextHop sessions. Exit a
child shell to return to its parent; for an in-place session, open an unmanaged terminal.

```sh
chop discover                         # preview local reconciliation
chop discover --write                 # save local imports
chop config discover <identity> [project-name-or-id]
chop config cache clear [identity]
```

Local imports read gcloud accounts/configurations, kubeconfig sources, and Docker
contexts without changing their active state. In the TUI, `l` opens import review
on Identities, Kubernetes, and Docker. Imports record metadata and references;
[authentication](credentials.md) may still be needed.

On Workspaces, Identities, Projects, and Kubernetes, `d` opens cloud discovery
scoped to the highlighted resource. Choose projects, clusters in one project,
or an explicit full scan. Returned resources and mappings save atomically,
preserving custom names, tags, hidden flags, and access profiles. Discovery
neither creates workspaces nor launches a shell.

Open operation results through Options. Results distinguish fresh, cached,
failed, and unscanned scopes; observations older than 24 hours are stale.
Shift+R reviews a retry with the original identity and failed/interrupted scopes.
Canceling a scan saves completed results. Complete refreshes replace only that
identity's observations for the selected scope; incomplete refreshes retain
prior results as stale without renewing their timestamps.

Per-project status distinguishes disabled APIs/billing, access denials, failures,
and successful cluster counts. Access through one identity does not establish
another's access, and enumeration does not prove authorization for all operations.

For resources enumeration cannot find, enter them manually or use
`chop link <project> <identity>`. GKE access setup accepts a
`gcloud container clusters get-credentials` command or its arguments and retains
DNS/internal-IP endpoint modes.

## Editing resources and tags

Use Options on the relevant tab. Names support spaces, punctuation, and Unicode;
quote names with spaces in CLI commands.

Hiding keeps relationships intact and survives discovery. Deleting removes only
ContextHop records, so later discovery may import them again. Workspaces are
always visible. Their editor preserves settings and tags; `≈` means matching
components, which can still have different ADC choices.

Routine additions, simple mappings, visibility changes, and tag edits save
immediately. Deletions, removals, cluster access setup, and workspace behavior
changes retain a review. Writes validate references, reject stale revisions,
and create backups. Failed saves retain edits and keep the browser open.

Tags are shared names and colors. Provider labels do not create them.
From a resource's tag editor:

- Remove an assignment to detach it from that resource while keeping the tag.
- Edit a tag name or color to update every assignment.
- **Delete tag everywhere…** reviews all assignments before removing the
  definition and its uses atomically. Unassigned tags can also be deleted.

Tags are searchable metadata and do not trigger confirmation or alter activation.
Explicit `prod`/`production` tags can add a production prompt marker.

## Paths and safe editing

```sh
chop config path
chop config validate
chop config edit [recovery-file]
```

The default catalog is `~/Library/Application Support/contexthop/config.yaml`
on macOS; `CONTEXTHOP_CONFIG` overrides it. Invalid or unsaved external edits
remain in a private recovery file whose path is reported.

Isolated Google credential directories default to `~/.config/contexthop/gcloud/`.
Caches use `contexthop/` under the user cache directory, or
`CONTEXTHOP_CACHE_DIR`. Moving credentials requires updating `cloudSdkConfig`
and `adc` references in the catalog.

## Backup, restore, and reset

```sh
chop backup
chop restore [backup-id|latest|path]
chop reset
```

Backups live beside the catalog under `backups/`. They contain the catalog and
non-secret discovery, validation, and recency metadata, excluding credentials,
generated kubeconfigs, and live sessions.

Restore validates its input, backs up the current state, and clears transient
session material without changing credentials. Reset backs up the catalog,
reimports the host configuration, and clears derived caches. It refuses to run
with active managed sessions. Credentials are preserved unless
`chop reset --credentials` is explicitly requested.

## Upgrading an existing installation

Exit managed shells before upgrading, then reload the Zsh hook in a fresh terminal.
If imported accounts are missing, preview local discovery there before saving.

The catalog remains version 1. Legacy `destinations` and `pinned` fields remain
compatible; pin values have no effect on list ordering. Launcher and pin controls
have been removed; use the [current selection flow](usage.md#selection-and-workspaces).
Existing CLI configuration aliases continue to open Workspaces.

Catalogs are not automatically moved between old and current configuration paths.
Move an older catalog or set `CONTEXTHOP_CONFIG`; update credential paths if those
directories also moved. Old session metadata is not reused. Browser bindings can
be edited through Options or the YAML `browser` block.
