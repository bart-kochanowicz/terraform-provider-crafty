#!/usr/bin/env python3
"""Run acceptance tests against a fresh, isolated Crafty Compose project."""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import urllib.error
import urllib.request

from api_contract import verify_contract, write_report

ROOT = Path(__file__).resolve().parent.parent
JWT = re.compile(r"eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+")


class Integration:
    def __init__(self):
        project = os.environ.get("COMPOSE_PROJECT_NAME", "")
        if not re.fullmatch(r"crafty-provider-ci-[a-z0-9-]+", project):
            raise RuntimeError("Set COMPOSE_PROJECT_NAME to a unique crafty-provider-ci-* name")
        self.compose = ["docker", "compose", "-p", project,
                        "-f", str(ROOT / "dev/compose.yml"),
                        "-f", str(ROOT / "dev/compose.ci.yml")]
        self.output = Path(os.environ.get("CRAFTY_CI_OUTPUT", str(ROOT / "bin/ci-logs")))
        self.secrets = []

    def capture(self, args):
        result = subprocess.run(self.compose + args, capture_output=True, text=True,
                                timeout=120, check=False)
        if result.returncode:
            raise RuntimeError(f"Compose command failed: {args[0]}")
        return result.stdout

    def mask(self, value):
        if not isinstance(value, str) or not value or "\n" in value or "\r" in value:
            raise RuntimeError("Crafty returned an invalid credential")
        self.secrets.append(value)
        if os.environ.get("GITHUB_ACTIONS") == "true":
            escaped = value.replace("%", "%25")
            print(f"::add-mask::{escaped}", flush=True)
        return value

    def redact(self, value):
        for secret in self.secrets:
            value = value.replace(secret, "[REDACTED]")
        return JWT.sub("[REDACTED]", value)

    def credentials(self):
        data = json.loads(self.capture(["exec", "-T", "crafty", "cat",
                                       "/crafty/app/config/default-creds.txt"]))
        self.mask(data["password"])
        return {"username": data["username"], "password": data["password"]}

    def api(self, method, path, data=None, token=None):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = f"Bearer {token}"
        body = json.dumps(data).encode() if data is not None else None
        request = urllib.request.Request("http://127.0.0.1:18001/api/v2/" + path,
                                         data=body, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                result = json.load(response)
        except urllib.error.HTTPError as error:
            code = error.code
            error.close()
            raise RuntimeError(f"Crafty {method} {path} returned HTTP {code}") from None
        except (urllib.error.URLError, TimeoutError, ValueError):
            raise RuntimeError(f"Crafty {method} {path} failed") from None
        if result.get("status") != "ok":
            raise RuntimeError(f"Crafty {method} {path} returned an unsuccessful status")
        return result.get("data")

    def token(self):
        login = self.api("POST", "auth/login", self.credentials())
        session = self.mask(login["token"])
        user_id = int(login["user_id"])
        key = self.api("PATCH", f"users/{user_id}/key", {
            "name": "terraform-ci", "full_access": True,
            "server_permissions_mask": "11111111", "crafty_permissions_mask": "111",
        }, session)
        return self.mask(self.api("GET", f"users/{user_id}/key/{int(key['id'])}", token=session))

    def up(self):
        # Pulls and first-time migrations can take several minutes on a cold runner.
        write_report(self, {"result": "not_run", "stage": "startup"})
        try:
            subprocess.run(self.compose + ["up", "-d", "--wait", "--wait-timeout", "600"],
                           cwd=ROOT, check=True, timeout=900)
        except Exception:
            write_report(self, {"result": "not_run", "stage": "startup", "failure_code": "startup_failed"})
            raise

    def test(self):
        stage = "bootstrap"
        try:
            token = self.token()
            stage = "precheck"
            # Authenticate with the newly issued API key before starting Terraform.
            self.api("GET", "servers", token=token)
        except Exception:
            write_report(self, {"result": "not_run", "stage": stage, "failure_code": stage + "_failed"})
            raise
        verify_contract(self, token)
        env = os.environ.copy()
        env.update(CRAFTY_TOKEN=token, CRAFTY_URL="http://127.0.0.1:18001", TF_ACC="1")
        env.pop("TF_CLI_CONFIG_FILE", None)
        self.output.mkdir(parents=True, exist_ok=True)
        command = [env.get("GO", "go"), "test", "-v", "-race", "-count=1", "-timeout", "20m",
                   "./internal/provider", "-run", "^TestAcc"]
        with (self.output / "acceptance.log").open("w") as log:
            with subprocess.Popen(command, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                                  stderr=subprocess.STDOUT, text=True) as process:
                for line in process.stdout:
                    safe = self.redact(line)
                    print(safe, end="", flush=True)
                    log.write(safe)
                if process.wait():
                    raise RuntimeError("Acceptance tests failed")

    def logs(self):
        self.output.mkdir(parents=True, exist_ok=True)
        # Startup may have failed before authentication. Mask the generated password
        # without making another login attempt or printing the credentials file.
        try:
            self.credentials()
        except (RuntimeError, ValueError, KeyError, subprocess.TimeoutExpired):
            pass
        for name, args in [
            ("compose.log", ["logs", "--no-color", "--timestamps", "--tail", "2000"]),
            ("status.json", ["ps", "--all", "--format", "json"]),
            ("crafty.log", ["exec", "-T", "crafty", "sh", "-c",
                            "find /crafty/logs -type f -name '*.log' -exec tail -n 500 {} +"]),
        ]:
            try:
                text = self.capture(args)
            except (RuntimeError, subprocess.TimeoutExpired) as error:
                text = str(error)
            (self.output / name).write_text(self.redact(text))

    def down(self):
        # The prefix check in __init__ prevents removing the local dev project's data.
        subprocess.run(self.compose + ["down", "--volumes", "--remove-orphans"],
                       cwd=ROOT, check=True, timeout=180)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["up", "test", "logs", "down", "run"])
    command = parser.parse_args().command
    runner = Integration()
    if command != "run":
        getattr(runner, command)()
        return
    failed = False
    try:
        runner.up()
        runner.test()
    except Exception:
        failed = True
        try:
            runner.logs()
        except Exception:
            print("Failure diagnostics could not be collected", file=sys.stderr)
        raise
    finally:
        try:
            runner.down()
        except Exception:
            if not failed:
                raise
            print("Compose cleanup failed after the original failure", file=sys.stderr)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.SubprocessError, KeyError, ValueError, OSError) as error:
        # Do not print HTTP response bodies or tracebacks containing authentication data.
        print(f"Integration error: {error}", file=sys.stderr)
        sys.exit(1)
