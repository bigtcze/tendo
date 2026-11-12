#!/usr/bin/env python3
"""GitHub ownership-label migration for the evergreen autonomy controller.

One-time, owner-authorized migration from the milestone-scoped V5 backlog
label `tendo:v5-autonomous` to the version-neutral `release` label that the
evergreen controller uses to recognise agent-owned work. The migration is
planned into a manifest, applied step by step with a verify-after-write,
validated against live GitHub (including the real ownership query and the
selector self-check) and can be rolled back to the exact original ownership
labels.

Guarantees:
  * Only the `release` label is added and only `tendo:v5-autonomous` is
    removed. Status labels (`status:idle|in-progress|blocked`), milestones,
    titles, bodies and closed Issues are never modified.
  * Every gh access is `gh api` (REST). A failed call or invalid JSON aborts
    the command with exit 2; an error is never treated as an empty list.
  * The manifest is written atomically (tmp + rename) with mode 0600 and
    records partial progress after every mutating step, so a failed apply can
    be re-run (resume) or rolled back.

Exit codes: 0 success, 2 refused/error, 3 rollback incomplete.

Usage:
  autonomy_migration.py plan      --manifest FILE [--repo OWNER/NAME]
  autonomy_migration.py apply     --manifest FILE [--repo OWNER/NAME]
  autonomy_migration.py validate  --manifest FILE [--repo OWNER/NAME] [--selector FILE]
  autonomy_migration.py rollback  --manifest FILE [--repo OWNER/NAME]
  autonomy_migration.py show-plan --manifest FILE
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from urllib.parse import quote

DEFAULT_REPO = "bigtcze/tendo"
SOURCE_MILESTONE = "Tendo v5.0.0 - Refined & Reliable"
SOURCE_LABEL = "tendo:v5-autonomous"
TARGET_LABEL = "release"
STATUS_LABELS = ("status:idle", "status:in-progress", "status:blocked")
RELEASE_COLOR = "0e8a16"
RELEASE_DESCRIPTION = "Agent-owned release work (Tendo autonomy controller)"

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_SELECTOR = os.path.join(REPO_ROOT, "scripts", "autonomy-work-selection.py")


class GhError(Exception):
    """A gh/API failure. Never converted into an empty result."""

    def __init__(self, message: str, status: int = 0):
        super().__init__(message)
        self.status = status


class ManifestError(Exception):
    """The manifest is missing, unreadable or inconsistent."""


# ---------------------------------------------------------------------------
# gh access
# ---------------------------------------------------------------------------
class GhCli:
    """Thin `gh api` runner. The only place that spawns gh."""

    def __init__(self, binary: str = "gh", timeout: int = 240):
        self.binary = binary
        self.timeout = timeout

    def _run(self, args: list[str]) -> str:
        proc = subprocess.run([self.binary, "api", *args], capture_output=True, text=True, timeout=self.timeout)
        if proc.returncode != 0:
            detail = (proc.stderr or proc.stdout or "").strip()
            raise GhError(f"gh api {args[0]} failed rc={proc.returncode}: {detail}")
        return proc.stdout

    @staticmethod
    def _decode(path: str, text: str):
        # An empty response is never "no data": every endpoint we use returns
        # JSON, and a silent empty result could turn into an empty inventory.
        if not text.strip():
            raise GhError(f"empty response from gh api {path}")
        try:
            payload = json.loads(text)
        except json.JSONDecodeError as exc:
            raise GhError(f"invalid JSON from gh api {path}: {exc}") from exc
        if payload is None:
            raise GhError(f"null response from gh api {path}")
        return payload

    def request(self, method: str, path: str, fields: dict | None = None):
        args: list[str] = []
        if method.upper() != "GET":
            args += ["--method", method.upper()]
        args.append(path)
        for key, value in (fields or {}).items():
            if isinstance(value, (list, tuple)):
                for item in value:
                    args += ["-f", f"{key}[]={item}"]
            else:
                args += ["-f", f"{key}={value}"]
        return self._decode(path, self._run(args))

    def paginate(self, path: str) -> list[dict]:
        pages = self._decode(path, self._run(["--paginate", "--slurp", path]))
        if not isinstance(pages, list):
            raise GhError(f"invalid paginated response from gh api {path}: expected a list of pages")
        items: list[dict] = []
        for page in pages:
            if not isinstance(page, list):
                raise GhError(f"invalid page type from gh api {path}: expected a list")
            items.extend(page)
        return items


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------
def label_names(issue: dict) -> list[str]:
    names = []
    for label in issue.get("labels") or []:
        names.append(label.get("name", "") if isinstance(label, dict) else str(label))
    return sorted(n for n in names if n)


def milestone_title(issue: dict) -> str | None:
    milestone = issue.get("milestone")
    if isinstance(milestone, dict):
        return milestone.get("title")
    return None


def is_open(issue: dict) -> bool:
    return str(issue.get("state", "")).lower() == "open"


def is_issue(entry: dict) -> bool:
    return "pull_request" not in entry


def now_utc() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def load_manifest(path: str) -> dict:
    try:
        with open(path, encoding="utf-8") as handle:
            manifest = json.load(handle)
    except FileNotFoundError as exc:
        raise ManifestError(f"manifest {path} does not exist; run `plan` first") from exc
    except (OSError, json.JSONDecodeError) as exc:
        raise ManifestError(f"manifest {path} is unreadable: {exc}") from exc
    if not isinstance(manifest, dict) or not isinstance(manifest.get("issues"), list):
        raise ManifestError(f"manifest {path} is not a migration manifest")
    return manifest


def save_manifest(path: str, manifest: dict) -> None:
    directory = os.path.dirname(os.path.abspath(path))
    os.makedirs(directory, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=".manifest-", dir=directory)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(manifest, handle, indent=2, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(tmp, 0o600)
        os.replace(tmp, path)
    except BaseException:
        if os.path.exists(tmp):
            os.unlink(tmp)
        raise


def set_phase(path: str, manifest: dict, phase: str) -> None:
    manifest["phase"] = phase
    save_manifest(path, manifest)


def paginated_entries(gh, path: str) -> list[dict]:
    """A paginated list endpoint must yield a list of objects, never null/short data."""
    entries = gh.paginate(path)
    if not isinstance(entries, list):
        raise GhError(f"invalid list response from gh api {path}: expected a list")
    for entry in entries:
        if not isinstance(entry, dict):
            raise GhError(f"invalid entry from gh api {path}: expected an object")
    return entries


def list_labels(gh, repo: str) -> list[dict]:
    return paginated_entries(gh, f"repos/{repo}/labels?per_page=100")


def list_issues(gh, repo: str, query: str) -> list[dict]:
    entries = paginated_entries(gh, f"repos/{repo}/issues?{query}")
    return [entry for entry in entries if is_issue(entry)]


def get_issue(gh, repo: str, number: int) -> dict:
    issue = gh.request("GET", f"repos/{repo}/issues/{number}")
    if not isinstance(issue, dict):
        raise GhError(f"issue #{number} returned an invalid response")
    return issue


def allowed_label_states(original: list[str]) -> set[frozenset]:
    base = set(original)
    return {
        frozenset(base),
        frozenset(base | {TARGET_LABEL}),
        frozenset((base | {TARGET_LABEL}) - {SOURCE_LABEL}),
    }


def describe_states(states: set[frozenset]) -> str:
    return ", ".join("[" + ", ".join(sorted(state)) + "]" for state in sorted(states, key=lambda s: sorted(s)))


# ---------------------------------------------------------------------------
# plan
# ---------------------------------------------------------------------------
def reverify_manifest(gh, repo: str, manifest: dict, out, err) -> None:
    if manifest.get("repo") and manifest["repo"] != repo:
        raise ManifestError(
            f"manifest was planned for {manifest['repo']}, not {repo}; refusing to mix repositories"
        )
    for issue in manifest["issues"]:
        number = int(issue["number"])
        try:
            live = get_issue(gh, repo, number)
        except GhError as exc:
            raise ManifestError(f"manifest issue #{number} cannot be read: {exc}") from exc
        live_title = live.get("title")
        if live_title and live_title != issue.get("title"):
            err.write(f"note: #{number} title changed since planning ({issue.get('title')!r} -> {live_title!r})\n")
        if milestone_title(live) != issue.get("milestone"):
            raise ManifestError(f"manifest issue #{number} milestone changed; refusing to resume")
        current = frozenset(label_names(live))
        if current not in allowed_label_states(issue["original_labels"]):
            raise ManifestError(
                f"manifest issue #{number} is in an unexpected label state "
                f"{sorted(current)}; refusing to resume (concurrent edit)"
            )
    out.write(f"manifest re-verified against live GitHub: {len(manifest['issues'])} issue(s); original labels kept\n")


def cmd_plan(gh, repo: str, manifest_path: str, out, err) -> int:
    if os.path.exists(manifest_path):
        manifest = load_manifest(manifest_path)
        reverify_manifest(gh, repo, manifest, out, err)
        migrated = sum(1 for i in manifest["issues"] if i["steps"]["removed_source"] and i["steps"]["added_release"])
        out.write(f"plan: resuming existing manifest ({migrated}/{len(manifest['issues'])} issue(s) migrated)\n")
        return 0

    labels = list_labels(gh, repo)
    release_existed = any(label.get("name") == TARGET_LABEL for label in labels)

    source_issues = list_issues(
        gh, repo, f"state=all&labels={quote(SOURCE_LABEL, safe='')}&per_page=100"
    )
    release_issues = list_issues(
        gh, repo, f"state=open&labels={quote(TARGET_LABEL, safe='')}&per_page=100"
    )

    targets: list[dict] = []
    untouched_closed: list[int] = []
    for entry in source_issues:
        number = int(entry["number"])
        names = label_names(entry)
        if not is_open(entry):
            untouched_closed.append(number)
            continue
        if SOURCE_LABEL not in names:
            continue
        if milestone_title(entry) != SOURCE_MILESTONE:
            err.write(
                f"note: open issue #{number} carries {SOURCE_LABEL} but not the source milestone; not a target\n"
            )
            continue
        statuses = [name for name in names if name in STATUS_LABELS]
        if len(statuses) != 1:
            raise ManifestError(
                f"anomaly: issue #{number} has {len(statuses)} status label(s) {statuses}; "
                "exactly one of " + "/".join(STATUS_LABELS) + " is required"
            )
        targets.append(
            {
                "number": number,
                "title": entry.get("title", ""),
                "milestone": milestone_title(entry),
                "original_labels": names,
                "status_label": statuses[0],
                "steps": {"added_release": False, "removed_source": False},
            }
        )

    targets.sort(key=lambda issue: issue["number"])
    if not targets:
        err.write("note: no open source-labelled issues found; nothing to migrate\n")

    known = {issue["number"] for issue in targets}
    for entry in release_issues:
        number = int(entry["number"])
        if number in known:
            continue
        if milestone_title(entry) == SOURCE_MILESTONE and SOURCE_LABEL not in label_names(entry):
            err.write(
                f"note: issue #{number} already carries {TARGET_LABEL} without {SOURCE_LABEL}; "
                "treated as already migrated, not a new target\n"
            )

    manifest = {
        "repo": repo,
        "created_at": now_utc(),
        "release_label_existed": release_existed,
        "release_label_created": False,
        "issues": targets,
        "untouched_closed": sorted(untouched_closed),
        "phase": "planned",
    }
    save_manifest(manifest_path, manifest)
    out.write(
        f"plan: {len(targets)} target issue(s), {len(untouched_closed)} closed issue(s) untouched, "
        f"{TARGET_LABEL} label {'exists' if release_existed else 'will be created'}\n"
    )
    return 0


# ---------------------------------------------------------------------------
# apply
# ---------------------------------------------------------------------------
def ensure_release_label(gh, repo: str, manifest_path: str, manifest: dict, out) -> None:
    labels = list_labels(gh, repo)
    if any(label.get("name") == TARGET_LABEL for label in labels):
        return
    if manifest.get("release_label_created"):
        raise GhError(f"{TARGET_LABEL} was recorded as created but is now missing; refusing to continue")
    out.write(f"apply: creating label {TARGET_LABEL}\n")
    try:
        gh.request(
            "POST",
            f"repos/{repo}/labels",
            {"name": TARGET_LABEL, "color": RELEASE_COLOR, "description": RELEASE_DESCRIPTION},
        )
        created = True
    except GhError as exc:
        already = "already_exists" in str(exc) or exc.status == 422
        if not already:
            raise
        rechecked = list_labels(gh, repo)
        if not any(label.get("name") == TARGET_LABEL for label in rechecked):
            raise GhError(
                f"{TARGET_LABEL} create reported already_exists but a re-GET does not list it"
            ) from exc
        created = False
    if created:
        manifest["release_label_created"] = True
        save_manifest(manifest_path, manifest)


def verify_issue_state(gh, repo: str, number: int, *, release: bool, source: bool,
                       status_label: str) -> None:
    live = get_issue(gh, repo, number)
    current = set(label_names(live))
    problems = []
    if (TARGET_LABEL in current) != release:
        problems.append(f"{TARGET_LABEL} {'missing' if release else 'still present'}")
    if (SOURCE_LABEL in current) != source:
        problems.append(f"{SOURCE_LABEL} {'missing' if source else 'still present'}")
    if status_label not in current:
        problems.append(f"status label {status_label} changed")
    if problems:
        raise GhError(f"verification failed for #{number}: " + "; ".join(problems))


def cmd_apply(gh, repo: str, manifest_path: str, out, err) -> int:
    manifest = load_manifest(manifest_path)
    if manifest.get("repo") and manifest["repo"] != repo:
        raise ManifestError(f"manifest was planned for {manifest['repo']}, not {repo}")
    ensure_release_label(gh, repo, manifest_path, manifest, out)

    for issue in sorted(manifest["issues"], key=lambda item: int(item["number"])):
        number = int(issue["number"])
        original = set(issue["original_labels"])
        expected_states = allowed_label_states(issue["original_labels"])

        live = get_issue(gh, repo, number)
        if not is_open(live):
            raise GhError(f"issue #{number} is no longer open; aborting (manifest kept at {manifest_path})")
        if milestone_title(live) != issue.get("milestone"):
            raise GhError(
                f"issue #{number} milestone changed from {issue.get('milestone')!r}; aborting (manifest kept)"
            )
        current = frozenset(label_names(live))
        if current not in expected_states:
            raise GhError(
                f"concurrent edit on #{number}: labels {sorted(current)} are not one of "
                f"{describe_states(expected_states)}; aborting (manifest kept at {manifest_path})"
            )

        if TARGET_LABEL not in current:
            gh.request("POST", f"repos/{repo}/issues/{number}/labels", {"labels": [TARGET_LABEL]})
            verify_issue_state(gh, repo, number, release=True, source=SOURCE_LABEL in current,
                               status_label=issue["status_label"])
            issue["steps"]["added_release"] = True
            save_manifest(manifest_path, manifest)
            out.write(f"apply: #{number} +{TARGET_LABEL}\n")
        elif not issue["steps"]["added_release"]:
            issue["steps"]["added_release"] = True
            save_manifest(manifest_path, manifest)

        current = set(label_names(get_issue(gh, repo, number)))
        if SOURCE_LABEL in current:
            gh.request(
                "DELETE",
                f"repos/{repo}/issues/{number}/labels/{quote(SOURCE_LABEL, safe='')}",
            )
            verify_issue_state(gh, repo, number, release=True, source=False,
                               status_label=issue["status_label"])
            issue["steps"]["removed_source"] = True
            save_manifest(manifest_path, manifest)
            out.write(f"apply: #{number} -{SOURCE_LABEL}\n")
        elif not issue["steps"]["removed_source"]:
            issue["steps"]["removed_source"] = True
            save_manifest(manifest_path, manifest)

    set_phase(manifest_path, manifest, "labels-migrated")
    out.write(f"apply: {len(manifest['issues'])} issue(s) migrated to {TARGET_LABEL}\n")
    return 0


# ---------------------------------------------------------------------------
# validate
# ---------------------------------------------------------------------------
def cmd_validate(gh, repo: str, manifest_path: str, selector: str, out, err) -> int:
    manifest = load_manifest(manifest_path)
    if manifest.get("repo") and manifest["repo"] != repo:
        raise ManifestError(f"manifest was planned for {manifest['repo']}, not {repo}")

    problems: list[str] = []
    warnings: list[str] = []
    open_targets: list[int] = []

    for issue in sorted(manifest["issues"], key=lambda item: int(item["number"])):
        number = int(issue["number"])
        live = get_issue(gh, repo, number)
        if not is_open(live):
            warnings.append(f"#{number} is closed (closed by a human); label checks skipped")
            continue
        open_targets.append(number)
        current = label_names(live)
        if TARGET_LABEL not in current:
            problems.append(f"#{number} lacks {TARGET_LABEL}")
        if SOURCE_LABEL in current:
            problems.append(f"#{number} still carries {SOURCE_LABEL}")
        statuses = [name for name in current if name in STATUS_LABELS]
        if len(statuses) != 1:
            problems.append(f"#{number} has {len(statuses)} status label(s) {statuses}")
        elif statuses[0] != issue["status_label"]:
            warnings.append(
                f"#{number} status label is now {statuses[0]} (planned {issue['status_label']}); work progressed"
            )

    if selector:
        if not os.path.isfile(selector):
            problems.append(f"selector {selector} does not exist")
        else:
            proc = subprocess.run(
                [sys.executable, selector, "--self-check"], capture_output=True, text=True, timeout=300
            )
            if proc.returncode != 0:
                problems.append(
                    f"selector self-check failed rc={proc.returncode}: "
                    f"{(proc.stderr or proc.stdout).strip().splitlines()[-1:] or ['no output']}"
                )

    owned = {
        int(entry["number"])
        for entry in list_issues(gh, repo, f"state=open&labels={quote(TARGET_LABEL, safe='')}&per_page=100")
    }
    missing = [number for number in open_targets if number not in owned]
    for number in missing:
        problems.append(f"#{number} does not appear in the live ownership query (state=open labels={TARGET_LABEL})")

    for warning in warnings:
        err.write(f"warning: {warning}\n")
    for problem in problems:
        err.write(f"problem: {problem}\n")
    if problems:
        err.write(f"validate: FAILED ({len(problems)} problem(s))\n")
        return 2
    out.write(
        f"validate: OK ({len(open_targets)} open owned issue(s), "
        f"{len(manifest['issues']) - len(open_targets)} closed warning(s))\n"
    )
    return 0


# ---------------------------------------------------------------------------
# rollback
# ---------------------------------------------------------------------------
def manual_command(repo: str, number: int, issue: dict) -> str:
    original = set(issue["original_labels"])
    actions = []
    if SOURCE_LABEL in original:
        actions.append(f"--add-label {SOURCE_LABEL}")
    if TARGET_LABEL not in original:
        actions.append(f"--remove-label {TARGET_LABEL}")
    return f"gh issue edit {number} --repo {repo} " + " ".join(actions)


def cmd_rollback(gh, repo: str, manifest_path: str, out, err) -> int:
    manifest = load_manifest(manifest_path)
    if manifest.get("repo") and manifest["repo"] != repo:
        raise ManifestError(f"manifest was planned for {manifest['repo']}, not {repo}")

    order = sorted(manifest["issues"], key=lambda item: int(item["number"]), reverse=True)
    restored: list[int] = []
    failure: str | None = None

    for issue in order:
        number = int(issue["number"])
        if failure:
            break
        try:
            live = get_issue(gh, repo, number)
            current = set(label_names(live))
            original = set(issue["original_labels"])
            statuses = [name for name in sorted(current) if name in STATUS_LABELS]
            if len(statuses) == 1 and statuses[0] != issue["status_label"]:
                out.write(
                    f"note: #{number} status is now {statuses[0]} (original {issue['status_label']}); "
                    "keeping the current status\n"
                )
            if SOURCE_LABEL in original and SOURCE_LABEL not in current:
                gh.request(
                    "POST", f"repos/{repo}/issues/{number}/labels", {"labels": [SOURCE_LABEL]}
                )
                verify_issue_state(
                    gh, repo, number,
                    release=TARGET_LABEL in current,
                    source=True,
                    status_label=statuses[0] if statuses else issue["status_label"],
                )
                current.add(SOURCE_LABEL)
                out.write(f"rollback: #{number} +{SOURCE_LABEL}\n")
            if TARGET_LABEL not in original and TARGET_LABEL in current:
                gh.request(
                    "DELETE",
                    f"repos/{repo}/issues/{number}/labels/{quote(TARGET_LABEL, safe='')}",
                )
                verify_issue_state(
                    gh, repo, number,
                    release=False,
                    source=SOURCE_LABEL in current,
                    status_label=statuses[0] if statuses else issue["status_label"],
                )
                out.write(f"rollback: #{number} -{TARGET_LABEL}\n")
            restored.append(number)
        except (GhError, ManifestError) as exc:
            failure = f"#{number}: {exc}"

    if failure:
        err.write(f"rollback: FAILED at {failure}\n")
        not_restored = [issue for issue in order if int(issue["number"]) not in restored]
        err.write("rollback: restore these issues manually:\n")
        for issue in sorted(not_restored, key=lambda item: int(item["number"])):
            err.write(f"  {manual_command(repo, int(issue['number']), issue)}\n")
        err.write(f"rollback: manifest kept at {manifest_path}\n")
        return 3

    set_phase(manifest_path, manifest, "labels-rolled-back")
    out.write(f"rollback: restored original ownership labels for {len(restored)} issue(s)\n")
    return 0


# ---------------------------------------------------------------------------
# show-plan
# ---------------------------------------------------------------------------
def cmd_show_plan(manifest_path: str, out) -> int:
    manifest = load_manifest(manifest_path)
    out.write(f"LABEL PLAN repo={manifest.get('repo')} source={SOURCE_LABEL} target={TARGET_LABEL}\n")
    if manifest.get("release_label_existed"):
        out.write(f"release label: exists\n")
    elif manifest.get("release_label_created"):
        out.write(f"release label: created by this migration\n")
    else:
        out.write(f"release label: will be created (color {RELEASE_COLOR})\n")
    for issue in sorted(manifest["issues"], key=lambda item: int(item["number"])):
        out.write(f"  #{issue['number']} {issue['title']}: +{TARGET_LABEL} -{SOURCE_LABEL}\n")
    closed = manifest.get("untouched_closed") or []
    if closed:
        out.write("untouched closed: " + " ".join(f"#{number}" for number in closed) + "\n")
    return 0


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------
def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="autonomy_migration.py",
        description="One-time GitHub ownership-label migration (tendo:v5-autonomous -> release).",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name, help_text in (
        ("plan", "inventory the source backlog into a manifest (read-only)"),
        ("apply", "apply the manifest labels with verify-after-write"),
        ("validate", "verify the migrated state and the selector self-check"),
        ("rollback", "restore the original ownership labels"),
        ("show-plan", "print the redacted human label plan"),
    ):
        sub = subparsers.add_parser(name, help=help_text)
        sub.add_argument("--manifest", required=True, help="manifest file path")
        sub.add_argument("--repo", default=DEFAULT_REPO, help=f"OWNER/NAME (default {DEFAULT_REPO})")
        if name == "validate":
            sub.add_argument("--selector", default=DEFAULT_SELECTOR, help="work-selection script for --self-check")
    return parser


def main(argv: list[str] | None = None, gh=None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    runner = gh if gh is not None else GhCli()
    try:
        if args.command == "plan":
            return cmd_plan(runner, args.repo, args.manifest, sys.stdout, sys.stderr)
        if args.command == "apply":
            return cmd_apply(runner, args.repo, args.manifest, sys.stdout, sys.stderr)
        if args.command == "validate":
            return cmd_validate(runner, args.repo, args.manifest, args.selector, sys.stdout, sys.stderr)
        if args.command == "rollback":
            return cmd_rollback(runner, args.repo, args.manifest, sys.stdout, sys.stderr)
        if args.command == "show-plan":
            return cmd_show_plan(args.manifest, sys.stdout)
        parser.error(f"unknown command {args.command}")
        return 2
    except (GhError, ManifestError, OSError, ValueError) as exc:
        print(f"MIGRATION ERROR: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
