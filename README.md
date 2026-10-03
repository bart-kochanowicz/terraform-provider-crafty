# Terraform Provider Crafty

A Terraform Plugin Framework provider for Crafty Controller v4's v2 API.
Provider address: `registry.terraform.io/bart-kochanowicz/crafty`.
Go module: `github.com/bart-kochanowicz/terraform-provider-crafty`.

## Project layout

The following abbreviated layout follows HashiCorp's [Plugin Framework scaffolding](https://github.com/hashicorp/terraform-provider-scaffolding-framework), with a separate internal API client.

```text
.
├── main.go                         # Plugin entry point
├── internal/
│   ├── client/
│   │   ├── client.go               # HTTP transport, authentication, and errors
│   │   ├── client_test.go          # Transport and failure-path tests
│   │   ├── models.go              # Typed Crafty JSON models
│   │   └── servers.go             # Server endpoint operations
│   └── provider/
│       ├── provider.go            # Provider configuration and registration
│       ├── provider_test.go       # Configuration and registration tests
│       ├── minecraft_server_model.go
│       ├── minecraft_server_schema.go
│       ├── minecraft_server_resource.go
│       └── minecraft_server_resource_test.go
├── docs/                          # Provider and resource references
├── dev/                           # Disposable Crafty environment and CI helpers
├── examples/docker/               # Local Docker Terraform configuration
├── examples/local/                # Runnable Terraform configuration
├── .github/workflows/ci.yml        # Lint, formatting, tests, build, validation
├── .golangci.yml
├── CONTRIBUTING.md                # Package boundaries and development workflow
├── Makefile
├── go.mod
└── go.sum
```

The API client has no Terraform dependencies. The provider converts Terraform values into typed API requests and API responses into state and diagnostics. Tests live beside the code they verify. See [contributing instructions](CONTRIBUTING.md) and [provider documentation](docs/index.md).

## Requirements

- Go 1.25 or newer and Make.
- Terraform 1.5 or newer.
- golangci-lint v2.14.0 for `make fmt`, `make lint`, and `make check`.
- Python 3 and Docker Compose 2.24.4 or newer for the full local `make check`.
- Crafty Controller 4.10.4 (verified baseline) with a trusted TLS certificate and an API token with server creation, access, and configuration permissions.
- A supported engine/version pair available in Crafty's download catalog.

## Install a release

For prebuilt Linux, macOS and Windows binaries, checksum verification, and Terraform initialization, follow [the v0.1.0 installation guide](docs/releasing.md). Assets are published by the tag workflow. GitHub Releases and Terraform Registry publication are separate steps.

## Build and local installation

```sh
go mod tidy
make fmt vet test build
make install
cd examples/local
terraform init -plugin-dir="$HOME/.terraform.d/plugins"
terraform validate
export TF_VAR_crafty_url='https://crafty.example.com:8443'
# Read the token without saving it in shell history (Bash/Zsh).
read -rs TF_VAR_crafty_token
export TF_VAR_crafty_token
terraform plan
terraform apply
```

`make install` copies version 0.1.0 to Terraform's local plugin directory for the current operating system and architecture. To initialize exclusively from that directory, use:

```sh
terraform init -plugin-dir="$HOME/.terraform.d/plugins"
```

Alternatively, build the provider and add an absolute repository path to your Terraform CLI configuration (`~/.terraformrc`):

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/bart-kochanowicz/crafty" = "/absolute/path/to/terraform-provider-crafty/bin"
  }
  direct {}
}
```

With development overrides, skip `terraform init` for this example and run `terraform validate`, `terraform plan`, and `terraform apply` directly. Terraform may warn about overrides; this is expected.

## Configuration

Provider attributes `url` and `token` are required strings. `url` is the panel base URL, without `/api/v2`. `token` is sensitive and is sent as a Bearer token. TLS verification remains enabled. Redirects are rejected to avoid sending credentials to another endpoint. Individual requests have a 120-second timeout, further limited by the resource operation deadline. Resource `timeouts` default to 10 minutes for create, 2 minutes for read, and 5 minutes each for update and delete. API response bodies are omitted from errors to protect secrets.

See [the complete example](examples/local/main.tf).

| Resource attribute | Terraform type | API mapping | Behavior |
| --- | --- | --- | --- |
| `id` | String | `new_server_id` / `server_id` | Computed |
| `name` | String | POST `name`, PATCH `server_name` | Mutable |
| `engine` | String | `download_jar_create_data.type` | Replacement |
| `version` | String | `download_jar_create_data.version` | Replacement |
| `mem_min`, `mem_max` | Int64 | `download_jar_create_data.mem_min`, `mem_max` | Replacement |
| `host` | String | `minecraft_java_monitoring_data.host` | Replacement |
| `port` | Int64 | Monitoring port and `server_properties_port` | Replacement |
| `auto_start` | Boolean | PATCH/GET `auto_start` | Optional + computed, mutable |
| `monitoring_host` | String | PATCH/GET `server_ip` | Optional + computed, mutable |
| `monitoring_port` | Int64 | PATCH/GET `server_port` | Optional + computed, mutable; monitoring only |
| `execution_command` | String | PATCH/GET `execution_command` | Optional + computed, mutable; overrides generated command |

Memory uses Crafty 4.10.4 Java download units: each input unit produces **1000 JVM MiB**, so `mem_min = 1` and `mem_max = 2` generate `-Xms1000M -Xmx2000M`. These are not exact GiB. Inputs remain integers with `1 <= mem_min <= mem_max`; the port must be 1–65535. Names require at least two characters and exclude slashes, backslashes, and `#`. Other input strings must not be empty. See the [verified API contract and supported versions](docs/api-contract.md).

Optional settings can be supplied during creation or added later. Creation waits for
metadata, then sends one settings PATCH within the create timeout. A rejected or
uncertain initial PATCH preserves the ID and warns; the next plan reads actual
settings, and apply reconciles any drift without creating another server.

`host` and `port` retain their original creation semantics. Use `monitoring_host`
and `monitoring_port` for mutable monitoring; these do not change `server.properties`.
An explicit `execution_command` overrides generated JVM flags, so creation-time
`mem_min`/`mem_max` no longer describe the effective command. These inputs still
require replacement when changed. Removing an optional setting stops enforcing it
and retains the value reported by Crafty; it does not restore the original default.
See [the resource guide](docs/resources/minecraft_server.md) for an example and limits.

## API scope and specification limitations

- POST `/api/v2/servers` creates a Minecraft Java server using `minecraft_java` and `download_jar`.
- GET `/api/v2/servers` reads visible server objects and finds the server by ID. Crafty 4.10.4 also returns server objects from single GET, contrary to the specification's role schema; the provider retains collection reads because single GET uses ambiguous HTTP 400 `NOT_AUTHORIZED` for a missing ID. Three consecutive successful collection responses without an established server remove it from Terraform state. A server whose post-create/update refresh is pending retains its ID while preparation or API recovery continues. A collection 404 is an endpoint error, not proof that the server was deleted.
- PATCH `/api/v2/servers/{serverID}` updates name, automatic start, monitoring address/port, and launch command without replacing the ID. Only changed, known settings are sent; an explicit `false` is preserved. There is no documented PUT operation.
- DELETE `/api/v2/servers/{serverID}` deletes the server. HTTP 404 is treated as already deleted.
- HTTP 401/403, other non-success HTTP statuses, malformed JSON, and application-level unsuccessful statuses become English Terraform diagnostics.
- Creation inputs remain in state. Reads refresh name, automatic start, monitoring address/port, and launch command. Explicit optional settings are reconciled on apply; omitted settings are observed without enforcing a default. GET also exposes the execution command and current monitoring address/port, but does not reconstruct the complete original download payload or server.properties port.
- Changing download inputs replaces the panel record and creates a new directory. In verified Crafty 4.10.4, the provider's default DELETE preserves files; it does not reuse old worlds during replacement. Review plans, back up data, and manage retained directories separately.
- Creation can finish preparing asynchronously. The ID and a private pending-refresh marker are saved before polling GET for complete metadata and the requested name. If polling times out or fails after Crafty returned an ID, the provider returns a warning and keeps that ID without tainting the resource. A later `terraform plan` resumes the refresh. If the saved ID remains permanently pending, follow the [recovery procedure](docs/resources/minecraft_server.md#resolving-a-server-that-stays-pending). This confirms API metadata visibility, not that Minecraft has started or that every background download succeeded.
- Only reads are retried: selected transient HTTP statuses (408, 429, 500, 502, 503, 504), transport timeouts, connection interruptions, and temporary DNS failures. Backoff starts at 1 second and doubles to a 10-second maximum; `Retry-After` can extend the delay within the operation deadline. Authentication, endpoint, malformed-response, and application-status errors stop immediately.
- POST and DELETE are sent once per operation; each configuration PATCH is sent once. After an accepted configuration PATCH, desired settings and ID are saved even if the following read fails. After an accepted DELETE, the provider requires three consecutive successful collection responses without the ID; reappearance or a read error resets confirmation. A verification failure leaves the ID in state for a later destroy attempt. Inspect Crafty before retrying a create whose response was lost: without a returned ID, the provider cannot safely identify the new server.
- Bedrock creation is not implemented. ID-only import is rejected with a specific diagnostic: verified GET cannot reliably reconstruct all required Java download inputs. No values are guessed from filenames, URLs, or arbitrary commands. See [import limitations](docs/resources/minecraft_server.md#import).
- The provider does not start the server, accept Minecraft EULA on your behalf, or modify server files. Complete required setup in Crafty.

## Local Docker environment

A real Crafty development environment is available in `dev/compose.yml`. Start it with `make dev-up`, then follow [the environment guide](dev/README.md) to create an API token and use `examples/docker`. Ports are restricted to loopback, and named volumes preserve test data.

## Testing

```sh
make test vet build
```

Unit tests use local HTTP servers and do not require Crafty credentials. For automated live acceptance tests, run `make test-acc` with `CRAFTY_TOKEN` set; see [the local test guide](dev/README.md#automated-acceptance-tests). For a manual live smoke test, apply the example to a disposable Crafty instance, change `name`, verify the PATCH update, run another plan to check convergence, and finally run `terraform destroy`. Terraform state may contain sensitive data; keep it out of version control.

## Code quality and continuous integration

GitHub Actions runs `.github/workflows/ci.yml` for pull requests, pushes to `main` (including merges), merge queues, and manual dispatches. The integration job starts a fresh Crafty instance and creates its API credentials automatically; no repository secrets or manual configuration are required.

- **Lint** uses golangci-lint **v2.14.0** with `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`. It also checks `gofmt` and `goimports` formatting.
- **Crafty acceptance tests** starts the Docker Compose stack, waits for readiness, bootstraps an API key, verifies the recorded API contract, and runs the live Terraform lifecycle suite. It also publishes a contract report on success and failure. Failures upload redacted diagnostics, and cleanup removes the disposable containers and volumes. See [the CI integration guide](dev/README.md#integration-tests-in-github-actions).
- **Tests and build** checks dependency integrity and whether `go mod tidy` changes tracked module files, checks Go and Terraform formatting, runs uncached tests with the race detector, runs `go vet`, builds the provider, and validates both Terraform examples with a development override. Its core checks use the same `make check-core` target as local development.
- **Release snapshot** builds all six platform ZIPs, verifies checksums and archive contents, and tests installation of the native package in a fresh Terraform configuration. It does not publish a release. See [release preparation](docs/releasing.md) and [the changelog](CHANGELOG.md).
- **Terraform compatibility** validates both examples and runs the controlled Terraform recovery/replacement suite on **1.5.0** (the declared minimum) and **1.16.4**. These tests do not require Crafty; live API compatibility remains covered by the integration job.

Install golangci-lint locally using Homebrew:

```sh
brew install golangci-lint
golangci-lint version
make fmt
make check
```

Use v2.14.0 for exact CI parity; see the [official installation instructions](https://golangci-lint.run/docs/welcome/install/local/) for version-specific binaries. `make fmt` updates Go import/formatting and Terraform examples. `make fmt-check` only checks formatting. `make lint` runs the configured analyzers and checks formatting. The full `make check` also covers module integrity/tidy, generated docs, release configuration, Python helpers, Compose configuration, example validation, and controlled Terraform acceptance. It does not start Crafty. Override executable paths using `GO`, `GOLANGCI_LINT`, `TERRAFORM`, `DOCKER`, and `PYTHON` when needed.

To prevent merging failing changes, configure a GitHub branch ruleset for `main` and require the **Lint**, **Tests and build**, and **Crafty acceptance tests** status checks plus **Terraform compatibility (1.5.0)** and **Terraform compatibility (1.16.4)** and **Release snapshot** after their first workflow run. The workflow alone runs checks but does not enforce branch protection.
