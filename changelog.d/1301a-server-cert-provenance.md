### Fixed

- **A self-issued server cert at a configured path stays managed; an absent
  configured path is refused (go-quicbaas#1301).** When
  `security.tls_cert_file` and `security.tls_key_file` hold the certificate
  this server's CA stores for the server CN, the boot now runs the SAN check
  on it and writes a re-issued certificate back to those paths. Before, any
  file at a configured path was served as the operator's and never
  re-issued. A boot with only one of the two paths configured, or with a
  configured path that does not exist, now fails with a
  `ServerCertPathError` (matching `ErrServerCertPath`) and writes no file.
  Before, it issued a certificate into the configured paths. The server
  issues its own certificate only when neither path is configured.
