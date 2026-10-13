# Attestation — User Guide

> **Status: NORMATIVE** · Last verified: 2026-10-01
> What it is for and how to use it. Feature folder: `docs/attestation/`.

The Verifier answers one question for a relying party: *is this TPM-backed
host in a state someone trustworthy signed off?*

1. **Endorse** the attestation-key CA (`ak-root`) and each reference-value
   provider (`reference-signer`).
2. **Publish reference values**: the provider signs a CoRIM listing the
   expected PCR digests; store it under a name (one per platform profile, say).
3. **Appraise** each quote the attester sends against that name. Act on the
   result: `verified` admits the host; `cryptographic_failure` means evidence
   was forged and the host should be quarantined; anything else (an absent
   check) means the evidence was incomplete and the host is denied service
   until it attests properly.

The Verifier never learns a baseline from a device. If no endorsed, signed
reference values match, the host does not verify.

```bash
quicbaas attestation endorsement put ak-ca --kind ak-root --certificate ak-ca.pem
quicbaas attestation endorsement put rvp --kind reference-signer --certificate rvp-ca.pem
quicbaas attestation reference-values put golden --corim golden.corim
quicbaas attestation appraise-quote --ak-cert ak.pem --quote quote.bin --signature quote.sig \
  --nonce <hex> --pcr 0=<hex> --pcr 7=<hex> --reference-values golden --output json
```

Every check in the result carries a reason. The common ones:

| Check reports | Meaning | Fix |
|---|---|---|
| `ak-certificate` absent: no `ak-root` endorsement is stored | Nothing to chain the AK certificate to | Endorse the AK CA as `ak-root` |
| `baseline` absent: reference values "…" not found | No reference values stored under that name | Publish them, or pass the right `--reference-values` |
| `baseline` absent: reference values are no longer endorsed | The `reference-signer` that signed them was deleted or rotated away | Store the new signer's endorsement, then re-publish |
| `baseline` absent: quoted PCR set differs | The quote covers other registers than the reference values list | Quote the registers the reference values name |
| any check `forged` | Evidence was present and wrong | Quarantine the host |

`appraise-quote` exits 0 when the appraisal completes, even when the host does
not verify: read `verified` from the output.
