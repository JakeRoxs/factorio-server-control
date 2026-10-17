import contextlib
import importlib.util
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch
import zipfile


SCRIPT = Path(__file__).resolve().parents[1] / "test-container-lifecycle.py"
spec = importlib.util.spec_from_file_location("container_lifecycle", SCRIPT)
lifecycle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lifecycle)


class ContainerLifecycleSafetyTests(unittest.TestCase):
    def test_explicit_disposable_exact_version_and_no_existing_endpoint(self):
        base = ["--version", "2.0.77", "--mode", "factorio", "--layout", "split"]
        for args in (base, ["--disposable", *base, "--url", "https://existing.example"],
                     ["--disposable", *base, "--version", "stable"]):
            with self.subTest(args=args), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit):
                    lifecycle.parse_args(args)
        self.assertEqual(lifecycle.parse_args(["--disposable", *base]).version, "2.0.77")

    def test_cleanup_requires_both_random_name_and_matching_ownership_label(self):
        token = "a" * 32
        name = "fsm-quality-" + token
        for kind, target, metadata in (
            ("container", name, {"Config": {"Labels": {lifecycle.LABEL: token}}}),
            ("volume", name + "-manager", {"Labels": {lifecycle.LABEL: token}}),
        ):
            self.assertTrue(lifecycle.owned_resource(kind, target, token, metadata))
            self.assertFalse(lifecycle.owned_resource(kind, "production", token, metadata))
            self.assertFalse(lifecycle.owned_resource(kind, target, "b" * 32, metadata))
            self.assertFalse(lifecycle.owned_resource(kind, target, token, {}))

    def test_cleanup_does_not_remove_unowned_resources(self):
        manager = lifecycle.DisposableManager("fixture:ci", "2.0.77", "split")
        with patch.object(manager, "inspect", return_value={"Config": {"Labels": {}}}), \
                patch.object(manager, "docker") as docker:
            with self.assertRaisesRegex(AssertionError, "unowned"):
                manager.cleanup()
            docker.assert_not_called()

    def test_fixture_contains_only_original_settings_and_version_metadata(self):
        with zipfile.ZipFile(io.BytesIO(lifecycle.fixture_mod("2.1.20"))) as archive:
            self.assertEqual(len(archive.namelist()), 2)
            info = json.loads(archive.read(lifecycle.MOD + "_1.0.0/info.json"))
            self.assertEqual(info["factorio_version"], "2.1")
            self.assertEqual(info["dependencies"], ["base >= 2.1.20"])
            self.assertIsNone(archive.testzip())


if __name__ == "__main__":
    unittest.main()
