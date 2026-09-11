import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from scripts import ci_scope


class ScopeTests(unittest.TestCase):
    def test_only_known_docs_skip(self):
        self.assertFalse(ci_scope.full_checks(["README.md", "docs/usage.md", "docs/images/browse.gif"]))
        for path in ["cmd/main.go", "go.mod", "Makefile", ".github/workflows/ci.yml",
                     "docs/demos/setup.sh", "internal/testenv/fixtures/acme/catalog.yaml", "scripts/tests/test_dev.py", "new-file"]:
            with self.subTest(path=path):
                self.assertTrue(ci_scope.full_checks(["README.md", path]))
        self.assertTrue(ci_scope.full_checks([]))

    def test_event_ranges(self):
        self.assertEqual(ci_scope.revision_range("pull_request", {"pull_request": {"base": {"sha": "a"}, "head": {"sha": "b"}}}), ("a", "b", True))
        self.assertEqual(ci_scope.revision_range("push", {"before": "a", "after": "b"}), ("a", "b", False))
        self.assertEqual(ci_scope.revision_range("merge_group", {"merge_group": {"base_sha": "a", "head_sha": "b"}}), ("a", "b", False))
        self.assertIsNone(ci_scope.revision_range("unknown", {}))

    def test_release_and_unknown_comparisons_require_full(self):
        for ref, force, event in [("refs/tags/v1.0.0", "false", {}), ("refs/heads/main", "true", {}), ("refs/heads/main", "false", {})]:
            with tempfile.TemporaryDirectory() as tmp:
                event_path, output = Path(tmp)/"event.json", Path(tmp)/"output"
                event_path.write_text(json.dumps(event))
                with patch.dict(os.environ, GITHUB_REF=ref, FORCE_FULL=force, GITHUB_EVENT_PATH=str(event_path), GITHUB_EVENT_NAME="push", GITHUB_OUTPUT=str(output)):
                    ci_scope.main()
                self.assertEqual(output.read_text(), "full=true\n")

    def test_required_gate_rejects_failed_or_unexpected_skips(self):
        workflow = Path(__file__).resolve().parents[2] / ".github/workflows/ci.yml"
        import textwrap
        gate = textwrap.dedent(workflow.read_text().rsplit("run: |\n", 1)[1])
        for scope in ["success", "failure", "cancelled", "skipped"]:
            for full in ["true", "false", ""]:
                for macos in ["success", "failure", "cancelled", "skipped"]:
                    expected = scope == "success" and ((full == "true" and macos == "success") or (full == "false" and macos == "skipped"))
                    result = subprocess.run(["bash", "-e", "-c", gate], env={**os.environ, "SCOPE_RESULT": scope, "FULL": full, "MACOS_RESULT": macos})
                    self.assertEqual(result.returncode == 0, expected, (scope, full, macos))

    def test_rename_cannot_hide_code_change_and_deleted_link_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            old = os.getcwd()
            try:
                os.chdir(tmp)
                def git(*args):
                    return subprocess.check_output(["git", *args], text=True).strip()
                git("init", "-q")
                git("config", "commit.gpgsign", "false")
                git("config", "user.name", "Test")
                git("config", "user.email", "test@example.com")
                Path("code.go").write_text("package example\n")
                Path("README.md").write_text("[Code](code.go)\n")
                git("add", ".")
                git("commit", "-qm", "base")
                base = git("rev-parse", "HEAD")
                git("mv", "code.go", "RELEASE.md")
                git("commit", "-qm", "rename")
                head = git("rev-parse", "HEAD")
                self.assertTrue(ci_scope.full_checks(ci_scope.changed_paths(base, head)[2]))
                with self.assertRaisesRegex(ValueError, "missing link target code.go"):
                    ci_scope.check_documentation(base, head)
                Path("README.md").write_text("[Release](RELEASE.md)\n[Website](https://example.com)\n[Section](#section)\n")
                ci_scope.check_documentation(base, head)
            finally:
                os.chdir(old)


if __name__ == "__main__":
    unittest.main()
