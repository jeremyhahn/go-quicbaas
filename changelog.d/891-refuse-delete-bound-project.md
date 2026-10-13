### Changed

- **Deleting a project with an SDLC binding is refused (go-quicbaas#891).**
  The delete fails with `ProjectBoundError` before the managed workspace is
  touched, and the message names `quicbaas sdlc unbind` as the first step.
  REST returns `409`, gRPC returns `FailedPrecondition`, and MCP passes the
  remedy through in the tool error. A binding store that cannot be read
  refuses the delete too. Delete never cascades into offboarding. The check
  runs on every node with a tenant store resolver, including nodes with the
  SDLC module disabled.
