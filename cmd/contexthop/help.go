package main

import "fmt"

func printHelp() { fmt.Print(helpText) }

const helpText = `ContextHop — cloud and runtime contexts for your shell.

USAGE
  chop                          Open the workspace picker
  chop <command> [options]      Run a command

SESSIONS
  shell <workspace>             Open an isolated shell; exit to return
  exec <workspace> -- <cmd>     Run a command without changing this shell
  use <destination>             Select a resource or workspace
  -                             Return to this shell's previous context
  reuse                         Copy the most recent live session
  list                          List saved workspaces
  status                        Show this terminal's context
  shared use                    Follow the shared config in this shell
  shared clear                  Clear the shared config

  shell and exec also accept component flags instead of a workspace:
    --identity NAME             Saved identity
    --project NAME              Saved project
    --kubernetes NAME           Saved Kubernetes target
    --docker NAME               Saved Docker context

  Omit unneeded components; saved dependencies still apply.
  Add --no-login to hand sign-in back to you, even in an interactive shell.

AUTHENTICATION & AGENTS
  auth <identity-or-workspace>  Sign in through the browser
    --adc                       Use application-default credentials
    --terminal                  Use terminal sign-in; do not open a browser
  skill                         Print instructions for Codex and Claude
  skill install codex|claude    Install instructions only; no access granted

CONFIGURATION
  config                        Open the catalog picker
  config path                   Show the catalog file path
  config validate               Validate the catalog
  config edit [file]            Edit safely or recover from a file
  config display [mode]         Display mode: summary, prompt, or off
  config summary-startup [on|off]
                                Show or hide the startup summary
  config browser <identity> <chrome|edge> <profile-directory>
                                Bind an identity to a browser profile
  config discover <identity> [project]
                                Discover and save cloud resources
  config cache clear [identity]
                                Clear cached provider metadata
  link <project> <identity>     Associate a project with an identity
  discover [--write|--dry-run]  Preview or import local contexts
  init [--write|--dry-run]      Discover and create configuration
  backup                        Snapshot non-secret local state
  restore [backup-id|latest|path]
                                Restore a backup
  reset [--credentials]         Back up and rebuild from discovery

CONSOLE & SETUP
  console [target] [--page <page>]
                                Open Google Cloud Console
                                Target: identity, project, workspace, or cluster
                                Page: details, workloads, or logs
  shell-init zsh [--in-place]   Print Zsh integration setup
  completion zsh                Print Zsh completion setup
  debug [config|<workspace>]    Trace switching steps and timings
  version                       Show the version
  help                          Show this help

EXAMPLES
  chop shell 'Payments Dev'
  chop exec 'Payments Dev' -- kubectl get pods
  chop shell --no-login --identity 'Acme Engineering' \
    --project acme-development --kubernetes payments-dev

IN THE PICKER
  Tab/Shift+Tab   Change tab        Space        Stage a choice
  Enter           Update shared     Shift+Enter  Pin this terminal
  Alt+Enter       Open subshell     Ctrl+G       Join shared
  Ctrl+W          Save workspace    /            Search
  o               Options           F1           Full keyboard help
  q               Quit              Ctrl+A       Alternative to Shift+Enter

  On macOS, Alt+Enter is Option+Enter.
  Without a terminal, chop prints context; chop config prints its file path.
`
