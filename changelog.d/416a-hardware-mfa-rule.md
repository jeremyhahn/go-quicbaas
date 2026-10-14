### Changed

- **Proven hardware satisfies `mfa_verified` (go-quicbaas#416).** The rule
  ratified for the release candidate is now stated where the claim is derived
  and pinned by a test: a credential whose WebAuthn or PIV attestation proves
  hardware yields `mfa_verified: true`, and an unattested or software-attested
  credential yields `false`. No behaviour changes. Evidence of a second factor
  (the WebAuthn user-verification flag, the PIV PIN policy) is not yet part of
  the rule; that is the follow-up on go-quicbaas#416.
