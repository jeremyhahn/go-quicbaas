### Changed

- First-party dependencies moved to the current tags in both modules: go-xkms,
  its SDK and xkey to `v0.9.2-alpha` (carries the IDevID signing fix,
  go-xkms#274/#276). Minimal version selection carries go-qrdb and its SDK to
  `v0.16.0-alpha` and go-quicraft to `v0.9.0-alpha`. go-xkms `v0.9.2-alpha`
  requires Go 1.26.7, so the `go` directive, `GO_VERSION` and every
  `golang:1.26.x-alpine` build image move with it. (#536)

- **`GitProvisioner` bounds the SSH agent teardown after a clone.** go-xkms's
  `ConnectSession.SyncAndStop` now takes a context and reports any signing
  operation still running when it elapsed; the clone teardown gives it a
  5-second bound of its own and logs abandoned operations instead of hanging
  on a hardware key awaiting touch. (#536)

- **A seal-disabled node's IAM records now live under the system tenant's
  confined keyspace.** go-qrdb v0.10.0 (#41) made every node expose a
  `StoreResolver`, so IAM writes route through `"{tenant}/"` whether or not
  the seal subsystem is on; the new `Server.IAMKVStore()` is the accessor
  for reading them from outside the IAM components. Pre-release policy: wipe
  and re-bootstrap dev deployments. (#536)

- **A known-hosts pin must carry a well-formed fingerprint.** go-xkms v0.9.x
  validates `SHA256:<unpadded base64 of 32 bytes>` at the store, so the REST
  and CLI paths now reject a malformed fingerprint with a 400 / typed error
  instead of pinning something no host key can ever match. (#536)

- **The broker's email reaches qrdb as the canonical login key.** go-qrdb
  v0.15.0 (#191) refuses a user with no email login key unless the caller
  declares `WithoutEmailLoginKey`; `QRDBUserStorage.CreateUser` now forwards
  the email (and the display name) and declares a certificate-only identity
  when there is none, and a duplicate email maps to `iam.ErrEmailAlreadyExists`.
  First boot was failing at admin-user creation without this. (#536)

- **`RoleAdapter` resolves built-in roles by name for first-boot re-runs.**
  go-qrdb's firstboot refuses to derive a system role's identifier when the
  roles already exist and nothing can resolve them (`UnresolvableSystemRoleError`);
  the adapter now implements `RoleNameResolver` and is wired as
  `FirstBootDependencies.RoleResolver`, so the retry after a partial first boot
  binds the admin grant to the persisted role ID. (#536)

- **Self-service registration is forwarded to both WebAuthn relying parties.**
  go-xkms v0.9.2-alpha reads an unset `AllowPublicRegistration` as closed,
  which would have silently switched off `iam.self_service_registration.enabled`
  on every node; the operator's decision now reaches quicbaas's own per-tenant
  relying party and the composed go-xkms module's, and a test pins that the
  two agree (port of the feature/fabric fix in #516). (#536)
