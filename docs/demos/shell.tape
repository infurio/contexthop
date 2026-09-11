Output docs/images/shell.gif
Source docs/demos/common.tape
Source docs/demos/start.tape
Type "# A workspace opens an isolated child shell"
Enter
Sleep 3s
Type "chop shell 'Local containers'"
Enter
Sleep 2s
Type "docker context show"
Enter
Sleep 2s
Type "# Switch this managed shell without nesting another"
Enter
Type "chop use docker:OrbStack"
Enter
Sleep 2s
Type "docker context show"
Enter
Sleep 2s
Type "# Return to this terminal's previous context"
Enter
Type "chop -"
Enter
Sleep 1s
Type "docker context show"
Enter
Sleep 2s
Type "exit"
Enter
Sleep 1s
Type "# The parent stays unchanged; exec isolates a single command"
Enter
Type "echo ${DOCKER_CONTEXT:-unset}"
Enter
Sleep 2s
Type "chop exec 'Local containers' -- docker context show"
Enter
Sleep 2s
Hide
Type "exit"
Enter
