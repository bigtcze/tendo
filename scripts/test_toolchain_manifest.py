#!/usr/bin/env python3
"""Fail when a toolchain pin in the repository drifts from toolchain.json."""
from __future__ import annotations

import json
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parent.parent
MANIFEST = json.loads((ROOT / "toolchain.json").read_text())
WORKFLOW = ".github/workflows/required.yml"


def read(path: str) -> str:
    return (ROOT / path).read_text()


def active_lines(text: str) -> list[str]:
    """Lines with YAML/shell comments removed, so commented-out pins never count."""
    return [re.sub(r"(?:^|\s)#.*$", "", line) for line in text.splitlines()]


def yaml_values(text: str, key: str) -> list[str]:
    """Every active value for `key:` (optionally a list item), with YAML quotes removed."""
    pattern = re.compile(rf"^\s*(?:-\s+)?{re.escape(key)}:\s*(.*?)\s*$")
    values = []
    for line in active_lines(text):
        match = pattern.match(line)
        if match:
            value = match.group(1)
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "'\"":
                value = value[1:-1]
            values.append(value)
    return values


def workflow_errors(text: str) -> list[str]:
    errors = []
    uses = yaml_values(text, "uses")
    for key, action, expected in (("go-version", "actions/setup-go", MANIFEST["go"]), ("node-version", "actions/setup-node", MANIFEST["node"])):
        values = yaml_values(text, key)
        steps = sum(1 for value in uses if value.startswith(f"{action}@"))
        if steps == 0 or len(values) != steps:
            errors.append(f"{key}: {len(values)} pins for {steps} {action} steps")
        errors += [f"{key} {value} != {expected}" for value in values if value != expected]
    for value in uses:
        action, _, version = value.partition("@")
        if action not in MANIFEST["actions"]:
            errors.append(f"action not in toolchain.json: {value}")
        elif version != MANIFEST["actions"][action]:
            errors.append(f"{action}@{version} != {MANIFEST['actions'][action]}")
    images = yaml_values(text, "image")
    if not images or any(image != MANIFEST["images"]["postgres"] for image in images):
        errors.append(f"service images {images} != {MANIFEST['images']['postgres']}")
    sqlc = MANIFEST["sqlc"]
    active = "\n".join(active_lines(text))
    archive = f"sqlc_{sqlc['version']}_linux_amd64.tar.gz"
    download = f"releases/download/v{sqlc['version']}/{archive}"
    verify = f"printf '%s  %s\\n' '{sqlc['linuxAmd64Sha256']}' \"$archive\" | sha256sum -c -"
    extract = 'tar -xzf "$archive"'
    for marker in (f'archive="$RUNNER_TEMP/{archive}"', download, verify, extract, f"sqlc\" version | grep -Fx 'v{sqlc['version']}'"):
        if marker not in active:
            errors.append(f"active sqlc install is missing: {marker}")
    if verify in active and extract in active and active.index(verify) > active.index(extract):
        errors.append("sqlc archive is extracted before SHA-256 verification")
    if len(set(re.findall(r"sqlc_(\d+\.\d+\.\d+)_linux_amd64", active))) > 1:
        errors.append("conflicting sqlc archive versions")
    return errors


class ToolchainManifestTests(unittest.TestCase):
    def assert_all(self, pattern: str, text: str, expected: str, where: str):
        found = set(re.findall(pattern, "\n".join(active_lines(text))))
        self.assertTrue(found, f"no pin matching {pattern!r} in {where}")
        self.assertEqual(found, {expected}, f"{where} pins {sorted(found)}, manifest says {expected}")

    def test_workflow_pins(self):
        errors = workflow_errors(read(WORKFLOW))
        self.assertEqual(errors, [], "\n".join(errors))

    def test_workflow_drift_is_detected(self):
        text = read(WORKFLOW)
        sha = MANIFEST["sqlc"]["linuxAmd64Sha256"]
        verify_line = next(line for line in text.splitlines() if sha in line)
        mutations = {
            "one go pin, double quoted": text.replace(f"go-version: '{MANIFEST['go']}'", 'go-version: "1.27.0"', 1),
            "one node pin, unquoted": text.replace(f"node-version: '{MANIFEST['node']}'", "node-version: 24.0.0", 1),
            "one postgres image": text.replace(f"image: {MANIFEST['images']['postgres']}", 'image: "postgres:17.1-bookworm"', 1),
            "go pin commented out": text.replace(f"go-version: '{MANIFEST['go']}'", f"# go-version: '{MANIFEST['go']}'", 1),
            "action major": text.replace("uses: actions/checkout@", 'uses: "actions/checkout@v9"  #', 1),
            "checksum only in a comment": text.replace(verify_line, verify_line.split("printf")[0] + f"true  # {sha}", 1),
            "wrong checksum": text.replace(sha, "0" * 64, 1),
            "extract before verify": text.replace(verify_line + "\n", "", 1).replace('echo "$RUNNER_TEMP/bin" >> "$GITHUB_PATH"', 'echo "$RUNNER_TEMP/bin" >> "$GITHUB_PATH"\n' + verify_line, 1),
        }
        for name, mutated in mutations.items():
            with self.subTest(name):
                self.assertNotEqual(mutated, text)
                self.assertTrue(workflow_errors(mutated), name)

    def test_sqlc_pins(self):
        version = MANIFEST["sqlc"]["version"]
        script = read("scripts/check-sqlc.sh")
        self.assert_all(r'"\$version" != "v([^"]+)"', script, version, "check-sqlc.sh comparison")
        self.assert_all(r"expected v(\d+\.\d+\.\d+)", script, version, "check-sqlc.sh message")
        generated = sorted(path for path in (ROOT / "backend/internal").rglob("*.go") if "Code generated by sqlc" in path.read_text())
        self.assertTrue(generated, "no sqlc output found")
        for path in generated:
            self.assert_all(r"sqlc v(\d+\.\d+\.\d+)", path.read_text(), version, str(path.relative_to(ROOT)))

    def test_container_image_pins(self):
        images = MANIFEST["images"]
        dockerfile = read("Dockerfile")
        self.assertEqual(re.findall(r"^FROM (\S+)", dockerfile, re.M), [images["nodeBuild"], images["goBuild"], images["runtime"]])
        provider = read("backend/test/oidc-provider/Dockerfile")
        self.assertEqual(re.findall(r"^FROM (\S+)", provider, re.M), [images["oidcProviderBuild"], images["oidcProviderRuntime"]])
        compose = read("compose.yaml")
        self.assertEqual(set(yaml_values(compose, "image")), {images["postgres"]})
        proxy = read("scripts/proxy-smoke.sh")
        for name in ("caddy", "nginx", "traefik"):
            self.assert_all(rf"'({name}:[^']+)'", proxy, images[name], f"proxy-smoke.sh {name}")
        self.assertTrue(images["goBuild"].startswith(f"golang:{MANIFEST['go']}-"))
        self.assertEqual(images["oidcProviderBuild"], f"golang:{MANIFEST['go']}")
        self.assertTrue(images["nodeBuild"].startswith(f"node:{MANIFEST['node']}-"))
        self.assertTrue(images["postgres"].startswith(f"postgres:{MANIFEST['postgres']}-"))

    def test_go_and_generator_pins(self):
        go_mod = read("backend/go.mod")
        self.assert_all(r"(?m)^go (\S+)$", go_mod, MANIFEST["goModuleDirective"], "backend/go.mod")
        self.assertNotRegex(go_mod, r"(?m)^toolchain\s", "backend/go.mod must not override the pinned Go toolchain")
        self.assert_all(r"OAPI_CODEGEN_VERSION := (\S+)", read("api/Makefile"), MANIFEST["oapiCodegen"], "api/Makefile")
        generated_files = sorted((ROOT / "backend/internal").rglob("*.gen.go"))
        self.assertTrue(generated_files, "no oapi-codegen output found")
        for generated in generated_files:
            self.assert_all(r"oapi-codegen/v2 version (\S+) DO NOT EDIT", generated.read_text(), MANIFEST["oapiCodegen"], str(generated.relative_to(ROOT)))

    def test_node_package_pins(self):
        api = json.loads(read("api/package.json"))["devDependencies"]
        self.assertEqual(api["openapi-typescript"], MANIFEST["openapiTypescript"])
        frontend = json.loads(read("frontend/package.json"))["devDependencies"]
        self.assertEqual(frontend["@playwright/test"], MANIFEST["playwright"])
        smoke = read("scripts/e2e-smoke.sh")
        self.assert_all(r'"\$installed" != (\S+) \]\]', smoke, MANIFEST["playwright"], "e2e-smoke.sh comparison")
        self.assert_all(r"expected (\d+\.\d+\.\d+), found", smoke, MANIFEST["playwright"], "e2e-smoke.sh message")


if __name__ == "__main__":
    unittest.main()
