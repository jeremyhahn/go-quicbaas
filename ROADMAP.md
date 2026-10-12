# go-quicbaas roadmap

> **Status: REFERENCE** · Owner: Software Department · Last verified: 2026-10-11
> Per handbook `software/conventions/roadmap.md`: the Software Department refreshes it; the project agent proposes changes by PR.
> This is the only roadmap. The August 2026 architecture plan is [docs/archive/ROADMAP-2026-08.md](docs/archive/ROADMAP-2026-08.md) (ARCHIVE, not a plan).

## Mission

The only composer: one SDK over the whole stack, plus the application layer it alone owns — agentic orchestration, messaging, RAG, scheduler and placement, service registry, attestation Verifier, IAM/OIDC, tenancy (handbook `docs/00` L1).

## Current milestone

`Public release` milestone (M15): 40 open, 0 closed as of 2026-10-08 (the open-issue list is not re-read from the forge on 2026-10-11; entries below are checked against `main`). Release candidate projected **2027-01-14** through go-signal, or **2026-11-05** if go-signal does not gate it.

Current version: `VERSION` is `0.14.0-alpha`, released by 18a5b994 (#1154) on 2026-10-03; the `v0.14.0-alpha` and `sdk/go` tags are cut on it, which closes #1155. Per-release detail is in [CHANGELOG.md](CHANGELOG.md); feature design is in [docs/README.md](docs/README.md).

## Complete

Grouped by feature; "delivered" names the release (CHANGELOG.md) or the merge on `main`.

### Software Factory (`docs/software-factory/`)
- Issue-driven delivery loop: a labelled forge issue becomes a task, a sandboxed agent run, a pull request, CI, an independent peer review and a merge — delivered v0.8.0-alpha (2026-09-14).
- Agent profiles and pools, the `openvino` backend, node-level triage — v0.9.0-alpha (2026-09-16); triage on small local models, review/unblock fixes — v0.10.0-alpha (2026-09-21).
- Paged, sortable, searchable list surfaces and a dozen stranded-work fixes — v0.11.0-alpha (2026-09-22).
- Production readiness: pooled provider accounts (balanced, capped, failover), fleet signing relay and load/capacity/image placement, per-backend usage, pricing and spend limits, per-project reviewer profiles, credentials held only in the server — v0.13.0-alpha (2026-09-29, #1123). v0.13.1-alpha fixed the case-colliding test files that blocked module download.
- List paging over gRPC for sessions, tasks, remaining orchestrator lists and workflow list on every transport — merge trains 1f8f2a50 (#1203), 5120dbd0 (#1209), 606ddf09 (#1221); #968 slices.
- Per-run docker isolation and agent-identity, job amendments by compare-and-swap, task workspace sweep — e992fe14 (#1208), 8b131dae (#1196), d5015f98 (#1193).
- Worker image: dev-cache build, protobuf toolchain, cgo headers, Serena language servers — 46ea4288 (#1160), cfaef439 (#1162), 5a5f879c (#1165), d5f3a4d1 (#1163).

### Service registry (`docs/service-registry/`)
- Lease-based register / renew / deregister / resolve / watch on gRPC, Unix, REST, QUIC, MCP and embedded, with the `quicbaas registry` CLI — v0.14.0-alpha (#1143, 956d2f87).

### Scheduler and placement (`docs/scheduler/`)
- Workload placement engine (filter and score by labels, capacity, affinity, anti-affinity, topology spread; one guarded transaction; cordon and drain) on every transport — v0.14.0-alpha (#1143, bfe0c7fa).
- Deterministic scheduler and catalog tests — 96d52729 (#1215).

### Attestation
- RFC 9334 Verifier on every transport — v0.14.0-alpha (#1143, b701ea2c).

### Tenancy and transport auth
- Tenant isolation fails closed on every transport (#1150 defects 1, 3, 4) — 64859841 (#1151), v0.14.0-alpha.
- gRPC mTLS tenant resolved through go-qrdb (#1150 slice b) — afb1351a (#1216).
- Platform bearer tokens on every transport — d4cb1fc3 (#1214).
- Admin role resolution and admin hardware-factor handling — 6640700a (#1204).

### Stack and toolchain
- go-xkms v0.49.0-alpha, go-qrdb v0.30.1-alpha, go-quicraft v0.15.0-alpha — v0.14.0-alpha. go-qrdb v0.31.0-alpha is deliberately not adopted (go-chi registers HTTP `QUERY` on all-method routes; a surface decision, CHANGELOG 0.14.0-alpha).
- Go 1.27.0 required from v0.10.0-alpha; Go 1.27.2 adopted by a48348ce (#1174).
- go-xkms v0.17.0-alpha (TPM endorse-01, FIDO2 MCP `email` key) — v0.12.0-alpha; go-xkms v0.14.0-alpha / go-qrdb v0.19.0-alpha (barrier credentials as wipeable bytes, per-permission tenancy scope) — v0.11.0-alpha.
- Secure-memory analyzer enforced in Lint — 405cfd00 (#1207). SDK Go integration builds from published modules — 104cf8df (#1227).
- Audit HMAC sign keys under 112 bits rejected — cb80fbb9 (a partial step toward #22, which stays open).

### CI and documentation
- Parallel unit-test matrix legs (e1a81672, #1177); ci-template v1.15 (49b6af1b); label-gated automerge (f2dc5b62, ecd47015, 4379aab4).
- Status banners and `Last verified` stamps (cb677970, #1186); ROADMAP.md (2e7f8cfc, #1167); one roadmap only (#1230).

## Remaining to the release candidate

Grouped by feature. Not re-read from the forge on 2026-10-11; an item is listed as open unless `main` shows its fix above.

### Transport auth and tenancy
- #1150 — every transport inherits the go-qrdb and go-xkms auth chain: defects 1, 3, 4 and the gRPC mTLS slice are complete (above); the issue stays open for its remaining slices.
- #482 — self-registration works and leaks no tenant (critical) — blocked
- #158 — REST rate limiting on by default — blocked
- #698, #718 — REST listener and IAM race tests stable under load — open

### IAM and hardware factors
- #460, #378, #379 — admin detection and hardware-factor enrollment (critical) — escalated; #1204 (above) landed part of admin role resolution
- The owner-level IAM and authz rulings: #301, #329, #338, #348, #361, #392, #400, #402, #416, #422, #502, #509 (all `task:blocked`)

### Audit, secrets and storage
- #22, #23 — audit signature fail-closed, signing key protected — blocked
- #32 — object storage and dynamic secrets tenant isolation — blocked
- #723 — secret rotation policies actually rotate — open

### Stack and build
- #1149 — go-qrdb pin builds on current go-quicraft — open
- #769 — scoped FIPS gate restored on push — open
- #1142 — SDLC webhook receiver back up — open

### Server lifecycle
- #714, #716 — clean shutdown (inline init, push hubs) — open

### Documentation
- #1131 — docs-accuracy batch — handed to QuicBaaS; #1186 landed the status banners and stamps
- #1230 — this roadmap and a `docs/<feature>/` folder for every feature — in progress

### After the release candidate
- #195 — go-blockstore volumes in container orchestration
- #28 — declarative workload placement (the placement engine shipped in v0.14.0-alpha; whether that closes it is the owner's call)
- A QuicBaaS tenant agent replaces the Company Agent (roadmap only; no work scheduled)

## Dependencies

- Up (pinned to release tags, `make check-library-pins`): go-xkms v0.49.0-alpha (latest v0.51.0-alpha), go-qrdb v0.30.1-alpha (v0.47.0-alpha), go-quicraft v0.15.0-alpha (v0.21.0-rc.1), go-signal v0.2.0-alpha (`pkg/*` import, a `docs/00` L3 violation), go-libsignal, go-objstore, go-enclave, go-auth, go-truststrap, go-codec, go-frost, arduino-generic.
- Down: go-trusted-platform (pins v0.12.0-alpha), go-recountly.

## Non-goals

- Database, clustering and seal: go-qrdb. Hardware crypto: go-xkms. Provisioning, SDN and node-side attestation: go-trusted-platform.
