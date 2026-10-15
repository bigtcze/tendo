#!/usr/bin/env python3
"""Extract and validate the documented quick-start shell commands."""
from __future__ import annotations

import json
from pathlib import Path
import re
import sys

INSTALL_HEADINGS = (
    "## 1. Get Tendo",
    "## 2. Create your settings file",
    "## 3. Start Tendo",
    "## 4. Create your household",
    "## 5. Switch setup off",
)
README_HEADINGS = ("## Quick start", "## First sign-in")
SHELL_LANGUAGES = {"", "sh", "bash", "shell", "console", "zsh"}
INSTALL_COUNTS = (1, 2, 2, 1, 1)


def _fences(text: str) -> list[tuple[str, str, int, int]]:
    """Return CommonMark-style fenced blocks (backtick or tilde, length >= 3) as (info, body, start, end)."""
    lines = text.splitlines()
    result = []
    i = 0
    while i < len(lines):
        match = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", lines[i])
        if not match or (match.group(1)[0] == "`" and "`" in match.group(2)):
            i += 1
            continue
        start = i
        fence = match.group(1)
        info_text = match.group(2).strip()
        info = info_text.split(maxsplit=1)[0].lower() if info_text else ""
        closing = re.compile(r"^ {0,3}" + re.escape(fence[0]) + "{" + str(len(fence)) + r",}\s*$")
        i += 1
        body_start = i
        while i < len(lines) and not closing.match(lines[i]):
            i += 1
        if i == len(lines):
            raise ValueError(f"unclosed fenced code block at line {start + 1}")
        result.append((info, "\n".join(lines[body_start:i]), start, i))
        i += 1
    return result


def _section_ranges(text: str, headings: tuple[str, ...], label: str) -> tuple[list[str], list[tuple[int, int]]]:
    lines = text.splitlines()
    positions = []
    for heading in headings:
        matches = [i for i, line in enumerate(lines) if line.strip() == heading]
        if len(matches) != 1:
            raise ValueError(f"{label}: expected exactly one heading {heading!r}; found {len(matches)}")
        positions.append(matches[0])
    ranges = []
    sections = []
    for idx, (heading, start) in enumerate(zip(headings, positions)):
        if idx and start <= positions[idx - 1]:
            raise ValueError(f"{label}: headings are out of order at {heading}")
        end = positions[idx + 1] if idx + 1 < len(positions) else next(
            (i for i in range(start + 1, len(lines)) if re.match(r"^##\s+", lines[i])), len(lines)
        )
        if label == "docs/admin/install.md" and idx + 1 < len(positions):
            unexpected = next((i for i in range(start + 1, end) if re.match(r"^##\s+", lines[i])), None)
            if unexpected is not None:
                raise ValueError(f"{label}: unexpected heading between required sections: {lines[unexpected].strip()}")
        sections.append("\n".join(lines[start + 1:end]))
        ranges.append((start + 1, end))
    return sections, ranges


def _commands(block: str) -> list[str]:
    joined = re.sub(r"\\\n\s*", " ", block)
    return [line.strip() for line in joined.splitlines() if line.strip()]


def _shell_fence_counts(text: str, ranges: list[tuple[int, int]], label: str, expected_total: int) -> None:
    fences = _fences(text)
    shell_fences = [f for f in fences if f[0] in SHELL_LANGUAGES]
    outside = [f for f in shell_fences if not any(start <= f[2] and f[3] < end for start, end in ranges)]
    if outside:
        raise ValueError(f"{label}: shell-language fenced block outside expected sections (line {outside[0][2] + 1})")
    if len(shell_fences) != expected_total:
        raise ValueError(f"{label}: expected exactly {expected_total} shell-language fenced block(s), found {len(shell_fences)}")
    for fence in shell_fences:
        if fence[0] != "sh":
            raise ValueError(f"{label}: expected sh fence, found {fence[0] or '(empty info string)'} at line {fence[2] + 1}")


def extract_blocks(install_text: str, readme_text: str, repo_root: str) -> list[str]:
    """Validate docs and return ordered install blocks; only substitute the clone URL."""
    install_sections, install_ranges = _section_ranges(install_text, INSTALL_HEADINGS, "docs/admin/install.md")
    install_blocks: list[list[str]] = []
    for heading, section, expected in zip(INSTALL_HEADINGS, install_sections, INSTALL_COUNTS):
        blocks = [body for info, body, _, _ in _fences(section) if info == "sh"]
        if len(blocks) != expected:
            raise ValueError(f"docs/admin/install.md: {heading}: expected {expected} sh block(s), found {len(blocks)}")
        install_blocks.append(blocks)
    _shell_fence_counts(install_text, install_ranges, "docs/admin/install.md", 7)

    readme_sections, readme_ranges = _section_ranges(readme_text, README_HEADINGS, "README.md")
    readme_blocks: list[list[str]] = []
    for heading, section, expected in zip(README_HEADINGS, readme_sections, (1, 2)):
        blocks = [body for info, body, _, _ in _fences(section) if info == "sh"]
        if len(blocks) != expected:
            raise ValueError(f"README.md: {heading}: expected {expected} sh block(s), found {len(blocks)}")
        readme_blocks.append(blocks)
    _shell_fence_counts(readme_text, readme_ranges, "README.md", 3)

    expected_quick = sum((_commands(block) for block in install_blocks[0] + install_blocks[1] + [install_blocks[2][0]]), [])
    actual_quick = sum((_commands(block) for block in readme_blocks[0]), [])
    if not actual_quick or actual_quick != expected_quick:
        raise ValueError("README.md ## Quick start commands must exactly match install sections 1, 2, and section 3's first block in order")
    expected_signin = sum((_commands(block) for block in install_blocks[3] + install_blocks[4]), [])
    actual_signin = sum((_commands(block) for block in readme_blocks[1]), [])
    if not actual_signin or actual_signin != expected_signin:
        raise ValueError("README.md ## First sign-in commands must exactly match install sections 4 and 5 in order")

    get_block = install_blocks[0][0]
    clone_command = "git clone https://github.com/bigtcze/tendo.git"
    if get_block.splitlines().count(clone_command) != 1:
        raise ValueError("docs/admin/install.md Get Tendo: expected exactly one exact GitHub clone command line")
    install_blocks[0][0] = get_block.replace(
        clone_command,
        f"git clone --quiet --no-hardlinks --depth 1 file://{repo_root} tendo",
        1,
    )
    # Block order: section 3's health block follows all five documented sections.
    return [
        install_blocks[0][0],
        *install_blocks[1],
        install_blocks[2][0],
        install_blocks[3][0],
        install_blocks[4][0],
        install_blocks[2][1],
    ]


def main() -> int:
    root = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
    try:
        blocks = extract_blocks(
            (root / "docs/admin/install.md").read_text(encoding="utf-8"),
            (root / "README.md").read_text(encoding="utf-8"),
            str(root),
        )
    except (OSError, ValueError) as error:
        print(error, file=sys.stderr)
        return 1
    # JSON preserves multiline shell blocks without eval or secret-bearing diagnostics.
    print(json.dumps(blocks))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
