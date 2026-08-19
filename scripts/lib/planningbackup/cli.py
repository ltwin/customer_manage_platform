#!/usr/bin/env python3
"""CLI entry point for schema-v2 validation and restore preflight."""

from __future__ import annotations

import argparse
import json
import shutil
import sys
from pathlib import Path

from .package import ContractError, V1_FILES, V2_PAYLOADS, assemble_v2, canonical_json, load_json, publish_v2, sha256_file, snapshot_v2, validate_package, validate_v2, write_json
from .preflight import run_preflight
from .state_machine import RestoreStateMachine, StateError


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)

    validate = commands.add_parser("validate")
    validate.add_argument("--input", type=Path, required=True)
    validate.add_argument("--project", default=None)

    preflight = commands.add_parser("preflight")
    preflight.add_argument("--package", type=Path, required=True)
    preflight.add_argument("--identity", type=Path, required=True)
    preflight.add_argument("--target-planning-media", type=Path, required=True)
    preflight.add_argument("--available-bytes", type=int)
    preflight.add_argument("--restore-tool-digest")
    preflight.add_argument("--target-generation")
    preflight.add_argument("--target-compose-sha256")
    preflight.add_argument("--target-app-digest")
    preflight.add_argument("--target-postgres-digest")
    preflight.add_argument("--target-postgres-major")
    preflight.add_argument("--target-database-counts", type=Path)
    preflight.add_argument("--target-avatar-manifest", type=Path)
    preflight.add_argument("--target-regular-files", type=int)
    preflight.add_argument("--target-bytes", type=int)
    preflight.add_argument("--target-unsafe-entries", type=int)
    preflight.add_argument("--target-total-entries", type=int)
    preflight.add_argument("--output", type=Path)

    assemble = commands.add_parser("assemble-v2")
    assemble.add_argument("--output", type=Path, required=True)
    assemble.add_argument("--database", type=Path, required=True)
    assemble.add_argument("--avatar-tar", type=Path, required=True)
    assemble.add_argument("--avatar-manifest", type=Path, required=True)
    assemble.add_argument("--planning-tar", type=Path, required=True)
    assemble.add_argument("--planning-manifest", type=Path, required=True)
    assemble.add_argument("--metadata", type=Path, required=True)

    publish = commands.add_parser("publish-v2")
    publish.add_argument("--input", type=Path, required=True)
    publish.add_argument("--output", type=Path, required=True)

    snapshot = commands.add_parser("snapshot-v2")
    snapshot.add_argument("--input", type=Path, required=True)
    snapshot.add_argument("--output", type=Path, required=True)

    legacy_view = commands.add_parser("make-v1-view")
    legacy_view.add_argument("--input", type=Path, required=True)
    legacy_view.add_argument("--output", type=Path, required=True)
    legacy_view.add_argument("--project", required=True)

    state = commands.add_parser("state")
    state.add_argument("--failure-after", choices=("preflight", "stop_writes", "replace_database", "replace_avatar", "replace_planning_media", "migrate", "validate", "reopen"))

    return parser


def main(argv: list[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        if args.command == "validate":
            validate_package(args.input, args.project)
            print("planning backup package: valid")
        elif args.command == "preflight":
            target_override = None
            inventory_values = (args.target_regular_files, args.target_bytes, args.target_unsafe_entries, args.target_total_entries)
            if any(value is not None for value in inventory_values):
                if any(value is None for value in inventory_values):
                    raise ContractError("target-inventory-incomplete")
                target_override = {"regular_files": args.target_regular_files, "bytes": args.target_bytes, "unsafe_entries": args.target_unsafe_entries, "total_entries": args.target_total_entries}
            target_database_counts = load_json(args.target_database_counts) if args.target_database_counts else None
            result = run_preflight(
                args.package, args.identity, args.target_planning_media, args.available_bytes,
                args.restore_tool_digest, args.target_generation, target_override,
                args.target_compose_sha256, args.target_app_digest, args.target_postgres_digest, args.target_postgres_major,
                target_database_counts, args.target_avatar_manifest,
            )
            encoded = canonical_json(result)
            if args.output:
                args.output.write_bytes(encoded)
            else:
                sys.stdout.buffer.write(encoded)
            return 0 if result["destructive_authorized"] else 9
        elif args.command == "assemble-v2":
            metadata = json.loads(args.metadata.read_text(encoding="utf-8"))
            assemble_v2(args.output, {
                "database.sql": args.database,
                "avatar-volume.tgz": args.avatar_tar,
                "avatar-manifest.json": args.avatar_manifest,
                "planning-media-volume.tgz": args.planning_tar,
                "planning-media-manifest.json": args.planning_manifest,
            }, metadata)
            print("planning backup package v2: published")
        elif args.command == "publish-v2":
            publish_v2(args.input, args.output)
            print("planning backup package v2: published")
        elif args.command == "snapshot-v2":
            snapshot_v2(args.input, args.output)
            print("planning backup package v2: immutable snapshot ready")
        elif args.command == "make-v1-view":
            metadata = load_json(args.input / "metadata.json")
            identity = metadata["source_deployment_identity"]
            payloads = {
                filename: {"filename": filename, "sha256": sha256_file(args.input / filename)}
                for filename in ("database.sql", "avatar-volume.tgz", "avatar-manifest.json")
            }
            legacy_metadata = {
                "schema_version": 1,
                "created_at": metadata["created_at"],
                "payloads": payloads,
                "manifest_schema_version": metadata["manifest_schemas"]["avatar"],
                "source_compose_project": identity["compose_project"],
                "compose_file_sha256": metadata["compose_file_sha256"],
                "images": {"app": {"id": metadata["images"]["app"]}, "postgres": {"id": metadata["images"]["postgres"]}},
                "postgres_server_major": metadata["postgres_server_major"],
                "app_was_running": False,
                "database_counts": metadata["database_counts"],
            }
            if args.output.exists() or args.output.is_symlink():
                raise ContractError("v1-view-destination")
            args.output.mkdir(mode=0o700)
            for filename in ("database.sql", "avatar-volume.tgz", "avatar-manifest.json"):
                shutil.copy2(args.input / filename, args.output / filename)
            write_json(args.output / "metadata.json", legacy_metadata)
            (args.output / "SHA256SUMS").write_text(
                "".join(f"{sha256_file(args.output / filename)}  {filename}\n" for filename in sorted(V1_FILES - {"SHA256SUMS"})),
                encoding="utf-8",
            )
            # This call keeps the legacy exact reader as the authority for the
            # compatibility view instead of duplicating its field rules.
            validate_package(args.output, args.project)
            print("planning backup package v1 compatibility view: ready")
        elif args.command == "state":
            machine = RestoreStateMachine()
            machine.authorize(True)
            for stage in ("stop_writes", "replace_database", "replace_avatar", "replace_planning_media", "migrate", "validate", "reopen"):
                machine.advance(stage)
                if args.failure_after == stage:
                    machine.fail(f"injected-{stage}")
                    break
            if machine.stage not in {"failed_restore_stopped"}:
                machine.advance("verified")
            print(canonical_json(machine.as_dict()).decode(), end="")
            return 0 if machine.stage == "verified" else 10
    except (ContractError, StateError, OSError, ValueError, json.JSONDecodeError) as error:
        print(f"planning backup error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
