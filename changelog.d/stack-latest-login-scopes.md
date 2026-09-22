### Fixed

- **An identity whose role carries a go-xkms permission can log in again.**
  go-qrdb v0.19.0-alpha removed the implicit self-scope, so a permission
  registered without a declared scope is refused, and
  `rbac.ListPermissions` fails on the *first* unscoped permission an
  identity holds. go-xkms v0.14.0-alpha and v0.15.0-alpha contributed
  `keys:read`, `keys:update`, `keys:write` and `ssh:use` with no scope, and
  token issuance calls `ListPermissions`, so every login for a role holding
  one of them failed with `permission: no scope declared for keys:read`.
  go-xkms v0.16.0-alpha (go-xkms#617, PR #618) declares all four
  `PermissionScopeSelf`, the reach they had before go-qrdb#504, so no
  deployment's authorization outcome changes. Fixed by the dependency bump
  below. `pkg/server/xkms_permission_scope_test.go` pins it from this side:
  the composed xkms vocabulary resolves a scope for `keys:read`, an identity
  holding it obtains a bearer whose `scp` carries it, and an identity
  holding an unrelated registered-but-unscoped permission still gets no
  token at all.

### Changed

- **Every `github.com/jeremyhahn/*` stack module is on its latest tag** (#1002), in
  both `go.mod` and `sdk/go/go.mod`:

  | Module | From | To |
  | --- | --- | --- |
  | `go-xkms`, `go-xkms/sdk/go`, `go-xkms/xkey` | v0.14.0-alpha | v0.16.0-alpha |
  | `go-quicraft` | v0.11.0-alpha | v0.13.1-alpha |
  | `go-frost` | v0.1.8-alpha | v0.3.0-alpha |

  `go-qrdb` (and `sdk/go`) v0.19.0-alpha, `go-enclave` v0.5.0-alpha,
  `go-auth` v0.2.0-alpha, `go-codec` v1.4.0, `go-objstore` v0.3.0-alpha,
  `go-signal` v0.2.0-alpha, `go-libsignal` v0.2.0-alpha and
  `go-truststrap` v0.4.0-alpha were already the latest tags. `golang.org/x`
  modules moved with them (`crypto` v0.57.0, `sys` v0.48.0, `text` v0.42.0,
  `sync` v0.23.0, `term` v0.46.0, `mod` v0.41.0).

  Two go-xkms API changes are absorbed:

  - `KeyConfig.Password` is now a `SecretValue` rather than a string. The CA
    root-key password resolved from `ca.root_key_password` is moved into the
    key config by custody transfer, so the plaintext no longer passes through
    a Go string and the `SA1019` suppression is gone (`pkg/server/lifecycle.go`).
  - go-xkms's REST CA contract gained `CRL(ctx)`, the read-current-CRL verb
    behind its `GET /ca/crl`. `xkmsCAServicer` implements it over
    `TrustedCA.CRL`, wrapping failures in `XKMSCAOperationError`
    (`pkg/api/rest/xkms_ca_servicer.go`). QuicBaaS does not yet route a
    `GET /api/v1/xkms/ca/crl`; `POST` (force-regenerate) is unchanged.
