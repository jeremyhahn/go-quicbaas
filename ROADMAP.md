# go-quicbaas roadmap
> **Status: REFERENCE** · Last verified: 2026-10-15 · Per handbook `software/conventions/roadmap.md`: the Software Department refreshes it; the project agent proposes changes by docs-only PR.
## Mission
Layer 4 of the stack, one product in layers: the application-layer framework and backend as a service. It embeds go-qrdb as a library, composes go-xkms on top with the hardware crypto passed in, re-exports every service of both through its SDK, and adds the application services. Every feature, its requirements and its status are in [docs/SPEC.md](docs/SPEC.md); what has shipped is in [CHANGELOG.md](CHANGELOG.md).

## Milestones
- [Dev complete](../../milestone/15) — target: RC, every SPEC row shipped and the API stable — 32 open, 60 closed — due 2026-11-16
- [Local Software Factory on QuicBaaS](../../milestone/33) — target: the local software factory running on QuicBaaS — 10 open, 11 closed — due 2026-11-06
- [Deployed on raft-1/2/3](../../milestone/32) — target: every node runs one go-quicbaas tag, with formation, smoke and failover evidence — 13 open, 12 closed — due 2026-11-11

## Complete
- **cache** — L1 in-memory cache with optional L2 Redis — release 1.0.0 — done
- **credentials** — versioned named-credential rotation — release 0.8.0-alpha — done
- **messaging** — channel pub/sub, delta publishing and deduplication — release 1.0.0 — done
- **observability** — health, readiness, metrics, logging and tracing — release 0.8.0-alpha — done
- **ollama** — the Ollama client and embedding provider — releases 0.13.0-alpha and 1.0.0 — done
- **parameter-store** — tenanted configuration parameters — release 0.1.0-alpha — done
- **service-registry** — lease-based service registry on every transport — [#1143](../../issues/1143), release 0.14.0-alpha — done
- **signal-bridge** — the go-signal bridge — release 0.4.0-alpha — done

## Now
- **acme** — re-export the go-xkms ACME service; the own `/api/v1/acme/*` verbs are removed — DC: [#1327](../../issues/1327) — blocked: see issue
- **audit-logging** — the audit signing key is held in protected memory — DC: [#23](../../issues/23) — blocked: see issue
- **authorization** — REST permissions derive from the operation; every local role, permission and check is replaced by go-qrdb's — DC: [#338](../../issues/338) — blocked: see issue
- **certificate-authority** — re-export the go-xkms CA, with device identity issued by it; the own certificate service and second datastore are removed — DC: [#1327](../../issues/1327) — blocked: see issue
- **composition** — the module route-collision guard does not depend on registration order — DC: [#477](../../issues/477) — blocked: see issue
- **iam** — `IAMService` mapped under authz, email-only login, invite self-registration, PIV attestation trust settings, a stable race pass — DC: [#301](../../issues/301), [#377](../../issues/377), [#358](../../issues/358), [#800](../../issues/800), [#718](../../issues/718) — open
- **mcp-server** — the public MCP tool set matches its specification and is guarded — DC: [#1127](../../issues/1127) — blocked: see issue
- **oauth2-oidc** — `acr` / `amr` emitted as documented; an xkms-issued FIDO2 login token verifies against the published JWKS — DC: [#392](../../issues/392), [#486](../../issues/486) — blocked: see issue
- **object-storage** — object storage and dynamic secrets are tenant-isolated as the compliance model claims — DC: [#32](../../issues/32) — blocked: see issue
- **orchestration** — RPCs always authenticated and gated by the role matrix; the session log neither overwrites events nor strands a conclusion; a pending task is claimed once across nodes — DC: [#502](../../issues/502), [#637](../../issues/637), [#200](../../issues/200), [#201](../../issues/201); M32: [#719](../../issues/719) — blocked: see issue
- **secrets** — rotation policies are performed, not only stored — DC: [#723](../../issues/723) — blocked: see issue
- **security-hardening** — the scoped FIPS gate runs on push; webhook fixtures use FIPS-strength HMAC keys — DC: [#769](../../issues/769), [#524](../../issues/524) — blocked: see issue
- **release-readiness** — no private host, path or lab value in the published tree; docs meet the handbook rules; CI mounts no host Docker socket — DC: [#1130](../../issues/1130), [#1258](../../issues/1258), [#1168](../../issues/1168) — open

## Next
- **accounts** — the spend cap is checked per turn — M33: [#782](../../issues/782) — blocked: see issue
- **agent-fleet** — agent images published to the registry — M32: [#756](../../issues/756) — open
- **attestation** — no firmware module in the dependency graph — M32: [#1185](../../issues/1185) — blocked: see issue
- **backup-restore** — re-export go-qrdb's backup service; the own backup manager is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **barrier** — passphrases, shares, recovery keys and PINs off argv and out of plain strings — M32: [#237](../../issues/237) — blocked: see issue
- **bootstrap** — first-boot re-entry delivers the admin credential — M32: [#64](../../issues/64) — open
- **cluster** — no hardcoded bind, advertise or upstream addresses — M32: [#185](../../issues/185) — open
- **codec** — the unused own codec package is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **data-encryption-keys** — re-export go-qrdb's DEK service; the own DEK store is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **data-services** — the own data clients and the second store beside the qrdb node are removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **deployment** — every node runs one go-quicbaas tag; formation, smoke and failover pass with a re-runnable suite — M32: [milestone 32](../../milestone/32) — open
- **desktop-gui** — the Prompts page in the web UI — [#806](../../issues/806) — blocked: see issue
- **embeddings** — re-export go-qrdb's embedding service; the own embedding service is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **go-sdk** — admin FIDO2 / PIV factor enrollment published in the SDK — M32: [#378](../../issues/378) — open
- **kms** — re-export go-xkms KMS; the own key store and backend router are removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **multi-tenancy** — `tenant barrier unseal-share` takes its share off argv — M32: [#488](../../issues/488) — blocked: see issue
- **multiprotocol-api** — go-qrdb's tenant and auth chain inherited on every transport — M32: [#1150](../../issues/1150), [#1141](../../issues/1141) — blocked: see issue
- **object-storage** — objects through go-qrdb's storage API; the direct go-objstore require is removed — [#1378](../../issues/1378) — blocked: waits on go-qrdb's Working fundamentally milestone
- **projects** — the project record serves the Software Factory on QuicBaaS — M33: [milestone 33](../../milestone/33) — open
- **rag** — re-export go-qrdb's RAG service; the own pipeline is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **scheduler** — guarded (CAS) writes for abandoned-run recovery and triage release — M32: [#105](../../issues/105) — open
- **sdlc** — webhook reviews, one-verb onboarding, adoption mode, the e2e gate and transport parity — M33: [#1142](../../issues/1142), [#1041](../../issues/1041), [#182](../../issues/182), [#141](../../issues/141), [#124](../../issues/124) — open
- **software-factory** — triage, workers, review and merge run on QuicBaaS; company agents as standing MCP principals; MCP signing replaces the signing socket — M33: [#1126](../../issues/1126), [#1136](../../issues/1136), [#903](../../issues/903) — open
- **ssh** — re-export the go-xkms SSH service; the own facade and key store are removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **timeseries** — re-export go-qrdb's time-series service; the own service is removed — [#1321](../../pulls/1321) — blocked: held for the owner's word
- **vector** — re-export go-qrdb's vector service; the own service and local index are removed — [#1321](../../pulls/1321) — blocked: held for the owner's word

## Later
- **hardware-backend-composition** — device backend adapters registered in the composition, the trusted-firmware go-xkms adapter first — [#1321](../../pulls/1321) — open
- **declarative-placement** — declarative workload placement — [#28](../../issues/28) — blocked: see issue
- **orchestration-volumes** — go-blockstore volumes in container orchestration, with the NBD volume store behind them — DC: [#195](../../issues/195) — blocked: see issue
- **container-runtime** — libcontainer in-process in the fleet node agent, co-located with quicbaasd — DC: [#1349](../../issues/1349) — blocked: see issue
- **services** — a signed service manifest, the seven lifecycle verbs on every transport, A/B slots and one rollout controller — DC: [#1344](../../issues/1344) — blocked: see issue
- **oci-registry** — the OCI registry re-homed: blobs and metadata through go-qrdb's storage API, mTLS and TPM-attestation authentication — DC: [#1345](../../issues/1345) — blocked: see issue
- **measured-launch** — measured containers, the cluster manifest, provenance, default deny — DC: [#1346](../../issues/1346) — blocked: see issue
- **service-policy** — policy as signed data with one Go engine — DC: [#1347](../../issues/1347) — blocked: see issue
- **transparency-log** — go-quicbaas's records appended to go-xkms's transparency log through `sdk/go` — DC: [#1341](../../issues/1341) — blocked: waits on go-xkms's transparency log
- **release-signing-key** — the tenant release signing key as the default signer of images and CoRIM, with a key named per repository, re-exported through `sdk/go` — [#1827](../../issues/1827) — blocked: waits on go-xkms#1697

## Future
- **identity-refactor** — the identity surface layered over go-qrdb's primitives; authentication on UI routes — [docs/SPEC.md](docs/SPEC.md) — open
- **websocket** — the WebSocket server mounted on a listener — [docs/websocket/api.md](docs/websocket/api.md) — open
- **tenant-agent** — a QuicBaaS tenant agent replaces the Company Agent — [milestone 33](../../milestone/33) — open

## Dependencies
- Up, pinned to release tags (`make check-library-pins`): go-xkms v0.49.0-alpha (the release without a firmware dependency is needed, [#1185](../../issues/1185)), go-qrdb v0.30.1-alpha, go-quicraft v0.15.0-alpha (indirect), go-signal v0.2.0-alpha (a `pkg/*` import), go-libsignal v0.2.0-alpha, go-objstore v0.3.0-alpha (a direct require: go-objstore owns all object storage and go-quicbaas re-exports it; buckets and multipart wait on go-objstore #89 and #90), go-enclave v0.6.1-alpha, go-auth v0.4.0-alpha, go-codec v1.4.0. Down: go-trusted-platform and go-recountly
- Seam waits on go-qrdb#1128: the compliance-mode switch, read through sdk/go, that the iam row's SO separation of duties follows (owner ruling 132)
- Seam waits on go-xkms#1697: the per-repository signing-key binding, tenant release key by default, that the release-signing-key row re-exports (owner rulings 133 (a), 134)
- Standards watch: draft-ietf-oauth-v2-1 (The OAuth 2.1 Authorization Framework), pinned at revision -16, implemented by `pkg/iam` ([copy and index](docs/reference/README.md))
