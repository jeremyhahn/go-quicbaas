### Added

- **A project can be bound paused (go-quicbaas#1041).** `quicbaas sdlc bind
  --paused` binds a project without switching its autonomy on, so it can be
  staged and switched on later with `quicbaas project update --autonomous`.
  A paused bind never switches autonomy off, and the flag is not stored on
  the binding. It is REST `?paused=true` (a value that is not a boolean is a
  `400`), gRPC `BindProjectRequest.paused`, the MCP `sdlc_bind` argument
  `paused`, and `BindRequest.Paused` / `bind(..., paused?)` in the Go and
  TypeScript SDKs.
