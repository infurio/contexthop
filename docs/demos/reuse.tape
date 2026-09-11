Output docs/images/reuse.gif
Source docs/demos/common.tape
Source docs/demos/start.tape
Hide
Type "chop exec 'Local containers' -- bash docs/demos/source-session.sh > /dev/null 2>&1 &"
Enter
Sleep 2s
Type "clear"
Enter
Show
Type "# Ctrl+R copies a live session into an independent shell"
Enter
Sleep 3s
Type "chop"
Enter
Sleep 1s
Ctrl+R
Sleep 3s
Enter
Sleep 2s
Type "docker context show"
Enter
Sleep 1s
Type "chop use docker:OrbStack"
Enter
Sleep 1s
Type "docker context show"
Enter
Sleep 2s
Type "# The source session still uses desktop-linux"
Enter
Type `cat "$HOME/source-context"`
Enter
Sleep 3s
Hide
Type "exit"
Enter
Type "exit"
Enter
