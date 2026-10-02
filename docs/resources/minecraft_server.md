---
page_title: "crafty_minecraft_server Resource"
description: "Manage a Minecraft Java server downloaded by Crafty."
---

# crafty_minecraft_server

Creates a Minecraft Java server using Crafty's `download_jar` operation. Bedrock servers and importing existing servers are not supported.

```hcl
resource "crafty_minecraft_server" "example" {
  name    = "Terraform Minecraft"
  engine  = "paper"
  version = "1.21.1"
  mem_min = 1
  mem_max = 2
  host    = "127.0.0.1"
  port    = 25565

  timeouts {
    create = "15m"
    read   = "2m"
    update = "5m"
    delete = "5m"
  }
}
```

## Schema

| Attribute | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | String | Yes | Server name; supports in-place updates. |
| `engine` | String | Yes | Crafty download engine identifier; changes replace the server. |
| `version` | String | Yes | Supported Minecraft download version; changes replace the server. |
| `mem_min` | Int64 | Yes | Minimum Java memory; at least 1. Changes replace the server. |
| `mem_max` | Int64 | Yes | Maximum Java memory; at least `mem_min`. Changes replace the server. |
| `host` | String | Yes | Monitoring host reachable from Crafty; changes replace the server. |
| `port` | Int64 | Yes | Monitoring and server.properties port, from 1 to 65535; changes replace the server. |
| `id` | String | Computed | Crafty server identifier. |
| `auto_start` | Boolean | Computed | Automatic-start setting; read-only. |

Memory follows Crafty's GiB convention; the supplied OpenAPI specification does not explicitly state units, so verify against your installed Crafty version. The engine and version must exist in the Crafty download catalog.

## Lifecycle and limitations

Creation uses POST `/api/v2/servers`; reads use GET `/api/v2/servers`; updates and deletion use PATCH and DELETE `/api/v2/servers/{serverID}`. The supplied single-server GET schema describes role fields, so reads locate the server in the documented collection response.

The supplied PATCH schema only supports `server_name`. Download inputs are retained in Terraform state because they cannot be reconstructed from GET. Drift detection covers the name and computed automatic-start setting. An established server missing from three consecutive successful list responses is removed from state. A server with a pending post-create/update refresh remains in state until its metadata can be read. DELETE 404 is treated as already deleted; a collection GET 404 remains an error.

Replacement and deletion can remove world files. Back up server data and review the Terraform plan.

## Timeouts and recovery

The optional `timeouts` block accepts positive Go duration strings such as `30s`,
`5m`, or `1h`. Defaults are `create = "10m"`, `read = "2m"`, `update = "5m"`,
and `delete = "5m"`. Each timeout covers the mutation and subsequent polling;
individual HTTP requests are also capped at 120 seconds. Caller cancellation
interrupts requests and retry delays.

Only GET requests are repeated. Transient HTTP 408, 429, 500, 502, 503, and 504
responses, transport timeouts, interrupted connections, and temporary DNS failures
use exponential delays from 1 to 10 seconds. A larger `Retry-After` is respected
without extending the operation deadline. Authentication errors, collection 404,
malformed JSON, and unsuccessful application statuses are not retried.
During preparation, incomplete metadata and an unconverged name are polled too.

Creation saves `new_server_id` and a private pending-refresh marker before its
follow-up GET. A successful refresh clears the marker. If refresh times out or
fails after an ID was returned, apply reports a warning and preserves identity
without tainting the resource. Later plans resume the read and retain the ID while
visibility is unresolved. Inspect Crafty and rerun `terraform plan` after recovery.
A resource that never becomes visible requires investigation; it is not automatically
replaced merely because a pending read timed out. The check confirms complete API
metadata and the requested name, not Minecraft startup or download success.

An accepted rename similarly saves the applied name and the existing computed
values before polling. An accepted delete polls for absence; a failed verification
returns an error while preserving identity for a later destroy. DELETE 404 remains
idempotent. Established-resource reads retain state on API errors and require three
consecutive successful absences before removing state.

POST, PATCH, and DELETE are never automatically replayed. A lost create response
without an ID cannot be reconciled safely: inspect the panel before retrying to
avoid duplicate servers.

The provider does not start the server, accept the Minecraft EULA, or modify server files. Complete those steps in Crafty.
