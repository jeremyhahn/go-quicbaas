### Changed

- **The TypeScript SDK's Unix transport refuses every verb with
  `TransportNotSupportedError` (go-quicbaas#809).** The server's Unix socket
  serves native gRPC only, and the SDK has no Node gRPC client. The
  orchestrator, auth and connection verbs therefore reject with the typed
  refusal naming the transport, family, method and reason, as projects, sdlc
  and accounts already did. `UnixNotImplementedError` is removed: its message
  pointed at a framing protocol the server does not speak.
