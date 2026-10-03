# Releases and installation

## Install v0.1.1 from GitHub Releases

Download your platform's ZIP and `terraform-provider-crafty_0.1.1_SHA256SUMS`
from [the v0.1.1 release](https://github.com/bart-kochanowicz/terraform-provider-crafty/releases/tag/v0.1.1).
Version v0.1.1 is published on GitHub and in Terraform Registry. For normal online
installation, require `bart-kochanowicz/crafty` version `0.1.1` and run
`terraform init` without `-plugin-dir` or development overrides. The mirror steps
below are an alternative for manually downloaded packages.

| System | OS identifier | Architectures |
| --- | --- | --- |
| Linux | `linux` | `amd64`, `arm64` |
| macOS | `darwin` | `amd64` (Intel), `arm64` (Apple Silicon) |
| Windows | `windows` | `amd64`, `arm64` |

Archives use `terraform-provider-crafty_0.1.1_<os>_<arch>.zip` and contain
`terraform-provider-crafty_v0.1.1` (with `.exe` on Windows) and the MIT `LICENSE`.
Release assets also include `terraform-provider-crafty_0.1.1_manifest.json` and
`terraform-provider-crafty_0.1.1_SHA256SUMS.sig`, a binary detached GPG signature.
Checksums cover all six ZIPs and the manifest. Checksums alone do not authenticate
downloads; Registry installation verifies the signature using the registered key.

### Linux and macOS

Run in Bash or Zsh with `curl` and `unzip` installed:

```sh
version=0.1.1
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
$version = '0.1.1'
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
      version = "0.1.1"
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
Registry publication requires provider registration and the matching public signing
key. The signed GitHub release triggers Registry ingestion through its webhook;
a successful GitHub workflow alone does not confirm Registry availability.

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
SHA-256 coverage and contents (including an exact copy of LICENSE), then installs the native archive into a temporary
provider mirror. Terraform init, validate, and schema export run in a fresh
configuration without Crafty credentials or development overrides. The smoke test
checks the native binary's `-version` output against package metadata; other platform binaries are
packaged but not executed. `Release snapshot` runs the same verification in PR CI.
Existing packages can be checked separately with `make release-verify`.

For subsequent releases, review [the MIT License](../LICENSE), finalize the
matching version entry in [the changelog](../CHANGELOG.md), and use the actual
release date. Keep the Makefile's default VERSION, example
version constraints, and installation guide aligned with the intended stable tag.
The snapshot version comes from GoReleaser's metadata and may differ from 0.1.1
when no stable tag exists. `make release-version-check` compares the intended
stable version with both example constraints, installation commands, and a dated
changelog entry. The tag workflow also rejects a tag that differs from that version
and uses only that version's changelog entry as the GitHub release description.

Check the required branch rules separately: CI execution alone does not prevent a
merge. Require Lint, Tests and build, Crafty acceptance tests, both Terraform
compatibility jobs, and Release snapshot before preparing a release tag. No tag
or publication is created by the snapshot checks.

After reviewing and committing the changes, merge the release commit into `main`,
wait for CI (including Crafty acceptance tests) to pass, then tag that commit:

```sh
git checkout main
git pull --ff-only
# Publish only after review, merge, green main CI and explicit release authorization.
git tag -a v0.1.1 -m 'Release v0.1.1'
git push origin v0.1.1
```

`.github/workflows/release.yml` accepts stable `vMAJOR.MINOR.PATCH` tags, reruns unit
tests, vet and the documentation check, verifies a complete snapshot and native
installation before publication, and publishes the ZIPs and checksum file
using the built-in `GITHUB_TOKEN` with job-scoped `contents: write` permission.
The publication job requires a committed, non-empty LICENSE and the
`GPG_PRIVATE_KEY` GitHub Actions secret. Set `PASSPHRASE` only if the key is
password-protected. The imported fingerprint selects the signing key. GoReleaser
uploads a draft; signature and installation verification must pass before the
workflow makes it public. A verification failure leaves the release as a draft. Verify all six assets on the GitHub
release and perform installation in a fresh Terraform directory. Failed publication
can be rerun after resolving its cause; do not move a published version tag.


## Registry signing setup

Use an RSA signing key; HashiCorp's Registry does not accept default ECC keys.
Add its ASCII-armored public key in Registry signing-key settings and the matching
private key as `GPG_PRIVATE_KEY` in GitHub repository Actions secrets. Never commit
the private key or passphrase. The optional `PASSPHRASE` secret unlocks protected
keys; an unprotected key does not need that secret.

`make release-snapshot` explicitly skips signing. PR CI additionally signs a second
snapshot with a disposable test key to exercise the real GoReleaser signing path.
Snapshots never receive production signing secrets, including fork PRs. Both
variants check the manifest and checksum coverage. To verify
a signed build locally with its public key imported, run:

```sh
python3 dev/verify_release.py --source-manifest terraform-registry-manifest.json --require-signature --signing-fingerprint FULL_PUBLIC_KEY_FINGERPRINT
```

The manifest is a standalone asset, not part of the ZIP. Local GoReleaser builds
checksum the repository manifest under its release asset name; downloaded releases
must include the versioned manifest file as well as ZIPs, checksums and signature.
Register `bart-kochanowicz/terraform-provider-crafty` in Registry and check ingestion
of the signed version. Registration alone does not make the unsigned v0.1.0 usable
from Registry. Do not replace v0.1.0 binaries or move its tag.

Registry ingestion and a fresh direct `terraform init` for 0.1.1 have been
verified. Local mirror installation remains available as an alternative. See the
[HashiCorp publishing requirements](https://developer.hashicorp.com/terraform/registry/providers/publishing).
