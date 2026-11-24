### Fixed

- Remote fleet deploys ask the CA for the fleet-node profile, which the server signs as a TLS client certificate with SPIFFE kind svc, so the enrollment check accepts certificates from a QuicBaaS provider (#1657).
- The IAM configuration reference documents `iam.login_rate_per_min` (#1775, #1901).
