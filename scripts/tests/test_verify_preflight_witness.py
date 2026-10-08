from __future__ import annotations

import hashlib
import io
import json
import os
import re
import stat
import subprocess
import tempfile
import time
import unittest
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
VERIFIER = ROOT / "scripts" / "verify-preflight-witness.sh"
WRITER = ROOT / "scripts" / "write-preflight-witness.sh"
WORKFLOW = ROOT / ".github" / "workflows" / "release-preflight.yml"
WORKFLOW_PATH = ".github/workflows/release-preflight.yml"
REPO = "example-owner/example-repo"
TAG = "v1.2.3"
SHA = "0123456789abcdef0123456789abcdef01234567"
OTHER_SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
TOKEN = "fixture-token-9f3a"
EVIL_URL = "https://evil.example/steal"

FAKE_GH = r'''#!/usr/bin/env python3
import json
import os
import sys
from pathlib import Path

scenario = json.loads(Path(os.environ["GH_SCENARIO"]).read_text())
log_path = Path(os.environ["GH_REQUEST_LOG"])
url = next(arg for arg in sys.argv[1:] if arg.startswith("/") or arg.startswith("http"))
with log_path.open("a", encoding="utf-8") as handle:
    handle.write(url + "\n")

def fail(status, payload):
    message = payload.get("message", "error") if isinstance(payload, dict) else "error"
    sys.stderr.write(f"gh: {message} (HTTP {status})\n")
    sys.stdout.write(json.dumps(payload if isinstance(payload, dict) else {"message": message}))
    raise SystemExit(1)

if url.endswith("/actions/workflows/release-preflight.yml"):
    status = int(scenario.get("probe_status", 200))
    body = scenario.get("probe_body") or {}
    if status != 200:
        fail(status, body or {"message": "Resource not accessible by integration"})
    sys.stdout.write(json.dumps(body))
    raise SystemExit(0)

if "/actions/workflows/release-preflight.yml/runs?" in url:
    index_path = Path(os.environ["GH_SCENARIO"] + ".runs-index")
    index = int(index_path.read_text()) if index_path.exists() else 0
    results = scenario.get("runs_results")
    if results is None:
        pages = scenario.get("runs_pages") or []
        body = pages[min(index, len(pages) - 1)] if pages else {"total_count": 0, "workflow_runs": []}
        result = {"status": 200, "body": body}
    else:
        result = results[min(index, len(results) - 1)]
    index_path.write_text(str(index + 1))
    status = int(result.get("status", 200))
    if status != 200:
        fail(status, result.get("body") or {"message": "boom"})
    sys.stdout.write(json.dumps(result.get("body") or {}))
    raise SystemExit(0)

if url.endswith("/artifacts") and "/actions/runs/" in url:
    status = int(scenario.get("artifacts_status", 200))
    body = scenario.get("artifacts_body") or {"total_count": 0, "artifacts": []}
    if status != 200:
        fail(status, body if isinstance(body, dict) else {"message": "artifact list failed"})
    sys.stdout.write(json.dumps(body))
    raise SystemExit(0)

if url.endswith("/zip") and "/actions/artifacts/" in url:
    status = int(scenario.get("zip_status", 200))
    if status != 200:
        fail(status, {"message": "artifact download failed"})
    blob = Path(os.environ["GH_ZIP"]).read_bytes()
    sys.stdout.buffer.write(blob)
    raise SystemExit(0)

fail(404, {"message": "Not Found"})
'''


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def jwt(payload: dict) -> str:
    def encode(value: dict) -> str:
        raw = json.dumps(value, separators=(",", ":")).encode()
        return __import__("base64").urlsafe_b64encode(raw).rstrip(b"=").decode()

    return encode({"alg": "none", "typ": "JWT"}) + "." + encode(payload) + ".sig"


def zip_bytes(member: str, payload: bytes, extra: list[tuple[str, bytes]] | None = None) -> bytes:
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w") as archive:
        archive.writestr(member, payload)
        for name, content in extra or []:
            archive.writestr(name, content)
    return buffer.getvalue()


class WitnessFixture:
    def __init__(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.case_number = 0
        self.preflight = b"preflight-bytes-v1\n"
        self.workflow = b"workflow-bytes-v1\n"
        workflow_path = self.root / WORKFLOW_PATH
        workflow_path.parent.mkdir(parents=True)
        (self.root / "scripts").mkdir()
        (self.root / "scripts" / "preflight.sh").write_bytes(self.preflight)
        workflow_path.write_bytes(self.workflow)
        self.run_id = 101
        self.run_attempt = 1
        self.artifact_id = 55

    def close(self) -> None:
        self.directory.cleanup()

    def witness(self, **overrides: object) -> dict:
        document = {
            "schema": "new-api.release-preflight-witness.v1",
            "repository": REPO,
            "head_repository": REPO,
            "workflow_path": WORKFLOW_PATH,
            "workflow_sha256": sha256(self.workflow),
            "event_name": "push",
            "ref": f"refs/tags/{TAG}",
            "sha": SHA,
            "scope": "all",
            "run_orbit_e2e": True,
            "preflight_sha256": sha256(self.preflight),
            "run_id": self.run_id,
            "run_attempt": self.run_attempt,
        }
        document.update(overrides)
        return document

    def run(
        self,
        *,
        event: str = "push",
        status: str = "completed",
        conclusion: str | None = "success",
        head_sha: str = SHA,
        head_branch: str = TAG,
        path: str = WORKFLOW_PATH,
        repository: str = REPO,
        head_repository: str = REPO,
        run_id: int | None = None,
        run_attempt: int | None = None,
    ) -> dict:
        return {
            "id": self.run_id if run_id is None else run_id,
            "run_attempt": self.run_attempt if run_attempt is None else run_attempt,
            "event": event,
            "status": status,
            "conclusion": conclusion,
            "head_sha": head_sha,
            "head_branch": head_branch,
            "path": path,
            "repository": {"full_name": repository},
            "head_repository": {"full_name": head_repository},
        }


class VerifyPreflightWitnessTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fixture = WitnessFixture()

    def tearDown(self) -> None:
        self.fixture.close()

    def execute(
        self,
        *,
        runs: list[dict] | None = None,
        runs_pages: list[dict] | None = None,
        runs_results: list[dict] | None = None,
        witness: dict | None = None,
        tag: str = TAG,
        sha: str = SHA,
        repository: str = REPO,
        token: str | None = TOKEN,
        github_token: str | None = TOKEN,
        timeout: str = "0",
        poll: str = "0",
        probe_status: int = 200,
        probe_body: dict | None = None,
        artifacts_body: dict | None = None,
        artifacts_status: int = 200,
        zip_status: int = 200,
        zip_payload: bytes | None = None,
        extra_zip: list[tuple[str, bytes]] | None = None,
        limit: float = 8,
    ) -> tuple[subprocess.CompletedProcess[str], str]:
        self.fixture.case_number += 1
        work = self.fixture.root / f"case-{self.fixture.case_number}"
        work.mkdir()
        document = self.fixture.witness() if witness is None else witness
        payload = json.dumps(document).encode()
        (work / "witness.zip").write_bytes(
            zip_payload
            if zip_payload is not None
            else zip_bytes("preflight-witness.json", payload, extra_zip)
        )
        if runs_pages is None:
            body = {
                "total_count": 0 if runs is None else len(runs),
                "workflow_runs": [] if runs is None else runs,
            }
            runs_pages = [body]
        scenario = {
            "probe_status": probe_status,
            "probe_body": probe_body
            or {"path": WORKFLOW_PATH, "state": "active", "name": "Release preflight witness"},
            "runs_pages": runs_pages,
            "artifacts_status": artifacts_status,
            "artifacts_body": artifacts_body
            if artifacts_body is not None
            else {
                "total_count": 1,
                "artifacts": [
                    {
                        "id": self.fixture.artifact_id,
                        "name": "preflight-witness",
                        "expired": False,
                        "size_in_bytes": 120,
                        "archive_download_url": EVIL_URL,
                    }
                ],
            },
            "zip_status": zip_status,
        }
        if runs_results is not None:
            scenario["runs_results"] = runs_results
        (work / "scenario.json").write_text(json.dumps(scenario))
        bin_dir = work / "bin"
        bin_dir.mkdir()
        fake = bin_dir / "gh"
        fake.write_text(FAKE_GH)
        fake.chmod(fake.stat().st_mode | stat.S_IEXEC)
        env = os.environ.copy()
        for name in (
            "GH_TOKEN",
            "GITHUB_TOKEN",
            "PREFLIGHT_WITNESS_TIMEOUT_SECONDS",
            "PREFLIGHT_WITNESS_POLL_SECONDS",
        ):
            env.pop(name, None)
        if token is not None:
            env["GH_TOKEN"] = token
        if github_token is not None:
            env["GITHUB_TOKEN"] = github_token
        env["PATH"] = str(bin_dir) + os.pathsep + env.get("PATH", "")
        env["GH_SCENARIO"] = str(work / "scenario.json")
        env["GH_ZIP"] = str(work / "witness.zip")
        env["GH_REQUEST_LOG"] = str(work / "requests.log")
        env["PREFLIGHT_WITNESS_TIMEOUT_SECONDS"] = timeout
        env["PREFLIGHT_WITNESS_POLL_SECONDS"] = poll
        started = time.monotonic()
        result = subprocess.run(
            [
                "bash",
                str(VERIFIER),
                "--repository",
                repository,
                "--tag",
                tag,
                "--sha",
                sha,
                "--root",
                str(self.fixture.root),
            ],
            cwd=self.fixture.root,
            env=env,
            capture_output=True,
            text=True,
            timeout=limit,
            check=False,
        )
        self.assertLess(time.monotonic() - started, limit)
        log = (work / "requests.log").read_text() if (work / "requests.log").exists() else ""
        self.assertNotIn("evil.example", log)
        self.assertNotIn("evil.example", result.stdout)
        self.assertNotIn("evil.example", result.stderr)
        if token and token.count(".") != 2:
            self.assertNotIn(token, result.stdout)
            self.assertNotIn(token, result.stderr)
            self.assertNotIn(token, log)
        return result, log

    def assert_status(
        self,
        result: subprocess.CompletedProcess[str],
        code: int,
        reason: str,
    ) -> None:
        classes = {0: "accepted", 2: "usage", 10: "unavailable", 20: "rejected"}
        self.assertEqual(
            result.returncode,
            code,
            msg=f"stdout={result.stdout!r}\nstderr={result.stderr!r}",
        )
        lines = result.stdout.splitlines()
        self.assertEqual(len(lines), 1, msg=result.stdout)
        self.assertIn(f"preflight-witness: {classes[code]} ", lines[0])
        self.assertIn(f"reason={reason}", lines[0])

    def test_accepts_matching_push_witness(self) -> None:
        result, log = self.execute(runs=[self.fixture.run()])
        self.assert_status(result, 0, "accepted")
        self.assertIn("run_id=101", result.stdout)
        self.assertIn("run_attempt=1", result.stdout)
        self.assertEqual(
            log.splitlines(),
            [
                f"/repos/{REPO}/actions/workflows/release-preflight.yml",
                f"/repos/{REPO}/actions/workflows/release-preflight.yml/runs?head_sha={SHA}&per_page=100",
                f"/repos/{REPO}/actions/runs/101/artifacts",
                f"/repos/{REPO}/actions/artifacts/55/zip",
            ],
        )

    def test_accepts_a_large_github_run_id(self) -> None:
        self.fixture.run_id = 37746278729
        self.fixture.artifact_id = 88000000001
        result, log = self.execute(runs=[self.fixture.run()])
        self.assert_status(result, 0, "accepted")
        self.assertIn("run_id=37746278729", result.stdout)
        self.assertIn("/actions/runs/37746278729/artifacts", log)
        self.assertIn("/actions/artifacts/88000000001/zip", log)

    def test_actions_write_token_still_proves_read_access(self) -> None:
        token = jwt({"permissions": {"actions": "write", "contents": "read"}})
        result, _log = self.execute(
            runs=[self.fixture.run()],
            token=token,
            github_token=token,
        )
        self.assert_status(result, 0, "accepted")

    def test_waits_for_queued_run_then_accepts(self) -> None:
        queued = self.fixture.run(status="queued", conclusion=None)
        ready = self.fixture.run()
        result, log = self.execute(
            runs_pages=[
                {"total_count": 1, "workflow_runs": [queued]},
                {"total_count": 1, "workflow_runs": [ready]},
            ],
            timeout="30",
            poll="0",
        )
        self.assert_status(result, 0, "accepted")
        self.assertEqual(sum("/runs?" in line for line in log.splitlines()), 2)

    def test_newer_in_progress_run_blocks_an_older_success(self) -> None:
        result, log = self.execute(
            runs=[
                self.fixture.run(run_id=101),
                self.fixture.run(run_id=202, status="in_progress", conclusion=None),
            ],
            timeout="30",
        )
        self.assert_status(result, 10, "timeout")
        self.assertNotIn("/artifacts", log)

    def test_newer_failure_blocks_an_older_success(self) -> None:
        result, log = self.execute(
            runs=[
                self.fixture.run(run_id=101),
                self.fixture.run(run_id=202, conclusion="failure"),
            ],
            timeout="30",
            poll="10",
        )
        self.assert_status(result, 20, "conclusion")
        self.assertIn("conclusion=failure", result.stderr)
        self.assertNotIn("/artifacts", log)

    def test_newer_success_is_used_after_an_older_failure(self) -> None:
        self.fixture.run_id = 202
        result, log = self.execute(
            runs=[
                self.fixture.run(run_id=101, conclusion="failure"),
                self.fixture.run(run_id=202),
            ]
        )
        self.assert_status(result, 0, "accepted")
        self.assertIn("run_id=202", result.stdout)
        self.assertIn("/actions/runs/202/artifacts", log)

    def test_retries_a_transient_run_list_error(self) -> None:
        result, log = self.execute(
            runs_results=[
                {"status": 500, "body": {"message": "boom"}},
                {
                    "status": 200,
                    "body": {"total_count": 1, "workflow_runs": [self.fixture.run()]},
                },
            ],
            timeout="30",
            poll="0",
        )
        self.assert_status(result, 0, "accepted")
        self.assertGreaterEqual(sum("/runs?" in line for line in log.splitlines()), 2)

    def test_rejects_fork(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run(head_repository="fork-owner/example-repo")]
        )
        self.assert_status(result, 20, "fork")
        self.assertNotIn("/artifacts", log)

    def test_other_tag_on_the_same_sha_waits_instead_of_wrong_tag(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run(head_branch="v9.9.9", run_id=404)]
        )
        self.assert_status(result, 10, "not-found")
        self.assertNotIn("wrong-tag", result.stdout)
        self.assertNotIn("/artifacts", log)

    def test_target_tag_success_after_another_tag_is_accepted(self) -> None:
        other = self.fixture.run(head_branch="v9.9.9", run_id=404)
        target = self.fixture.run(run_id=101)
        result, log = self.execute(
            runs_pages=[
                {"total_count": 1, "workflow_runs": [other]},
                {"total_count": 2, "workflow_runs": [other, target]},
            ],
            timeout="30",
            poll="0",
        )
        self.assert_status(result, 0, "accepted")
        self.assertIn("run_id=101", result.stdout)
        self.assertNotIn("/actions/runs/404/", log)

    def test_target_tag_failure_after_another_tag_is_rejected(self) -> None:
        other = self.fixture.run(head_branch="v9.9.9", run_id=404)
        failed = self.fixture.run(run_id=101, conclusion="failure")
        result, log = self.execute(
            runs_pages=[
                {"total_count": 1, "workflow_runs": [other]},
                {"total_count": 2, "workflow_runs": [other, failed]},
            ],
            timeout="30",
            poll="0",
        )
        self.assert_status(result, 20, "conclusion")
        self.assertIn("conclusion=failure", result.stderr)
        self.assertNotIn("/artifacts", log)

    def test_other_tag_success_does_not_hide_target_failure(self) -> None:
        result, log = self.execute(
            runs=[
                self.fixture.run(head_branch="v9.9.9", run_id=404),
                self.fixture.run(run_id=101, conclusion="failure"),
            ]
        )
        self.assert_status(result, 20, "conclusion")
        self.assertNotIn("/actions/runs/404/", log)

    def test_other_tag_success_is_not_the_witness(self) -> None:
        result, log = self.execute(
            runs=[
                self.fixture.run(head_branch="v9.9.9", run_id=404),
                self.fixture.run(run_id=101),
            ]
        )
        self.assert_status(result, 0, "accepted")
        self.assertIn("run_id=101", result.stdout)
        self.assertNotIn("/actions/runs/404/", log)

    def test_target_in_progress_is_not_replaced_by_other_tag_success(self) -> None:
        result, log = self.execute(
            runs=[
                self.fixture.run(head_branch="v9.9.9", run_id=404),
                self.fixture.run(run_id=101, status="in_progress", conclusion=None),
            ]
        )
        self.assert_status(result, 10, "timeout")
        self.assertNotIn("/artifacts", log)

    def test_rejects_wrong_sha(self) -> None:
        result, _log = self.execute(runs=[self.fixture.run(head_sha=OTHER_SHA)])
        self.assert_status(result, 20, "wrong-sha")

    def test_rejects_workflow_dispatch(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run(event="workflow_dispatch", conclusion="success")]
        )
        self.assert_status(result, 20, "wrong-event")
        self.assertNotIn("/artifacts", log)

    def test_rejects_ci_workflow_run(self) -> None:
        result, _log = self.execute(
            runs=[self.fixture.run(path=".github/workflows/ci.yml")]
        )
        self.assert_status(result, 20, "wrong-workflow")

    def test_rejects_terminal_failures_without_waiting(self) -> None:
        for conclusion in ("failure", "cancelled", "timed_out", "startup_failure"):
            with self.subTest(conclusion=conclusion):
                result, log = self.execute(
                    runs=[self.fixture.run(conclusion=conclusion)],
                    timeout="30",
                    poll="10",
                )
                self.assert_status(result, 20, "conclusion")
                self.assertIn(f"conclusion={conclusion}", result.stderr)
                self.assertEqual(sum("/runs?" in line for line in log.splitlines()), 1)

    def test_rejects_preflight_hash_mismatch(self) -> None:
        witness = self.fixture.witness(preflight_sha256="ab" * 32)
        result, _log = self.execute(runs=[self.fixture.run()], witness=witness)
        self.assert_status(result, 20, "preflight-hash")

    def test_rejects_workflow_hash_mismatch(self) -> None:
        witness = self.fixture.witness(workflow_sha256="cd" * 32)
        result, _log = self.execute(runs=[self.fixture.run()], witness=witness)
        self.assert_status(result, 20, "workflow-hash")

    def test_rejects_binding_mismatches(self) -> None:
        mutations = (
            {"event_name": "workflow_dispatch"},
            {"scope": "backend"},
            {"run_orbit_e2e": False},
            {"run_id": 999},
            {"run_attempt": 2},
            {"ref": "refs/tags/v9.9.9"},
            {"sha": OTHER_SHA},
            {"repository": "other/repo"},
            {"head_repository": "other/repo"},
            {"schema": "other"},
            {"extra": True},
        )
        for mutation in mutations:
            with self.subTest(mutation=mutation):
                result, _log = self.execute(
                    runs=[self.fixture.run()],
                    witness=self.fixture.witness(**mutation),
                )
                self.assert_status(result, 20, "binding")

    def test_rejects_ambiguous_artifacts(self) -> None:
        artifacts = {
            "total_count": 2,
            "artifacts": [
                {
                    "id": 55,
                    "name": "preflight-witness",
                    "expired": False,
                    "size_in_bytes": 10,
                    "archive_download_url": EVIL_URL,
                },
                {
                    "id": 56,
                    "name": "preflight-witness",
                    "expired": False,
                    "size_in_bytes": 10,
                    "archive_download_url": EVIL_URL,
                },
            ],
        }
        result, log = self.execute(runs=[self.fixture.run()], artifacts_body=artifacts)
        self.assert_status(result, 20, "ambiguous-artifact")
        self.assertNotIn("/zip", log)

    def test_times_out_while_run_stays_queued(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run(status="queued", conclusion=None)]
        )
        self.assert_status(result, 10, "timeout")
        self.assertEqual(sum("/runs?" in line for line in log.splitlines()), 1)

    def test_not_found_when_no_run_is_visible(self) -> None:
        result, _log = self.execute(runs=[])
        self.assert_status(result, 10, "not-found")

    def test_truncated_run_page_is_unavailable(self) -> None:
        result, log = self.execute(
            runs_pages=[
                {"total_count": 2, "workflow_runs": [self.fixture.run()]}
            ]
        )
        self.assert_status(result, 10, "response-truncated")
        self.assertNotIn("/artifacts", log)

    def test_artifact_download_failure_is_unavailable(self) -> None:
        result, _log = self.execute(runs=[self.fixture.run()], zip_status=500)
        self.assert_status(result, 10, "artifact-unreadable")

    def test_missing_artifact_is_unavailable(self) -> None:
        result, _log = self.execute(
            runs=[self.fixture.run()],
            artifacts_body={"total_count": 0, "artifacts": []},
        )
        self.assert_status(result, 10, "artifact-missing")

    def test_expired_artifact_is_unavailable(self) -> None:
        result, _log = self.execute(
            runs=[self.fixture.run()],
            artifacts_body={
                "total_count": 1,
                "artifacts": [
                    {
                        "id": 55,
                        "name": "preflight-witness",
                        "expired": True,
                        "size_in_bytes": 10,
                        "archive_download_url": EVIL_URL,
                    }
                ],
            },
        )
        self.assert_status(result, 10, "artifact-missing")

    def test_corrupt_zip_is_unavailable(self) -> None:
        result, _log = self.execute(
            runs=[self.fixture.run()],
            zip_payload=b"not-a-zip",
        )
        self.assert_status(result, 10, "artifact-unreadable")

    def test_hostile_zip_layout_is_rejected(self) -> None:
        result, _log = self.execute(
            runs=[self.fixture.run()],
            extra_zip=[("../preflight-witness.json", b"{}")],
        )
        self.assert_status(result, 20, "binding")

    def test_missing_actions_read_is_unavailable(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run()],
            probe_status=403,
            probe_body={"message": "Resource not accessible by integration"},
        )
        self.assert_status(result, 10, "actions-read-required")
        self.assertEqual(sum("/runs?" in line for line in log.splitlines()), 0)

    def test_jwt_without_actions_permission_is_unavailable(self) -> None:
        token = jwt({"permissions": {"contents": "read"}})
        result, log = self.execute(
            runs=[self.fixture.run()],
            token=token,
            github_token=token,
        )
        self.assert_status(result, 10, "actions-read-required")
        self.assertEqual(log, "")

    def test_jwt_claim_does_not_override_api_denial(self) -> None:
        token = jwt({"permissions": {"actions": "read"}})
        result, _log = self.execute(
            runs=[self.fixture.run()],
            token=token,
            github_token=token,
            probe_status=403,
            probe_body={"message": "Resource not accessible by integration"},
        )
        self.assert_status(result, 10, "actions-read-required")

    def test_disabled_workflow_is_unavailable(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run()],
            probe_body={"path": WORKFLOW_PATH, "state": "disabled_manually"},
        )
        self.assert_status(result, 10, "workflow-disabled")
        self.assertNotIn("/runs?", log)

    def test_probe_for_another_workflow_is_rejected(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run()],
            probe_body={"path": ".github/workflows/ci.yml", "state": "active"},
        )
        self.assert_status(result, 20, "wrong-workflow")
        self.assertNotIn("/runs?", log)

    def test_missing_workflow_registration_is_unavailable(self) -> None:
        result, _log = self.execute(
            runs=[self.fixture.run()],
            probe_status=404,
            probe_body={"message": "Not Found"},
        )
        self.assert_status(result, 10, "workflow-missing")

    def test_nightly_tag_is_rejected_without_api_calls(self) -> None:
        result, log = self.execute(tag="nightly-20261008", runs=[self.fixture.run()])
        self.assert_status(result, 20, "wrong-tag")
        self.assertEqual(log, "")

    def test_usage_without_token(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run()],
            token=None,
            github_token=None,
        )
        self.assert_status(result, 2, "usage")
        self.assertEqual(log, "")

    def test_usage_when_tokens_disagree(self) -> None:
        result, _log = self.execute(
            runs=[self.fixture.run()],
            token=TOKEN,
            github_token="other-token",
        )
        self.assert_status(result, 2, "usage")

    def test_poll_zero_does_not_block_on_a_long_timeout(self) -> None:
        result, log = self.execute(
            runs=[self.fixture.run(status="in_progress", conclusion=None)],
            timeout="99999",
            poll="0",
        )
        self.assert_status(result, 10, "timeout")
        self.assertLessEqual(sum("/runs?" in line for line in log.splitlines()), 8)

    def test_default_timeout_is_shorter_than_publish_verify(self) -> None:
        result = subprocess.run(
            ["bash", str(VERIFIER), "--print-config"],
            cwd=ROOT,
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, msg=result.stderr)
        values = dict(
            line.split("=", 1) for line in result.stdout.splitlines() if "=" in line
        )
        self.assertEqual(values["timeout_seconds"], "2700")
        self.assertEqual(values["poll_seconds"], "15")
        self.assertEqual(values["workflow_path"], WORKFLOW_PATH)
        self.assertEqual(values["artifact_name"], "preflight-witness")
        self.assertLess(int(values["timeout_seconds"]), 60 * 60)
        # The 45-minute wait must finish with a full 60-minute preflight,
        # toolchain setup, and slack still inside the verify job.
        self.assertGreaterEqual(140 * 60, int(values["timeout_seconds"]) + 3600 + 900 + 1200)
        for name in ("release.yml", "electron-build.yml", "docker-build.yml"):
            text = (ROOT / ".github" / "workflows" / name).read_text()
            self.assertIn("timeout-minutes: 140", text)
            self.assertNotIn("timeout-minutes: 60", text)


class WritePreflightWitnessTests(unittest.TestCase):
    def writer_env(self, root: Path, out: Path, **overrides: str) -> dict[str, str]:
        env = os.environ.copy()
        env.update(
            {
                "PREFLIGHT_WITNESS_ROOT": str(root),
                "WITNESS_OUT": str(out),
                "GITHUB_EVENT_NAME": "push",
                "GITHUB_REF": f"refs/tags/{TAG}",
                "GITHUB_SHA": SHA,
                "GITHUB_REPOSITORY": REPO,
                "WITNESS_EVENT_REPOSITORY": REPO,
                "GITHUB_RUN_ID": "101",
                "GITHUB_RUN_ATTEMPT": "1",
                "GITHUB_WORKFLOW_REF": f"{REPO}/{WORKFLOW_PATH}@refs/tags/{TAG}",
                "PREFLIGHT_SCOPE": "all",
                "RUN_ORBIT_E2E": "1",
                "GITHUB_ACTIONS": "false",
            }
        )
        env.update(overrides)
        return env

    def run_writer(self, env: dict[str, str]) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(WRITER)],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_writer_output_is_accepted_for_a_clean_push(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            workflow = repo / WORKFLOW_PATH
            workflow.parent.mkdir(parents=True)
            (repo / "scripts").mkdir()
            (repo / "scripts" / "preflight.sh").write_text("committed-preflight\n")
            workflow.write_text("committed-workflow\n")
            subprocess.run(["git", "-C", str(repo), "init", "-q", "-b", "main"], check=True)
            subprocess.run(
                ["git", "-C", str(repo), "config", "user.email", "witness-test@example.invalid"],
                check=True,
            )
            subprocess.run(
                ["git", "-C", str(repo), "config", "user.name", "witness-test"],
                check=True,
            )
            subprocess.run(["git", "-C", str(repo), "add", "."], check=True)
            subprocess.run(["git", "-C", str(repo), "commit", "-qm", "witness"], check=True)
            head = subprocess.check_output(
                ["git", "-C", str(repo), "rev-parse", "HEAD"], text=True
            ).strip()
            out = repo / "witness.json"
            result = self.run_writer(
                self.writer_env(repo, out, GITHUB_SHA=head, GITHUB_ACTIONS="true")
            )
            self.assertEqual(result.returncode, 0, msg=result.stderr)
            document = json.loads(out.read_text())
            self.assertEqual(document["event_name"], "push")
            self.assertEqual(document["sha"], head)
            self.assertEqual(document["run_orbit_e2e"], True)
            self.assertEqual(
                document["preflight_sha256"],
                sha256((repo / "scripts" / "preflight.sh").read_bytes()),
            )

            work = repo / "gh-case"
            work.mkdir()
            payload = out.read_bytes()
            (work / "witness.zip").write_bytes(zip_bytes("preflight-witness.json", payload))
            scenario = {
                "probe_status": 200,
                "probe_body": {"path": WORKFLOW_PATH, "state": "active"},
                "runs_pages": [
                    {
                        "total_count": 1,
                        "workflow_runs": [
                            {
                                "id": 101,
                                "run_attempt": 1,
                                "event": "push",
                                "status": "completed",
                                "conclusion": "success",
                                "head_sha": head,
                                "head_branch": TAG,
                                "path": WORKFLOW_PATH,
                                "repository": {"full_name": REPO},
                                "head_repository": {"full_name": REPO},
                            }
                        ],
                    }
                ],
                "artifacts_body": {
                    "total_count": 1,
                    "artifacts": [
                        {
                            "id": 55,
                            "name": "preflight-witness",
                            "expired": False,
                            "size_in_bytes": len(payload),
                            "archive_download_url": EVIL_URL,
                        }
                    ],
                },
                "zip_status": 200,
            }
            (work / "scenario.json").write_text(json.dumps(scenario))
            bin_dir = work / "bin"
            bin_dir.mkdir()
            fake = bin_dir / "gh"
            fake.write_text(FAKE_GH)
            fake.chmod(fake.stat().st_mode | stat.S_IEXEC)
            env = os.environ.copy()
            env["PATH"] = str(bin_dir) + os.pathsep + env.get("PATH", "")
            env["GH_TOKEN"] = TOKEN
            env["GITHUB_TOKEN"] = TOKEN
            env["GH_SCENARIO"] = str(work / "scenario.json")
            env["GH_ZIP"] = str(work / "witness.zip")
            env["GH_REQUEST_LOG"] = str(work / "requests.log")
            env["PREFLIGHT_WITNESS_TIMEOUT_SECONDS"] = "0"
            env["PREFLIGHT_WITNESS_POLL_SECONDS"] = "0"
            verified = subprocess.run(
                [
                    "bash",
                    str(VERIFIER),
                    "--repository",
                    REPO,
                    "--tag",
                    TAG,
                    "--sha",
                    head,
                    "--root",
                    str(repo),
                ],
                cwd=repo,
                env=env,
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(verified.returncode, 0, msg=f"{verified.stdout}\n{verified.stderr}")
            self.assertIn("reason=accepted", verified.stdout)
            self.assertNotIn("evil.example", (work / "requests.log").read_text())

    def test_writer_refuses_dirty_actions_checkout(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            workflow = repo / WORKFLOW_PATH
            workflow.parent.mkdir(parents=True)
            (repo / "scripts").mkdir()
            preflight = repo / "scripts" / "preflight.sh"
            preflight.write_text("clean\n")
            workflow.write_text("workflow\n")
            subprocess.run(["git", "-C", str(repo), "init", "-q", "-b", "main"], check=True)
            subprocess.run(
                ["git", "-C", str(repo), "config", "user.email", "witness-test@example.invalid"],
                check=True,
            )
            subprocess.run(
                ["git", "-C", str(repo), "config", "user.name", "witness-test"],
                check=True,
            )
            subprocess.run(["git", "-C", str(repo), "add", "."], check=True)
            subprocess.run(["git", "-C", str(repo), "commit", "-qm", "witness"], check=True)
            head = subprocess.check_output(
                ["git", "-C", str(repo), "rev-parse", "HEAD"], text=True
            ).strip()
            preflight.write_text("dirty\n")
            result = self.run_writer(
                self.writer_env(repo, repo / "witness.json", GITHUB_SHA=head, GITHUB_ACTIONS="true")
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("dirty", result.stderr)

    def test_writer_allows_preflight_build_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            workflow = repo / WORKFLOW_PATH
            workflow.parent.mkdir(parents=True)
            (repo / "scripts").mkdir()
            (repo / "scripts" / "preflight.sh").write_text("committed-preflight\n")
            workflow.write_text("committed-workflow\n")
            (repo / ".gitignore").write_text("web/default/dist\nnew-api\n")
            subprocess.run(["git", "-C", str(repo), "init", "-q", "-b", "main"], check=True)
            subprocess.run(
                ["git", "-C", str(repo), "config", "user.email", "witness-test@example.invalid"],
                check=True,
            )
            subprocess.run(
                ["git", "-C", str(repo), "config", "user.name", "witness-test"],
                check=True,
            )
            subprocess.run(["git", "-C", str(repo), "add", "."], check=True)
            subprocess.run(["git", "-C", str(repo), "commit", "-qm", "witness"], check=True)
            head = subprocess.check_output(
                ["git", "-C", str(repo), "rev-parse", "HEAD"], text=True
            ).strip()
            screenshot = repo / "bulb-orbit" / "v2" / "test-results" / "landing-full.png"
            screenshot.parent.mkdir(parents=True)
            screenshot.write_bytes(b"png")
            embed = repo / "web" / "default" / "dist" / "index.html"
            embed.parent.mkdir(parents=True)
            embed.write_text("<!doctype html>\n")
            (repo / "new-api").write_bytes(b"binary")
            out = repo / "witness.json"
            result = self.run_writer(
                self.writer_env(repo, out, GITHUB_SHA=head, GITHUB_ACTIONS="true")
            )
            self.assertEqual(result.returncode, 0, msg=result.stderr)
            self.assertTrue(out.is_file())

    def test_writer_refuses_unsafe_sources(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "scripts" / "preflight.sh").write_text("preflight\n")
            workflow = root / WORKFLOW_PATH
            workflow.parent.mkdir(parents=True)
            workflow.write_text("workflow\n")
            out = root / "witness.json"
            cases = (
                {"RUN_ORBIT_E2E": "0"},
                {"PREFLIGHT_SCOPE": "backend"},
                {"GITHUB_EVENT_NAME": "pull_request"},
                {"GITHUB_REF": "refs/heads/main"},
                {"GITHUB_REF": "refs/tags/nightly-1"},
                {"WITNESS_EVENT_REPOSITORY": "other/repo"},
                {"GITHUB_SHA": "ABC"},
            )
            for overrides in cases:
                with self.subTest(overrides=overrides):
                    result = self.run_writer(self.writer_env(root, out, **overrides))
                    self.assertNotEqual(result.returncode, 0, msg=result.stderr)
                    self.assertFalse(out.exists())

    def test_writer_records_dispatch_without_making_it_acceptable(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "scripts" / "preflight.sh").write_text("preflight\n")
            workflow = root / WORKFLOW_PATH
            workflow.parent.mkdir(parents=True)
            workflow.write_text("workflow\n")
            out = root / "witness.json"
            result = self.run_writer(
                self.writer_env(
                    root,
                    out,
                    GITHUB_EVENT_NAME="workflow_dispatch",
                    GITHUB_REF="refs/heads/main",
                    GITHUB_WORKFLOW_REF=f"{REPO}/{WORKFLOW_PATH}@refs/heads/main",
                )
            )
            self.assertEqual(result.returncode, 0, msg=result.stderr)
            document = json.loads(out.read_text())
            self.assertEqual(document["event_name"], "workflow_dispatch")
            self.assertEqual(document["ref"], "refs/heads/main")


class ReleaseWorkflowGuardTests(unittest.TestCase):
    def test_witness_workflow_is_isolated(self) -> None:
        text = WORKFLOW.read_text()
        group_lines = [
            line.strip() for line in text.splitlines() if line.strip().startswith("group:")
        ]
        self.assertEqual(group_lines, ["group: tag-preflight-${{ github.ref }}"])
        self.assertNotIn("formal-release", "\n".join(group_lines))
        self.assertIn("cancel-in-progress: false", text)
        self.assertIn("contents: read", text)
        self.assertNotIn("contents: write", text)
        self.assertNotIn("id-token", text)
        self.assertNotIn("actions:", text)
        self.assertFalse(
            any(line.strip().startswith("environment:") for line in text.splitlines())
        )
        self.assertIn("RUN_ORBIT_E2E: '1'", text)
        self.assertIn("PREFLIGHT_SCOPE: all", text)
        self.assertIn("bash scripts/preflight.sh", text)
        self.assertIn("bash scripts/write-preflight-witness.sh", text)
        self.assertNotIn("verify-preflight-witness.sh", text)
        self.assertIn("name: preflight-witness", text)
        self.assertIn("if-no-files-found: error", text)
        self.assertIn("overwrite: true", text)
        self.assertIn("cache: false", text)
        self.assertIn("persist-credentials: false", text)
        self.assertIn("timeout-minutes: 60", text)
        self.assertIn("- '*'", text)
        self.assertIn("- '!nightly*'", text)
        self.assertIn("workflow_dispatch:", text)

    def test_publish_workflows_reuse_a_tag_push_witness_or_run_preflight(self) -> None:
        expected = {
            "release.yml": "group: formal-release-${{ github.ref_name || github.run_id }}",
            "electron-build.yml": "group: formal-release-${{ github.ref_name || github.run_id }}",
            "docker-build.yml": "group: publish-docker-tag-${{ github.event.inputs.tag || github.ref_name || github.run_id }}",
        }
        for name, group in expected.items():
            text = (ROOT / ".github" / "workflows" / name).read_text()
            self.assertIn(group, text)
            self.assertIn("bash scripts/reuse-or-run-release-preflight.sh", text)
            self.assertIn("GH_TOKEN: ${{ github.token }}", text)
            self.assertIn("GITHUB_TOKEN: ${{ github.token }}", text)
            self.assertIn("PREFLIGHT_SCOPE: all", text)
            self.assertIn("RUN_ORBIT_E2E: '1'", text)
            self.assertIn("timeout-minutes: 140", text)
            self.assertNotIn("timeout-minutes: 60", text)
            self.assertNotIn("verify-preflight-witness.sh", text)
            self.assertNotIn("release-preflight.yml", text)
            self.assertNotIn("write-preflight-witness.sh", text)
            verify_body = re.split(
                r"\n  [A-Za-z0-9_]+:\n",
                text.split("\n  verify:\n", 1)[1],
                maxsplit=1,
            )[0]
            self.assertNotIn("id-token", verify_body)
            self.assertNotIn("environment:", verify_body)
            self.assertNotIn("\n    if:", verify_body)


if __name__ == "__main__":
    unittest.main()
