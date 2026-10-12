"""Keep the security-patched Go toolchain consistent across build paths."""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


class GoToolchainTests(unittest.TestCase):
    def test_build_and_ci_versions_match_go_mod(self):
        module = (ROOT / "src/go.mod").read_text(encoding="utf-8")
        version = re.search(r"^go (\d+\.\d+\.\d+)$", module, re.MULTILINE)
        self.assertIsNotNone(version, "go.mod must pin an exact Go patch version")
        expected = version.group(1)

        for filename in (
            "docker/Dockerfile.registry",
            "docker/Dockerfile-build",
            ".github/workflows/test-workflow.yml",
        ):
            with self.subTest(file=filename):
                content = (ROOT / filename).read_text(encoding="utf-8")
                images = re.findall(r"\bgolang:([^\s]+)", content)
                self.assertTrue(images, "expected a pinned Go container image")
                for image in images:
                    self.assertRegex(
                        image,
                        rf"^{re.escape(expected)}-[\w.-]+@sha256:[a-f0-9]{{64}}$",
                    )

        for filename in (
            ".github/workflows/test-workflow.yml",
            ".github/workflows/create-release-workflow.yml",
        ):
            with self.subTest(file=filename):
                content = (ROOT / filename).read_text(encoding="utf-8")
                checks = re.findall(r"go version \| grep -F 'go(\d+\.\d+\.\d+) '", content)
                self.assertTrue(checks, "expected an exact Go version check")
                self.assertEqual(set(checks), {expected})


if __name__ == "__main__":
    unittest.main()
