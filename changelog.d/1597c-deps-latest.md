### Changed

- Third-party requires moved to their latest release at least 7 days old
  (#1597), in `go.mod` and, where it requires them, `sdk/go/go.mod`:
  - `cloud.google.com/go/kms` v1.31.0 to v1.35.0
  - `github.com/aws/aws-sdk-go-v2` v1.43.5 to v1.47.3
  - `github.com/aws/aws-sdk-go-v2/config` v1.32.36 to v1.33.9
  - `github.com/aws/aws-sdk-go-v2/credentials` v1.19.35 to v1.20.9
  - `github.com/aws/aws-sdk-go-v2/feature/ec2/imds` v1.18.36 to v1.20.3
  - `github.com/aws/aws-sdk-go-v2/internal/configsources` v1.4.36 to v1.5.6
  - `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2` v2.7.36 to v2.8.6
  - `github.com/aws/aws-sdk-go-v2/internal/v4a` v1.4.37 to v1.5.6
  - `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding` v1.13.16 to v1.13.21
  - `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url` v1.13.36 to v1.14.6
  - `github.com/aws/aws-sdk-go-v2/service/kms` v1.52.2 to v1.61.5
  - `github.com/aws/aws-sdk-go-v2/service/signin` v1.5.5 to v1.10.4
  - `github.com/aws/aws-sdk-go-v2/service/sso` v1.33.5 to v1.38.4
  - `github.com/aws/aws-sdk-go-v2/service/ssooidc` v1.38.5 to v1.43.4
  - `github.com/aws/aws-sdk-go-v2/service/sts` v1.45.5 to v1.51.4
  - `github.com/aws/smithy-go` v1.27.7 to v1.28.5
  - `github.com/anatol/devmapper.go` v0.0.0-20250316020617-2671eefd35d7 to
    v0.0.0-20260315162726-c47dfb6cf674 (`sdk/go` only)
  - `github.com/cockroachdb/pebble/v2` v2.1.4 to v2.1.7
  - `github.com/coder/websocket` v1.8.14 to v1.8.15
  - `github.com/descope/virtualwebauthn` v1.0.3 to v1.0.5
  - `github.com/grpc-ecosystem/grpc-gateway/v2` v2.29.0 to v2.30.0
  - `github.com/jackc/pgx/v5` v5.10.0 to v5.11.0
  - `github.com/pkg/sftp` v1.13.9 to v1.13.11
  - `github.com/prometheus/client_golang` v1.24.1 to v1.25.0
  - `github.com/prometheus/common` v0.70.1 to v0.72.0
  - `github.com/prometheus/procfs` v0.21.1 to v0.22.0
  - `github.com/redis/go-redis/v9` v9.18.0 to v9.23.0
  - `github.com/tidwall/btree` v1.8.1 to v1.8.2
  - `go.etcd.io/etcd/api/v3` v3.6.8 to v3.7.2
  - `go.mongodb.org/mongo-driver` v1.17.9 to v1.17.10
  - `go.uber.org/atomic` v1.11.0 to v1.12.0
  - `golang.org/x/crypto` v0.57.0 to v0.58.0
  - `golang.org/x/mod` v0.41.0 to v0.42.0
  - `golang.org/x/net` v0.60.0 to v0.61.0
  - `golang.org/x/oauth2` v0.36.0 to v0.37.0
  - `golang.org/x/telemetry` v0.0.0-20260811182544-a038080d80e5 to
    v0.0.0-20260924152758-ed294f943157
  - `golang.org/x/term` v0.46.0 to v0.47.0
  - `golang.org/x/text` v0.42.0 to v0.43.0
  - `golang.org/x/tools` v0.49.0 to v0.51.0
  - `google.golang.org/genproto/googleapis/api` and `.../rpc`
    v0.0.0-20260810153831-ec0a7760b754 to v0.0.0-20260928230214-8a89bd6388cc
  - `google.golang.org/grpc` v1.83.2 to v1.84.0
- Requires still held carry a `// held:` comment naming the reason: chi
  (v5.5.0 adds the QUERY method to every route), go-jose, quic-go, wails
  and mcp-go (latest release under 7 days old), go-webauthn (v0.18.2 not
  yet available to the build), and OpenTelemetry (v1.47.0 needs
  `go.opentelemetry.io/otel/metric/x` v0.69.0, not yet available).
