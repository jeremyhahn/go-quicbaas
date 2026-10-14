# go-quicbaas roadmap

> **Status: REFERENCE** · Last verified: 2026-10-12
> Per handbook `software/conventions/roadmap.md`: the Software Department refreshes it; the project agent proposes changes by PR.
> This is the only roadmap.

## Mission

The only composer: one SDK over the whole stack, plus the application layer it alone owns — agentic orchestration, messaging, RAG, scheduler and placement, service registry, attestation Verifier, IAM/OIDC, tenancy (handbook `docs/00` L1).

Every feature, its requirements and its status are in [docs/SPEC.md](docs/SPEC.md); this roadmap and that table agree. Each line below starts with the SPEC feature it belongs to. What has shipped is in [CHANGELOG.md](CHANGELOG.md).

## Milestones

The current milestone first, then every other open milestone in delivery order.

- [Release candidate](../../milestone/15) — target: release candidate — 28 open, 46 closed — due 2026-11-16
- [Deployed on raft-1/2/3](../../milestone/32) — target: every node runs one go-quicbaas tag, with formation, smoke and failover evidence — 17 open, 5 closed — due 2026-10-30
- [Local Software Factory on QuicBaaS](../../milestone/33) — target: the local software factory running on QuicBaaS — 14 open, 4 closed — due 2026-11-06

## Complete

Shipped features, each with the release that carries it ([CHANGELOG.md](CHANGELOG.md)).

- **attestation** — the RFC 9334 attestation Verifier — release 0.14.0-alpha
- **backup-restore** — backup and point-in-time recovery to multiple backends — release 1.0.0
- **cache** — L1 in-memory cache with optional L2 Redis — release 1.0.0
- **certificate-authority** — certificate issuance and ACME through go-xkms — release 0.8.0-alpha
- **codec** — serialization — [docs/codec/](docs/codec/)
- **credentials** — versioned named-credential rotation — release 0.8.0-alpha
- **data-encryption-keys** — DEK lifecycle, rotation and re-encryption — release 0.12.0-alpha
- **embeddings** — embedding providers — release 1.0.0
- **kms** — key management through go-xkms backends — release 1.0.0
- **messaging** — channel pub/sub, delta publishing and deduplication — release 1.0.0
- **observability** — health, readiness, metrics, logging and tracing — release 0.8.0-alpha
- **ollama** — the Ollama client — release 0.13.0-alpha; the Ollama embedding provider — release 1.0.0
- **parameter-store** — tenanted configuration parameters — [docs/parameter-store/](docs/parameter-store/)
- **rag** — the RAG pipeline — release 1.0.0
- **service-registry** — lease-based service registry on every transport ([#1143](../../issues/1143)) — release 0.14.0-alpha
- **signal-bridge** — the go-signal bridge — release 0.4.0-alpha
- **ssh** — server-held SSH keys, known hosts and authorized keys — release 0.8.0-alpha
- **timeseries** — time-series write, query and retention — release 1.0.0
- **vector** — vector namespaces, search and indexing — release 1.0.0

Closed issues on features still in progress:

- **agent-fleet** — [#680](../../issues/680), [#700](../../issues/700) — fleet listings are tenant-scoped and rendered by one served-backends renderer
- **barrier** — [#180](../../issues/180) — the barrier passphrase is read from stdin or `QUICBAAS_BARRIER_PASSPHRASE`, never argv
- **desktop-gui** — [#369](../../issues/369) — the web UI reconciles the session identity before it attaches a bearer
- **go-sdk** — [#144](../../issues/144), [#560](../../issues/560) — an embedded-transport stream Close no longer races a handler send; `sdk/go/xkms` re-exports the ACME http-01 responder and the SSH fingerprint validator
- **iam** — [#259](../../issues/259), [#335](../../issues/335), [#376](../../issues/376) — SDK session verbs on `/api/v1/iam` with a per-request bearer; invalid IAM fields are refused as invalid arguments on every transport; the SDK resolves a user from a client certificate under `Security().Auth()`
- **multiprotocol-api** — [#158](../../issues/158) — REST rate limiting on by default, with an opt-out
- **projects** — [#826](../../issues/826) — project-name arbitration yields one winner
- **sdlc** — [#189](../../issues/189) — the spent trigger label is removed when a merged item's issue closes

## Now

Each line: SPEC feature, issue, outcome, status.

- **go-sdk** — M32: [#1171](../../issues/1171) — platform bootstrap re-exports for go-trusted-platform: go-xkms v0.52.0-alpha and go-qrdb v0.47.0-alpha adopted; tenant context, signer store, TPM policy, secret value helpers and the ACME TPM adapter published in `sdk/go`; released as v0.15.0-alpha — in progress
- **attestation** — M32: [#1185](../../issues/1185) — no firmware module in the dependency graph: the go-xkms release without a firmware dependency is adopted and the arduino-generic indirect require is dropped — blocked: see issue
- **iam** — [#358](../../issues/358) — self-service registration through invite activation on every transport — in review

## Next

One line per SPEC feature. RC is the [Release candidate](../../milestone/15), M32 is [Deployed on raft-1/2/3](../../milestone/32), M33 is [Local Software Factory on QuicBaaS](../../milestone/33), which is not in the release candidate.

- **accounts** — M33: [#997](../../issues/997), [#782](../../issues/782) — sessions roll with account rotation; the spend cap is checked per turn
- **agent-fleet** — M32: [#756](../../issues/756) — agent images published to the registry
- **audit-logging** — RC: [#22](../../issues/22), [#23](../../issues/23) — audit signatures fail closed and the signing key is protected — decision pending
- **authorization** — RC: [#338](../../issues/338), [#329](../../issues/329) — permissions derive from the operation; open decision: is in-process access through the embedded transport trusted by design, or gated — decision pending
- **barrier** — M32: [#237](../../issues/237) — passphrases, shares, recovery keys and PINs off argv and out of plain strings
- **bootstrap** — M32: [#125](../../issues/125), [#64](../../issues/64) — SPIFFE trust domain settable through the SDK; first-boot re-entry delivers the admin credential
- **cluster** — M32: [#185](../../issues/185) — no hardcoded bind, advertise or upstream addresses
- **composition** — RC: [#477](../../issues/477) (decision pending); M32: [#1141](../../issues/1141) — module route registration is order-independent; pin the released go-qrdb and go-xkms tags
- **data-services** — M32: [#176](../../issues/176) — `storage.data_dir` derives `wal_dir`
- **deployment** — M32: [milestone 32](../../milestone/32) — every node runs one go-quicbaas tag; formation, smoke and failover pass with a re-runnable suite
- **desktop-gui** — [#851](../../issues/851), [#806](../../issues/806) — the "N online" count ignores agents that never started and honours `last_seen`; the Prompts page in the web UI
- **go-sdk** — M32: [#378](../../issues/378) — admin FIDO2 / PIV factor enrollment published in the SDK
- **iam** — RC: [#301](../../issues/301), [#377](../../issues/377), [#402](../../issues/402), [#416](../../issues/416), [#422](../../issues/422), [#800](../../issues/800) (blocked: see issue), [#718](../../issues/718) (blocked: see issue); M32: [#482](../../issues/482) — `IAMService` mapped under authz, email-only login, TOTP secrets protected; open decisions: does hardware attestation alone satisfy `mfa_verified`, and does a PIV slot-attestation certificate reach the server in the TLS handshake or out of band; PIV attestation trust settings reach the go-xkms module route; REST listener and IAM race tests stable under load; self-registration succeeds and leaves no orphan tenant
- **mcp-server** — RC: [#1127](../../issues/1127) — the public MCP tool set matches its specification and is guarded — blocked: see issue
- **multi-tenancy** — RC: [#348](../../issues/348) (decision pending); M32: [#488](../../issues/488) — an empty caller tenant is never the system tenant; `tenant barrier unseal-share` takes its share off argv
- **multiprotocol-api** — M32: [#1150](../../issues/1150) — go-qrdb's tenant and auth chain inherited on every transport
- **oauth2-oidc** — RC: [#392](../../issues/392) (decision pending), [#486](../../issues/486) (blocked: see issue) — `acr` / `amr` are emitted as documented; an xkms-issued FIDO2 login token verifies against the published JWKS
- **object-storage** — RC: [#32](../../issues/32) — object storage and dynamic secrets are tenant-isolated as the compliance model claims
- **orchestration** — RC: [#502](../../issues/502), [#637](../../issues/637), [#200](../../issues/200), [#201](../../issues/201) (decision pending); M32: [#719](../../issues/719) — orchestration RPCs are always authenticated and gated by the role matrix; the session log neither overwrites events nor strands a conclusion; a pending task is claimed once across nodes
- **projects** — M33: [#891](../../issues/891) — deleting a bound project is refused with a typed error naming `unbind`
- **release-readiness** — RC: [#1130](../../issues/1130), [#1258](../../issues/1258), [#1168](../../issues/1168) — the published tree carries no private host, path or lab value; every `docs/` folder meets the handbook documentation rules; CI workflows do not mount the host Docker socket — blocked: see issue
- **scheduler** — M32: [#105](../../issues/105) — guarded (CAS) writes for abandoned-run recovery and triage release
- **sdlc** — RC: [#524](../../issues/524) (decision pending); M33: [#1142](../../issues/1142), [#1041](../../issues/1041), [#894](../../issues/894), [#250](../../issues/250), [#182](../../issues/182), [#141](../../issues/141), [#124](../../issues/124), [#71](../../issues/71) — webhook fixtures use FIPS-strength HMAC keys; webhook reviews, one-verb onboarding, triage coalescing, derived worker allowlist, adoption mode, e2e gate, transport parity, terminal-state safety
- **secrets** — RC: [#723](../../issues/723) (decision pending); M32: [#120](../../issues/120) — secret rotation policies actually rotate; storage-level write arbitration across nodes
- **security-hardening** — RC: [#769](../../issues/769) (blocked: see issue); M32: [#177](../../issues/177) — the scoped FIPS gate runs on push again; server certificate SANs honoured and unique
- **software-factory** — M33: [milestone 33](../../milestone/33), [#1126](../../issues/1126), [#1136](../../issues/1136), [#903](../../issues/903) — triage, task workers, review and merge run on the QuicBaaS deployment and the old host is drained; company agents as standing MCP principals; MCP signing replaces the signing socket

## Later

- **declarative-placement** — [#28](../../issues/28) — declarative workload placement
- **orchestration-volumes** — [#195](../../issues/195) — go-blockstore volumes in container orchestration

## Future

- **identity-refactor** — layer the identity surface over go-qrdb's overlapping primitives and enforce authentication on UI routes
- **websocket** — the WebSocket server compiles and is unit-tested but is not mounted on any listener ([docs/websocket/api.md](docs/websocket/api.md))
- **tenant-agent** — a QuicBaaS tenant agent replaces the Company Agent ([milestone 33](../../milestone/33))

## Dependencies

- Up (pinned to release tags, `make check-library-pins`), pinned → needed:
  - go-xkms v0.49.0-alpha → v0.52.0-alpha ([#1171](../../issues/1171)); the only dependency for DICE and attestation primitives
  - go-qrdb v0.30.1-alpha → v0.47.0-alpha ([#1171](../../issues/1171))
  - go-quicraft v0.15.0-alpha (indirect)
  - go-signal v0.2.0-alpha (`pkg/*` import, a `docs/00` L3 violation)
  - go-libsignal v0.2.0-alpha, go-objstore v0.3.0-alpha, go-enclave v0.6.1-alpha, go-auth v0.4.0-alpha, go-codec v1.4.0
  - indirect: go-truststrap v0.4.0-alpha, go-frost v0.3.0-alpha
- Down: go-trusted-platform (pins v0.12.0-alpha), go-recountly.
