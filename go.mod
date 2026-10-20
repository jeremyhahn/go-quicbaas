module github.com/jeremyhahn/go-quicbaas

go 1.27.2

// Exclude old genproto monorepo to avoid conflicts with split packages
exclude google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1

require (
	cloud.google.com/go/kms v1.31.0 // held: #1597, the third-party bump to latest
	connectrpc.com/connect v1.21.1
	github.com/aws/aws-sdk-go-v2 v1.43.5 // held: #1597, the third-party bump to latest
	github.com/aws/aws-sdk-go-v2/config v1.32.36 // held: #1597, the third-party bump to latest
	github.com/aws/aws-sdk-go-v2/credentials v1.19.35 // held: #1597, the third-party bump to latest
	github.com/aws/aws-sdk-go-v2/service/kms v1.52.2 // held: #1597, the third-party bump to latest
	github.com/cockroachdb/pebble v1.1.5
	github.com/cockroachdb/pebble/v2 v2.1.4 // held: #1597, the third-party bump to latest
	github.com/coder/websocket v1.8.14 // held: #1597, the third-party bump to latest
	github.com/cyphar/filepath-securejoin v0.7.0
	github.com/descope/virtualwebauthn v1.0.3 // held: #1597, the third-party bump to latest
	github.com/fsnotify/fsnotify v1.10.1
	github.com/go-chi/chi/v5 v5.3.0 // held: #1597, the third-party bump to latest
	github.com/go-jose/go-jose/v4 v4.1.4 // held: #1597, the third-party bump to latest
	github.com/go-webauthn/webauthn v0.17.4 // held: #1597, the third-party bump to latest
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/go-tpm v0.9.8
	github.com/google/uuid v1.6.0
	github.com/hashicorp/go-msgpack/v2 v2.1.5
	github.com/hashicorp/vault/api v1.23.0
	github.com/jeremyhahn/go-auth v0.4.0-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-codec v1.4.0
	github.com/jeremyhahn/go-enclave v0.6.1-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-libsignal v0.2.0-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-objstore v0.3.0-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-qrdb v0.30.1-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-qrdb/sdk/go v0.30.1-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-quicbaas/sdk/go v0.16.0-alpha
	github.com/jeremyhahn/go-signal v0.2.0-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-xkms v0.49.0-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/jeremyhahn/go-xkms/sdk/go v0.49.0-alpha // held: #1389, the go-qrdb/go-xkms stack pin bump
	github.com/klauspost/compress v1.20.3 // held: #1597, the third-party bump to latest
	github.com/lestrrat-go/jwx/v2 v2.1.7
	github.com/mark3labs/mcp-go v0.45.0 // held: #1597, the third-party bump to latest
	github.com/minio/blake2b-simd v0.0.0-20160723061019-3f5f724cb5b1
	github.com/moby/sys/user v0.4.1
	github.com/mochi-mqtt/server/v2 v2.7.9
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/opencontainers/runc v1.5.2
	github.com/opencontainers/runtime-spec v1.3.0
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/pkg/sftp v1.13.9 // held: #1597, the third-party bump to latest
	github.com/prometheus/client_golang v1.24.1 // held: #1597, the third-party bump to latest
	github.com/prometheus/client_model v0.6.3
	github.com/quic-go/quic-go v0.63.0 // held: #1597, the third-party bump to latest
	github.com/redis/go-redis/v9 v9.18.0 // held: #1597, the third-party bump to latest
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/spf13/viper v1.21.0
	github.com/stretchr/testify v1.12.1
	github.com/veraison/corim v1.1.2
	github.com/veraison/go-cose v1.3.0
	github.com/veraison/swid v1.1.1-0.20230911094910-8ffdd07a22ca
	github.com/vmihailenco/msgpack/v5 v5.4.1
	github.com/wailsapp/wails/v2 v2.12.0 // held: #1597, the third-party bump to latest
	go.opentelemetry.io/otel v1.45.0 // held: #1597, the third-party bump to latest
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.45.0 // held: #1597, the third-party bump to latest
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.45.0 // held: #1597, the third-party bump to latest
	go.opentelemetry.io/otel/sdk v1.45.0 // held: #1597, the third-party bump to latest
	go.opentelemetry.io/otel/trace v1.45.0 // held: #1597, the third-party bump to latest
	go.podman.io/image/v5 v5.41.2 // held: v5.42.0 released 2026-10-20, under 7 days old (#1704)
	go.podman.io/storage v1.65.0
	golang.org/x/crypto v0.57.0 // held: #1597, the third-party bump to latest
	golang.org/x/mod v0.41.0 // held: #1597, the third-party bump to latest
	golang.org/x/net v0.60.0 // held: #1597, the third-party bump to latest
	golang.org/x/oauth2 v0.36.0 // held: #1597, the third-party bump to latest
	golang.org/x/sys v0.49.0 // held: #1597, the third-party bump to latest
	golang.org/x/term v0.46.0 // held: #1597, the third-party bump to latest
	google.golang.org/grpc v1.83.2 // held: #1597, the third-party bump to latest
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cel.dev/expr v0.25.2 // indirect
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.23.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.13.0 // indirect
	cloud.google.com/go/longrunning v1.2.0 // indirect
	cloud.google.com/go/monitoring v1.30.0 // indirect
	cloud.google.com/go/storage v1.64.0 // indirect
	cyphar.com/go-pathrs v0.2.6 // indirect
	filippo.io/bigmod v0.1.0 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	git.sr.ht/~jackmordaunt/go-toast/v2 v2.0.3 // indirect
	github.com/Azure/azure-pipeline-go v0.2.3 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.23.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.14.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.12.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage v1.8.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys v1.5.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets v1.5.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/internal v1.2.0 // indirect
	github.com/Azure/azure-storage-blob-go v0.15.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.8.0 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/DataDog/zstd v1.5.7 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.35.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.59.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.59.0 // indirect
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/RaduBerinde/axisds v0.1.0 // indirect
	github.com/RaduBerinde/btreemap v0.0.0-20260105202824-d3184786f603 // indirect
	github.com/VividCortex/ewma v1.2.0 // indirect
	github.com/acarl005/stripansi v0.0.0-20180116102854-5a71ef0e047d // indirect
	github.com/aperturerobotics/protobuf-go-lite v0.14.0 // indirect
	github.com/awnumar/memcall v0.5.0 // indirect
	github.com/awnumar/memguard v0.23.0 // indirect
	github.com/aws/aws-sdk-go v1.55.8 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.36 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.36 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.36 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.37 // indirect
	github.com/aws/aws-sdk-go-v2/service/glacier v1.35.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.16 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.36 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.5 // indirect
	github.com/aws/smithy-go v1.27.7 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bep/debounce v1.2.1 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/bytedance/gopkg v0.1.4 // indirect
	github.com/bytedance/sonic v1.15.2 // indirect
	github.com/bytedance/sonic/loader v0.5.2 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/checkpoint-restore/go-criu/v8 v8.3.0 // indirect
	github.com/chewxy/math32 v1.10.1 // indirect
	github.com/cilium/ebpf v0.17.3 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/cloudwego/base64x v0.1.7 // indirect
	github.com/cncf/xds/go v0.0.0-20260202195803-dba9d589def2 // indirect
	github.com/cockroachdb/apd/v3 v3.2.3 // indirect
	github.com/cockroachdb/crlib v0.0.0-20251122031428-fe658a2dbda1 // indirect
	github.com/cockroachdb/errors v1.12.0 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240816210425-c5d0cb0b6fc0 // indirect
	github.com/cockroachdb/logtags v0.0.0-20241215232642-bb51bb14a506 // indirect
	github.com/cockroachdb/redact v1.1.8 // indirect
	github.com/cockroachdb/swiss v0.0.0-20260820225851-333444432258 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20250429170803-42689b6311bb // indirect
	github.com/containerd/console v1.0.5 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.19.0 // indirect
	github.com/containers/libtrust v0.0.0-20230121012942-c1716e8a8d01 // indirect
	github.com/containers/ocicrypt v1.3.2 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.8 // indirect
	github.com/docker/go-connections v0.8.1 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dolthub/flatbuffers/v23 v23.3.3-dh.2 // indirect
	github.com/dolthub/go-icu-regex v0.0.0-20260610153742-72563bc7ca83 // indirect
	github.com/dolthub/go-mysql-server v0.20.1-0.20260812191550-bda99d66b4bf // indirect
	github.com/dolthub/jsonpath v0.0.2-0.20260807003725-336cd89c1c76 // indirect
	github.com/dolthub/vitess v0.0.0-20260728212736-0542037326d7 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.37.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.3 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/flynn/noise v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.2 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/getsentry/sentry-go v0.43.0 // indirect
	github.com/gin-contrib/sse v1.1.1 // indirect
	github.com/gin-gonic/gin v1.12.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.3 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.2.6 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/go-containerregistry v0.21.6 // indirect
	github.com/google/go-intervals v0.0.2 // indirect
	github.com/google/go-tpm-tools v0.4.10 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.21 // indirect
	github.com/googleapis/gax-go/v2 v2.23.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/gtank/ristretto255 v0.2.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.2.0 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/jeremyhahn/arduino-generic/sdk/go v0.3.0-alpha // indirect
	github.com/jeremyhahn/go-frost v0.3.0-alpha // indirect
	github.com/jeremyhahn/go-quicraft v0.15.0-alpha // indirect; hold lifted: go-qrdb v0.25.1-alpha declares the at-rest choice for in-process nodes (database.NewRaftWAL: barrier, or WithoutEncryption when seal is off), go-qrdb#566 — the real-node tests in pkg/scheduler/placement start a clustered in-process node on quicraft v0.15.0-alpha.
	github.com/jeremyhahn/go-truststrap v0.4.0-alpha // indirect
	github.com/jeremyhahn/go-xkms/xkey v0.49.0-alpha // indirect
	github.com/jeroenrinzema/psql-wire v0.18.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kevinburke/ssh_config v1.6.0 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/klauspost/pgzip v1.2.7 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/labstack/echo/v4 v4.15.3 // indirect
	github.com/labstack/gommon v0.5.0 // indirect
	github.com/leaanthony/go-ansi-parser v1.6.1 // indirect
	github.com/leaanthony/gosod v1.0.4 // indirect
	github.com/leaanthony/slicer v1.6.0 // indirect
	github.com/leaanthony/u v1.1.1 // indirect
	github.com/leodido/go-urn v1.5.0 // indirect
	github.com/lestrrat-go/blackmagic v1.0.4 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.6 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/lestrrat-go/strftime v1.2.0 // indirect
	github.com/mailru/easyjson v0.9.2 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-ieproxy v0.0.12 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/mattn/go-sqlite3 v1.14.48 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/miekg/pkcs11 v1.1.2 // indirect
	github.com/minio/minlz v1.1.0 // indirect
	github.com/mistifyio/go-zfs/v4 v4.0.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.1-0.20231216201459-8508981c8b6c // indirect
	github.com/moby/moby/client v0.5.1 // indirect
	github.com/moby/sys/capability v0.4.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/moby/sys/userns v0.2.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/mrunalp/fileutils v0.5.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/cgroups v0.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/opencontainers/selinux v1.15.1 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pingcap/errors v0.11.5-0.20211224045212-9687c2b0f87c // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20250313105119-ba97887b0a25 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/samber/lo v1.53.0 // indirect
	github.com/seccomp/libseccomp-golang v0.12.0 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.11.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sigstore/fulcio v1.8.7 // indirect
	github.com/sigstore/protobuf-specs v0.5.1 // indirect
	github.com/sigstore/sigstore v1.10.8 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	github.com/smallstep/pkcs7 v0.2.1 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spiffe/go-spiffe/v2 v2.8.1 // indirect
	github.com/stefanberger/go-pkcs11uri v0.0.0-20230803200340-78284954bff6 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tchap/go-patricia/v2 v2.3.3 // indirect
	github.com/tidwall/btree v1.8.1 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/tkrajina/go-reflector v0.5.8 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.2 // indirect
	github.com/ulikunitz/xz v0.5.17 // indirect
	github.com/urfave/cli v1.22.17 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/varlink/go v0.4.1-0.20261015173642-c935be0c53f7 // indirect
	github.com/vbatts/tar-split v0.12.3 // indirect
	github.com/vbauerster/cupwriter v0.0.4 // indirect
	github.com/vbauerster/mpb/v8 v8.14.0 // indirect
	github.com/veraison/eat v0.0.0-20210331113810-3da8a4dd42ff // indirect
	github.com/vishvananda/netlink v1.3.1 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/viterin/partial v1.1.0 // indirect
	github.com/viterin/vek v0.4.3 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/wailsapp/go-webview2 v1.0.23 // indirect
	github.com/wailsapp/mimetype v1.4.1 // indirect
	github.com/weppos/publicsuffix-go v0.50.3 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	gitlab.com/yawning/secp256k1-voi v0.0.0-20230925100816-f2616030848b // indirect
	go.etcd.io/etcd/api/v3 v3.6.8 // indirect
	go.mongodb.org/mongo-driver v1.17.9 // indirect
	go.mongodb.org/mongo-driver/v2 v2.8.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.45.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.70.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.70.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.45.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.45.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/arch v0.30.0 // indirect
	golang.org/x/exp v0.0.0-20260529124908-c761662dc8c9 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/telemetry v0.0.0-20260811182544-a038080d80e5 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/api v0.293.0 // indirect
	google.golang.org/genproto v0.0.0-20260810153831-ec0a7760b754 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260810153831-ec0a7760b754 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260810153831-ec0a7760b754 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/src-d/go-errors.v1 v1.0.0 // indirect
	software.sslmate.com/src/go-pkcs12 v0.7.3 // indirect
)

// Use local QRDB for distributed KV primitives

// Pin armon/go-metrics to v0.4.1 - newer versions declare as hashicorp/go-metrics
// but hashicorp/memberlist's compat layer still imports the armon path.
// Build with -tags hashicorpmetrics (default in Makefile) to use hashicorp/go-metrics.
// This replace prevents "go get -u" from upgrading to broken versions.
replace github.com/armon/go-metrics => github.com/armon/go-metrics v0.4.1

replace github.com/jeremyhahn/go-quicbaas/sdk/go => ./sdk/go
