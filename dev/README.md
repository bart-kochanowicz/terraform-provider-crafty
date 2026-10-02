# Local Crafty integration environment

This Docker Compose stack runs a disposable Crafty installation for provider development. It uses the [official Crafty image](https://docs.craftycontrol.com/pages/getting-started/installation/docker/), which supports ARM64 and AMD64. Crafty is pinned to 4.10.4; set `CRAFTY_IMAGE` to test another official tag or digest.

## Start and inspect

Start Docker Desktop, OrbStack, or another Docker engine. From the repository root:

```sh
make dev-check
make dev-up
make dev-status
```

The first start downloads images and initializes Crafty; it can take several minutes. `make dev-up` waits up to 240 seconds for readiness after image downloads. If it times out, inspect `make dev-logs` and rerun after initialization.

| Endpoint | Purpose |
| --- | --- |
| `https://localhost:8443` | Crafty dashboard; accept the generated certificate for this local instance. |
| `http://127.0.0.1:18000/api/v2/` | Loopback API bridge used by Terraform. |
| `127.0.0.1:25565` | Minecraft connection after starting the server in Crafty. |

All published ports bind to loopback. The API bridge only forwards `/api/v2/`, disables access logging, and accepts Crafty's generated certificate inside the Compose network. It is exclusively for disposable local development. Do not expose it to a network or reuse this TLS setup for production. The provider's TLS behavior is unchanged.

## Initial login and API token

Run this locally to display the generated administrator credentials:

```sh
make dev-credentials
```

Sign in to the dashboard and change the initial password. Create an API key in your user's API Keys settings. For this isolated development instance, a superuser API key is the simplest way to ensure creation and access to newly created servers. Keep it private and do not commit it. See [Crafty's initial access documentation](https://docs.craftycontrol.com/pages/getting-started/access/) and [API token permissions](https://gitlab.com/crafty-controller/crafty-4/-/issues/209).

## Test the local provider

From the repository root:

```sh
make dev-provider
export TF_CLI_CONFIG_FILE="$PWD/bin/dev.tfrc"
cd examples/docker
terraform validate
read -rs TF_VAR_crafty_token
export TF_VAR_crafty_token
terraform plan
terraform apply
```

`make dev-provider` builds the provider and writes an ignored, project-local CLI configuration with a development override. Do not run `terraform init` with this override for this example. No global Terraform configuration is changed. The example defaults to the loopback API bridge; set `TF_VAR_crafty_url` to override it.

The engine/version must be available in Crafty's download catalog. The example requests Paper 1.21.1, 1–2 GiB of Java memory, and port 25565. Allow several GiB of RAM for the container if you intend to run Minecraft. Server creation needs internet access to download the server executable.

Verify the server appears in Crafty. Change the example's `name`, run `terraform plan` and `terraform apply`, then run another plan to verify convergence. Download-time changes replace the server. The provider does not start Minecraft or accept its EULA; complete required setup in Crafty before testing port 25565.

To test deletion:

```sh
terraform destroy
unset TF_VAR_crafty_token
unset TF_CLI_CONFIG_FILE
```

These are manual integration tests against real Crafty. Unit tests remain independent of Docker.

## Automated acceptance tests

The acceptance suite uses `terraform-plugin-testing` and a local Terraform binary
(1.5 or newer). From the repository root, with an API token created as described above:

```sh
read -rs CRAFTY_TOKEN
export CRAFTY_TOKEN
make test-acc
unset CRAFTY_TOKEN
```

`make test-acc` starts or reuses `dev/compose.yml`, waits for healthy services,
and runs uncached acceptance tests with the race detector and a 30-minute timeout.
It does not stop the stack or remove its volumes. No installed provider or
`make dev-provider` is needed: the suite serves the provider directly over protocol 6.
Unset `TF_CLI_CONFIG_FILE` if it contains development overrides, so Terraform's
test initialization is not affected by your manual smoke-test configuration.

The test creates a uniquely named `tf-acc-*` server, verifies state against the API,
refreshes it, renames it without replacing its ID, checks an empty plan, and lets
the test framework destroy it. `CheckDestroy` polls the API to verify removal.
A Go cleanup callback also deletes this run's server by captured ID or exact
randomized name if a step fails, including creation before an ID reaches state.
Cleanup errors fail the test. Forced termination (`kill -9`, a Go test timeout,
or Docker/API failure) can prevent cleanup; inspect Crafty for the reported
`tf-acc-*` resource and delete it manually after restoring connectivity.
The suite does not delete unrelated servers or volumes.

Optional environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `CRAFTY_URL` | `http://127.0.0.1:18000` | Disposable instance API base URL |
| `CRAFTY_TEST_ENGINE` | `paper` | Engine available in the download catalog |
| `CRAFTY_TEST_VERSION` | `1.21.1` | Version available for that engine |

Creation requires internet access from Crafty. The server uses 1–2 GiB memory
settings and port 25565, but is never started and its EULA is not accepted.
Do not point these tests at a production instance. The token is passed through a
sensitive Terraform input variable; avoid debug logging and keep test artifacts private.

`make test` skips acceptance tests unless `TF_ACC=1` is explicitly set. GitHub
Actions continues to run offline tests; it does not start Crafty or run this suite.

## Stop and preserve data

```sh
make dev-down
```

Named volumes preserve configuration, server worlds, logs, backups, and imports across container recreation. No container data or credentials are stored in the working tree. Stopping the environment does not destroy Terraform state.

To intentionally remove all environment data, first destroy managed resources, then run:

```sh
docker compose -f dev/compose.yml down --volumes
```

This permanently removes the local Crafty database, credentials, and worlds. If volumes are removed before Terraform destroy, the saved state points to servers that no longer exist.
