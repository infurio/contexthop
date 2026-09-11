Output docs/images/browse.gif
Source docs/demos/common.tape
Source docs/demos/start.tape
Type "# Filter, Esc to keep results, then Space to stage"
Enter
Sleep 3s
Type "chop"
Enter
Sleep 2s
Type "/payments"
Sleep 2s
Escape
Sleep 500ms
Space
Sleep 2s
# Resume editing, keep the query, then clear it with a second Escape.
Type "/"
Sleep 1s
Escape
Sleep 500ms
Escape
Sleep 1s
# Each entity tab focuses the component staged by the workspace.
Right
Sleep 2s
Right
Sleep 2s
Right
Sleep 2s
# Browsing another cluster leaves the staged choice fixed.
Down
Sleep 2s
# Progressive unstage clears Kubernetes before its project and identity.
Escape
Sleep 2s
Hide
Ctrl+C
Type "exit"
Enter
