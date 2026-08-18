# Certificate Authority — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/certificate-authority/`.

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

- [`docs/sdk/security.md`](../ssh/engineering.md) —
  `Security().Certificate()` and `Security().CertificateExt()`.
- [`docs/credentials/README.md`](../credentials/user.md) — the
  broader credentials engine that the CA verbs back.
- [`docs/api/xkms-compatibility.md`](../composition/api.md) —
  legacy xkms `/api/v1/xkms/ca/*` routes (compat bridge).


---

## quicbaas acme

ACME (RFC 8555) certificate operations against the active QuicBaaS daemon.

### Overview

QuicBaaS is the **broker-as-CA**: it *is* the certificate authority. The
protocol-client verbs below are therefore in-process service calls, **not**
JWS-signed exchanges against an external ACME directory.

The verb tree wires to `Security().ACME()`:

- `Server()` — the server-control surface (`register-challenge`, `issue`)
- `Client()` — the broker-as-CA protocol surface (`new-order`,
  `finalize-order`, `renew`)

Source: [`cmd/quicbaas/acme.go`](../../cmd/quicbaas/acme.go).

### Transport exception

ACME is an HTTP-native protocol. Per
[`pkg/service/acme_service.go`](../../pkg/service/acme_service.go) there are
deliberately **no gRPC / QUIC / MCP opcodes for ACME**, so this command group
is an explicit, documented exception to the multi-protocol parity rule.

The SDK compensates: when `--transport` is `grpc`, `quic`, `unix`, or `mcp`,
`Security().ACME()` lazily builds a dedicated REST transport to the same
`--address` and caches it. That requires an address a REST client can dial;
when one cannot be derived the SDK surfaces a typed transport error naming the
reason instead of silently doing nothing. `--transport rest` and
`--transport embedded` use the main transport directly.

The raw RFC 8555 directory endpoints (`/acme/directory`, `/acme/new-order`, …)
are mounted at the REST router root for standards-compliant ACME clients; the
control verbs this CLI drives live under `/api/v1/acme/*`.

### Verb summary

| Verb | Surface | Purpose |
|------|---------|---------|
| `register-challenge <name>` | Server | Install a challenge verifier on the server |
| `issue` | Server | Server-side issuance from a CSR + validated challenge |
| `new-order` | Client | Create a broker-as-CA order for one or more identifiers |
| `finalize-order` | Client | Finalize an order by signing the supplied CSR |
| `renew <cert-url>` | Client | Resolve an issued certificate for renewal |

### Flags

`acme` accepts `--output <json|yaml|table>` (default `table`) as a persistent
flag. Global flags are inherited from the root command.

### register-challenge

```bash
quicbaas acme register-challenge http-01
quicbaas acme register-challenge enroll-01 --output json
```

Valid names: `dns-01`, `endorse-01`, `enroll-01`, `http-01`.

Only the **name** travels over the wire — the server installs its own canonical
verifier for it. The SDK also requires a non-nil process-local verifier (for
callers that additionally embed an ACME server); the CLI supplies thin
signature adapters onto exactly the same SDK-re-exported xkms verifiers the
server installs, so there is no forked verification logic.

### issue

```bash
quicbaas acme issue --csr ./api.csr --challenge-type http-01 --challenge-value api.example.com
quicbaas acme issue --csr - --challenge-type dns-01 --challenge-value _acme-challenge.example.com
quicbaas acme issue --csr ./api.csr --challenge-type http-01 --cert-output ./api.crt
```

| Flag | Meaning |
|------|---------|
| `--csr` | Path to the PEM-encoded CSR; `-` reads stdin (required) |
| `--challenge-type` | Challenge type used for validation (required) |
| `--challenge-value` | Challenge value (identifier) that was validated |
| `--cert-output` | Write the issued certificate PEM to this path |

The challenge must already have been validated; the server signs the CSR with
the composed CA and returns the leaf plus the issuing chain.

### new-order

```bash
quicbaas acme new-order --identifier dns:api.example.com --challenge-type http-01
quicbaas acme new-order --identifier dns:a.example.com --identifier dns:b.example.com
```

`--identifier` is repeatable and takes `<type>:<value>`; the type is usually
`dns` or `ip`. The returned order carries the finalize URL to hand to
`finalize-order`.

### finalize-order

```bash
quicbaas acme finalize-order --order-url /acme/order/42 --csr ./api.csr
quicbaas acme finalize-order --order-url /acme/order/42 --csr - --cert-output ./api.crt
```

### renew

```bash
quicbaas acme renew 7a4f3c1d
quicbaas acme renew /acme/cert/7a4f3c1d --output json
```

`<cert-url>` is the hex serial the CA assigned at issuance, optionally in
`/acme/cert/<serial>` form. Because the broker-as-CA holds no subscriber
private key, this verb resolves the existing certificate but cannot re-sign a
replacement leaf; use `new-order` + `finalize-order` with a fresh CSR for that.

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
the three server-side trust-artifact verbs only.

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
