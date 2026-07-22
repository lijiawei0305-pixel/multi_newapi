from __future__ import annotations

import importlib.util
import ast
import io
import json
import sys
import tempfile
import threading
import unittest
from contextlib import redirect_stderr
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs


SCRIPT = Path(__file__).resolve().parents[1] / "gitee_release.py"
SPEC = importlib.util.spec_from_file_location("gitee_release", SCRIPT)
assert SPEC and SPEC.loader
gitee_release = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = gitee_release
SPEC.loader.exec_module(gitee_release)


class FakeGiteeHandler(BaseHTTPRequestHandler):
    requests: list[tuple[str, dict[str, str], bytes]] = []
    responses: list[tuple[int, object]] = []

    def handle_request(self) -> None:
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        self.__class__.requests.append((self.path, dict(self.headers), body))
        status, payload = self.__class__.responses.pop(0)
        encoded = payload if isinstance(payload, bytes) else json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header(
            "Content-Type",
            "application/octet-stream" if isinstance(payload, bytes) else "application/json",
        )
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    do_GET = handle_request
    do_POST = handle_request

    def log_message(self, _format: str, *_args: object) -> None:
        return


class FakeGiteeServer:
    def __enter__(self) -> "FakeGiteeServer":
        FakeGiteeHandler.requests = []
        FakeGiteeHandler.responses = []
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), FakeGiteeHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        host, port = self.server.server_address
        self.api_base = f"http://{host}:{port}/api/v5"
        return self

    def __exit__(self, *_args: object) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


class GiteeReleaseTests(unittest.TestCase):
    def config(self, asset: Path, *, retries: int = 3):
        return gitee_release.ReleaseConfig(
            owner="QuantumNous",
            repo="new-api",
            token="sensitive-test-token",
            tag_name="v1.2.3",
            release_name="Release 1.2.3",
            release_body="Deterministic body",
            target_commitish="main",
            assets=(asset,),
            upload_retries=retries,
        )

    def test_create_and_stream_asset_without_token_in_url_or_logs(self) -> None:
        with tempfile.TemporaryDirectory() as directory, FakeGiteeServer() as server:
            asset = Path(directory) / "release artifact.exe"
            asset.write_bytes(b"signed-binary-content")
            FakeGiteeHandler.responses = [
                (404, {"message": "not found"}),
                (201, {"id": 42}),
                (200, []),
                (201, {"id": 43, "name": "release artifact.exe", "size": 21}),
                (200, b"signed-binary-content"),
            ]
            logs: list[str] = []
            client = gitee_release.GiteeClient(
                "QuantumNous",
                "new-api",
                "sensitive-test-token",
                api_base=server.api_base,
                boundary_factory=lambda: "deterministic-boundary",
            )

            release_id = gitee_release.sync_release(
                self.config(asset), client, log=logs.append, sleep=lambda _delay: None
            )

            self.assertEqual(release_id, "42")
            self.assertEqual(len(FakeGiteeHandler.requests), 5)
            create_path, create_headers, create_body = FakeGiteeHandler.requests[1]
            self.assertEqual(create_path, "/api/v5/repos/QuantumNous/new-api/releases")
            self.assertNotIn("sensitive-test-token", create_path)
            create_form = parse_qs(create_body.decode("utf-8"))
            self.assertEqual(create_form["tag_name"], ["v1.2.3"])
            self.assertEqual(create_headers["Authorization"], "Bearer sensitive-test-token")
            self.assertNotIn(b"sensitive-test-token", create_body)
            self.assertIn("application/x-www-form-urlencoded", create_headers["Content-Type"])

            upload_path, upload_headers, upload_body = FakeGiteeHandler.requests[3]
            self.assertEqual(
                upload_path,
                "/api/v5/repos/QuantumNous/new-api/releases/42/attach_files",
            )
            self.assertNotIn("sensitive-test-token", upload_path)
            self.assertIn("multipart/form-data", upload_headers["Content-Type"])
            self.assertEqual(upload_headers["Authorization"], "Bearer sensitive-test-token")
            self.assertNotIn(b"sensitive-test-token", upload_body)
            self.assertIn(b'release artifact.exe', upload_body)
            self.assertIn(b"signed-binary-content", upload_body)
            self.assertNotIn("sensitive-test-token", "\n".join(logs))
            readback_path, readback_headers, readback_body = FakeGiteeHandler.requests[4]
            self.assertEqual(
                readback_path,
                "/api/v5/repos/QuantumNous/new-api/releases/42/attach_files/43/download",
            )
            self.assertEqual(
                readback_headers["Authorization"], "Bearer sensitive-test-token"
            )
            self.assertEqual(readback_body, b"")

    def test_transient_upload_is_retried_without_recreating_release(self) -> None:
        with tempfile.TemporaryDirectory() as directory, FakeGiteeServer() as server:
            asset = Path(directory) / "asset.bin"
            asset.write_bytes(b"asset")
            FakeGiteeHandler.responses = [
                (404, {"message": "not found"}),
                (201, {"id": 7}),
                (200, []),
                (503, {"message": "temporarily unavailable"}),
                (200, []),
                (201, {"id": 8, "name": "asset.bin", "size": 5}),
                (200, b"asset"),
            ]
            logs: list[str] = []
            client = gitee_release.GiteeClient(
                "QuantumNous",
                "new-api",
                "sensitive-test-token",
                api_base=server.api_base,
                boundary_factory=lambda: "deterministic-boundary",
            )

            gitee_release.sync_release(
                self.config(asset, retries=1),
                client,
                log=logs.append,
                sleep=lambda _delay: None,
            )

            self.assertEqual(len(FakeGiteeHandler.requests), 7)
            self.assertEqual(
                sum(path.endswith("/releases") for path, _headers, _body in FakeGiteeHandler.requests),
                1,
            )
            self.assertTrue(any("Retrying Gitee release asset" in line for line in logs))

    def test_uploaded_asset_must_pass_full_readback_verification(self) -> None:
        with tempfile.TemporaryDirectory() as directory, FakeGiteeServer() as server:
            asset = Path(directory) / "asset.bin"
            asset.write_bytes(b"expected")
            FakeGiteeHandler.responses = [
                (404, {"message": "not found"}),
                (201, {"id": 7}),
                (200, []),
                (201, {"id": 8, "name": "asset.bin", "size": 8}),
                (200, b"corrupt!"),
            ]
            client = gitee_release.GiteeClient(
                "QuantumNous",
                "new-api",
                "sensitive-test-token",
                api_base=server.api_base,
                boundary_factory=lambda: "deterministic-boundary",
            )

            with self.assertRaises(gitee_release.GiteeReleaseError) as raised:
                gitee_release.sync_release(
                    self.config(asset), client, sleep=lambda _delay: None
                )

            self.assertIn("readback verification", str(raised.exception))

    def test_rerun_reuses_identical_release_and_asset(self) -> None:
        with tempfile.TemporaryDirectory() as directory, FakeGiteeServer() as server:
            asset = Path(directory) / "asset.bin"
            asset.write_bytes(b"same-content")
            FakeGiteeHandler.responses = [
                (
                    200,
                    {
                        "id": 9,
                        "tag_name": "v1.2.3",
                        "name": "Release 1.2.3",
                        "body": "Deterministic body",
                        "target_commitish": "main",
                    },
                ),
                (200, [{"id": 11, "name": "asset.bin", "size": 12}]),
                (200, b"same-content"),
            ]
            logs: list[str] = []
            client = gitee_release.GiteeClient(
                "QuantumNous",
                "new-api",
                "sensitive-test-token",
                api_base=server.api_base,
            )

            release_id = gitee_release.sync_release(
                self.config(asset), client, log=logs.append, sleep=lambda _delay: None
            )

            self.assertEqual(release_id, "9")
            self.assertEqual(len(FakeGiteeHandler.requests), 3)
            self.assertTrue(all(body == b"" for _path, _headers, body in FakeGiteeHandler.requests))
            self.assertTrue(any("Reusing existing Gitee release asset" in line for line in logs))

    def test_api_error_redacts_token(self) -> None:
        with FakeGiteeServer() as server:
            FakeGiteeHandler.responses = [
                (401, {"message": "rejected sensitive-test-token"})
            ]
            client = gitee_release.GiteeClient(
                "QuantumNous",
                "new-api",
                "sensitive-test-token",
                api_base=server.api_base,
            )
            with self.assertRaises(gitee_release.GiteeReleaseError) as raised:
                client.create_release("v1.2.3", "Release", "Body", "main")
            self.assertNotIn("sensitive-test-token", str(raised.exception))
            self.assertIn("[REDACTED]", str(raised.exception))

    def test_github_asset_manifest_mismatch_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            metadata = root / "release.json"
            assets = root / "release_assets"
            assets.mkdir()
            metadata.write_text(
                json.dumps({"assets": [{"name": "one.exe"}, {"name": "two.exe"}]}),
                encoding="utf-8",
            )
            (assets / "one.exe").write_bytes(b"one")
            with self.assertRaises(gitee_release.GiteeReleaseError):
                gitee_release.verify_github_assets(metadata, assets)

    def test_environment_rejects_symlinked_asset(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            workspace = Path(directory)
            asset_root = workspace / "release_assets"
            asset_root.mkdir()
            outside = workspace / "private.env"
            outside.write_text("secret", encoding="utf-8")
            (asset_root / "asset.bin").symlink_to(outside)
            environment = {
                "GITEE_OWNER": "QuantumNous",
                "GITEE_REPO": "new-api",
                "GITEE_TOKEN": "sensitive-test-token",
                "GITEE_TAG_NAME": "v1.2.3",
                "GITEE_RELEASE_NAME": "Release",
                "GITEE_RELEASE_BODY": "",
                "GITEE_TARGET_COMMITISH": "main",
                "GITEE_FILES": "release_assets/*",
                "GITEE_ASSET_ROOT": "release_assets",
            }
            with self.assertRaises(gitee_release.GiteeReleaseError) as raised:
                gitee_release.ReleaseConfig.from_environment(environment, cwd=workspace)
            self.assertIn("symlink", str(raised.exception))

    def test_cli_failure_never_echoes_token(self) -> None:
        stderr = io.StringIO()
        with redirect_stderr(stderr):
            result = gitee_release.main(
                {
                    "GITEE_TOKEN": "sensitive-test-token",
                    "GITEE_OWNER": "bad/owner-sensitive-test-token",
                },
                [],
            )
        self.assertEqual(result, 1)
        self.assertNotIn("sensitive-test-token", stderr.getvalue())

    def test_script_imports_only_python_standard_library(self) -> None:
        tree = ast.parse(SCRIPT.read_text(encoding="utf-8"))
        imported_roots: set[str] = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imported_roots.update(alias.name.split(".", 1)[0] for alias in node.names)
            elif isinstance(node, ast.ImportFrom) and node.module:
                imported_roots.add(node.module.split(".", 1)[0])
        non_standard = imported_roots - sys.stdlib_module_names - {"__future__"}
        self.assertEqual(non_standard, set())


if __name__ == "__main__":
    unittest.main()
