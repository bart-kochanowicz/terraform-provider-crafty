"""Check the recorded Crafty 4.10.4 API contract against a disposable CI instance."""

import json
from pathlib import Path
import time
import urllib.error
import urllib.request
import uuid

FIXTURES = Path(__file__).resolve().parent.parent / "internal/client/testdata/crafty-4.10.4"


def assert_fields(actual, recorded):
    """Require recorded fields and types, while allowing additive API changes."""
    for key, expected in recorded.items():
        if key not in actual or type(actual[key]) is not type(expected):
            raise RuntimeError(f"API contract: missing or incorrectly typed GET field {key}")


def request(token, method, path, data=None, expected_status=200):
    body = json.dumps(data).encode() if data is not None else None
    req = urllib.request.Request("http://127.0.0.1:18001/api/v2/" + path, method=method,
                                 data=body, headers={"Content-Type": "application/json",
                                                    "Authorization": "Bearer " + token})
    try:
        response = urllib.request.urlopen(req, timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        if response.code != expected_status:
            raise RuntimeError(f"API contract: {method} {path} returned HTTP {response.code}; expected {expected_status}")
        result = json.load(response)
    if expected_status < 400 and result.get("status") != "ok":
        raise RuntimeError(f"API contract: {method} {path} returned an unsuccessful status")
    return result


def file_exists(runner, path):
    return runner.capture(["exec", "-T", "crafty", "python3", "-c",
                           "import pathlib,sys; print(pathlib.Path(sys.argv[1]).exists())", path]).strip() == "True"


def verify_contract(runner, token):
    version = json.loads(runner.capture(["exec", "-T", "crafty", "cat", "/crafty/app/config/version.json"]))
    actual_version = ".".join(str(version[key]) for key in ("major", "minor", "sub"))
    if actual_version != "4.10.4":
        raise RuntimeError(f"API contract: supported baseline is Crafty 4.10.4, got {actual_version}; verify a new baseline first")
    spec = json.loads((FIXTURES / "spec-summary.json").read_text())
    recorded_single = json.loads((FIXTURES / "server-response.json").read_text())["data"]
    recorded_list = json.loads((FIXTURES / "list-response.json").read_text())["data"][0]
    report = {"crafty_version": actual_version, "openapi_spec_version": spec["openapi_version"], "api_spec_version": spec["api_version"],
              "openapi_sha256": spec["sha256"], "memory_commands": [], "checks": []}
    runner.output.mkdir(parents=True, exist_ok=True)
    report_path = runner.output / "api-contract.json"
    pending = []
    try:
        for minimum, maximum, remove_files in [(1, 2, False), (1.5, 2.5, True)]:
            payload = json.loads((FIXTURES / "create-request.json").read_text())
            payload["name"] = "contract-" + uuid.uuid4().hex
            download = payload["minecraft_java_create_data"]["download_jar_create_data"]
            download.update(mem_min=minimum, mem_max=maximum)
            created = request(token, "POST", "servers", payload, 201)["data"]
            server_id = created["new_server_id"]
            if not isinstance(server_id, str) or not server_id:
                raise RuntimeError("API contract: POST did not return a string new_server_id")
            pending.append(server_id)
            single = request(token, "GET", "servers/" + server_id)["data"]
            collection = request(token, "GET", "servers")["data"]
            listed = next((item for item in collection if item["server_id"] == server_id), None)
            if listed is None:
                raise RuntimeError("API contract: new server missing from GET collection")
            assert_fields(single, recorded_single)
            assert_fields(listed, recorded_list)
            expected_command = f"java -Xms{int(minimum * 1000)}M -Xmx{int(maximum * 1000)}M -jar paper.jar nogui"
            if single["execution_command"] != expected_command or listed["execution_command"] != expected_command:
                raise RuntimeError("API contract: download_jar RAM no longer maps to 1000M per input unit")
            monitoring = payload["minecraft_java_monitoring_data"]
            if single["server_ip"] != monitoring["host"] or single["server_port"] != monitoring["port"]:
                raise RuntimeError("API contract: GET monitoring fields do not match create inputs")
            server_path = single["path"]
            properties_path = server_path + "/server.properties"
            expected_port = f"server-port={download['server_properties_port']}"
            properties = runner.capture(["exec", "-T", "crafty", "cat", properties_path])
            if expected_port not in properties.splitlines():
                raise RuntimeError("API contract: server.properties port does not match its separate create input")
            report["memory_commands"].append({"mem_min": minimum, "mem_max": maximum, "execution_command": expected_command})
            if not remove_files:
                report["get_collection_fields"] = sorted(listed)
                report["get_single_fields"] = sorted(single)
                report["server_fields_missing_from_runtime"] = sorted(set(spec["server_fields"]) - set(single))
                report["runtime_fields_missing_from_spec"] = sorted(set(single) - set(spec["server_fields"]))
                patch = {"server_name": payload["name"] + "-renamed", "auto_start": True,
                         "server_ip": "127.0.0.2", "server_port": 25578,
                         "execution_command": expected_command.replace("1000M", "1500M").replace("2000M", "2500M")}
                request(token, "PATCH", "servers/" + server_id, patch)
                updated = request(token, "GET", "servers/" + server_id)["data"]
                if any(updated[key] != value for key, value in patch.items()):
                    raise RuntimeError("API contract: accepted PATCH fields were not persisted")
                if runner.capture(["exec", "-T", "crafty", "cat", properties_path]) != properties:
                    raise RuntimeError("API contract: monitoring-port PATCH unexpectedly changed server.properties")
                report["verified_patch_fields"] = sorted(patch)
                for body in ({"mem_min": 3}, {"server_name": "x"}, {"server_name": "invalid/name"},
                             {"server_name": "invalid\\name"}, {"server_name": "invalid#name"},
                             {"server_name": "🚀"}):
                    rejected = request(token, "PATCH", "servers/" + server_id, body, 400)
                    if rejected.get("error") != "INVALID_JSON_SCHEMA":
                        raise RuntimeError("API contract: unsupported PATCH was not rejected by schema validation")
                invalid_create = dict(payload, name="x")
                if request(token, "POST", "servers", invalid_create, 400).get("error") != "INVALID_JSON_SCHEMA":
                    raise RuntimeError("API contract: invalid creation name was accepted")
                request(token, "PATCH", "servers/" + server_id, {"server_name": "🚀🚀"})
                if request(token, "GET", "servers/" + server_id)["data"]["server_name"] != "🚀🚀":
                    raise RuntimeError("API contract: valid Unicode name was not persisted")
                report["checks"].extend(["create and PATCH name constraints, including Unicode",
                                        "PATCH configuration persisted", "PATCH monitoring port preserves server.properties", "PATCH unsupported memory and invalid names rejected"])
            suffix = "?files=true" if remove_files else ""
            request(token, "DELETE", "servers/" + server_id + suffix)
            deadline = time.monotonic() + 30
            while any(item["server_id"] == server_id for item in request(token, "GET", "servers")["data"]):
                if time.monotonic() >= deadline:
                    raise RuntimeError("API contract: DELETE did not remove the server from the collection")
                time.sleep(0.5)
            missing = request(token, "GET", "servers/" + server_id, expected_status=400)
            if missing.get("error") != "NOT_AUTHORIZED":
                raise RuntimeError("API contract: missing single GET error changed")
            exists = file_exists(runner, server_path)
            if exists == remove_files:
                raise RuntimeError("API contract: DELETE files flag behavior changed")
            pending.remove(server_id)
            report["checks"].append("DELETE files=true removes directory" if remove_files else "DELETE default preserves directory")
        report["checks"].extend(["integer and fractional RAM conversion", "GET collection and single-server field types", "missing single GET returns HTTP 400 NOT_AUTHORIZED"])
        report["result"] = "passed"
        print(f"API contract passed: Crafty {actual_version}, RAM 1/2 -> 1000M/2000M, GET fields, five PATCH fields, DELETE semantics", flush=True)
    finally:
        # Each ID is registered immediately after POST. CI teardown also removes all
        # project volumes if a response is lost or the API becomes unavailable.
        for server_id in pending:
            try:
                request(token, "DELETE", "servers/" + server_id + "?files=true")
            except Exception:
                report["cleanup_error"] = True
        report_path.write_text(json.dumps(report, indent=2) + "\n")
