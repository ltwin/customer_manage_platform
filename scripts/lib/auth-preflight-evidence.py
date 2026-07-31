#!/usr/bin/env python3
"""Build and verify the non-secret evidence contract for auth production readiness."""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import re
import sys
from datetime import datetime, timezone
from email.utils import parseaddr
from pathlib import Path
from typing import Any


COMMON_FIELDS = {
    "version",
    "generated_at",
    "build_revision",
    "schema_version",
    "config_fingerprint",
    "environment",
    "status",
    "evidence_path",
}
MAIL_FIELDS = COMMON_FIELDS | {
    "provider",
    "provider_message_id",
    "secret_version_ref",
    "recipient_ref",
}
LIVE_FIELDS = COMMON_FIELDS | {"checks"}
ARTIFACTS = {
    "mail": ("mail-accepted.json", 24 * 60 * 60),
    "monitor": ("monitor.json", 24 * 60 * 60),
    "security": ("security.json", 24 * 60 * 60),
    "rollback": ("rollback.json", 7 * 24 * 60 * 60),
    "rotation": ("rotation.json", 7 * 24 * 60 * 60),
}
BUILD_REVISION_RE = re.compile(r"[0-9a-f]{40}")
FINGERPRINT_RE = re.compile(r"[0-9a-f]{64}")
REFERENCE_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}")
PROVIDER_MESSAGE_RE = re.compile(r"[A-Za-z0-9_-]{1,128}")
RECIPIENT_REFERENCE_RE = re.compile(r"email:[0-9a-f]{12,64}")
MAX_EVIDENCE_BYTES = 64 * 1024
FUTURE_SKEW_SECONDS = 5 * 60
LIVE_MAX_AGE_SECONDS = 5 * 60


class EvidenceError(Exception):
    def __init__(self, artifact: str, classification: str) -> None:
        super().__init__(f"{artifact}:{classification}")
        self.artifact = artifact
        self.classification = classification


def parse_utc(value: Any, artifact: str) -> datetime:
    if not isinstance(value, str) or not value.endswith("Z"):
        raise EvidenceError(artifact, "generated_at")
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError as error:
        raise EvidenceError(artifact, "generated_at") from error
    if parsed.tzinfo != timezone.utc:
        raise EvidenceError(artifact, "generated_at")
    return parsed


def load_document(path: Path, artifact: str) -> dict[str, Any]:
    try:
        if not path.is_file() or path.stat().st_size > MAX_EVIDENCE_BYTES:
            raise EvidenceError(artifact, "missing")
        document = json.loads(path.read_text(encoding="utf-8"))
    except EvidenceError:
        raise
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise EvidenceError(artifact, "document") from error
    if not isinstance(document, dict):
        raise EvidenceError(artifact, "document")
    return document


def verify_common(
    document: dict[str, Any],
    *,
    artifact: str,
    allowed_fields: set[str],
    evidence_path: str,
    now: datetime,
    maximum_age_seconds: int,
    build_revision: str,
    schema_version: str,
    config_fingerprint: str,
) -> None:
    if set(document) != allowed_fields:
        raise EvidenceError(artifact, "fields")
    expected = {
        "version": 1,
        "build_revision": build_revision,
        "schema_version": schema_version,
        "config_fingerprint": config_fingerprint,
        "environment": "production",
        "status": "passed",
        "evidence_path": evidence_path,
    }
    for field, value in expected.items():
        if document.get(field) != value:
            raise EvidenceError(artifact, field)
    generated_at = parse_utc(document.get("generated_at"), artifact)
    age_seconds = (now - generated_at).total_seconds()
    if age_seconds > maximum_age_seconds:
        raise EvidenceError(artifact, "stale")
    if age_seconds < -FUTURE_SKEW_SECONDS:
        raise EvidenceError(artifact, "future")


def canonical_proxy_cidrs(raw: str) -> list[str]:
    if not raw:
        return []
    result: list[str] = []
    for item in raw.split(","):
        candidate = item.strip()
        if not candidate:
            raise ValueError("empty proxy CIDR")
        network = ipaddress.ip_network(candidate, strict=True)
        canonical = network.compressed
        if candidate != canonical:
            raise ValueError("non-canonical proxy CIDR")
        result.append(canonical)
    return sorted(result)


def sender_domain(mail_from: str) -> str:
    if "\n" in mail_from or "\r" in mail_from:
        raise ValueError("invalid sender")
    _, address = parseaddr(mail_from)
    local, separator, domain = address.rpartition("@")
    if not separator or not local or not domain:
        raise ValueError("invalid sender")
    return domain.lower().encode("idna").decode("ascii")


def fingerprint(args: argparse.Namespace) -> int:
    if not REFERENCE_RE.fullmatch(args.secret_version_ref):
        return 2
    try:
        proxy_cidrs = canonical_proxy_cidrs(args.proxy_cidrs)
        domain = sender_domain(args.mail_from)
    except ValueError:
        return 2
    canonical = {
        "cookie_profile": args.cookie_profile,
        "issuer": args.issuer,
        "limiter_kdf_version": args.limiter_kdf_version,
        "mail_driver": args.mail_driver,
        "mail_provider": args.mail_provider,
        "public_base_url": args.public_base_url,
        "proxy_cidrs": proxy_cidrs,
        "secret_version_ref": args.secret_version_ref,
        "sender_domain": domain,
    }
    payload = json.dumps(canonical, ensure_ascii=True, sort_keys=True, separators=(",", ":")).encode()
    print(hashlib.sha256(payload).hexdigest())
    return 0


def verify(args: argparse.Namespace) -> int:
    try:
        if not BUILD_REVISION_RE.fullmatch(args.build_revision):
            raise EvidenceError("context", "build_revision")
        if not FINGERPRINT_RE.fullmatch(args.config_fingerprint):
            raise EvidenceError("context", "config_fingerprint")
        if not REFERENCE_RE.fullmatch(args.secret_version_ref):
            raise EvidenceError("context", "secret_version_ref")
        now = parse_utc(args.now, "context")
        evidence_dir = Path(args.evidence_dir).resolve(strict=True)
        if not evidence_dir.is_dir():
            raise EvidenceError("context", "evidence_dir")

        for artifact, (filename, maximum_age) in ARTIFACTS.items():
            document = load_document(evidence_dir / filename, artifact)
            allowed = MAIL_FIELDS if artifact == "mail" else COMMON_FIELDS
            verify_common(
                document,
                artifact=artifact,
                allowed_fields=allowed,
                evidence_path=filename,
                now=now,
                maximum_age_seconds=maximum_age,
                build_revision=args.build_revision,
                schema_version=args.schema_version,
                config_fingerprint=args.config_fingerprint,
            )
            if artifact == "mail":
                if document.get("provider") != args.mail_provider:
                    raise EvidenceError(artifact, "provider")
                if document.get("secret_version_ref") != args.secret_version_ref:
                    raise EvidenceError(artifact, "secret_version_ref")
                if not PROVIDER_MESSAGE_RE.fullmatch(str(document.get("provider_message_id", ""))):
                    raise EvidenceError(artifact, "provider_message_id")
                if not RECIPIENT_REFERENCE_RE.fullmatch(str(document.get("recipient_ref", ""))):
                    raise EvidenceError(artifact, "recipient_ref")
            print(f"evidence/{artifact}=passed")

        live_document = load_document(Path(args.live_report), "live")
        verify_common(
            live_document,
            artifact="live",
            allowed_fields=LIVE_FIELDS,
            evidence_path="accountctl://auth/readiness",
            now=now,
            maximum_age_seconds=LIVE_MAX_AGE_SECONDS,
            build_revision=args.build_revision,
            schema_version=args.schema_version,
            config_fingerprint=args.config_fingerprint,
        )
        checks = live_document.get("checks")
        expected_checks = {
            "database": "passed",
            "limiter_schema": "passed",
            "legacy_cutover": "passed",
        }
        if checks != expected_checks:
            raise EvidenceError("live", "checks")
        print("evidence/live=passed")
        return 0
    except EvidenceError as error:
        print(
            f"evidence_error artifact={error.artifact} class={error.classification}",
            file=sys.stderr,
        )
        return 1
    except (OSError, RuntimeError, ValueError):
        print("evidence_error artifact=context class=operational", file=sys.stderr)
        return 1


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(add_help=True)
    commands = root.add_subparsers(dest="command", required=True)

    fingerprint_parser = commands.add_parser("fingerprint")
    fingerprint_parser.add_argument("--mail-driver", required=True)
    fingerprint_parser.add_argument("--mail-provider", required=True)
    fingerprint_parser.add_argument("--mail-from", required=True)
    fingerprint_parser.add_argument("--issuer", required=True)
    fingerprint_parser.add_argument("--public-base-url", required=True)
    fingerprint_parser.add_argument("--proxy-cidrs", required=True)
    fingerprint_parser.add_argument("--cookie-profile", required=True)
    fingerprint_parser.add_argument("--limiter-kdf-version", required=True)
    fingerprint_parser.add_argument("--secret-version-ref", required=True)
    fingerprint_parser.set_defaults(handler=fingerprint)

    verify_parser = commands.add_parser("verify")
    verify_parser.add_argument("--evidence-dir", required=True)
    verify_parser.add_argument("--live-report", required=True)
    verify_parser.add_argument("--build-revision", required=True)
    verify_parser.add_argument("--schema-version", required=True)
    verify_parser.add_argument("--config-fingerprint", required=True)
    verify_parser.add_argument("--now", required=True)
    verify_parser.add_argument("--mail-provider", required=True)
    verify_parser.add_argument("--secret-version-ref", required=True)
    verify_parser.set_defaults(handler=verify)
    return root


def main() -> int:
    args = parser().parse_args()
    return args.handler(args)


if __name__ == "__main__":
    raise SystemExit(main())
