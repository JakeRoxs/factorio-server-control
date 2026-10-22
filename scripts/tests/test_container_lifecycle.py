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
    def test_managed_wait_targets_only_the_validated_server_id(self):
        manager = lifecycle.DisposableManager("fixture:ci", "2.0.77", "combined")
        with patch.object(manager, "api", return_value={"running": True, "rcon_connected": True, "stopping": False}) as api, \
                patch.object(manager, "wait_for", side_effect=lambda condition, *args: self.assertTrue(condition())):
            manager.wait_managed_game("2")
            api.assert_called_once_with("GET", "/servers/2")
        with patch.object(manager, "api") as api:
            with self.assertRaises(AssertionError):
                manager.wait_managed_game("../1")
            api.assert_not_called()

    def test_http_diagnostics_never_print_response_contents(self):
        manager = lifecycle.DisposableManager("fixture:ci", "2.0.77", "combined")
        manager.base = "http://127.0.0.1:12345"
        manager.opener = lifecycle.urllib.request.build_opener()
        error = lifecycle.urllib.error.HTTPError(manager.base, 400, "Bad Request", {}, io.BytesIO(b'{"error":"target mods cannot form a preset; password=private-value"}'))
        with patch.object(manager.opener, "open", side_effect=error):
            with self.assertRaises(lifecycle.HTTPFailure) as failed:
                manager.api("POST", "/servers/1/profiles", {})
        self.assertIn("target mods cannot form a preset", str(failed.exception))
        self.assertNotIn("private-value", str(failed.exception))
        self.assertNotIn("password", str(failed.exception))

    def test_allocator_check_reads_only_the_owned_game_process_maps(self):
        manager = lifecycle.DisposableManager("fixture:ci", "2.0.77", "combined")
        with patch.object(manager, "require_owned") as owned, \
                patch.object(manager, "docker", return_value=b"") as docker:
            self.assertTrue(manager.allocator_loaded())
            owned.assert_called_once_with("container", manager.name)
            self.assertEqual(docker.call_args.args[:4], ("exec", manager.name, "sh", "-c"))
            self.assertIn('"$name" = factorio', docker.call_args.args[4])
            self.assertIn("/maps", docker.call_args.args[4])
            self.assertIn("libmimalloc", docker.call_args.args[4])
        with patch.object(manager, "require_owned"), \
                patch.object(manager, "docker", return_value=None):
            self.assertFalse(manager.allocator_loaded())

    def test_fixture_publishes_the_explicit_manager_port(self):
        manager = lifecycle.DisposableManager("fixture:ci", "2.0.77", "split")
        with patch.object(manager, "inspect", return_value=None), \
                patch.object(manager, "require_owned"), \
                patch.object(manager, "wait_http"), \
                patch.object(manager, "docker") as docker:
            manager.create_container("2.0.77")
            arguments = docker.call_args.args
            self.assertIn("127.0.0.1::8080", arguments)
            self.assertIn("FSM_PORT=8080", arguments)
        with patch.object(manager, "docker", return_value=b"127.0.0.1:45678") as docker, \
                patch.object(manager, "wait_for"):
            manager.wait_http()
            docker.assert_called_once_with("port", manager.name, "8080/tcp")

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
