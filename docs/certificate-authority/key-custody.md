# Key Custody — who creates a private key, and where

> **Status: NORMATIVE** · Last verified: 2026-10-14
> Layer Ownership contract for asymmetric key creation. Feature folder: `docs/certificate-authority/`.
> Enforced by `make check-local-keygen`. Ruling: go-quicbaas#910.

## The rule

go-xkms owns keys and certificates for the whole stack. QuicBaaS **composes**
it and creates no asymmetric key of its own, with exactly one exception, in
exactly one file, checked by the build.

| What is needed | Who creates the key | Where it lives | Reached through |
|---|---|---|---|
| Platform key material (CA keys, server TLS, seal/DEK keys) | go-xkms keystore | TPM 2.0 / PKCS#11 / PKCS#8 backend, never exported | `Security().KMS()`, `TrustedCA` |
| A signature over a keystore-resident key | go-xkms | key never leaves the backend | `xkmssdk.NewXKMSSigner`, `Security().Crypto()` |
| A certificate | go-xkms CA | — | `TrustedCA.SignCSR` — the stack's single `x509.CreateCertificate` call |
| A credential the **requester** will hold | `pkg/xkms.GenerateRequesterKey` | the requester's custody; the CA only ever sees the CSR | `Security().Certificate().IssueCertificate`, `/api/v1/xkms/ca/sign-csr` |

## Why the last row exists

go-xkms cannot hand back a private key, on any backend; its keystore custody is
documented in
[Key Management — Architecture](../../../go-xkms/docs/key-management/architecture.md).
So
`TrustedCA.IssueCertificate` returns `IssuedCertificate.PrivateKeyPEM` as
`nil` — always. That is pinned by
`pkg/testutil/xkmsca`'s `TestIssueCertificateNeverExportsThePrivateKey`, against
a real software CA, so the claim cannot go stale.

This is the custody rule, not a gap to route around. go-xkms states it in its
own implementation of the same flow
(`go-xkms/pkg/init/firstboot_adapter.go` `GenerateAdminCert`):

> The key is generated HERE, outside the CA's keystore, deliberately: it is the
> administrator's personal credential, not platform key material, so it must
> never enter the platform's key custody — the CA only ever sees the CSR.

So a credential whose private key belongs to somebody else — the platform
administrator's personal certificate, a deployed fleet node daemon's mTLS identity, a
`ca issue` caller's key pair — is created at the point of custody and certified
through a CSR. Putting it in the platform keystore instead would give the
platform a copy of a credential it has no business holding, and would leave the
requester with a certificate they cannot use.

## The one generator

`pkg/xkms/keygen.go` is the only file in go-quicbaas permitted to call
`ecdsa.GenerateKey`, `rsa.GenerateKey` or `ed25519.GenerateKey`.

Every issuer that needs a requester credential therefore uses the same three
steps — generate, CSR, sign — and **no** issuer calls a CA's
`IssueCertificate`: on that route the key stays in the keystore, so the response
carries a certificate and no credential.

```go
signer, keyAlgorithm, err := xkms.GenerateRequesterKey("ecdsa-p256")
```

It accepts every algorithm spelling the wire and the CLI use (`""`, `ecdsa`,
`p256`, `ecdsa-p384`, `rsa`, `rsa-3072`, `rsa-4096`, `ed25519`, and the
documented aliases), returns a `crypto.Signer` plus the
`x509.PublicKeyAlgorithm` an encoder needs, and reports
`*api.UnsupportedKeyAlgorithmError` (matching `xkms.ErrUnsupportedKeyAlgorithm`)
for anything else. Everything downstream of it is go-xkms:
`xkmssdk.CreateCSRWithKey` builds the CSR, `TrustedCA.SignCSR` signs it, and
`xkmssdk.EncodePrivateKeyPEM` encodes the key for delivery.

### About the entropy source

The generator passes the go-xkms FIPS RNG resolver (`xkms.Reader()`, which
prefers TPM 2.0 then PKCS#11 then software). Be precise about what that buys,
because it is less than it looks:

> Since Go 1.26, a secure source of random bytes is always used, and the Reader
> is ignored unless `GODEBUG=cryptocustomrand=1` is set. This setting will be
> removed in a future Go release.
> — `crypto/ecdsa.GenerateKey`, `crypto/rsa.GenerateKey`

On the ECDSA and RSA paths the reader is **discarded by the toolchain** and the
key comes from Go's FIPS 140-3 approved DRBG. Only `ed25519.GenerateKey` still
consumes what it is handed. Both facts are asserted in `pkg/xkms/keygen_test.go`
(`TestGenerateRequesterKey_XKMSResolverIsOnTheEd25519Path`,
`TestGenerateRequesterKey_StdlibIgnoresTheReader`) so neither this document nor
the code comment can drift from the toolchain.

The value of the single generator is therefore **not** the entropy. It is that
there is one audited site instead of six, and a build gate that keeps it that
way.

## The build gate

```
make check-local-keygen
```

`scripts/localkeygen` parses `pkg/`, `cmd/` and `sdk/` and fails on any
`ecdsa.GenerateKey`, `rsa.GenerateKey`, `ed25519.GenerateKey`,
`x509.CreateCertificate` or `x509.CreateCertificateRequest` in non-test code. It
runs as a prerequisite of `make test`, `test-minimal`, `test-standard`,
`test-lean` and `test-desktop`.

Resolution is syntactic, so it needs no build tags and no module loading and
runs on a tree that does not compile. Test files are out of scope — a fixture
minting a throwaway self-signed certificate is not a key-custody decision — and
`-include-tests` reports them anyway for an audit.

A site that genuinely has no alternative waives itself in place, with a reason:

```go
// local-keygen-ok: <why nothing else can work here>
```

An empty reason is not a waiver, and a waiver covers its own vicinity rather
than the whole function. This mirrors `check-coded-sentinels`.

### Current waivers

| Site | Reason |
|---|---|
| `pkg/xkms/keygen.go` (6) | The audited generator itself. |
| `pkg/signal/service.go` (2) | Signal-protocol X3DH identity and signed-prekey material, not PKI. It is never certified by the CA and never presented in a TLS handshake; the prekey is signed by the identity key, not by any authority. Whether this material belongs under go-xkms custody at all is an open architecture question. |

## Where each requester credential comes from

| Credential | Path | Key created by | Signed by |
|---|---|---|---|
| Platform admin (`<data-dir>/admin/`) | qrdb first-boot Path A | go-xkms `CAAdapter.GenerateAdminCert` | go-xkms CA, client profile, system-tenant SPIFFE SAN |
| Platform admin, operator-supplied CSR | qrdb first-boot Path B | the operator, off-host | go-xkms CA |
| Platform admin, presented certificate | qrdb first-boot Path C | nothing is created | already issued elsewhere; adopted after validation |
| Fleet node daemon / per-session agent MCP identity | `service.XKMSFleetNodeCertIssuer` | `xkms.GenerateRequesterKey` | embedded go-xkms CA, `clientAuth`, tenant SPIFFE SAN |
| Agent identity over an SDK transport | `service.SDKFleetNodeCertIssuer` | `xkms.GenerateRequesterKey` | remote go-xkms CA via `CAService.SignCSR` |
| `quicbaas fleet deploy` | `cmd/quicbaas` `sdkFleetNodeCertIssuer` | `xkms.GenerateRequesterKey`, on the operator's host | remote CA via `/api/v1/xkms/ca/sign-csr` |
| `quicbaas ca issue` | `caServiceAdapter.IssueCertificate` (QuicBaaS's own CA adapter, being removed: [SPEC](../SPEC.md) row `certificate-authority`) | `xkms.GenerateRequesterKey` | embedded go-xkms CA |
| `/api/v1/xkms/ca/issue` (mounted go-xkms REST CA) | go-xkms keystore | go-xkms, non-exportable | go-xkms CA. **Returns no private key** — use the CSR-sign route or the certificate service. |

Requester keys are held in an mlocked `secure.Buffer` and destroyed as soon as
the material reaches its destination. The admin bundle and the per-session agent
directory are written owner-only (`0600` key, `0700` directory).

## See Also

- `pkg/xkms/keygen.go` — the generator and the full reasoning
- `scripts/localkeygen` — the gate
- [Engineering](engineering.md) — TLS and certificate configuration
- [KMS](../kms/engineering.md) — key management for data encryption
- Repo-root `CLAUDE.md` — Layer Ownership and the SDK boundary
