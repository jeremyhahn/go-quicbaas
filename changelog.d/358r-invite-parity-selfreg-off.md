### Changed

- **Invite routing on QUIC and Unix, and the registration flag's off leg, are
  now pinned by tests (go-quicbaas#358).** New SDK transport tests assert
  `quicbaas.IAM.Invite` reaches the per-tenant route
  `/api/v1/iam/{tenant_id}/invites/{token}` over QUIC and delegates every
  invite verb over Unix, and that a request without the tenant is refused
  before it is sent. A new IAM integration test asserts that with
  `iam.self_service_registration.enabled` at its default, both registration
  steps answer `403 self_registration_disabled`.
