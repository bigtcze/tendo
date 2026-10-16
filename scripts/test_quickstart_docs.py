#!/usr/bin/env python3
"""Regression tests for install-guide shell extraction and README alignment."""
from __future__ import annotations

from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "scripts"))
from quickstart_docs import RELEASE_DOWNLOAD_URL, extract_blocks
INSTALL = (ROOT / "docs/admin/install.md").read_text(encoding="utf-8")
README = (ROOT / "README.md").read_text(encoding="utf-8")
ASSETS = "file:///tmp/tendo-release-assets/"


def mutated_readme(replace: str, with_: str) -> str:
    if replace not in README:
        raise AssertionError(f"README fixture text not found: {replace!r}")
    return README.replace(replace, with_, 1)


class QuickstartDocsTests(unittest.TestCase):
    def test_real_documents_extract_expected_ordered_blocks(self):
        blocks = extract_blocks(INSTALL, README, ASSETS)
        self.assertEqual(len(blocks), 7)
        self.assertIn(f"curl -fsSLO {ASSETS}compose.yaml", blocks[0])
        self.assertIn(f"curl -fsSL -o .env {ASSETS}example.env", blocks[1])
        self.assertNotIn(RELEASE_DOWNLOAD_URL, "\n".join(blocks))
        self.assertIn("curl --fail --show-error http://localhost:8080/health/ready", blocks[6])
        self.assertIn("docker compose up -d --force-recreate --wait app", blocks[5])

    def test_readme_missing_command_fails(self):
        with self.assertRaisesRegex(ValueError, "Quick start commands must exactly match"):
            extract_blocks(INSTALL, mutated_readme("cd tendo\n", ""), ASSETS)

    def test_readme_reordered_commands_fail(self):
        with self.assertRaisesRegex(ValueError, "Quick start commands must exactly match"):
            extract_blocks(INSTALL, mutated_readme("mkdir tendo\ncd tendo\n", "cd tendo\nmkdir tendo\n"), ASSETS)

    def test_readme_empty_quick_start_fails(self):
        with self.assertRaisesRegex(ValueError, "README.md ## Quick start commands must exactly match|README.md: ## Quick start: expected 1 sh block"): 
            extract_blocks(INSTALL, README[:README.index("```sh", README.index("## Quick start"))] + "```sh\n```" + README[README.index("```", README.index("```sh", README.index("## Quick start")) + 5) + 3:], ASSETS)

    def test_bash_shell_fence_inside_install_section_fails(self):
        added = INSTALL.replace("## 3. Start Tendo\n", "## 3. Start Tendo\n\n```bash\necho unexpected\n```\n", 1)
        with self.assertRaisesRegex(ValueError, "expected exactly 7 shell-language fenced block"):
            extract_blocks(added, README, ASSETS)

    def test_extra_shell_fence_after_install_steps_fails(self):
        added = INSTALL.replace("## How this is tested", "## 6. Extra\n\n```sh\necho unexpected\n```\n\n## How this is tested", 1)
        with self.assertRaisesRegex(ValueError, "shell-language fenced block outside expected sections"):
            extract_blocks(added, README, ASSETS)

    def test_intervening_install_heading_fails(self):
        added = INSTALL.replace("## 2. Create your settings file", "## 2b. Extra\n\n## 2. Create your settings file", 1)
        with self.assertRaisesRegex(ValueError, "unexpected heading between required sections"):
            extract_blocks(added, README, ASSETS)

    def test_readme_continuation_with_trailing_space_fails(self):
        with self.assertRaisesRegex(ValueError, "Quick start commands must exactly match"):
            extract_blocks(INSTALL, mutated_readme("sed -i \\\n", "sed -i \\ \n"), ASSETS)

    def test_tilde_shell_fence_inside_install_section_fails(self):
        added = INSTALL.replace("## 3. Start Tendo\n", "## 3. Start Tendo\n\n~~~sh\nnonexistent-command\n~~~\n", 1)
        with self.assertRaises(ValueError):
            extract_blocks(added, README, ASSETS)

    def test_four_backtick_shell_fence_inside_install_section_fails(self):
        added = INSTALL.replace("## 3. Start Tendo\n", "## 3. Start Tendo\n\n````sh\nnonexistent-command\n````\n", 1)
        with self.assertRaises(ValueError):
            extract_blocks(added, README, ASSETS)

    def test_release_download_other_than_compose_and_example_env_fails(self):
        changed = INSTALL.replace("download/example.env", "download/other.env", 1)
        with self.assertRaisesRegex(ValueError, "must download exactly"):
            extract_blocks(changed, README.replace("download/example.env", "download/other.env", 1), ASSETS)

    def test_download_from_a_pinned_release_instead_of_latest_fails(self):
        pinned = "https://github.com/bigtcze/tendo/releases/download/v1.0.0/"
        changed_install = INSTALL.replace(RELEASE_DOWNLOAD_URL + "compose.yaml", pinned + "compose.yaml", 1)
        changed_readme = README.replace(RELEASE_DOWNLOAD_URL + "compose.yaml", pinned + "compose.yaml", 1)
        with self.assertRaisesRegex(ValueError, "must download exactly"):
            extract_blocks(changed_install, changed_readme, ASSETS)

    def test_get_tendo_directory_transition_is_verified_by_smoke_not_extraction(self):
        """The smoke appends pwd -P and checks it equals the clone path after block 1."""
        self.assertIn('"${blocks[0]}"$\'\\n\'"pwd -P"', (ROOT / "scripts/quickstart-smoke.sh").read_text())


if __name__ == "__main__":
    unittest.main(verbosity=2)
