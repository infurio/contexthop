package main

import "fmt"

func printHelp() {
	fmt.Print(`ContextHop — cloud and runtime contexts for your shell.

Usage: chop [command]
  chop (or chop config) opens Workspaces in an interactive terminal.
  Without a terminal, chop prints context; chop config prints the config path.

Sessions:
  use <destination>                     select a destination (also: chop <destination>)
  -                                     switch back in this managed shell
  reuse                                 reuse the most recent live session
  shell <workspace>                     enter an isolated workspace shell
  exec <workspace> -- <command> [args...] run a command in a workspace
  shared use                        follow the shared config in this shell
  shared clear                         clear the shared config
  status                                show the observed context
  list                                  list resolved workspaces
  console [target] [--page <page>]      open Google Cloud Console
                                        target: identity, project, workspace or cluster
                                        page: details, workloads or logs

Configuration:
  config path                           show the configuration path
  config validate                       validate the configuration
  config prompt-prefix [on|off]         show or change the terminal prompt prefix setting
  config edit [recovery-file]           safely edit or recover configuration
  config browser <identity> <chrome|edge> <profile-directory>
                                        configure a browser profile
  config discover <identity> [project]  discover and save cloud projects/clusters
  config cache clear [identity]         clear cached provider metadata
  discover [--write|--dry-run]          preview or import local contexts
  init [--write|--dry-run]              discover and create configuration
  backup                                snapshot non-secret local state
  restore [backup-id|latest|path]       restore a backup
  reset [--credentials]                 back up and rebuild from discovery

Authentication:
  auth [--adc] <identity-or-workspace>  authenticate isolated credentials
  link <project> <identity>             manually associate a project with an identity

Setup and diagnostics:
  shell-init zsh [--in-place]           print shell integration setup
  completion zsh                        print Zsh completion setup
  debug [config|<workspace>]            trace switching steps and timings
  version                               show the version
  help                                  show this help

In the application:
  Tab / Shift+Tab       switch tabs; / searches the current list
  Space                stage a choice
  Enter                Update shared: update the shared config
  Shift+Enter / Ctrl+A Apply here: pin this shell
  Alt+Enter            start a subshell (Option+Enter on macOS)
  Ctrl+G               Join shared without publishing Selected
  Ctrl+W               save Selected as a workspace
  o                    options; F1 opens full keyboard help
  q                    quit (outside search)
`)
}
