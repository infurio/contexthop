# User guide

[Documentation](README.md) · [Catalog management](configuration.md)

## Selection and workspaces

`chop` and `chop config` open **Workspaces**. Tab visits Identities, Projects,
Kubernetes, and Docker. Lists are alphabetical; `/` filters names and metadata.
Starting `chop` highlights the last successfully selected visible resource in
the opening tab.
This does not apply a context until you confirm it.

The header separates **Active**, the observed terminal context, from
**Selected**, the context that launch and save will use. On regular-sized
terminals, aligned columns compare Identity, Project, Kubernetes, and Docker.
A blue `[ADC]` badge beside an identity means ADC is enabled for that column;
no badge means off. Narrow or short terminals use a compact header.

Both Kubernetes columns show the kubeconfig context name and effective namespace.
Selected reads an inherited namespace from the configured local source, or uses
an explicit namespace override. If the source cannot be read, it retains the
“source default” label. Changes made inside an active shell do not change the
selected source defaults.

All tabs share one reserved message row above the border for selection warnings,
discovery feedback, and scope notices. Navigation and table headings stay in place
when a message appears or clears. Selection warnings take priority over tab status;
`i` Details includes both when they coexist. Long messages end with `…`; press `i` for the full explanation.

Selection markers retain their meaning:

- **✓ Green:** staged with Space; stays fixed while browsing.
- **›:** supplied by the cursor or its resolved dependencies.
- **— Grey:** no component selected.

Space on another row replaces that component; Space on the staged row removes
it. Removing an identity also removes its project and Kubernetes selection;
removing a project removes Kubernetes. Docker is independent. A workspace stages
its complete saved combination. Cursor movement never replaces staged components.

Stage an identity to narrow projects, then a project to narrow clusters. You can
also choose a cluster first: eligible preferences or a sole identity resolve
automatically, while ambiguity opens a chooser. Canceling restores the selection.
Browse-all mode widens the list while retaining staging.

**Enter — Update shared** applies Selected to all terminals following the shared
config. Pinned shells and subshells stay unchanged. **Shift+Enter — Pin**
applies it only here; **Option+Enter — Subshell** starts an isolated subshell.
Ctrl+W opens the workspace editor with Selected, including ADC. Saving does not
activate it. Changed fields are marked with `*` and show their previous and new
values; `i` opens the full comparison. Give workspaces memorable names so filtering is useful.

Esc leaves filter editing, then clears a retained filter, or closes a dialog.
At the main list, further Esc presses unstage
Kubernetes, project, identity, then Docker. `c` clears the whole selection.
Tabs share the panel’s top border. Cyan brackets and a bold white label mark
the active tab; inactive labels are muted. Tabs containing a staged selection
turn green, with a brighter label and brackets when active. A staged row under
the cursor uses a bright green background; moving away restores its dark green
highlight. The application name and build version appear at the footer's right edge when space permits.
Staging a workspace focuses its selected resource on the next visit to each
entity tab. A saved filter is cleared only if it hides that resource. After that,
tabs retain browsing state within the same scope. While searching, printable
keys are text; Tab and Left/Right switch tabs immediately and retain each tab's filter.
Returning to a filtered tab lets you browse its results. `/` starts or resumes filter editing.
Esc finishes editing without clearing the query or moving the cursor; Space can then
stage the highlighted result. Press Esc again to clear the filter. Further Esc
presses follow normal selection and navigation behavior. The editing and clear hints appear
beside the query when space permits.
The filter appears in the bottom table border, with the matching and total counts on
the right. While typing, the border and `/` prompt are cyan and the query is white.
A retained filter is muted when browsing results. Filtering does not move the table headings.
All tabs use the same bottom-right count format: resource count, hidden count when
applicable, and a scroll hint when needed. Scrolling shows a range such as
`1–25 of 98 projects · ↑/↓ scroll`; filtered ranges also show the unfiltered total.

## Keyboard reference

Options (`o`) includes Apply, Subshell, Stage/Unstage, and Save workspace, alongside
actions for the highlighted item and tab.
Enter runs an option; Esc returns without changing staging. The footer keeps the
primary controls; Help lists available shortcuts for the current screen.

| Key | Action |
| --- | --- |
| Tab / Shift+Tab, ← / → | Change tab |
| Shift+W/I/P/K/D | Jump to Workspaces / Identities / Projects / Kubernetes / Docker |
| ↑ / ↓, Home / End, PgUp / PgDown | Navigate rows |
| Enter | Update shared — replace the shared config with Selected and follow it here |
| Shift+Enter / Ctrl+A | Pin — apply Selected here and pin it |
| Alt+Enter (Option+Enter on macOS) | Subshell — start an isolated subshell with Selected |
| Space | Stage or unstage |
| Ctrl+G | Join shared without publishing Selected |
| Ctrl+W | Review and save Selected as a workspace |
| Shift+A | Toggle ADC; requires a selected identity |
| `/` / Ctrl+U | Start search / clear search text |
| Esc / `c` | Close or progressively unstage / clear all staging |
| `o` / `i` | Options / resource information |
| `n` | Add a resource |
| `e` / Shift+N on Workspaces | Edit / duplicate |
| `a` on Identities | Login/logout |
| `m` | Manage mappings or identity preferences |
| `b` on Projects/Kubernetes | Toggle selection-based filtering |
| `d` | Cloud discovery, outside Docker |
| `l` on Identities/Kubernetes/Docker | Import local contexts |
| `h` / Shift+H | Show hidden rows / hide or unhide the highlighted item |
| `t` | Manage tags |
| Ctrl+D | Review deletion of a local record; Cancel is selected by default |
| Ctrl+O | Open a supported web console |
| `r` / Ctrl+R | Refresh active shell status / copy another session's context |
| Shift+R, when offered | Review a retry of the last operation |
| `?` / F1 | Help outside text entry / Help anywhere |
| `q` / Ctrl+C | Quit |

Browser profiles and Docker unmapping are Options-only actions. ADC reset is
also in Options after an override. There is no `s` launch-settings shortcut.
Dialogs use Enter to confirm. In change confirmations, `i` opens the full,
scrollable review; Esc returns to confirmation. Text fields use normal
text-editing keys.
Discovery requires an unmanaged shell; see [catalog management](configuration.md#discovery).

## ADC

Shift+A toggles ADC for Selected without staging cursor-derived resources or
changing the active shell. Options shows **Enable ADC** or **Disable ADC** and,
after an override, a reset to the workspace setting or default.

New combinations default to ADC off; saved workspaces can opt in.
Without a selected identity, ADC stays off. Ctrl+W includes the effective ADC
choice when saving. With ADC enabled, all three launch actions prepare CLI and ADC
credentials together when both need authentication, or prepare ADC separately
when CLI login is already valid. They resume your selection after verification. This works with
unsaved selections as well as workspaces.

For ADC setup without launching, highlight an identity and choose
**Authentication** (`a`, or through Options) → **ADC login**. This preserves your
selection and does not enable ADC for a launch. **ADC logout** in the same menu
revokes ADC for all shells and applications using those credentials; shared CLI
credentials may also need login again. Disable ADC in Selected to stop using it
in just that shell.

Credential preparation and launch-time verification are explained in [credentials and isolation](credentials.md).

## Zsh integration

For shared config and current-shell switching, run this once or add it to your
Zsh startup file:

```zsh
eval "$(chop shell-init zsh --in-place)"
```

Integrated terminals follow the shared config at each prompt and before their
next command. **Update shared** (Enter) publishes a verified snapshot and makes
the launching terminal follow it too. New integrated terminals follow automatically.
Without integration, Update shared still saves the shared config and prints setup
instructions; that terminal’s environment stays unchanged. Existing running
applications retain the environment they started with.

Shift+Enter pins this shell to its selection; Alt+Enter starts a pinned subshell.
Neither follows later shared-config changes. The header shows **Active · Shared**
when following the shared config. When
using a pinned shell, it also shows **Shared config** alongside Active and
Selected when there is room. As the terminal gets narrower or shorter, Shared
config is hidden first, then Active, leaving Selected visible. The shared preview
refreshes while chop is open; **Not set** means no shared config has been published.

Press **Ctrl+G** or choose **Options → Join shared** to follow the existing shared
config without publishing Selected. The footer shows this shortcut when a shared
config exists and this shell is not following it. It remains in Options and Help.
**Update shared** publishes Selected and makes this terminal follow it. A saved
workspace is not required for any mode.

```zsh
chop shared use     # resume following in this terminal
chop shared clear   # remove the shared config for all following terminals
```

![Update shared, pin this shell, inspect the shared config, and rejoin](images/shared.gif)

The older `chop default follow` and `chop default clear` commands remain supported
as aliases.

Clearing restores the managed environment bindings each following terminal had
when integration initialized. Pinned shells stay pinned. Shared snapshots live
in chop's configuration directory, and each terminal gets its own session and
kubeconfig. Native gcloud configuration and kubeconfig files remain untouched.
Changing selections through chop updates the shared config; direct changes with
`gcloud config set` or `kubectl config` do not publish a new shared config.
Authentication alone does not change Selected or publish it: press Enter to apply.

After upgrading, reload the integration line above in existing terminals.
To remove integration from a following shell and restore its original bindings,
run `eval "$(chop shell-init zsh)"` and remove the startup line.

Managed child shells already provide integration. Applying preserves the working
directory and unrelated variables while updating managed bindings.

Choose how integrated terminals display the activated selection:

```sh
chop config display summary  # show when selection changes (default)
chop config display prompt   # keep the selection in the prompt
chop config display off      # no automatic display
chop config display          # print the effective mode
chop config summary-startup on   # show once when a terminal opens (default)
chop config summary-startup off  # quiet startup
```

Summary mode leaves your prompt alone. It shows the scope, one workspace or
resource label, and its user-defined tags. For example:

```text
Pinned · Payments Dev · Engineering
```

New terminals and managed subshells show the summary once at startup by default.
Use `chop config summary-startup off` for quiet startup.
This only applies in summary mode; prompt and off modes stay unchanged. The single-line summary appears
after pinning a changed selection or adopting a shared config. It shows the workspace or
resource label and user-defined tags, prefixed with Shared, Pinned, or Subshell.
It has no application name or version. A subshell that joins shared configuration
shows Shared. Unchanged selections and canceled operations do not repeat it. Scripts and redirected
output receive no automatic summary. Use `chop status` for the full status.

Prompt mode shows one label plus the selection's user-defined tags, for example
`[Payments Dev|development]`. It uses the workspace name when selected; otherwise
it uses the Kubernetes, Docker, project or identity label, in that order. A
standalone Kubernetes label includes its namespace when it is not `default`.
The full component breakdown remains available through `chop status`.
Off disables automatic display while keeping context switching and completion active.
The setting is saved as `display: summary`, `display: prompt` or `display: off`.
All integrated terminals using that configuration pick it up at their next prompt.
After upgrading, reload shell integration once in already-open terminals.

The summary scope and separators are muted. Resource values match each item's first
tag in alphabetical order; untagged items use cyan. Tags retain their exact
user-defined names and colours, with no synthetic markers or special tag matching.
Colours and tags are saved with the session; reapply a selection after editing
them, or to add tag labels to sessions created by older versions.
`chop status` uses the same label, resource and tag colours for its full report.
`NO_COLOR` and `TERM=dumb` disable colours; redirected status output is plain text.

Prompt integration preserves the shell's existing `PROMPT_SUBST` setting.
When enabled (as in Oh My Zsh), the context label uses a stable variable reference
so updates do not rewrite terminal prompt markers. Otherwise, the label is
updated literally. Removing integration removes its own label while preserving
later theme edits and terminal markers.

An unchanged current-shell context may be a no-op after required ADC verification.
Recoverable launch failures retain Selected for retry; changed dependencies require
a fresh review. Subshell exit status is preserved.

Integration registers completion if `compinit` has run. For completion alone:

```zsh
eval "$(chop completion zsh)"
```

## CLI launches and session reuse

```sh
chop shell "Payments Dev"
chop exec "Payments Dev" -- terraform plan
chop use kubernetes:local-cluster
chop status
chop -
chop reuse
```

Qualify ambiguous names with an entity kind. `chop -` returns to this terminal's
previous context. `chop reuse` copies the most recently active live session;
Ctrl+R lets you choose one. Copies have independent mutable state and do not
inherit previous-context history. Reuse starts a child in an ordinary terminal
and replaces the current managed context through Zsh integration.

The demo copies a live fixture session with Ctrl+R, switches the copy to
OrbStack, and reads the source session’s current Docker context to show it
still uses `desktop-linux`.

![Copy a live session and switch independently](images/reuse.gif)

## Google Cloud Console

Ctrl+O opens the highlighted account, project, or supported cluster without
switching the terminal. In Identities, choose **Options → Browser profile**
to set a binding, or use:

```sh
chop config browser work chrome "Profile 1"
chop console
chop console "Payments Dev" --page logs
chop console "Payments Dev" --page workloads
```

Use the last directory of **Profile Path** on `chrome://version` or `edge://version`,
not the display name. The profile must exist and be signed into the intended
account. Without a binding, a unique exact-account Chrome or Edge match is used;
missing or ambiguous matches require configuration. Console does not use domain
matches, fall back to the default profile, or verify the website's signed-in account.
Custom browser data roots and non-macOS profile launchers are unsupported.

Accounts open the console home; projects open their dashboard; GKE targets open
cluster details. Logs are cluster-scoped, while Workloads opens the project's
overview. Non-GKE targets linked to GCP open the project dashboard. Docker and
standalone Kubernetes have no web action. Identity ambiguity opens a chooser;
canceling preserves the originating tab and Selected context.
