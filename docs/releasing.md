# Releases and installation

## Install v0.1.0 from GitHub Releases

Download your platform's ZIP and `terraform-provider-crafty_0.1.0_SHA256SUMS`
from [the v0.1.0 release](https://github.com/bart-kochanowicz/terraform-provider-crafty/releases/tag/v0.1.0).
These assets become available when the release workflow successfully publishes the tag.

| System | OS identifier | Architectures |
| --- | --- | --- |
| Linux | `linux` | `amd64`, `arm64` |
| macOS | `darwin` | `amd64` (Intel), `arm64` (Apple Silicon) |
| Windows | `windows` | `amd64`, `arm64` |

Archives use `terraform-provider-crafty_0.1.0_<os>_<arch>.zip` and contain
`terraform-provider-crafty_v0.1.0` (with `.exe` on Windows).
SHA-256 checksums detect damaged or mismatched downloads; they are not signatures.

### Linux and macOS

Run in Bash or Zsh with `curl` and `unzip` installed:

```sh
version=0.1.0
case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo 'Unsupported operating system'; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo 'Unsupported architecture'; exit 1 ;;
esac
archive="terraform-provider-crafty_${version}_${os}_${arch}.zip"
checksums="terraform-provider-crafty_${version}_SHA256SUMS"
base="https://github.com/bart-kochanowicz/terraform-provider-crafty/releases/download/v${version}"
work=$(mktemp -d)
(
  set -eu
  cd "$work"
  curl -fLO "$base/$archive"
  curl -fLO "$base/$checksums"
  if [ "$os" = darwin ]; then
    expected=$(awk -v file="$archive" '$2 == file {print $1}' "$checksums")
    actual=$(shasum -a 256 "$archive" | awk '{print $1}')
    test -n "$expected" && test "$expected" = "$actual"
  else
    awk -v file="$archive" '$2 == file {print}' "$checksums" > selected.sha256
    test -s selected.sha256
    sha256sum -c selected.sha256
  fi
  destination="$HOME/.terraform.d/plugins/registry.terraform.io/bart-kochanowicz/crafty/$version/${os}_${arch}"
  mkdir -p "$destination"
  unzip -o "$archive" -d "$destination"
  chmod +x "$destination/terraform-provider-crafty_v$version"
)
```

Remove the temporary download directory after successful installation with
`rm -r "$work"`.

### Windows (PowerShell)

Use the architecture of your Terraform executable (usually `amd64`):

```powershell
$ErrorActionPreference = 'Stop'
$version = '0.1.0'
$arch = 'amd64' # Change to arm64 for native ARM64 Terraform.
$archive = "terraform-provider-crafty_${version}_windows_${arch}.zip"
$checksums = "terraform-provider-crafty_${version}_SHA256SUMS"
$base = "https://github.com/bart-kochanowicz/terraform-provider-crafty/releases/download/v$version"
$work = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $work | Out-Null
Invoke-WebRequest "$base/$archive" -OutFile (Join-Path $work $archive)
Invoke-WebRequest "$base/$checksums" -OutFile (Join-Path $work $checksums)
$line = Get-Content (Join-Path $work $checksums) | Where-Object { ($_ -split '\s+')[1] -eq $archive }
if (@($line).Count -ne 1) { throw 'Missing or duplicate checksum' }
$expected = ($line -split '\s+')[0]
$actual = (Get-FileHash (Join-Path $work $archive) -Algorithm SHA256).Hash
if ($actual -ne $expected) { throw 'Checksum mismatch' }
$destination = Join-Path $env:APPDATA "terraform.d/plugins/registry.terraform.io/bart-kochanowicz/crafty/$version/windows_$arch"
New-Item -ItemType Directory -Force -Path $destination | Out-Null
Expand-Archive (Join-Path $work $archive) -DestinationPath $destination -Force
Remove-Item -Recurse $work
```

### Initialize Terraform

Your configuration must specify the provider address and version:

```hcl
terraform {
  required_providers {
    crafty = {
      source  = "bart-kochanowicz/crafty"
      version = "0.1.0"
    }
  }
}
```

Disable any development override for this provider before testing the release.
In your Terraform configuration directory, initialize from the local mirror:

```sh
# Linux / macOS
terraform init -plugin-dir="$HOME/.terraform.d/plugins"
terraform validate
```

```powershell
# Windows
terraform init -plugin-dir="$env:APPDATA/terraform.d/plugins"
terraform validate
```

`-plugin-dir` restricts installation to this mirror; configurations using other
providers must also have those providers installed there. Use the repository's
`examples/local` configuration for a standalone Crafty example. Set the URL and
API token as described in the README, then run `terraform plan` and `terraform apply`.
Do not reuse a lock file created from a different development binary; install the
release in a fresh example directory or deliberately regenerate its provider lock.
Commit the resulting `.terraform.lock.hcl` for repeatable installation.

GitHub Releases installation does not require a Terraform Registry listing.
Registry publication is a separate maintainer task requiring provider registration,
a signing key and signed checksums; this workflow does not claim to publish there.

## Maintainer workflow

Documentation uses pinned `tfplugindocs` and templates in `templates/`.
Edit the templates for narrative changes and the Go schema for attribute descriptions,
then run `make docs`. Existing API-contract and installation guides remain hand-written.
CI runs `make docs-check` to reject stale generated documentation and
GoReleaser `check` to validate the release configuration. Local `make release-check`
and `make release-snapshot` run the pinned GoReleaser through Go; Go may download
a newer toolchain automatically to meet the tool's requirements.

Before tagging:

```sh
make docs
make check
make release-snapshot
```

`release-snapshot` creates all six ZIPs and SHA-256 checksums in `dist/`, without
publishing. Snapshot versions differ from stable versions. The GoReleaser linker
flags embed the release version in the provider metadata. The snapshot target
also runs `make release-verify`: it checks the exact six platform archives,
SHA-256 coverage and contents, then installs the native archive into a temporary
provider mirror. Terraform init, validate, and schema export run in a fresh
configuration without Crafty credentials or development overrides. The smoke test
checks the native binary's `-version` output against package metadata; other platform binaries are
packaged but not executed. `Release snapshot` runs the same verification in PR CI.
Existing packages can be checked separately with `make release-verify`.

Before an initial public release, select and commit the project's LICENSE, review
[the changelog](../CHANGELOG.md), and replace its planned/unreleased heading with
the actual version and release date. Keep the Makefile's default VERSION, example
version constraints, and installation guide aligned with the intended stable tag.
The snapshot version comes from GoReleaser's metadata and may differ from 0.1.0
when no stable tag exists.

Check the required branch rules separately: CI execution alone does not prevent a
merge. Require Lint, Tests and build, Crafty acceptance tests, both Terraform
compatibility jobs, and Release snapshot before preparing a release tag. No tag
or publication is created by the snapshot checks.

After reviewing and committing the changes, merge the release commit into `main`,
wait for CI (including Crafty acceptance tests) to pass, then tag that commit:

```sh
git checkout main
git pull --ff-only
git tag -a v0.1.0 -m 'Release v0.1.0'
git push origin v0.1.0
```

`.github/workflows/release.yml` accepts stable `vMAJOR.MINOR.PATCH` tags, reruns unit
tests, vet and the documentation check, verifies a complete snapshot and native
installation before publication, and publishes the ZIPs and checksum file
using the built-in `GITHUB_TOKEN` with job-scoped `contents: write` permission.
The publication job requires a committed, non-empty LICENSE. No additional release
secret is required. Verify all six assets on the GitHub
release and perform installation in a fresh Terraform directory. Failed publication
can be rerun after resolving its cause; do not move a published version tag.
