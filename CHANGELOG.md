# Changelog

All notable changes to QuicBaaS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.7.0-alpha] - 2026-09-06

MINOR rather than PATCH because the span since `0.6.0-alpha` carries breaking
dependency moves, and pre-1.0 that is where breakage lives
(release-versioning.md §1). go-qrdb and its SDK cross seven minors
(`0.9.0-alpha` → `0.16.0-alpha`), go-xkms, its SDK and xkey cross two
(`0.7.7-alpha` → `0.9.4-alpha`), go-quicraft one (`0.8.0-alpha` →
`0.9.0-alpha`) and go-truststrap two (`0.2.0-alpha` → `0.4.0-alpha`). Those
moves reach consumers through the type-alias re-exports in `sdk/go`, and
go-xkms `v0.9.2-alpha` raises the required Go toolchain to 1.26.7.

### Added

- **`sdk/go/xkms/ca` re-exports the TCG certificate-policy OIDs** as aliases of
  the go-xkms values: `OIDTCGKpAIKCertificate`,
  `OIDTCGVerifiedTPM{Residency,Fixed,Restricted}` and the `x509.OID` forms
  `TCGVerifiedTPM{Residency,Fixed,Restricted}Policy`. A relying party must
  check `tcg-cap-verifiedTPMRestricted` on an IAK/LAK certificate before
  trusting an attestation, and the platform may reach the arc only through
  this SDK. (#533)

- **`sdk/go/xkms/ca` re-exports the IAK certificate surface** from go-xkms#371:
  `CAIAKCertificateReader` (`IAKCertificate(ctx, model, serial)`) and
  `CAIAKCommonName` (`"iak-<model>-<serial>"`), the name the CA now issues and
  stores the Initial Attestation Key under so the device's IDevID record no
  longer overwrites it. (#533)

- **`sdk/go/xkms` re-exports the attested-peer verification primitive** from
  go-xkms#273: `PeerAttestationVerifier` / `NewPeerAttestationVerifier` /
  `WithPeerVerifierClock`, `PeerEvidence`, `PeerPolicy`, `PeerVerdict`,
  `PeerChecks`, `PeerClockInfo`, `PeerFailureClass` with
  `PeerFailure{None,Absence,Cryptographic}`, the typed
  `Peer{Policy,Context,ChannelBinding}Error`s, and `PeerChannelBinding` /
  `PeerQualifyingData` with `PeerChannelBindingExporterLabel` /
  `PeerChannelBindingLength`. Pure aliases; the platform's remote-attestation
  path is the consumer. Not aliased: `peer.FailureClassError`, which go-xkms's
  own SDK does not yet export (go-xkms#391); it follows in a later alpha. (#533)

### Changed

- First-party dependencies moved to the current tags in both modules: go-xkms,
  its SDK and xkey to `v0.9.2-alpha` (carries the IDevID signing fix,
  go-xkms#274/#276). Minimal version selection carries go-qrdb and its SDK to
  `v0.16.0-alpha` and go-quicraft to `v0.9.0-alpha`. go-xkms `v0.9.2-alpha`
  requires Go 1.26.7, so the `go` directive, `GO_VERSION` and every
  `golang:1.26.x-alpine` build image move with it. (#536)

- go-xkms, its SDK and xkey moved on to `v0.9.4-alpha` in both modules — the
  tag that carries the `Restricted` OID (go-xkms#359), the IAK certstore fix
  (go-xkms#383) and the attested-peer primitive (go-xkms#385). Minimal version
  selection carries go-truststrap to `v0.4.0-alpha` with it. (#533)

- **`GitProvisioner` bounds the SSH agent teardown after a clone.** go-xkms's
  `ConnectSession.SyncAndStop` now takes a context and reports any signing
  operation still running when it elapsed; the clone teardown gives it a
  5-second bound of its own and logs abandoned operations instead of hanging
  on a hardware key awaiting touch. (#536)

- **A seal-disabled node's IAM records now live under the system tenant's
  confined keyspace.** go-qrdb v0.10.0 (#41) made every node expose a
  `StoreResolver`, so IAM writes route through `"{tenant}/"` whether or not
  the seal subsystem is on; the new `Server.IAMKVStore()` is the accessor
  for reading them from outside the IAM components. Pre-release policy: wipe
  and re-bootstrap dev deployments. (#536)

- **A known-hosts pin must carry a well-formed fingerprint.** go-xkms v0.9.x
  validates `SHA256:<unpadded base64 of 32 bytes>` at the store, so the REST
  and CLI paths now reject a malformed fingerprint with a 400 / typed error
  instead of pinning something no host key can ever match. (#536)

- **The broker's email reaches qrdb as the canonical login key.** go-qrdb
  v0.15.0 (#191) refuses a user with no email login key unless the caller
  declares `WithoutEmailLoginKey`; `QRDBUserStorage.CreateUser` now forwards
  the email (and the display name) and declares a certificate-only identity
  when there is none, and a duplicate email maps to `iam.ErrEmailAlreadyExists`.
  First boot was failing at admin-user creation without this. (#536)

- **`RoleAdapter` resolves built-in roles by name for first-boot re-runs.**
  go-qrdb's firstboot refuses to derive a system role's identifier when the
  roles already exist and nothing can resolve them (`UnresolvableSystemRoleError`);
  the adapter now implements `RoleNameResolver` and is wired as
  `FirstBootDependencies.RoleResolver`, so the retry after a partial first boot
  binds the admin grant to the persisted role ID. (#536)

- **Self-service registration is forwarded to both WebAuthn relying parties.**
  go-xkms v0.9.2-alpha reads an unset `AllowPublicRegistration` as closed,
  which would have silently switched off `iam.self_service_registration.enabled`
  on every node; the operator's decision now reaches quicbaas's own per-tenant
  relying party and the composed go-xkms module's, and a test pins that the
  two agree (port of the feature/fabric fix in #516). (#536)

## [0.4.0-alpha] - 2026-08-20

MINOR rather than PATCH because the span since `0.3.0-alpha` carries breaking
change, and pre-1.0 that is where breakage lives (release-versioning.md §1).
Two things break in it: service-account client secrets are now derived with
PBKDF2-HMAC-SHA256, so secrets issued before this release no longer verify;
and the dependency bump below moves go-qrdb across two minors, go-quicraft
across two, and go-signal and go-libsignal across one each, which reaches
consumers through the type-alias re-exports in `sdk/go`.

### Changed

- First-party dependencies moved to the current tags in both modules: go-qrdb
  and its SDK to `v0.9.0-alpha`, go-xkms, its SDK and xkey to `v0.7.7-alpha`,
  go-signal to `v0.2.0-alpha`, go-libsignal to `v0.2.0-alpha`, and go-objstore
  to `v0.3.0-alpha`. Minimal version selection carries go-quicraft to
  `v0.8.0-alpha` and go-truststrap to `v0.2.0-alpha`.

- **`signalbridge` reads the Kyber pre-key's public half through
  `SerializedPublicKey`.** go-libsignal `v0.2.0-alpha` moved the KEM secret
  key into guarded memory and withdrew the `KyberPreKey.KeyPair()` accessor
  that handed out the whole pair; the public half is now reached without
  touching the secret. The bytes published in the pre-key bundle are
  unchanged.

### Security

- **Service-account client secrets are derived with PBKDF2-HMAC-SHA256.**
  Secrets issued before this release no longer verify; re-issue them. Local
  deployments are wiped and re-bootstrapped, which is the pre-release upgrade
  path (release-versioning.md §1).

## [0.3.0-alpha] - 2026-08-19

MINOR rather than PATCH because the route de-collision below is breaking, and
pre-1.0 that is where breakage lives (release-versioning.md §1).

### Fixed

- **`StdioTransport.Send` no longer discards a response that races the
  transport close.** `Send` selected on the response channel and the
  transport's `done` channel together. `readLoop` delivers a response before
  it observes EOF and closes `done`, so an agent that answers and then
  immediately exits left both cases ready — and `select` chooses uniformly
  among ready cases. Roughly half the time the delivered response was thrown
  away and the call returned `ErrSessionClosed`. This affected any short-lived
  or one-shot ACP agent, not only tests; it surfaced as an intermittent
  `TestACPBackend_JSONRPC_Spawn_InitializeError` failure.

### Security

- **The form-encoded OAuth2 endpoints now bound their request bodies.**
  `/oauth2/token`, `/oauth2/token-mtls`, `/oauth2/revoke` and
  `/oauth2/introspect` are `RouteAuthPublic` and are registered outside the
  `BodyLimitMiddleware` group, so `ParseForm` would buffer an unbounded body
  from an unauthenticated caller. All four are capped at 1 MB via the new
  exported `MaxOAuth2FormBytes`.

### Changed

- First-party dependencies moved to the current tags: go-qrdb and its SDK to
  `v0.6.0-alpha`, go-xkms, its SDK and xkey to `v0.7.5-alpha`, go-quicraft to
  `v0.6.2-alpha`, go-truststrap to `v0.1.4-alpha`, go-frost to `v0.1.8-alpha`,
  go-luks2 to `v0.1.7-alpha`.

### Build

- `make vuln` distinguishes a govulncheck killed by a signal (exit >128, in
  practice the job container's memory cap) from a real vulnerability finding,
  instead of reporting both as "found reachable vulnerabilities". Still
  fail-closed in both cases.

### Changed — BREAKING: unified-node route de-collision (P3.3)

- **`/api/v1/auth/{refresh,logout,validate,login/piv}` dropped from the
  module surface.** On a composed node (the qrdb-embedded node's own
  `RegisterHTTP`, `pkg/apimodule/module_http.go`) go-xkms's ServiceModule
  owns the identical `/api/v1/auth/*` session-login plane; quicbaas's own
  registrations at those four paths would route-collide. All four are the SAME token/session family — they resolve
  through the same `authService.sessionAuthority` -> `iam.OIDCProvider`
  (`pkg/service/quicbaas.go`) that PIV login mints from — so they were
  removed together. The legacy in-process listener (standalone, non-composed
  deployments) is unaffected for `refresh`/`logout`/`validate`: they remain
  reachable at their original paths there.
- **`POST /api/v1/auth/login/piv` moved to `POST /api/v1/iam/login/piv`.**
  quicbaas's OIDC-session PIV login capability is kept (it is functionally
  distinct from the qrdb `LoginCeremony`-backed
  `/api/v1/auth/login/{begin,finish}`, which is unaffected), but renamed
  under the IAM namespace to clear the path collision with xkms's own PIV
  login endpoint, following the same tenant-namespaced-mount convention as
  `/api/v1/iam/{tenant_id}/webauthn/*`. Both the module surface and the
  legacy listener serve the new path only — the old path is gone on both.
  SDK callers (TypeScript `rest.ts` `loginPIV()`, integration tests) were
  repointed.
- **`/api/v1/barrier/*` and `/api/v1/init/*` excluded from the module
  surface** (`pkg/api/rest.ModuleRoutes()`); the legacy listener keeps
  serving them for the standalone sealed-window boot path.

### Changed — BREAKING: ceremony claim-share is now a Begin/Complete handshake

- **Single-shot `CeremonyClaimShare` deleted** (tracks go-qrdb's regenerated
  qrdbv1 protos + SDK). The officer Shamir-share claim is now a two-step
  challenge-response handshake, mirroring qrdb's officer-share wire contract
  verbatim on every transport:
  - **SDK** (`Security().Ceremony()`, the re-exported qrdb `CeremonyClient`):
    `CeremonyClaimShare(*ClaimShareRequest)` →
    `CeremonyBeginClaimShare(*BeginClaimShareRequest)` (returns
    `nonce_hex` + `expires_at`) then
    `CeremonyCompleteClaimShare(*CompleteClaimShareRequest{Username,
    NonceHex, Signature})` (returns `share_index` + `share_json`, released
    exactly once). The caller signs the nonce with the private key of their
    enrolled officer certificate; the server derives the caller thumbprint
    from the mTLS peer chain — it never travels on the wire.
  - **REST**: `POST /api/v1/init/claim-share` and the qrdb wire mirror
    `POST /v1/ceremony/claim-share` are replaced by
    `/api/v1/init/officer-share/{begin,complete}` and
    `/v1/ceremony/officer-share/{begin,complete}`.
  - **gRPC** (`quicbaas.ceremony.v1.Ceremony`): `CeremonyClaimShare` RPC
    replaced by `CeremonyBeginClaimShare` / `CeremonyCompleteClaimShare`.
  - **Native QUIC**: `SubOpCeremonyClaimShare` (0x05) is now
    `SubOpCeremonyBeginClaimShare` (0x05) plus
    `SubOpCeremonyCompleteClaimShare` (0x09).
  - **qrdb-QUIC bridge**: `ceremony.ClaimShare` replaced by
    `ceremony.BeginClaimShare` / `ceremony.CompleteClaimShare`.
  - **MCP**: tool `qrdb_ceremony_claim_share` replaced by
    `qrdb_ceremony_begin_claim_share` / `qrdb_ceremony_complete_claim_share`
    (signature and released share travel base64-encoded).
  - **CLI**: `quicbaas init claim-share` keeps its flags but now drives the
    full handshake internally, signing the challenge nonce with `--tls-key`.
  - Service facade (`service.CeremonyServicer`), sealed-window allowlists
    (new `ceremony-officer-share-begin/complete` verbs mirroring qrdb's
    canonical 34-verb table), and gRPC PEP mappings
    (`ceremony:claim_share_begin` / `ceremony:claim_share_complete`)
    migrated accordingly. There is no compatibility shim — the stack is
    pre-v1 with exactly one wire format.

### Added

- **Barrier enable-encryption (live on all transports).** System (`POST /api/v1/barrier/enable-encryption`) and per-tenant (`POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption`) routes enable migration from cleartext (`none`) to encrypted barriers across REST, gRPC, QUIC, MCP, Unix, and embedded transports. Driven by the embedded qrdb node (wired as `EncryptionMigrator`), not the qrdb `BarrierClient` transport, providing direct cluster coordination and fail-safe recovery. Supports passphrase, Shamir, TPM2, PKCS#11, AWS KMS, GCP KMS, Azure Key Vault, and Vault strategies. Request fields: `strategy`, `passphrase`, `shamir_threshold`, `shamir_total`, `mechanism`, `availability`.
- **Tenant quota and entitlement governance.** All qrdb `Quotas` fields (MaxDatabases, MaxRaftGroups, MaxReplicasPerGroup, MaxTotalReplicas, MaxCollections, MaxKeys, MaxStorageBytes, MaxDatabaseSizeBytes, MaxUsers, MaxRoles, MaxCertificates, MaxBackups, MaxVectorDims, MaxTSMetrics, MaxDedicatedWALGroups, SystemBarrierQuota) and `Entitlements` fields (AllowedIsolationModes, AllowedBarrierModes, AllowDedicatedRaft, AllowVolumeStorage, AllowSystemManagedBarrier, AllowDedicatedWAL) are settable at tenant create-time via the provisioner and via dedicated update endpoints on REST/gRPC. Quota enforcement blocks operations that exceed limits; entitlement checks reject feature use (e.g., enable-encryption to a disallowed barrier mode) with 403.
- **Expanded tenant isolation modes.** Quicbaas config now allows `keyspace`, `barrier`, `volume`, and `raft` isolation modes (matching qrdb's full set), enabling per-tenant enable-encryption. Raft mode requires clustered quicbaas deployment.
- **Entitlement-denied error mapping.** `ErrSharedBarrierImmutable` / `EntitlementError` → 403 Forbidden; `ErrMigrationInProgress` / `ErrMigrationSourceNotNone` → 409 Conflict; invalid mechanism/availability → 400 Bad Request; `ErrMigrationProviderUnavailable` → 501 Not Implemented. Consistent across all transports.
- **Barrier in embedded mode.** Full barrier surface (status, init, unseal, seal, rekey, recovery, enable-encryption) now wired in embedded mode via the embedded qrdb node, eliminating the prior "NilServiceError" limitation. CLI commands like `quicbaas barrier status --transport embedded` and the init ceremony operate without a network listener.

### Changed — `seal` → `barrier` rename (cross-project)

- **Encryption-at-rest unified with go-qrdb / go-xkms.** The barrier
  subsystem now wraps every Pebble-backed data-plane store via the
  encrypting `vfs.FS` from `go-qrdb/pkg/seal/encfs`; sealed-window
  middleware enforces a canonical allowlist of `/v1/health` +
  `/v1/barrier/*` + `/v1/init/*` + `/v1/bootstrap/*` while the barrier
  is sealed. See `docs/architecture/barrier.md`.
- **REST surface renamed** `/api/v1/seal/*` → `/api/v1/barrier/*` (16
  endpoints) plus 7 new `/api/v1/init/*` and 4 new `/api/v1/bootstrap/*`
  endpoints. Legacy `/seal/*` paths retained for one release with
  `Deprecation:` and `Sunset:` headers. Migration table at
  `docs/migrations/seal-to-barrier.md`.
- **CLI surface split**: `quicbaas seal …` → `quicbaas barrier …` /
  `quicbaas init …` / `quicbaas bootstrap …`, mirroring `xkmsctl`
  verb-for-verb. `quicbaas seal …` retained for one release as a
  deprecation alias that warns to stderr.
- **TypeScript SDK** ships `BarrierClient`, `CeremonyClient`,
  `BootstrapClient` interfaces with REST + stub grpc-web/quic/mcp
  transports. `SealClient` retained as a deprecation alias that
  `console.warn`s on construction.
- **`defaultSealManager` deleted** in favour of the SDK-backed
  `pkg/barrier/manager.go` adapter. `pkg/interfaces/managers.go`
  exposes both `SealManager` (alias) and `BarrierManager` for one
  release.
- **`cmd/quicbaasd/init.go`** added — daemon-side init command mirroring
  `xkmsd init` (`--barrier-strategy`, `--shares`, `--threshold`,
  `--so name:KIND:path`, `--trusted-ca`).
- **Frontend** `BarrierView.svelte`, `CeremonyView.svelte`, and
  `BootstrapView.svelte` replace `SealView.svelte`. Sidebar label
  "Seal" → "Barrier" with "Ceremony"/"Bootstrap" sub-items under
  SECURITY.
- **`make verify-naming`, `make verify-cli-flags`, `make verify-api-shapes`**
  added — CI watchdogs that catch operator/SDK drift across QuicBaaS,
  go-qrdb, and go-xkms.

### Breaking

- **No in-place upgrade from pre-encfs data directories.** A new
  format-version sentinel at `<data-dir>/seal/__sys/barrier_format_version`
  is checked at startup; daemons refuse to start on a non-empty data
  dir without the sentinel. Operators must export and re-import. See
  `docs/migrations/seal-to-barrier.md`.

### Changed

- **Agent identity is now mTLS-based.** The one-shot enrollment-token
  bootstrap was removed. `quicbaas agent deploy` issues an X.509 client
  certificate for each deployed daemon via the server-embedded CA and
  uploads it to `$DATA_DIR/agent.crt`, `$DATA_DIR/agent.key`, and
  `$DATA_DIR/server-ca.crt`. The daemon presents the cert on the TLS
  handshake when calling `POST /api/v1/agents/register`; the server
  returns OAuth2 client credentials the daemon uses for heartbeat and
  event streams thereafter.
- **Renamed `--central` → `--server` everywhere.** The central
  orchestrator terminology on the agent wire protocol, CLI, GUI, and
  SDK DTOs is now uniformly "server". `agent.Config.CentralAddr` is
  now `agent.Config.ServerAddr`, `deploy.Spec.Central` is now
  `deploy.Spec.Server`, and the Svelte deploy dialog input is now
  labelled "Server URL".
- **`agent serve` requires `--agent-cert`, `--agent-key`, `--server-ca`.**
  The `--enrollment-token` and `--device-cert` flags have been removed.

### Removed

- `iam role grant agent-runtime` CLI command.
- `iam enrollment-token issue` CLI command.
- `POST /api/v1/agents/enroll` REST endpoint.
- `POST /api/v1/agents/enrollment-tokens` REST endpoint.
- `iam.AgentEnrollmentService` and `iam.MemoryEnrollmentTokenStore`.
- `sdk/go/client/iam.IssueEnrollmentToken` and `IssueEnrollmentTokenRequest/Response`.
- The `enrollment_token` field on the `EnrollRequest` proto message.

## [1.0.0] - 2026-02-03

### First Public Release

QuicBaaS is a high-performance, multi-faceted, distributed key-value database built on multi-Raft consensus and PebbleDB storage. Designed for speed, strong consistency, horizontal scalability, security, and operational simplicity.

### Core Features

#### Storage & Data
- **Multi-Raft Consensus** - Distributed consensus using QuicRaft with configurable write modes (sync, WAL, batched)
- **PebbleDB Storage Engine** - High-performance LSM-tree storage with MVCC support
- **Key-Value Operations** - Full CRUD with transactions, batch operations, and prefix queries
- **Vector Database** - HNSW and IVF indexing for similarity search
- **Time-Series Database** - Metrics storage with retention policies and downsampling
- **Object Storage** - Multi-backend support (S3, GCS, Azure, MinIO) with tiering

#### Security
- **IAM/ABAC** - Identity and access management with attribute-based access control
- **KMS Integration** - Support for AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault, PKCS#11
- **Secrets Engine** - Versioned secrets with rotation and watch API
- **Encryption** - At-rest and in-transit encryption with FIPS 140-3 compliance option
- **Seal/Unseal** - Vault-style initialization with Shamir secret sharing
- **mTLS** - Mutual TLS for all inter-node communication

#### APIs & Protocols
- **gRPC** - High-performance RPC with streaming support
- **REST/HTTP** - RESTful API with SSE for real-time updates
- **QUIC** - Low-latency UDP-based transport
- **WebSocket** - Real-time communication with MQTT, AMQP, STOMP subprotocols
- **MCP** - Model Context Protocol for AI/LLM integration (270+ tools)
- **Unix Socket** - Local IPC for embedded use cases

#### AI & Machine Learning
- **RAG Pipeline** - Retrieval-Augmented Generation with chunking, embedding, and ranking
- **Embedding Providers** - Support for OpenAI, Ollama, and custom providers
- **Vector Search** - Semantic similarity search with configurable distance metrics

#### Messaging & Real-Time
- **Push Notifications** - Channel-based pub/sub with delta publishing
- **Watch System** - Real-time change notifications with delta computation
- **Message Deduplication** - Content-based and ID-based deduplication

#### Operations
- **Distributed Scheduler** - Cron-based job scheduling across the cluster
- **Backup & Recovery** - Point-in-time recovery with multiple storage backends
- **Cache Layer** - L1 in-memory cache with optional L2 Redis
- **Cluster Management** - Dynamic node addition/removal, rebalancing

#### SDKs
- **Go SDK** - Full-featured client with all transports
- **JavaScript SDK** - Browser and Node.js support with 100% test coverage

#### CLI
- **308 Commands** - Comprehensive CLI across 24 categories
- **Multiple Transports** - gRPC, REST, QUIC, Unix socket, embedded

### Build Profiles
- **Minimal** - Core KV + gRPC only
- **Embedded** - Single-node, no clustering
- **Standard** - Cluster mode, no AI features
- **Cluster** - All features, no cloud KMS
- **Enterprise** - All features + cloud KMS
- **Full** - Everything including HSM and quantum-safe crypto

### Documentation
- Comprehensive documentation in `/docs/`
- API references for all protocols
- Production deployment checklist
- Security hardening guides

### Testing
- 88%+ unit test coverage
- Integration test suite with Docker
- Benchmark suite for performance validation

---

## [0.1.0-alpha] - 2025-01-01

### Initial Development Release
- Core key-value storage
- Basic Raft consensus
- gRPC API
- Foundation for future development
