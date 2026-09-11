"""Reuse full CI only from a successful main push for the exact release commit."""
import json
import os
import subprocess
from urllib.parse import urlencode


def api(endpoint):
    return json.loads(subprocess.check_output(["gh", "api", endpoint], text=True))


def reusable_run(repo, sha):
    workflow = api(f"repos/{repo}/actions/workflows/ci.yml")
    if workflow.get("path") != ".github/workflows/ci.yml":
        return None
    query = urlencode(dict(branch="main", event="push", head_sha=sha, per_page=1))
    runs = api(f"repos/{repo}/actions/workflows/{workflow['id']}/runs?{query}")["workflow_runs"]
    if not runs:
        return None
    run = runs[0]
    if not (run.get("head_sha") == sha and run.get("head_branch") == "main"
            and run.get("event") == "push" and run.get("workflow_id") == workflow["id"]
            and run.get("head_repository", {}).get("full_name") == repo
            and run.get("status") == "completed" and run.get("conclusion") == "success"):
        return None
    # Pin the attempt too: never accept jobs from a previous successful attempt.
    jobs = api(f"repos/{repo}/actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs?per_page=100")["jobs"]
    for name in ("Change scope and documentation", "macOS (arm64)", "Required checks"):
        matches = [job for job in jobs if job.get("name") == name]
        if len(matches) != 1 or not all(matches[0].get(key) == value for key, value in
                                      (("head_sha", sha), ("status", "completed"), ("conclusion", "success"))):
            return None
    return run["id"]


def main():
    run_id = None
    try:
        run_id = reusable_run(os.environ["GITHUB_REPOSITORY"], os.environ["GITHUB_SHA"])
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError):
        print("CI evidence unavailable; running full checks in this release.")
    if run_id:
        print(f"Reusing full CI from run {run_id} for {os.environ['GITHUB_SHA']}.")
    else:
        print("No reusable full CI; running full checks in this release.")
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        output.write(f"reuse={'true' if run_id else 'false'}\n")


if __name__ == "__main__":
    main()
