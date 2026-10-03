"""Verify release archives and install the native package in a temporary mirror."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import tempfile
import zipfile

PROVIDER = "terraform-provider-crafty"
ADDRESS = "registry.terraform.io/bart-kochanowicz/crafty"
PLATFORMS = {(system, arch) for system in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")}


def verify_archives(directory):
    version = json.loads((directory / "metadata.json").read_text())["version"]
    if not isinstance(version, str) or not re.fullmatch(r"\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?", version):
        raise RuntimeError("Invalid release version in metadata")
    expected = {f"{PROVIDER}_{version}_{system}_{arch}.zip" for system, arch in PLATFORMS}
    if {path.name for path in directory.glob("*.zip")} != expected:
        raise RuntimeError("Release must contain exactly the six supported platform archives")
    recorded = {}
    for line in (directory / f"{PROVIDER}_{version}_SHA256SUMS").read_text().splitlines():
        fields = line.split()
        if len(fields) != 2 or fields[1] in recorded or not re.fullmatch(r"[a-fA-F0-9]{64}", fields[0]):
            raise RuntimeError("Invalid or duplicate release checksum entry")
        recorded[fields[1]] = fields[0].lower()
    if set(recorded) != expected:
        raise RuntimeError("Checksums must cover exactly the six platform archives")
    for name in sorted(expected):
        archive = directory / name
        if hashlib.sha256(archive.read_bytes()).hexdigest() != recorded[name]:
            raise RuntimeError(f"Checksum mismatch: {name}")
        binary = f"{PROVIDER}_v{version}" + (".exe" if "_windows_" in name else "")
        with zipfile.ZipFile(archive) as package:
            if package.namelist() != [binary] or package.getinfo(binary).file_size == 0:
                raise RuntimeError(f"Unexpected archive contents: {name}")
    return version


def run(command, directory, env):
    result = subprocess.run(command, cwd=directory, env=env, capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise RuntimeError(f"Release smoke command failed: {command[0]} {command[1]}\n{result.stdout}{result.stderr}")
    return result.stdout


def verify_installation(directory, version, terraform):
    system = platform.system().lower()
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine().lower())
    if (system, arch) not in PLATFORMS:
        raise RuntimeError("Release smoke test requires a supported native platform")
    archive = directory / f"{PROVIDER}_{version}_{system}_{arch}.zip"
    binary_name = f"{PROVIDER}_v{version}" + (".exe" if system == "windows" else "")
    with tempfile.TemporaryDirectory(prefix="crafty-release-smoke-") as temporary:
        work = Path(temporary)
        mirror = work / "plugins"
        destination = mirror / ADDRESS / version / f"{system}_{arch}"
        destination.mkdir(parents=True)
        binary = destination / binary_name
        with zipfile.ZipFile(archive) as package:
            binary.write_bytes(package.read(binary_name))
        binary.chmod(0o755)
        env = {key: value for key, value in os.environ.items() if not key.startswith("TF_CLI_ARGS")}
        for key in ("TF_DATA_DIR", "TF_PLUGIN_CACHE_DIR", "TF_REATTACH_PROVIDERS"):
            env.pop(key, None)
        config = work / "terraform.tfrc"
        config.write_text('provider_installation {\n filesystem_mirror {\n path = ' + json.dumps(mirror.as_posix()) + '\n }\n}\n')
        env.update(TF_CLI_CONFIG_FILE=str(config), CHECKPOINT_DISABLE="1", TF_IN_AUTOMATION="1")
        (work / "main.tf").write_text('terraform {\n required_providers {\n crafty = {\n source = "bart-kochanowicz/crafty"\n version = ' + json.dumps(version) + '\n }\n }\n}\nprovider "crafty" {\n url = "https://crafty.example.com:8443"\n token = "validation-only"\n}\nresource "crafty_minecraft_server" "example" {\n name = "Release smoke test"\n engine = "paper"\n version = "1.21.1"\n mem_min = 1\n mem_max = 2\n host = "127.0.0.1"\n port = 25565\n}\n')
        metadata = run([str(binary), "-version"], work, env).strip()
        if metadata != version:
            raise RuntimeError("Native binary reports an unexpected provider version")
        run([terraform, "init", "-backend=false", "-input=false", "-plugin-dir=" + str(mirror)], work, env)
        run([terraform, "validate", "-no-color"], work, env)
        schema = json.loads(run([terraform, "providers", "schema", "-json"], work, env))
        attributes = schema["provider_schemas"][ADDRESS]["resource_schemas"]["crafty_minecraft_server"]["block"]["attributes"]
        if not {"id", "name", "engine", "version", "mem_min", "mem_max", "host", "port", "auto_start", "monitoring_host", "monitoring_port", "execution_command"} <= set(attributes):
            raise RuntimeError("Installed native provider lacks expected resource attributes")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    parser.add_argument("--terraform", default="terraform")
    args = parser.parse_args()
    version = verify_archives(args.dist)
    verify_installation(args.dist, version, args.terraform)
    print(f"Release {version}: six archives, checksums, native provider version and Terraform installation verified")


if __name__ == "__main__":
    main()
