"""Keep the optional destructive smoke test aligned with the API contract."""

from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class SmokeScriptTests(unittest.TestCase):
    def test_mutations_use_explicit_supported_methods(self):
        script = (ROOT / "scripts/smoke-new-world.sh").read_text(encoding="utf-8")
        commands = script.replace("\\\n", " ").splitlines()
        checked = 0
        for command in commands:
            if "curl " not in command:
                continue
            expected = None
            if "/api/server/stop" in command:
                expected = "POST"
            elif "/api/saves/rm/" in command:
                expected = "DELETE"
            if expected:
                checked += 1
                with self.subTest(command=command):
                    self.assertRegex(command, rf"--request\s+{expected}\b")
        self.assertEqual(checked, 6, "cover normal and trap-cleanup requests")

    def test_requires_disposable_target_and_tracks_created_save(self):
        script = (ROOT / "scripts/smoke-new-world.sh").read_text(encoding="utf-8")
        self.assertIn('"${FSM_SMOKE_DISPOSABLE:-}"', script)
        self.assertIn("save_created=false", script)
        self.assertIn("save_created=true", script)
        self.assertRegex(script, r'if \[ "\$save_created" = true \]; then\s+curl[^\n]+/api/saves/rm/\$test_save')


if __name__ == "__main__":
    unittest.main()
