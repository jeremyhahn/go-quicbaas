### Added

- **Discard removes concluded attempt workspaces (go-quicbaas#777).** The
  discard stage removes earlier attempts' workspaces, and this attempt's when
  the task completed; a parked attempt keeps its checkout. Removal runs on the
  host holding the workspace (locally, or through the fleet node's new
  `RemoveWorkspace` RPC), refuses links, non-directories and a workspace held
  by a live session, and logs a failure as `WorkspaceRemovalError`. The fleet
  daemon sweeps at start-up and every 15 minutes.
- **Fleet nodes advertise a reachable address (go-quicbaas#795).**
  `quicbaas fleet serve --advertise <host:port>` (also `advertise_addr` in
  config and `QUICBAAS_ADVERTISE_ADDR`) names the address the control plane
  dials. The node binds before it enrolls, and registration refuses an
  address that does not resolve or accept a connection with
  `422 agent_address_unreachable`. Fleet node records carry
  `advertised_address` and `reachability`. A model listing that cannot reach
  the node's daemon answers 503 (502 when the daemon fails) naming the daemon
  address, and the web Inference page shows that message and opens on a
  backend whose models list.
