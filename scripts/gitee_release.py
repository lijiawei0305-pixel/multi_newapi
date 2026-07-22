#!/usr/bin/env python3
"""Create a Gitee release and upload its assets without third-party packages."""

from __future__ import annotations

import glob
import hashlib
import http.client
import json
import os
import re
import secrets
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Mapping
from urllib.parse import quote, urlencode, urlsplit


GITEE_API_BASE = "https://gitee.com/api/v5"
MAX_RESPONSE_BYTES = 1024 * 1024
IDENTIFIER_RE = re.compile(r"^[A-Za-z0-9_.-]+$")
SEMVER_TAG_RE = re.compile(r"^v?[0-9]+\.[0-9]+\.[0-9]+$")


class GiteeReleaseError(RuntimeError):
    def __init__(self, message: str, *, retryable: bool = False) -> None:
        super().__init__(message)
        self.retryable = retryable


def redact(message: str, token: str) -> str:
    if not token:
        return message
    redacted = message.replace(token, "[REDACTED]")
    return redacted.replace(quote(token, safe=""), "[REDACTED]")


def require_environment(
    environ: Mapping[str, str], key: str, *, allow_empty: bool = False
) -> str:
    if key not in environ or (not allow_empty and not environ[key]):
        raise GiteeReleaseError(f"{key} is required")
    return environ[key]


def file_digest(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


@dataclass(frozen=True)
class ReleaseConfig:
    owner: str
    repo: str
    token: str
    tag_name: str
    release_name: str
    release_body: str
    target_commitish: str
    assets: tuple[Path, ...]
    upload_retries: int

    @classmethod
    def from_environment(
        cls, environ: Mapping[str, str], *, cwd: Path | None = None
    ) -> "ReleaseConfig":
        workspace = (cwd or Path.cwd()).resolve()
        owner = require_environment(environ, "GITEE_OWNER")
        repo = require_environment(environ, "GITEE_REPO")
        token = require_environment(environ, "GITEE_TOKEN")
        tag_name = require_environment(environ, "GITEE_TAG_NAME")
        release_name = require_environment(environ, "GITEE_RELEASE_NAME", allow_empty=True)
        release_body = require_environment(environ, "GITEE_RELEASE_BODY", allow_empty=True)
        target_commitish = require_environment(environ, "GITEE_TARGET_COMMITISH")

        for label, value in (("owner", owner), ("repo", repo)):
            if not IDENTIFIER_RE.fullmatch(value):
                raise GiteeReleaseError(f"invalid Gitee {label}")
        if not SEMVER_TAG_RE.fullmatch(tag_name):
            raise GiteeReleaseError("GITEE_TAG_NAME must be a strict semantic-version tag")
        if "\r" in token or "\n" in token:
            raise GiteeReleaseError("GITEE_TOKEN contains an invalid newline")
        if not target_commitish or len(target_commitish) > 256 or "\n" in target_commitish:
            raise GiteeReleaseError("GITEE_TARGET_COMMITISH is invalid")

        retries_text = environ.get("GITEE_UPLOAD_RETRIES", "3")
        try:
            upload_retries = int(retries_text)
        except ValueError as exc:
            raise GiteeReleaseError("GITEE_UPLOAD_RETRIES must be an integer") from exc
        if not 0 <= upload_retries <= 10:
            raise GiteeReleaseError("GITEE_UPLOAD_RETRIES must be between 0 and 10")

        asset_root_text = environ.get("GITEE_ASSET_ROOT", "release_assets")
        asset_root = (workspace / asset_root_text).resolve()
        try:
            asset_root.relative_to(workspace)
        except ValueError as exc:
            raise GiteeReleaseError("GITEE_ASSET_ROOT must stay inside the workspace") from exc

        assets: list[Path] = []
        seen: set[Path] = set()
        for pattern in environ.get("GITEE_FILES", "").splitlines():
            pattern = pattern.strip()
            if not pattern:
                continue
            if Path(pattern).is_absolute():
                raise GiteeReleaseError("GITEE_FILES patterns must be workspace-relative")
            matches = glob.glob(str(workspace / pattern), recursive="**" in pattern)
            for match in sorted(matches):
                candidate = Path(match)
                if candidate.is_symlink():
                    raise GiteeReleaseError(f"refusing symlink release asset: {candidate.name}")
                resolved = candidate.resolve()
                try:
                    resolved.relative_to(asset_root)
                except ValueError as exc:
                    raise GiteeReleaseError(
                        f"release asset is outside GITEE_ASSET_ROOT: {candidate.name}"
                    ) from exc
                if not resolved.is_file() or resolved in seen:
                    continue
                seen.add(resolved)
                assets.append(resolved)

        return cls(
            owner=owner,
            repo=repo,
            token=token,
            tag_name=tag_name,
            release_name=release_name,
            release_body=release_body,
            target_commitish=target_commitish,
            assets=tuple(assets),
            upload_retries=upload_retries,
        )


class GiteeClient:
    def __init__(
        self,
        owner: str,
        repo: str,
        token: str,
        *,
        api_base: str = GITEE_API_BASE,
        timeout: float = 60,
        boundary_factory: Callable[[], str] | None = None,
    ) -> None:
        self.owner = owner
        self.repo = repo
        self.token = token
        self.timeout = timeout
        self.boundary_factory = boundary_factory or (
            lambda: f"newapi-gitee-{secrets.token_hex(16)}"
        )
        parsed = urlsplit(api_base)
        if (
            parsed.scheme not in {"http", "https"}
            or not parsed.hostname
            or parsed.username
            or parsed.password
            or parsed.query
            or parsed.fragment
        ):
            raise GiteeReleaseError("invalid Gitee API base URL")
        self.scheme = parsed.scheme
        self.host = parsed.hostname
        self.port = parsed.port
        self.base_path = parsed.path.rstrip("/")

    def _connection(self) -> http.client.HTTPConnection:
        connection_type = (
            http.client.HTTPSConnection
            if self.scheme == "https"
            else http.client.HTTPConnection
        )
        return connection_type(self.host, self.port, timeout=self.timeout)

    def _endpoint(self, suffix: str) -> str:
        owner = quote(self.owner, safe="")
        repo = quote(self.repo, safe="")
        return f"{self.base_path}/repos/{owner}/{repo}{suffix}"

    def _decode_response(self, response: http.client.HTTPResponse) -> object:
        payload_bytes = response.read(MAX_RESPONSE_BYTES + 1)
        if len(payload_bytes) > MAX_RESPONSE_BYTES:
            raise GiteeReleaseError("Gitee API response exceeded 1 MiB")
        try:
            payload = json.loads(payload_bytes.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise GiteeReleaseError(
                f"Gitee API returned invalid JSON (HTTP {response.status})",
                retryable=response.status == 429 or response.status >= 500,
            ) from exc
        if not 200 <= response.status < 300:
            server_message = payload.get("message") if isinstance(payload, dict) else None
            detail = server_message if isinstance(server_message, str) else "request failed"
            raise GiteeReleaseError(
                redact(f"Gitee API HTTP {response.status}: {detail}", self.token),
                retryable=response.status == 429 or response.status >= 500,
            )
        return payload

    def _post_bytes(
        self, endpoint: str, body: bytes, content_type: str
    ) -> dict[str, object]:
        connection = self._connection()
        try:
            connection.putrequest("POST", endpoint)
            connection.putheader("Accept", "application/json")
            connection.putheader("Authorization", f"Bearer {self.token}")
            connection.putheader("Content-Type", content_type)
            connection.putheader("Content-Length", str(len(body)))
            connection.putheader("User-Agent", "new-api-gitee-release-sync/1")
            connection.endheaders()
            connection.send(body)
            payload = self._decode_response(connection.getresponse())
            if not isinstance(payload, dict):
                raise GiteeReleaseError("Gitee API returned a non-object response")
            return payload
        except GiteeReleaseError:
            raise
        except (OSError, http.client.HTTPException) as exc:
            raise GiteeReleaseError(
                redact(f"Gitee API request failed: {exc}", self.token), retryable=True
            ) from exc
        finally:
            connection.close()

    def _get_json(self, endpoint: str, *, allow_not_found: bool = False) -> object | None:
        connection = self._connection()
        try:
            connection.putrequest("GET", endpoint)
            connection.putheader("Accept", "application/json")
            connection.putheader("Authorization", f"Bearer {self.token}")
            connection.putheader("User-Agent", "new-api-gitee-release-sync/1")
            connection.endheaders()
            response = connection.getresponse()
            if allow_not_found and response.status == 404:
                response.read(MAX_RESPONSE_BYTES + 1)
                return None
            return self._decode_response(response)
        except GiteeReleaseError:
            raise
        except (OSError, http.client.HTTPException) as exc:
            raise GiteeReleaseError(
                redact(f"Gitee API request failed: {exc}", self.token), retryable=True
            ) from exc
        finally:
            connection.close()

    def get_release_by_tag(self, tag_name: str) -> dict[str, object] | None:
        payload = self._get_json(
            self._endpoint(f"/releases/tags/{quote(tag_name, safe='')}"),
            allow_not_found=True,
        )
        if payload is not None and not isinstance(payload, dict):
            raise GiteeReleaseError("Gitee tag lookup returned a non-object response")
        return payload

    def list_assets(self, release_id: str) -> list[dict[str, object]]:
        if not release_id.isdigit():
            raise GiteeReleaseError("invalid Gitee release id")
        payload = self._get_json(
            self._endpoint(
                f"/releases/{release_id}/attach_files?page=1&per_page=100&direction=asc"
            )
        )
        if not isinstance(payload, list) or any(not isinstance(item, dict) for item in payload):
            raise GiteeReleaseError("Gitee asset listing returned an invalid response")
        if len(payload) == 100:
            raise GiteeReleaseError("Gitee release has 100 or more assets; refusing partial listing")
        return payload

    def asset_digest(self, release_id: str, asset_id: str) -> str:
        if not release_id.isdigit() or not asset_id.isdigit():
            raise GiteeReleaseError("invalid Gitee release or asset id")
        endpoint = self._endpoint(
            f"/releases/{release_id}/attach_files/{asset_id}/download"
        )
        connection = self._connection()
        try:
            connection.putrequest("GET", endpoint)
            connection.putheader("Accept", "application/octet-stream")
            connection.putheader("Authorization", f"Bearer {self.token}")
            connection.putheader("User-Agent", "new-api-gitee-release-sync/1")
            connection.endheaders()
            response = connection.getresponse()
            if not 200 <= response.status < 300:
                self._decode_response(response)
            digest = hashlib.sha256()
            while chunk := response.read(1024 * 1024):
                digest.update(chunk)
            return digest.hexdigest()
        except GiteeReleaseError:
            raise
        except (OSError, http.client.HTTPException) as exc:
            raise GiteeReleaseError(
                redact(f"Gitee asset verification failed: {exc}", self.token),
                retryable=True,
            ) from exc
        finally:
            connection.close()

    def create_release(
        self, tag_name: str, name: str, body: str, target_commitish: str
    ) -> str:
        form = urlencode(
            {
                "tag_name": tag_name,
                "name": name,
                "body": body,
                "target_commitish": target_commitish,
            }
        ).encode("utf-8")
        payload = self._post_bytes(
            self._endpoint("/releases"),
            form,
            "application/x-www-form-urlencoded; charset=utf-8",
        )
        release_id = str(payload.get("id", ""))
        if not release_id.isdigit():
            raise GiteeReleaseError("Gitee create-release response has no numeric id")
        return release_id

    def upload_asset(self, release_id: str, asset: Path) -> str:
        if not release_id.isdigit():
            raise GiteeReleaseError("invalid Gitee release id")
        filename = asset.name
        if not filename or "\r" in filename or "\n" in filename:
            raise GiteeReleaseError("invalid release asset filename")
        quoted_filename = filename.replace("\\", "_").replace('"', "_")
        boundary = self.boundary_factory()
        if not re.fullmatch(r"[A-Za-z0-9_.-]{1,70}", boundary):
            raise GiteeReleaseError("invalid multipart boundary")
        prefix = (
            f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="file"; filename="{quoted_filename}"\r\n'
            "Content-Type: application/octet-stream\r\n\r\n"
        ).encode("utf-8")
        suffix = f"\r\n--{boundary}--\r\n".encode("ascii")
        content_length = len(prefix) + asset.stat().st_size + len(suffix)
        endpoint = self._endpoint(f"/releases/{release_id}/attach_files")
        connection = self._connection()
        try:
            connection.putrequest("POST", endpoint)
            connection.putheader("Accept", "application/json")
            connection.putheader("Authorization", f"Bearer {self.token}")
            connection.putheader(
                "Content-Type", f"multipart/form-data; boundary={boundary}"
            )
            connection.putheader("Content-Length", str(content_length))
            connection.putheader("User-Agent", "new-api-gitee-release-sync/1")
            connection.endheaders()
            connection.send(prefix)
            with asset.open("rb") as stream:
                while chunk := stream.read(1024 * 1024):
                    connection.send(chunk)
            connection.send(suffix)
            payload = self._decode_response(connection.getresponse())
        except GiteeReleaseError:
            raise
        except (OSError, http.client.HTTPException) as exc:
            raise GiteeReleaseError(
                redact(f"Gitee asset upload failed: {exc}", self.token), retryable=True
            ) from exc
        finally:
            connection.close()
        if not isinstance(payload, dict):
            raise GiteeReleaseError("Gitee upload returned a non-object response")
        asset_id = str(payload.get("id", ""))
        if not asset_id.isdigit():
            raise GiteeReleaseError("Gitee upload response has no numeric asset id")
        if payload.get("name") not in (None, asset.name):
            raise GiteeReleaseError("Gitee upload response has a mismatched asset name")
        if payload.get("size") not in (None, asset.stat().st_size):
            raise GiteeReleaseError("Gitee upload response has a mismatched asset size")
        return asset_id


def sync_release(
    config: ReleaseConfig,
    client: GiteeClient,
    *,
    log: Callable[[str], None] = print,
    sleep: Callable[[float], None] = time.sleep,
) -> str:
    existing_release = client.get_release_by_tag(config.tag_name)
    if existing_release is None:
        release_id = client.create_release(
            config.tag_name,
            config.release_name,
            config.release_body,
            config.target_commitish,
        )
        log(f"Created Gitee release {config.tag_name} (id {release_id})")
    else:
        release_id = str(existing_release.get("id", ""))
        if not release_id.isdigit():
            raise GiteeReleaseError("existing Gitee release has no numeric id")
        expected_fields = {
            "tag_name": config.tag_name,
            "name": config.release_name,
            "body": config.release_body,
            "target_commitish": config.target_commitish,
        }
        conflicts = [
            key for key, expected in expected_fields.items() if existing_release.get(key) != expected
        ]
        if conflicts:
            raise GiteeReleaseError(
                "existing Gitee release metadata conflicts for: " + ", ".join(conflicts)
            )
        log(f"Reusing existing Gitee release {config.tag_name} (id {release_id})")
    if not config.assets:
        log("No Gitee release assets to upload")
        return release_id

    for asset in config.assets:
        for attempt in range(config.upload_retries + 1):
            try:
                matches = [
                    item for item in client.list_assets(release_id) if item.get("name") == asset.name
                ]
                if len(matches) > 1:
                    raise GiteeReleaseError(
                        f"Gitee release has duplicate assets named {asset.name}"
                    )
                if matches:
                    remote_id = str(matches[0].get("id", ""))
                    remote_size = matches[0].get("size")
                    if remote_size != asset.stat().st_size:
                        raise GiteeReleaseError(
                            f"existing Gitee asset conflicts with local file: {asset.name}"
                        )
                    local_digest = file_digest(asset)
                    if client.asset_digest(release_id, remote_id) != local_digest:
                        raise GiteeReleaseError(
                            f"existing Gitee asset content conflicts: {asset.name}"
                        )
                    log(f"Reusing existing Gitee release asset: {asset.name}")
                    break
                uploaded_id = client.upload_asset(release_id, asset)
                if client.asset_digest(release_id, uploaded_id) != file_digest(asset):
                    raise GiteeReleaseError(
                        f"uploaded Gitee asset failed readback verification: {asset.name}"
                    )
                log(f"Uploaded Gitee release asset: {asset.name}")
                break
            except GiteeReleaseError as exc:
                if not exc.retryable or attempt == config.upload_retries:
                    raise
                log(
                    f"Retrying Gitee release asset {asset.name} "
                    f"({attempt + 1}/{config.upload_retries})"
                )
                sleep(min(2**attempt, 8))
    return release_id


def verify_github_assets(release_info_file: Path, asset_directory: Path) -> None:
    try:
        release_info = json.loads(release_info_file.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise GiteeReleaseError("GitHub release metadata is unreadable") from exc
    raw_assets = release_info.get("assets") if isinstance(release_info, dict) else None
    if not isinstance(raw_assets, list):
        raise GiteeReleaseError("GitHub release metadata has no asset manifest")
    expected: list[str] = []
    for item in raw_assets:
        name = item.get("name") if isinstance(item, dict) else None
        if not isinstance(name, str) or not name or Path(name).name != name:
            raise GiteeReleaseError("GitHub release contains an invalid asset name")
        expected.append(name)
    if len(expected) != len(set(expected)):
        raise GiteeReleaseError("GitHub release contains duplicate asset names")
    actual: list[str] = []
    for item in asset_directory.iterdir():
        if item.is_symlink() or not item.is_file():
            raise GiteeReleaseError(f"downloaded release asset is not a regular file: {item.name}")
        actual.append(item.name)
    if sorted(actual) != sorted(expected):
        raise GiteeReleaseError("downloaded GitHub release assets differ from its manifest")


def main(
    environ: Mapping[str, str] | None = None, argv: list[str] | None = None
) -> int:
    environment = os.environ if environ is None else environ
    arguments = sys.argv[1:] if argv is None else argv
    token = environment.get("GITEE_TOKEN", "")
    try:
        if arguments == ["verify-github-assets"]:
            verify_github_assets(
                Path(require_environment(environment, "GITHUB_RELEASE_INFO_FILE")),
                Path(require_environment(environment, "GITHUB_RELEASE_ASSET_DIR")),
            )
            return 0
        if arguments:
            raise GiteeReleaseError("unsupported command")
        config = ReleaseConfig.from_environment(environment)
        client = GiteeClient(config.owner, config.repo, config.token)
        sync_release(config, client)
    except Exception as exc:  # Keep unexpected library errors secret-safe too.
        print(f"Gitee release sync failed: {redact(str(exc), token)}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
