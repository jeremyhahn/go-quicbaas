# go-quicbaas roadmap

> **Status: REFERENCE** · Last verified: 2026-10-12
> Per handbook `software/conventions/roadmap.md`: the Software Department refreshes it; the project agent proposes changes by PR.
> This is the only roadmap.

## Mission

Layer 4 of the stack, one product in layers (owner ruling 2026-10-14): the application-layer framework and backend as a service. It embeds go-qrdb as a library, composes go-xkms on top with the hardware crypto passed in, re-exports every service of both through its SDK, and adds the application services — agentic orchestration and the Software Factory, messaging, scheduler and placement, service registry, attestation Verifier and reference-value registry, OAuth 2.1 / OIDC. One cluster engine and one datastore per deployment.

Every feature, its requirements and its status are in [docs/SPEC.md](docs/SPEC.md); this roadmap and that table agree. Each line below starts with the SPEC feature it belongs to. What has shipped is in [CHANGELOG.md](CHANGELOG.md).

## Milestones

The current milestone first, then every other open milestone in delivery order.

- [Release candidate](../../milestone/15) — target: release candidate — 28 open, 46 closed — due 2026-11-16
- [Deployed on raft-1/2/3](../../milestone/32) — target: every node runs one go-quicbaas tag, with formation, smoke and failover evidence — 17 open, 5 closed — due 2026-10-30
- [Local Software Factory on QuicBaaS](../../milestone/33) — target: the local software factory running on QuicBaaS — 14 open, 4 closed — due 2026-11-06

## Complete

Shipped features, each with the release that carries it ([CHANGELOG.md](CHANGELOG.md)).

- **cache** — L1 in-memory cache with optional L2 Redis — release 1.0.0 — done
- **credentials** — versioned named-credential rotation — release 0.8.0-alpha — done
- **messaging** — channel pub/sub, delta publishing and deduplication — release 1.0.0 — done
- **observability** — health, readiness, metrics, logging and tracing — release 0.8.0-alpha — done
- **ollama** — the Ollama client — release 0.13.0-alpha — done
- **ollama** — the Ollama embedding provider — release 1.0.0 — done
- **parameter-store** — tenanted configuration parameters — release 0.1.0-alpha — done
- **service-registry** — lease-based service registry on every transport — [#1143](../../issues/1143), release 0.14.0-alpha — done
- **signal-bridge** — the go-signal bridge — release 0.4.0-alpha — done

## Now

- **go-sdk** — platform bootstrap re-exports for go-trusted-platform: go-xkms v0.52.0-alpha and go-qrdb v0.47.0-alpha adopted; tenant context, signer store, TPM policy, secret value helpers and the ACME TPM adapter published in `sdk/go`; released as v0.15.0-alpha — M32: [#1171](../../issues/1171) — open
- **attestation** — no firmware module in the dependency graph: the go-xkms release without a firmware dependency is adopted and the arduino-generic indirect require is dropped — M32: [#1185](../../issues/1185) — blocked: see issue
- **iam** — self-service registration through invite activation on every transport — [#358](../../issues/358) — in review

## Next

RC is the [Release candidate](../../milestone/15), M32 is [Deployed on raft-1/2/3](../../milestone/32), M33 is [Local Software Factory on QuicBaaS](../../milestone/33), which is not in the release candidate. A removal held for the owner's word has no issue yet; it is listed in [#1321](../../pulls/1321).

- **accounts** — sessions roll with account rotation; the spend cap is checked per turn — M33: [#997](../../issues/997), [#782](../../issues/782) — open
- **acme** — re-export the go-xkms ACME service; the own `/api/v1/acme/*` verbs are removed, after [#1171](../../issues/1171) — RC: [#1327](../../issues/1327) — open
- **agent-fleet** — agent images published to the registry — M32: [#756](../../issues/756) — open
- **agent-fleet** — fleet listings are tenant-scoped and rendered by one served-backends renderer — [#680](../../issues/680), [#700](../../issues/700) — done
- **attestation** — unattended unlock across updates, step 3: reference values (signed CoRIM, `attestation:provision`) are the registry of approved images, fetched by go-trusted-platform through the SDK — [#1321](../../pulls/1321) — blocked: waits on the go-xkms step-2 tag
- **audit-logging** — audit signatures fail closed and the signing key is protected; the own audit model and store removed on the owner's word (re-export of the lower service) — RC: [#22](../../issues/22), [#23](../../issues/23) — blocked: decision pending
- **authorization** — permissions derive from the operation; open decision: is in-process access through the embedded transport trusted by design, or gated — RC: [#338](../../issues/338), [#329](../../issues/329) — blocked: decision pending
- **backup-restore** — re-export go-qrdb's backup service; the own backup manager is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **barrier** — passphrases, shares, recovery keys and PINs off argv and out of plain strings — M32: [#237](../../issues/237) — open
- **barrier** — the barrier passphrase is read from stdin or `QUICBAAS_BARRIER_PASSPHRASE`, never argv; the own seal service removed on the owner's word (re-export of the lower service) — [#180](../../issues/180) — done
- **bootstrap** — SPIFFE trust domain settable through the SDK; first-boot re-entry delivers the admin credential; the own ceremony facade removed on the owner's word (re-export of the lower service) — M32: [#125](../../issues/125), [#64](../../issues/64) — open
- **certificate-authority** — re-export the go-xkms CA, with device identity issued by the go-xkms CA; the own certificate service, SPIFFE SAN and device-identity logic and the second datastore are removed, after [#1171](../../issues/1171) — RC: [#1327](../../issues/1327) — open
- **cluster** — no hardcoded bind, advertise or upstream addresses; the own cluster service and client removed on the owner's word (re-export of the lower service) — M32: [#185](../../issues/185) — open
- **codec** — the unused own codec package is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **composition** — module route registration is order-independent; pin the released go-qrdb and go-xkms tags — M32: [#1141](../../issues/1141) — open
- **composition** — the composition decision — RC: [#477](../../issues/477) — blocked: decision pending
- **data-encryption-keys** — re-export go-qrdb's DEK service; the own DEK store is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **data-services** — `storage.data_dir` derives `wal_dir`; the own data clients and the second store beside the qrdb node removed on the owner's word (re-export of the lower service) — M32: [#176](../../issues/176) — open
- **deployment** — every node runs one go-quicbaas tag; formation, smoke and failover pass with a re-runnable suite — M32: [milestone 32](../../milestone/32) — open
- **desktop-gui** — the "N online" count ignores agents that never started and honours `last_seen`; the Prompts page in the web UI — [#851](../../issues/851), [#806](../../issues/806) — open
- **desktop-gui** — the web UI reconciles the session identity before it attaches a bearer — [#369](../../issues/369) — done
- **embeddings** — re-export go-qrdb's embedding service; the own embedding service is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **go-sdk** — admin FIDO2 / PIV factor enrollment published in the SDK — M32: [#378](../../issues/378) — open
- **go-sdk** — unattended unlock step 3: `Security().TPMPolicy()` and the initrd re-exports as go-xkms aliases, the TPM-policy update variant on all six transports — [#1321](../../pulls/1321) — blocked: waits on the go-xkms step-2 tag
- **go-sdk** — an embedded-transport stream Close no longer races a handler send; `sdk/go/xkms` re-exports the ACME http-01 responder and the SSH fingerprint validator — [#144](../../issues/144), [#560](../../issues/560) — done
- **iam** — `IAMService` mapped under authz, email-only login, TOTP secrets protected; open decisions: does hardware attestation alone satisfy `mfa_verified`, and does a PIV slot-attestation certificate reach the server in the TLS handshake or out of band — RC: [#301](../../issues/301), [#377](../../issues/377), [#402](../../issues/402), [#416](../../issues/416), [#422](../../issues/422) — open
- **iam** — PIV attestation trust settings reach the go-xkms module route — RC: [#800](../../issues/800), [#718](../../issues/718) — blocked: see issue
- **iam** — REST listener and IAM race tests stable under load; self-registration succeeds and leaves no orphan tenant — M32: [#482](../../issues/482) — open
- **iam** — SDK session verbs on `/api/v1/iam` with a per-request bearer; invalid IAM fields are refused as invalid arguments on every transport; the SDK resolves a user from a client certificate under `Security().Auth()`; the own user, role, token, policy, MFA and session storage removed on the owner's word (re-export of the lower service) — [#259](../../issues/259), [#335](../../issues/335), [#376](../../issues/376) — done
- **kms** — re-export go-xkms KMS; the own key store and backend router are removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **mcp-server** — the public MCP tool set matches its specification and is guarded — RC: [#1127](../../issues/1127) — blocked: see issue
- **multi-tenancy** — an empty caller tenant is never the system tenant; `tenant barrier unseal-share` takes its share off argv; the own tenant manager and storage removed on the owner's word (re-export of the lower service) — M32: [#488](../../issues/488) — open
- **multi-tenancy** — the multi-tenancy decision — RC: [#348](../../issues/348) — blocked: decision pending
- **multiprotocol-api** — go-qrdb's tenant and auth chain inherited on every transport — M32: [#1150](../../issues/1150) — open
- **multiprotocol-api** — REST rate limiting on by default, with an opt-out — [#158](../../issues/158) — done
- **oauth2-oidc** — `acr` / `amr` are emitted as documented — RC: [#392](../../issues/392) — blocked: decision pending
- **oauth2-oidc** — an xkms-issued FIDO2 login token verifies against the published JWKS — RC: [#486](../../issues/486) — blocked: see issue
- **object-storage** — object storage and dynamic secrets are tenant-isolated as the compliance model claims — RC: [#32](../../issues/32) — open
- **orchestration** — orchestration RPCs are always authenticated and gated by the role matrix; the session log neither overwrites events nor strands a conclusion; a pending task is claimed once across nodes — RC: [#502](../../issues/502), [#637](../../issues/637), [#200](../../issues/200); M32: [#719](../../issues/719) — open
- **orchestration** — the orchestration decision — RC: [#201](../../issues/201) — blocked: decision pending
- **projects** — deleting a bound project is refused with a typed error naming `unbind` — M33: [#891](../../issues/891) — open
- **projects** — project-name arbitration yields one winner — [#826](../../issues/826) — done
- **rag** — re-export go-qrdb's RAG service; the own pipeline is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **release-readiness** — the published tree carries no private host, path or lab value; every `docs/` folder meets the handbook documentation rules; CI workflows do not mount the host Docker socket — RC: [#1130](../../issues/1130), [#1258](../../issues/1258), [#1168](../../issues/1168) — blocked: see issue
- **scheduler** — guarded (CAS) writes for abandoned-run recovery and triage release — M32: [#105](../../issues/105) — open
- **sdlc** — webhook fixtures use FIPS-strength HMAC keys; webhook reviews, one-verb onboarding, triage coalescing, derived worker allowlist, adoption mode, e2e gate, transport parity, terminal-state safety — M33: [#1142](../../issues/1142), [#1041](../../issues/1041), [#894](../../issues/894), [#250](../../issues/250), [#182](../../issues/182), [#141](../../issues/141), [#124](../../issues/124), [#71](../../issues/71) — open
- **sdlc** — the sdlc decision — RC: [#524](../../issues/524) — blocked: decision pending
- **sdlc** — the spent trigger label is removed when a merged item's issue closes — [#189](../../issues/189) — done
- **secrets** — secret rotation policies actually rotate; storage-level write arbitration across nodes; the own encrypted secrets backend removed on the owner's word (re-export of the lower service) — M32: [#120](../../issues/120) — open
- **secrets** — the secrets decision — RC: [#723](../../issues/723) — blocked: decision pending
- **security-hardening** — the scoped FIPS gate runs on push again; server certificate SANs honoured and unique — M32: [#177](../../issues/177) — open
- **security-hardening** — the security-hardening release-candidate item — RC: [#769](../../issues/769) — blocked: see issue
- **software-factory** — triage, task workers, review and merge run on the QuicBaaS deployment and the old host is drained; company agents as standing MCP principals; MCP signing replaces the signing socket — M33: [milestone 33](../../milestone/33), [#1126](../../issues/1126), [#1136](../../issues/1136), [#903](../../issues/903) — open
- **software-factory** — requirements contract for automating every deterministic step of the manual factory — M33: [milestone 33](../../milestone/33) — open
- **ssh** — re-export the go-xkms SSH service; the own facade and key store are removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **timeseries** — re-export go-qrdb's time-series service; the own service is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **vector** — re-export go-qrdb's vector service; the own service and local index are removed — [#1321](../../pulls/1321) — blocked: held for the owner's word

## Later

- **hardware-backend-composition** — device backend adapters registered in the composition, the trusted-firmware go-xkms adapter first; consumer go-trusted-platform through the SDK — [#1321](../../pulls/1321) — open
- **declarative-placement** — declarative workload placement — [#28](../../issues/28) — open
- **orchestration-volumes** — go-blockstore volumes in container orchestration — [#195](../../issues/195) — open

## Future

- **identity-refactor** — layer the identity surface over go-qrdb's overlapping primitives and enforce authentication on UI routes — [docs/SPEC.md](docs/SPEC.md) — open
- **websocket** — the WebSocket server compiles and is unit-tested but is not mounted on any listener — [docs/websocket/api.md](docs/websocket/api.md) — open
- **tenant-agent** — a QuicBaaS tenant agent replaces the Company Agent — [milestone 33](../../milestone/33) — open

## Dependencies

- Up (pinned to release tags, `make check-library-pins`), pinned → needed:
  - go-xkms v0.49.0-alpha → v0.52.0-alpha ([#1171](../../issues/1171)); the only dependency for DICE and attestation primitives
  - go-qrdb v0.30.1-alpha → v0.47.0-alpha ([#1171](../../issues/1171))
  - go-quicraft v0.15.0-alpha (indirect)
  - go-signal v0.2.0-alpha (`pkg/*` import, a `docs/00` L3 violation)
  - go-libsignal v0.2.0-alpha, go-objstore v0.3.0-alpha, go-enclave v0.6.1-alpha, go-auth v0.4.0-alpha, go-codec v1.4.0
  - indirect: go-truststrap v0.4.0-alpha, go-frost v0.3.0-alpha
- Down: go-trusted-platform (pins v0.12.0-alpha), go-recountly.
