### Added

- **`quicbaas iam invite show` and `quicbaas iam invite activate-piv`
  (go-quicbaas#358).** `show` prints a pending invite's user, permitted auth
  methods and expiry without consuming it. `activate-piv` binds a PEM PIV
  certificate to the pending user and activates them. It refuses a file that
  is not an X.509 certificate before any call. Both use the root `--tenant`
  and a `--token` flag, and go through the SDK `InviteClient` on REST, gRPC,
  QUIC, Unix and embedded. MCP serves no invite tools pending an owner ruling,
  so on `--transport mcp` both verbs fail with the SDK's error. New CLI
  integration tests cover `iam tenant provision` in cert and invite mode,
  then show and redeem the invite.
