### Changed

- **`code_challenge_method` is required (go-quicbaas#1435).** Per
  draft-ietf-oauth-v2-1-16 §4.1.1, an authorization request that sends
  `code_challenge` without `code_challenge_method` is now refused with
  `invalid_request` (`iam.ErrPKCEMethodRequired`) instead of defaulting to
  S256. Clients must send `code_challenge_method=S256`; `plain` stays refused.
