#!/usr/bin/env python3
"""Pinned-endpoint Docker proxy used by the v1 ops synthetic smoke suite.

The proxy never selects an Engine of its own.  Context inspection is answered
from the runner-provided endpoint and every forwarded call is forced onto that
same local Unix endpoint.  Faults are deliberately narrow and are recorded in
the per-case JSONL evidence log.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path
from typing import Any


REAL_DOCKER = os.environ["V1_SMOKE_REAL_DOCKER"]
PINNED_HOST = os.environ["V1_SMOKE_PINNED_HOST"]
CASE_ID = os.environ.get("V1_SMOKE_CASE_ID", "harness")
FAULT = os.environ.get("V1_SMOKE_FAULT", "")
LOG_PATH = Path(os.environ["V1_SMOKE_DOCKER_LOG"])
STATE_DIR = Path(os.environ["V1_SMOKE_PROXY_STATE"])
APP_ID = os.environ.get("V1_SMOKE_APP_ID", "")
OUTPUT_PATH = os.environ.get("V1_SMOKE_OUTPUT", "")
INPUT_PATH = os.environ.get("V1_SMOKE_INPUT", "")
LOCK_ROLE = os.environ.get("V1_SMOKE_LOCK_ROLE", "")
HELPER_ROLE = os.environ.get("V1_SMOKE_HELPER_ROLE", "")


def append_log(payload: dict[str, Any]) -> None:
    payload = {"case_id": CASE_ID, "time_ns": time.time_ns(), **payload}
    LOG_PATH.parent.mkdir(parents=True, exist_ok=True)
    with LOG_PATH.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(payload, sort_keys=True) + "\n")


def object_path(container_id: str) -> Path:
    return STATE_DIR / f"container-{container_id}.json"


def save_object(container_id: str, name: str, kind: str, role: str) -> None:
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    object_path(container_id).write_text(
        json.dumps({"id": container_id, "name": name, "kind": kind, "role": role}) + "\n",
        encoding="utf-8",
    )


def load_object(container_id: str) -> dict[str, str]:
    try:
        value = json.loads(object_path(container_id).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return value if isinstance(value, dict) else {}


def option_value(args: list[str], name: str) -> str:
    try:
        return args[args.index(name) + 1]
    except (ValueError, IndexError):
        return ""


def option_values(args: list[str], name: str) -> list[str]:
    return [args[index + 1] for index, token in enumerate(args[:-1]) if token == name]


def sanitized_observation(args: list[str], code: int, stdout: bytes) -> dict[str, Any] | None:
    """Return structural Docker evidence without paths, env values, or output bodies."""
    if not args:
        return None
    if args[0] == "create":
        labels = {}
        for raw in option_values(args, "--label"):
            key, separator, value = raw.partition("=")
            if separator and key.startswith("com.photographer-crm.ops."):
                labels[key] = value
        kind = labels.get("com.photographer-crm.ops.kind", "")
        role = LOCK_ROLE if kind == "lock" else HELPER_ROLE
        return {
            "operation": "create",
            "container_id": stdout.decode(errors="replace").strip() if code == 0 else None,
            "name": option_value(args, "--name"),
            "network_mode": option_value(args, "--network") or "default",
            "mount_count": len(option_values(args, "--mount")),
            "labels": labels,
            "smoke_role": role or None,
            "started": False,
        }
    if args[:2] == ["container", "inspect"] and "--format" not in args:
        observation: dict[str, Any] = {
            "operation": "inspect",
            "container_id": args[2] if len(args) > 2 else "",
            "exists": code == 0,
        }
        if code != 0:
            return observation
        try:
            item = json.loads(stdout)[0]
            labels = item.get("Config", {}).get("Labels") or {}
            volumes = item.get("Config", {}).get("Volumes") or {}
            state = item.get("State") or {}
            observation.update({
                "network_mode": (item.get("HostConfig") or {}).get("NetworkMode"),
                "mount_count": len(item.get("Mounts") or []),
                "image_declared_volume_count": len(volumes),
                "state": state.get("Status"),
                "started": bool(state.get("Running")) or state.get("Status") not in {"created", "exited"},
                "labels": {
                    key: value for key, value in labels.items()
                    if key.startswith("com.photographer-crm.ops.")
                },
            })
        except (IndexError, KeyError, TypeError, json.JSONDecodeError):
            observation["parse_error"] = True
        return observation
    if args[0] == "rm":
        targets = [value for value in args[1:] if not value.startswith("-")]
        removed_by = {}
        for target in targets:
            role = load_object(target).get("role", "")
            if role == "stale-candidate":
                removed_by[target] = "authorized-breaker"
            elif role in {"owner", "old-owner", "new-owner", "winner", "invalid-candidate"} or not role:
                removed_by[target] = "owner"
            else:
                removed_by[target] = "none"
        return {
            "operation": "rm",
            "container_ids": targets,
            "removed": code == 0,
            "removed_by": removed_by,
        }
    return None


def normalize_args(raw: list[str]) -> list[str]:
    result: list[str] = []
    index = 0
    while index < len(raw):
        if raw[index] == "--host" and index + 1 < len(raw):
            index += 2
            continue
        if raw[index].startswith("--host="):
            index += 1
            continue
        result.append(raw[index])
        index += 1
    return result


def is_compose_mutation(args: list[str]) -> bool:
    if "compose" not in args:
        return False
    return any(token in {"stop", "start", "up", "down", "restart"} for token in args)


def category(args: list[str]) -> str:
    if not args:
        return "readonly"
    if args[:2] == ["context", "inspect"]:
        # Context inspection reads local CLI configuration; it is not an Engine call.
        return "selector"
    command = args[0]
    if command == "create":
        labels = [args[index + 1] for index, token in enumerate(args[:-1]) if token == "--label"]
        if any("com.photographer-crm.ops" in label for label in labels):
            return "lock_metadata"
    if command == "rm":
        return "lock_metadata"
    if is_compose_mutation(args):
        return "target_mutation"
    if command == "exec" and any(token in {"dropdb", "createdb", "pg_dump"} for token in args):
        return "target_mutation"
    if command in {"start", "cp"}:
        target = args[-1] if args else ""
        if command == "start" and len(args) >= 2:
            target = args[-1]
        if load_object(target):
            return "target_mutation"
    return "readonly"


def context_response(args: list[str]) -> tuple[int, bytes, bytes] | None:
    if len(args) >= 2 and args[0:2] == ["context", "inspect"]:
        if FAULT in {"endpoint-tcp", "selector-remote-tcp"}:
            value = "tcp://127.0.0.1:2375\n"
        elif FAULT in {"endpoint-ssh", "selector-remote-ssh"}:
            value = "ssh://synthetic.invalid\n"
        else:
            value = PINNED_HOST + "\n"
        return 0, value.encode(), b""
    return None


def injected_response(args: list[str]) -> tuple[int, bytes, bytes] | None:
    if FAULT == "restore-input-symlink" and args[:2] == ["context", "inspect"] and INPUT_PATH:
        package = Path(INPUT_PATH)
        artifact = package / "database.sql"
        if artifact.exists() and not artifact.is_symlink():
            artifact.unlink()
            artifact.symlink_to("metadata.json")
            append_log({
                "operation": "fault-fixture",
                "fault": FAULT,
                "boundary": "after-input-path-validation-before-package-snapshot",
                "fixture_kind": "symlink-artifact-race",
            })

    context = context_response(args)
    if context is not None:
        return context

    if FAULT == "compose-version-old" and args[:2] == ["compose", "version"]:
        return 0, b"2.23.3\n", b""

    if FAULT == "image-declared-volume" and args[:2] == ["image", "inspect"]:
        if option_value(args, "--format") == "{{json .Config.Volumes}}":
            return 0, b'{"/synthetic":{}}\n', b""

    if FAULT.startswith("app-state-") and args[:2] == ["container", "inspect"]:
        target = args[2] if len(args) > 2 else ""
        if target == APP_ID and "--format" not in args:
            status = FAULT.removeprefix("app-state-")
            if status == "absent":
                return 1, b"", b"synthetic absent app\n"
            state = {
                "Status": status,
                "Running": status == "running",
                "Paused": status == "paused",
                "Restarting": status == "restarting",
                "Dead": status == "dead",
            }
            if status == "unknown":
                state["Status"] = "synthetic-unknown"
            completed = subprocess.run(
                [REAL_DOCKER, "--host", PINNED_HOST, *args],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
            if completed.returncode != 0:
                return completed.returncode, completed.stdout, completed.stderr
            try:
                doc = json.loads(completed.stdout)
                doc[0]["State"].update(state)
            except (IndexError, KeyError, TypeError, json.JSONDecodeError):
                return 99, b"", b"synthetic app-state rewrite failed\n"
            return 0, (json.dumps(doc) + "\n").encode(), b""

    if FAULT in {"source-image", "source-mount", "cleanup-residual"} and args[:2] == ["container", "inspect"]:
        target = args[2] if len(args) > 2 else ""
        fmt = option_value(args, "--format")
        if target == APP_ID and FAULT in {"source-image", "cleanup-residual"} and fmt == "{{.Image}}":
            return 0, ("sha256:" + "f" * 64 + "\n").encode(), b""
        if target == APP_ID and FAULT == "source-mount" and "/var/lib/crm/avatars" in fmt:
            return 0, b"synthetic-wrong-avatar-volume\n", b""

    if FAULT in {"overlap-avatar", "overlap-pg"} and args[:2] == ["volume", "inspect"]:
        if option_value(args, "--format") == "{{.Mountpoint}}" and OUTPUT_PATH:
            target = args[2] if len(args) > 2 else ""
            is_avatar = "avatar" in target
            if (FAULT == "overlap-avatar" and is_avatar) or (FAULT == "overlap-pg" and not is_avatar):
                return 0, (OUTPUT_PATH + "\n").encode(), b""

    if args[:2] == ["container", "inspect"] and "--format" not in args:
        target = args[2] if len(args) > 2 else ""
        meta = load_object(target)
        if meta.get("kind") == "lock" and FAULT.startswith("lock-invariant-"):
            completed = subprocess.run(
                [REAL_DOCKER, "--host", PINNED_HOST, *args],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
            if completed.returncode != 0:
                return completed.returncode, completed.stdout, completed.stderr
            try:
                doc = json.loads(completed.stdout)
                item = doc[0]
                fault = FAULT.removeprefix("lock-invariant-")
                if fault == "network":
                    item["HostConfig"]["NetworkMode"] = "default"
                elif fault == "mount":
                    item["Mounts"] = [{"Type": "volume", "Name": "synthetic-invalid"}]
                elif fault == "label":
                    item["Config"]["Labels"]["com.photographer-crm.ops.kind"] = "invalid"
                elif fault == "state":
                    item["State"].update({"Status": "running", "Running": True})
            except (IndexError, KeyError, TypeError, json.JSONDecodeError):
                return 99, b"", b"synthetic lock rewrite failed\n"
            return 0, (json.dumps(doc) + "\n").encode(), b""

    if FAULT == "backup-pgdump" and args and args[0] == "exec" and "pg_dump" in args:
        return 1, b"", b"synthetic pg_dump failure\n"

    if args and args[0] == "start" and "-a" in args:
        target = args[-1]
        metadata = load_object(target)
        name = metadata.get("name", "")
        kind = metadata.get("kind", "")
        if FAULT == "backup-avatar-tar" and kind == "backup" and "-tar-" in name:
            return 1, b"", b"synthetic avatar tar failure\n"
        if FAULT == "backup-manifest" and kind == "backup" and "-manifest-" in name:
            return 1, b"", b"synthetic manifest failure\n"
        if FAULT == "restore-avatar" and kind == "restore" and "-avatar-" in name:
            return 1, b"", b"synthetic avatar restore failure\n"
        if FAULT == "restore-verify" and kind == "restore" and "-verify-" in name:
            return 1, b"", b"synthetic avatar verify failure\n"

    if FAULT == "restore-db" and args and args[0] == "exec" and "dropdb" in args:
        return 1, b"", b"synthetic database replacement failure\n"

    if FAULT == "restore-avatar" and args and args[0] == "cp":
        target = args[-1].split(":", 1)[0]
        metadata = load_object(target)
        if metadata.get("kind") == "restore" and "-avatar-" in metadata.get("name", ""):
            return 1, b"", b"synthetic avatar copy failure\n"

    if FAULT == "restore-start" and is_compose_mutation(args) and "start" in args:
        return 1, b"", b"synthetic app start failure\n"

    if FAULT in {"backup-health", "restore-health"} and args[:2] == ["container", "inspect"]:
        target = args[2] if len(args) > 2 else ""
        if target == APP_ID and ".State.Health" in option_value(args, "--format"):
            return 0, b"unhealthy\n", b""

    if FAULT in {"backup-publish", "backup-half-package"} and args and args[0] == "exec" and "postgres" in args and "-V" in args:
        if OUTPUT_PATH:
            output = Path(OUTPUT_PATH)
            output.mkdir(parents=False, exist_ok=True)
            (output / ".v1-smoke-publish-race").write_text(FAULT + "\n", encoding="utf-8")
            if FAULT == "backup-half-package":
                (output / "synthetic-partial").write_text("partial\n", encoding="utf-8")
            append_log({
                "operation": "fault-fixture",
                "fault": FAULT,
                "boundary": "after-initial-output-validation-before-package-verify",
                "fixture_kind": "competing-output-leaf",
            })

    if FAULT == "cleanup-residual" and args[:2] == ["container", "inspect"]:
        marker = STATE_DIR / "cleanup-verification-failure"
        target = args[2] if len(args) > 2 else ""
        if marker.exists() and marker.read_text(encoding="utf-8").strip() == target:
            return 0, b"[]\n", b""

    return None


def record_created(args: list[str], stdout: bytes) -> None:
    if not args or args[0] != "create":
        return
    container_id = stdout.decode(errors="replace").strip()
    if not container_id:
        return
    name = option_value(args, "--name")
    kind = ""
    for index, token in enumerate(args[:-1]):
        if token == "--label" and args[index + 1].startswith("com.photographer-crm.ops.kind="):
            kind = args[index + 1].split("=", 1)[1]
    role = LOCK_ROLE if kind == "lock" else HELPER_ROLE
    save_object(container_id, name, kind, role)


def main() -> int:
    args = normalize_args(sys.argv[1:])
    call_category = category(args)
    injected = injected_response(args)
    if injected is None:
        completed = subprocess.run(
            [REAL_DOCKER, "--host", PINNED_HOST, *args],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        code, stdout, stderr = completed.returncode, completed.stdout, completed.stderr
    else:
        code, stdout, stderr = injected
    if FAULT == "cleanup-residual" and code == 0 and args[:1] == ["rm"]:
        targets = [value for value in args[1:] if not value.startswith("-")]
        for target in targets:
            if load_object(target).get("kind") == "lock":
                STATE_DIR.mkdir(parents=True, exist_ok=True)
                (STATE_DIR / "cleanup-verification-failure").write_text(target + "\n", encoding="utf-8")
    if code == 0:
        record_created(args, stdout)
    append_log({
        "argv": args,
        "category": call_category,
        "exit_code": code,
        "injected": injected is not None,
        "created_id": stdout.decode(errors="replace").strip() if args[:1] == ["create"] and code == 0 else None,
        "created_name": option_value(args, "--name") if args[:1] == ["create"] and code == 0 else None,
        "stderr_excerpt": stderr.decode(errors="replace")[:2000] if code != 0 else "",
        "observation": sanitized_observation(args, code, stdout),
    })
    sys.stdout.buffer.write(stdout)
    sys.stderr.buffer.write(stderr)
    return code


if __name__ == "__main__":
    raise SystemExit(main())
