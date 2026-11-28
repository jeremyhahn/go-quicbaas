### Added

- **User approve and reject over REST, QUIC and MCP (go-quicbaas#1689).**
  `sdk/go/client/iam` `Approve` and `Reject` now reach quicbaasd's approve
  and reject routes on the REST and QUIC transports, and quicbaasd registers
  the `quicbaas_iam_user_approve` and `quicbaas_iam_user_reject` MCP tools
  under the same `users:enable` and `users:disable` permissions.
- **Policy `GetEffectivePermissions` over REST and QUIC (go-quicbaas#1689).**
  `sdk/go/client/iam` `GetEffectivePermissions` now reaches quicbaasd's
  `POST /api/v1/tenants/{tenant_id}/policies/effective-permissions` route on
  the REST and QUIC transports and returns the server's per-resource result as
  the SDK's flat permission list.
