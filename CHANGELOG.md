# Changelog

The initial release was published on 2026-10-03. Download binaries and checksums
from [GitHub Releases](https://github.com/bart-kochanowicz/terraform-provider-crafty/releases/tag/v0.1.0).

## 0.1.5 — 2026-10-08

- Add `crafty_webhook` resource with configuration management, drift detection, and import by server/webhook ID.

## 0.1.4 — 2026-10-08

- Add `crafty_server` data source to read an existing server's current settings by ID.

## 0.1.3 — 2026-10-08

- Verify Crafty 4.11.0 compatibility, make it the default development image, and test both recorded versions in CI.
- Discover contract fixtures automatically and keep provider code and descriptions independent of version lists.
- Use one pinned Terraform version in CI; remove the legacy compatibility matrix.

## 0.1.2 — 2026-10-03

- Preserve established Minecraft server IDs and all existing Terraform state when the configured token no longer sees a server. Three consecutive successful collection responses without its ID now return a read error instead of allowing a duplicate create.
- Servers deleted outside Terraform also remain in state: independently confirm deletion, back up state, and remove only the affected resource with `terraform state rm` before recreating it. Restore permissions instead when the server still exists. Pending post-create/update recovery and accepted DELETE verification are unchanged.
- Add regression coverage for empty and filtered server collections, including unchanged state on read failure.
- Add manual release publication from GitHub Actions, requiring successful CI for the selected main commit and a finalized changelog entry. The workflow derives the tag from Makefile VERSION, creates an annotated tag and performs signed publication without a locally pushed tag.
- Use compatible patch constraints in examples and version-independent installation instructions; future patch releases update only Makefile VERSION and the changelog. Update recovery documentation for 0.1.2. Registry installation must be verified after publication; restricted-token permission combinations remain outside the verified baseline.

## 0.1.1 — 2026-10-03

- Add Terraform Registry manifest declaring provider protocol 6.0.
- Sign SHA-256 checksums with GPG and include the manifest in checksum coverage.
- Verify the detached signature and native installation before publishing the draft release.
- No provider resource behavior changes. Version 0.1.1 is published in Terraform Registry.

## 0.1.0 — 2026-10-03

### Provider capabilities

- Manage Minecraft Java servers with Crafty Controller 4.10.4's v2 API; Paper 1.21.1 is the verified engine/version baseline.
- Create servers from download inputs and update name, automatic start, monitoring address/port, and the complete execution command without changing the ID.
- Require replacement when engine, Minecraft version, creation memory, host, or port changes. Crafty's memory input unit produces 1000 JVM MiB in the verified download path.
- Observe omitted optional settings, reconcile explicitly configured settings, and preserve explicit `false` values.
- Preserve returned server IDs after incomplete post-create/update refreshes, avoiding automatic mutation replay and duplicate creates. Support configurable operation timeouts and transient read retries.
- Require three consecutive successful absences before confirming accepted deletion or removing established resources from state. Keep pending identity during unresolved visibility.

### Verification and distribution

- MIT License included in the repository and every release archive.
- HTTP client, resource, Terraform recovery/replacement, and live disposable Crafty acceptance tests; recorded API fixtures and live contract reports.
- Example validation and controlled Terraform acceptance on Terraform 1.5.0 and 1.16.4.
- Shared local and CI checks, generated provider/resource documentation, and documented pending-state recovery.
- GoReleaser ZIPs and SHA-256 checksums for Linux, macOS, and Windows on amd64/arm64; package-content verification and native Terraform installation smoke tests.

### Scope and limitations

- Default deletion preserves world directories in Crafty 4.10.4; replacement creates a distinct directory without reusing the old world.
- ID-only import, Bedrock creation, server startup, EULA acceptance, and file management are not implemented.
- Restricted-token permissions and other Crafty baselines remain unverified.
- GitHub release publication and Terraform Registry registration/signing are separate steps. Registry installation is not available until that separate registration is completed.
