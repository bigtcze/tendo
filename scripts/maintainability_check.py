#!/usr/bin/env python3
"""Enforce maintainability thresholds with named, downward-only debt ratchets."""
from __future__ import annotations

import argparse
from collections import Counter
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent.parent
BASELINE = ROOT / "ci/maintainability-baseline.json"
THRESHOLDS = {
    "file_lines": {
        ".go": 500, ".ts": 350, ".tsx": 350,
        ".js": 350, ".mjs": 350, ".cjs": 350,
        ".py": 500, ".sh": 500, ".sql": 400,
    },
    "long_lines": {"*": 0}, "func_lines": {"*": 80},
    "func_complexity": {"*": 15}, "func_nesting": {"*": 4},
}
MIGRATION_DIR = ("backend", "internal", "platform", "database")
SQLC_PACKAGES = ("household", "identity", "item", "subject")
OAPI_FILES = (
    (
        "backend/internal/identity/httpapi/setup.gen.go",
        ("backend", "internal", "identity", "httpapi", "setup.gen.go"),
        "oapi-codegen setup types; API generation checks detect drift.",
    ),
    (
        "backend/internal/household/httpapi/household.gen.go",
        ("backend", "internal", "household", "httpapi", "household.gen.go"),
        "oapi-codegen household types; API generation checks detect drift.",
    ),
    (
        "backend/internal/item/httpapi/item.gen.go",
        ("backend", "internal", "item", "httpapi", "item.gen.go"),
        "oapi-codegen item types; API generation checks detect drift.",
    ),
    (
        "backend/internal/subject/httpapi/subject.gen.go",
        ("backend", "internal", "subject", "httpapi", "subject.gen.go"),
        "oapi-codegen subject types; API generation checks detect drift.",
    ),
    (
        "backend/internal/platform/httpx/health.gen.go",
        ("backend", "internal", "platform", "httpx", "health.gen.go"),
        "oapi-codegen health types; API generation checks detect drift.",
    ),
)
OPENAPI_FILES = (
    (
        "api/generated/api.d.ts",
        ("api", "generated", "api.d.ts"),
        "openapi-typescript API declaration; generation tests detect drift.",
    ),
    (
        "api/generated/health.d.ts",
        ("api", "generated", "health.d.ts"),
        "openapi-typescript health declaration; generation tests detect drift.",
    ),
)
EXCLUSIONS = (
    (
        "*_test.go",
        lambda p, n: n.endswith("_test.go"),
        "Go tests are excluded; policy does not require splitting a test suite.",
    ),
    (
        "*.test.*",
        lambda p, n: ".test." in n,
        "Test suites are excluded; policy does not require splitting them.",
    ),
    (
        "frontend/e2e/*.spec.ts",
        lambda p, n: p[:2] == ("frontend", "e2e") and n.endswith(".spec.ts"),
        "Browser E2E specs are test suites.",
    ),
    (
        "scripts/test_*.py",
        lambda p, n: p[:1] == ("scripts",)
        and n.startswith("test_") and n.endswith(".py"),
        "Python harness suites are test code.",
    ),
    (
        "*/testdata/*",
        lambda p, n: "testdata" in p,
        "Fixture data and test-only source are excluded.",
    ),
    (
        "backend/internal/platform/database/NNN_*.sql",
        lambda p, n: p[:-1] == MIGRATION_DIR
        and re.fullmatch(r"\d{3}_.+\.sql", n) is not None,
        "Embedded numbered migration history is immutable.",
    ),
    (
        "frontend/src/i18n/{cs,en}.ts",
        lambda p, n: p[:3] == ("frontend", "src", "i18n")
        and n in {"cs.ts", "en.ts"},
        "Cohesive static locale catalogs are not split to satisfy line limits.",
    ),
    *(
        (
            f"backend/internal/{package}/postgres/dbgen/",
            lambda parts, name, package=package: (
                len(parts) >= 5
                and parts[:5] == ("backend", "internal", package, "postgres", "dbgen")
            ),
            f"sqlc generated {package} code; scripts/check-sqlc.sh checks drift.",
        )
        for package in SQLC_PACKAGES
    ),
    *(
        (
            pattern,
            lambda parts, name, target=target: (
                parts == target and name == target[-1]
            ),
            reason,
        )
        for pattern, target, reason in OAPI_FILES
    ),
    *(
        (
            pattern,
            lambda parts, name, target=target: (
                parts == target and name == target[-1]
            ),
            reason,
        )
        for pattern, target, reason in OPENAPI_FILES
    ),
)
SUFFIXES = {".go", ".ts", ".tsx", ".js", ".mjs", ".cjs", ".py", ".sh", ".sql"}
KINDS = set(THRESHOLDS)
FUNCTION_METRICS = (
    ("lines", "func_lines"),
    ("complexity", "func_complexity"),
    ("nesting", "func_nesting"),
)


def excluded(path: str) -> bool:
    parts = PurePosixPath(path).parts
    return any(predicate(parts, parts[-1]) for _, predicate, _ in EXCLUSIONS)


def file_metrics(text: str) -> dict[str, int]:
    lines = text.splitlines()
    nonblank = sum(bool(line.strip()) for line in lines)
    long_lines = sum(len(line) > 160 for line in lines)
    return {"file_lines": nonblank, "long_lines": long_lines}


def key(entry: dict) -> tuple[str, str, str]:
    return entry["kind"], entry["path"], entry.get("name", "")


def metric_display(path: str, name: str) -> str:
    return f"{path}:{name}" if name else path


def violation(kind: str, path: str, name: str, value: int, allowed: int) -> str:
    return f"{metric_display(path, name)} {kind} measured={value} allowed={allowed}"


def threshold(kind: str, path: str) -> int:
    suffix = PurePosixPath(path).suffix
    return THRESHOLDS[kind].get(suffix, THRESHOLDS[kind].get("*", 0))


def check_metrics(metrics: dict[tuple[str, str, str], int], baseline: list[dict]) -> list[str]:
    errors = []
    entries = {}
    for entry in baseline:
        item_key = key(entry)
        if item_key in entries:
            errors.append(f"{entry.get('path', '?')} {entry.get('kind', '?')} duplicate baseline entry")
            continue
        entries[item_key] = entry
        if entry.get("kind") not in KINDS:
            errors.append(
                f"{entry.get('path', '?')} invalid baseline kind={entry.get('kind')}"
            )
        for field in ("reason", "owner"):
            value = entry.get(field)
            if not isinstance(value, str) or not value.strip() or value.startswith("TODO:"):
                path = entry.get("path", "?")
                name = entry.get("name", "")
                kind = entry.get("kind", "?")
                errors.append(f"{path} {name} {kind} missing {field}")
    for metric_key, value in sorted(metrics.items()):
        kind, path, name = metric_key
        exception = entries.get(metric_key)
        if exception is None:
            limit = threshold(kind, path)
            if value > limit:
                errors.append(violation(kind, path, name, value, limit))
            continue
        allowed = exception.get("allowed")
        if not isinstance(allowed, int):
            errors.append(f"{metric_display(path, name)} {kind} invalid allowed value")
            continue
        if value > allowed:
            errors.append(violation(kind, path, name, value, allowed))
        elif value < allowed:
            errors.append(f"{metric_display(path, name)} {kind} stale measured={value} allowed={allowed}; tighten")
        entries.pop(metric_key, None)
    for (kind, path, name), entry in sorted(entries.items()):
        if kind in KINDS and "allowed" in entry:
            errors.append(f"{metric_display(path, name)} {kind} missing measured entry")
    return errors


def enumerate_files(root: Path) -> list[str]:
    command = ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"]
    result = subprocess.run(
        command, cwd=root, check=True, stdout=subprocess.PIPE
    )
    raw = result.stdout
    paths = sorted({item.decode() for item in raw.split(b"\0") if item})
    return [p for p in paths if PurePosixPath(p).suffix in SUFFIXES and not excluded(p)]


def baseline_errors_against_base(current: list[dict], base: list[dict]) -> list[str]:
    old = {key(entry): entry for entry in base}
    errors = []
    current_by_key = {key(entry): entry for entry in current}
    for item_key, entry in current_by_key.items():
        previous = old.get(item_key)
        if previous is None:
            errors.append(
                f"{entry.get('path')} {entry.get('name', '')} "
                f"{entry.get('kind')} new exception relative to base"
            )
        elif entry.get("allowed", 0) > previous.get("allowed", 0):
            path = metric_display(entry.get("path", ""), entry.get("name", ""))
            errors.append(
                f"{path} {entry.get('kind')} allowed raised={entry.get('allowed')} "
                f"base={previous.get('allowed')}"
            )
    return errors


def discover_go_files(root: Path, paths: list[str]) -> list[str]:
    go_paths = []
    for path in paths:
        if not path.startswith("backend/") or not path.endswith(".go"):
            continue
        if (root / path).is_file():
            go_paths.append(path.removeprefix("backend/"))
    return go_paths


def gather(root: Path) -> dict[tuple[str, str, str], int]:
    paths = enumerate_files(root)
    metrics = {}
    for path in paths:
        text = (root / path).read_text(encoding="utf-8", errors="replace")
        for kind, value in file_metrics(text).items():
            metrics[(kind, path, "")] = value
    go_files = discover_go_files(root, paths)
    if any(p.endswith(".go") and not p.startswith("backend/") for p in paths):
        raise RuntimeError("Go sources outside backend/ are not measured; extend funcmetrics wiring")
    if not go_files:
        return metrics
    backend = root / "backend"
    if not (backend / "go.mod").is_file():
        raise RuntimeError("Go metrics tool requires the backend module")
    with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False, encoding="utf-8") as handle:
        handle.write("\n".join(go_files) + "\n")
    source_list = Path(handle.name)
    try:
        command = [
            "go", "run", "./tools/funcmetrics", "-prefix", "backend",
            "-files", str(source_list),
        ]
        result = subprocess.run(
            command, cwd=backend, check=True, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
    except FileNotFoundError as error:
        raise RuntimeError(
            "Go is required on PATH to run backend/tools/funcmetrics"
        ) from error
    except subprocess.CalledProcessError as error:
        raise RuntimeError(
            f"Go function metrics failed: {error.stderr.strip()}"
        ) from error
    finally:
        source_list.unlink(missing_ok=True)
    for item in json.loads(result.stdout):
        name = item["name"]
        path = item["path"]
        for field, kind in FUNCTION_METRICS:
            metrics[(kind, path, name)] = item[field]
    return metrics


def write_baseline(
    metrics: dict[tuple[str, str, str], int],
    existing: list[dict],
) -> list[dict]:
    old = {key(entry): entry for entry in existing}
    result = []
    for metric_key, value in sorted(metrics.items()):
        kind, path, name = metric_key
        if value <= threshold(kind, path):
            continue
        previous = old.get(metric_key)
        if previous and value > previous.get("allowed", 0):
            raise ValueError(f"refusing to raise {path}:{name} {kind}")
        entry = {"kind": kind, "path": path}
        if name:
            entry["name"] = name
        reason = previous.get("reason", "TODO: provide technical rationale") if previous else "TODO: provide technical rationale"
        owner = previous.get("owner", "TODO: name owner") if previous else "TODO: name owner"
        entry.update({"allowed": value, "reason": reason, "owner": owner})
        result.append(entry)
    return result


def load_base_baseline(ref: str | None) -> list[dict] | None:
    if not ref:
        return None
    command = ["git", "show", f"{ref}:ci/maintainability-baseline.json"]
    result = subprocess.run(
        command,
        cwd=ROOT,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        return None
    return json.loads(result.stdout)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--write-baseline", action="store_true")
    parser.add_argument("--list-exclusions", action="store_true")
    parser.add_argument("--base-ref", default=None)
    args = parser.parse_args()
    if args.list_exclusions:
        for pattern, _, reason in EXCLUSIONS:
            print(f"{pattern}: {reason}")
        print(
            "Unmeasured formats: YAML, JSON, Markdown, CSS, HTML, and other "
            "formats without calibrated source metrics."
        )
        return 0
    try:
        metrics = gather(ROOT)
        existing = json.loads(BASELINE.read_text(encoding="utf-8")) if BASELINE.exists() else []
        base = load_base_baseline(args.base_ref or os.environ.get("MAINTAINABILITY_BASE_REF"))
        if args.write_baseline:
            updated = write_baseline(metrics, existing)
            BASELINE.write_text(json.dumps(updated, indent=2) + "\n", encoding="utf-8")
            print(f"wrote {len(updated)} maintainability exceptions")
            return 0
        errors = check_metrics(metrics, existing)
        if base is not None:
            errors.extend(baseline_errors_against_base(existing, base))
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
