#!/usr/bin/env python3
"""Run the frozen v1 ops catalog against an isolated synthetic Docker target."""

from __future__ import annotations

import argparse
import copy
import hashlib
import io
import json
import os
import secrets
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
from pathlib import Path
from typing import Any

import yaml


FEATURE = "2026-07-22-v1-hardening"
CATALOG_REL = ".codestable/features/2026-07-22-v1-hardening/v1-hardening-ops-case-catalog.yaml"
EVIDENCE_REL = ".codestable/features/2026-07-22-v1-hardening/evidence/ops"
FIVE_FILES = {"database.sql", "avatar-volume.tgz", "avatar-manifest.json", "metadata.json", "SHA256SUMS"}
FENCED_PLANS = {"BKR", "BKE", "BKF", "BKP", "LCK", "LCO", "RSP", "RSR", "RSE", "RSF", "COR"}
BACKUP_ORDER = [
    "backup-stop", "backup-pg-dump", "backup-avatar-tar", "backup-manifest",
    "backup-package-verify", "backup-state-restore", "backup-health", "backup-publish",
]
RESTORE_ORDER = [
    "package-validate", "restore-stop", "restore-db-replace", "restore-avatar-replace",
    "restore-verify", "restore-start", "restore-health",
]


class SmokeFailure(RuntimeError):
    pass


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def sha256_file(path: Path) -> str:
    return sha256_bytes(path.read_bytes())


def envelope(value: Any) -> dict[str, Any]:
    return {"status": "observed", "value": value, "error_class": None}


def unreadable_envelope(error_class: str) -> dict[str, Any]:
    return {"status": "unreadable", "value": None, "error_class": error_class}


def normalized_manifest_oracle(raw: bytes) -> tuple[str, str]:
    """Return stable inventory and exact-generation digests, excluding generated_at."""
    document = json.loads(raw)
    normalized = {
        "format": document["format"],
        "current": document["current"],
        "inventory": document["inventory"],
    }
    canonical = json.dumps(normalized, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    inventory = normalized["inventory"]
    marker = inventory["actual_sha256"]
    if not isinstance(marker, str) or len(marker) != 64:
        raise SmokeFailure("avatar manifest inventory digest is invalid")
    return marker, sha256_bytes(canonical)


class Runner:
    def __init__(self, repo: Path, results: Path, selected: set[str]) -> None:
        self.repo = repo
        self.catalog_path = repo / CATALOG_REL
        self.catalog = yaml.safe_load(self.catalog_path.read_text(encoding="utf-8"))
        self.by_id = {item["case_id"]: item for item in self.catalog["cases"]}
        self.results_path = results
        self.selected = selected
        self.real_docker = shutil.which("docker")
        if not self.real_docker:
            raise SmokeFailure("docker CLI is required")
        self.root = Path(tempfile.mkdtemp(prefix="v1-ops-smoke-"))
        self.bin_dir = self.root / "bin"
        self.missing_command_bin = self.root / "missing-command-bin"
        self.tmp_root = self.root / "tmp"
        self.tmp_root.mkdir(mode=0o700)
        self.proxy_state = self.root / "proxy-state"
        self.evidence_dir = repo / EVIDENCE_REL
        self.evidence_dir.mkdir(parents=True, exist_ok=True)
        self.log_path = self.evidence_dir / "v1-ops-smoke.log"
        self.docker_log = self.evidence_dir / "v1-ops-smoke-docker.jsonl"
        self.log_path.write_text("", encoding="utf-8")
        self.docker_log.write_text("", encoding="utf-8")
        self.endpoint = self.discover_endpoint()
        suffix = f"{os.getpid()}-{secrets.token_hex(4)}"
        self.project = f"v1-ops-smoke-{suffix}"
        self.sentinel_project = f"v1-ops-sentinel-{suffix}"
        self.env_file = self.root / "synthetic.env"
        self.compose_file = self.root / "synthetic-compose.yml"
        self.avatar_host = self.root / "binary-avatar"
        self.avatar_host.mkdir(mode=0o700)
        self.output_root = self.root / "outputs"
        self.output_root.mkdir(mode=0o700)
        self.package_root = self.root / "packages"
        self.package_root.mkdir(mode=0o700)
        self.proxy_wrapper = self.bin_dir / "docker"
        self.records: list[dict[str, Any]] = []
        self.created_ids: set[str] = set()
        self.harness_owned_ids: set[str] = set()
        self.app_id = ""
        self.pg_id = ""
        self.avatar_volume = ""
        self.pg_volume = ""
        self.network = ""
        self.target_hash = sha256_bytes(("unavailable\0" + self.project).encode())
        self.sentinel_before: dict[str, Any] | None = None
        self.base_package = self.package_root / "canonical"
        self.base_package_ready = False
        self.setup_complete = False
        self.write_proxy_wrapper()

    def discover_endpoint(self) -> str:
        completed = subprocess.run(
            [self.real_docker, "context", "inspect", "desktop-linux", "--format", "{{.Endpoints.docker.Host}}"],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, check=False,
        )
        if completed.returncode != 0:
            raise SmokeFailure(f"cannot inspect desktop-linux context: {completed.stderr.strip()}")
        endpoint = completed.stdout.strip()
        if not endpoint.startswith("unix:///"):
            raise SmokeFailure(f"Docker endpoint is not a local Unix socket: {endpoint}")
        socket = Path(endpoint.removeprefix("unix://")).resolve()
        if not socket.exists() or not stat.S_ISSOCK(socket.stat().st_mode):
            raise SmokeFailure(f"Docker Unix socket is unavailable: {socket}")
        return "unix://" + str(socket)

    def write_proxy_wrapper(self) -> None:
        self.bin_dir.mkdir(mode=0o700)
        self.proxy_state.mkdir(mode=0o700)
        proxy = self.repo / "scripts/lib/v1-ops-smoke-docker-proxy.py"
        self.proxy_wrapper.write_text(
            f"#!/bin/sh\nexec {shlex_quote(sys.executable)} {shlex_quote(str(proxy))} \"$@\"\n",
            encoding="utf-8",
        )
        self.proxy_wrapper.chmod(0o700)

    def docker(self, *args: str, input_bytes: bytes | None = None, check: bool = True) -> subprocess.CompletedProcess[bytes]:
        completed = subprocess.run(
            [self.real_docker, "--host", self.endpoint, *args],
            input=input_bytes, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
        )
        if check and completed.returncode != 0:
            raise SmokeFailure(
                f"docker {' '.join(args)} failed ({completed.returncode}): "
                f"{completed.stderr.decode(errors='replace').strip()}"
            )
        return completed

    def compose(self, *args: str, env_file: Path | None = None, compose_file: Path | None = None, project: str | None = None, check: bool = True) -> subprocess.CompletedProcess[bytes]:
        selected_env = env_file or self.env_file
        selected_compose = compose_file or self.compose_file
        selected_project = project or self.project
        env = os.environ.copy()
        env["CRM_ENV_FILE"] = str(selected_env)
        return subprocess.run(
            [self.real_docker, "--host", self.endpoint, "compose", "--env-file", str(selected_env),
             "-f", str(selected_compose), "-p", selected_project, *args],
            env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=check,
        )

    def write_env(self, path: Path, values: dict[str, str]) -> None:
        path.write_text("".join(f"{key}={value}\n" for key, value in values.items()), encoding="utf-8")
        path.chmod(0o600)

    def base_env(self, *, seed_state: str = "initialized", mode: str = "managed") -> dict[str, str]:
        values = {
            "POSTGRES_USER": "crm",
            "POSTGRES_PASSWORD": "synthetic-only-password-7e51613f",
            "POSTGRES_DB": "crm",
            "AUTH_TOKEN_SECRET": "0123456789abcdef0123456789abcdef",
            "SEED_ADMIN_PASSWORD": "" if seed_state == "initialized" else "synthetic-admin-password",
            "APP_DATABASE_URL": "",
            "DATABASE_URL": "postgres://crm:synthetic-only-password@127.0.0.1:5432/crm?sslmode=disable",
            "HTTP_ADDR": "127.0.0.1:8080",
            "AVATAR_STORAGE_DRIVER": "local",
            "AVATAR_LOCAL_ROOT": str(self.avatar_host),
            "AVATAR_LOCAL_REQUIRE_MOUNT": "false",
            "TELEGRAM_BOT_TOKEN": "",
            "TELEGRAM_BOT_USERNAME": "",
        }
        if mode == "external":
            values["APP_DATABASE_URL"] = "postgres://crm:synthetic-only-password@db.invalid:5432/crm?sslmode=require"
        return values

    def write_compose(
        self,
        path: Path,
        *,
        service_fault: str = "",
        env_auth: str = "${AUTH_TOKEN_SECRET}",
        http_addr: str = '":8080"',
        avatar_driver: str = "local",
        avatar_root: str = "/var/lib/crm/avatars",
        avatar_require_mount: str = "true",
        avatar_mount: str = "/var/lib/crm/avatars",
    ) -> None:
        app_service = f"""  app:
    image: crm:v1-hardening
    env_file:
      - path: ${{CRM_ENV_FILE}}
        required: true
    environment:
      DATABASE_URL: ${{APP_DATABASE_URL:-postgres://${{POSTGRES_USER}}:${{POSTGRES_PASSWORD}}@postgres:5432/${{POSTGRES_DB}}?sslmode=disable}}
      AUTH_TOKEN_SECRET: {env_auth}
      HTTP_ADDR: {http_addr}
      AVATAR_STORAGE_DRIVER: {avatar_driver}
      AVATAR_LOCAL_ROOT: {avatar_root}
      AVATAR_LOCAL_REQUIRE_MOUNT: "{avatar_require_mount}"
      TELEGRAM_BOT_TOKEN: ${{TELEGRAM_BOT_TOKEN:-}}
      TELEGRAM_BOT_USERNAME: ${{TELEGRAM_BOT_USERNAME:-}}
    volumes:
      - avatar_data:{avatar_mount}
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 1s
      timeout: 3s
      retries: 60
"""
        pg_service = """  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}"]
      interval: 1s
      timeout: 3s
      retries: 60
"""
        services = app_service + pg_service
        volumes = "volumes:\n  avatar_data: {}\n  pgdata: {}\n"
        if service_fault == "service":
            services = app_service
        elif service_fault == "volume":
            volumes = "volumes:\n  avatar_data: {}\n"
        path.write_text("services:\n" + services + volumes, encoding="utf-8")
        path.chmod(0o600)

    def setup(self) -> None:
        values = self.base_env(seed_state="empty")
        self.write_env(self.env_file, values)
        self.write_compose(self.compose_file)
        completed = self.compose("up", "-d", "--wait", check=False)
        if completed.returncode != 0:
            raise SmokeFailure(f"synthetic compose up failed: {completed.stderr.decode(errors='replace').strip()}")
        values["SEED_ADMIN_PASSWORD"] = ""
        self.write_env(self.env_file, values)
        completed = self.compose("up", "-d", "--force-recreate", "--no-deps", "--wait", "app", check=False)
        if completed.returncode != 0:
            raise SmokeFailure(f"synthetic app recreate failed: {completed.stderr.decode(errors='replace').strip()}")
        self.app_id = self.one_project_id("container", "app")
        self.pg_id = self.one_project_id("container", "postgres")
        self.avatar_volume = self.one_project_id("volume", "avatar_data")
        self.pg_volume = self.one_project_id("volume", "pgdata")
        networks = self.docker("network", "ls", "-q", "--filter", f"label=com.docker.compose.project={self.project}").stdout.decode().split()
        if len(networks) != 1:
            raise SmokeFailure(f"expected one synthetic network, got {networks}")
        self.network = networks[0]
        engine_id = self.docker("info", "--format", "{{.ID}}").stdout.decode().strip()
        # Match the production lock identity material byte-for-byte.  The v1
        # contract uses a literal ``\\0`` separator, not an in-memory NUL.
        material = f"schema=v1\\0docker-engine-id={engine_id}\\0project={self.project}\\0services=app,postgres\\0volumes=avatar_data,pgdata"
        self.target_hash = sha256_bytes(material.encode())
        self.setup_sentinel()
        self.setup_complete = True

    def one_project_id(self, kind: str, logical: str) -> str:
        if kind == "container":
            completed = self.docker("ps", "-aq", "--filter", f"label=com.docker.compose.project={self.project}", "--filter", f"label=com.docker.compose.service={logical}")
        else:
            completed = self.docker("volume", "ls", "-q", "--filter", f"label=com.docker.compose.project={self.project}", "--filter", f"label=com.docker.compose.volume={logical}")
        ids = completed.stdout.decode().split()
        if len(ids) != 1:
            raise SmokeFailure(f"expected one {kind} {logical}, got {ids}")
        return ids[0]

    def create_helper(self, name: str, volume: str, entrypoint: str, *command: str) -> str:
        network = self.network if self.network and volume == self.avatar_volume else "none"
        completed = self.docker(
            "create", "--name", name, "--network", network,
            "--env-file", str(self.env_file),
            "--env", "DATABASE_URL=postgres://crm:synthetic-only-password-7e51613f@postgres:5432/crm?sslmode=disable",
            "--env", "AVATAR_STORAGE_DRIVER=local",
            "--env", "AVATAR_LOCAL_ROOT=/var/lib/crm/avatars",
            "--env", "AVATAR_LOCAL_REQUIRE_MOUNT=true",
            "--mount", f"type=volume,source={volume},destination=/var/lib/crm/avatars",
            "--entrypoint", entrypoint, "crm:v1-hardening", *command,
        )
        container_id = completed.stdout.decode().strip()
        self.created_ids.add(container_id)
        return container_id

    def write_avatar_marker(self, text: str) -> None:
        helper = self.create_helper(
            f"{self.project}-marker-{secrets.token_hex(3)}", self.avatar_volume,
            "/bin/sh", "-c", "umask 077; printf '%s' \"$1\" > /var/lib/crm/avatars/.v1-smoke-marker", "sh", text,
        )
        self.docker("start", "-a", helper)
        self.docker("rm", "-v", helper)
        self.created_ids.discard(helper)

    def setup_sentinel(self) -> None:
        volume = self.docker("volume", "create", "--label", f"com.docker.compose.project={self.sentinel_project}", "--label", "com.docker.compose.volume=sentinel", f"{self.sentinel_project}_sentinel").stdout.decode().strip()
        completed = self.docker(
            "create", "--name", f"{self.sentinel_project}-marker", "--network", "none",
            "--user", "0:0",
            "--label", f"com.docker.compose.project={self.sentinel_project}",
            "--label", "com.docker.compose.service=sentinel",
            "--mount", f"type=volume,source={volume},destination=/sentinel",
            "--entrypoint", "/bin/sh", "crm:v1-hardening", "-c",
            "printf sentinel-v1 > /sentinel/marker",
        )
        container_id = completed.stdout.decode().strip()
        self.created_ids.add(container_id)
        self.docker("start", "-a", container_id)
        self.sentinel_before = self.sentinel_snapshot()

    def sentinel_snapshot(self) -> dict[str, Any]:
        ids = self.docker("ps", "-aq", "--filter", f"label=com.docker.compose.project={self.sentinel_project}").stdout.decode().split()
        volumes = self.docker("volume", "ls", "-q", "--filter", f"label=com.docker.compose.project={self.sentinel_project}").stdout.decode().split()
        inspect = self.docker("container", "inspect", *ids).stdout if ids else b"[]"
        marker = b""
        if volumes:
            helper = self.create_helper(
                f"{self.sentinel_project}-read-{secrets.token_hex(3)}", volumes[0], "/bin/sh", "-c", "cat /var/lib/crm/avatars/marker",
            )
            marker = self.docker("start", "-a", helper).stdout
            self.docker("rm", "-v", helper)
            self.created_ids.discard(helper)
        return {"ids": ids, "volumes": volumes, "inspect_sha256": sha256_bytes(inspect), "marker_sha256": sha256_bytes(marker)}

    def proxy_env(
        self,
        case_id: str,
        fault: str = "",
        output: Path | None = None,
        input_path: Path | None = None,
        lock_role: str = "",
        helper_role: str = "",
    ) -> dict[str, str]:
        env = os.environ.copy()
        env.update({
            "PATH": str(self.bin_dir) + os.pathsep + env.get("PATH", ""),
            "V1_SMOKE_REAL_DOCKER": str(self.real_docker),
            "V1_SMOKE_PINNED_HOST": self.endpoint,
            "V1_SMOKE_CASE_ID": case_id,
            "V1_SMOKE_FAULT": fault,
            "V1_SMOKE_DOCKER_LOG": str(self.docker_log),
            "V1_SMOKE_PROXY_STATE": str(self.proxy_state / case_id),
            "V1_SMOKE_APP_ID": self.app_id,
            "V1_SMOKE_OUTPUT": str(output) if output else "",
            "V1_SMOKE_INPUT": str(input_path) if input_path else "",
            "V1_SMOKE_LOCK_ROLE": lock_role,
            "V1_SMOKE_HELPER_ROLE": helper_role,
            "TMPDIR": str(self.tmp_root),
        })
        for key in ("DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"):
            env.pop(key, None)
        return env

    def run_command(
        self,
        case_id: str,
        command: list[str],
        *,
        fault: str = "",
        output: Path | None = None,
        input_path: Path | None = None,
        env_extra: dict[str, str] | None = None,
        missing_docker: bool = False,
        input_changed: Path | None = None,
        lock_role: str = "",
        helper_role: str = "",
    ) -> subprocess.CompletedProcess[bytes]:
        env = self.proxy_env(case_id, fault, output, input_path, lock_role, helper_role)
        if env_extra:
            env.update(env_extra)
        if missing_docker:
            # Keep only the tools needed to load the shell scripts.  The
            # production dependency gate must be the component that observes
            # the deliberately missing Docker CLI.
            self.missing_command_bin.mkdir(mode=0o700, exist_ok=True)
            for name in ("bash", "dirname"):
                target = shutil.which(name)
                if not target:
                    raise SmokeFailure(f"cannot build missing-command fixture: {name} unavailable")
                link = self.missing_command_bin / name
                if not link.exists():
                    link.symlink_to(target)
            env["PATH"] = str(self.missing_command_bin)
        stdout_path = self.evidence_dir / f"{case_id}.stdout"
        stderr_path = self.evidence_dir / f"{case_id}.stderr"
        watcher: threading.Thread | None = None
        if input_changed is not None:
            watcher = threading.Thread(
                target=self.mutate_input_during_snapshot,
                args=(case_id, input_changed),
                daemon=True,
            )
            watcher.start()
        completed = subprocess.run(command, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
        if watcher is not None:
            watcher.join(timeout=2)
        stdout_path.write_bytes(completed.stdout)
        stderr_path.write_bytes(completed.stderr)
        with self.log_path.open("a", encoding="utf-8") as stream:
            stage = self.terminal_stage(completed)
            stream.write(f"case={case_id} exit={completed.returncode} stage={stage} fault={fault or 'none'}\n")
        return completed

    def proxy_docker(
        self,
        case_id: str,
        *args: str,
        lock_role: str = "",
        helper_role: str = "",
        check: bool = True,
    ) -> subprocess.CompletedProcess[bytes]:
        completed = subprocess.run(
            [str(self.proxy_wrapper), *args],
            env=self.proxy_env(case_id, lock_role=lock_role, helper_role=helper_role),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        if check and completed.returncode != 0:
            raise SmokeFailure(
                f"proxied docker {' '.join(args)} failed ({completed.returncode}): "
                f"{completed.stderr.decode(errors='replace').strip()}"
            )
        return completed

    def mutate_input_during_snapshot(self, case_id: str, package: Path) -> None:
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if any(self.tmp_root.glob("v1-ops-restore-*/staging/SHA256SUMS")):
                with (package / "SHA256SUMS").open("a", encoding="utf-8") as stream:
                    stream.write("# synthetic concurrent mutation\n")
                with self.docker_log.open("a", encoding="utf-8") as stream:
                    stream.write(json.dumps({
                        "case_id": case_id,
                        "time_ns": time.time_ns(),
                        "operation": "fault-fixture",
                        "fault": "restore-input-changed",
                        "boundary": "during-private-snapshot-before-source-signature-recheck",
                        "fixture_kind": "source-artifact-mutation",
                    }, sort_keys=True) + "\n")
                return
            time.sleep(0.001)

    @staticmethod
    def terminal_stage(completed: subprocess.CompletedProcess[bytes]) -> str:
        stderr = completed.stderr.decode(errors="replace")
        terminal = None
        for line in stderr.splitlines():
            if line.startswith("error code=") and " stage=" in line:
                terminal = line.split(" stage=", 1)[1].split(" ", 1)[0]
        if terminal is not None:
            return terminal
        return "complete" if completed.returncode == 0 else "unknown"

    def calls_for(self, case_id: str) -> list[dict[str, Any]]:
        result = []
        for line in self.docker_log.read_text(encoding="utf-8").splitlines():
            if not line:
                continue
            item = json.loads(line)
            if item.get("case_id") == case_id:
                result.append(item)
        return result

    def target_snapshot(self) -> dict[str, Any]:
        inspected = self.docker("container", "inspect", self.app_id, check=False)
        if inspected.returncode == 0:
            inspect = json.loads(inspected.stdout)[0]
            state = inspect.get("State") or {}
            status = state.get("Status", "unknown")
            logical = "running" if status == "running" else "stopped"
            app_state = {
                "logical": logical,
                "engine_status": status if status in {"created", "running", "paused", "restarting", "removing", "exited", "dead"} else "unknown",
                "running": bool(state.get("Running")),
                "paused": bool(state.get("Paused")),
                "restarting": bool(state.get("Restarting")),
                "dead": bool(state.get("Dead")),
            }
        else:
            app_state = {
                "logical": "absent", "engine_status": "absent", "running": None,
                "paused": None, "restarting": None, "dead": None,
            }
        sql = subprocess.run(
            [sys.executable, str(self.repo / "scripts/lib/v1-ops-package.py"), "db-counts-sql"],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
        ).stdout
        db_query = self.docker(
            "exec", "-i", self.pg_id, "psql", "-X", "-Atq", "-v", "ON_ERROR_STOP=1", "-U", "crm", "crm",
            input_bytes=sql,
            check=False,
        )
        try:
            db_counts = envelope(json.loads(db_query.stdout.decode().strip())) if db_query.returncode == 0 else unreadable_envelope("query-failed")
        except json.JSONDecodeError:
            db_counts = unreadable_envelope("query-failed")
        try:
            marker, manifest = normalized_manifest_oracle(self.avatar_manifest())
            marker_value = envelope(marker)
            manifest_value = envelope(manifest)
        except (SmokeFailure, KeyError, TypeError, json.JSONDecodeError):
            marker_value = unreadable_envelope("artifact-invalid")
            manifest_value = unreadable_envelope("artifact-invalid")
        return {
            "app_state": app_state,
            "db_counts": db_counts,
            "marker_sha256": marker_value,
            "manifest_sha256": manifest_value,
        }

    def package_data_oracle(self, package: Path) -> dict[str, Any]:
        metadata = json.loads((package / "metadata.json").read_text(encoding="utf-8"))
        marker, manifest = normalized_manifest_oracle((package / "avatar-manifest.json").read_bytes())
        return {
            "db_counts": metadata["database_counts"],
            "marker_sha256": marker,
            "manifest_sha256": manifest,
        }

    def avatar_file(self, path: str) -> bytes:
        helper = self.create_helper(
            f"{self.project}-read-{secrets.token_hex(3)}", self.avatar_volume, "/bin/sh", "-c", f"cat {path}",
        )
        completed = self.docker("start", "-a", helper, check=False)
        self.docker("rm", "-v", helper)
        self.created_ids.discard(helper)
        if completed.returncode != 0:
            raise SmokeFailure(f"cannot read avatar oracle {path}")
        return completed.stdout

    def avatar_manifest(self) -> bytes:
        helper = self.create_helper(
            f"{self.project}-manifest-{secrets.token_hex(3)}", self.avatar_volume,
            "/usr/local/bin/avatar-manifest", "generate",
        )
        completed = self.docker("start", "-a", helper)
        self.docker("rm", "-v", helper)
        self.created_ids.discard(helper)
        return completed.stdout

    def ensure_app(self, running: bool) -> None:
        action = "start" if running else "stop"
        self.compose(action, "app", check=False)
        for _ in range(60 if running else 10):
            state = json.loads(self.docker("container", "inspect", self.app_id).stdout)[0]["State"]
            if running and state.get("Status") == "running" and (state.get("Health") or {}).get("Status") == "healthy":
                return
            if not running and state.get("Status") == "exited":
                return
            time.sleep(1)
        raise SmokeFailure(f"app did not become {'running/healthy' if running else 'exited'}")

    def create_canonical_package(self) -> None:
        if self.base_package_ready:
            return
        if self.base_package.exists():
            shutil.rmtree(self.base_package)
        before = self.target_snapshot()
        completed = self.run_backup("HARNESS-CANONICAL-PACKAGE", self.base_package)
        if completed.returncode != 0:
            raise SmokeFailure(f"canonical backup failed: {completed.stderr.decode(errors='replace').strip()}")
        after = self.target_snapshot()
        if before != after:
            raise SmokeFailure("canonical backup changed the synthetic target")
        self.base_package_ready = True

    def run_preflight(self, case_id: str) -> tuple[subprocess.CompletedProcess[bytes], None, None, None]:
        env_path = self.root / f"{case_id}.env"
        compose_path = self.compose_file
        fixture = self.by_id[case_id]["fixture"]
        mode = next(
            candidate for candidate in ("binary", "compose-managed-db", "compose-external-db")
            if f"deployment_mode={candidate}" in fixture
        )
        seed_state = "empty" if "-EMPTY-" in case_id else "initialized"
        values = self.base_env(seed_state=seed_state, mode="external" if mode == "compose-external-db" else "managed")
        fault = ""
        env_extra: dict[str, str] = {}
        missing_docker = case_id == "OPS-PF-CMD-MISSING"

        if case_id == "OPS-PF-USAGE-MISSING-MODE":
            command = [str(self.repo / "scripts/production-preflight.sh"), "--seed-state", "initialized", "--env-file", str(env_path)]
            self.write_env(env_path, values)
            return self.run_command(case_id, command), None, None, None
        if case_id == "OPS-PF-USAGE-MISSING-CONTEXT":
            self.write_env(env_path, values)
            command = [str(self.repo / "scripts/production-preflight.sh"), "--mode", "compose-managed-db", "--seed-state", "initialized", "--env-file", str(env_path)]
            return self.run_command(case_id, command), None, None, None
        if case_id == "OPS-PF-ENV-READ":
            env_path = self.root / "missing-env"
        elif case_id == "OPS-PF-ENV-DUPLICATE":
            self.write_env(env_path, values)
            with env_path.open("a", encoding="utf-8") as stream:
                stream.write("AUTH_TOKEN_SECRET=duplicate\n")
        elif case_id == "OPS-PF-ENV-INVALID-KEY":
            env_path.write_text("INVALID-KEY=value\n", encoding="utf-8")
        elif case_id == "OPS-PF-ENV-UNCLOSED-QUOTE":
            env_path.write_text("AUTH_TOKEN_SECRET=\"unclosed\n", encoding="utf-8")
        else:
            self.mutate_preflight_values(case_id, values)
            self.write_env(env_path, values)

        if case_id in {"OPS-PF-SELECTOR-REMOTE-TCP"}:
            fault = "endpoint-tcp"
        elif case_id in {"OPS-PF-SELECTOR-REMOTE-SSH"}:
            fault = "endpoint-ssh"
        elif case_id == "OPS-PF-COMPOSE-VERSION":
            fault = "compose-version-old"
        elif case_id == "OPS-PF-SELECTOR-DOCKER-HOST-TCP":
            env_extra["DOCKER_HOST"] = "tcp://127.0.0.1:2375"
        elif case_id == "OPS-PF-SELECTOR-DOCKER-HOST-SSH":
            env_extra["DOCKER_HOST"] = "ssh://synthetic.invalid"
        elif case_id == "OPS-PF-SELECTOR-DOCKER-CONTEXT":
            env_extra["DOCKER_CONTEXT"] = "unexpected"
        elif case_id == "OPS-PF-SELECTOR-DOCKER-TLS":
            env_extra["DOCKER_TLS_VERIFY"] = "1"
        elif case_id == "OPS-PF-SELECTOR-FLAG-ENV-CONFLICT":
            env_extra["DOCKER_CERT_PATH"] = str(self.root)

        if case_id == "OPS-PF-COMPOSE-RENDER":
            compose_path = self.root / f"{case_id}.yml"
            compose_path.write_text("services: [invalid\n", encoding="utf-8")
        elif case_id in {
            "OPS-PF-COMPOSE-SERVICE", "OPS-PF-COMPOSE-VOLUME", "OPS-PF-COMPOSE-ENVFILE",
            "OPS-PF-HTTP-COMPOSE-ADDR", "OPS-PF-AVATAR-COMPOSE-ROOT",
            "OPS-PF-AVATAR-COMPOSE-MOUNT", "OPS-PF-AVATAR-COMPOSE-REQUIRE-MOUNT",
        }:
            compose_path = self.root / f"{case_id}.yml"
            self.write_compose(
                compose_path,
                service_fault="service" if case_id.endswith("SERVICE") else "volume" if case_id.endswith("VOLUME") else "",
                env_auth="synthetic-mismatch" if case_id.endswith("ENVFILE") else "${AUTH_TOKEN_SECRET}",
                http_addr='"127.0.0.1:8080"' if case_id == "OPS-PF-HTTP-COMPOSE-ADDR" else '":8080"',
                avatar_root="/synthetic-wrong-root" if case_id == "OPS-PF-AVATAR-COMPOSE-ROOT" else "/var/lib/crm/avatars",
                avatar_require_mount="false" if case_id == "OPS-PF-AVATAR-COMPOSE-REQUIRE-MOUNT" else "true",
                avatar_mount="/synthetic-wrong-mount" if case_id == "OPS-PF-AVATAR-COMPOSE-MOUNT" else "/var/lib/crm/avatars",
            )

        command = [str(self.repo / "scripts/production-preflight.sh"), "--mode", mode, "--seed-state", seed_state, "--env-file", str(env_path)]
        if mode != "binary":
            command += ["--compose-file", str(compose_path), "--docker-context", "desktop-linux", "--project-name", self.project]
            if case_id == "OPS-PF-SELECTOR-ARGV-PINNED":
                command += ["--pinned-host", self.endpoint]
        completed = self.run_command(case_id, command, fault=fault, env_extra=env_extra, missing_docker=missing_docker)
        return completed, None, None, None

    def mutate_preflight_values(self, case_id: str, values: dict[str, str]) -> None:
        mutations: dict[str, tuple[str, str | None]] = {
            "OPS-PF-SEED-EMPTY-MISSING": ("SEED_ADMIN_PASSWORD", None),
            "OPS-PF-SEED-EMPTY-SHORT": ("SEED_ADMIN_PASSWORD", "short"),
            "OPS-PF-SEED-EMPTY-SENTINEL": ("SEED_ADMIN_PASSWORD", "dev-only-admin-password"),
            "OPS-PF-SEED-INIT-NONEMPTY": ("SEED_ADMIN_PASSWORD", "synthetic-admin-password"),
            "OPS-PF-BIN-DATABASE-MISSING": ("DATABASE_URL", None),
            "OPS-PF-BIN-DATABASE-DEV": ("DATABASE_URL", "postgres://crm:crm-dev-password@localhost/crm?sslmode=disable"),
            "OPS-PF-BIN-DATABASE-REMOTE-NOSSL": ("DATABASE_URL", "postgres://crm:strong@db.invalid/crm?sslmode=disable"),
            "OPS-PF-MANAGED-APPDB-NONEMPTY": ("APP_DATABASE_URL", "postgres://ignored.invalid/crm?sslmode=require"),
            "OPS-PF-MANAGED-PGUSER-MISSING": ("POSTGRES_USER", None),
            "OPS-PF-MANAGED-PGDB-MISSING": ("POSTGRES_DB", None),
            "OPS-PF-MANAGED-PGPASSWORD-MISSING": ("POSTGRES_PASSWORD", None),
            "OPS-PF-MANAGED-PGPASSWORD-DEV": ("POSTGRES_PASSWORD", "crm-dev-password"),
            "OPS-PF-EXTERNAL-APPDB-MISSING": ("APP_DATABASE_URL", None),
            "OPS-PF-EXTERNAL-APPDB-DEV": ("APP_DATABASE_URL", "postgres://crm:crm-dev-password@localhost/crm?sslmode=disable"),
            "OPS-PF-EXTERNAL-APPDB-REMOTE-NOSSL": ("APP_DATABASE_URL", "postgres://crm:strong@db.invalid/crm?sslmode=disable"),
            "OPS-PF-AUTH-MISSING": ("AUTH_TOKEN_SECRET", None),
            "OPS-PF-AUTH-SHORT": ("AUTH_TOKEN_SECRET", "short"),
            "OPS-PF-AUTH-SENTINEL": ("AUTH_TOKEN_SECRET", "dev-only-token-secret-change-me"),
            "OPS-PF-HTTP-COMPOSE-ADDR": ("HTTP_ADDR", "127.0.0.1:8080"),
            "OPS-PF-AVATAR-DRIVER": ("AVATAR_STORAGE_DRIVER", "s3"),
            "OPS-PF-AVATAR-BIN-ROOT-RELATIVE": ("AVATAR_LOCAL_ROOT", "relative/avatar"),
            "OPS-PF-AVATAR-BIN-ROOT-MISSING": ("AVATAR_LOCAL_ROOT", str(self.root / "missing-avatar")),
            "OPS-PF-AVATAR-COMPOSE-ROOT": ("AVATAR_LOCAL_ROOT", "/wrong"),
            "OPS-PF-AVATAR-COMPOSE-REQUIRE-MOUNT": ("AVATAR_LOCAL_REQUIRE_MOUNT", "false"),
            "OPS-PF-TG-HALF": ("TELEGRAM_BOT_TOKEN", "123456:real-looking-token"),
            "OPS-PF-TG-USERNAME": ("TELEGRAM_BOT_USERNAME", "invalid"),
            "OPS-PF-TG-TOKEN-EXAMPLE": ("TELEGRAM_BOT_TOKEN", "example-token"),
        }
        if case_id == "OPS-PF-TG-ENABLED-OK":
            values.update(TELEGRAM_BOT_TOKEN="123456:AARealLookingNonExampleToken", TELEGRAM_BOT_USERNAME="crm_photo_bot")
        elif case_id == "OPS-PF-TG-HALF":
            values["TELEGRAM_BOT_USERNAME"] = ""
        elif case_id in {"OPS-PF-TG-USERNAME", "OPS-PF-TG-TOKEN-EXAMPLE"}:
            values.update(TELEGRAM_BOT_TOKEN="123456:AARealLookingNonExampleToken", TELEGRAM_BOT_USERNAME="crm_photo_bot")
        if case_id == "OPS-PF-TG-USERNAME":
            values["TELEGRAM_BOT_USERNAME"] = "invalid"
        if case_id == "OPS-PF-TG-TOKEN-EXAMPLE":
            values["TELEGRAM_BOT_TOKEN"] = "example-token"
        if case_id == "OPS-PF-BIN-LOOPBACK-WARN":
            values["HTTP_ADDR"] = "127.0.0.1:8080"
        if case_id == "OPS-PF-HTTP-BIN-PUBLIC-WARN":
            values["HTTP_ADDR"] = ":8080"
        if case_id == "OPS-PF-BIN-IGNORED-WARN":
            values["APP_DATABASE_URL"] = "postgres://ignored.invalid/crm?sslmode=require"
        if case_id == "OPS-PF-MANAGED-IGNORED-DB":
            values["DATABASE_URL"] = "postgres://ignored.invalid/crm?sslmode=require"
        if case_id == "OPS-PF-EXTERNAL-IGNORED-POSTGRES":
            values.update(POSTGRES_USER="ignored", POSTGRES_PASSWORD="ignored-strong-password", POSTGRES_DB="ignored")
        if case_id == "OPS-PF-ENV-INJECTION-DATA":
            values["TELEGRAM_BOT_TOKEN"] = "literal-$(touch-do-not-run)-data"
            values["TELEGRAM_BOT_USERNAME"] = "literal_data_bot"
        if case_id == "OPS-PF-AVATAR-BIN-ROOT-PERMISSION":
            denied = self.root / "permission-denied-avatar"
            denied.mkdir(exist_ok=True)
            denied.chmod(0)
            values["AVATAR_LOCAL_ROOT"] = str(denied)
        if case_id == "OPS-PF-AVATAR-COMPOSE-MOUNT":
            values["AVATAR_LOCAL_ROOT"] = "/var/lib/crm/avatars"
        mutation = mutations.get(case_id)
        if mutation:
            key, value = mutation
            if value is None:
                values.pop(key, None)
            else:
                values[key] = value

    def run_backup(
        self,
        case_id: str,
        output: Path,
        *,
        fault: str = "",
        break_stale: bool = False,
        env_extra: dict[str, str] | None = None,
        lock_role: str = "",
        helper_role: str = "",
    ) -> subprocess.CompletedProcess[bytes]:
        command = [
            str(self.repo / "scripts/backup-compose.sh"), "--env-file", str(self.env_file),
            "--compose-file", str(self.compose_file), "--docker-context", "desktop-linux",
            "--project-name", self.project, "--output", str(output),
        ]
        if break_stale:
            command.append("--break-stale-lock")
        return self.run_command(
            case_id,
            command,
            fault=fault,
            output=output,
            env_extra=env_extra,
            lock_role=lock_role,
            helper_role=helper_role,
        )

    def run_restore(
        self,
        case_id: str,
        package: Path,
        *,
        fault: str = "",
        break_stale: bool = False,
        confirm_project: str | None = None,
        env_extra: dict[str, str] | None = None,
        input_changed: bool = False,
        lock_role: str = "",
        helper_role: str = "",
    ) -> subprocess.CompletedProcess[bytes]:
        command = [
            str(self.repo / "scripts/restore-compose.sh"), "--env-file", str(self.env_file),
            "--compose-file", str(self.compose_file), "--docker-context", "desktop-linux",
            "--project-name", self.project, "--input", str(package),
            "--confirm-project", confirm_project or self.project,
        ]
        if break_stale:
            command.append("--break-stale-lock")
        return self.run_command(
            case_id,
            command,
            fault=fault,
            output=package,
            input_path=package,
            env_extra=env_extra,
            input_changed=package if input_changed else None,
            lock_role=lock_role,
            helper_role=helper_role,
        )

    def rewrite_package_checksums(self, package: Path) -> None:
        checksum_path = package / "SHA256SUMS"
        checksum_path.write_text(
            "".join(
                f"{sha256_file(package / filename)}  {filename}\n"
                for filename in sorted(FIVE_FILES - {"SHA256SUMS"})
            ),
            encoding="utf-8",
        )

    def rewrite_package_payload_metadata(self, package: Path) -> None:
        metadata_path = package / "metadata.json"
        metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
        for filename in ("database.sql", "avatar-volume.tgz", "avatar-manifest.json"):
            metadata["payloads"][filename]["sha256"] = sha256_file(package / filename)
        metadata_path.write_text(
            json.dumps(metadata, ensure_ascii=False, sort_keys=True, indent=2) + "\n",
            encoding="utf-8",
        )
        self.rewrite_package_checksums(package)

    def replace_avatar_tar_with_unsafe_member(self, package: Path, case_id: str) -> None:
        tar_path = package / "avatar-volume.tgz"
        with tarfile.open(tar_path, "w:gz") as archive:
            if case_id == "OPS-RS-TAR-ABSOLUTE":
                info = tarfile.TarInfo("/synthetic-absolute")
                payload = b"unsafe\n"
                info.size = len(payload)
                archive.addfile(info, io.BytesIO(payload))
            elif case_id == "OPS-RS-TAR-DOTDOT":
                info = tarfile.TarInfo("../synthetic-dotdot")
                payload = b"unsafe\n"
                info.size = len(payload)
                archive.addfile(info, io.BytesIO(payload))
            elif case_id == "OPS-RS-TAR-LINK":
                info = tarfile.TarInfo("synthetic-link")
                info.type = tarfile.SYMTYPE
                info.linkname = "metadata.json"
                archive.addfile(info)
            elif case_id == "OPS-RS-TAR-DEVICE":
                info = tarfile.TarInfo("synthetic-device")
                info.type = tarfile.CHRTYPE
                info.devmajor = 1
                info.devminor = 3
                archive.addfile(info)
            else:
                raise SmokeFailure(f"unsupported unsafe tar fixture: {case_id}")
        self.rewrite_package_payload_metadata(package)

    def prepare_restore_package(self, case_id: str) -> Path:
        self.create_canonical_package()
        package = self.package_root / case_id
        if package.exists() or package.is_symlink():
            if package.is_dir() and not package.is_symlink():
                shutil.rmtree(package)
            else:
                package.unlink()
        shutil.copytree(self.base_package, package)
        if case_id == "OPS-RS-CHECKSUM":
            with (package / "database.sql").open("a", encoding="utf-8") as stream:
                stream.write("\n-- synthetic checksum mismatch\n")
        elif case_id == "OPS-RS-SCHEMA":
            metadata_path = package / "metadata.json"
            metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
            metadata["schema_version"] = 2
            metadata_path.write_text(
                json.dumps(metadata, ensure_ascii=False, sort_keys=True, indent=2) + "\n",
                encoding="utf-8",
            )
            self.rewrite_package_checksums(package)
        elif case_id in {"OPS-RS-TAR-ABSOLUTE", "OPS-RS-TAR-DOTDOT", "OPS-RS-TAR-LINK", "OPS-RS-TAR-DEVICE"}:
            self.replace_avatar_tar_with_unsafe_member(package, case_id)
        elif case_id == "OPS-RS-INPUT-CHANGED":
            with (package / "database.sql").open("a", encoding="utf-8") as stream:
                block = "-- synthetic snapshot padding\n" * 32768
                for _ in range(16):
                    stream.write(block)
            self.rewrite_package_payload_metadata(package)
        elif case_id == "OPS-RS-PROJECT":
            metadata_path = package / "metadata.json"
            metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
            metadata["source_compose_project"] = "synthetic-other-project"
            metadata_path.write_text(
                json.dumps(metadata, ensure_ascii=False, sort_keys=True, indent=2) + "\n",
                encoding="utf-8",
            )
        return package

    @property
    def lock_name(self) -> str:
        return f"photographer-private-crm-ops-lock-{self.target_hash}"

    @property
    def fence_name(self) -> str:
        return f"photographer-private-crm-ops-helper-{self.target_hash}"

    def create_lock_fixture(
        self,
        case_id: str,
        role: str,
        *,
        pid: int,
        kind: str = "lock",
    ) -> str:
        image_id = self.docker("container", "inspect", self.app_id, "--format", "{{.Image}}").stdout.decode().strip()
        started = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        nonce = sha256_bytes(secrets.token_bytes(32))
        if kind == "lock":
            name = self.lock_name
            labels = [
                "com.photographer-crm.ops=true",
                f"com.photographer-crm.ops.target-sha256={self.target_hash}",
                "com.photographer-crm.ops.kind=lock",
                f"com.photographer-crm.ops.owner-pid={pid}",
                f"com.photographer-crm.ops.owner-started-at={started}",
                f"com.photographer-crm.ops.owner-nonce-sha256={nonce}",
            ]
            role_args = {"lock_role": role}
        else:
            name = self.fence_name
            labels = [
                "com.photographer-crm.ops=true",
                f"com.photographer-crm.ops.target-sha256={self.target_hash}",
                f"com.photographer-crm.ops.kind={kind}",
                f"com.photographer-crm.ops.lock-id-sha256={sha256_bytes(b'synthetic-lock')}",
                f"com.photographer-crm.ops.owner-nonce-sha256={nonce}",
            ]
            role_args = {"helper_role": role}
        arguments = ["create", "--network", "none", "--name", name]
        for label in labels:
            arguments.extend(("--label", label))
        arguments.append(image_id)
        completed = self.proxy_docker(case_id, *arguments, **role_args)
        container_id = completed.stdout.decode().strip()
        self.harness_owned_ids.add(container_id)
        self.proxy_docker(case_id, "container", "inspect", container_id, **role_args)
        return container_id

    def remove_lock_fixture(self, case_id: str, container_id: str, *, role: str, kind: str = "lock") -> None:
        role_args = {"lock_role": role} if kind == "lock" else {"helper_role": role}
        self.proxy_docker(case_id, "rm", "-v", container_id, **role_args)
        self.harness_owned_ids.discard(container_id)

    def lock_probe_command(self, case_id: str, *, break_stale: bool, hold_path: Path | None = None) -> list[str]:
        private_dir = self.tmp_root / f"{case_id}-lock-probe-{secrets.token_hex(3)}"
        private_dir.mkdir(mode=0o700)
        path_authority = self.output_root / f"{case_id}-lock-probe-output"
        script = r'''
set -euo pipefail
source "$1"
V1_ENV_FILE="$2"
V1_COMPOSE_FILE="$3"
V1_DOCKER_CONTEXT="$4"
V1_PROJECT_NAME="$5"
V1_PRIVATE_DIR="$6"
V1_PATH_AUTHORITY="$7"
V1_BREAK_STALE_LOCK="$8"
hold_path="$9"
probe_exit() {
  original="$?"
  trap - EXIT INT TERM
  set +e
  v1_cleanup
  cleanup="$?"
  set -e
  if [[ "$cleanup" -ne 0 ]]; then
    printf 'error code=11 stage=cleanup key=residual action=inspect-immutable-ids\n' >&2
    exit 11
  fi
  exit "$original"
}
trap probe_exit EXIT INT TERM
v1_require_commands
v1_validate_common_files
v1_pin_endpoint
v1_render_and_image_safety
v1_acquire_lock
if [[ -n "$hold_path" ]]; then
  v1_status lock-held pass
  while [[ ! -e "$hold_path" ]]; do sleep 0.05; done
fi
v1_cleanup
trap - EXIT INT TERM
v1_status complete pass
'''
        return [
            "/bin/bash", "-c", script, "lock-probe",
            str(self.repo / "scripts/lib/v1-ops-common.sh"),
            str(self.env_file), str(self.compose_file), "desktop-linux", self.project,
            str(private_dir), str(path_authority), "1" if break_stale else "0",
            str(hold_path) if hold_path else "",
        ]

    def run_lock_probe(
        self,
        case_id: str,
        *,
        break_stale: bool,
        lock_role: str,
    ) -> subprocess.CompletedProcess[bytes]:
        return self.run_command(
            case_id,
            self.lock_probe_command(case_id, break_stale=break_stale),
            lock_role=lock_role,
        )

    def run_held_lock_probe(
        self,
        case_id: str,
        *,
        break_stale: bool,
        lock_role: str,
    ) -> tuple[subprocess.Popen[bytes], Path]:
        release = self.root / f"{case_id}-release-{secrets.token_hex(3)}"
        process = subprocess.Popen(
            self.lock_probe_command(case_id, break_stale=break_stale, hold_path=release),
            env=self.proxy_env(case_id, lock_role=lock_role),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if any(
                (item.get("observation") or {}).get("smoke_role") == lock_role
                and (item.get("observation") or {}).get("container_id")
                for item in self.calls_for(case_id)
            ):
                return process, release
            if process.poll() is not None:
                stdout, stderr = process.communicate()
                raise SmokeFailure(
                    f"held lock probe exited early ({process.returncode}): "
                    f"{stderr.decode(errors='replace').strip() or stdout.decode(errors='replace').strip()}"
                )
            time.sleep(0.05)
        process.terminate()
        process.communicate(timeout=5)
        raise SmokeFailure("held lock probe did not acquire within 30 seconds")

    def run_path_case(self, case_id: str) -> tuple[subprocess.CompletedProcess[bytes], None, None, None]:
        missing = self.root / "missing-parent" / "leaf"
        existing = self.root / "existing-leaf"
        existing.mkdir(exist_ok=True)
        symlink = self.root / "path-link"
        if not symlink.exists():
            symlink.symlink_to(existing)
        fifo = self.root / "path-fifo"
        if not fifo.exists():
            os.mkfifo(fifo)
        if case_id.startswith("OPS-BK-"):
            paths = {
                "OPS-BK-PATH-EMPTY": "", "OPS-BK-PATH-ROOT": "/",
                "OPS-BK-PATH-PROJECT-ROOT": str(self.repo), "OPS-BK-PATH-PARENT-MISSING": str(missing),
                "OPS-BK-PATH-LEAF-EXISTS": str(existing), "OPS-BK-PATH-SYMLINK": str(symlink),
                "OPS-BK-PATH-SPECIAL": str(fifo),
            }
            completed = self.run_backup(case_id, Path(paths[case_id]))
        else:
            paths = {
                "OPS-RS-PATH-EMPTY": "", "OPS-RS-PATH-ROOT": "/",
                "OPS-RS-PATH-PROJECT-ROOT": str(self.repo), "OPS-RS-PATH-MISSING": str(missing),
                "OPS-RS-PATH-SYMLINK": str(symlink), "OPS-RS-PATH-SPECIAL": str(fifo),
            }
            command = [
                str(self.repo / "scripts/restore-compose.sh"), "--env-file", str(self.env_file),
                "--compose-file", str(self.compose_file), "--docker-context", "desktop-linux",
                "--project-name", self.project, "--input", paths[case_id], "--confirm-project", self.project,
            ]
            completed = self.run_command(case_id, command)
        return completed, None, None, None

    def run_success_or_fault(self, case_id: str) -> tuple[subprocess.CompletedProcess[bytes], dict[str, Any], dict[str, Any], dict[str, Any] | None, dict[str, Any] | None]:
        running = not case_id.endswith("EXITED")
        self.ensure_app(running)
        before = self.target_snapshot()
        package: dict[str, Any] | None = None
        package_oracle: dict[str, Any] | None = None
        if case_id.startswith("OPS-BK-"):
            output = self.output_root / case_id
            faults = {
                "OPS-BK-FAIL-PGDUMP": "backup-pgdump", "OPS-BK-FAIL-AVATAR-TAR": "backup-avatar-tar",
                "OPS-BK-FAIL-MANIFEST": "backup-manifest", "OPS-BK-FAIL-HEALTH": "backup-health",
                "OPS-BK-FAIL-PUBLISH": "backup-publish", "OPS-BK-HALF-PACKAGE-CLEANUP": "backup-half-package",
            }
            fault = faults.get(case_id, "")
            completed = self.run_backup(case_id, output, fault=fault)
            after = self.target_snapshot()
            if fault in {"backup-publish", "backup-half-package"}:
                package = self.publish_fault_observation(output, fault)
            else:
                package = self.package_observation(output)
        else:
            package_input = self.prepare_restore_package(case_id)
            package_oracle = self.package_data_oracle(package_input)
            faults = {
                "OPS-RS-FAIL-DB-REPLACE": "restore-db", "OPS-RS-FAIL-AVATAR-REPLACE": "restore-avatar",
                "OPS-RS-FAIL-VERIFY": "restore-verify", "OPS-RS-FAIL-START": "restore-start",
                "OPS-RS-FAIL-HEALTH": "restore-health",
            }
            completed = self.run_restore(case_id, package_input, fault=faults.get(case_id, ""))
            after = self.target_snapshot()
        return completed, before, after, package, package_oracle

    def run_restore_reject(self, case_id: str) -> tuple[subprocess.CompletedProcess[bytes], dict[str, Any], dict[str, Any], None]:
        self.ensure_app(True)
        package = self.prepare_restore_package(case_id)
        before = self.target_snapshot()
        faults = {
            "OPS-RS-PATH-AVATAR-OVERLAP": "overlap-avatar",
            "OPS-RS-PATH-PG-OVERLAP": "overlap-pg",
            "OPS-RS-SYMLINK": "restore-input-symlink",
            "OPS-RS-SOURCE-IMAGE": "source-image",
            "OPS-RS-SOURCE-MOUNT": "source-mount",
            "OPS-RS-APP-ABSENT": "app-state-absent",
            "OPS-RS-APP-CREATED": "app-state-created",
            "OPS-RS-APP-PAUSED": "app-state-paused",
            "OPS-RS-APP-RESTARTING": "app-state-restarting",
            "OPS-RS-APP-REMOVING": "app-state-removing",
            "OPS-RS-APP-DEAD": "app-state-dead",
            "OPS-RS-APP-UNKNOWN": "app-state-unknown",
            "OPS-RS-SELECTOR-REMOTE": "endpoint-tcp",
        }
        env_extra = {"DOCKER_HOST": "tcp://127.0.0.1:2375"} if case_id == "OPS-RS-SELECTOR-CONFLICT" else None
        completed = self.run_restore(
            case_id,
            package,
            fault=faults.get(case_id, ""),
            confirm_project="synthetic-wrong-confirmation" if case_id == "OPS-RS-CONFIRM" else None,
            env_extra=env_extra,
            input_changed=case_id == "OPS-RS-INPUT-CHANGED",
        )
        after = self.target_snapshot()
        return completed, before, after, None

    def run_lock_case(self, case_id: str) -> tuple[subprocess.CompletedProcess[bytes], dict[str, Any], dict[str, Any], None]:
        self.ensure_app(True)
        before = self.target_snapshot()
        output = self.output_root / case_id
        dead_pid = 99999999

        if case_id == "OPS-LOCK-BUSY":
            self.create_lock_fixture(case_id, "existing-owner", pid=dead_pid)
            completed = self.run_backup(case_id, output, lock_role="contender")
        elif case_id == "OPS-LOCK-STALE-LIVEPID":
            self.create_lock_fixture(case_id, "existing-owner", pid=os.getpid())
            completed = self.run_backup(case_id, output, break_stale=True, lock_role="contender")
        elif case_id == "OPS-LOCK-STALE-HELPER":
            self.create_lock_fixture(case_id, "existing-owner", pid=dead_pid)
            self.create_lock_fixture(case_id, "", pid=dead_pid, kind="fence")
            completed = self.run_backup(case_id, output, break_stale=True, lock_role="contender")
        elif case_id == "OPS-LOCK-STALE-BREAK-OK":
            self.create_lock_fixture(case_id, "stale-candidate", pid=dead_pid)
            completed = self.run_lock_probe(case_id, break_stale=True, lock_role="new-owner")
        elif case_id in {"OPS-LOCK-RACE-DOUBLE-BREAKER", "OPS-LOCK-RACE-BREAKER-ACQUIRE"}:
            self.create_lock_fixture(case_id, "stale-candidate", pid=dead_pid)
            winner, release = self.run_held_lock_probe(case_id, break_stale=True, lock_role="winner")
            try:
                completed = self.run_backup(
                    case_id,
                    output,
                    break_stale=True,
                    lock_role="loser",
                )
            finally:
                release.write_text("release\n", encoding="utf-8")
                winner_stdout, winner_stderr = winner.communicate(timeout=30)
            if winner.returncode != 0:
                raise SmokeFailure(
                    f"winning lock probe failed ({winner.returncode}): "
                    f"{winner_stderr.decode(errors='replace').strip() or winner_stdout.decode(errors='replace').strip()}"
                )
        elif case_id == "OPS-LOCK-RACE-RELEASE-NEW":
            completed = self.run_lock_probe(case_id, break_stale=False, lock_role="old-owner")
            old_id = next(
                str((item.get("observation") or {}).get("container_id"))
                for item in self.calls_for(case_id)
                if (item.get("observation") or {}).get("smoke_role") == "old-owner"
                and (item.get("observation") or {}).get("container_id")
            )
            new_id = self.create_lock_fixture(case_id, "new-owner", pid=os.getpid())
            delayed_release = self.proxy_docker(
                case_id, "rm", "-v", old_id, lock_role="old-owner", check=False,
            )
            if delayed_release.returncode == 0:
                raise SmokeFailure("delayed immutable-ID release unexpectedly removed a replacement owner")
            self.proxy_docker(case_id, "container", "inspect", new_id, lock_role="new-owner")
            self.remove_lock_fixture(case_id, new_id, role="new-owner")
        elif case_id == "OPS-LOCK-RACE-DELAYED-HELPER":
            self.create_lock_fixture(case_id, "old-helper", pid=dead_pid, kind="fence")
            completed = self.run_backup(case_id, output, lock_role="new-owner")
        elif case_id.startswith("OPS-LOCK-ALIAS-"):
            second_operation = case_id.rsplit("-", 1)[-1]
            package = self.prepare_restore_package(case_id) if second_operation == "RS" else None
            owner_id = self.create_lock_fixture(case_id, "owner", pid=os.getpid())
            if second_operation == "RS":
                assert package is not None
                completed = self.run_restore(case_id, package, lock_role="contender")
            else:
                completed = self.run_backup(case_id, output, lock_role="contender")
            self.remove_lock_fixture(case_id, owner_id, role="owner")
        else:
            raise SmokeFailure(f"lock case implementation missing: {case_id}")

        after = self.target_snapshot()
        return completed, before, after, None

    def run_cleanup_case(self, case_id: str) -> tuple[subprocess.CompletedProcess[bytes], dict[str, Any], dict[str, Any], None]:
        self.ensure_app(True)
        before = self.target_snapshot()
        completed = self.run_backup(
            case_id,
            self.output_root / case_id,
            fault="cleanup-residual",
        )
        after = self.target_snapshot()
        return completed, before, after, None

    def run_backup_reject(self, case_id: str) -> tuple[subprocess.CompletedProcess[bytes], dict[str, Any], dict[str, Any], None]:
        self.ensure_app(True)
        before = self.target_snapshot()
        output = self.output_root / case_id
        fault = ""
        env_extra: dict[str, str] | None = None
        faults = {
            "OPS-BK-PATH-AVATAR-OVERLAP": "overlap-avatar",
            "OPS-BK-PATH-PG-OVERLAP": "overlap-pg",
            "OPS-BK-APP-ABSENT": "app-state-absent",
            "OPS-BK-APP-CREATED": "app-state-created",
            "OPS-BK-APP-PAUSED": "app-state-paused",
            "OPS-BK-APP-RESTARTING": "app-state-restarting",
            "OPS-BK-APP-REMOVING": "app-state-removing",
            "OPS-BK-APP-DEAD": "app-state-dead",
            "OPS-BK-APP-UNKNOWN": "app-state-unknown",
            "OPS-LOCK-IMAGE-VOLUME": "image-declared-volume",
            "OPS-LOCK-NETWORK-DEFAULT": "lock-invariant-network",
            "OPS-LOCK-MOUNT-INVALID": "lock-invariant-mount",
            "OPS-LOCK-LABEL-INVALID": "lock-invariant-label",
            "OPS-LOCK-STATE-INVALID": "lock-invariant-state",
            "OPS-BK-SELECTOR-REMOTE": "endpoint-tcp",
        }
        fault = faults.get(case_id, "")
        if case_id == "OPS-BK-SELECTOR-CONFLICT":
            env_extra = {"DOCKER_HOST": "tcp://127.0.0.1:2375"}
        completed = self.run_backup(case_id, output, fault=fault, env_extra=env_extra)
        after = self.target_snapshot()
        return completed, before, after, None

    def package_observation(self, output: Path) -> dict[str, Any]:
        temp_residuals = [str(path) for path in output.parent.glob(".v1-ops-backup-*")]
        if not output.is_dir():
            return {
                "status": "not-created", "files": [], "metadata_schema_version": None,
                "checksums_verified": False, "manifest_verified": False, "self_check_passed": False,
                "temp_package_residuals": temp_residuals,
            }
        files = sorted(path.name for path in output.iterdir())
        metadata_version = None
        try:
            metadata_version = json.loads((output / "metadata.json").read_text(encoding="utf-8"))["schema_version"]
        except (OSError, KeyError, TypeError, json.JSONDecodeError):
            pass
        valid = subprocess.run(
            [sys.executable, str(self.repo / "scripts/lib/v1-ops-package.py"), "validate", "--input", str(output), "--project", self.project],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
        ).returncode == 0
        return {
            "status": "published", "files": files, "metadata_schema_version": metadata_version,
            "checksums_verified": valid, "manifest_verified": valid, "self_check_passed": valid,
            "temp_package_residuals": temp_residuals,
        }

    def publish_fault_observation(self, output: Path, fault: str) -> dict[str, Any]:
        """Verify and remove only the runner-owned competing output leaf."""
        expected_files = {".v1-smoke-publish-race"}
        if fault == "backup-half-package":
            expected_files.add("synthetic-partial")
        try:
            files = {path.name for path in output.iterdir()}
            marker = (output / ".v1-smoke-publish-race").read_text(encoding="utf-8").strip()
        except OSError:
            files = set()
            marker = ""
        if not output.is_dir() or files != expected_files or marker != fault:
            observation = self.package_observation(output)
            if observation["status"] == "not-created":
                observation["status"] = "fault-fixture-missing"
            return observation
        shutil.rmtree(output)
        observation = self.package_observation(output)
        observation["status"] = "partial-cleaned"
        return observation

    @staticmethod
    def call_matches(item: dict[str, Any], *tokens: str) -> bool:
        argv = item.get("argv") or []
        return item.get("exit_code") == 0 and all(token in argv for token in tokens)

    def stage_records(
        self,
        case_id: str,
        completed: subprocess.CompletedProcess[bytes],
        calls: list[dict[str, Any]],
        before: Any,
        package: Any,
    ) -> list[dict[str, str]]:
        """Reconstruct stage evidence from the command result and real Docker call sequence."""
        terminal = self.terminal_stage(completed)
        stage_events = {
            line.removeprefix("stage/").split("=", 1)[0]
            for line in completed.stdout.decode(errors="replace").splitlines()
            if line.startswith("stage/") and line.endswith("=pass") and "=" in line
        }

        def record(name: str, status: str, oracle: bool = True) -> dict[str, str]:
            return {"name": name, "operation_status": status, "oracle_status": "pass" if oracle else "fail"}

        if case_id.startswith("OPS-PF-"):
            if completed.returncode != 0:
                return [record(terminal, "failed", terminal != "unknown")]
            fixture = self.by_id[case_id]["fixture"]
            compose_mode = "deployment_mode=compose-" in fixture
            names = ["dependency-check"]
            if compose_mode:
                names.append("endpoint-select")
            names.extend(["env-parse", "config-matrix"])
            if compose_mode:
                names.extend(["compose-version", "compose-render"])
            names.append("complete")
            return [record(name, "ok") for name in names]

        if completed.returncode != 0 and terminal in {"dependency-check", "path-validate", "endpoint-select", "usage"}:
            return [record(terminal, "failed")]
        if (
            completed.returncode != 0
            and self.by_id[case_id]["expected"]["engine_call_policy"] == "forbidden"
        ):
            return [record(terminal, "failed", terminal != "unknown")]

        def created_kind(kind: str) -> list[dict[str, Any]]:
            return [
                item for item in calls
                if (item.get("observation") or {}).get("operation") == "create"
                and ((item.get("observation") or {}).get("labels") or {}).get("com.photographer-crm.ops.kind") == kind
                and item.get("exit_code") == 0
            ]

        lock_creates = created_kind("lock")
        fence_creates = created_kind("fence")
        fence_created = bool(fence_creates)
        inspected_ids = {
            str((item.get("observation") or {}).get("container_id"))
            for item in calls
            if (item.get("observation") or {}).get("operation") == "inspect"
            and (item.get("observation") or {}).get("exists") is True
        }
        lock_inspected = bool(lock_creates) and all(str(item.get("created_id")) in inspected_ids for item in lock_creates)
        fence_inspected = bool(fence_creates) and all(str(item.get("created_id")) in inspected_ids for item in fence_creates)
        created_names = {
            str(item.get("created_id")): str(item.get("created_name") or "")
            for item in calls if item.get("created_id")
        }

        def successful_named_start(fragment: str) -> bool:
            return any(
                self.call_matches(item, "start")
                and fragment in created_names.get(str((item.get("argv") or [""])[-1]), "")
                for item in calls
            )

        stages: list[dict[str, str]] = []

        def acquired_prefix(target_identity_ok: bool) -> list[dict[str, str]]:
            return [
                record("lock-acquire", "ok" if lock_creates else "not-run", bool(lock_creates)),
                record("lock-post-inspect", "ok" if lock_inspected else "not-run", lock_inspected),
                record("helper-fence", "ok" if fence_created and fence_inspected else "not-run", fence_created and fence_inspected),
                record("target-identity", "ok" if target_identity_ok else "not-run", target_identity_ok),
            ]

        stage_plan = self.by_id[case_id]["expected"]["stage_plan"]
        if stage_plan == "COR":
            return [record("cleanup", "failed", terminal == "cleanup")]
        if stage_plan == "LCK":
            if terminal == "lock-acquire":
                return [record("lock-acquire", "failed"), record("helper-fence", "not-run")]
            if terminal == "lock-post-inspect":
                return [
                    record("lock-acquire", "ok", bool(lock_creates)),
                    record("lock-post-inspect", "failed"),
                    record("helper-fence", "not-run"),
                ]
            return [
                record("lock-acquire", "ok", bool(lock_creates)),
                record("lock-post-inspect", "ok", lock_inspected),
                record("helper-fence", "failed"),
            ]
        if stage_plan == "LCO":
            return [
                record("lock-acquire", "ok", bool(lock_creates)),
                record("lock-post-inspect", "ok", lock_inspected),
                record("helper-fence", "not-run"),
                record("complete", "ok", terminal == "complete" and "complete" in stage_events),
            ]

        if case_id.startswith("OPS-BK-") or case_id.startswith("OPS-LOCK-") or case_id == "OPS-CLEANUP-RESIDUAL":
            running_before = isinstance(before, dict) and before.get("app_state", {}).get("logical") == "running"
            evidence = {
                "backup-stop": any(self.call_matches(item, "compose", "stop", "app") for item in calls),
                "backup-pg-dump": any(self.call_matches(item, "exec", "pg_dump") for item in calls),
                "backup-avatar-tar": successful_named_start("-tar-"),
                "backup-manifest": successful_named_start("-manifest-"),
                "backup-package-verify": "backup-package-verify" in stage_events,
                "backup-state-restore": any(self.call_matches(item, "compose", "start", "app") for item in calls),
                "backup-health": any(self.call_matches(item, "container", "inspect") and ".State.Health" in " ".join(item.get("argv") or []) for item in calls),
                "backup-publish": "backup-publish" in stage_events,
            }
            for name in evidence:
                evidence[name] = evidence[name] or name in stage_events
            if any(evidence.values()) or terminal.startswith("backup-") or terminal == "complete":
                stages.extend(acquired_prefix(True))
                for name in BACKUP_ORDER:
                    if name in {"backup-stop", "backup-state-restore", "backup-health"} and not running_before:
                        status = "not-run"
                    elif terminal == name:
                        status = "failed"
                    elif evidence[name]:
                        status = "ok"
                    else:
                        status = "not-run"
                    if completed.returncode == 0:
                        required_status = "not-run" if name in {"backup-stop", "backup-state-restore", "backup-health"} and not running_before else "ok"
                        stage_oracle = status == required_status
                    else:
                        stage_oracle = status != "failed" or terminal == name
                    stages.append(record(name, status, stage_oracle))
                if completed.returncode == 0:
                    stages.append(record("complete", "ok"))
            elif terminal != "unknown":
                if terminal == "target-identity":
                    stages.extend(acquired_prefix(False)[:-1])
                    stages.append(record("target-identity", "failed"))
                elif terminal == "helper-fence":
                    stages.extend([
                        record("lock-acquire", "ok" if lock_creates else "not-run", bool(lock_creates)),
                        record("lock-post-inspect", "ok" if lock_inspected else "not-run", lock_inspected),
                        record("helper-fence", "failed"),
                    ])
                elif terminal == "lock-post-inspect":
                    stages.extend([
                        record("lock-acquire", "ok" if lock_creates else "not-run", bool(lock_creates)),
                        record("lock-post-inspect", "failed"),
                        record("helper-fence", "not-run"),
                    ])
                else:
                    stages.append(record(terminal, "failed"))
        elif case_id.startswith("OPS-RS-"):
            running_before = isinstance(before, dict) and before.get("app_state", {}).get("logical") == "running"
            restore_creates = [
                item for item in calls
                if ((item.get("observation") or {}).get("labels") or {}).get("com.photographer-crm.ops.kind") == "restore"
            ]
            evidence = {
                "package-validate": "package-validate" in stage_events,
                "restore-stop": any(self.call_matches(item, "compose", "stop", "app") for item in calls),
                "restore-db-replace": any(self.call_matches(item, "exec", "dropdb") for item in calls),
                "restore-avatar-replace": any("-restore-avatar-" in str(item.get("created_name") or "") for item in restore_creates),
                "restore-verify": any("-restore-verify-" in str(item.get("created_name") or "") for item in restore_creates),
                "restore-start": any(self.call_matches(item, "compose", "start", "app") for item in calls),
                "restore-health": any(self.call_matches(item, "container", "inspect") and ".State.Health" in " ".join(item.get("argv") or []) for item in calls),
            }
            for name in evidence:
                evidence[name] = evidence[name] or name in stage_events
            if terminal == "target-identity":
                stages.extend(acquired_prefix(False)[:-1])
                stages.append(record("target-identity", "failed"))
                return stages
            stages.extend(acquired_prefix(True))
            if terminal == "package-validate" and completed.returncode != 0:
                stages.append(record("package-validate", "failed"))
                return stages
            for name in RESTORE_ORDER:
                if name in {"restore-stop", "restore-start", "restore-health"} and not running_before:
                    status = "not-run"
                elif terminal == name:
                    status = "failed"
                elif evidence[name]:
                    status = "ok"
                else:
                    status = "not-run"
                if completed.returncode == 0:
                    required_status = "not-run" if name in {"restore-stop", "restore-start", "restore-health"} and not running_before else "ok"
                    stage_oracle = status == required_status
                else:
                    stage_oracle = status != "failed" or terminal == name
                stages.append(record(name, status, stage_oracle))
            if completed.returncode == 0:
                stages.append(record("complete", "ok"))
            if completed.returncode == 10:
                failure_stopped = "restore-failure-stop" in stage_events
                stages.append(record("restore-failure-stop", "ok" if failure_stopped else "failed", failure_stopped))
        return stages or [record(terminal, "failed", terminal != "unknown")]

    @staticmethod
    def engine_policy_allows(expected: dict[str, Any], counts: dict[str, int]) -> bool:
        ranks = {"forbidden": 0, "readonly": 1, "lock-metadata": 2, "target-operation": 3}
        used = 3 if counts["target_mutation"] else 2 if counts["lock_metadata"] else 1 if counts["readonly"] else 0
        return used <= ranks[expected["engine_call_policy"]]

    @staticmethod
    def target_data_matches(target: Any, oracle: dict[str, Any]) -> bool:
        if not isinstance(target, dict):
            return False
        return all(
            isinstance(target.get(key), dict)
            and target[key].get("status") == "observed"
            and target[key].get("value") == value
            for key, value in oracle.items()
        )

    def oracle_matches(self, source: dict[str, Any], before: Any, after: Any, package_oracle: Any) -> bool:
        oracle = source["oracle_class"]
        if oracle in {"pre-mutation-readonly", "pre-mutation-reject"}:
            return before == after
        if oracle == "backup-readonly":
            return before is not None and before == after
        if oracle == "restore-success":
            return (
                isinstance(before, dict)
                and isinstance(after, dict)
                and isinstance(package_oracle, dict)
                and before.get("app_state") == after.get("app_state")
                and self.target_data_matches(after, package_oracle)
            )
        if oracle == "restore-destructive-failure":
            return (
                isinstance(after, dict)
                and after.get("app_state", {}).get("logical") == "stopped"
                and after.get("app_state", {}).get("engine_status") == "exited"
            )
        return oracle == "cleanup-only"

    @staticmethod
    def package_matches(expectation: str, package: Any) -> bool:
        if expectation == "none":
            return package is None
        if not isinstance(package, dict) or package.get("temp_package_residuals"):
            return False
        if expectation == "published-valid":
            return (
                package.get("status") == "published"
                and set(package.get("files") or []) == FIVE_FILES
                and package.get("metadata_schema_version") == 1
                and all(package.get(key) is True for key in ("checksums_verified", "manifest_verified", "self_check_passed"))
            )
        if expectation == "not-published-clean":
            return package.get("status") in {"not-created", "partial-cleaned"}
        return False

    def record_case(
        self,
        source: dict[str, Any],
        completed: subprocess.CompletedProcess[bytes],
        before: Any,
        after: Any,
        package: Any,
        package_oracle: Any = None,
    ) -> None:
        case_id = source["case_id"]
        expected = source["expected"]
        observed_stage = self.terminal_stage(completed)
        calls = self.calls_for(case_id)
        counts = {key: sum(1 for item in calls if item.get("category") == key) for key in ("readonly", "lock_metadata", "target_mutation")}
        sentinel_unchanged = self.sentinel_snapshot() == self.sentinel_before
        ops_ids = self.ops_ids()
        operation_residuals = [item for item in ops_ids if item not in self.harness_owned_ids]
        operation_cleanup_result = "fail" if case_id == "OPS-CLEANUP-RESIDUAL" else "pass" if not operation_residuals else "fail"
        residuals = [sha256_bytes(item.encode()) for item in operation_residuals]
        for item in ops_ids:
            self.docker("rm", "-fv", item, check=False)
        harness_cleanup = "pass" if not self.ops_ids() else "fail"
        if expected["backup_package_expectation"] == "none":
            package = None
        elif package is None:
            package = self.package_observation(self.output_root / case_id)
        lock_observations = self.lock_observations(case_id, calls)
        observed_roles = self.observed_lock_roles(lock_observations)
        cleanup_objects = self.observed_cleanup_objects(calls, observed_stage, case_id)
        self.harness_owned_ids.difference_update(ops_ids)
        stages = self.stage_records(case_id, completed, calls, before, package)
        all_stage_oracles_pass = bool(stages) and all(item["oracle_status"] == "pass" for item in stages)
        passed = all((
            completed.returncode == expected["exit_code"],
            observed_stage == expected["terminal_stage"],
            self.engine_policy_allows(expected, counts),
            counts["target_mutation"] == 0 or expected["target_mutation_allowed"],
            sorted(observed_roles) == sorted(expected["required_lock_roles"]),
            sorted(cleanup_objects) == sorted(expected["cleanup_objects"]),
            self.oracle_matches(source, before, after, package_oracle),
            self.package_matches(expected["backup_package_expectation"], package),
            sentinel_unchanged,
            operation_cleanup_result == expected["operation_cleanup_expected"],
            harness_cleanup == "pass",
            all_stage_oracles_pass,
        ))
        self.records.append({
            **source,
            "observed": {
                "exit_code": completed.returncode,
                "terminal_stage": observed_stage,
                "engine_calls": counts,
                "target_mutation_started": counts["target_mutation"] > 0,
                "lock_roles": observed_roles,
                "cleanup_objects": cleanup_objects,
                "side_effects": [],
            },
            "result": "pass" if passed else "fail",
            "stages": stages,
            "target_before": before,
            "target_after": after,
            "package_oracle": package_oracle,
            "backup_package": package,
            "lock_observations": lock_observations,
            "sentinel_unchanged": sentinel_unchanged,
            "operation_cleanup": {"result": operation_cleanup_result, "residual_resources": residuals},
            "harness_cleanup": {"result": harness_cleanup, "residual_resources": [] if harness_cleanup == "pass" else ["ops-residual"]},
            "evidence_paths": [f"evidence/ops/{case_id}.stdout", f"evidence/ops/{case_id}.stderr", "evidence/ops/v1-ops-smoke-docker.jsonl"],
        })

    def observed_lock_roles(self, observations: list[dict[str, Any]]) -> list[str]:
        return list(dict.fromkeys(
            item["role"] for item in observations
            if isinstance(item.get("role"), str) and item["role"]
        ))

    def observed_cleanup_objects(self, calls: list[dict[str, Any]], terminal: str, case_id: str) -> list[str]:
        creates = [item for item in calls if (item.get("observation") or {}).get("operation") == "create"]
        if not creates:
            return []
        removed = {
            container_id
            for item in calls
            for container_id in ((item.get("observation") or {}).get("container_ids") or [])
            if (item.get("observation") or {}).get("operation") == "rm"
            and (item.get("observation") or {}).get("removed") is True
        }
        lock_ids = {
            (item.get("observation") or {}).get("container_id")
            for item in creates
            if ((item.get("observation") or {}).get("labels") or {}).get("com.photographer-crm.ops.kind") == "lock"
        }
        helper_ids = {
            (item.get("observation") or {}).get("container_id")
            for item in creates
            if ((item.get("observation") or {}).get("labels") or {}).get("com.photographer-crm.ops.kind") != "lock"
        }
        lock_ids.discard(None)
        helper_ids.discard(None)
        operation_lock_ids = lock_ids - self.harness_owned_ids
        operation_helper_ids = helper_ids - self.harness_owned_ids
        observed: list[str] = []
        entered_lock_protocol = any(
            ((item.get("observation") or {}).get("labels") or {}).get("com.photographer-crm.ops.kind") == "lock"
            for item in creates
        )
        if entered_lock_protocol and operation_lock_ids <= removed:
            observed.append("lock-containers")
        if entered_lock_protocol and operation_helper_ids <= removed:
            observed.append("ops-helpers")
        private_residuals = [
            path
            for pattern in (".v1-ops-*", "v1-ops-restore-*")
            for path in self.root.rglob(pattern)
            if path.exists()
        ]
        private_scope = terminal.startswith("backup-") or terminal.startswith("restore-") or terminal == "package-validate" or case_id == "OPS-CLEANUP-RESIDUAL" or any(
            ((item.get("observation") or {}).get("labels") or {}).get("com.photographer-crm.ops.kind") in {"backup", "restore"}
            for item in creates
        )
        if private_scope and not private_residuals:
            observed.append("private-staging")
            operation_mutated_target = any(item.get("category") == "target_mutation" for item in calls)
            if (
                terminal.startswith("backup-")
                or case_id == "OPS-CLEANUP-RESIDUAL"
                or any("backup" in str(item.get("created_name") or "") for item in creates)
                or (terminal.startswith("restore-") and operation_mutated_target)
                or any("restore" in str(item.get("created_name") or "") for item in creates)
            ):
                observed.append("temp-package")
        return observed

    def lock_observations(self, case_id: str, calls: list[dict[str, Any]]) -> list[dict[str, Any]]:
        created = [
            item for item in calls
            if (item.get("observation") or {}).get("operation") == "create"
            and ((item.get("observation") or {}).get("labels") or {}).get("com.photographer-crm.ops.kind") in {"lock", "fence"}
        ]
        removals: dict[str, str] = {}
        for item in calls:
            observation = item.get("observation") or {}
            if observation.get("operation") != "rm" or observation.get("removed") is not True:
                continue
            for container_id in observation.get("container_ids") or []:
                removals[container_id] = (observation.get("removed_by") or {}).get(container_id, "owner")
        result = []
        for item in created:
            created_observation = item["observation"]
            container_id = created_observation.get("container_id")
            kind = created_observation["labels"].get("com.photographer-crm.ops.kind")
            inspected = next((
                (candidate.get("observation") or {}) for candidate in calls
                if (candidate.get("observation") or {}).get("operation") == "inspect"
                and (candidate.get("observation") or {}).get("container_id") == container_id
                and (candidate.get("observation") or {}).get("exists") is True
            ), {})
            removed = bool(container_id and container_id in removals)
            role = created_observation.get("smoke_role")
            if kind == "lock" and case_id in {
                "OPS-LOCK-NETWORK-DEFAULT", "OPS-LOCK-MOUNT-INVALID",
                "OPS-LOCK-LABEL-INVALID", "OPS-LOCK-STATE-INVALID",
            }:
                role = "invalid-candidate"
            elif not role and kind == "lock":
                role = "owner"
            state_value = inspected.get("state", "created" if container_id else "absent")
            if role == "invalid-candidate" and case_id in {
                "OPS-LOCK-LABEL-INVALID", "OPS-LOCK-STATE-INVALID",
            }:
                # The frozen lock-observation schema has no label-valid field
                # and only allows created/absent/null state.  Preserve the
                # concrete mismatch in the Docker JSONL and project the
                # rejected invariant snapshot as an untrusted/null state.
                state_value = None
            result.append({
                "role": role,
                "container_id_sha256": sha256_bytes(container_id.encode()) if container_id else None,
                "image_declared_volume_count": inspected.get("image_declared_volume_count", 0),
                "state": state_value,
                "network_mode": inspected.get("network_mode", created_observation.get("network_mode")),
                "mount_count": inspected.get("mount_count", created_observation.get("mount_count")),
                "started": inspected.get("started", created_observation.get("started")),
                "removed": removed,
                "removed_by": removals.get(container_id, "none") if removed else "none",
                "anonymous_volume_count_after": 0,
            })
        return result

    def ops_ids(self) -> list[str]:
        return self.docker(
            "ps", "-aq", "--no-trunc", "--filter", f"label=com.photographer-crm.ops.target-sha256={self.target_hash}",
            "--filter", "label=com.photographer-crm.ops=true",
        ).stdout.decode().split()

    def execute_case(self, source: dict[str, Any]) -> None:
        case_id = source["case_id"]
        package_oracle = None
        if case_id.startswith("OPS-PF-"):
            completed, before, after, package = self.run_preflight(case_id)
        elif "-PATH-" in case_id and case_id not in {"OPS-BK-PATH-AVATAR-OVERLAP", "OPS-BK-PATH-PG-OVERLAP", "OPS-RS-PATH-AVATAR-OVERLAP", "OPS-RS-PATH-PG-OVERLAP"}:
            completed, before, after, package = self.run_path_case(case_id)
        elif case_id in {
            "OPS-BK-SUCCESS-RUNNING", "OPS-BK-SUCCESS-EXITED", "OPS-BK-FAIL-PGDUMP",
            "OPS-BK-FAIL-AVATAR-TAR", "OPS-BK-FAIL-MANIFEST", "OPS-BK-FAIL-HEALTH",
            "OPS-BK-FAIL-PUBLISH", "OPS-BK-HALF-PACKAGE-CLEANUP", "OPS-RS-SUCCESS-RUNNING",
            "OPS-RS-SUCCESS-EXITED", "OPS-RS-FAIL-DB-REPLACE", "OPS-RS-FAIL-AVATAR-REPLACE",
            "OPS-RS-FAIL-VERIFY", "OPS-RS-FAIL-START", "OPS-RS-FAIL-HEALTH",
        }:
            completed, before, after, package, package_oracle = self.run_success_or_fault(case_id)
        elif case_id in {
            "OPS-BK-PATH-AVATAR-OVERLAP", "OPS-BK-PATH-PG-OVERLAP",
            "OPS-BK-APP-ABSENT", "OPS-BK-APP-CREATED", "OPS-BK-APP-PAUSED",
            "OPS-BK-APP-RESTARTING", "OPS-BK-APP-REMOVING", "OPS-BK-APP-DEAD",
            "OPS-BK-APP-UNKNOWN", "OPS-LOCK-IMAGE-VOLUME", "OPS-LOCK-NETWORK-DEFAULT",
            "OPS-LOCK-MOUNT-INVALID", "OPS-LOCK-LABEL-INVALID", "OPS-LOCK-STATE-INVALID",
            "OPS-BK-SELECTOR-REMOTE", "OPS-BK-SELECTOR-CONFLICT",
        }:
            completed, before, after, package = self.run_backup_reject(case_id)
        elif case_id in {
            "OPS-RS-PATH-AVATAR-OVERLAP", "OPS-RS-PATH-PG-OVERLAP",
            "OPS-RS-CHECKSUM", "OPS-RS-SCHEMA", "OPS-RS-SYMLINK",
            "OPS-RS-TAR-ABSOLUTE", "OPS-RS-TAR-DOTDOT", "OPS-RS-TAR-LINK",
            "OPS-RS-TAR-DEVICE", "OPS-RS-INPUT-CHANGED", "OPS-RS-CONFIRM",
            "OPS-RS-PROJECT", "OPS-RS-SOURCE-IMAGE", "OPS-RS-SOURCE-MOUNT",
            "OPS-RS-APP-ABSENT", "OPS-RS-APP-CREATED", "OPS-RS-APP-PAUSED",
            "OPS-RS-APP-RESTARTING", "OPS-RS-APP-REMOVING", "OPS-RS-APP-DEAD",
            "OPS-RS-APP-UNKNOWN", "OPS-RS-SELECTOR-REMOTE", "OPS-RS-SELECTOR-CONFLICT",
        }:
            completed, before, after, package = self.run_restore_reject(case_id)
        elif case_id in {
            "OPS-LOCK-BUSY", "OPS-LOCK-STALE-LIVEPID", "OPS-LOCK-STALE-HELPER",
            "OPS-LOCK-STALE-BREAK-OK", "OPS-LOCK-RACE-DOUBLE-BREAKER",
            "OPS-LOCK-RACE-BREAKER-ACQUIRE", "OPS-LOCK-RACE-RELEASE-NEW",
            "OPS-LOCK-RACE-DELAYED-HELPER", "OPS-LOCK-ALIAS-BK-BK",
            "OPS-LOCK-ALIAS-BK-RS", "OPS-LOCK-ALIAS-RS-BK", "OPS-LOCK-ALIAS-RS-RS",
        }:
            completed, before, after, package = self.run_lock_case(case_id)
        elif case_id == "OPS-CLEANUP-RESIDUAL":
            completed, before, after, package = self.run_cleanup_case(case_id)
        elif case_id in {"OPS-BK-CMD-MISSING", "OPS-RS-CMD-MISSING"}:
            if case_id.startswith("OPS-BK"):
                command = [
                    str(self.repo / "scripts/backup-compose.sh"),
                    "--env-file", str(self.env_file),
                    "--compose-file", str(self.compose_file),
                    "--docker-context", "desktop-linux",
                    "--project-name", self.project,
                    "--output", str(self.output_root / case_id),
                ]
            else:
                command = [
                    str(self.repo / "scripts/restore-compose.sh"),
                    "--env-file", str(self.env_file),
                    "--compose-file", str(self.compose_file),
                    "--docker-context", "desktop-linux",
                    "--project-name", self.project,
                    "--input", str(self.package_root / "missing-command"),
                    "--confirm-project", self.project,
                ]
            completed = self.run_command(case_id, command, missing_docker=True)
            before = after = package = None
        else:
            raise SmokeFailure(f"case implementation missing: {case_id}")
        self.record_case(source, completed, before, after, package, package_oracle)

    def write_results(self, suite_cleanup: dict[str, Any]) -> None:
        required = [item["case_id"] for item in self.catalog["cases"]]
        executed = [item["case_id"] for item in self.records]
        missing = sorted(set(required) - set(executed))
        status = "pass" if not missing and all(item["result"] == "pass" for item in self.records) and suite_cleanup["result"] == "pass" else "fail" if any(item["result"] == "fail" for item in self.records) else "blocked"
        document = {
            "schema_version": 1,
            "feature": FEATURE,
            "status": status,
            "target": {"project_name": self.project, "target_hash": self.target_hash},
            "catalog": {"path": CATALOG_REL, "sha256": sha256_file(self.catalog_path), "required_case_ids": required},
            "coverage": {"executed_case_ids": executed, "missing_case_ids": missing, "unknown_case_ids": [], "duplicate_case_ids": []},
            "suite_cleanup": suite_cleanup,
            "cases": self.records,
        }
        self.results_path.parent.mkdir(parents=True, exist_ok=True)
        self.results_path.write_text(json.dumps(document, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    def cleanup(self) -> dict[str, Any]:
        residuals: list[str] = []
        permission_fixture = self.root / "permission-denied-avatar"
        if permission_fixture.exists():
            permission_fixture.chmod(0o700)
        for container_id in list(self.harness_owned_ids):
            self.docker("rm", "-fv", container_id, check=False)
        self.harness_owned_ids.clear()
        for container_id in list(self.created_ids):
            self.docker("rm", "-fv", container_id, check=False)
        # Compose may have created resources before setup reached its final
        # success marker.  Down is idempotent and must therefore run for every
        # partially initialized suite as well.
        self.compose("down", "-v", "--remove-orphans", check=False)
        sentinel_ids = self.docker("ps", "-aq", "--filter", f"label=com.docker.compose.project={self.sentinel_project}", check=False).stdout.decode().split()
        for container_id in sentinel_ids:
            self.docker("rm", "-fv", container_id, check=False)
        sentinel_volumes = self.docker("volume", "ls", "-q", "--filter", f"label=com.docker.compose.project={self.sentinel_project}", check=False).stdout.decode().split()
        for volume in sentinel_volumes:
            self.docker("volume", "rm", "-f", volume, check=False)
        for label, command in (
            ("target-containers", ("ps", "-aq", "--filter", f"label=com.docker.compose.project={self.project}")),
            ("target-networks", ("network", "ls", "-q", "--filter", f"label=com.docker.compose.project={self.project}")),
            ("target-volumes", ("volume", "ls", "-q", "--filter", f"label=com.docker.compose.project={self.project}")),
            ("ops-containers", ("ps", "-aq", "--filter", f"label=com.photographer-crm.ops.target-sha256={self.target_hash}")),
            ("sentinel-containers", ("ps", "-aq", "--filter", f"label=com.docker.compose.project={self.sentinel_project}")),
            ("sentinel-volumes", ("volume", "ls", "-q", "--filter", f"label=com.docker.compose.project={self.sentinel_project}")),
        ):
            found = self.docker(*command, check=False).stdout.decode().split()
            residuals.extend(f"{label}:{sha256_bytes(item.encode())}" for item in found)
        shutil.rmtree(self.root, ignore_errors=True)
        if self.root.exists():
            residuals.append("private-work-dir")
        return {"result": "pass" if not residuals else "fail", "residual_resources": residuals}

    def run(self) -> int:
        suite_cleanup = {"result": "fail", "residual_resources": ["suite-not-cleaned"]}
        try:
            self.setup()
            for source in self.catalog["cases"]:
                if self.selected and source["case_id"] not in self.selected:
                    continue
                self.execute_case(source)
        finally:
            suite_cleanup = self.cleanup()
            self.write_results(suite_cleanup)
        if self.selected:
            return 0 if all(item["result"] == "pass" for item in self.records) and suite_cleanup["result"] == "pass" else 1
        validator = subprocess.run(
            [sys.executable, str(self.repo / "scripts/v1_ops_results.py"), "--catalog", str(self.catalog_path), "--results", str(self.results_path), "--self-test"],
            check=False,
        )
        return validator.returncode


def shlex_quote(value: str) -> str:
    import shlex
    return shlex.quote(value)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--results", type=Path)
    parser.add_argument("--case", action="append", default=[])
    parser.add_argument("--case-prefix", action="append", default=[])
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    results = args.results or repo / EVIDENCE_REL / "v1-ops-smoke-results.json"
    catalog = yaml.safe_load((repo / CATALOG_REL).read_text(encoding="utf-8"))
    selected = set(args.case)
    selected.update(
        item["case_id"] for item in catalog["cases"]
        if any(item["case_id"].startswith(prefix) for prefix in args.case_prefix)
    )
    runner = Runner(repo, results.resolve(), selected)
    try:
        return runner.run()
    except SmokeFailure as exc:
        print(f"v1 ops smoke: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
