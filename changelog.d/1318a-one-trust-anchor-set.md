### Removed

- **`iam.mtls.tls_client_auth_ca_file` and `iam.piv_login.ca_file`
  (go-quicbaas#1318).** `security.tls_ca_file` is now the one client
  trust-anchor set. The TLS handshake verifies client certs against it on
  both the operator-cert and the self-issued paths. The `tls_client_auth`
  authenticator, PIV login and PIV invite activation trust the same set.
  The config loader refuses a config that sets either removed key with
  `RemovedKeyError`, which names the key and `security.tls_ca_file`. To
  migrate, move that CA into `security.tls_ca_file`.
  - A configured `tls_ca_file` that cannot be read, or holds no
    certificate, now stops the boot with `ClientTrustAnchorsError`.
  - The self-issued path no longer overwrites an operator `tls_ca_file`.
