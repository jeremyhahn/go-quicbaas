### Changed

- **The TypeScript SDK's QUIC transport refuses every verb with a typed reason
  (go-quicbaas#809).** Orchestration, cost and worker-image verbs, plus auth,
  barrier, ceremony, bootstrap, `connect` and `disconnect`, no longer throw a
  bare "not implemented". `QuicNotImplementedError` is now a
  `TransportNotSupportedError` and names the transport, family, method and
  reason. Orchestration and cost refuse because the server's QUIC listener has
  no route for them (go-quicbaas#819); the other families refuse because the
  SDK does not speak qrdb's frame protocol. Nothing is sent over the network.
  The reasons are recorded in `docs/software-factory/api.md`.
