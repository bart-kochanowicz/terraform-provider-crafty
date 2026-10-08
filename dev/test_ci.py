"""Verify isolation and credential handling in the CI helper."""

import io
import json
import os
import tempfile
import subprocess
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch
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

    def test_passes_selected_baseline_to_contract_guard(self):
        runner = self.runner()
        with patch.dict(os.environ, {"CRAFTY_TEST_BASELINE": "9.8.7"}):
            with patch.object(runner, "token", return_value="token"), patch.object(runner, "api"):
                with patch("ci.verify_contract", side_effect=RuntimeError("stop before acceptance")) as contract:
                    with self.assertRaisesRegex(RuntimeError, "stop before acceptance"):
                        runner.test()
        contract.assert_called_once_with(runner, "token", "9.8.7")

    def test_reports_failures_before_contract(self):
        for stage in ("startup", "bootstrap", "precheck"):
            with self.subTest(stage=stage), tempfile.TemporaryDirectory() as directory:
                runner = self.runner()
                runner.output = Path(directory)
                with patch.object(runner, "token", side_effect=RuntimeError("secret-token") if stage == "bootstrap" else None, return_value="token"):
                    with patch.object(runner, "api", side_effect=RuntimeError("secret-body")):
                        with patch("ci.subprocess.run", side_effect=subprocess.CalledProcessError(1, "docker")):
                            with self.assertRaises(Exception):
                                runner.up() if stage == "startup" else runner.test()
                content = (runner.output / "api-contract.json").read_text()
                report = json.loads(content)
                self.assertEqual(report["result"], "not_run")
                self.assertEqual(report["stage"], stage)
                self.assertNotIn("secret", content)

    def test_logs_and_cleanup_do_not_mask_original_failure(self):
        runner = self.runner()
        with patch("sys.argv", ["ci.py", "run"]), patch("ci.Integration", return_value=runner):
            with patch.object(runner, "up", side_effect=RuntimeError("original startup")):
                with patch.object(runner, "logs", side_effect=RuntimeError("logs failed")):
                    with patch.object(runner, "down", side_effect=RuntimeError("down failed")) as down:
                        with self.assertRaisesRegex(RuntimeError, "original startup"):
                            ci.main()
        down.assert_called_once()

    def test_acceptance_failure_preserves_passing_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = self.runner()
            runner.output = Path(directory)
            process = MagicMock()
            process.__enter__.return_value = process
            process.stdout = ["secret-token acceptance failed\n"]
            process.wait.return_value = 1
            runner.mask("secret-token")
            def passed_contract(runner, token, baseline):
                ci.write_report(runner, {"result": "passed", "stage": "complete"})
            with patch.object(runner, "token", return_value="secret-token"), patch.object(runner, "api"):
                with patch("ci.verify_contract", side_effect=passed_contract), patch("ci.subprocess.Popen", return_value=process):
                    with self.assertRaisesRegex(RuntimeError, "Acceptance tests failed"):
                        runner.test()
            self.assertEqual(json.loads((runner.output / "api-contract.json").read_text())["result"], "passed")
            self.assertNotIn("secret-token", (runner.output / "acceptance.log").read_text())

    def test_cleanup_failure_after_success_fails_run(self):
        runner = self.runner()
        with patch("sys.argv", ["ci.py", "run"]), patch("ci.Integration", return_value=runner):
            with patch.object(runner, "up"), patch.object(runner, "test"):
                with patch.object(runner, "down", side_effect=RuntimeError("down failed")):
                    with self.assertRaisesRegex(RuntimeError, "down failed"):
                        ci.main()


if __name__ == "__main__":
    unittest.main()
