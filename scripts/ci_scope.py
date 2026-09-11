"""Select full CI unless a known Git diff contains only documentation."""
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
from urllib.parse import unquote, urlsplit


def documentation(path):
    p = PurePosixPath(path)
    return (path in {"README.md", "RELEASE.md"}
            or (p.parts[0] == "docs" and p.suffix == ".md")
            or (p.parent == PurePosixPath("docs/images")
                and p.suffix in {".gif", ".png", ".jpg", ".svg"}))


def revision_range(event_name, event):
    if event_name == "pull_request":
        return event["pull_request"]["base"]["sha"], event["pull_request"]["head"]["sha"], True
    if event_name == "push":
        return event["before"], event["after"], False
    if event_name == "merge_group":
        return event["merge_group"]["base_sha"], event["merge_group"]["head_sha"], False
    return None


def changed_paths(base, head, merge_base=False):
    if not all(re.fullmatch(r"[0-9a-f]{40}", ref) and int(ref, 16) for ref in (base, head)):
        raise ValueError("No usable comparison commits")
    if merge_base:
        base = subprocess.check_output(["git", "merge-base", base, head], text=True).strip()
    # A code file renamed into docs must still trigger full CI.
    data = subprocess.check_output(["git", "diff", "--no-renames", "--name-only", "-z", base, head])
    return base, head, [p for p in data.decode().split("\0") if p]


def full_checks(paths):
    return not paths or not all(documentation(p) for p in paths)


def check_documentation(base, head):
    subprocess.run(["git", "diff", "--check", base, head], check=True)
    # Check local inline links/images across docs, including links to deleted files.
    files = subprocess.check_output(["git", "ls-files", "-z"], text=True).split("\0")
    errors = []
    for name in files:
        if not name.endswith(".md") or not documentation(name):
            continue
        text = Path(name).read_text()
        text = re.sub(r"(?ms)^```.*?^```[^\n]*", "", text)
        for match in re.finditer(r"\]\((<[^>]+>|[^\s)]+)(?:\s+\"[^\"]*\")?\)", text):
            target = urlsplit(match[1].strip("<>"))
            if target.scheme or target.netloc or not target.path:
                continue
            path = unquote(target.path)
            local = Path(path.lstrip("/")) if path.startswith("/") else Path(name).parent / path
            if not local.exists():
                errors.append(f"{name}: missing link target {path}")
    if errors:
        raise ValueError("\n".join(errors))


def main():
    full = True
    if os.environ.get("FORCE_FULL") != "true" and not os.environ.get("GITHUB_REF", "").startswith("refs/tags/"):
        try:
            event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
            revisions = revision_range(os.environ["GITHUB_EVENT_NAME"], event)
            if revisions:
                base, head, paths = changed_paths(*revisions)
                full = full_checks(paths)
        except (KeyError, ValueError, OSError, subprocess.CalledProcessError) as exc:
            print(f"Cannot establish a documentation-only diff; running full CI: {exc}")
    if not full:
        check_documentation(base, head)
    print("Full checks required" if full else "Documentation checks passed; skipping macOS build and tests")
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        output.write(f"full={'true' if full else 'false'}\n")


if __name__ == "__main__":
    main()
