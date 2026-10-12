### Fixed

- **WebAuthn relying-party settings changes and login handoffs are now
  race-free.** A relying-party settings change can no longer leave the old RP
  cached when it races a ceremony that is building the tenant's service. Login
  ceremony handoffs are consumed exactly once and expire with the ceremony
  timeout, so abandoned or failed logins no longer accumulate in memory, and
  two concurrent finishes for one challenge cannot both proceed
  (go-quicbaas#384).
