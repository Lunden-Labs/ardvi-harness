#!/usr/bin/env python3
"""Exercise the real updater/installer/bootstrap using a local release and fake service."""
import fcntl
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / ".harness/scripts/update_release.py"
spec = importlib.util.spec_from_file_location("update_release", SCRIPT)
updater = importlib.util.module_from_spec(spec)
spec.loader.exec_module(updater)


class UpdateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.home = self.base / "home"
        self.home.mkdir()
        self.bin = self.base / "custom bin"
        self.bin.mkdir()
        self.data = self.base / "custom data"
        self.log = self.base / "service.log"
        docker = self.bin / "docker"
        docker.write_text("#!/bin/sh\nexit 0\n")
        docker.chmod(0o755)
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith(("ARDVI_", "HARNESS_", "PROMPT", "PROJECT_SLUG"))}
        self.env.update(HOME=str(self.home), ARDVI_BIN_DIR=str(self.bin),
                        ARDVI_DATA_DIR=str(self.data), SERVICE_LOG=str(self.log),
                        PATH=str(self.bin) + os.pathsep + os.environ["PATH"])
        self.release = self.base / "release"
        self.harness = self.release / "harness/.harness"
        shutil.copytree(ROOT / ".harness", self.harness,
                        ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
        subprocess.run([sys.executable, str(self.harness / "scripts/manage_harness.py"),
                        "record", str(self.harness), updater.REPOSITORY, "main", "fixture"], check=True)
        shutil.copy2(ROOT / "install.sh", self.release / "install.sh")
        cli = self.release / "ardvi"
        cli.write_text('#!/bin/sh\nprintf "%s\\n" "$@" >> "$SERVICE_LOG"\nexit "${FAIL_INSTALL:-0}"\n')
        cli.chmod(0o755)
        self.archive = self.base / "release.tar.gz"
        with tarfile.open(self.archive, "w:gz") as archive:
            archive.add(self.release, arcname=".")
        self.manifest = self.base / "manifest.json"
        self.write_manifest()

    def write_manifest(self, checksum=None):
        machine = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[platform.machine()]
        self.manifest.write_text(json.dumps({
            "schema": 1, "version": "test", "commit": "fixture",
            "image": "ghcr.io/example/ardvi@sha256:" + "a" * 64, "upstreams": {},
            "binaries": {f"{platform.system().lower()}_{machine}": {
                "url": str(self.archive),
                "sha256": checksum or hashlib.sha256(self.archive.read_bytes()).hexdigest()}}}))

    def invoke(self, *args, cwd=None, success=True):
        result = subprocess.run([sys.executable, str(SCRIPT), "--manifest", str(self.manifest),
                                 "--no-start", "--config-dir", str(self.base / "config"), *args],
                                env=self.env, cwd=cwd or self.home, capture_output=True, text=True)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result.stdout + result.stderr

    def project(self):
        root = self.base / "project"
        root.mkdir()
        subprocess.run(["git", "init", "-q", str(root)], check=True)
        shutil.copytree(self.harness, root / ".harness")
        (root / ".ardvi").mkdir()
        config = {"id": "b2345678-1234-4234-8234-123456789abc", "name": "fixture", "codex_single_orchestrator": True}
        (root / ".ardvi/project.json").write_text(json.dumps(config))
        (root / "AGENTS.md").write_text("Keep project policy.\n")
        result = subprocess.run(["bash", str(root / ".harness/scripts/bootstrap.sh")],
                                cwd=root, env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return root

    def test_host_custom_paths_and_retry(self):
        for _ in range(2):
            self.invoke()
            self.assertTrue((self.bin / "ardvi").is_file())
            self.assertTrue((self.data / "harness/.harness/scripts/update_release.py").is_file())
        self.assertIn("--no-start", self.log.read_text())
        self.assertIn(str(self.base / "config"), self.log.read_text())

    def test_project_preserves_identity_policy_hooks_and_retry(self):
        root = self.project()
        identity = (root / ".ardvi/project.json").read_bytes()
        hooks_path = root / ".codex/hooks.json"
        hooks = json.loads(hooks_path.read_text())
        foreign = {"hooks": [{"type": "command", "command": "echo custom-hook"}]}
        hooks["hooks"]["SessionStart"].append(foreign)
        hooks_path.write_text(json.dumps(hooks))
        (root / "subdirectory").mkdir()
        for _ in range(2):
            self.invoke(cwd=root / "subdirectory")
            self.assertEqual((root / ".ardvi/project.json").read_bytes(), identity)
            self.assertTrue((root / "AGENTS.md").read_text().startswith("Keep project policy."))
            self.assertIn("--single-orchestrator", (root / ".codex/hooks.json").read_text())
            self.assertIn(foreign, json.loads(hooks_path.read_text())["hooks"]["SessionStart"])
            self.assertEqual(list(root.glob(".harness-backup-*")), [])

    def test_modified_harness_requires_replace_and_keeps_backup(self):
        root = self.project()
        marker = root / ".harness/local-patch"
        marker.write_text("local fix")
        self.invoke(cwd=root, success=False)
        self.assertFalse(self.log.exists())
        self.invoke("--replace-harness", cwd=root)
        backups = list(root.glob(".harness-backup-*/.harness/local-patch"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].read_text(), "local fix")
        self.assertFalse(marker.exists())

    def test_failed_install_preserves_host_and_project(self):
        root = self.project()
        (self.bin / "ardvi").write_text("old CLI")
        self.env["FAIL_INSTALL"] = "1"
        self.invoke(cwd=root, success=False)
        self.assertEqual((self.bin / "ardvi").read_text(), "old CLI")
        self.assertEqual(list(root.glob(".harness-backup-*")), [])

    def test_source_checkout_never_replaced(self):
        root = self.project()
        subprocess.run(["git", "-C", str(root), "remote", "add", "origin",
                        "git@github.com:Lunden-Labs/ardvi-harness.git"], check=True)
        (root / ".harness/.managed-state.json").unlink()
        (root / ".harness/source-marker").write_text("source")
        self.invoke("--replace-harness", cwd=root)
        self.assertEqual((root / ".harness/source-marker").read_text(), "source")
        subprocess.run(["git", "-C", str(root), "remote", "remove", "origin"], check=True)
        (root / "install.sh").write_text("# source installer")
        (root / ".github/workflows").mkdir(parents=True)
        (root / ".github/workflows/ci.yml").write_text("# source workflow")
        subprocess.run(["git", "-C", str(root), "add", "install.sh", ".github/workflows/ci.yml",
                        ".harness/mcp/go.mod"], check=True)
        self.invoke("--replace-harness", cwd=root)
        self.assertEqual((root / ".harness/source-marker").read_text(), "source")

    def test_partial_failure_keeps_backup_and_can_retry(self):
        root = self.project()
        policy = root / "AGENTS.md"
        original = policy.read_text()
        policy.write_text(original.replace("Agent ID is stable", "Changed policy"))
        output = self.invoke(cwd=root, success=False)
        self.assertIn("Host update completed; project refresh failed", output)
        self.assertEqual(len(list(root.glob(".harness-backup-*"))), 1)
        policy.write_text(original)
        self.invoke(cwd=root)

    def test_bad_checksum_and_unsafe_archives_never_execute(self):
        self.write_manifest("0" * 64)
        self.invoke(success=False)
        self.assertFalse(self.log.exists())
        for name, kind in [("../escape", tarfile.REGTYPE), ("/escape", tarfile.REGTYPE),
                           ("link", tarfile.SYMTYPE), ("hard", tarfile.LNKTYPE),
                           ("pipe", tarfile.FIFOTYPE)]:
            with tarfile.open(self.archive, "w:gz") as archive:
                entry = tarfile.TarInfo(name)
                entry.type = kind
                entry.linkname = "../outside"
                archive.addfile(entry, io.BytesIO())
            self.write_manifest()
            self.invoke(success=False)
            self.assertFalse(self.log.exists())

    def test_concurrent_update_refused(self):
        self.data.mkdir()
        with (self.data / ".update.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.assertIn("another Ardvi update", self.invoke(success=False))
        self.assertFalse(self.log.exists())

    def test_old_cli_bootstrap_uses_same_updater(self):
        curl = self.bin / "curl"
        curl.write_text('#!/bin/sh\nfor argument do destination="$argument"; done\ncp "$UPDATER_SOURCE" "$destination"\n')
        curl.chmod(0o755)
        self.env["UPDATER_SOURCE"] = str(SCRIPT)
        result = subprocess.run(["bash", str(ROOT / "upgrade.sh"), "--manifest", str(self.manifest),
                                 "--no-start"], cwd=self.home, env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue((self.bin / "ardvi").is_file())

    def test_missing_platform_and_oversized_archive_never_execute(self):
        value = json.loads(self.manifest.read_text())
        value["binaries"] = {}
        self.manifest.write_text(json.dumps(value))
        self.invoke(success=False)
        self.assertFalse(self.log.exists())
        with tarfile.open(self.archive, "w:gz") as archive:
            entry = tarfile.TarInfo("huge")
            entry.size = 512 * 1024 * 1024 + 1
            archive.addfile(entry)
        self.write_manifest()
        self.invoke(success=False)
        self.assertFalse(self.log.exists())


if __name__ == "__main__":
    unittest.main()
