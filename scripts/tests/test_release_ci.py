"""Check release CI evidence without calling GitHub or publishing anything."""
import copy
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from scripts.release import release_ci, release_notes


class ReleaseCI(unittest.TestCase):
    def setUp(self):
        self.sha = 'a' * 40
        self.run = dict(id=20, run_attempt=2, workflow_id=10, head_sha=self.sha,
                        head_branch='main', event='push', status='completed',
                        conclusion='success', head_repository=dict(full_name='example/source'))
        self.jobs = [dict(name=name, head_sha=self.sha, status='completed', conclusion='success')
                     for name in ('Change scope and documentation', 'macOS (arm64)', 'Required checks')]

    def check(self, run=None, jobs=None, workflow=None):
        with patch.object(release_ci, 'api', side_effect=[
            workflow or dict(id=10, path='.github/workflows/ci.yml'),
            dict(workflow_runs=[self.run if run is None else run]),
            dict(jobs=self.jobs if jobs is None else jobs),
        ]) as api:
            result = release_ci.reusable_run('example/source', self.sha)
            return result, api

    def test_exact_successful_main_run_and_attempt(self):
        result, api = self.check()
        self.assertEqual(result, 20)
        self.assertIn('head_sha=' + self.sha, api.call_args_list[1].args[0])
        self.assertIn('/20/attempts/2/jobs', api.call_args_list[2].args[0])

    def test_untrusted_or_incomplete_run_is_not_reused(self):
        for key, value in [('head_sha', 'b' * 40), ('head_branch', 'feature'),
                           ('event', 'pull_request'), ('workflow_id', 99),
                           ('status', 'in_progress'), ('conclusion', 'failure'),
                           ('head_repository', dict(full_name='fork/source'))]:
            with self.subTest(key=key):
                self.assertIsNone(self.check(run=dict(self.run, **{key: value}))[0])
        self.assertIsNone(self.check(workflow=dict(id=10, path='.github/workflows/other.yml'))[0])

    def test_skipped_failed_missing_duplicate_or_wrong_commit_jobs(self):
        for key, value in [('conclusion', 'skipped'), ('conclusion', 'failure'),
                           ('status', 'in_progress'), ('head_sha', 'b' * 40)]:
            jobs = copy.deepcopy(self.jobs)
            jobs[1][key] = value
            self.assertIsNone(self.check(jobs=jobs)[0])
        self.assertIsNone(self.check(jobs=self.jobs[:1])[0])
        self.assertIsNone(self.check(jobs=self.jobs + [self.jobs[1]])[0])

    def test_no_runs(self):
        with patch.object(release_ci, 'api', side_effect=[dict(id=10, path='.github/workflows/ci.yml'), dict(workflow_runs=[])]):
            self.assertIsNone(release_ci.reusable_run('example/source', self.sha))

    def test_api_error_falls_back_to_full_checks(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'output'
            with patch.dict(os.environ, GITHUB_REPOSITORY='example/source', GITHUB_SHA=self.sha, GITHUB_OUTPUT=str(output)), patch.object(release_ci, 'api', side_effect=subprocess.CalledProcessError(1, 'gh')):
                release_ci.main()
            self.assertEqual(output.read_text(), 'reuse=false\n')


class ReleaseNotes(unittest.TestCase):
    def test_reviewed_notes_and_installation_are_combined(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'docs/releases').mkdir(parents=True)
            path = root / 'docs/releases/1.2.3.md'
            path.write_text('# ContextHop 1.2.3\n\nFaster releases.\n')
            notes = release_notes.render('v1.2.3', 'a' * 40, root)
            self.assertIn('Faster releases.', notes)
            self.assertIn('brew upgrade infurio/tap/contexthop', notes)
            self.assertIn('Source commit: ' + 'a' * 40, notes)
            with self.assertRaises(FileNotFoundError):
                release_notes.render('v1.2.4', 'a' * 40, root)
            path.write_text(' ')
            with self.assertRaises(ValueError):
                release_notes.render('v1.2.3', 'a' * 40, root)
            for tag in ('v01.2.3', '../1.2.3', 'v1.2.3; echo unsafe'):
                with self.assertRaises(ValueError):
                    release_notes.render(tag, 'a' * 40, root)


if __name__ == '__main__':
    unittest.main()
