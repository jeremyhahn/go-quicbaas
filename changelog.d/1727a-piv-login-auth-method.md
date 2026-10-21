### Fixed

- First boot now adds `mtls` to the permitted-method policy of the system admin whose PIV certificate it binds. Before this fix the admin kept the `webauthn` default that `CreateUser` assigns. The certificate login at `POST /api/v1/auth/login/piv` checks that policy for `mtls`, so it refused the bound certificate with 401 "authentication failed". Every integration helper that logs in as the system admin failed the same way (#1727).
- A node that serves an operator-supplied TLS certificate now also trusts the platform CA for client certificates. Before this fix its listener named only the operator CA, so a client holding a certificate the platform CA signed was never asked for it. That covers every cert-mode tenant admin and agent mTLS cert. Its PIV login answered 401 "client certificate required" (#1727).
- Cert-mode tenant provisioning now adds `mtls` to the tenant admin's permitted-method policy when it enrolls the issued certificate, the same as first boot (#1727).
