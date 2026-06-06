# quicbaas ca

Certificate Authority operations against the active QuicBaaS daemon.

## Overview

The `ca` verb tree wires directly to `Security().Certificate()` over
the active SDK transport (REST / gRPC / QUIC / MCP / Unix /
embedded). Verbs are the canonical set per the locked architecture
spec §6. Phase 8e completed the surface by adding `CAInfo` and
`ListCertificates` so all six verbs are real-wire on every transport.

Source: [`cmd/quicbaas/ca.go`](../../cmd/quicbaas/ca.go).

## Verb summary

| Verb | Purpose |
|------|---------|
| `info` | Show the active issuing CA's parsed metadata |
| `bundle` | Print the CA trust-chain PEM to stdout |
| `list` | List certificates stored in the CA cert store |
| `issue` | Issue a fresh certificate (key generated server-side) |
| `revoke` | Revoke a previously-issued certificate by hex serial |
| `export-bundle` | Write the CA bundle PEM to a file on disk |

## Global flags

`ca` accepts `--output <json|table>` (default `table`) as a
persistent flag.

## info

Display metadata for the active issuing CA: Common Name,
subject/issuer, serial, validity window, key type/size, signature
algorithm.

```bash
quicbaas ca info
quicbaas ca info --output json
```

Backed by `Security().Certificate().CAInfo()` (added Phase 8e).

## bundle

Print the PEM-encoded CA trust-chain bundle to stdout. The output is
the raw concatenated certificate chain (issuer-first).

```bash
quicbaas ca bundle > /etc/pki/quicbaas-ca.pem

# JSON form (chain wrapped in an object)
quicbaas ca bundle --output json
```

For writing to a file with an operator-visible confirmation message,
use `export-bundle` instead.

## list

Enumerate certificates currently held in the CA cert store. By
default only currently-valid (non-revoked, non-expired) certificates
are returned.

```bash
quicbaas ca list
quicbaas ca list --common-name api.       # substring filter
quicbaas ca list --include-expired
quicbaas ca list --include-revoked
quicbaas ca list --limit 50
```

| Flag | Default | Description |
|------|---------|-------------|
| `--common-name <substr>` | (none) | Case-insensitive substring filter on Common Name |
| `--include-revoked` | `false` | Include revoked certificates in the response |
| `--include-expired` | `false` | Include expired certificates in the response |
| `--limit <int>` | `0` | Maximum certificates to return (0 = no caller-supplied limit) |

The table output shows COMMON NAME / SERIAL / NOT BEFORE / NOT AFTER
/ IS CA columns plus a `(showing X of Y total)` footer so a truncated
list is obvious.

Backed by `Security().Certificate().ListCertificates()` (added Phase
8e).

## issue

Issue a fresh certificate from the active CA. The server generates a
new keypair and signs a certificate with the supplied subject. The
response includes the certificate PEM, chain PEM, and the
server-generated private-key PEM.

```bash
quicbaas ca issue \
  --common-name api.example.com \
  --dns api.example.com \
  --validity-days 90

quicbaas ca issue \
  --common-name client-1 \
  --usage client \
  --key-algorithm ecdsa
```

| Flag | Default | Description |
|------|---------|-------------|
| `--common-name <string>` | (required) | Certificate Common Name |
| `--organization <string>` | (none) | Subject Organization |
| `--dns <name>` (repeatable) | (none) | DNS SAN entries |
| `--ip <addr>` (repeatable) | (none) | IP SAN entries |
| `--validity-days <int>` | `0` (profile default) | Certificate validity in days |
| `--usage <profile>` | (server profile default) | `client` / `server` / `both` |
| `--key-algorithm <name>` | (profile default) | `ecdsa` / `rsa` |
| `--key-size <int>` | `0` (algorithm default) | Key size in bits |

For client certificates, store the returned `PrivateKeyPEM` securely;
the server does not retain a copy.

## revoke

Revoke a previously-issued certificate by hex serial.

```bash
quicbaas ca revoke 7a4f3c1d
quicbaas ca revoke 0x7a4f3c1d           # 0x prefix accepted
```

The serial is the hex-encoded big-integer serial returned by `issue`
or `list`. Revocation writes a CRL entry and propagates through the
CA cert store.

Output (table form):

```
certificate 7a4f3c1d revoked
```

## export-bundle

Write the PEM-encoded CA trust-chain bundle to a file on disk.

```bash
quicbaas ca export-bundle /etc/pki/quicbaas-ca.pem
quicbaas ca export-bundle -          # write to stdout
```

The file is created with `0o644` permissions, overwriting any
existing file at that path. Use `-` to send the bundle to stdout
(equivalent to `quicbaas ca bundle`, but with `export-bundle`'s
operator-confirmation message).

Output:

```
CA bundle written to /etc/pki/quicbaas-ca.pem (3284 bytes)
```

## Error types

| Type | When |
|------|------|
| `CAArgRequiredError` | Verb invoked without its required positional argument |
| `CAFlagRequiredError` | Verb invoked without a required flag |
| `CAExportWriteError` | `export-bundle` could not write to the target path |

`CAExportWriteError` wraps the underlying `os.WriteFile` error and
implements `Unwrap` so `errors.Is(err, os.ErrPermission)` traverses
correctly.

## Transport coverage

All six CA verbs work on every transport (Phase 8e closed the
matrix):

| Transport | info | bundle | list | issue | revoke | export-bundle |
|-----------|------|--------|------|-------|--------|---------------|
| REST | yes | yes | yes | yes | yes | yes |
| gRPC | yes | yes | yes | yes | yes | yes |
| QUIC | yes | yes | yes | yes | yes | yes |
| MCP | yes | yes | yes | yes | yes | yes |
| Unix | yes | yes | yes | yes | yes | yes |
| Embedded | yes | yes | yes | yes | yes | yes |

## See also

- [`docs/sdk/security.md`](../sdk/security.md) —
  `Security().Certificate()` and `Security().CertificateExt()`.
- [`docs/credentials/README.md`](../credentials/README.md) — the
  broader credentials engine that the CA verbs back.
- [`docs/api/xkms-compatibility.md`](../api/xkms-compatibility.md) —
  legacy xkms `/api/v1/xkms/ca/*` routes (compat bridge).
