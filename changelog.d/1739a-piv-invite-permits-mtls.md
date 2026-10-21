### Fixed

- PIV invite activation (`POST /api/v1/iam/{tenant_id}/invites/{token}/activate/piv`) now adds `mtls` beside `piv` to the activated user's permitted-method policy. Before this fix the policy kept `piv` only. The certificate login at `POST /api/v1/auth/login/piv` checks the policy for `mtls`, so it refused the activated certificate with 401. An empty policy, which permits every method, is left as it is (#1739).
