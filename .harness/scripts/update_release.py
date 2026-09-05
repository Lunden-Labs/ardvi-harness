#!/usr/bin/env python3
"""Update the host and current project from one checksum-verified release."""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request


REPOSITORY = "https://github.com/Lunden-Labs/ardvi-harness"
DEFAULT_MANIFEST = REPOSITORY + "/releases/latest/download/release-manifest.json"


def download(location: str, target: Path, limit: int, local: bool = False) -> None:
    if location.startswith("https://"):
        source = urllib.request.urlopen(location, timeout=60)
        if not source.geturl().startswith("https://"):
            source.close()
            raise RuntimeError("download redirected outside HTTPS")
    elif local and "://" not in location:
        source = open(location, "rb")
    else:
        raise RuntimeError("release downloads require HTTPS")
    with source, target.open("wb") as output:
        total = 0
        while chunk := source.read(1024 * 1024):
            total += len(chunk)
            if total > limit:
                raise RuntimeError("release download exceeds size limit")
            output.write(chunk)


def extract(archive: Path, destination: Path) -> None:
    with tarfile.open(archive, "r:gz") as bundle:
        members = []
        seen = set()
        total = 0
        for member in bundle:
            name = PurePosixPath(member.name)
            if (name.is_absolute() or ".." in name.parts or
                    not (member.isdir() or member.isfile()) or name in seen):
                raise RuntimeError(f"unsafe release archive entry: {member.name}")
            seen.add(name)
            total += member.size
            if total > 512 * 1024 * 1024 or len(seen) > 50000:
                raise RuntimeError("release archive exceeds extraction limit")
            members.append(member)
        # Only directories and regular files survive validation; never follow links.
        for member in members:
            target = destination / member.name
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with bundle.extractfile(member) as source, target.open("xb") as output:
                    shutil.copyfileobj(source, output)
                target.chmod(member.mode & 0o777)


def run(*args: str | Path, **kwargs) -> None:
    subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def project_root(explicit: str | None) -> Path | None:
    result = subprocess.run(["git", "-C", explicit or os.getcwd(), "rev-parse",
                             "--show-toplevel"], capture_output=True, text=True)
    if result.returncode == 0:
        root = Path(result.stdout.strip())
        if (root / ".ardvi/project.json").is_file() and (root / ".harness").is_dir():
            return root
    if explicit:
        raise RuntimeError("--project must name an initialized Ardvi Git project")
    return None


def source_checkout(root: Path) -> bool:
    result = subprocess.run(["git", "-C", str(root), "remote", "get-url", "origin"],
                            capture_output=True, text=True)
    remote = result.stdout.strip().removesuffix(".git").rstrip("/")
    remote = remote.replace("git@github.com:", "https://github.com/")
    if remote == REPOSITORY:
        return True
    # Forks and checkouts without an origin still contain the tracked release tooling.
    return not (root / ".harness/.managed-state.json").exists() and subprocess.run(
        ["git", "-C", str(root), "ls-files", "--error-unmatch", "--",
         "install.sh", ".github/workflows/ci.yml", ".harness/mcp/go.mod"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def update(args: argparse.Namespace, work: Path, bin_dir: Path, data_dir: Path) -> None:
    manifest_path = work / "ardvi-release.json"
    local = "://" not in args.manifest
    download(args.manifest, manifest_path, 1024 * 1024, local=local)
    manifest = json.loads(manifest_path.read_text())
    if (not isinstance(manifest, dict) or manifest.get("schema") != 1 or
            not isinstance(manifest.get("version"), str) or not manifest["version"] or
            not isinstance(manifest.get("binaries"), dict) or
            not re.fullmatch(r"[a-z0-9][a-z0-9./_-]*@sha256:[0-9a-f]{64}",
                             manifest.get("image", ""))):
        raise RuntimeError("invalid release manifest")
    machine = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
    key = f"{platform.system().lower()}_{machine}"
    binary = manifest.get("binaries", {}).get(key)
    if not isinstance(binary, dict) or not re.fullmatch(r"[0-9a-f]{64}", binary.get("sha256", "")):
        raise RuntimeError(f"release has no valid archive for {key}")
    archive = work / "release.tar.gz"
    print(f"Downloading Ardvi {manifest['version']} ({key})", flush=True)
    download(binary["url"], archive, 256 * 1024 * 1024, local=local)
    with archive.open("rb") as source:
        digest = hashlib.sha256()
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    if digest.hexdigest() != binary["sha256"]:
        raise RuntimeError("release archive SHA-256 mismatch")
    release = work / "release"
    release.mkdir()
    extract(archive, release)
    harness = release / "harness/.harness"
    for name in ("ardvi", "install.sh", "harness/.harness/scripts/bootstrap.sh",
                 "harness/.harness/scripts/manage_harness.py"):
        if not (release / name).is_file():
            raise RuntimeError(f"incomplete release: missing {name}")
    manager = harness / "scripts/manage_harness.py"
    run(sys.executable, manager, "verify", harness)
    root = project_root(args.project)
    is_source = root is not None and source_checkout(root)
    if root and (root / ".harness").is_symlink():
        raise RuntimeError("project .harness must not be a symlink")
    if root and not is_source and not args.replace_harness:
        run(sys.executable, manager, "verify", root / ".harness")

    environment = dict(os.environ, ARDVI_BIN_DIR=str(bin_dir), ARDVI_DATA_DIR=str(data_dir))
    command = ["bash", release / "install.sh", "--manifest", manifest_path]
    if args.config_dir:
        command += ["--config-dir", args.config_dir]
    if args.no_start:
        command += ["--no-start"]
    run(*command, env=environment)
    print(f"Host updated to Ardvi {manifest['version']}", flush=True)

    if root:
        backup = None
        try:
            if is_source:
                print("Source checkout: keeping tracked harness; refreshing its integration.", flush=True)
            else:
                # Keep staging on the project's filesystem for atomic renames.
                with tempfile.TemporaryDirectory(prefix=".harness-update-", dir=root) as staging:
                    candidate = Path(staging) / ".harness"
                    shutil.copytree(harness, candidate)
                    backup = Path(tempfile.mkdtemp(prefix=".harness-backup-", dir=root)) / ".harness"
                    (root / ".harness").rename(backup)
                    try:
                        candidate.rename(root / ".harness")
                    except OSError:
                        backup.rename(root / ".harness")
                        backup.parent.rmdir()
                        raise
            environment["PATH"] = str(bin_dir) + os.pathsep + environment.get("PATH", "")
            environment.pop("PROMPT", None)
            environment.pop("PROMPT_FILE", None)
            run("bash", root / ".harness/scripts/bootstrap.sh", env=environment, cwd=root)
        except Exception:
            print("Host update completed; project refresh failed. Fix the reported conflict and rerun ardvi update.", file=sys.stderr)
            if backup and backup.exists():
                print(f"Previous harness retained at {backup}", file=sys.stderr)
            raise
        if backup:
            if args.replace_harness:
                print(f"Previous harness retained at {backup}")
            else:
                shutil.rmtree(backup.parent)
        print(f"Project integration updated: {root}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", default=os.environ.get("ARDVI_RELEASE_MANIFEST_URL", DEFAULT_MANIFEST))
    parser.add_argument("--config-dir")
    parser.add_argument("--no-start", action="store_true")
    parser.add_argument("--project", help="initialized Git project (default: current project, if any)")
    parser.add_argument("--replace-harness", action="store_true", help="back up and replace a modified copied harness")
    parser.add_argument("--bin-dir", default=os.environ.get("ARDVI_BIN_DIR", str(Path.home() / ".local/bin")))
    args = parser.parse_args()
    try:
        data_dir = Path(os.environ.get("ARDVI_DATA_DIR", str(Path.home() / ".local/share/ardvi"))).resolve()
        data_dir.mkdir(parents=True, exist_ok=True)
        # ponytail: one host lock; updates are rare and share one service/binary.
        with (data_dir / ".update.lock").open("a") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise RuntimeError("another Ardvi update is running") from None
            with tempfile.TemporaryDirectory(prefix=".update-", dir=data_dir) as temporary:
                update(args, Path(temporary), Path(args.bin_dir).resolve(), data_dir)
        return 0
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, tarfile.TarError,
            subprocess.CalledProcessError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
