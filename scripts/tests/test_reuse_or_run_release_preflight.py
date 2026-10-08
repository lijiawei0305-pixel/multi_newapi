from __future__ import annotations

import os
import re
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
WRAPPER = ROOT / "scripts" / "reuse-or-run-release-preflight.sh"
VERIFIER = ROOT / "scripts" / "verify-preflight-witness.sh"
WORKFLOWS = {
    "release.yml": {
        "group": "group: formal-release-${{ github.ref_name || github.run_id }}",
        "needs": {
            "linux": "verify",
            "macos": "verify",
            "windows": "verify",
            "publish": "[linux, macos, windows]",
        },
    },
    "electron-build.yml": {
        "group": "group: formal-release-${{ github.ref_name || github.run_id }}",
        "needs": {
            "build": "verify",
            "release": "build",
        },
    },
    "docker-build.yml": {
        "group": "group: publish-docker-tag-${{ github.event.inputs.tag || github.ref_name || github.run_id }}",
        "needs": {
            "build_single_arch": "verify",
            "create_manifests": "[verify, build_single_arch]",
        },
    },
}
BUDGET = {
    "WITNESS_WAIT_SECONDS": 2700,
    "PREFLIGHT_BUDGET_SECONDS": 3600,
    "TOOLCHAIN_BUDGET_SECONDS": 900,
    "PREFIX_SLACK_SECONDS": 1200,
}


def run(cmd: list[str], cwd: Path, env: dict[str, str]) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    merged.update(env)
    return subprocess.run(
        cmd,
        cwd=cwd,
        env=merged,
        text=True,
        capture_output=True,
        check=False,
    )


class ReuseOrRunReleasePreflightTests(unittest.TestCase):
    def make_fixture(self, verifier_exit: int) -> tuple[tempfile.TemporaryDirectory[str], Path, Path]:
        tmp = tempfile.TemporaryDirectory()
        root = Path(tmp.name)
        scripts = root / "scripts"
        scripts.mkdir()
        wrapper = scripts / "reuse-or-run-release-preflight.sh"
        wrapper.write_text(WRAPPER.read_text())
        wrapper.chmod(wrapper.stat().st_mode | stat.S_IXUSR)
        log = root / "verifier.log"
        verifier = scripts / "verify-preflight-witness.sh"
        verifier.write_text(
            "\n".join(
                [
                    "#!/usr/bin/env bash",
                    f"printf '%s\\n' \"$*\" > {str(log)!r}",
                    "printf 'GH_TOKEN=%s\\n' \"${GH_TOKEN-}\" >> " + repr(str(log)),
                    "printf 'GITHUB_TOKEN=%s\\n' \"${GITHUB_TOKEN-}\" >> " + repr(str(log)),
                    "printf 'WAIT=%s\\n' \"${PREFLIGHT_WITNESS_TIMEOUT_SECONDS-}\" >> " + repr(str(log)),
                    "printf 'POLL=%s\\n' \"${PREFLIGHT_WITNESS_POLL_SECONDS-}\" >> " + repr(str(log)),
                    f"exit {verifier_exit}",
                    "",
                ]
            )
        )
        verifier.chmod(verifier.stat().st_mode | stat.S_IXUSR)
        preflight = scripts / "preflight.sh"
        preflight.write_text(
            "\n".join(
                [
                    "#!/usr/bin/env bash",
                    "printf '%s\\n' \"${PREFLIGHT_SCOPE-}\" > \"$PWD/preflight.scope\"",
                    "printf '%s\\n' \"${RUN_ORBIT_E2E-}\" > \"$PWD/preflight.orbit\"",
                    "exit \"${PREFLIGHT_EXIT:-0}\"",
                    "",
                ]
            )
        )
        preflight.chmod(preflight.stat().st_mode | stat.S_IXUSR)
        run(["git", "init", "-q"], root, {})
        run(["git", "add", "scripts"], root, {})
        committed = run(
            [
                "git",
                "-c",
                "user.name=preflight-test",
                "-c",
                "user.email=preflight-test@example.com",
                "commit",
                "-q",
                "-m",
                "fixture",
            ],
            root,
            {},
        )
        self.assertEqual(committed.returncode, 0, committed.stderr)
        return tmp, root, log

    def execute(
        self,
        *,
        verifier_exit: int = 0,
        event: str = "push",
        ref: str = "refs/tags/v1.2.3",
        repository: str = "example-owner/example-repo",
        gh_token: str = "same-token",
        github_token: str = "same-token",
        scope: str = "all",
        orbit: str = "1",
        preflight_exit: str = "0",
        use_real_verifier: bool = False,
    ) -> tuple[subprocess.CompletedProcess[str], Path, Path]:
        tmp, root, log = self.make_fixture(verifier_exit)
        self.addCleanup(tmp.cleanup)
        if use_real_verifier:
            real = root / "scripts" / "verify-preflight-witness.sh"
            real.write_text(VERIFIER.read_text())
            real.chmod(real.stat().st_mode | stat.S_IXUSR)
        env = {
            "GITHUB_EVENT_NAME": event,
            "GITHUB_REF": ref,
            "GITHUB_REPOSITORY": repository,
            "PREFLIGHT_SCOPE": scope,
            "RUN_ORBIT_E2E": orbit,
            "PREFLIGHT_EXIT": preflight_exit,
            "GH_TOKEN": gh_token,
            "GITHUB_TOKEN": github_token,
        }
        result = run(
            ["bash", "scripts/reuse-or-run-release-preflight.sh"],
            root,
            env,
        )
        return result, root, log

    def assert_preflight_ran(self, root: Path) -> None:
        self.assertEqual((root / "preflight.scope").read_text().strip(), "all")
        self.assertEqual((root / "preflight.orbit").read_text().strip(), "1")

    def assert_preflight_skipped(self, root: Path) -> None:
        self.assertFalse((root / "preflight.scope").exists())

    def test_accepted_witness_skips_duplicate_preflight(self) -> None:
        result, root, log = self.execute(verifier_exit=0)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("skipping duplicate preflight", result.stderr)
        self.assert_preflight_skipped(root)
        recorded = log.read_text()
        sha = run(["git", "rev-parse", "HEAD"], root, {}).stdout.strip()
        self.assertIn("--tag v1.2.3", recorded)
        self.assertIn(f"--sha {sha}", recorded)
        self.assertIn("--repository example-owner/example-repo", recorded)
        self.assertIn("--root ", recorded)
        self.assertIn("GH_TOKEN=same-token", recorded)
        self.assertIn("GITHUB_TOKEN=same-token", recorded)
        self.assertIn("WAIT=2700", recorded)
        self.assertIn("POLL=15", recorded)

    def test_missing_or_timeout_runs_full_preflight(self) -> None:
        result, root, _log = self.execute(verifier_exit=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("witness unavailable", result.stderr)
        self.assert_preflight_ran(root)

    def test_permission_unavailable_runs_full_preflight(self) -> None:
        result, root, _log = self.execute(verifier_exit=10, gh_token="no-actions", github_token="no-actions")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assert_preflight_ran(root)

    def test_argument_error_runs_full_preflight(self) -> None:
        result, root, _log = self.execute(verifier_exit=2)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("misconfigured", result.stderr)
        self.assert_preflight_ran(root)

    def test_token_mismatch_runs_full_preflight_without_the_verifier(self) -> None:
        result, root, log = self.execute(gh_token="one", github_token="two")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("GH_TOKEN and GITHUB_TOKEN disagree", result.stderr)
        self.assert_preflight_ran(root)
        self.assertFalse(log.exists())

    def test_rejected_witness_fails_closed(self) -> None:
        result, root, _log = self.execute(verifier_exit=20)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("rejected", result.stderr)
        self.assert_preflight_skipped(root)

    def test_unknown_exit_fails_closed(self) -> None:
        result, root, _log = self.execute(verifier_exit=99)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("unexpected exit 99", result.stderr)
        self.assert_preflight_skipped(root)

    def test_workflow_dispatch_runs_full_preflight_even_on_a_tag(self) -> None:
        result, root, log = self.execute(
            event="workflow_dispatch",
            ref="refs/tags/v1.2.3",
            verifier_exit=0,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("workflow_dispatch", result.stderr)
        self.assert_preflight_ran(root)
        self.assertFalse(log.exists())

    def test_preflight_failure_is_not_swallowed(self) -> None:
        result, root, _log = self.execute(verifier_exit=10, preflight_exit="7")
        self.assertEqual(result.returncode, 7, result.stderr)
        self.assert_preflight_ran(root)

    def test_narrow_scope_cannot_replace_full_preflight(self) -> None:
        result, _root, log = self.execute(event="workflow_dispatch", scope="backend")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("PREFLIGHT_SCOPE=all", result.stderr)
        self.assertFalse(log.exists())

    def test_real_verifier_rejects_nightly_tag(self) -> None:
        result, root, _log = self.execute(
            ref="refs/tags/nightly-20261008",
            use_real_verifier=True,
        )
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("rejected", result.stderr)
        self.assert_preflight_skipped(root)

    def test_budget_covers_wait_toolchain_and_full_preflight(self) -> None:
        text = WRAPPER.read_text()
        found = {}
        for name in BUDGET:
            match = re.search(rf"^readonly {name}=(\d+)$", text, re.M)
            self.assertIsNotNone(match, name)
            found[name] = int(match.group(1))
        self.assertEqual(found, BUDGET)
        self.assertEqual(sum(BUDGET.values()), 140 * 60)
        self.assertNotIn("gh workflow", text)
        self.assertNotIn("workflow run", text)
        self.assertIsNone(re.search(r"(?m)^\s*timeout\s+\d", text))

    def test_publish_workflows_keep_isolation_budget_and_verify_gate(self) -> None:
        witness = (ROOT / ".github" / "workflows" / "release-preflight.yml").read_text()
        witness_groups = [
            line.strip()
            for line in witness.splitlines()
            if line.strip().startswith("group:")
        ]
        self.assertEqual(witness_groups, ["group: tag-preflight-${{ github.ref }}"])
        for name, expected in WORKFLOWS.items():
            text = (ROOT / ".github" / "workflows" / name).read_text()
            self.assertIn(expected["group"], text)
            self.assertNotIn("group: tag-preflight-", text)
            self.assertEqual(text.count("timeout-minutes: 140"), 1)
            body = text.split("\njobs:\n", 1)[1]
            matches = list(re.finditer(r"(?m)^  ([A-Za-z0-9_]+):\n", body))
            blocks = {}
            for index, match in enumerate(matches):
                end = matches[index + 1].start() if index + 1 < len(matches) else len(body)
                blocks[match.group(1)] = body[match.start():end]
            self.assertIn("verify", blocks)
            verify = blocks["verify"]
            self.assertNotIn("\n    if:", verify)
            self.assertNotIn("environment:", verify)
            self.assertNotIn("id-token", verify)
            self.assertIn("bash scripts/reuse-or-run-release-preflight.sh", verify)
            self.assertIn("needs: verify", text)
            for job, needs in expected["needs"].items():
                self.assertIn(job, blocks, name)
                self.assertIn(f"needs: {needs}", blocks[job])
            self.assertNotIn("if: always()", text)
            self.assertNotIn("success() || failure()", text)


if __name__ == "__main__":
    unittest.main()
