### Changed

- **CA, ACME and TPM documentation points at go-xkms (go-quicbaas#1258).**
  A feature go-quicbaas re-exports from go-xkms is documented once, in
  go-xkms. `docs/certificate-authority/` now keeps only the go-quicbaas delta
  (transport matrix, permissions, CLI verbs, TLS configuration keys, key
  custody) and links to the go-xkms CA, ACME, trust-bootstrap and DANE pages.
  The built-in certificate generator, OpenSSL CA tutorial, certificate
  key-type tables, broker-as-CA ACME prose and profile semantics are removed.
  QuicBaaS's own certificate service and `/api/v1/acme/*` verbs are marked as
  being removed, per the SPEC rows. The attestation, composition,
  security-hardening, go-sdk and ssh pages link to go-xkms for the TPM 2.0,
  TPM-policy, attestation-evidence and ACME behaviour they re-export.
