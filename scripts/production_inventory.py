#!/usr/bin/env python3
"""Render the hand-written production code inventory from maintainability metrics.

The inventory reuses the ratchet's file enumeration, exclusions, thresholds and
baseline, so every measured production file gets a disposition derived from its
measured values. Tracked files in formats without calibrated metrics are listed
separately so nothing disappears silently.
"""
from __future__ import annotations

import argparse
from collections import Counter
import json
from pathlib import Path, PurePosixPath
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import maintainability_check as check  # noqa: E402

ROOT = check.ROOT
DEFAULT_OUT = "docs/development/v2-1-production-inventory.md"
GO_FUNCTION_KINDS = ("func_lines", "func_complexity", "func_nesting")
LOCKFILES = {"package-lock.json", "go.sum"}
BINARY_SUFFIXES = {".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp", ".pdf", ".asc"}
TEST_SUPPORT_DIRS = {"itemtest", "subjecttest", "testoidc", "testpostgres"}

Metrics = dict[tuple[str, str, str], int]
PathMetrics = dict[str, list[tuple[str, str, int]]]


def test_classification(path: str) -> str | None:
    """Return "test", "test-support" or None (production) for a measured path."""
    parts = PurePosixPath(path).parts
    name = parts[-1]
    if name.endswith("_test.go") or ".test." in name:
        return "test"
    if parts[0] == "scripts" and name.startswith("test_") and name.endswith(".py"):
        return "test"
    if parts[:2] == ("frontend", "e2e"):
        return "test" if name.endswith(".spec.ts") else "test-support"
    in_test_tree = parts[:2] == ("backend", "test") or parts[:3] == ("frontend", "src", "test")
    if in_test_tree or any(part in TEST_SUPPORT_DIRS for part in parts):
        return "test-support"
    return None


def unmeasured_classification(path: str) -> str:
    """Classify a tracked file whose format has no calibrated source metric."""
    parts = PurePosixPath(path).parts
    name = parts[-1]
    if parts[0] == ".autonomous":
        return "agent context"
    if name.endswith(".md") or name == "LICENSE":
        return "documentation"
    if name in LOCKFILES:
        return "lockfile"
    if PurePosixPath(name).suffix.lower() in BINARY_SUFFIXES or name == ".gitkeep":
        return "binary asset or placeholder"
    return "authored"


def tracked_files(root: Path) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root, check=True, stdout=subprocess.PIPE,
    )
    return sorted({item.decode() for item in result.stdout.split(b"\0") if item})


def area(path: str) -> str:
    suffix = PurePosixPath(path).suffix
    if suffix == ".go":
        return "backend Go"
    if suffix in {".ts", ".tsx"} and path.startswith("frontend/"):
        return "frontend TS"
    return "SQL" if suffix == ".sql" else "script/tooling"


def index_metrics(metrics: Metrics) -> PathMetrics:
    indexed: PathMetrics = {}
    for (kind, path, name), value in metrics.items():
        indexed.setdefault(path, []).append((kind, name, value))
    return indexed


def metric_disposition(path: str, kind: str, name: str, value: int, exceptions: dict) -> str | None:
    limit = check.threshold(kind, path)
    label = f"{kind}{f' {name}' if name else ''}"
    entry = exceptions.get((kind, path, name))
    if entry is None:
        return None if value <= limit else f"VIOLATION {label} measured={value} threshold={limit}"
    if value > entry["allowed"]:
        return f"VIOLATION {label} measured={value} allowed={entry['allowed']}"
    return f"baseline exception {label} measured={value} allowed={entry['allowed']} ({entry['owner']})"


def file_disposition(path: str, items: list[tuple[str, str, int]], exceptions: dict) -> str:
    notes = [metric_disposition(path, kind, name, value, exceptions) for kind, name, value in sorted(items)]
    notes = [note for note in notes if note]
    return "; ".join(notes) if notes else "within thresholds"


def go_function_maxima(items: list[tuple[str, str, int]]) -> str:
    functions: dict[str, dict[str, int]] = {}
    for kind, name, value in items:
        if kind in GO_FUNCTION_KINDS:
            functions.setdefault(name, {})[kind] = value
    maxima = []
    for kind in GO_FUNCTION_KINDS:
        rows = ((values.get(kind, 0), name) for name, values in sorted(functions.items()))
        value, name = max(rows, key=lambda row: row[0], default=(0, "—"))
        maxima.append(f"{value} ({name})")
    return f"{len(functions)}; " + "; ".join(maxima)


def production_row(path: str, indexed: PathMetrics, exceptions: dict) -> str:
    items = indexed.get(path, [])
    values = {kind: value for kind, name, value in items if not name}
    detail = go_function_maxima(items) if area(path) == "backend Go" else "—"
    disposition = file_disposition(path, items, exceptions)
    return f"| `{path}` | {area(path)} | {values.get('file_lines', 0)} | {values.get('long_lines', 0)} | {detail} | {disposition} |"


def header(head: str, production: list[str], test_counts: Counter, frontend_entries: int) -> list[str]:
    areas = Counter(area(path) for path in production)
    lines = [
        "# V2.1 hand-written production code inventory", "",
        f"Regenerate with `bash scripts/dev-exec.sh -- python3 scripts/production_inventory.py --out {DEFAULT_OUT}`.",
        f"Snapshot at Git HEAD `{head}`. The maintainability ratchet (`scripts/maintainability_check.py`) checks the live tree on every PR.",
        "Go files have function-level metrics. Frontend TypeScript function rules are enforced by ESLint "
        f"with `frontend/maintainability-baseline.json` ({frontend_entries} entries). Python, shell and SQL are measured at file level only.",
        "Dispositions are derived from measured values against thresholds and named baseline exceptions; generation fails on any violation.",
        "", "## Summary", "", "| Area | Production files |", "|---|---:|",
    ]
    lines.extend(f"| {name} | {areas[name]} |" for name in ("backend Go", "frontend TS", "SQL", "script/tooling"))
    lines.extend([f"| **Total** | **{len(production)}** |", "",
                  f"Measured test files: {test_counts['test']}; test-support files: {test_counts['test-support']}."])
    return lines


def production_section(production: list[str], metrics: Metrics, baseline: list[dict]) -> list[str]:
    indexed = index_metrics(metrics)
    exceptions = {check.key(entry): entry for entry in baseline}
    lines = ["", "## Measured production files", "",
             "| Path | Area | Non-blank lines | Long lines | Go functions; max lines; max complexity; max nesting | Disposition |",
             "|---|---|---:|---:|---|---|"]
    lines.extend(production_row(path, indexed, exceptions) for path in production)
    return lines


def unmeasured_section(root: Path, paths: list[str]) -> list[str]:
    groups: dict[str, list[str]] = {}
    for path in paths:
        groups.setdefault(unmeasured_classification(path), []).append(path)
    lines = ["", "## Authored files in unmeasured formats", "",
             "Configuration, deployment, markup, style and data files have no calibrated metric; each was reviewed for scope.", "",
             "| Path | Non-blank lines | Test-only | Disposition |", "|---|---:|---|---|"]
    for path in groups.pop("authored", []):
        nonblank = check.file_metrics((root / path).read_text(encoding="utf-8", errors="replace"))["file_lines"]
        test_only = "yes" if test_classification(path) or ".e2e." in path or ".oidc-test." in path else "no"
        lines.append(f"| `{path}` | {nonblank} | {test_only} | reviewed; unmeasured format |")
    lines.extend(["", "Not production source (counts):", ""])
    lines.extend(f"- {name}: {len(groups[name])}" for name in sorted(groups))
    return lines


def excluded_section(paths: list[str]) -> list[str]:
    groups: dict[str, list[str]] = {}
    for path in paths:
        parts = PurePosixPath(path).parts
        for pattern, predicate, reason in check.EXCLUSIONS:
            if predicate(parts, parts[-1]):
                groups.setdefault(f"{pattern}: {reason}", []).append(path)
                break
    lines = ["", "## Measured-format paths excluded from the ratchet", ""]
    for reason in sorted(groups):
        lines.extend([f"### {reason}", "", f"{len(groups[reason])} file(s).", ""])
        if not all(test_classification(path) for path in groups[reason]):
            lines.extend([*(f"- `{path}`" for path in groups[reason]), ""])
    return lines


def render(root: Path, metrics: Metrics, baseline: list[dict], head: str) -> str:
    tracked = tracked_files(root)
    measured_format = [path for path in tracked if PurePosixPath(path).suffix in check.SUFFIXES]
    measured = [path for path in measured_format if not check.excluded(path)]
    production = [path for path in measured if test_classification(path) is None]
    test_counts = Counter(test_classification(path) for path in measured_format)
    frontend_entries = len(json.loads((root / "frontend/maintainability-baseline.json").read_text(encoding="utf-8")))
    lines = header(head, production, test_counts, frontend_entries)
    lines.extend(production_section(production, metrics, baseline))
    lines.extend(unmeasured_section(root, [path for path in tracked if path not in set(measured_format)]))
    lines.extend(excluded_section([path for path in measured_format if check.excluded(path)]))
    return "\n".join(lines).rstrip() + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--out", help="write Markdown to this path instead of stdout")
    args = parser.parse_args()
    metrics = check.gather(ROOT)
    baseline = json.loads(check.BASELINE.read_text(encoding="utf-8"))
    head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, check=True, text=True, stdout=subprocess.PIPE).stdout.strip()
    rendered = render(ROOT, metrics, baseline, head)
    if args.out:
        (ROOT / args.out).write_text(rendered, encoding="utf-8")
    else:
        sys.stdout.write(rendered)
    errors = check.check_metrics(metrics, baseline)
    if errors:
        print("\n".join(errors), file=sys.stderr)
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
