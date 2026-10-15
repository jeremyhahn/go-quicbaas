# QuicBaaS Documentation

> **Status: NORMATIVE** · Last verified: 2026-10-11
> The index of feature documentation and the record of this repository's
> migration state. Conflicts with this document are defects.

Every feature in [SPEC.md](SPEC.md) that needs more than its SPEC row has a
folder here. Which files a folder holds is set by the handbook's
`software/conventions/feature-docs.md`: each file exists only when it has
content, and a placeholder is deleted.

## Migration state

**Complete.** 44 feature folders, 257 files, migrated 2026-08-19; last
recounted 2026-10-14. Counting is derived, not maintained:
`ls docs/*/architecture.md | wc -l` and `find docs -name '*.md' | wc -l`.

| Date | Change |
|---|---|
| 2026-08-19 | The `docs/api/` + `docs/architecture/` + `docs/cli/` + `docs/security/` split, plus per-topic folders, became feature folders. |
| 2026-09-10 | The three folders `agent-orchestration`, `sdlc` and `agentic-projects` became one, [software-factory/](software-factory/): the engine's ten pages became the base, sdlc's seven (architecture, binding, configuration, outcomes, README, api, runbook) and agentic-projects' five (architecture, engineering, cli, api, user) were absorbed into them, and the result is the five canonical files plus supplements `sdlc.md`, `projects.md`, `accounts.md`, `prompt-sets.md`, `job-runners.md`, `autonomy-and-dispatch.md`, `job-admission.md`, `sandbox.md` and `cost-enforcement.md`. The two folders that had no `cli.md`/`engineering.md`/`user.md` now do. The folder count had already drifted to 39 before this change; both figures above are recounted, not adjusted. |
| 2026-10-11 | The orchestration engine moved out of [software-factory/](software-factory/) into its own folder, [orchestration/](orchestration/) (#1231): the five canonical files' engine content, and the supplements `job-runners.md`, `autonomy-and-dispatch.md`, `job-admission.md`, `sandbox.md` and `fleet.md`. software-factory/ keeps projects, accounts, the SDLC module and prompt sets, and points at the engine. The content was brought up to date with agent sessions, inference backends, attach, job amendments and the list contract, and the CLI verbs and API routes were re-checked against the code. The counts above are recounted. |
| 2026-10-11 | The SDLC module moved out of `software-factory/` into its own folder, [sdlc/](sdlc/): `software-factory/sdlc.md` became `sdlc/architecture.md`, the supplements `review.md`, `triage.md`, `merge-train.md` and `ci-failure-classification.md` moved with it, and the SDLC sections of `software-factory/{engineering,cli,api,user}.md` became the new folder's four other canonical files. `software-factory/projects.md` became [projects/](projects/), and `accounts.md`, `cost-enforcement.md` and `job-admission.md` became [accounts/](accounts/); `orchestration/job-admission.md` is a pointer into accounts/. Each old path keeps a one-paragraph pointer page so existing links resolve. |
| 2026-10-12 | The fleet content left [orchestration/](orchestration/) and [software-factory/](software-factory/) for its own folder, [agent-fleet/](agent-fleet/): enrollment, heartbeat and recovery, `fleet serve` and `fleet deploy`, the embedded node, and the worker image. `orchestration/fleet.md` became `agent-fleet/user.md` and keeps a pointer page; `software-factory/worker-image.md` became `agent-fleet/worker-image.md`; the fleet sections of the five canonical files moved to their agent-fleet counterparts. The counts above are recounted. |
| 2026-10-14 | Placeholders and stale copies were deleted under the handbook's feature-docs rule: the pointer pages `orchestration/fleet.md`, `orchestration/job-admission.md` and `software-factory/{ci-failure-classification,merge-train,review,triage,cost-enforcement}.md`, which no link reached; `parameter-store/cli.md` and `signal-bridge/user.md`, which had no content; and `archive/`. The pointer pages `software-factory/sdlc.md` and `software-factory/projects.md` stay while source comments link to them. The counts above are recounted. |

**This file is the migration record.** New features get a folder from day one;
do not add a flat `.md` at the `docs/` root, and do not recreate the old
`docs/api/`, `docs/architecture/`, `docs/cli/` or `docs/security/` split — those
directories were removed deliberately and their content is inside the feature
folders.

Documentation was reshaped from the previous layout (a `docs/api/`,
`docs/architecture/`, `docs/cli/` and `docs/security/` split, plus per-topic
folders) into the folders below. Content was **moved and absorbed, not
rewritten**; where a document contradicted the code, the document was corrected
and the correction noted in place.

Five directories are deliberately **not** feature folders and keep their
existing shape:

| Directory | Why it is not a feature folder |
|---|---|
| `deployment/` | Operational guides — Kubernetes manifests, production checklist. Not a capability. |
| `design/` | Working design documents and roadmaps for work that is planned, not built. A feature folder describes what exists. |
| `identity-refactor/` | A cross-repo migration plan, still awaiting sign-off. |
| `testing/` | Contributor guides for running the suites. |
| `release/` | What must be true of the tree before it is published, and the gates that enforce it. |

There is one roadmap, [`ROADMAP.md`](../ROADMAP.md) at the repository root, and
one specification, [SPEC.md](SPEC.md), which lists every feature.

## Features

### Application layer — owned by this repository

| Feature | What it is |
|---|---|
| [orchestration/](orchestration/) | The orchestration engine: agent sessions and their durable event log and attach, backends and inference, the job envelope and its runners, workflows, tasks and autonomy, container trust and sandboxing, and the list contract |
| [agent-fleet/](agent-fleet/) | The agent fleet: remote and embedded fleet nodes, mTLS enrollment, heartbeat and recovery, `fleet serve` and `fleet deploy`, the node sandbox, and the worker image |
| [software-factory/](software-factory/) | The Software Factory built on the engine: how the engine, projects, accounts and the SDLC module fit together, the agent catalog, prompt sets, admission and readiness and workspace storage tiers |
| [accounts/](accounts/) | Pooled provider accounts, the spend cap, cost enforcement and job admission |
| [sdlc/](sdlc/) | The Software Factory's SDLC module: a labelled forge issue becomes a branch, a pull request, a review and a merge — project bindings, onboarding and conformance, review, triage, the merge train, CI-failure classification and the attention inbox |
| [projects/](projects/) | Projects: the tenant-scoped record that ties a repository, a git identity and signing history, a workspace, fleet-node roles and limits together; onboarding, forge bindings and source-repository identity |
| [messaging/](messaging/) | Channels, pub/sub, delta publishing, deduplication |
| [scheduler/](scheduler/) | Distributed job scheduling, cron and delayed jobs, leader-elected execution |
| [cache/](cache/) | L1 in-memory LRU with optional L2 Redis, entity-aware |
| [rag/](rag/) | Retrieval-augmented generation: chunking, ranking, context assembly |
| [embeddings/](embeddings/) | Embedding providers — Ollama, OpenAI, Voyage, LM Studio |
| [ollama/](ollama/) | The Ollama client: embeddings, chat, model inventory, health |
| [iam/](iam/) | Identity lifecycle across fifteen sub-services: users, roles, groups, tenants, sessions, MFA, WebAuthn, GDPR |
| [oauth2-oidc/](oauth2-oidc/) | OAuth 2.1 / OIDC provider, MCP authorization, external IdP federation, DPoP |
| [authorization/](authorization/) | RBAC through go-qrdb's PDP, plus the ABAC attribute overlay |
| [audit-logging/](audit-logging/) | The audit trail: recording, querying, signing |
| [multi-tenancy/](multi-tenancy/) | Tenant lifecycle and per-tenant barriers |
| [parameter-store/](parameter-store/) | Tenanted configuration parameters |
| [object-storage/](object-storage/) | Tenanted object storage: backends, tiering, replication, catalog |
| [observability/](observability/) | Health, liveness, readiness, metrics, logging, tracing |

### Composed stack — this repository's surface over the layers below

| Feature | What it is |
|---|---|
| [composition/](composition/) | The composer: building go-xkms adapters and injecting them into go-qrdb. The capability this repository alone owns |
| [multiprotocol-api/](multiprotocol-api/) | The unified API server and the transport model — start here to understand how any capability reaches any transport |
| [go-sdk/](go-sdk/) | The Go SDK, the canonical entry point for the whole stack |
| [mcp-server/](mcp-server/) | The Model Context Protocol surface: 520 registered tools, scope-filtered per principal |
| [desktop-gui/](desktop-gui/) | The Wails desktop application and the embedded web UI |
| [data-services/](data-services/) | KV, DAO, collections, transactions, batch, sharding, storage engine |
| [vector/](vector/) | Vector namespaces, search, indexing |
| [timeseries/](timeseries/) | Time-series write and query, retention |
| [cluster/](cluster/) | Raft consensus, membership, discovery, rebalancing, node operations |
| [service-registry/](service-registry/) | Tenant-private service discovery: lease-backed registration, resolve, ordered watch |
| [kms/](kms/) | Key management and cryptographic operations |
| [data-encryption-keys/](data-encryption-keys/) | DEK lifecycle, rotation, re-encryption, the safety interlock |
| [secrets/](secrets/) | The secrets engine, dynamic secrets, secret-reference resolution |
| [credentials/](credentials/) | Credential storage and lifecycle |
| [barrier/](barrier/) | The system barrier: init, unseal, seal, rekey, recovery, Shamir custody |
| [certificate-authority/](certificate-authority/) | How the go-xkms CA, ACME and trust services are reached: transports, permissions, CLI |
| [ssh/](ssh/) | Server-held SSH keys, known hosts, authorized keys |
| [bootstrap/](bootstrap/) | First-boot ceremony and trust bootstrap |
| [attestation/](attestation/) | The attestation Verifier: endorsements, signed reference values, TPM quote appraisal; statement and chain verification |
| [backup-restore/](backup-restore/) | Backup to S3/Azure/GCS/MinIO, restore, scheduling |
| [codec/](codec/) | Serialization |
| [security-hardening/](security-hardening/) | FIPS mode, TLS posture, ingress binding, the hardening baseline |
| [signal-bridge/](signal-bridge/) | The go-signal bridge (build-tag gated) |
| [websocket/](websocket/) | The WebSocket server package — **see the note below** |
| [errors/](errors/) | Error conventions: qrdb-coded sentinels, why `errors.Is` over-matches them, and the guard |

## Three things a new reader should know before planning against these docs

These were found while verifying the documentation against the code during the
2026-08-19 migration. They are recorded here because each one invalidates a
plan that the surrounding documentation would otherwise support.

1. **Nothing in this repository serves HTTP/3.** The SDK's `quic` transport
   (`sdk/go/transport/quic`) embeds `*rest.Transport` and swaps only the
   `http.Client` for an `http3.Transport`. There is no `ServerQUICConfig`, no
   `--quic-port` on `quicbaasd`, and no `http3.Server` anywhere under `pkg/`.
   Every QUIC row in an `api.md` is its REST row carried over HTTP/3 by a
   client that currently has nothing to dial.
   Detail: [multiprotocol-api/architecture.md](multiprotocol-api/architecture.md).

2. **The WebSocket server is not mounted on any listener.**
   `pkg/api/websocket` is imported by nothing outside itself. A running
   `quicbaasd` does not accept WebSocket upgrades.
   Detail: [websocket/api.md](websocket/api.md).

3. **Environment variables work only where they are explicitly read.**
   `pkg/config/loader.go` honours one list of 47 variables in
   `applyEnvironmentOverrides`. It also calls `viper.AutomaticEnv()`, but the
   config struct is populated by `Unmarshal`, and viper's automatic binding does
   not reach `Unmarshal` targets for keys never registered with `SetDefault` or
   `BindEnv` — and none are. A `QUICBAAS_*` variable outside that list is
   silently ignored. Each feature's `engineering.md` states which of its
   settings are file-only. A variable that IS on the list but does not parse
   (a port that is not an integer, a duration `time.ParseDuration` rejects, a
   boolean other than `true`/`1`/`yes`/`on` or `false`/`0`/`no`/`off`) fails
   `Loader.Load` with `config.InvalidEnvValueError` naming the variable; it is
   never dropped in favour of the file or default value. `quicbaasd` parses a
   few variables itself (`QUICBAAS_SERVER_{WRITE,READ,IDLE}_TIMEOUT`,
   `QUICBAAS_SERVER_SHARDS`, `QUICBAAS_SERVER_ENTITY_SHARD_PINS`,
   `QUICBAAS_ENABLE_SCHEDULER`, `QUICBAAS_{TIMESERIES,VECTOR,BACKUP}_ENABLED`)
   with the same rules: a malformed value fails daemon startup with the same
   error.

## What "Last verified: 2026-08-19" does and does not claim on these pages

Every file carries the stamp, and the stamp was earned differently depending on
how the file came to be. Saying so is the point: a stamp that means two things
without saying which is worse than no stamp.

**Fully verified.** Everything authored during the migration, and every
`api.md` transport matrix. The matrices are generated from source on each
publish, so they cannot drift silently. Every CLI verb and flag written during
the migration was read out of the cobra tree in `cmd/`, every config key out of
`pkg/config`, every route out of `pkg/api/rest`.

**Verified structurally, with a residue.** Content moved from the previous
layout was checked by extracting every backticked identifier from every feature
document and grepping it against `pkg/`, `sdk/`, `cmd/`, `api/`, and then
against the go-qrdb and go-xkms module sources. Of 3,330 identifiers:

| Outcome | Count |
|---|---|
| resolve in this repository | 2,466 |
| resolve in go-qrdb or go-xkms (correct: docs describing consumed types) | 491 |
| resolve nowhere | 373 |

Of the 373, 49 are dotted selectors whose base type does exist here, and a
further share are not Go identifiers at all — wire error codes
(`CHANNEL_NOT_FOUND`, `DEK_NOT_FOUND`), third-party environment variables
(`ANTHROPIC_BASE_URL`), Svelte icon component names (`CheckCircle2`,
`Building2`) and build-output paths. **The remainder is genuine stale-identifier
drift inherited from the previous documentation and not yet corrected.**

Reproduce the check:

```bash
grep -oh '`[^`]\+`' docs/*/*.md | tr -d '`' \
  | grep -E '^([A-Za-z_][A-Za-z0-9_]*\.)?[A-Z][A-Za-z0-9_]{2,}$' | sort -u \
  | while read -r id; do
      grep -rqF -- "${id##*.}" pkg sdk cmd api || echo "MISS $id"
    done
```

Correcting that residue is a second pass, per feature, and it is the natural
next piece of work on this documentation. Doing it feature by feature is
deliberate: a bulk find-and-replace across 190 files is how a correct name gets
replaced with a plausible wrong one.


## Where the transports are documented

Rather than repeat the transport model in 38 places, it lives once in
[multiprotocol-api/architecture.md](multiprotocol-api/architecture.md), and each
feature's `api.md` carries a matrix generated from source: SDK method names from
`sdk/go/client/<pkg>`, REST routes from the transport's route tables, a
served-or-not check against the server's own route table in `pkg/api/rest`, MCP
tool names from the server tool registry, embedded routes from the embedded
transport, and the required `resource:action` from
`pkg/api/rest/routes_permissions_generated.go`.

A **no** in the `Served?` column is a live SDK-to-server mismatch, not a
documentation gap. Several exist and are named in place.

## Quick start

```bash
# Start a server. REST, gRPC and a Unix socket; MCP from config.
quicbaasd start --http-port 18080 --grpc-port 19090 \
  --unix-socket /tmp/quicbaas.sock --data-dir ./data

# Basic operations.
quicbaas --address https://quicbaas.example.com:18080 data put users user1 '{"name":"Alice"}'
quicbaas --address https://quicbaas.example.com:18080 data get users user1

# Vector search.
quicbaas vector namespace create embeddings --dimension 384
quicbaas vector upsert embeddings --id doc1 --vector "0.1,0.2,..." --metadata '{"title":"Doc"}'
quicbaas vector search similar embeddings --vector "0.1,0.2,..." --k 10
```

Feature-specific getting-started material is in each folder's `user.md`.

## Contributing to these docs

- **A PR that changes a feature's behaviour without touching its folder is an
  incomplete PR.**
- Docs follow code for work already shipped: where a document contradicts the
  code, the document is wrong. Fix the document to the canonical name and note
  the correction. Never create an identifier to make a document true.
- For new work the order reverses: the doc is written or updated in the same PR
  as the code, far enough ahead to be reviewed as the requirement.
- Every Go identifier, path, CLI verb and config key must be grepped in this
  repository before it is written down.
- **These folders publish.** No internal addresses, host names, workstation
  paths or internal issue references. Use `quicbaas.example.com`,
  `<install-dir>`, `$XDG_CONFIG_HOME/...`.
- Update the `Last verified:` stamp only after checking the claims, not after
  re-reading the prose.
