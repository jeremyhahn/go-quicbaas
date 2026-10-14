### Changed

- **Self-service registration is proved end to end over REST
  (go-quicbaas#358).** A new integration test boots the daemon with
  `iam.self_service_registration.enabled` off and on. Off, both
  `register/begin` and `register/finish` answer 403
  `self_registration_disabled` and no tenant is written. On, a software
  ES256 passkey completes the ceremony: the tenant is created, the registrant
  holds its Admin role, the session cannot read another tenant's users, a
  replayed finish is refused, and the passkey signs the registrant in through
  the FIDO2 login route. The software authenticator is a shared integration
  helper, `helpers.SoftwarePasskey`.
