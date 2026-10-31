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


def test_classification(path: str) -> str | None:
    """Return the test/test-support category for a non-production source path."""
    parts = PurePosixPath(path).parts
    name = parts[-1]
    if name.endswith("_test.go") or ".test." in name:
        return "test"
    if ((parts[0] == "scripts" and name.startswith("test_") and name.endswith(".py"))
            or (parts[:2] == ("frontend", "e2e") and name.endswith(".spec.ts"))):
        return "test"
    support_dirs = {"itemtest", "subjecttest", "testoidc", "testpostgres", "test", "e2e"}
    if "e2e" in parts[:2] or "test" in parts[:3] or any(part in support_dirs for part in parts):
        return "test-support"
    return None


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
        if allowed <= threshold(kind, path):
            errors.append(f"{metric_display(path, name)} {kind} exception allowed={allowed} is within threshold; remove it")
        elif value > allowed:
            errors.append(violation(kind, path, name, value, allowed))
        elif value < allowed:
            errors.append(f"{metric_display(path, name)} {kind} stale measured={value} allowed={allowed}; tighten")
        entries.pop(metric_key, None)
    for (kind, path, name), entry in sorted(entries.items()):
        if kind in KINDS and "allowed" in entry:
            errors.append(f"{metric_display(path, name)} {kind} missing measured entry")
    return errors


def enumerate_files(root: Path) -> list[str]:
    return [path for path in source_candidates(root) if not excluded(path)]


def exclusion_reason(path: str) -> str | None:
    parts = PurePosixPath(path).parts
    for pattern, predicate, reason in EXCLUSIONS:
        if predicate(parts, parts[-1]):
            return f"{pattern}: {reason}"
    return None


def source_candidates(root: Path) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=root, check=True, stdout=subprocess.PIPE,
    )
    paths = sorted({item.decode() for item in result.stdout.split(b"\0") if item})
    return [path for path in paths if PurePosixPath(path).suffix in SUFFIXES]


def inventory_area(path: str) -> str:
    suffix = PurePosixPath(path).suffix
    if suffix == ".go":
        return "backend Go"
    if suffix in {".ts", ".tsx"} and path.startswith("frontend/"):
        return "frontend TS"
    return "SQL" if suffix == ".sql" else "script"


def collect_exclusion_info(paths: list[str]) -> tuple[dict[str, list[str]], Counter]:
    groups: dict[str, list[str]] = {}
    counts = Counter()
    for path in paths:
        reason, category = exclusion_reason(path), test_classification(path)
        counts[category] += bool(category)
        if reason:
            groups.setdefault(reason, []).append(path)
    return groups, counts


def index_metrics(metrics: dict[tuple[str, str, str], int]) -> dict[str, list[tuple[str, str, int]]]:
    indexed: dict[str, list[tuple[str, str, int]]] = {}
    for (kind, path, name), value in metrics.items():
        indexed.setdefault(path, []).append((kind, name, value))
    return indexed


def index_exceptions(baseline: list[dict]) -> dict[str, list[dict]]:
    indexed: dict[str, list[dict]] = {}
    for entry in baseline:
        if test_classification(entry["path"]) is None:
            indexed.setdefault(entry["path"], []).append(entry)
    return indexed


def file_disposition(entries: list[dict]) -> str:
    if not entries:
        return "within thresholds"
    details = "; ".join(
        f"{entry['kind']} allowed={entry['allowed']} ({entry['owner']})"
        for entry in sorted(entries, key=lambda item: (item["kind"], item.get("name", "")))
    )
    return "baseline exception: " + details


def go_function_maxima(items: list[tuple[str, str, int]]) -> str:
    functions: dict[str, dict[str, int]] = {}
    for kind, name, value in items:
        if kind.startswith("func_"):
            functions.setdefault(name, {})[kind] = value
    maxima = []
    for kind in ("func_lines", "func_complexity", "func_nesting"):
        name, value = max(((name, values.get(kind, 0)) for name, values in functions.items()), key=lambda row: row[1], default=("—", 0))
        maxima.append(f"{value} ({name})")
    return f"{len(functions)}; " + "; ".join(maxima)


def render_inventory_row(path: str, indexed_metrics: dict[str, list[tuple[str, str, int]]], exceptions: dict[str, list[dict]]) -> str:
    area = inventory_area(path)
    values = {kind: value for kind, _, value in indexed_metrics.get(path, [])}
    detail = go_function_maxima(indexed_metrics.get(path, [])) if area == "backend Go" else "—"
    return f"| `{path}` | {area} | {values.get('file_lines', 0)} | {values.get('long_lines', 0)} | {detail} | {file_disposition(exceptions.get(path, []))} |"


def render_inventory_header(root: Path, head: str, areas: Counter, total: int, counts: Counter) -> list[str]:
    frontend = json.loads((root / "frontend/maintainability-baseline.json").read_text(encoding="utf-8"))
    lines = [
        "# V2.1 Hand-Written Production Code Inventory", "",
        "Regenerate with `bash scripts/dev-exec.sh -- python3 scripts/maintainability_check.py --inventory-out docs/development/v2-1-production-inventory.md`.",
        f"This is a snapshot at Git HEAD `{head}`; rerun the command after source changes.",
        "The maintainability ratchet checks the live tree and guarantees ongoing threshold compliance between inventory snapshots.",
        "Go has function-level metrics; frontend TS rules are enforced by ESLint.",
        "Python, shell, and SQL are measured at file level only (size and long lines).",
        "", "## Summary", "", "| Area | Production files |", "|---|---:|",
    ]
    lines.extend(f"| {area} | {areas[area]} |" for area in ("backend Go", "frontend TS", "SQL", "script"))
    lines.extend([
        f"| **Total production** | **{total}** |", "",
        f"Test files: {counts['test']}; test-support files: {counts['test-support']}.",
        f"Frontend function-level rules are enforced by ESLint with `frontend/maintainability-baseline.json` ({len(frontend)} entries).",
    ])
    return lines


def render_exclusion_section(groups: dict[str, list[str]]) -> list[str]:
    lines = ["", "## Excluded source paths", ""]
    for reason, paths in sorted(groups.items()):
        lines.extend([f"### {reason}", "", *(f"- `{path}`" for path in sorted(paths)), ""])
    return lines


def inventory_markdown(root: Path, metrics: dict[tuple[str, str, str], int], baseline: list[dict]) -> str:
    paths = enumerate_files(root)
    production = [path for path in paths if test_classification(path) is None]
    groups, counts = collect_exclusion_info(source_candidates(root))
    indexed_metrics, exceptions = index_metrics(metrics), index_exceptions(baseline)
    areas = Counter(inventory_area(path) for path in production)
    head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=root, check=True, text=True, stdout=subprocess.PIPE).stdout.strip()
    lines = render_inventory_header(root, head, areas, len(production), counts)
    lines.extend([
        "", "## Production files", "",
        "| Path | Area | Non-blank lines | Long lines | Go functions; max lines (function); complexity (function); nesting (function) | Disposition |",
        "|---|---|---:|---:|---|---|",
    ])
    lines.extend(render_inventory_row(path, indexed_metrics, exceptions) for path in production)
    lines.extend(render_exclusion_section(groups))
    return "\n".join(lines).rstrip() + "\n"


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


def add_function_metrics(metrics: dict[tuple[str, str, str], int], items: list[dict]) -> None:
    for item in items:
        name = item["name"]
        path = item["path"]
        for field, kind in FUNCTION_METRICS:
            metric_key = (kind, path, name)
            if metric_key in metrics:
                raise RuntimeError(f"duplicate Go function metric identity: {path}:{name}:{kind}")
            metrics[metric_key] = item[field]


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
            "go", "run", "./tools/funcmetrics", "-repo-root", str(backend),
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
    add_function_metrics(metrics, json.loads(result.stdout))
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


def load_base_baseline(ref: str | None, root: Path = ROOT) -> list[dict] | None:
    if not ref:
        return None
    resolved = subprocess.run(
        ["git", "rev-parse", "--verify", f"{ref}^{{commit}}"],
        cwd=root,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if resolved.returncode != 0:
        raise RuntimeError(f"cannot resolve base ref {ref}: {resolved.stderr.strip()}")
    commit = resolved.stdout.strip()
    path = "ci/maintainability-baseline.json"
    exists = subprocess.run(
        ["git", "cat-file", "-e", f"{commit}:{path}"],
        cwd=root,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if exists.returncode != 0:
        missing = subprocess.run(
            ["git", "ls-tree", "--name-only", commit, path], cwd=root,
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
        if missing.returncode != 0:
            raise RuntimeError(f"cannot inspect baseline at {ref}: {missing.stderr.strip()}")
        if missing.stdout.strip():
            raise RuntimeError(
                f"baseline object at {ref} exists in tree but cannot be read: {exists.stderr.decode(errors='replace').strip()}"
            )
        return None
    result = subprocess.run(
        ["git", "show", f"{commit}:{path}"], cwd=root,
        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        raise RuntimeError(f"cannot read baseline at {ref}: {result.stderr.strip()}")
    baseline = json.loads(result.stdout)
    if not isinstance(baseline, list):
        raise ValueError(f"baseline at {ref} must be a JSON array")
    return baseline


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--write-baseline", action="store_true")
    parser.add_argument("--list-exclusions", action="store_true")
    parser.add_argument("--inventory", action="store_true")
    parser.add_argument("--inventory-out", metavar="PATH")
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
        if args.inventory or args.inventory_out:
            rendered = inventory_markdown(ROOT, metrics, existing)
            if args.inventory_out:
                output = Path(args.inventory_out)
                if not output.is_absolute():
                    output = ROOT / output
                output.write_text(rendered, encoding="utf-8")
            else:
                print(rendered, end="")
            return 0
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
