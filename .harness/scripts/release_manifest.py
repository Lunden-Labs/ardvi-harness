#!/usr/bin/env python3
"""Create the signed-release input manifest from pinned build outputs."""

from __future__ import annotations

import argparse
import hashlib
import json
import pathlib


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--artifacts", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    args = parser.parse_args()
    if "@sha256:" not in args.image:
        raise SystemExit("image must be pinned by digest")

    root = pathlib.Path(__file__).resolve().parent.parent
    upstreams: dict[str, str] = {}
    for line in (root / "upstreams.lock.tsv").read_text().splitlines():
        if line and not line.startswith("#"):
            fields = line.split("\t")
            upstreams[fields[0]] = fields[3]

    binaries: dict[str, dict[str, str]] = {}
    base = f"https://github.com/Lunden-Labs/ardvi-harness/releases/download/v{args.version}"
    for artifact in sorted(args.artifacts.glob("ardvi_*.tar.gz")):
        key = artifact.name.removeprefix("ardvi_").removesuffix(".tar.gz")
        binaries[key] = {
            "url": f"{base}/{artifact.name}",
            "sha256": hashlib.sha256(artifact.read_bytes()).hexdigest(),
        }
    if not binaries:
        raise SystemExit("no release archives found")

    value = {
        "schema": 1,
        "version": args.version,
        "commit": args.commit,
        "image": args.image,
        "upstreams": upstreams,
        "binaries": binaries,
    }
    args.output.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
