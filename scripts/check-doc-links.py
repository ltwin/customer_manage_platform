#!/usr/bin/env python3
"""Check that repo docs only link to resolvable paths (GUAN-3 AC4).

Checks, per file:
  1. relative Markdown links `[..](path)` resolve to an existing file or directory;
  2. inline-code paths whose first segment is a top-level repo entry resolve,
     or are ignored build outputs (`git check-ignore`);
  3. every `.codestable/` occurrence is an archive reference, i.e. immediately
     preceded by `archive/codestable-final:`.

With --verify-archive, every archive reference is opened from the tag via
`git cat-file`; a `<path>#<locator>` reference is split at the first `#` and the
locator must appear in the archived blob. The working-tree `.codestable/` is
never read.
"""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
from pathlib import Path

ARCHIVE_TAG = "archive/codestable-final"
ARCHIVE_PREFIX = ARCHIVE_TAG + ":"

DEFAULT_FILES = [
    "CLAUDE.md",
    "AGENTS.md",
    "GEMINI.md",
    "README.md",
    "docs/domain/glossary.md",
    "docs/product/vision.md",
    *(
        f"docs/product/capabilities/{name}.md"
        for name in (
            "account-center",
            "creative-asset-platform",
            "creative-canvas-foundation",
            "creative-shoot-planning",
            "customer-profile",
            "dashboard",
            "order-tracking",
            "package-catalog",
            "reminder-engine",
            "schedule-calendar",
            "self-service-account-system",
            "telegram-digest",
        )
    ),
    "docs/adr/001-account-scoped-data-model.md",
    "docs/adr/002-postgresql-as-primary-store.md",
    "docs/adr/003-monolith-first-gin-openapi.md",
    "docs/adr/004-avatar-object-store-as-binary-adjunct.md",
    "docs/adr/005-separate-account-identity-and-credential.md",
    "docs/adr/006-short-lived-access-and-rotating-refresh-sessions.md",
    "docs/adr/007-claim-legacy-seed-account-in-place.md",
    "docs/adr/008-creative-explicit-key-integrity.md",
    "docs/adr/009-separate-plan-revision-from-append-only-execution-facts.md",
    *(
        f"docs/decisions/{name}.md"
        for name in (
            "2026-07-06-accountscope-fail-loud",
            "2026-07-06-api-spa-fallback-boundary",
            "2026-07-06-decision-go-uber-style-guide",
            "2026-07-06-deployment-dual-track-evidence",
            "2026-07-06-openapi-roadmap-bidirectional-check",
            "2026-07-06-telegram-smoke-chat-id",
            "2026-07-07-decision-frontend-toolchain-first-standard",
            "2026-07-07-decision-lint-gate-pre-commit",
            "2026-07-07-openapi-feature-tag-slicing",
            "2026-07-09-cross-domain-read-model",
            "2026-07-13-avatar-immutable-generation-pattern",
        )
    ),
]

LINK_RE = re.compile(r"\[[^\]]*\]\(([^)\s]+)\)")
CODE_RE = re.compile(r"`([^`\n]+)`")
CODESTABLE_RE = re.compile(r"\.codestable/")
ARCHIVE_REF_RE = re.compile(re.escape(ARCHIVE_PREFIX) + r"(\.codestable/[^\s`)'\"，。；、）]*)")
SKIP_LINK_RE = re.compile(r"^(https?:|mailto:|#)")
UNRESOLVABLE_CHARS = set("*<>{}$… ")
LINE_SUFFIX_RE = re.compile(r":\d[\d,\-]*$")


def repo_root() -> Path:
    out = subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True)
    return Path(out.strip())


def is_ignored(root: Path, rel: str) -> bool:
    return (
        subprocess.run(
            ["git", "check-ignore", "-q", "--no-index", rel],
            cwd=root,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        ).returncode
        == 0
    )


def prose_lines(text: str):
    """Yield (lineno, line, in_fence) so code checks can skip fenced blocks."""
    in_fence = False
    for lineno, line in enumerate(text.splitlines(), start=1):
        if line.lstrip().startswith("```"):
            in_fence = not in_fence
            yield lineno, line, True
            continue
        yield lineno, line, in_fence


def check_file(root: Path, rel: str, top_level: set[str]) -> list[str]:
    path = root / rel
    if not path.is_file():
        return [f"{rel}:0: file not found"]
    errors: list[str] = []
    text = path.read_text(encoding="utf-8")
    for lineno, line, in_fence in prose_lines(text):
        where = f"{rel}:{lineno}"
        for m in CODESTABLE_RE.finditer(line):
            if not line[: m.start()].endswith(ARCHIVE_PREFIX):
                errors.append(f"{where}: .codestable/ reference without {ARCHIVE_PREFIX} prefix")
        if in_fence:
            continue
        for m in LINK_RE.finditer(line):
            href = m.group(1)
            if SKIP_LINK_RE.match(href):
                continue
            target = href.split("#", 1)[0]
            if not (path.parent / target).exists():
                errors.append(f"{where}: broken link {href}")
        for m in CODE_RE.finditer(line):
            span = m.group(1).split("#", 1)[0]
            span = LINE_SUFFIX_RE.sub("", span)
            if not span or UNRESOLVABLE_CHARS & set(span):
                continue
            if span.split("/", 1)[0] not in top_level:
                continue
            if not (root / span).exists() and not is_ignored(root, span):
                errors.append(f"{where}: path not found `{span}`")
    return errors


def archive_refs(root: Path, rel: str) -> list[tuple[str, str]]:
    path = root / rel
    if not path.is_file():
        return []
    refs = []
    for lineno, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        for m in ARCHIVE_REF_RE.finditer(line):
            if UNRESOLVABLE_CHARS & set(m.group(1)):
                continue  # placeholder such as `.codestable/<path>` in a usage note
            refs.append((f"{rel}:{lineno}", m.group(1)))
    return refs


def verify_archive_ref(root: Path, ref: str) -> str | None:
    archived_path, _, locator = ref.partition("#")
    archived_path = archived_path.rstrip("/")
    obj = f"{ARCHIVE_PREFIX}{archived_path}"
    if subprocess.run(["git", "cat-file", "-e", obj], cwd=root, stderr=subprocess.DEVNULL).returncode:
        return f"archive object missing {obj}"
    if locator:
        blob = subprocess.run(["git", "cat-file", "-p", obj], cwd=root, capture_output=True, text=True)
        if locator not in blob.stdout:
            return f"locator #{locator} not found in {obj}"
    return None


def archive_names_have_no_hash(root: Path) -> str | None:
    out = subprocess.run(
        ["git", "ls-tree", "-r", "--name-only", ARCHIVE_TAG, "--", ".codestable"],
        cwd=root,
        capture_output=True,
        text=True,
    )
    if out.returncode:
        return f"cannot list {ARCHIVE_TAG}: {out.stderr.strip()}"
    bad = [name for name in out.stdout.splitlines() if "#" in name]
    if bad:
        return f"archived file names contain '#', locator split is ambiguous: {bad[:3]}"
    return None


def self_test(root: Path) -> int:
    cases = [
        (".codestable/roadmap/creative-shoot-planning/approval-report.md#round-8-share-contract", True),
        (".codestable/roadmap/account-center/approval-report.md#account-center-requirement", True),
        (".codestable/roadmap/account-center/approval-report.md#no-such-locator-guan-3", False),
        (".codestable/no-such-file-guan-3.md", False),
    ]
    failed = 0
    for ref, want_ok in cases:
        got_ok = verify_archive_ref(root, ref) is None
        if got_ok != want_ok:
            failed += 1
            print(f"self-test: {ref}: want ok={want_ok}, got ok={got_ok}", file=sys.stderr)
    if err := archive_names_have_no_hash(root):
        failed += 1
        print(f"self-test: {err}", file=sys.stderr)
    print(f"self-test: {len(cases) + 1 - failed}/{len(cases) + 1} passed")
    return 1 if failed else 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("files", nargs="*", help="repo-relative files; defaults to migrated docs + entry files")
    parser.add_argument("--verify-archive", action="store_true", help=f"open every reference from {ARCHIVE_TAG}")
    parser.add_argument("--self-test", action="store_true", help="run archive resolution self-test and exit")
    args = parser.parse_args()

    root = repo_root()
    if args.self_test:
        return self_test(root)

    files = args.files or DEFAULT_FILES
    top_level = set(os.listdir(root)) - {".git"}
    errors: list[str] = []
    for rel in files:
        errors.extend(check_file(root, rel, top_level))

    refs: list[tuple[str, str]] = []
    if args.verify_archive:
        if err := archive_names_have_no_hash(root):
            errors.append(f"{ARCHIVE_TAG}: {err}")
        for rel in files:
            refs.extend(archive_refs(root, rel))
        for where, ref in refs:
            if err := verify_archive_ref(root, ref):
                errors.append(f"{where}: {err}")

    for err in errors:
        print(err)
    summary = f"checked {len(files)} files"
    if args.verify_archive:
        summary += f", {len(refs)} archive references"
    print(f"{summary}: {'FAIL' if errors else 'ok'} ({len(errors)} errors)", file=sys.stderr)
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
