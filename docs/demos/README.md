# Recording the product demos

The recordings use [Charm VHS](https://github.com/charmbracelet/vhs).
From the repository root:

```sh
brew install vhs
scripts/record-demos
```

Record only affected demos by passing their names, for example:

```sh
scripts/record-demos browse workspace
scripts/record-demos --version 0.5.0 browse
```

The command always builds the current checkout (including uncommitted changes)
into a temporary directory, using the repository's Go toolchain, along with the
shared fixture launcher. It prints the version and pins that executable for every
selected tape and its child sessions.
It never uses or replaces `bin/chop` or the Homebrew installation. By default the
binary displays the source development version; `--version` explicitly sets the
version shown in release recordings. The temporary build is removed on exit.
Run recordings through this command: direct tape execution fails without the
pinned binary. Each tape still gets its own fresh fictional data.

VHS requires `ttyd`, `ffmpeg`, and a headless browser (it can download Chromium).
These tapes use Bash and Zsh; the recordings use Menlo at 1280×650 and 15 fps.
The shared-context tape uses 1640×650 to keep all three comparison columns visible.
`common.tape` contains settings; tapes apply any overrides before sourcing
`start.tape`, which opens the fixture shell.

`setup.sh` delegates to `internal/testenv/fixturecmd`, which starts a clean shell
with a temporary catalog, home, XDG directories, and cache, then removes them on
exit. The Acme catalog and provider commands live in
[`internal/testenv/fixtures/acme`](../../internal/testenv/fixtures/acme) and are
shared with integration tests. Provider commands return fictional responses and
reject unsupported operations. Recordings set a one-second provider delay; tests
use zero delay. The GIFs show the real app's browsing, discovery/persistence, and
shell-isolation flows against these fixtures; they do not demonstrate live
authentication or cluster connectivity.

| Tape | Demonstrates |
| --- | --- |
| `browse.tape` | Filter editing and retention with Esc, workspace staging, entity cursor alignment, and progressive unstaging |
| `workspace.tape` | Compose cloud + Kubernetes + Docker, toggle ADC, review and save “Services Dev”, clear staging, then find and stage the saved workspace |
| `discover.tape` | Discover projects from an identity, retain existing names/tags, then filter and inspect a newly discovered project |
| `shell.tape` | Launch a Docker workspace, switch the current managed shell, return with `chop -`, exit to an unchanged parent, and isolate a single command with `exec` |
| `shared.tape` | Update shared, pin this shell, inspect the shared config in the header, and rejoin with Ctrl+G |
| `reuse.tape` | Copy a live session with Ctrl+R, switch the copy to OrbStack, and verify the source still uses desktop-linux |

`source-session.sh` supplies a real background session for reuse. It publishes
its observed Docker context atomically so the demo can read it after switching
the copy. Setup cleans it up on exit, and it also expires after 90 seconds.

Each tape has one story, a short visible introduction followed by a three-second
pause, and pauses at the results.
Keep README recordings around 20–35 seconds; session reuse lives in the user guide.
In the resource browser, the first Escape leaves filter editing and keeps results;
a second Escape clears the query. `/` resumes editing. Stage with Space after
leaving editing. Left/Right and Tab switch tabs and retain each tab’s filter.
Pause after Escape before typing another key so terminal escape decoding does
not turn the following action into an Alt-key sequence.

ADC is demonstrated as a selection and saved setting; the recordings do not
activate ADC or claim to verify real credentials. Discovery runs from the
unmanaged fixture shell. Each tape gets a fresh catalog, so run them independently.

Before recording, run `vhs validate 'docs/demos/*.tape'`. VHS starts a local
terminal server and needs permission to listen on localhost.

Keep fixture data fictional. Regenerate GIFs after changing the UI or tapes and
review the output for readable text, complete flows, and unintended host details.

## VHS 0.12.0 rendering workaround

VHS 0.12.0 cancels its recording context before passing it to FFmpeg, and suppresses
FFmpeg startup errors. It can print “Creating …” and exit successfully while
leaving an existing GIF unchanged. Verify that output files actually change.

These recordings were generated with VHS 0.12.0 and the accompanying
[rendering patch](vhs-0.12.0.patch). To reproduce without changing the installed VHS:

```sh
recording_repo="$PWD"
recording_tools=$(mktemp -d)
git clone --depth 1 --branch v0.12.0 https://github.com/charmbracelet/vhs.git "$recording_tools/vhs"
git -C "$recording_tools/vhs" apply --unidiff-zero "$recording_repo/docs/demos/vhs-0.12.0.patch"
(cd "$recording_tools/vhs" && go build -o "$recording_tools/vhs-fixed" .)
VHS_BIN="$recording_tools/vhs-fixed" scripts/record-demos browse
```

Use the same `VHS_BIN` setting for the remaining demos too. The patch keeps the parent
context alive for rendering and returns rendering failures to the caller.
