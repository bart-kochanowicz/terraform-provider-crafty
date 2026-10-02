"""Verify isolation and credential handling in the CI helper."""

import io
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError

import ci


class IntegrationTest(unittest.TestCase):
    def runner(self):
        with patch.dict(os.environ, {"COMPOSE_PROJECT_NAME": "crafty-provider-ci-unit"}):
            return ci.Integration()

    def test_refuses_local_development_project(self):
        for project in ("", "crafty-provider-dev", "crafty-provider-ci-", "../dev"):
            with self.subTest(project=project):
                with patch.dict(os.environ, {"COMPOSE_PROJECT_NAME": project}):
                    with self.assertRaisesRegex(RuntimeError, "unique crafty-provider-ci"):
                        ci.Integration()

    def test_redacts_password_and_tokens(self):
        runner = self.runner()
        with patch.dict(os.environ, {"GITHUB_ACTIONS": "true"}):
            with patch("sys.stdout", new_callable=io.StringIO) as stdout:
                runner.mask("secret%password")
                self.assertIn("::add-mask::secret%25password", stdout.getvalue())
        self.assertEqual(
            runner.redact("secret%password Bearer eyJhbGci.eyJ1c2Vy.signature"),
            "[REDACTED] Bearer [REDACTED]",
        )
        with self.assertRaises(RuntimeError):
            runner.mask("unsafe\nworkflow command")

    def test_http_error_omits_response_body(self):
        runner = self.runner()
        error = HTTPError("http://localhost", 403, "Forbidden", {},
                          io.BytesIO(b'{"password":"do-not-print"}'))
        with patch("urllib.request.urlopen", side_effect=error):
            with self.assertRaisesRegex(RuntimeError, "returned HTTP 403") as result:
                runner.api("POST", "auth/login", {"password": "do-not-print"})
        self.assertNotIn("do-not-print", str(result.exception))

    def test_logs_survive_startup_failure(self):
        runner = self.runner()
        with tempfile.TemporaryDirectory() as directory:
            runner.output = Path(directory)
            with patch.object(runner, "credentials", side_effect=RuntimeError("not ready")):
                with patch.object(runner, "capture", side_effect=RuntimeError("container unavailable")):
                    runner.logs()
            self.assertEqual(len(list(runner.output.iterdir())), 3)
            self.assertIn("container unavailable", (runner.output / "compose.log").read_text())

    def test_local_run_cleans_up_after_bootstrap_failure(self):
        runner = self.runner()
        with patch("sys.argv", ["ci.py", "run"]), patch("ci.Integration", return_value=runner):
            with patch.object(runner, "up"), patch.object(runner, "test", side_effect=RuntimeError("bootstrap failed")):
                with patch.object(runner, "logs") as logs, patch.object(runner, "down") as down:
                    with self.assertRaisesRegex(RuntimeError, "bootstrap failed"):
                        ci.main()
        logs.assert_called_once()
        down.assert_called_once()


if __name__ == "__main__":
    unittest.main()
