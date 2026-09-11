"""Exercise the dev launcher with real Zsh and a fictional Homebrew installation."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parents[1]


@unittest.skipUnless(shutil.which("zsh"), "requires zsh")
class DevelopmentShell(unittest.TestCase):
    def test_repo_binding_survives_user_hooks_and_nested_login_shells(self):
        for custom in (False, True):
            with self.subTest(custom_zdotdir=custom), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                repo = root / "checkout with spaces"
                home = root / "home"
                user_rc = root / "custom rc" if custom else home
                tools = root / "tools"
                for p in (repo / "scripts", repo / "bin", home, user_rc, tools):
                    p.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(SCRIPTS / "dev", repo / "scripts/dev")
                make = tools / "make"
                make.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$DEV_MAKE_LOG"\n')
                make.chmod(0o755)
                binary = repo / "bin/chop"
                binary.write_text('''#!/bin/sh
if [ "$1" = shell-init ]; then
  printf "export CONTEXTHOP_BINARY='%s'\\n" "$0"
  echo 'chop() { command "$CONTEXTHOP_BINARY" "$@"; }'
else
  echo REPO_BINARY
fi
''')
                binary.chmod(0o755)
                brew = tools / "chop"
                brew.write_text('#!/bin/sh\necho WRONG_BREW_BINARY\n')
                brew.chmod(0o755)
                hook = '''export PATH="$DEV_FAKE_BREW:$PATH"
export CONTEXTHOP_BINARY="$DEV_FAKE_BREW/chop"
unalias chop 2>/dev/null || true
chop() { command "$DEV_FAKE_BREW/chop" "$@"; }
alias chop="$DEV_FAKE_BREW/chop"
'''
                (user_rc / ".zshrc").write_text(hook)
                (user_rc / ".zlogin").write_text(hook)
                env = dict(HOME=str(home), PATH=str(tools) + ":/usr/bin:/bin", TERM="dumb",
                           DEV_FAKE_BREW=str(tools), DEV_MAKE_LOG=str(root / "make.log"),
                           CONTEXTHOP_BINARY=str(brew))
                if custom:
                    # Redirect from .zshenv, as some Zsh setups do.
                    (home / ".zshenv").write_text('export ZDOTDIR="$DEV_USER_RC"\n')
                    env["DEV_USER_RC"] = str(user_rc)
                commands = '''chop version
command chop version
print -r -- "OVERLAY=$ZDOTDIR"
zsh -ic 'chop version; command chop version'
zsh -lic 'chop version; command chop version'
# Emulate the original-startup path used by ContextHop managed subshells.
zsh -f -c 'source "$CONTEXTHOP_ORIGINAL_ZDOTDIR/.zshenv"; source "$ZDOTDIR/.zshrc"; chop version'
exit
'''
                result = subprocess.run(["bash", str(repo / "scripts/dev")], input=commands,
                                        env=env, cwd=root, capture_output=True, text=True, timeout=30)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.count("REPO_BINARY"), 7, result.stdout + result.stderr)
                self.assertNotIn("WRONG_BREW_BINARY", result.stdout + result.stderr)
                self.assertIn(str(binary), result.stdout)
                self.assertIn("build", (root / "make.log").read_text())
                self.assertEqual((user_rc / ".zshrc").read_text(), hook)
                self.assertEqual((user_rc / ".zlogin").read_text(), hook)
                overlay = next(line.split("=", 1)[1] for line in result.stdout.splitlines() if line.startswith("OVERLAY="))
                self.assertFalse(Path(overlay).exists(), "temporary startup overlay leaked")


if __name__ == "__main__":
    unittest.main()
