### Fixed

- The MCP tool `quicbaas_secrets_set_rotation_policy` now stores the policy it is given: `enabled`, `interval` (a Go duration string such as `36h`), `retain_versions` and `rotation_lambda` reach the secrets service, so `quicbaas_secrets_get_rotation_policy` returns them. A missing `secret_id`, an unparsable interval, a wrong-typed field or an undeclared policy field is refused with a typed invalid-input error before the service is called (#1636).
