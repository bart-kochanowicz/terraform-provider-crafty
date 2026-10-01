# Terraform Provider Crafty

A Terraform Plugin Framework provider for Crafty Controller v4's v2 API.
Provider address: `registry.terraform.io/bart-kochanowicz/crafty`.
Go module: `github.com/bart-kochanowicz/terraform-provider-crafty`.

## Project layout

The layout follows HashiCorp's [Plugin Framework scaffolding](https://github.com/hashicorp/terraform-provider-scaffolding-framework), with a separate internal API client.

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
- Crafty Controller v4 with a trusted TLS certificate and an API token with server creation, access, and configuration permissions.
- A supported engine/version pair available in Crafty's download catalog.

## Build and local installation

```sh
go mod tidy
make fmt vet test build
make install
cd examples/local
terraform init
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

Provider attributes `url` and `token` are required strings. `url` is the panel base URL, without `/api/v2`. `token` is sensitive and is sent as a Bearer token. TLS verification remains enabled. Redirects are rejected to avoid sending credentials to another endpoint. Requests have a 120-second timeout. API response bodies are omitted from errors to protect secrets.

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
| `auto_start` | Boolean | Server `auto_start` | Computed, read-only |

Memory uses Crafty's Java download inputs (GiB), not bytes. The supplied specification gives integer examples of 1 and 2 but does not explicitly declare units; confirm this convention for your installed Crafty version. Memory must satisfy `1 <= mem_min <= mem_max`; the port must be 1–65535. Input strings must not be empty.

## API scope and specification limitations

- POST `/api/v2/servers` creates a Minecraft Java server using `minecraft_java` and `download_jar`.
- GET `/api/v2/servers` reads the documented `Server` objects and finds the server by ID. The supplied single-server GET schema incorrectly contains role fields, so it is deliberately not used. A missing server in a successful collection response removes the resource from Terraform state. A collection 404 is an endpoint error, not proof that the server was deleted.
- PATCH `/api/v2/servers/{serverID}` updates only `server_name`, the sole documented PATCH field. There is no documented PUT operation.
- DELETE `/api/v2/servers/{serverID}` deletes the server. HTTP 404 is treated as already deleted.
- HTTP 401/403, other non-success HTTP statuses, malformed JSON, and application-level unsuccessful statuses become English Terraform diagnostics.
- Download inputs are not returned by the Server schema and remain in state. Drift detection covers the name and computed automatic-start setting; it cannot verify RAM, engine, version, or download-time monitoring configuration.
- Changing download inputs replaces the server and can delete its files. Review plans and back up world data before applying replacements or running `terraform destroy`.
- Creation can finish preparing asynchronously. The ID is saved before the post-create read. If a created server is not visible yet, rerun `terraform plan` after preparation completes; inspect the panel before retrying a creation whose response was lost.
- Bedrock creation and importing existing servers are not implemented. The supplied API cannot reconstruct Java download inputs for an import.
- The provider does not start the server, accept Minecraft EULA on your behalf, or modify server files. Complete required setup in Crafty.

## Testing

```sh
make test vet build
```

Unit tests use local HTTP servers and do not require Crafty credentials. For a live smoke test, apply the example to a disposable Crafty instance, change `name`, verify the PATCH update, run another plan to check convergence, and finally run `terraform destroy`. Terraform state may contain sensitive data; keep it out of version control.

## Code quality and continuous integration

GitHub Actions runs `.github/workflows/ci.yml` for pull requests, pushes to `main` (including merges), merge queues, and manual dispatches. No Crafty instance or API secrets are required.

- **Lint** uses golangci-lint **v2.14.0** with `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`. It also checks `gofmt` and `goimports` formatting.
- **Tests and build** checks dependency integrity and whether `go mod tidy` changes tracked module files, checks Go and Terraform formatting, runs uncached tests with the race detector, runs `go vet`, builds the provider, and validates the Terraform example with a development override.

Install golangci-lint locally using Homebrew:

```sh
brew install golangci-lint
golangci-lint version
make fmt
make check
```

Use v2.14.0 for exact CI parity; see the [official installation instructions](https://golangci-lint.run/docs/welcome/install/local/) for version-specific binaries. `make fmt` updates Go import/formatting and Terraform examples. `make fmt-check` only checks formatting. `make lint` runs the configured analyzers and checks formatting. Override executable paths using `GO`, `GOLANGCI_LINT`, and `TERRAFORM` when needed.

To prevent merging failing changes, configure a GitHub branch ruleset for `main` and require the **Lint** and **Tests and build** status checks after their first workflow run. The workflow alone runs checks but does not enforce branch protection.
