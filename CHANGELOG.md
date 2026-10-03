# Changelog

Initial release notes finalized on 2026-10-03. Publication is pending; the
[GitHub release](https://github.com/bart-kochanowicz/terraform-provider-crafty/releases/tag/v0.1.0)
is the source of truth for availability.

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
