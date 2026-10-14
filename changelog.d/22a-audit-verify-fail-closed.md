### Fixed

- **Audit signature verification fails closed when signing is on (#22).**
  With `sign_logs` enabled, `AuditLogger.VerifySignature` now reports an
  event with an empty signature as unverified instead of valid, so stripping
  a signature no longer hides a tampered field. With signing off every event
  still verifies. Records written before signing was enabled carry no
  signature and now report unverified while signing is on; they remain
  readable.
