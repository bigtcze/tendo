#!/usr/bin/env python3
"""Mutation-checked end-to-end drift tests for native generators."""
from __future__ import annotations

import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
API_GO = [
    "internal/identity/httpapi/setup.gen.go",
    "internal/platform/httpx/health.gen.go",
    "internal/household/httpapi/household.gen.go",
    "internal/subject/httpapi/subject.gen.go",
    "internal/item/httpapi/item.gen.go",
]


def digest(path: Path) -> str:
    if path.is_file():
        return hashlib.sha256(path.read_bytes()).hexdigest()
    entries = []
    for item in sorted(path.rglob("*")):
        if item.is_file():
            entries.append((item.relative_to(path).as_posix(), hashlib.sha256(item.read_bytes()).hexdigest()))
    return hashlib.sha256(repr(entries).encode()).hexdigest()


class GenerationDriftTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="tendo-generation-tests-")
        self.root = Path(self.temp.name) / "repo"
        (self.root / "api").mkdir(parents=True)
        (self.root / "backend").mkdir()
        (self.root / "scripts").mkdir()
        shutil.copy2(ROOT / "api/Makefile", self.root / "api/Makefile")
        for name in ("openapi.yaml", "oapi-codegen.yaml"):
            shutil.copy2(ROOT / "api" / name, self.root / "api" / name)
        shutil.copytree(ROOT / "api/generated", self.root / "api/generated")
        shutil.copy2(ROOT / "backend/go.mod", self.root / "backend/go.mod")
        shutil.copy2(ROOT / "backend/go.sum", self.root / "backend/go.sum")
        shutil.copy2(ROOT / "backend/sqlc.yaml", self.root / "backend/sqlc.yaml")
        shutil.copytree(ROOT / "backend/internal/platform", self.root / "backend/internal/platform")
        for module in ("identity", "household", "subject", "item"):
            source = ROOT / "backend/internal" / module / "postgres"
            destination = self.root / "backend/internal" / module / "postgres"
            destination.mkdir(parents=True, exist_ok=True)
            shutil.copytree(source / "queries", destination / "queries")
            if (source / "dbgen").exists():
                shutil.copytree(source / "dbgen", destination / "dbgen")
        for artifact in API_GO:
            source = ROOT / "backend" / artifact
            destination = self.root / "backend" / artifact
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, destination)
        shutil.copy2(ROOT / "scripts/check-api-generation.sh", self.root / "scripts/check-api-generation.sh")
        shutil.copy2(ROOT / "scripts/check-sqlc.sh", self.root / "scripts/check-sqlc.sh")
        node_modules = ROOT / "api/node_modules"
        if node_modules.exists():
            (self.root / "api/node_modules").symlink_to(node_modules, target_is_directory=True)

    def tearDown(self):
        self.temp.cleanup()

    def run_api(self):
        return subprocess.run(["bash", str(self.root / "scripts/check-api-generation.sh")], cwd=self.root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

    def run_sqlc(self):
        return subprocess.run(["bash", str(self.root / "scripts/check-sqlc.sh")], cwd=self.root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

    def test_api_generation_is_clean_and_go_ts_extra_drift_fails_without_mutation(self):
        before = {path: digest(self.root / "backend" / path) for path in API_GO}
        before["api/generated"] = digest(self.root / "api/generated")
        self.assertEqual(self.run_api().returncode, 0)
        cases = [
            (self.root / "backend" / API_GO[1], lambda p: p.write_text(p.read_text() + "// deliberate drift\n")),
            (self.root / "api/generated/api.d.ts", lambda p: p.write_text(p.read_text() + "// deliberate drift\n")),
            (self.root / "api/generated/unexpected.txt", lambda p: p.write_text("extra generated artifact\n")),
        ]
        for path, mutate in cases:
            with self.subTest(path=str(path)):
                original = path.read_bytes() if path.exists() else None
                mutate(path)
                mutated_hash = digest(path.parent if path.name == "unexpected.txt" else path)
                try:
                    result = self.run_api()
                    self.assertNotEqual(result.returncode, 0, result.stdout)
                    after = path.read_bytes() if path.exists() else None
                    self.assertEqual(after, b"extra generated artifact\n" if original is None else original + b"// deliberate drift\n")
                    self.assertEqual(digest(path.parent if path.name == "unexpected.txt" else path), mutated_hash)
                finally:
                    if original is None:
                        path.unlink(missing_ok=True)
                    else:
                        path.write_bytes(original)
        self.assertEqual(before["api/generated"], digest(self.root / "api/generated"))

    def test_sqlc_generation_rejects_go_drift_and_unexpected_file_without_mutation(self):
        modules = ("identity", "household", "subject", "item")
        paths = [self.root / "backend/internal" / module / "postgres/dbgen" for module in modules]
        baseline = {path: digest(path) for path in paths}
        self.assertEqual(self.run_sqlc().returncode, 0)
        target = paths[-1] / "item.sql.go"
        original = target.read_bytes()
        target.write_bytes(original + b"// deliberate drift\n")
        mutated = digest(target)
        try:
            result = self.run_sqlc()
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertEqual(digest(target), mutated)
        finally:
            target.write_bytes(original)
        extra = paths[0] / "unexpected.generated.go"
        extra.write_text("// extra artifact\n")
        extra_digest = digest(paths[0])
        try:
            result = self.run_sqlc()
            self.assertNotEqual(result.returncode, 0, result.stdout)
            self.assertEqual(digest(paths[0]), extra_digest)
        finally:
            extra.unlink(missing_ok=True)
        self.assertEqual({path: digest(path) for path in paths}, baseline)

    def test_sqlc_wrong_version_is_rejected_before_generation(self):
        shim_dir = Path(self.temp.name) / "bin"
        shim_dir.mkdir()
        shim = shim_dir / "sqlc"
        shim.write_text("#!/bin/sh\nprintf 'v1.29.0\\n'\n")
        shim.chmod(0o755)
        env = os.environ.copy()
        env["PATH"] = str(shim_dir) + os.pathsep + env.get("PATH", "")
        result = subprocess.run(["bash", str(self.root / "scripts/check-sqlc.sh")], cwd=self.root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("expected v1.30.0, found v1.29.0", result.stdout)


if __name__ == "__main__":
    unittest.main()
