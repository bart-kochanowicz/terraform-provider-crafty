# Verified Crafty API contract

## Supported and tested versions

| Component | Verified baseline | Evidence |
| --- | --- | --- |
| Crafty Controller | **4.10.4**, official Linux Docker image | Fresh-instance API probes, Terraform acceptance tests, runtime `version.json` check |
| Image | `registry.gitlab.com/crafty-controller/crafty-4:4.10.4` | Local ARM64; GitHub Actions Ubuntu AMD64 |
| API | `/api/v2`, published specification version **2.0.1** | Official OpenAPI 3.0.4 snapshot retrieved on 2026-10-02 |
| Minecraft creation | Java `download_jar`, **Paper 1.21.1** | Whole and fractional memory probes; provider lifecycle tests |
| Authentication | Superuser full-access API key on a disposable instance | Automatic bootstrap in `dev/ci.py` |

Other Crafty releases, operating systems, engines, Minecraft versions, and restricted
permission combinations have not been certified by these tests. The provider does
not block other versions, but their compatibility is unverified. CI deliberately
rejects a different runtime version until a new baseline is reviewed. A download
catalog entry must also remain available; its availability is outside the provider.

The specification is served by the [official API reference](https://docs.craftycontrol.com/pages/developer-guide/api-reference/v2/).
Its [raw OpenAPI file](https://docs.craftycontrol.com/pages/developer-guide/api-reference/openapi-spec.yml)
has SHA-256 `bfadb88c5bba1996e2a5ffe6102f266ba257eae662470f6210896c16518dfb3b`
for this comparison. The documentation site is mutable; tests use the recorded
[factual schema summary](../internal/client/testdata/crafty-4.10.4/spec-summary.json)
instead of fetching a changing specification during CI. The upstream source tag is
[`v4.10.4`](https://gitlab.com/crafty-controller/crafty-4/-/tree/v4.10.4)
(commit `6394bbc178feebc091b775bd4f6e4e865ffa3e96`).

## Specification versus observed behavior

| Area | Published specification | Observed Crafty 4.10.4 | Provider behavior |
| --- | --- | --- | --- |
| POST Java memory | Integer examples `1` and `2`; no unit declaration | Inputs `1/2` generate `-Xms1000M -Xmx2000M`; `1.5/2.5` generate `1500M/2500M` | Whole integer inputs, passed unchanged; `1 <= mem_min <= mem_max` |
| Collection GET | Array of `Server` objects | Array with string `server_id`/`server_name` and boolean `auto_start` | Refresh name, `auto_start`, monitoring address/port, and execution command by ID |
| Single GET | Schema mistakenly lists `role_id`/`role_name` | A server object, with the same basic fields plus a `status` object | Keep collection reads; do not infer deletion from ambiguous single-GET errors |
| Missing single GET | Not-found semantics unspecified | HTTP **400** with `NOT_AUTHORIZED` after deletion | Do not classify HTTP 400 as not found; it can also mean insufficient access |
| PATCH | Only `server_name` documented | Name, `auto_start`, `server_ip`, `server_port`, and `execution_command` persisted in probes | All five verified fields are exposed as in-place updates |
| Name validation | No documented minimum or exclusion pattern | At least two characters; `/`, `\`, and `#` rejected | Validate these constraints before mutation |
| DELETE | `StatusOK`; no `files` parameter documented | Default deletes panel record and preserves directory; `?files=true` removes directory | Default DELETE, without `files=true`; retained files need separate cleanup |

### Memory units

The Java download path constructs its command through
[`create_api_server`](https://gitlab.com/crafty-controller/crafty-4/-/blob/v4.10.4/app/classes/shared/main_controller.py)
and [`Helpers.float_to_string`](https://gitlab.com/crafty-controller/crafty-4/-/blob/v4.10.4/app/classes/helpers/helpers.py),
which multiplies memory inputs by **1000**. JVM `M` suffixes represent 1024²-byte
units; the [Java command reference](https://docs.oracle.com/javase/8/docs/technotes/tools/unix/java.html)
shows equivalent byte and `m` examples. Thus a Crafty input unit is **1000 MiB**,
not exactly 1 GiB (1024 MiB). `mem_max = 2` produces 2000 MiB of maximum heap.

The provider retains its existing integer inputs and does not add another
conversion. Earlier descriptions labeling them as GiB were inaccurate. This
contract applies to Java `download_jar`; do not extrapolate it to imports, Bedrock,
SteamCMD, or modded-engine installation paths. Fractional inputs are accepted by
the observed API but are not exposed by the Terraform resource's Int64 schema.
The tests inspect configured JVM flags; they do not start Minecraft or measure
actual process memory usage.

### GET fields and drift

The [recorded single response](../internal/client/testdata/crafty-4.10.4/server-response.json)
and [collection response](../internal/client/testdata/crafty-4.10.4/list-response.json)
contain the complete observed field shapes, with identifiers and timestamps
normalized. Both omit the specification's `server_uuid` and `backup_path` fields.
Runtime fields absent from the `Server` schema include `app_id`, `created_by`,
`count_players`, `ignored_exits`, `show_status`, `shutdown_timeout`, and
`update_watcher`; single GET additionally includes `status`.

`type` is `minecraft-java`, not the download engine `paper`. `executable` is
`paper.jar` and does not encode the requested Minecraft version. There are no
standalone original `engine`, `version`, `mem_min`, or `mem_max` creation fields.
Memory appears in `execution_command`; current monitoring configuration appears
in `server_ip` and `server_port`. Neither reconstructs the complete original
creation payload or the separate `server.properties` port.

The provider refreshes name, `auto_start`, monitoring address/port, and execution
command. Explicit optional settings are reconciled on apply; omitted settings are
observed. Other creation inputs remain in state by design; their retention is not a claim that
GET cannot return monitoring settings. The client tolerates extra response fields
and distinguishes a missing boolean from a valid `false` value.

### PATCH capabilities and resource scope

The live probe persists five fields in one PATCH and confirms each with GET:
`server_name`, `auto_start`, `server_ip`, `server_port`, and `execution_command`.
It also confirms that `mem_min` as a direct PATCH field is rejected with HTTP 400
`INVALID_JSON_SCHEMA`. The
[upstream handler](https://gitlab.com/crafty-controller/crafty-4/-/blob/v4.10.4/app/classes/web/routes/api/servers/server/index.py)
accepts additional schema fields and applies different schemas to superusers and
ordinary users. Their full behavior and restricted-token access are not covered
by these probes.

Changing `server_port` updates monitoring while leaving the generated
`server.properties` unchanged. Updating an arbitrary execution command is not an
equivalent, engine-independent update of the download inputs. The provider exposes
name, optional `auto_start`, `monitoring_host`,
`monitoring_port`, and `execution_command` as mutable fields. Original RAM, engine,
version, host, and port inputs still require replacement. Explicit launch commands
override generated RAM flags without changing stored creation inputs. This is a
deliberate provider scope, not an asserted restriction of the Crafty API.

ID-only import remains unavailable because the verified GET fields do not reliably
reconstruct the required original download payload. The Terraform acceptance suite
tests the explicit diagnostic without changing the created server. See the
[resource import limitations](resources/minecraft_server.md#import).

### Deletion

Tests confirm both deletion modes on servers they create. The provider sends
plain DELETE and preserves world directories in this verified version. A replacement
creates a new server directory; it does not reuse the old world automatically.
Review replacement plans and manage retained files separately. CI's final Compose
teardown removes the entire disposable project's volumes, including retained files.

A successful collection response contains servers visible to the current token.
Absence proves that the ID is no longer listed for that token; it does not distinguish
physical deletion from revoked access. The provider requires three consecutive
successful absences before removing established resources from state. API errors
retain state, and pending post-create/update refreshes also retain identity.

## Automated evidence and extending support

`COMPOSE_PROJECT_NAME="crafty-provider-ci-local-$(date +%s)" make test-acc-ci`
reproduces automatic authentication, the live contract probes, Terraform tests, and
teardown. It uses a fresh instance on loopback port 18001. The probe checks the runtime
version, GET field types, RAM conversion, PATCH persistence and rejections, separate
monitoring/file ports, and both DELETE modes. It generates ignored
`bin/ci-logs/api-contract.json`, containing only contract facts, not credentials,
server paths, or Terraform state. GitHub Actions publishes that report for seven
days on both success and failure; broader diagnostics are uploaded on failure.

`make test` exercises typed client requests and responses against recorded fixtures,
including the ambiguous HTTP 400 case, and verifies provider name validation.
Python helper tests check response comparisons and safe error diagnostics.
`make test-acc` remains the Terraform lifecycle suite against a manually configured
instance; the container/file contract probes belong to `make test-acc-ci`.

To certify another version, start a fresh official image, verify its runtime
version, compare its schemas and observed responses with the recorded specification,
run all probes and Terraform scenarios (including mutable settings, drift, import
rejection, and post-create settings failure recovery), and review the resulting differences.
Update the fixtures, version guard, support table, and CI pin together. An image
starting successfully is not sufficient evidence of API compatibility.
