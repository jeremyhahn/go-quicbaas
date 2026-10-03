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
