"""Regression tests for comparisons against the recorded API response shapes."""

from contextlib import nullcontext
import io
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

import api_contract
from api_contract import assert_fields, request


class ContractTest(unittest.TestCase):
    def test_additive_fields_are_allowed(self):
        assert_fields({"server_id": "abc", "auto_start": False, "new_field": 1},
                      {"server_id": "recorded-id", "auto_start": False})

    def test_missing_or_incorrectly_typed_fields_fail(self):
        for actual in ({"server_id": "abc"}, {"server_id": "abc", "auto_start": 0}):
            with self.subTest(actual=actual), self.assertRaisesRegex(RuntimeError, "auto_start"):
                assert_fields(actual, {"server_id": "id", "auto_start": False})

    def test_failure_diagnostics_exclude_response_body(self):
        error = HTTPError("http://localhost", 500, "error", {}, io.BytesIO(b'secret-body'))
        with patch("urllib.request.urlopen", side_effect=error):
            with self.assertRaisesRegex(RuntimeError, "returned HTTP 500") as result:
                request("secret-token", "GET", "servers")
        self.assertNotIn("secret", str(result.exception))

    def test_expected_validation_error_is_decoded(self):
        error = HTTPError("http://localhost", 400, "error", {},
                          io.BytesIO(json.dumps({"status": "error", "error": "INVALID_JSON_SCHEMA"}).encode()))
        with patch("urllib.request.urlopen", side_effect=error):
            self.assertEqual(request("token", "PATCH", "servers/id", {}, 400)["error"],
                             "INVALID_JSON_SCHEMA")


class ReportTest(unittest.TestCase):
    def test_early_failures_always_write_safe_report(self):
        cases = [
            ('{"major":4,"minor":10,"sub":5}', None, "version"),
            ('secret-token', None, "version"),
            ('{"major":4,"minor":10,"sub":4}', OSError("secret-path"), "fixtures"),
            ('{"major":4,"minor":10,"sub":4}', None, "probes"),
        ]
        for version, fixture_error, stage in cases:
            with self.subTest(stage=stage), tempfile.TemporaryDirectory() as directory:
                runner = SimpleNamespace(output=Path(directory), capture=lambda _: version)
                with patch("api_contract.request", side_effect=RuntimeError("secret-body")):
                    with patch.object(Path, "read_text", side_effect=fixture_error) if fixture_error else nullcontext():
                        with self.assertRaises(Exception):
                            api_contract.verify_contract(runner, "secret-token")
                content = (runner.output / "api-contract.json").read_text()
                report = json.loads(content)
                self.assertEqual(report["result"], "failed")
                self.assertEqual(report["stage"], stage)
                self.assertIn("failure_code", report)
                self.assertNotIn("secret", content)

    def test_probe_failure_preserves_error_when_cleanup_and_report_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = SimpleNamespace(output=Path(directory), capture=lambda _: '{"major":4,"minor":10,"sub":4}')
            with patch("api_contract.request", side_effect=[{"data": {"new_server_id": "id"}}, RuntimeError("probe failed"), RuntimeError("cleanup failed")]):
                with self.assertRaisesRegex(RuntimeError, "probe failed"):
                    api_contract.verify_contract(runner, "secret-token")
            report = json.loads((runner.output / "api-contract.json").read_text())
            self.assertTrue(report["cleanup_error"])
            self.assertEqual(report["result"], "failed")
            with patch.object(Path, "write_text", side_effect=OSError("secret-path")):
                with patch("api_contract.request", side_effect=RuntimeError("original probe")):
                    with self.assertRaisesRegex(RuntimeError, "original probe"):
                        api_contract.verify_contract(runner, "token")

    def test_report_write_error_without_original_failure_is_fatal(self):
        runner = SimpleNamespace(output=Path("/unused"))
        with patch.object(Path, "mkdir", side_effect=OSError("secret-path")):
            with self.assertRaisesRegex(RuntimeError, "report could not be written") as raised:
                api_contract.write_report(runner, {"result": "passed"})
        self.assertNotIn("secret", str(raised.exception))

    def test_successful_probes_produce_passed_report(self):
        single = json.loads((api_contract.FIXTURES / "server-response.json").read_text())["data"]
        current = {}
        deleted = False

        def capture(args):
            if args[-1].endswith("version.json"):
                return '{"major":4,"minor":10,"sub":4}'
            return "server-port=25577\n"

        def fake_request(token, method, path, data=None, expected_status=200):
            nonlocal current, deleted
            if method == "POST":
                if expected_status == 400:
                    return {"error": "INVALID_JSON_SCHEMA"}
                download = data["minecraft_java_create_data"]["download_jar_create_data"]
                minimum, maximum = int(download["mem_min"] * 1000), int(download["mem_max"] * 1000)
                current = dict(single, server_id="id", server_name=data["name"],
                               execution_command=f"java -Xms{minimum}M -Xmx{maximum}M -jar paper.jar nogui")
                deleted = False
                return {"data": {"new_server_id": "id"}}
            if method == "PATCH":
                if expected_status == 400:
                    return {"error": "INVALID_JSON_SCHEMA"}
                current.update(data)
                return {}
            if method == "DELETE":
                deleted = True
                return {}
            if path == "servers":
                return {"data": [] if deleted else [current]}
            if expected_status == 400:
                return {"error": "NOT_AUTHORIZED"}
            return {"data": current}

        with tempfile.TemporaryDirectory() as directory:
            runner = SimpleNamespace(output=Path(directory), capture=capture)
            with patch("api_contract.request", side_effect=fake_request), patch("api_contract.file_exists", side_effect=[True, False]):
                api_contract.verify_contract(runner, "secret-token")
            report = json.loads((runner.output / "api-contract.json").read_text())
            self.assertEqual((report["result"], report["stage"]), ("passed", "complete"))
            self.assertEqual(len(report["memory_commands"]), 2)
            self.assertNotIn("failure_code", report)
