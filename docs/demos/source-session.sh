#!/bin/bash
# A real live session stands in for the user's other terminal.
set -eu
printf '%s\n' "$$" > "$HOME/source-pid"
trap 'exit 0' TERM INT
for ((tick=0; tick<90; tick++)); do
  docker context show > "$HOME/source-context.next"
  mv "$HOME/source-context.next" "$HOME/source-context"
  sleep 1
done
