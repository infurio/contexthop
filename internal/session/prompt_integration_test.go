package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/infurio/contexthop/internal/testenv"
)

func TestPromptRefreshWithTerminalMarkers(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh unavailable")
	}
	env, err := testenv.Create(t.TempDir(), testenv.Options{Scenario: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(env.Bin, "chop")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = _prompt ]; then printf '%s' \"$CONTEXTHOP_PROMPT_PREFIX\"; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	init := filepath.Join(env.Root, "init.zsh")
	if err := os.WriteFile(init, []byte(CurrentShellInit(binary)), 0600); err != nil {
		t.Fatal(err)
	}
	remove, err := ShellInit("zsh")
	if err != nil {
		t.Fatal(err)
	}
	for _, substitution := range []string{"on", "off"} {
		for _, terminal := range []string{"plain", "opaque", "snapshot"} {
			t.Run(substitution+"/"+terminal, func(t *testing.T) {
				// Fictional terminal hooks model either opaque wrapping or Ghostty's
				// snapshot contract. No real startup files or configuration are read.
				script := `
if [[ $2 == on ]]; then setopt promptsubst; else unsetopt promptsubst; fi
original_options=$options[promptsubst]
CONTEXTHOP_SCOPE=local
export CONTEXTHOP_PROMPT_PREFIX=''
base=$'theme\n> '
PROMPT=$base
PS2='continuation> '
RPROMPT='right'
fixture_hook() { :; }
precmd_functions=(fixture_hook)
preexec_functions=(fixture_hook)
source "$1"
[[ $PROMPT == "$base" ]] || exit 21
CONTEXTHOP_PROMPT_PREFIX='%F{39}[Acme]%f '
_chop_refresh_prompt
mark_a=$'%{\e]133;A;cl=line\a%}'
mark_b=$'%{\e]133;B\a%}'
if [[ $3 == plain ]]; then mark_a=''; mark_b=''; fi
terminal_style=$3
if [[ $terminal_style == snapshot ]]; then
  _ghostty_precmd() { :; }
fi
terminal_precmd() {
  if [[ $terminal_style == snapshot ]]; then
    if [[ -n ${_ghostty_marked_ps1+x} && $PROMPT == "$_ghostty_marked_ps1" ]]; then
      PROMPT=$_ghostty_saved_ps1
    fi
    _ghostty_saved_ps1=$PROMPT
    PROMPT="${mark_a}${PROMPT}${mark_b}"
    _ghostty_marked_ps1=$PROMPT
  elif [[ -n $mark_a && $PROMPT != "$mark_a"* ]]; then
    PROMPT="${mark_a}${PROMPT}${mark_b}"
  fi
}
check_prompt() {
  local fragment=$CONTEXTHOP_PROMPT_PREFIX
  if [[ -o promptsubst ]]; then fragment='${CONTEXTHOP_APPLIED_PROMPT_PREFIX}'; fi
  [[ $PROMPT == "${mark_a}${fragment}${base}${mark_b}" ]] || exit 10
  [[ $options[promptsubst] == $original_options ]] || exit 11
  [[ $PS2 == 'continuation> ' && $RPROMPT == right ]] || exit 12
  [[ ${(j: :)precmd_functions} == 'fixture_hook _chop_refresh_prompt _chop_show_summary' ]] || exit 18
  [[ ${(j: :)preexec_functions} == 'fixture_hook _chop_sync_default' ]] || exit 19
  # Verify substitution actually renders the current label, including colors.
  if [[ -o promptsubst ]]; then
    [[ $(print -P -- "$PROMPT") == $(print -P -- "${mark_a}${CONTEXTHOP_PROMPT_PREFIX}${base}${mark_b}") ]] || exit 13
  fi
}
terminal_precmd
check_prompt
for i in {1..8}; do
  before=$PROMPT
  _chop_refresh_prompt
  [[ $PROMPT == "$before" ]] || exit 14
  terminal_precmd
  check_prompt
done
# Context changes must preserve the terminal's prompt snapshot when substitution
# is enabled, and preserve exactly one pair of markers in either mode.
before=$PROMPT
CONTEXTHOP_PROMPT_PREFIX='[Acme development] '
if [[ $terminal_style != snapshot ]]; then
  # Stale variables do not authorize restoring a terminal snapshot when that
  # terminal's hook is not installed.
  _ghostty_marked_ps1=$PROMPT
  _ghostty_saved_ps1='unrelated snapshot'
fi
_chop_refresh_prompt
if [[ -o promptsubst && $PROMPT != "$before" ]]; then exit 15; fi
terminal_precmd
check_prompt
# Disabling and re-enabling keeps markers intact, without a fallback label.
CONTEXTHOP_PROMPT_PREFIX=''
_chop_refresh_prompt
terminal_precmd
check_prompt
CONTEXTHOP_PROMPT_PREFIX='[Acme development] '
_chop_refresh_prompt
terminal_precmd
check_prompt
# A command's preexec can restore a clean prompt before our hook runs.
if [[ $terminal_style == snapshot ]]; then PROMPT=$_ghostty_saved_ps1; fi
_chop_refresh_prompt
terminal_precmd
check_prompt
# A theme may replace its prompt after the terminal took its snapshot.
base=$'new theme\n> '
PROMPT=$base
_chop_refresh_prompt
terminal_precmd
check_prompt
# Re-sourcing integration is idempotent, too.
source "$1"
terminal_precmd
check_prompt
# Removing disabled integration preserves terminal markers and later theme edits.
CONTEXTHOP_PROMPT_PREFIX=''
_chop_refresh_prompt
terminal_precmd
check_prompt
PROMPT="${PROMPT}tail"
` + remove + `
[[ $PROMPT == "${mark_a}${base}${mark_b}tail" ]] || exit 16
[[ $PS2 == 'continuation> ' && $RPROMPT == right ]] || exit 17
[[ ${(j: :)precmd_functions} == fixture_hook && ${(j: :)preexec_functions} == fixture_hook ]] || exit 20
`
				command := exec.Command(zsh, "-f", "-c", script, "_", init, substitution, terminal)
				command.Env = env.Environ()
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("prompt integration: %v: %s", err, output)
				}
			})
		}
	}
}
