### Changed

- **The registration flag's on leg and cert-mode gRPC provisioning are now
  pinned by tests (go-quicbaas#358).** The integration stack gains
  `test-selfreg`, a single-node daemon with
  `iam.self_service_registration.enabled=true` and admin approval off. A new
  IAM integration test registers against it anonymously with a software
  WebAuthn authenticator and asserts the tenant is created with its built-in
  roles seeded, the registrant holds Admin in it, and the registrant can
  invite a member. New gRPC unit tests cover `ProvisionTenant` in cert mode: a
  valid CSR yields the issued certificate, and a missing, malformed,
  mismatched-key or below-policy CSR is refused `InvalidArgument` with the
  typed provisioning error.
