# QuicBaaS Specification

> **Status: NORMATIVE**
> What QuicBaaS is and every feature it carries, with requirements and status.
> [ROADMAP.md](../ROADMAP.md) states what is complete and what remains; the two agree.
> A feature listed here, in any status, is never deleted (not its docs, code or issues); removing or re-scoping one needs the owner's change to this file.

## Purpose

QuicBaaS is the composer and the single SDK frontage for the stack: it composes go-qrdb (KV, Raft, ceremony, PIN backend) and go-xkms (hardware crypto, WebAuthn, FIDO2, PIV, CA, ACME, trust bootstrap) into one SDK and one multi-protocol API surface, plus the application layer it alone owns.
Its Go SDK is the canonical entry point for the whole stack.
It is the only way downstream applications, such as go-trusted-platform, consume the stack's crypto, KV and IAM capabilities.

## Boundaries

QuicBaaS owns:

- Composition: building go-xkms adapters and injecting them into go-qrdb through go-qrdb's `With*` options, and exposing every capability through the SDK on every transport.
- The application layer: agentic orchestration and the Software Factory, messaging, scheduler, cache, RAG, embeddings, IAM and OAuth 2.1 / OIDC, authorization overlay, audit, multi-tenancy, parameter store, object storage, observability, service registry and the attestation Verifier.

QuicBaaS never implements:

- Database, clustering and seal, the barrier (system and per-tenant), the Shamir initialization ceremony, first-boot and bootstrap, the user/identity model and SPKI cert-binding, and the RBAC decision point: go-qrdb's.
- Hardware crypto, FIDO2, WebAuthn, PIV, ACME, DANE, TPM logic, the certificate authority, trust bootstrap and attestation evidence primitives: go-xkms's.
- Provisioning, SDN and node-side attestation: go-trusted-platform's.

QuicBaaS has no firmware dependency. It carries only the generic DICE and attestation primitives usable with any device, and attestation verification goes through go-xkms.

## Features

Status is one of `shipped`, `in progress`, `planned`, `future`.

| feature | status | requirements | detail |
|---|---|---|---|
| accounts | in progress | Pooled provider accounts, the spend cap, cost enforcement and job admission<br>The spend cap is checked per turn, not only at spawn<br>A worker session rolls when its pooled account rotates | [accounts/](accounts/) |
| agent-fleet | in progress | Remote and embedded fleet nodes, mTLS enrollment, heartbeat and recovery<br>`fleet serve` and `fleet deploy`, the node sandbox and the worker image<br>Agent images published to the registry | [agent-fleet/](agent-fleet/) |
| attestation | shipped | The attestation Verifier: endorsements, signed reference values, TPM quote appraisal<br>Statement and chain verification over go-xkms evidence primitives | [attestation/](attestation/) |
| audit-logging | in progress | Record, query and sign the audit trail<br>Signature verification fails closed when signing is on<br>The signing key is held in protected memory | [audit-logging/](audit-logging/) |
| authorization | in progress | RBAC through go-qrdb's PDP, plus the ABAC attribute overlay<br>REST permissions do not derive from the first path segment<br>Authorization for the embedded transport: decision pending ([#329](../../issues/329)) | [authorization/](authorization/) |
| backup-restore | shipped | Backup to S3, Azure, GCS and MinIO<br>Restore and scheduling | [backup-restore/](backup-restore/) |
| barrier | in progress | The system barrier: init, unseal, seal, rekey, recovery, Shamir custody<br>Shamir shares, recovery keys and PINs are not held in plain strings<br>The master passphrase is not forced into argv | [barrier/](barrier/) |
| bootstrap | in progress | First-boot ceremony and trust bootstrap<br>First-boot re-entry delivers the admin credential<br>The embedded node's SPIFFE trust domain is settable through the SDK | [bootstrap/](bootstrap/) |
| cache | shipped | L1 in-memory LRU with optional L2 Redis<br>Entity-aware | [cache/](cache/) |
| certificate-authority | shipped | Certificate issuance, ACME (RFC 8555) and trust anchors | [certificate-authority/](certificate-authority/) |
| cluster | in progress | Raft consensus, membership, discovery, rebalancing and node operations<br>No hardcoded bind, advertise or upstream addresses | [cluster/](cluster/) |
| codec | shipped | Serialization: the encoding every other capability travels in<br>No API surface of its own | [codec/](codec/) |
| composition | in progress | Build go-xkms adapters and inject them into go-qrdb<br>Pin the released go-qrdb and go-xkms tags, no unreleased library main<br>The module route-collision guard does not depend on registration order | [composition/](composition/) |
| credentials | shipped | Credential storage and lifecycle: go-qrdb's versioned named-credential rotation | [credentials/](credentials/) |
| data-encryption-keys | shipped | DEK lifecycle, rotation and re-encryption<br>The safety interlock | [data-encryption-keys/](data-encryption-keys/) |
| data-services | in progress | KV, DAO, collections, transactions, batch, sharding, storage engine<br>`storage.data_dir` derives `wal_dir` | [data-services/](data-services/) |
| deployment | in progress | Kubernetes deployment and a production checklist<br>Every node of the 3-node Raft cluster runs one go-quicbaas tag; formation, smoke and failover pass | [deployment/](deployment/) |
| desktop-gui | in progress | The Wails desktop application and the embedded web UI<br>The web UI reconciles the session identity before it attaches a bearer | [desktop-gui/](desktop-gui/) |
| embeddings | shipped | Embedding providers: Ollama, OpenAI, Voyage, LM Studio | [embeddings/](embeddings/) |
| go-sdk | in progress | The canonical entry point for the whole stack, `Security()` sub-services included<br>An embedded-transport stream Close does not race a handler send<br>Admin FIDO2 / PIV factor enrollment published in the SDK<br>Platform bootstrap re-exports for go-trusted-platform: tenant context, signer store, TPM policy, secret value helpers and the ACME TPM adapter | [go-sdk/](go-sdk/) |
| iam | in progress | Identity lifecycle: users, roles, groups, tenants, sessions, MFA, WebAuthn, GDPR<br>Email is the only subject login key; invite-based self-service registration<br>`IAMService` mapped under `RequireAuthz`; TOTP secrets protected beyond derivation | [iam/](iam/) |
| identity-refactor | future | Layer the identity surface over go-qrdb's overlapping primitives<br>Authentication enforced on UI routes | [identity-refactor/](identity-refactor/) |
| kms | shipped | Key management and cryptographic operations<br>AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault and PKCS#11 | [kms/](kms/) |
| mcp-server | in progress | The Model Context Protocol surface, scope-filtered per principal<br>Unauthenticated `tools/list` does not return the full registry | [mcp-server/](mcp-server/) |
| messaging | shipped | Channels, pub/sub, delta publishing<br>Deduplication | [messaging/](messaging/) |
| multi-tenancy | in progress | Tenant lifecycle and per-tenant barriers<br>An empty caller tenant is not treated as the system tenant<br>`tenant barrier unseal-share` has a path off argv | [multi-tenancy/](multi-tenancy/) |
| multiprotocol-api | in progress | The unified API server and the transport model<br>go-qrdb's unified tenant and authentication chain on every transport<br>REST rate limiting on by default, with an opt-out | [multiprotocol-api/](multiprotocol-api/) |
| oauth2-oidc | in progress | OAuth 2.1 / OIDC provider, MCP authorization, external IdP federation, DPoP<br>The xkms FIDO2 login token's `kid` is in the published JWKS<br>`acr` / `amr` emitted as their doc comments state | [oauth2-oidc/](oauth2-oidc/) |
| object-storage | in progress | Tenanted object storage: backends, tiering, replication, catalog<br>Tenant isolation as the compliance model claims | [object-storage/](object-storage/) |
| observability | shipped | Health, liveness, readiness, metrics<br>Logging and tracing | [observability/](observability/) |
| ollama | shipped | The Ollama client: embeddings, chat, model inventory, health | [ollama/](ollama/) |
| orchestration | in progress | Agent sessions with a durable event log and attach; backends and inference<br>The job envelope and its runners, workflows, tasks, autonomy and sandboxing<br>A pending task is claimed once across nodes; no RPC goes out unauthenticated | [orchestration/](orchestration/) |
| parameter-store | shipped | Tenanted configuration parameters | [parameter-store/](parameter-store/) |
| projects | in progress | The tenant-scoped record tying a repository, git identity, workspace and fleet-node roles<br>Deleting a bound project is refused with a typed error naming `unbind`<br>Project-name arbitration yields one winner | [projects/](projects/) |
| rag | shipped | Retrieval-augmented generation: chunking, ranking, context assembly | [rag/](rag/) |
| scheduler | in progress | Distributed job scheduling, cron and delayed jobs, leader-elected execution<br>The workload placement engine<br>Guarded (CAS) writes for abandoned-run recovery | [scheduler/](scheduler/) |
| sdlc | in progress | A labelled forge issue becomes a branch, a pull request, a review and a merge<br>Project bindings, onboarding, review, triage, merge train, CI-failure classification<br>SDLC surface parity across transports | [sdlc/](sdlc/) |
| secrets | in progress | The secrets engine, dynamic secrets, secret-reference resolution<br>Rotation policies are performed, not only stored<br>Storage-level write arbitration across nodes | [secrets/](secrets/) |
| security-hardening | in progress | FIPS mode, TLS posture, ingress binding, the hardening baseline<br>Server certificate SANs honoured and not duplicated<br>The scoped FIPS gate runs on push | [security-hardening/](security-hardening/) |
| service-registry | shipped | Tenant-private service discovery: lease-backed registration, resolve, ordered watch<br>Served on every transport | [service-registry/](service-registry/) |
| signal-bridge | shipped | The go-signal bridge (build-tag gated) | [signal-bridge/](signal-bridge/) |
| software-factory | in progress | The Software Factory built on the engine: projects, accounts and the SDLC module<br>Triage, task workers, review and merge run on the QuicBaaS deployment<br>A standing MCP principal for every company agent | [software-factory/](software-factory/) |
| ssh | shipped | Server-held SSH keys, known hosts, authorized keys<br>Crypto through go-xkms | [ssh/](ssh/) |
| timeseries | shipped | Time-series write and query, retention | [timeseries/](timeseries/) |
| vector | shipped | Vector namespaces, search, indexing | [vector/](vector/) |
| websocket | future | The WebSocket server with MQTT, AMQP and STOMP subprotocols<br>Compiles and is unit-tested; not mounted on any listener | [websocket/](websocket/) |
| release-readiness | in progress | The published tree carries no private host, path or lab value<br>Every `docs/` folder meets the documentation rules<br>CI workflows do not mount the host Docker socket | [release/](release/) |
| declarative-placement | planned | Declarative workload placement | [scheduler/](scheduler/) |
| orchestration-volumes | planned | go-blockstore volumes in container orchestration | none |
| tenant-agent | future | A QuicBaaS tenant agent replaces the Company Agent | none |

## Interfaces

- Go SDK: [`sdk/go`](../sdk/go/), with the `Security()` accessors in [services.go](../sdk/go/services.go); see [go-sdk/](go-sdk/).
- TypeScript SDK: [`sdk/typescript`](../sdk/typescript/).
- CLIs: [`quicbaas`](../cmd/quicbaas/) and [`quicbaasd`](../cmd/quicbaasd/).
- REST: [multiprotocol-api/](multiprotocol-api/), route tables in [`pkg/api/rest`](../pkg/api/rest/).
- gRPC: [multiprotocol-api/](multiprotocol-api/), protos in [`api/proto`](../api/proto/).
- QUIC: [`sdk/go/transport/quic`](../sdk/go/transport/quic/), the REST dialect over HTTP/3.
- MCP: [mcp-server/](mcp-server/).
- Unix: [`sdk/go/transport/unix`](../sdk/go/transport/unix/), gRPC over `AF_UNIX`.
- Embedded: [`sdk/go/transport/embedded`](../sdk/go/transport/embedded/).

## Release criteria

Milestone 15, Release candidate, closes when every open issue below is resolved.

- Security fails closed: audit signatures and the audit signing key ([#22](../../../issues/22), [#23](../../../issues/23)); object-storage and dynamic-secrets tenant isolation ([#32](../../../issues/32)); TOTP secrets protected beyond derivation ([#402](../../../issues/402)); webhook fixture HMAC keys ([#524](../../../issues/524)).
- Authorization and tenancy: REST permissions ([#338](../../../issues/338)), the empty caller tenant ([#348](../../../issues/348)), embedded-transport authorization ([#329](../../../issues/329)), `IAMService` under `RequireAuthz` ([#301](../../../issues/301)), orchestration tokens and `DeleteTask` ([#502](../../../issues/502), [#637](../../../issues/637)), the module route-collision guard ([#477](../../../issues/477)).
- Identity and login: tenant onboarding ([#358](../../../issues/358)), email-only login ([#377](../../../issues/377)), `acr` / `amr` ([#392](../../../issues/392)), hardware attestation and `mfa_verified` ([#416](../../../issues/416), [#422](../../../issues/422)), the FIDO2 login token ([#486](../../../issues/486)), PIV attestation trust material ([#800](../../../issues/800)).
- Durable state: the session log ([#200](../../../issues/200), [#201](../../../issues/201)) and secret rotation ([#723](../../../issues/723)).
- MCP: unauthenticated `tools/list` and `quicbaas_readiness` ([#1127](../../../issues/1127)).
- Gates: the `pkg/iam` race pass ([#718](../../../issues/718)), the scoped FIPS gate on push ([#769](../../../issues/769)), the host Docker socket in CI ([#1168](../../../issues/1168)).
- Release: public release readiness ([#1130](../../../issues/1130)) and the docs review ([#1258](../../../issues/1258)).

The software-factory feature belongs to milestone 33, not to the release candidate.
