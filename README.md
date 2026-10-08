# Terraform Provider Crafty

A Terraform Plugin Framework provider for Crafty Controller v4's v2 API.
Provider address: `registry.terraform.io/bart-kochanowicz/crafty`.
Go module: `github.com/bart-kochanowicz/terraform-provider-crafty`.

Read existing servers by ID with the [`crafty_server` data source](docs/data-sources/server.md).
Manage event notifications with the [`crafty_webhook` resource](docs/resources/webhook.md).
Manage recurring tasks with the [`crafty_schedule` resource](docs/resources/schedule.md).
Manage existing backup policies with [`crafty_backup_config`](docs/resources/backup_config.md).

Licensed under the [MIT License](LICENSE). Release archives include the license text.

## Start here

To try the published provider on a disposable local Crafty instance, follow the
[local quickstart](#local-quickstart). For an existing Crafty instance, use
[Registry installation](#install-a-release). To modify the provider itself, see
[build and local installation](#build-and-local-installation).

## Requirements

- Git, Make, and Terraform.
- Go 1.25 or newer only when building the provider or running its development checks.
- For the local quickstart: a running Docker engine with Docker Compose and free
  local ports 8443, 18000, and 25565. Docker Desktop or OrbStack is suitable.
- Internet access for Registry packages, container images, and Crafty's server download.
- For an existing instance: Crafty Controller with the v2 API, a trusted
  TLS certificate, and an API token with server creation, access, configuration,
  and deletion permissions. See the [tested compatibility matrix](docs/api-contract.md#supported-and-tested-versions) and the
  [visibility limitation](#api-token-visibility).
- golangci-lint v2.14.0 is needed only for formatting/lint and the full checks;
  Python 3 and Docker Compose 2.24.4 or newer are also needed for `make check`.

## Local quickstart

Commands below are for Bash or Zsh on macOS/Linux. Start Docker first. Use one
terminal throughout so the project name, provider configuration, and token remain
available. The separate Compose project gets fresh volumes; it still uses fixed
host ports, so stop any other local stack occupying those ports first.

### 1. Create a fresh checkout and start Crafty

```sh
export COMPOSE_PROJECT_NAME="crafty-quickstart-$(date +%s)"
git clone https://github.com/bart-kochanowicz/terraform-provider-crafty.git "$HOME/$COMPOSE_PROJECT_NAME"
cd "$HOME/$COMPOSE_PROJECT_NAME"

# Avoid settings inherited from another Terraform or Crafty session.
unset TF_CLI_CONFIG_FILE TF_DATA_DIR TF_PLUGIN_CACHE_DIR TF_REATTACH_PROVIDERS
unset TF_VAR_crafty_url TF_VAR_crafty_token CRAFTY_TOKEN CRAFTY_IMAGE
unset TF_CLI_ARGS TF_CLI_ARGS_init TF_CLI_ARGS_validate TF_CLI_ARGS_plan TF_CLI_ARGS_apply TF_CLI_ARGS_destroy

docker version
make dev-check
make dev-up
make dev-status
```

Both services should be healthy. Image downloads and first initialization can take
several minutes. If startup times out, inspect `make dev-logs` (Ctrl-C stops log
streaming) and retry `make dev-up`. A port conflict needs to be resolved first;
changing the project name alone does not change ports.

### 2. Sign in and create a token

```sh
make dev-credentials
```

Open **https://localhost:8443**, accept the generated certificate for this local
instance, and sign in using the displayed credentials. Change the initial password.
In your user's **API Keys** settings, create a **superuser API key** for this
disposable instance. Do not paste credentials or the token into issues or commits.

Terraform uses **http://127.0.0.1:18000**, the loopback-only API bridge. The dashboard
address and the API bridge address serve different purposes. This local bridge
setup is for disposable development; use trusted HTTPS with a real installation.

### 3. Install from Registry and create a server

From the repository root:

```sh
# Ignore global CLI overrides while testing the published provider.
mkdir -p bin
printf 'provider_installation { direct {} }\n' > bin/registry.tfrc
export TF_CLI_CONFIG_FILE="$PWD/bin/registry.tfrc"
cd examples/docker
terraform init

# Paste the API key, then press Enter. Input is hidden.
read -rs TF_VAR_crafty_token
export TF_VAR_crafty_token
terraform validate
terraform plan
terraform apply
```

Terraform should install **bart-kochanowicz/crafty** at a version satisfying
the example constraint with a developer
signature (`self-signed`). This is expected for a community provider. The CLI
configuration is local to this checkout and leaves `~/.terraformrc` unchanged.
A development-override warning is unexpected on this path; check that
`TF_CLI_CONFIG_FILE` points to the new `bin/registry.tfrc`.

The plan should show **one resource to add**. Review it, then confirm apply with
`yes`. The example uses Paper 1.21.1 and creation memory inputs 1–2, which generate
1000–2000 JVM MiB in Crafty's Java download API. Creation downloads the server executable.
Check the returned `server_id` and the server's appearance in the Crafty dashboard.
The provider does not start Minecraft or accept its EULA. A created record confirms
API metadata visibility, not successful completion of every background download.

### 4. Verify an update and convergence

In `examples/docker/main.tf`, change only the resource's `name`, for example to
`"Terraform quickstart renamed"`. Run:

```sh
terraform plan
terraform apply
terraform output server_id
terraform plan
```

Expect **one resource to change**, the same server ID, the new name in Crafty,
and finally **No changes**. If the plan proposes replacement, check that only
`name` changed. Engine, version, creation memory, host, and port require replacement.
For a resource that stays pending, follow the [recovery procedure](docs/resources/minecraft_server.md#resolving-a-server-that-stays-pending).

### 5. Destroy, then remove the disposable environment

Still in `examples/docker`, with the same token and CLI configuration:

```sh
terraform destroy
terraform state list
```

Confirm destroy with `yes`. The state list should be empty and the server should
be absent from Crafty. If destroy fails, keep the state and volumes and resolve the
error before continuing. Default deletion preserves world directories in Crafty.
The next command removes those retained files together with this test instance's
configuration, credentials, logs, backups, and imports:

```sh
cd ../..
docker compose -f dev/compose.yml -p "$COMPOSE_PROJECT_NAME" down --volumes
unset TF_VAR_crafty_token TF_CLI_CONFIG_FILE COMPOSE_PROJECT_NAME
```

The checkout remains available for inspection. Terraform state can contain
sensitive data; keep it private. To stop and preserve the instance instead of
removing it, use `make dev-down` with the same project name. In a new terminal,
restore the original project name before running any Compose command.
Do not use a global Docker prune or delete state to reset this test.

## Install a release

Choose a published version from [Terraform Registry](https://registry.terraform.io/providers/bart-kochanowicz/crafty/latest).
Examples accept compatible 0.1.x patches starting at 0.1.2, which includes the
visibility fix. If that minimum version is not yet available, use a source build.
Commit your provider lock file; use `terraform init -upgrade` to select newer
versions within the constraint.
For an existing Crafty instance, use a fresh checkout, configure its URL and token,
and initialize the example without a local mirror:

```sh
mkdir -p bin
printf 'provider_installation { direct {} }\n' > bin/registry.tfrc
export TF_CLI_CONFIG_FILE="$PWD/bin/registry.tfrc"
cd examples/local
terraform init
export TF_VAR_crafty_url='https://crafty.example.com:8443'
read -rs TF_VAR_crafty_token
export TF_VAR_crafty_token
terraform validate
terraform plan
```

Replace the URL and use a full-access API key belonging to an account with access
to the managed servers. Review the plan before applying to your existing instance.
Keep the resulting `.terraform.lock.hcl` for repeatable provider installation.
Unset the token and CLI config when finished. For offline/local-mirror installation
and direct GitHub downloads, see [the installation guide](docs/releasing.md).

## Build and local installation

For an existing Crafty instance, from a fresh checkout:

```sh
make test vet build
make install
cd examples/local
# Use an empty CLI config to avoid global development overrides.
export TF_CLI_CONFIG_FILE="$(mktemp "${TMPDIR:-/tmp}/crafty-cli.XXXXXX")"
terraform init -plugin-dir="$HOME/.terraform.d/plugins"
export TF_VAR_crafty_url='https://crafty.example.com:8443'
read -rs TF_VAR_crafty_token
export TF_VAR_crafty_token
terraform validate
terraform plan
terraform apply
```

Replace the URL and use your instance's API key. `make install` copies the version
defined by the Makefile's `VERSION` to Terraform's local plugin directory for the current OS and architecture.
The explicit plugin directory restricts initialization to that mirror. Use a fresh
example directory; do not reuse state or a lock file from another build. Retain
state for subsequent management and restore these environment variables in later
sessions. Unset the token and CLI config when finished.

For development, `make dev-provider` writes `bin/dev.tfrc`; use its absolute path
as `TF_CLI_CONFIG_FILE` and skip init, as in the quickstart. More details are in
[the environment guide](dev/README.md).

## API token visibility

Use a full-access key for the verified quickstart. Disabling
`full_access` on an administrator's key can hide servers when the account has no
server roles. An API request can then return HTTP 200 with an empty collection
while the server still exists. Creation permission alone does not guarantee
visibility of the new server.

**Provider 0.1.1 cannot distinguish this from external deletion for an established
resource:** after three missing observations, a plan may propose creating a server
that already exists. If this happens after changing a token or roles, stop before
apply, restore the original full-access token/account access, and run plan again.
Version 0.1.2 fixes this behavior: three missing observations cause a read
error and preserve the existing state instead of proposing a duplicate. This fix
is not included in the published 0.1.1 provider. After independently confirming
real deletion, back up state and remove only the affected resource from state
before recreating it; automatic recreation after collection absence is disabled.

Do not use state removal as a workaround for lost visibility. The pending-state
recovery procedure applies only after confirming real deletion.
See [verification results and remaining scope](docs/verification.md).

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

Memory uses Crafty's Java download units: each input unit produces **1000 JVM MiB**, so `mem_min = 1` and `mem_max = 2` generate `-Xms1000M -Xmx2000M`. These are not exact GiB. Inputs remain integers with `1 <= mem_min <= mem_max`; the port must be 1–65535. Names require at least two characters and exclude slashes, backslashes, and `#`. Other input strings must not be empty. See the [verified API contract and supported versions](docs/api-contract.md).

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
- GET `/api/v2/servers` reads visible server objects and finds the server by ID. Crafty also returns server objects from single GET, contrary to the specification's role schema; the provider retains collection reads because single GET uses ambiguous HTTP 400 `NOT_AUTHORIZED` for a missing ID. Three consecutive successful collection responses without an established server cause a read error and preserve Terraform state. An empty or filtered collection cannot distinguish deletion from lost token access. A server whose post-create/update refresh is pending retains its ID while preparation or API recovery continues. A collection 404 is an endpoint error, not proof that the server was deleted.
- PATCH `/api/v2/servers/{serverID}` updates name, automatic start, monitoring address/port, and launch command without replacing the ID. Only changed, known settings are sent; an explicit `false` is preserved. There is no documented PUT operation.
- DELETE `/api/v2/servers/{serverID}` deletes the server. HTTP 404 is treated as already deleted.
- HTTP 401/403, other non-success HTTP statuses, malformed JSON, and application-level unsuccessful statuses become English Terraform diagnostics.
- Creation inputs remain in state. Reads refresh name, automatic start, monitoring address/port, and launch command. Explicit optional settings are reconciled on apply; omitted settings are observed without enforcing a default. GET also exposes the execution command and current monitoring address/port, but does not reconstruct the complete original download payload or server.properties port.
- Changing download inputs replaces the panel record and creates a new directory. The provider's default DELETE preserves files; it does not reuse old worlds during replacement. Review plans, back up data, and manage retained directories separately.
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
- **Crafty acceptance tests** requires every configured Crafty compatibility job to pass. Each run starts a fresh Docker Compose stack, waits for readiness, bootstraps an API key, verifies the recorded API contract, and runs the live Terraform lifecycle suite. It also publishes a contract report on success and failure. Failures upload redacted diagnostics, and cleanup removes the disposable containers and volumes. See [the CI integration guide](dev/README.md#integration-tests-in-github-actions).
- **Tests and build** checks dependencies and formatting, runs Go tests and vet, builds the provider, validates examples, and tests Terraform recovery/replacement behavior. It runs `make check-core test-acc-mock` with the Terraform version pinned in CI.
- **Release snapshot** builds all six platform ZIPs, verifies checksums and archive contents, and tests installation of the native package in a fresh Terraform configuration. It does not publish a release. See [release preparation](docs/releasing.md) and [the changelog](CHANGELOG.md).

Install golangci-lint locally using Homebrew:

```sh
brew install golangci-lint
golangci-lint version
make fmt
make check
```

Use v2.14.0 for exact CI parity; see the [official installation instructions](https://golangci-lint.run/docs/welcome/install/local/) for version-specific binaries. `make fmt` updates Go import/formatting and Terraform examples. `make fmt-check` only checks formatting. `make lint` runs the configured analyzers and checks formatting. The full `make check` also covers module integrity/tidy, generated docs, release configuration, Python helpers, Compose configuration, example validation, and controlled Terraform acceptance. It does not start Crafty. Override executable paths using `GO`, `GOLANGCI_LINT`, `TERRAFORM`, `DOCKER`, and `PYTHON` when needed.

The [Protect main ruleset](https://github.com/bart-kochanowicz/terraform-provider-crafty/rules/24375657) requires a pull request, an up-to-date branch, and all six checks listed above. It also blocks force pushes and deletion of `main`. These GitHub settings are separate from the workflow; maintainers should verify they remain active when changing CI.

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
