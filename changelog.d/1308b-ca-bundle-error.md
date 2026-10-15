### Fixed

- **CA issuance fails when the CA bundle cannot be read (go-quicbaas#1308).**
  IssueCertificate, SignCSR, SignCSRPEM (the ACME issue path) and device
  identity issuance discarded the go-xkms CA-bundle read error and returned
  a certificate with an empty chain. They now return a
  `CAServiceAdapterBundleError`, matching `ErrCAServiceAdapterBundle` and
  wrapping the cause, and no certificate.
