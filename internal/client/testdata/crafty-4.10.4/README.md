# Crafty 4.10.4 contract fixtures

Captured on 2026-10-02 from a fresh official Docker instance, using a generated
superuser API key and Paper 1.21.1. UUIDs, resource names, and timestamps are
normalized. Server configuration responses contain no authentication material.

- `create-request.json`: successful Java download payload, deliberately using
  different monitoring (25576) and server.properties (25577) ports.
- `create-response.json`: HTTP 201 response with `new_server_id`.
- `list-response.json`: HTTP 200 collection with the created server.
- `server-response.json`: HTTP 200 single-server response before PATCH.
- `patch-response.json`: HTTP 200 acknowledgment after configuration PATCH.
- `unsupported-patch-response.json`: HTTP 400 for direct `mem_min` PATCH.
- `missing-server-response.json`: HTTP 400 `NOT_AUTHORIZED` after deleting the ID.
- `spec-summary.json`: factual field/type projection of the official OpenAPI file,
  with source URL, retrieval date, and SHA-256. It is not a runtime response.

See [the API contract](../../../../docs/api-contract.md) for supported scope,
discrepancies, sources, and the live CI checks. Do not regenerate these fixtures
from a persistent or production instance, and do not add tokens or database files.
