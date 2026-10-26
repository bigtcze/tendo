#!/usr/bin/env python3
"""Enforce maintainability thresholds with named, downward-only debt ratchets."""
from __future__ import annotations

import argparse
from collections import Counter
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
BASELINE = ROOT / "ci/maintainability-baseline.json"
THRESHOLDS = {
    "file_lines": {".go": 500, ".ts": 350, ".tsx": 350, ".py": 500, ".sh": 500, ".sql": 400},
    "long_lines": {"*": 0},
    "func_lines": {"*": 80},
    "func_complexity": {"*": 15},
    "func_nesting": {"*": 4},
}
MIGRATION_DIR = ("backend", "internal", "platform", "database")


def is_numbered_migration(parts: tuple[str, ...], name: str) -> bool:
    return parts[:-1] == MIGRATION_DIR and re.fullmatch(r"\d{3}_.+\.sql", name) is not None


EXCLUSIONS = (
    ("*_test.go", lambda parts, name: name.endswith("_test.go"),
     "Go test suites are excluded from structural limits; policy does not require splitting a test suite."),
    ("*.test.ts(x)", lambda parts, name: name.endswith((".test.ts", ".test.tsx")),
     "Frontend unit test suites are excluded from structural limits."),
    ("frontend/e2e/*.spec.ts", lambda parts, name: parts[:2] == ("frontend", "e2e") and name.endswith(".spec.ts"),
     "Browser E2E specifications are test suites, not production code."),
    ("scripts/test_*.py", lambda parts, name: parts[:1] == ("scripts",) and name.startswith("test_") and name.endswith(".py"),
     "Python harness test suites are excluded from structural limits."),
    ("*.gen.go", lambda parts, name: name.endswith(".gen.go"),
     "Generated Go output is not human-authored."),
    ("*/testdata/*", lambda parts, name: "testdata" in parts,
     "Fixture data and test-only source are excluded from maintainability limits."),
    ("*/dbgen/*", lambda parts, name: "dbgen" in parts,
     "Generated database code is not human-authored."),
    ("backend/internal/platform/database/NNN_*.sql", is_numbered_migration,
     "Numbered embedded database migrations are immutable schema history."),
    ("api/generated/*", lambda parts, name: parts[:2] == ("api", "generated"),
     "Generated API artifacts are not human-authored."),
)
SUFFIXES = {".go", ".ts", ".tsx", ".py", ".sh", ".sql"}
KINDS = set(THRESHOLDS)


def excluded(path: str) -> bool:
    parts = PurePosixPath(path).parts
    name = parts[-1]
    return any(predicate(parts, name) for _, predicate, _ in EXCLUSIONS)


def file_metrics(text: str) -> dict[str, int]:
    lines = text.splitlines()
    return {"file_lines": sum(bool(line.strip()) for line in lines),
            "long_lines": sum(len(line) > 160 for line in lines)}


def key(entry: dict) -> tuple[str, str, str]:
    return entry["kind"], entry["path"], entry.get("name", "")


def check_metrics(metrics: dict[tuple[str, str, str], int], baseline: list[dict]) -> list[str]:
    errors: list[str] = []
    entries: dict[tuple[str, str, str], dict] = {}
    for entry in baseline:
        entry_key = key(entry)
        if entry_key in entries:
            errors.append(f"{entry.get('path', '?')} {entry.get('kind', '?')} duplicate baseline entry")
            continue
        entries[entry_key] = entry
        if entry.get("kind") not in KINDS:
            errors.append(f"{entry.get('path', '?')} invalid baseline kind={entry.get('kind')}")
        reason = entry.get("reason")
        if not isinstance(reason, str) or not reason.strip() or reason.startswith("TODO:"):
            errors.append(f"{entry.get('path', '?')} {entry.get('name', '')} {entry.get('kind', '?')} missing reason")
        owner = entry.get("owner")
        if not isinstance(owner, str) or not owner.strip() or owner.startswith("TODO:"):
            errors.append(f"{entry.get('path', '?')} {entry.get('name', '')} {entry.get('kind', '?')} missing owner")

    for metric_key, value in sorted(metrics.items()):
        kind, path, name = metric_key
        limit = threshold(kind, path)
        exception = entries.get(metric_key)
        if exception is None:
            if value > limit:
                errors.append(f"{path}{':' + name if name else ''} {kind} measured={value} allowed={limit}")
            continue
        allowed = exception.get("allowed")
        if not isinstance(allowed, int):
            errors.append(f"{path}{':' + name if name else ''} {kind} invalid allowed value")
            continue
        if value > allowed:
            errors.append(f"{path}{':' + name if name else ''} {kind} measured={value} allowed={allowed}")
        elif value < allowed:
            errors.append(f"{path}{':' + name if name else ''} {kind} stale measured={value} allowed={allowed}; tighten or delete")
        entries.pop(metric_key, None)
    for (kind, path, name), entry in sorted(entries.items()):
        if kind in KINDS and "allowed" in entry:
            errors.append(f"{path}{':' + name if name else ''} {kind} missing measured entry (exception target no longer exists)")
    return errors


def threshold(kind: str, path: str) -> int:
    suffix = PurePosixPath(path).suffix
    return THRESHOLDS[kind].get(suffix, THRESHOLDS[kind].get("*", 0))


def enumerate_files(root: Path) -> list[str]:
    result = subprocess.run(["git", "ls-files", "-z"], cwd=root, check=True, stdout=subprocess.PIPE).stdout
    paths = [item.decode() for item in result.split(b"\0") if item]
    return sorted(path for path in paths if path.split("/", 1)[0] in {"backend", "frontend", "api", "scripts", "deploy"}
                  and PurePosixPath(path).suffix in SUFFIXES and not excluded(path))


def gather(root: Path) -> dict[tuple[str, str, str], int]:
    metrics: dict[tuple[str, str, str], int] = {}
    for path in enumerate_files(root):
        values = file_metrics((root / path).read_text(encoding="utf-8", errors="replace"))
        for kind, value in values.items():
            metrics[(kind, path, "")] = value
    command = ["go", "run", "./tools/funcmetrics", "-prefix", "backend"]
    try:
        completed = subprocess.run(command, cwd=root / "backend", check=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    except FileNotFoundError as error:
        raise RuntimeError("Go is required on PATH to run backend/tools/funcmetrics") from error
    except subprocess.CalledProcessError as error:
        raise RuntimeError(f"Go function metrics failed: {error.stderr.strip()}") from error
    for item in json.loads(completed.stdout):
        path, name = item["path"], item["name"]
        for field, kind in (("lines", "func_lines"), ("complexity", "func_complexity"), ("nesting", "func_nesting")):
            metrics[(kind, path, name)] = item[field]
    return metrics


def write_baseline(metrics: dict[tuple[str, str, str], int], existing: list[dict]) -> list[dict]:
    old = {key(entry): entry for entry in existing}
    result = []
    for metric_key, value in sorted(metrics.items()):
        kind, path, name = metric_key
        if value <= threshold(kind, path):
            continue
        previous = old.get(metric_key)
        if previous and value > previous.get("allowed", 0):
            raise ValueError(f"refusing to raise {path}{':' + name if name else ''} {kind}: {previous.get('allowed')} -> {value}")
        if previous and value <= previous.get("allowed", value):
            reason = previous.get("reason", "TODO: provide a technical rationale")
            owner = previous.get("owner", "TODO: name owner")
        else:
            reason = "TODO: provide a technical rationale and audit reference"
            owner = "TODO: name owning domain"
        entry = {"kind": kind, "path": path}
        if name:
            entry["name"] = name
        entry.update({"allowed": value, "reason": reason, "owner": owner})
        result.append(entry)
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--write-baseline", action="store_true")
    parser.add_argument("--list-exclusions", action="store_true")
    args = parser.parse_args()
    if args.list_exclusions:
        for pattern, _, reason in EXCLUSIONS:
            print(f"{pattern}: {reason}")
        return 0
    try:
        metrics = gather(ROOT)
        existing = json.loads(BASELINE.read_text(encoding="utf-8")) if BASELINE.exists() else []
        if args.write_baseline:
            updated = write_baseline(metrics, existing)
            BASELINE.parent.mkdir(parents=True, exist_ok=True)
            BASELINE.write_text(json.dumps(updated, indent=2) + "\n", encoding="utf-8")
            print(f"wrote {len(updated)} maintainability exceptions to {BASELINE.relative_to(ROOT)}")
            return 0
        errors = check_metrics(metrics, existing)
        if errors:
            print("\n".join(errors), file=sys.stderr)
            return 1
        counts = Counter(entry["kind"] for entry in existing)
        print("maintainability ratchet passed: " + ", ".join(f"{kind}={counts[kind]}" for kind in sorted(KINDS)))
        return 0
    except (OSError, RuntimeError, ValueError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(f"maintainability check failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
