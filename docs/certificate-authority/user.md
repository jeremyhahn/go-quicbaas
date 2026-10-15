# Certificate Authority — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-14
> How the go-xkms CA, ACME and trust services are reached through QuicBaaS.
> Feature folder: `docs/certificate-authority/`.

The certificate authority, ACME and trust anchors are go-xkms services.
go-xkms documents them once; this page covers only what QuicBaaS adds: the
CLI verbs, the permissions and the transports that reach them.

| To learn about | Read (go-xkms) |
|---|---|
| Issuing, checking and revoking certificates; CRLs; profiles | [Certificate Authority — User Guide](../../../go-xkms/docs/certificate-authority/user.md) |
| Getting a certificate over ACME (RFC 8555, `enroll-01`) | [ACME Issuance — User Guide](../../../go-xkms/docs/acme-issuance/user.md) |
| Getting the trust bundle safely before you trust the server | [Trust Bootstrap — User Guide](../../../go-xkms/docs/trust-bootstrap/user.md) |

> **Being removed.** The `ca` and `acme` verbs below still run on QuicBaaS's
> own certificate service and its own `/api/v1/certificate` and
> `/api/v1/acme/*` routes. That is a parallel implementation, removed on the
> owner's word ([SPEC](../SPEC.md) rows `certificate-authority` and `acme`).
> Its replacement is the go-xkms CA and ACME services re-exported through
> `sdk/go`. Do not build on the current verbs' wire routes.

## Prerequisites

- An initialised, unsealed server. The CA's signing key rests under the
  barrier ([`../barrier/user.md`](../barrier/user.md)); a sealed server cannot
  sign.
- A principal holding the certificate permissions: `certs:read` to read the
  bundle or list certificates, `certs:generate` to issue. A service can be
  allowed to read the bundle without being allowed to mint certificates.

## CLI verbs

| Group | Verbs | Source |
|---|---|---|
| `ca` | `info`, `bundle`, `list`, `issue`, `revoke <serial>`, `export-bundle <path>` | `cmd/quicbaas/ca.go` |
| `acme` | `register-challenge <name>`, `issue`, `new-order`, `finalize-order`, `renew <cert-url>` | `cmd/quicbaas/acme.go` |
| `trust` | `publish-dane`, `cross-signed-roots`, `auto-import-aia` | `cmd/quicbaas/trust.go` |

Flags and examples are in [cli.md](cli.md).

### Distribute the trust anchor

```bash
quicbaas ca bundle > /etc/quicbaas/ca-chain.crt
quicbaas --tls-ca /etc/quicbaas/ca-chain.crt iam user list
```

Everything that connects needs the bundle. Fetching it requires only
`certs:read`.

## Two things that catch people

**A sealed barrier means no issuance.** `ca info` failing right after a restart
is almost always this; check `barrier status` first.

**Three methods do not reach the server over REST.** `GenerateCRL`,
`IsInitialized` and `IsRevoked` fail over REST and QUIC and work over gRPC,
Unix, MCP and embedded. See [api.md](api.md). Until then, use
`--transport grpc`.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `ca info` fails after a restart | The barrier is sealed. Unseal it. |
| `GenerateCRL` / `IsRevoked` / `IsInitialized` fail over REST but work elsewhere | Known route mismatch; see [api.md](api.md). Use `--transport grpc`. |
| `ca revoke` rejects the argument | It takes the certificate **serial**. Get it from `ca list`. |
| http-01 or enroll-01 validation is refused for a device on a private network | QuicBaaS's own ACME handling refuses reserved addresses by default. Set `server.acme.allow_private_challenge_targets: true` when devices enroll from a private network. |
| `permission denied` reading the bundle | `certs:read` is separate from `certs:generate`. Grant the narrower one. |

For CA and ACME behaviour itself (revocation and CRLs, challenge validation,
profiles), see the go-xkms pages in the table at the top.
