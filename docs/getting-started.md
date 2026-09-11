# Getting started

[Documentation](README.md)

ContextHop supports **macOS on Apple Silicon (ARM64), with zsh**.
Install the tools you use—`gcloud`, `kubectl`, or Docker—separately.

## 1. Install and import local contexts

From a fresh, unmanaged terminal:

```sh
brew install infurio/tap/contexthop
chop init --write
```

Run `init` only for a new catalog. For an existing catalog, use
`chop discover` to preview local imports and `chop discover --write` to save them.

Imports read the gcloud accounts/configurations, kubeconfig files, and Docker
contexts visible to that terminal. They record account metadata, not login
credentials; an imported identity may need authentication.

## 2. Enable current-shell switching

Add this line to your `~/.zshrc` (or `.zshrc` under your custom `ZDOTDIR`):

```zsh
eval "$(chop shell-init zsh --in-place)"
```

Open a new terminal, or run that line in the current one. Integrated terminals
follow the shared config, checking at each prompt and before each command.
**Enter — Update shared** publishes your selection as the shared config. **Shift+Enter —
Pin** applies only here and pins the selection. **Option+Enter — Subshell**
(Alt+Enter) opens an isolated subshell and works without setup. Pinned shells and
subshells stay unchanged when you apply a new shared config.
Loading integration adopts an existing shared config if one has been published.
Press **Ctrl+G** in chop to rejoin it from a pinned shell without publishing Selected.

## 3. Discover your projects and Kubernetes clusters

Open the app before activating a workspace:

```sh
chop
```

Local imports do not scan all your cloud resources. To populate the app from
Google Cloud:

1. Press Tab to open **Identities**, then highlight the account you want to use.
   If it is missing, press `n` to add it, or `l` to import local identities.
2. Press `d` to open **Discover**. Choose **Scope → Projects and all their clusters**
   for an initial scan, then choose **Start discovery**. Follow authentication
   prompts if needed. A full scan can take longer for accounts with many projects.
3. Browse the results in **Projects** and **Kubernetes**. Discovered resources and
   their identity mappings are saved automatically. Repeat for other identities.

For a smaller scan, choose **Projects** first, then highlight a project and press
`d` to discover **Clusters in one project**. Cloud discovery finds GKE clusters;
use `l` on **Kubernetes** to import other clusters from local kubeconfigs.

Discovery needs an unmanaged terminal and is unavailable after you apply a
context. Open a fresh terminal to discover more resources later. See
[catalog management](configuration.md#discovery) for details and discovery failures.

## 4. Open the matching web console

Highlight a Google Cloud identity, project, or GKE cluster and press **Ctrl+O**.
An identity opens the console home, a project opens its dashboard, and a GKE
cluster opens its details page. Opening the browser does not switch your terminal.

If ContextHop cannot find a unique matching Chrome or Edge profile, go to
**Identities → Options → Browser profile** and bind the intended profile.
That profile must already be signed into the account. See
[console navigation](usage.md#google-cloud-console) for profile setup.

## 5. Save and use your first workspace

1. Use Tab to visit the entity tabs. Stage your identity, project, cluster, and
   optional Docker context with Space; review the **Selected** header.
2. Press Ctrl+W, give the combination a memorable name, review it, and save.
3. Choose **Enter — Update shared**, **Shift+Enter — Pin**, or
   **Option+Enter — Subshell**. Run `exit` to leave a subshell.
4. Next time, open `chop`, filter the workspace name with `/`, and press Enter.

Continue with the [user guide](usage.md) for all shortcuts and ADC, and
[credentials and isolation](credentials.md) for authentication and session boundaries.

To work from a checkout, use the [development setup](architecture.md#local-development).
