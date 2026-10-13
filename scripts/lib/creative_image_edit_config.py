#!/usr/bin/env python3
"""校验 API / worker 部署 env 的图片编辑开关；不执行 dotenv，不打印配置值。"""
import argparse
from pathlib import Path
import re
import sys


def read_env(path: Path) -> dict[str, str]:
    values = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        match = re.fullmatch(r"([A-Za-z_][A-Za-z0-9_]*)=(.*)", line)
        if not match or match[1] in values:
            raise ValueError("invalid dotenv")
        key, raw = match.groups()
        if raw.startswith(("'", '"')):
            if len(raw) < 2 or raw[-1] != raw[0]:
                raise ValueError("invalid dotenv quotes")
            raw = raw[1:-1]
        elif "'" in raw or '"' in raw:
            raise ValueError("invalid dotenv quotes")
        values[key] = raw.strip()
    return values


def enabled(values: dict[str, str]) -> bool:
    switch = values.get("CREATIVE_IMAGE_EDIT", "")
    if switch not in ("", "off", "on"):
        raise ValueError("invalid image edit switch")
    return switch == "on"


def check(api_path: Path, worker_path: Path | None) -> None:
    api = read_env(api_path)
    on = enabled(api)
    if worker_path is None:
        if on:
            raise ValueError("worker env required")
        return
    worker = read_env(worker_path)
    if enabled(worker) != on:
        raise ValueError("image edit switches differ")
    if on and any(v.get("CREATIVE_MEDIA_GC_MODE") != "delete" for v in (api, worker)):
        raise ValueError("deleting GC required on both sides")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api-env", required=True, type=Path)
    parser.add_argument("--worker-env", type=Path)
    args = parser.parse_args()
    try:
        check(args.api_env, args.worker_env)
    except (ValueError, OSError, UnicodeError):
        # Never echo paths, raw dotenv lines, credentials or exception text.
        print("image-edit-config: invalid or mismatched deployment configuration", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
