# Certificate Authority — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to issuing, verifying and revoking certificates, and to
> ACME. Feature folder: `docs/certificate-authority/`.

The CA is how everything in the deployment gets an identity it can prove: node
certificates, client certificates for mTLS, and ACME-issued server certificates.

## Prerequisites

- An initialised, unsealed server. The CA's signing key lives under the barrier
  ([`../barrier/user.md`](../barrier/user.md)); a sealed server cannot sign.
- A principal holding the certificate permissions. They are per verb —
  `certs:read` for inspection, `certs:generate` for issuance — and the split is
  the useful one: a service can be allowed to read the CA bundle without being
  allowed to mint certificates.

## First run

```bash
# Is the CA initialised?
quicbaas ca info

# Fetch the trust anchor. This is what a client needs to verify anything.
quicbaas ca bundle

# Issue a certificate.
quicbaas ca issue

# See what has been issued.
quicbaas ca list
```

The `ca` group has exactly six verbs (`cmd/quicbaas/ca.go`): `info`, `bundle`,
`list`, `issue`, `revoke <serial>` and `export-bundle <path>`. There is no
`ca verify` and no `ca sign-csr` at the CLI — CSR signing exists in the API and
during the initialisation ceremony (`quicbaas init sign-csr`), not as a
standalone CA verb.

## Common tasks

### Distribute the trust anchor

```bash
quicbaas ca bundle > /etc/quicbaas/ca-chain.crt
quicbaas --tls-ca /etc/quicbaas/ca-chain.crt iam user list
```

Everything that connects needs this. `GET /api/v1/certificate/ca/bundle` is the
route behind it, and it requires only `certs:read`.

### Export the bundle to a file

```bash
quicbaas ca export-bundle /etc/quicbaas/ca-chain.crt
```

### Revoke

```bash
quicbaas ca list                 # find the serial
quicbaas ca revoke <serial>
```

`revoke` takes a **serial**, not a subject or a filename. Revocation is only as
effective as CRL distribution: revoking without publishing a fresh CRL leaves
every verifier still accepting the certificate. CRL generation is an API
operation (`GenerateCRL`) with no CLI verb — see the caveat below.

### Get a certificate over ACME

```bash
quicbaas acme register-challenge <name>
quicbaas acme new-order
quicbaas acme finalize-order
quicbaas acme issue
quicbaas acme renew <cert-url>
```

Five verbs (`cmd/quicbaas/acme.go`). `issue` is the single-shot path; the
`new-order` / `finalize-order` pair is the explicit one when you need to see or
influence what happens between them. `renew` takes the certificate URL the order
returned, not a name.

The surface is RFC 8555 and the routes are under `/api/v1/acme`, following the
directory-URL convention. `api.md` has them.

### Manage trust anchors

```bash
quicbaas trust publish-dane
quicbaas trust cross-signed-roots
quicbaas trust auto-import-aia
```

Three verbs (`cmd/quicbaas/trust.go`). Trust controls which anchors this
deployment **accepts**, as distinct from which certificates it **issues**.
Confusing the two is how a deployment ends up trusting an issuer nobody chose —
`auto-import-aia` in particular follows Authority Information Access pointers,
so understand what it will import before enabling it.

## Two things that catch people

**A sealed barrier means no issuance.** Every signing operation needs the key,
and the key is under the barrier. `ca info` failing right after a restart is
almost always this — check `barrier status` first.

**Three methods reach unregistered routes.** `GenerateCRL`, `IsInitialized` and
`IsRevoked` fall through the REST transport's route map to its default branch
and are issued as `POST /api/v1/certificate/generatecrl`,
`.../isinitialized` and `.../isrevoked` — paths the server does not register.
Those three therefore fail over REST and QUIC while working over gRPC, MCP and
embedded. See the matrix in [api.md](api.md). Until it is fixed, reach them with
`--transport grpc` (or `unix`, which is gRPC over a socket).

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `ca info` fails after a restart | The barrier is sealed. Unseal it. |
| Clients reject a freshly issued certificate | They do not have the CA bundle, or they have an older one. Redistribute. |
| A revoked certificate is still accepted | The CRL was not regenerated or not published. |
| `GenerateCRL` / `IsRevoked` / `IsInitialized` fail over REST but work elsewhere | Known route mismatch — see above and [api.md](api.md). Use `--transport grpc`. |
| `ca revoke` rejects the argument | It takes the certificate **serial**. Get it from `ca list`. |
| ACME order fails validation | The challenge could not be served. ACME validates reachability; a private deployment usually needs DNS-01 rather than HTTP-01. |
| `permission denied` reading the bundle | `certs:read` is separate from `certs:generate`. Grant the narrower one. |

## What this feature does not do

- **It is not a public CA.** Certificates it issues are trusted by whoever
  installs its bundle.
- **It does not distribute the bundle.** Fetching it is a command; getting it
  onto every host is your configuration management's job.
- **It does not rotate the CA key on a schedule.** That is a deliberate,
  planned operation with a redistribution step.
