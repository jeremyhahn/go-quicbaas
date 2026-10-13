# Attestation — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-01
> Every verb and flag, with runnable examples. Feature folder: `docs/attestation/`.

`quicbaas attestation` routes through the active `--transport` and acts on the
authenticated tenant. The global `--tenant` flag has **no effect** on these
verbs: the server reads the tenant from your credentials only. `--output`
selects `table` (default), `json` or `yaml`.

| Command | Flags |
|---|---|
| `attestation endorsement put <name>` | `--kind ak-root\|reference-signer`, `--certificate FILE` or `--public-key FILE` |
| `attestation endorsement list` | `--kind` |
| `attestation endorsement delete <name>` | — |
| `attestation reference-values put <name>` | `--corim FILE` (COSE_Sign1 signed CoRIM) |
| `attestation reference-values get <name>` | `--corim-out FILE` |
| `attestation reference-values list` | — |
| `attestation reference-values delete <name>` | — |
| `attestation appraise-quote` | `--ak-cert FILE --quote FILE --signature FILE --nonce HEX --pcr IDX=HEX… [--event-log FILE] --reference-values NAME` |

```bash
quicbaas attestation endorsement put ak-ca --kind ak-root --certificate ak-ca.pem
quicbaas attestation endorsement put rvp --kind reference-signer --certificate rvp-ca.pem
quicbaas attestation reference-values put golden --corim golden.corim
quicbaas attestation appraise-quote --ak-cert ak.pem --quote quote.bin --signature quote.sig \
  --nonce 6e6f6e6365 --pcr 0=<hex> --pcr 7=<hex> --reference-values golden --output json
```

## Refusals before any call

The CLI rejects these locally and exits non-zero without contacting the server:

- `reference-values put` without `--corim`; `appraise-quote` without
  `--ak-cert`, `--quote` or `--signature`; any named file that cannot be read.
- `--nonce` that is not hex.
- `--pcr` that is not `IDX=HEX` with a decimal index.

`--kind` on `endorsement put` is checked by the server, which refuses a
missing or unknown kind.

## Exit status of `appraise-quote`

A completed appraisal exits 0 whatever its outcome. Evidence that fails is a
result with `verified: false`, not an error, so scripts must read `verified`
and `cryptographic_failure` from `--output json`. A non-zero exit means the
request itself failed: a local refusal above, a denied permission or an
unreachable server. A reference-values name that is not stored is not an
error: the `baseline` check reports `absent`.

## Verbs with no CLI

`Verify`, `VerifyChain`, `VerifyImageSignature` and `AttestContainer` have no
`quicbaas attestation` verb. Call them through the SDK
(`client.Security().Attestation()`), REST or MCP; see [api.md](api.md).
