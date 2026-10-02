"""Regression tests for comparisons against the recorded API response shapes."""

import io
import json
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

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
