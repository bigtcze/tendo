#!/usr/bin/env python3
"""Regression tests for install-guide shell extraction and README alignment."""
from __future__ import annotations

from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "scripts"))
from quickstart_docs import extract_blocks
INSTALL = (ROOT / "docs/admin/install.md").read_text(encoding="utf-8")
README = (ROOT / "README.md").read_text(encoding="utf-8")


def mutated_readme(replace: str, with_: str) -> str:
    if replace not in README:
        raise AssertionError(f"README fixture text not found: {replace!r}")
    return README.replace(replace, with_, 1)


class QuickstartDocsTests(unittest.TestCase):
    def test_real_documents_extract_expected_ordered_blocks(self):
        blocks = extract_blocks(INSTALL, README, str(ROOT))
        self.assertEqual(len(blocks), 7)
        self.assertIn("git clone --quiet --no-hardlinks --depth 1 file://", blocks[0])
        self.assertIn("curl --fail --show-error http://localhost:8080/health/ready", blocks[6])
        self.assertIn("docker compose up -d --force-recreate --wait app", blocks[5])

    def test_readme_missing_command_fails(self):
        with self.assertRaisesRegex(ValueError, "Quick start commands must exactly match"):
            extract_blocks(INSTALL, mutated_readme("cd tendo\n", ""), str(ROOT))

    def test_readme_reordered_commands_fail(self):
        with self.assertRaisesRegex(ValueError, "Quick start commands must exactly match"):
            extract_blocks(INSTALL, mutated_readme("umask 077\ncp .env.example .env", "cp .env.example .env\numask 077"), str(ROOT))

    def test_readme_empty_quick_start_fails(self):
        with self.assertRaisesRegex(ValueError, "README.md ## Quick start commands must exactly match|README.md: ## Quick start: expected 1 sh block"): 
            extract_blocks(INSTALL, mutated_readme("```sh\ngit clone https://github.com/bigtcze/tendo.git\ncd tendo\numask 077\ncp .env.example .env\nsed -i \\\n  -e \"s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(openssl rand -hex 32)/\" \\\n  -e \"s/^TENDO_DATABASE_PASSWORD=.*/TENDO_DATABASE_PASSWORD=$(openssl rand -hex 32)/\" \\\n  -e \"s|^TENDO_SETUP_TOKEN=.*|TENDO_SETUP_TOKEN=$(openssl rand -base64 32)|\" \\\n  .env\ndocker compose up -d --build --wait\n```", "```sh\n```"), str(ROOT))

    def test_bash_shell_fence_inside_install_section_fails(self):
        added = INSTALL.replace("## 3. Start Tendo\n", "## 3. Start Tendo\n\n```bash\necho unexpected\n```\n", 1)
        with self.assertRaisesRegex(ValueError, "expected exactly 7 shell-language fenced block"):
            extract_blocks(added, README, str(ROOT))

    def test_extra_shell_fence_after_install_steps_fails(self):
        added = INSTALL.replace("## How this is tested", "## 6. Extra\n\n```sh\necho unexpected\n```\n\n## How this is tested", 1)
        with self.assertRaisesRegex(ValueError, "shell-language fenced block outside expected sections"):
            extract_blocks(added, README, str(ROOT))

    def test_intervening_install_heading_fails(self):
        added = INSTALL.replace("## 2. Create your settings file", "## 2b. Extra\n\n## 2. Create your settings file", 1)
        with self.assertRaisesRegex(ValueError, "unexpected heading between required sections"):
            extract_blocks(added, README, str(ROOT))

    def test_get_tendo_directory_transition_is_verified_by_smoke_not_extraction(self):
        """The smoke appends pwd -P and checks it equals the clone path after block 1."""
        self.assertIn('"${blocks[0]}"$\'\\n\'"pwd -P"', (ROOT / "scripts/quickstart-smoke.sh").read_text())


if __name__ == "__main__":
    unittest.main(verbosity=2)
