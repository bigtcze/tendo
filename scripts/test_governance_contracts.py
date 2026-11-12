#!/usr/bin/env python3
"""Offline consolidation and Git-preservation regression checks.

No host services, external network, product DB or browser resources. Historical
contract fingerprints protect exact golden decisions; semantic review remains
mandatory because string assertions cannot prove all prose equivalence.
"""
from pathlib import Path
import hashlib
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
CONTRACTS = ROOT / '.autonomous'


class CanonicalContracts(unittest.TestCase):
    def test_no_versioned_governance_required_on_current_tree(self):
        self.assertFalse(list(CONTRACTS.glob('V[2345]*.md')))
        for name in ('CYCLE_PROMPT.md', 'V3_ISSUES_READY'):
            self.assertFalse((CONTRACTS / name).exists())
        for name in ('autonomous-loop.sh', 'v5_work_selection.py'):
            self.assertFalse((ROOT / 'scripts' / name).exists())
        agents = (ROOT / 'AGENTS.md').read_text()
        self.assertIn('Canonical contracts and compatibility', agents)
        for name in ('PRODUCT', 'RECURRENCE', 'SECURITY', 'UX', 'TESTING',
                     'DEFINITION_OF_DONE', 'ARCHITECTURE', 'API', 'FLOW'):
            self.assertTrue((CONTRACTS / f'{name}.md').is_file())

    def test_recurrence_golden_table_preserved_exactly(self):
        rows = '\n'.join(line for line in (CONTRACTS / 'RECURRENCE.md').read_text().splitlines()
                         if line.startswith('|'))
        self.assertEqual(hashlib.sha256(rows.encode()).hexdigest(),
                         '1c5f2ab571ba0cfafac5994d92221e21387da3e1cf923a2aa009c2a3e0fbaa5f')
        text = (CONTRACTS / 'RECURRENCE.md').read_text()
        for invariant in ('1, 2, 3, 6, 12 months', '1, 7, 14 days', '2000 and 2100',
                          'real-PostgreSQL', 'MCP tool path', 'intervening changed state'):
            self.assertIn(invariant, text)

    def test_security_quality_and_migration_boundaries_remain_binding(self):
        security = (CONTRACTS / 'SECURITY.md').read_text()
        for invariant in ('BOTH sides', 'match is unique', 'issuer, subject',
                          'LOCAL authenticated session', 'OIDC cannot use this flow',
                          'PKCE', 'admin_max_scopes ∩ user_selected_scopes',
                          'FAIL CLOSED', 'atomic under concurrency'):
            self.assertIn(invariant, security)
        testing = (CONTRACTS / 'TESTING.md').read_text()
        for invariant in ('no Docker for native runs', 'real PostgreSQL', 'race detector',
                          'production-built Tendo', 'zero ignored failures',
                          'state-machine/model/property', 'screen-reader'):
            self.assertIn(invariant, testing)
        architecture = (CONTRACTS / 'ARCHITECTURE.md').read_text()
        for invariant in ('two', 'restricted', 'privileged', '127.0.0.1'):
            self.assertIn(invariant, architecture)
        flow = (CONTRACTS / 'FLOW.md').read_text()
        for invariant in ('Dependency recovery', 'unchanged acceptance',
                          'No premature closure', 'No force push', 'explicit RELEASE/HANDOFF'):
            self.assertIn(invariant, flow)

    def test_historical_completion_markers_are_byte_identical(self):
        # Fixed pre-consolidation evidence, also runnable in shallow CI clones.
        fingerprints = {
            'PROJECT_COMPLETE': 'e60e0c1e756a2a8babb06c7a538313656310b4dfb30dc804277fe9931fbce9c4',
            'V2_COMPLETE': '977043ff29829895574d1d7ce5e10b9b44347dcfbf4fecbfe5ce0fa66007d231',
            'V2_1_COMPLETE': '4f88f22224d670fea7bb61fc585ada25f5c272a2d4dae6738dcb3a949acb6dd5',
            'V3_COMPLETE': 'c08c71af625906aabedab151902cef9530cf79d488eae934cd4ad33715198020',
            'V4_COMPLETE': 'dcfbe310c4a1cb9aff9f3e12f709ec2b334af2d425bf33bbc25eb7035eeb7e2e',
        }
        for name, fingerprint in fingerprints.items():
            self.assertEqual(hashlib.sha256((CONTRACTS / name).read_bytes()).hexdigest(),
                             fingerprint)

    def test_controller_has_no_git_mutation_or_stage_file_dependency(self):
        controller = (ROOT / 'scripts/autonomy-controller.sh').read_text()
        selector = (ROOT / 'scripts/autonomy-work-selection.py').read_text()
        for text in (controller, selector):
            for unsafe in ('git push', 'git reset', 'git clean', '--force-with-lease',
                           'git checkout', 'V5_CYCLE_PROMPT.md', 'V5_ISSUES_WORKFLOW.md'):
                self.assertNotIn(unsafe, text)


class ConcurrentGitSafety(unittest.TestCase):
    def test_normal_push_rejection_preserves_concurrent_work(self):
        # A real local bare remote, not a fake success. Prove the documented
        # non-force procedure rejects stale writes and preserves both histories.
        with tempfile.TemporaryDirectory(prefix='tendo-git-safety-') as temp:
            root = Path(temp)
            remote, first, second = (root / n for n in ('remote.git', 'first', 'second'))

            def git(cwd, *args, check=True):
                return subprocess.run(['git', *args], cwd=cwd, check=check,
                                      capture_output=True, text=True)

            git(root, 'init', '--bare', str(remote))
            git(root, 'clone', str(remote), str(first))
            git(first, 'config', 'user.name', 'test')
            git(first, 'config', 'user.email', 'test@example.invalid')
            (first / 'base').write_text('base\n')
            git(first, 'add', '.')
            git(first, 'commit', '-m', 'base')
            git(first, 'push', 'origin', 'HEAD:refs/heads/work')
            git(root, 'clone', '--branch', 'work', str(remote), str(second))
            git(second, 'config', 'user.name', 'test')
            git(second, 'config', 'user.email', 'test@example.invalid')
            (second / 'other-agent').write_text('must survive\n')
            git(second, 'add', '.')
            git(second, 'commit', '-m', 'concurrent work')
            git(second, 'push', 'origin', 'HEAD:work')
            remote_head = git(second, 'rev-parse', 'HEAD').stdout.strip()
            (first / 'our-work').write_text('also preserved\n')
            git(first, 'add', '.')
            git(first, 'commit', '-m', 'our work')
            local_head = git(first, 'rev-parse', 'HEAD').stdout.strip()
            rejection = git(first, 'push', 'origin', 'HEAD:work', check=False)
            self.assertNotEqual(rejection.returncode, 0)
            self.assertIn('[rejected]', rejection.stderr)
            self.assertEqual(git(root, '--git-dir', str(remote), 'rev-parse', 'work').stdout.strip(),
                             remote_head)
            self.assertEqual(git(first, 'rev-parse', 'HEAD').stdout.strip(), local_head)
            self.assertEqual(git(root, '--git-dir', str(remote), 'show', 'work:other-agent').stdout,
                             'must survive\n')
            self.assertEqual((first / 'our-work').read_text(), 'also preserved\n')


if __name__ == '__main__':
    unittest.main()
