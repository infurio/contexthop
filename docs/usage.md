# User guide

[Documentation](README.md) · [Catalog management](configuration.md) · [Credentials](credentials.md)

Chop selects the identity, project, Kubernetes target, and Docker context for
your terminal. Use a saved **workspace** or choose the components you need.

**Start here:** [Interactive picker](#selection-and-workspaces) ·
[CLI commands](#cli-launches-and-session-reuse) · [Codex and Claude](#local-agents)

**Reference:** [Shared terminals](#zsh-integration) · [Display](#terminal-display) ·
[ADC](#adc) · [Keyboard shortcuts](#keyboard-reference) · [Cloud Console](#google-cloud-console)

## Quick start

Open the workspace picker:

```sh
chop
```

Choose where to apply your selection:

| Action | Key | What changes |
| --- | --- | --- |
| Update shared | Enter | Terminals following the shared config |
| Pin | Shift+Enter or Ctrl+A | This terminal only |
| Subshell | Alt+Enter (Option+Enter on macOS) | A new isolated child shell; `exit` returns |

Shared and current-terminal switching require [Zsh integration](#zsh-integration).
For an isolated shell without that setup:

```sh
chop shell 'Payments Dev'
# Work in this environment, then return:
exit
```

Run `chop status` to inspect the active context, or `chop help` for the CLI reference.

## Selection and workspaces

`chop` and `chop config` open **Workspaces**. Tab visits Identities, Projects,
Kubernetes, and Docker. Lists are alphabetical; `/` filters names and metadata.
The initial highlight remembers the last successfully selected visible resource.
Highlighting alone does not activate anything.

### Build a selection

1. Highlight a resource and press **Space** to stage it.
2. Visit another tab and stage the next component. Staged choices stay fixed
   while you browse.
3. Review **Selected** in the header, then choose Update shared, Pin, or Subshell.

Stage an identity to narrow projects, then a project to narrow clusters. You can
also start with a cluster: saved preferences or a sole eligible identity resolve
automatically; ambiguity opens a chooser. Canceling preserves your selection.

| Marker | Meaning |
| --- | --- |
| ✓ Green | Staged with Space; fixed while browsing |
| › | Supplied by the cursor or its dependencies |
| — Grey | Not selected |

Space on a staged row removes it; staging another row replaces that component.
Removing an identity also removes its project and Kubernetes target. Removing a
project removes Kubernetes. Docker is independent. Press `c` to clear everything,
or `b` on Projects/Kubernetes to toggle selection-based filtering.

### Read the header

- **Active** is the observed context of this terminal.
- **Selected** is the combination that launch and save will use.
- **Shared config** previews the published selection when this terminal is pinned
  and there is enough room. **Active · Shared** means this terminal follows it.
- **[ADC]** beside an identity means application-default credentials are enabled.

Kubernetes values include the context name and effective namespace. Selected
uses the source namespace unless explicitly overridden. If the source cannot
be read, it shows “source default”. Changes inside a running shell do not alter
the selected source defaults.

Warnings appear above the table; press `i` for the full explanation. Smaller
terminals use a compact header that prioritizes Selected.

### Save a workspace

Press **Ctrl+W** to review and save Selected, including its ADC setting.
Saving does not activate it. Changed fields have a `*`; press `i` in the
confirmation to inspect the full comparison.

A workspace stages its complete saved combination. Use memorable names so
filtering is useful. See [catalog management](configuration.md) for editing and discovery.

### Search and navigation

Press `/` to start or resume filtering. Tab and Left/Right switch tabs while
retaining their filters. Press Esc once to finish editing and browse results;
press it again to clear the filter. Space stages the highlighted result.

Outside search or dialogs, repeated Esc presses unstage Kubernetes, project,
identity, then Docker. Tabs retain browsing state within the same scope.
After staging a workspace, each component tab first focuses its selected
resource, clearing a filter only if it hides that resource.

## CLI launches and session reuse

### Use a saved workspace

```sh
chop list                                  # list workspaces
chop shell 'Payments Dev'                   # open an isolated shell
chop exec 'Payments Dev' -- kubectl get pods # run one command
chop status                                # inspect this terminal
```

`shell` keeps the environment active until you exit. `exec` selects it for one
command without changing the calling shell. It can also run an existing script
or a multiline command through `sh -c`.

### Explicit component selection

Use saved component names instead of creating a workspace:

```sh
chop shell \
  --identity 'Acme Engineering' \
  --project acme-development \
  --kubernetes payments-dev \
  --docker 'Local Docker'
```

The same flags work with `exec`:

```sh
chop exec \
  --identity 'Acme Engineering' \
  --project acme-development \
  -- gcloud projects describe acme-development
```

Omit components you do not need. Saved dependencies still apply: a cluster can
select its project, and a project can select an unambiguous eligible identity.
Nothing is filled from the surrounding shell. Conflicting explicit choices and
ambiguous identities fail instead of silently selecting another target.

These selections do not save a workspace or change shared config. Values are
**catalog names**, not arbitrary provider IDs. Do not combine a workspace with
component flags; `none` is not a special value. Use `--workspace=NAME` for a
workspace name beginning with a dash.

### Switch or copy a context

```sh
chop use kubernetes:payments-dev # qualify ambiguous names with an entity kind
chop -                          # return to this managed terminal's previous context
chop reuse                      # copy the most recent live session
```

With Zsh integration, `use` changes the current terminal; in an ordinary terminal
it opens a child shell. `reuse` follows the same pattern. Press **Ctrl+R** in the
picker to choose a session to copy.

Copies have independent mutable state and no inherited previous-context history.
The demo switches a copy to OrbStack while the source keeps `desktop-linux`:

![Copy a live session and switch independently](images/reuse.gif)

## Local agents

Tell Codex or Claude:

> Use Chop for this investigation. Run `chop skill` first.

To make the instructions available in future chats, install the skill for your agent:

```sh
chop skill install codex
# Or:
chop skill install claude
```

Start a new chat after installation. This installs instructions only: it does
not copy credentials, grant command permissions, or change agent settings.

### Choose the execution mode

For a one-off command or comparison:

```sh
chop exec --no-login 'Payments Dev' -- kubectl get pods
```

For sustained work in a persistent interactive terminal:

```sh
chop shell --no-login 'Payments Dev'
```

The agent must send subsequent commands to that **same terminal session**.
Use the startup summary and `chop status` to confirm the context, then `exit`
before opening another environment. Other terminals retain their own contexts.
If the agent cannot keep a terminal alive, use `exec`.

Both modes accept [component flags](#explicit-component-selection).
`--no-login` reuses valid credentials but hands any required sign-in back to you,
even with an interactive terminal. It applies only to that launch; it is not
inherited by commands inside the child shell.

### When access needs attention

- **Authentication required; command not started:** run the printed `chop auth`
  command in your own terminal. Tell the agent when finished so it can retry once.
- **`provider_access_denied`; command not started:** the agent can request host
  approval and retry the same command once. If that fails, report the access
  error; another login is not the remedy.
- **Network, configuration, or child-command failure:** report or investigate it.
  Do not automatically replay a command that already started.

Authentication handoffs exit with code 77. Child commands preserve their own exit
codes, so **exit 77 alone is not a signal to authenticate or retry**. ADC handoffs
print `chop auth --adc` for the resolved identity.

<details>
<summary>Skill installation paths and updates</summary>

The installer writes `chop/SKILL.md` under `$CODEX_HOME/skills` (default
`~/.codex/skills`) or `~/.claude/skills`. Reinstalling identical instructions is a
no-op. A differing existing skill is left intact: use `chop skill` to review the
new instructions and update it manually.

</details>

## Zsh integration

For shared config and current-shell switching, run this once or add it to your
Zsh startup file:

```zsh
eval "$(chop shell-init zsh --in-place)"
```

New integrated terminals follow the shared config automatically. Followers adopt
updates at prompts and before their next command. Already-running applications
keep the environment they started with.

### Share, pin, and rejoin

- **Enter — Update shared:** publish Selected and follow it here. Other followers
  adopt it; pinned shells and subshells stay unchanged.
- **Shift+Enter — Pin:** apply Selected only here and stop following updates.
- **Alt+Enter — Subshell:** start a pinned child shell.
- **Ctrl+G — Join shared:** follow the existing shared config without publishing Selected.

None of these actions requires a saved workspace. Without integration, Update
shared still saves the shared config and prints setup instructions, but leaves
that terminal's environment unchanged.

```zsh
chop shared use   # resume following in this terminal
chop shared clear # clear the shared config for following terminals
```

![Update shared, pin this shell, inspect the shared config, and rejoin](images/shared.gif)

Each follower gets its own session and kubeconfig; native provider configuration
files remain untouched. Direct changes with `gcloud config set` or `kubectl config`
do not publish shared updates. Authentication alone does not publish Selected either.

Clearing shared config restores the managed bindings followers had when integration
initialized. Pinned shells stay pinned. The older `chop default follow` and
`chop default clear` aliases remain supported.

### Reload, remove, or enable completion

After upgrading, rerun the integration line in existing terminals.
To remove integration and restore a following shell's original bindings, run:

```zsh
eval "$(chop shell-init zsh)"
```

Also remove the integration line from your startup file. Managed child shells
already provide integration. Switching preserves the working directory and
unrelated environment variables. Recoverable launch failures retain Selected
for retry; changed dependencies require a fresh review.

Integration registers completion when `compinit` has run. For completion alone:

```zsh
eval "$(chop completion zsh)"
```

## Terminal display

The default summary leaves your prompt alone and shows the scope, selection label,
and user-defined tags:

```text
Pinned · Payments Dev · Engineering
```

It appears once at startup and after a changed selection. Unchanged selections
and canceled operations do not repeat it. Scripts and redirected output remain
quiet. Use `chop status` for the full component breakdown.

```sh
chop config display summary      # show when selection changes (default)
chop config display prompt       # keep the label and tags in the prompt
chop config display off          # disable automatic display
chop config display              # show the current mode

chop config summary-startup off  # quiet startup in summary mode
chop config summary-startup on   # restore startup summary (default)
```

The display mode applies to integrated terminals at their next prompt. Off leaves
switching and completion active. Prompt mode uses the workspace name, or the
Kubernetes, Docker, project, or identity label in that order.

<details>
<summary>Colours and prompt compatibility</summary>

Resource colours follow the item's first alphabetical tag; untagged resources use
cyan. Tags keep their user-defined names and colours. Reapply a selection after
editing tags or colours to update an existing session. `NO_COLOR` and `TERM=dumb`
disable colours; redirected status output is plain text.

Prompt integration preserves `PROMPT_SUBST`, terminal markers, and theme changes.
Removing integration removes only its own label. A standalone Kubernetes prompt
label includes the namespace when it is not `default`.

</details>

## ADC

Application-default credentials (ADC) are opt-in. New combinations default to off;
saved workspaces can enable them. Press **Shift+A** to toggle ADC for Selected.
This neither activates the selection nor stages cursor-derived resources. Options
also offers enable, disable, and reset to the workspace setting after an override.

ADC requires a selected identity. Ctrl+W saves the effective setting with a
workspace. Interactive launch actions prepare CLI and ADC credentials together
when both need login, or ADC separately when CLI login is valid. Launch resumes
after verification; this also works for unsaved selections.

To prepare ADC without launching, highlight an identity and choose
**Authentication → ADC login** (`a` or Options). This does not enable ADC for a launch.

**ADC logout affects every shell and application using those credentials**;
shared CLI credentials may also need login again. To stop using ADC in just your
selection, disable it instead of logging out.

See [credentials and isolation](credentials.md) for storage and verification.

## Keyboard reference

Press **F1** for screen-specific help or **`o`** for Options. Browser profiles and
Docker unmapping are Options-only actions. Dialogs use Enter to confirm and Esc
to cancel; `i` opens the full review in change confirmations.

### Navigation and selection

| Key | Action |
| --- | --- |
| Tab / Shift+Tab, ← / → | Change tab |
| Shift+W/I/P/K/D | Jump to Workspaces / Identities / Projects / Kubernetes / Docker |
| ↑ / ↓, Home / End, PgUp / PgDown | Navigate rows |
| Space | Stage or unstage |
| `/` / Ctrl+U | Start search / clear search text |
| Esc / `c` | Close or progressively unstage / clear all staging |
| `b` on Projects/Kubernetes | Toggle selection-based filtering |
| `o` / `i` | Options / information |
| `?` / F1 | Help outside text entry / Help anywhere |
| `q` / Ctrl+C | Quit |

### Launch and sessions

| Key | Action |
| --- | --- |
| Enter | Publish Selected to shared config and follow it here |
| Shift+Enter / Ctrl+A | Apply Selected here and pin this terminal |
| Alt+Enter (Option+Enter on macOS) | Start an isolated subshell |
| Ctrl+G | Join shared without publishing Selected |
| Ctrl+W | Review and save Selected as a workspace |
| Shift+A | Toggle ADC for Selected |
| `r` / Ctrl+R | Refresh active status / copy another session's context |
| Shift+R, when offered | Review a retry of the last operation |
| Ctrl+O | Open a supported web console |

### Catalog management

| Key | Action |
| --- | --- |
| `n` | Add a resource |
| `e` / Shift+N on Workspaces | Edit / duplicate |
| `a` on Identities | Authentication menu |
| `m` | Manage mappings or identity preferences |
| `d`, outside Docker | Cloud discovery |
| `l` on Identities/Kubernetes/Docker | Import local contexts |
| `h` / Shift+H | Show hidden rows / hide or unhide an item |
| `t` | Manage tags |
| Ctrl+D | Review deletion of a local record; defaults to Cancel |

Discovery requires an unmanaged shell. See [catalog management](configuration.md#discovery).

## Google Cloud Console

Press **Ctrl+O** to open the highlighted account, project, or supported cluster
without switching the terminal. You can also use:

```sh
chop console
chop console 'Payments Dev' --page logs
chop console 'Payments Dev' --page workloads
```

To bind an identity to a browser profile, choose **Options → Browser profile**
on Identities, or run:

```sh
chop config browser 'Acme Engineering' chrome 'Profile 1'
```

Use the last directory of **Profile Path** on `chrome://version` or `edge://version`,
not the profile's display name. The profile must exist and already be signed into
the intended account. Without a binding, Chop uses a unique exact-account Chrome
or Edge match; missing or ambiguous matches require configuration.

Accounts open the console home, projects their dashboard, and GKE targets their
cluster details. Logs are cluster-scoped; Workloads opens the project's overview.
Non-GKE targets linked to GCP open the project dashboard. Docker and standalone
Kubernetes have no web action. Identity ambiguity opens a chooser; canceling
preserves the tab and selection.

Console does not use domain matches, fall back to the default browser profile,
or verify the website's signed-in account. Custom browser data roots and
non-macOS profile launchers are unsupported.
