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

The supplied PATCH schema only supports `server_name`. Download inputs are retained in Terraform state because they cannot be reconstructed from GET. Drift detection covers the name and computed automatic-start setting. A server missing from a successful list response is removed from state. DELETE 404 is treated as already deleted; a collection GET 404 remains an error.

Replacement and deletion can remove world files. Back up server data and review the Terraform plan. Creation saves the returned server ID before refreshing; if preparation is still in progress, rerun the plan after Crafty completes it.

The provider does not start the server, accept the Minecraft EULA, or modify server files. Complete those steps in Crafty.
