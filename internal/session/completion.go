package session

func ZshCompletion() string {
	return `_chop() {
  local -a _chop_names
  if (( CURRENT == 2 )); then
    compadd -- use shell exec console config reuse status shared completion -
  fi
  if (( CURRENT == 3 )) && [[ "$words[2]" == (shared|default) ]]; then
    if [[ "$words[2]" == shared ]]; then compadd -- use clear; else compadd -- follow clear; fi
  fi
  if (( CURRENT == 3 )) && [[ "$words[2]" == config ]]; then
    compadd -- path validate edit browser discover cache prompt-prefix
  fi
  if (( CURRENT == 4 )) && [[ "$words[2]" == config && "$words[3]" == prompt-prefix ]]; then
    compadd -- on off
  fi
  if (( CURRENT == 2 )) || { (( CURRENT == 3 )) && [[ "$words[2]" == (use|shell|exec|console) ]]; }; then
    _chop_names=("${(@f)$(command "${CONTEXTHOP_BINARY:-chop}" _complete "$words[2]" 2>/dev/null)}")
    compadd -- "${_chop_names[@]}"
  fi
}
(( $+functions[compdef] )) && compdef _chop chop
`
}
