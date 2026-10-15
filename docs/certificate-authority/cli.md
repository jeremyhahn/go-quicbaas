# Certificate Authority — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-14
> Every verb and flag, with runnable examples. Feature folder: `docs/certificate-authority/`.

The `ca`, `acme` and `trust` verbs reach go-xkms services. What a CA, ACME or
trust operation does is documented once, in go-xkms:
[CA CLI](../../../go-xkms/docs/certificate-authority/cli.md),
[ACME CLI](../../../go-xkms/docs/acme-issuance/cli.md),
[Trust Bootstrap CLI](../../../go-xkms/docs/trust-bootstrap/cli.md). This page
lists only the QuicBaaS verbs, their flags and their transports.

> **Being removed.** The `ca` and `acme` verbs run today on QuicBaaS's own
> certificate service and `/api/v1/acme/*` verbs, a parallel implementation
> removed on the owner's word ([SPEC](../SPEC.md) rows `certificate-authority`
> and `acme`). The verbs are documented here because the code still ships.

## Overview

The `ca` verb tree wires to `Security().Certificate()` over the active SDK
transport (REST / gRPC / QUIC / MCP / Unix / embedded).

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
or `list`. Revocation and CRL publication are go-xkms behaviour; see its
[CA user guide](../../../go-xkms/docs/certificate-authority/user.md).

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

- [`../go-sdk/api.md`](../go-sdk/api.md) — `Security().Certificate()` and
  `Security().CertificateExt()`.
- [`../composition/api.md`](../composition/api.md) — the mounted go-xkms
  `/api/v1/xkms/ca/*` routes.


---

## quicbaas acme

ACME verbs against the active QuicBaaS daemon. ACME itself (orders,
challenges, `enroll-01`, renewal) is go-xkms's: see
[ACME Issuance — CLI](../../../go-xkms/docs/acme-issuance/cli.md) and its
[user guide](../../../go-xkms/docs/acme-issuance/user.md).

> **Being removed.** These verbs drive QuicBaaS's own `/api/v1/acme/*` control
> verbs and ACME handling, a parallel implementation removed on the owner's
> word ([SPEC](../SPEC.md) row `acme`). Its replacement is the go-xkms ACME
> service re-exported through `sdk/go`.

Source: [`cmd/quicbaas/acme.go`](../../cmd/quicbaas/acme.go). The verb tree
wires to `Security().ACME()`: `Server()` for `register-challenge` and `issue`,
`Client()` for `new-order`, `finalize-order` and `renew`.

### Transports

There are no gRPC, QUIC or MCP opcodes for ACME. When `--transport` is `grpc`,
`quic`, `unix` or `mcp`, `Security().ACME()` builds a dedicated REST transport
to the same `--address` and caches it; when no REST address can be derived it
returns a typed transport error naming the reason. `--transport rest` and
`--transport embedded` use the main transport directly.

### Verbs and flags

`acme` accepts `--output <json|yaml|table>` (default `table`) as a persistent
flag.

| Verb | Flags |
|------|-------|
| `register-challenge <name>` | `<name>` is `dns-01`, `endorse-01`, `enroll-01` or `http-01` |
| `issue` | `--csr <path\|->` (required), `--challenge-type` (required), `--challenge-value`, `--cert-output` |
| `new-order` | `--identifier <type>:<value>` (repeatable), `--challenge-type` |
| `finalize-order` | `--order-url`, `--csr <path\|->`, `--cert-output` |
| `renew <cert-url>` | `<cert-url>` is the hex serial, optionally as `/acme/cert/<serial>` |

```bash
quicbaas acme register-challenge http-01
quicbaas acme issue --csr ./api.csr --challenge-type http-01 --challenge-value api.example.com
quicbaas acme new-order --identifier dns:api.example.com --challenge-type http-01
quicbaas acme finalize-order --order-url /acme/order/42 --csr ./api.csr --cert-output ./api.crt
quicbaas acme renew 7a4f3c1d
```

### Errors

| Type | Condition |
|------|-----------|
| `ACMEFlagRequiredError` | A required flag was not supplied |
| `ACMEArgRequiredError` | The positional argument is missing or blank |
| `ACMEUnknownChallengeError` | The challenge name has no canonical verifier |
| `ACMEIdentifierFormatError` | `--identifier` is not in `<type>:<value>` form |
| `ACMEReadError` / `ACMEWriteError` | Filesystem failure reading a CSR or writing a certificate |
| `ACMEEnrollCAUnsupportedError` | The enroll-01 verifier was handed a CA that cannot verify TCG-CSR-IDEVID |


---

## quicbaas trust

Trust artifact operations against the active QuicBaaS daemon.

### Overview

The `trust` verb tree wires directly to `Security().Trust()` over the active
SDK transport (REST / gRPC / QUIC / MCP / Unix / embedded).

Trust-**bootstrap** (DANE / Noise / SPKI / Direct fetch of the initial CA
bundle, i.e. `Security().Trust().Bootstrap()`) is **not** duplicated here — it
has a canonical home at `quicbaas bootstrap`. This tree covers
the three server-side trust-artifact verbs only. What DANE publication,
cross-signed roots and AIA import do is go-xkms behaviour, documented in its
[DANE](../../../go-xkms/docs/ca/dane.md) and
[Trust Bootstrap](../../../go-xkms/docs/trust-bootstrap/architecture.md) pages.

Source: [`cmd/quicbaas/trust.go`](../../cmd/quicbaas/trust.go).

### Verb summary

| Verb | Purpose |
|------|---------|
| `publish-dane` | Publish DANE TLSA records for a `(name, port, cert)` tuple |
| `cross-signed-roots` | Fetch the cross-signed root pool used to bridge issuers |
| `auto-import-aia` | Walk a chain's AIA extensions and import the referenced CAs |

### Flags

`trust` accepts `--output <json|yaml|table>` (default `table`) as a persistent
flag. Global flags are inherited from the root command.

### publish-dane

```bash
quicbaas trust publish-dane --name api.example.com --port 443 --cert ./api.crt
cat api.crt | quicbaas trust publish-dane --name api.example.com --port 443 --cert -
```

| Flag | Meaning |
|------|---------|
| `--name` | DNS name the TLSA records are published for (required) |
| `--port` | TCP port the TLSA records are published for (required, 1–65535) |
| `--cert` | PEM-encoded certificate; `-` reads stdin (required) |

> **Requires a DANE publisher.** The server derives the TLSA records and hands
> them to the registered DANE publisher. **QuicBaaS ships none by design** —
> DNS provisioning belongs to the embedding application (go-trusted-platform
> registers one via the server's DANE publisher option). Against a bare
> `quicbaasd`, `PublishDANE` returns `ErrTrustNoDANEPublisher`; the CLI wraps
> that in a `TrustDANEPublisherError` that spells out the cause and where a
> publisher comes from, so the failure is legible rather than cryptic.

### cross-signed-roots

```bash
quicbaas trust cross-signed-roots
quicbaas trust cross-signed-roots --output json
quicbaas trust cross-signed-roots --roots-output ./cross-signed-roots.txt
```

The SDK returns an `*x509.CertPool`, whose only enumeration hook is the set of
DER-encoded subjects, so the output is a **subject listing** rather than PEM
certificates. `--roots-output` writes that listing to a file.

### auto-import-aia

```bash
quicbaas trust auto-import-aia --chain ./leaf-and-intermediates.pem
cat chain.pem | quicbaas trust auto-import-aia --chain -
```

`--chain` accepts a PEM file holding one or more concatenated certificates.
The server walks each certificate's AuthorityInformationAccess extension and
imports every referenced authority certificate into its trust store.

### Errors

| Type | Condition |
|------|-----------|
| `TrustFlagRequiredError` | A required flag was not supplied |
| `TrustInvalidPortError` | `--port` is outside 1–65535 |
| `TrustReadError` / `TrustWriteError` | Filesystem failure reading or writing certificate material |
| `TrustNoCertificatesError` | The supplied PEM held no `CERTIFICATE` blocks |
| `TrustCertificateParseError` | A `CERTIFICATE` block was not valid DER |
| `TrustDANEPublisherError` | `publish-dane` failed; wraps the cause and explains the missing-publisher case |
