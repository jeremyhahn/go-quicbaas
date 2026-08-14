# quicbaas trust

Trust artifact operations against the active QuicBaaS daemon.

## Overview

The `trust` verb tree wires directly to `Security().Trust()` over the active
SDK transport (REST / gRPC / QUIC / MCP / Unix / embedded).

Trust-**bootstrap** (DANE / Noise / SPKI / Direct fetch of the initial CA
bundle, i.e. `Security().Trust().Bootstrap()`) is **not** duplicated here — it
has a canonical home at [`quicbaas bootstrap`](bootstrap.md). This tree covers
the three server-side trust-artifact verbs only.

Source: [`cmd/quicbaas/trust.go`](../../cmd/quicbaas/trust.go).

## Verb summary

| Verb | Purpose |
|------|---------|
| `publish-dane` | Publish DANE TLSA records for a `(name, port, cert)` tuple |
| `cross-signed-roots` | Fetch the cross-signed root pool used to bridge issuers |
| `auto-import-aia` | Walk a chain's AIA extensions and import the referenced CAs |

## Flags

`trust` accepts `--output <json|yaml|table>` (default `table`) as a persistent
flag. Global flags are inherited from the root command.

## publish-dane

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

## cross-signed-roots

```bash
quicbaas trust cross-signed-roots
quicbaas trust cross-signed-roots --output json
quicbaas trust cross-signed-roots --roots-output ./cross-signed-roots.txt
```

The SDK returns an `*x509.CertPool`, whose only enumeration hook is the set of
DER-encoded subjects, so the output is a **subject listing** rather than PEM
certificates. `--roots-output` writes that listing to a file.

## auto-import-aia

```bash
quicbaas trust auto-import-aia --chain ./leaf-and-intermediates.pem
cat chain.pem | quicbaas trust auto-import-aia --chain -
```

`--chain` accepts a PEM file holding one or more concatenated certificates.
The server walks each certificate's AuthorityInformationAccess extension and
imports every referenced authority certificate into its trust store.

## Errors

| Type | Condition |
|------|-----------|
| `TrustFlagRequiredError` | A required flag was not supplied |
| `TrustInvalidPortError` | `--port` is outside 1–65535 |
| `TrustReadError` / `TrustWriteError` | Filesystem failure reading or writing certificate material |
| `TrustNoCertificatesError` | The supplied PEM held no `CERTIFICATE` blocks |
| `TrustCertificateParseError` | A `CERTIFICATE` block was not valid DER |
| `TrustDANEPublisherError` | `publish-dane` failed; wraps the cause and explains the missing-publisher case |
