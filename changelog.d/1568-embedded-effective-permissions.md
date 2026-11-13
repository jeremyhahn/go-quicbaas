### Fixed

- **The embedded transport serves IAM Policy GetEffectivePermissions
  (go-quicbaas#1568).** The call evaluates the subject's actions on the
  request's ResourceType through the same policy engine the REST route uses;
  before, it failed with an unknown-method error.
