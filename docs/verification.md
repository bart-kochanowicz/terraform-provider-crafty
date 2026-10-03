# Quickstart and visibility verification

Verified on 2026-10-03 with Terraform 1.16.4 on macOS ARM64, the published
Crafty provider 0.1.1, and a fresh Crafty Controller 4.10.4 Compose project.
Minecraft was not started and its EULA was not accepted.

## Published-provider quickstart

- Initialized a fresh Terraform directory using direct Registry installation,
  without a local mirror, plugin cache or development override. Terraform accepted
  the developer signature and installed 0.1.1.
- Validated the Docker example and reviewed a plan with one resource to create.
- Applied creation and confirmed the returned ID exists in the real Crafty API.
- Changed only the name, reviewed an update plan, applied it and confirmed the
  same ID and new name in Crafty. The subsequent plan reported no changes.
- Destroyed the resource, confirmed an empty state list and absence in Crafty.

The environment used isolated volumes and the CI Compose override's loopback API
on port 18001, because other local projects were running. The README's normal
Compose path uses port 18000 and exposes the dashboard on 8443. These changes
affect test isolation, not provider operations.

Credentials and a full-access API key were obtained through the existing automatic
bootstrap helper. Browser login, changing the initial password, copying a key
from the dashboard, and the human interaction with Terraform confirmation prompts
were **not** manually verified. CLI operations used saved plans and automatic
confirmation after checking their actions.

## Pending-state recovery

A loopback proxy forwarded mutations to real Crafty but deliberately returned
HTTP 403 for reads during creation. This deterministic fault injection exercised
the pending state without changing Crafty's implementation or permissions.

- Create retained the returned ID and sent only one POST after the read failed.
- An administrator confirmed the server existed, then deleted this disposable
  server outside Terraform and confirmed its absence with a full-access token.
- After restoring normal reads, plan retained the pending ID and sent no new POST.
- Saved a private state backup outside the repository, verified its contents,
  performed `state rm -dry-run`, and removed only the example resource from state.
- Reviewed a plan proposing creation, applied it, confirmed a new ID, then
  destroyed it and verified an empty state list.

State backups and credentials are not included in this document or repository.
The procedure does not restore retained world directories.

## Restricted-token finding in 0.1.1

These were exploratory probes, not a claim of general restricted-token support.
The disposable administrator account had no server roles.

- An API key with `full_access=false` returned a successful empty collection
  while a full-access key could still see the same existing server.
- A key with zero Crafty/server permission masks was denied POST, PATCH and DELETE
  with HTTP 400 `NOT_AUTHORIZED`.
- Granting `SERVER_CREATION` and `CONFIG` allowed POST but did not make the created
  server visible to the role-less account's restricted key. No minimum permission
  recommendation follows from those two bits alone.
- After creating an established Terraform resource with a full-access key,
  switching to a restricted key caused a plan to propose a new create while the
  original server still existed. That plan was **not applied**. Access was restored
  and the original resource was destroyed using its full-access key.

This confirms that three successful missing observations cannot distinguish
deletion from lost visibility. Provider 0.1.1 retains pending IDs, but its
established-resource absence handling can propose duplicate creation after access
changes. Stop before applying such a plan and restore access.

The next provider change should cover this regression and define safe handling of
ambiguous absence. Further tests must cover ordinary accounts, roles assigned to
new servers, loss of roles using the same token, and the complete permission
matrix. Full-access keys remain the verified quickstart baseline.
