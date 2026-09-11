Output docs/images/shared.gif
Source docs/demos/common.tape
# Keep all three context columns visible in the shared comparison.
Set Width 1640
Source docs/demos/start.tape
Hide
Type "zsh"
Enter
Type `eval "$(chop shell-init zsh --in-place)"`
Enter
Type "clear"
Enter
Sleep 1s
Show
Type "# Enter updates the shared config with this selection"
Enter
Sleep 3s
Type "chop"
Enter
Sleep 1s
Type "/local containers"
Sleep 1s
Enter
Sleep 2s
Type "docker context show"
Enter
Sleep 1s
Type "# Pin this shell; the shared config stays unchanged"
Enter
Type "chop use docker:OrbStack"
Enter
Sleep 1s
Type "# Ctrl+G joins the shared config without changing it"
Enter
Type "chop"
Enter
Sleep 4s
Ctrl+G
Sleep 1s
Type "docker context show"
Enter
Sleep 3s
Hide
Type "exit"
Enter
Type "exit"
Enter
