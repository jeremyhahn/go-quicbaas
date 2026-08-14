# quicbaas acme

ACME (RFC 8555) certificate operations against the active QuicBaaS daemon.

## Overview

QuicBaaS is the **broker-as-CA**: it *is* the certificate authority. The
protocol-client verbs below are therefore in-process service calls, **not**
JWS-signed exchanges against an external ACME directory.

The verb tree wires to `Security().ACME()`:

- `Server()` — the server-control surface (`register-challenge`, `issue`)
- `Client()` — the broker-as-CA protocol surface (`new-order`,
  `finalize-order`, `renew`)

Source: [`cmd/quicbaas/acme.go`](../../cmd/quicbaas/acme.go).

## Transport exception

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

## Verb summary

| Verb | Surface | Purpose |
|------|---------|---------|
| `register-challenge <name>` | Server | Install a challenge verifier on the server |
| `issue` | Server | Server-side issuance from a CSR + validated challenge |
| `new-order` | Client | Create a broker-as-CA order for one or more identifiers |
| `finalize-order` | Client | Finalize an order by signing the supplied CSR |
| `renew <cert-url>` | Client | Resolve an issued certificate for renewal |

## Flags

`acme` accepts `--output <json|yaml|table>` (default `table`) as a persistent
flag. Global flags are inherited from the root command.

## register-challenge

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

## issue

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

## new-order

```bash
quicbaas acme new-order --identifier dns:api.example.com --challenge-type http-01
quicbaas acme new-order --identifier dns:a.example.com --identifier dns:b.example.com
```

`--identifier` is repeatable and takes `<type>:<value>`; the type is usually
`dns` or `ip`. The returned order carries the finalize URL to hand to
`finalize-order`.

## finalize-order

```bash
quicbaas acme finalize-order --order-url /acme/order/42 --csr ./api.csr
quicbaas acme finalize-order --order-url /acme/order/42 --csr - --cert-output ./api.crt
```

## renew

```bash
quicbaas acme renew 7a4f3c1d
quicbaas acme renew /acme/cert/7a4f3c1d --output json
```

`<cert-url>` is the hex serial the CA assigned at issuance, optionally in
`/acme/cert/<serial>` form. Because the broker-as-CA holds no subscriber
private key, this verb resolves the existing certificate but cannot re-sign a
replacement leaf; use `new-order` + `finalize-order` with a fresh CSR for that.

## Errors

| Type | Condition |
|------|-----------|
| `ACMEFlagRequiredError` | A required flag was not supplied |
| `ACMEArgRequiredError` | The positional argument is missing or blank |
| `ACMEUnknownChallengeError` | The challenge name has no canonical verifier |
| `ACMEIdentifierFormatError` | `--identifier` is not in `<type>:<value>` form |
| `ACMEReadError` / `ACMEWriteError` | Filesystem failure reading a CSR or writing a certificate |
| `ACMEEnrollCAUnsupportedError` | The enroll-01 verifier was handed a CA that cannot verify TCG-CSR-IDEVID |
