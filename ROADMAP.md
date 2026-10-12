# go-quicbaas roadmap

> **Status: REFERENCE** · Last verified: 2026-10-12
> Per handbook `software/conventions/roadmap.md`: the Software Department refreshes it; the project agent proposes changes by PR.
> This is the only roadmap. [docs/archive/ROADMAP-2026-08.md](docs/archive/ROADMAP-2026-08.md) is ARCHIVE, not a plan.

## Mission

The only composer: one SDK over the whole stack, plus the application layer it alone owns — agentic orchestration, messaging, RAG, scheduler and placement, service registry, attestation Verifier, IAM/OIDC, tenancy (handbook `docs/00` L1).

What has shipped is in [CHANGELOG.md](CHANGELOG.md). Feature specifications are indexed in [docs/README.md](docs/README.md).

## Current milestone

The current milestone first, then every other open milestone in delivery order.

- [Release candidate](../../milestone/15) — target: release candidate — date: not yet projected
- [Deployed on raft-1/2/3](../../milestone/32) — target: deployment to the raft-1/2/3 cluster — date: 2026-10-30
- [Local Software Factory on QuicBaaS](../../milestone/33) — target: the local software factory running on QuicBaaS — date: 2026-11-06

## Now

Each line: issue, outcome, the `docs/<feature>/` folder that specifies it, status.

- [#144](../../issues/144) — an embedded-transport stream Close no longer races a handler send — [docs/go-sdk/](docs/go-sdk/) — in review
- [#158](../../issues/158) — REST rate limiting on by default, with an opt-out — [docs/multiprotocol-api/](docs/multiprotocol-api/) — in review
- [#259](../../issues/259) — SDK session verbs on `/api/v1/iam` with a per-request bearer — [docs/iam/](docs/iam/) — in review
- [#335](../../issues/335) — invalid IAM fields are refused as invalid arguments on every transport — [docs/iam/](docs/iam/) — in review
- [#358](../../issues/358) — self-service registration through invite activation on every transport — [docs/iam/](docs/iam/) — in review
- [#369](../../issues/369) — the web UI reconciles the session identity before it attaches a bearer — [docs/desktop-gui/](docs/desktop-gui/) — in review
- [#376](../../issues/376) — the SDK resolves a user from a client certificate under `Security().Auth()` — [docs/iam/](docs/iam/) — in review
- [#560](../../issues/560) — `sdk/go/xkms` re-exports the ACME http-01 responder and the SSH fingerprint validator — [docs/go-sdk/](docs/go-sdk/) — in review
- [#680](../../issues/680), [#700](../../issues/700) — fleet listings are tenant-scoped and rendered by one served-backends renderer — [docs/software-factory/](docs/software-factory/) — in review

## Next

Blocked on an owner decision:

- [#22](../../issues/22), [#23](../../issues/23) — audit signatures fail closed and the signing key is protected — [docs/audit-logging/](docs/audit-logging/) — blocked: owner decision
- [#338](../../issues/338), [#348](../../issues/348), [#392](../../issues/392), [#502](../../issues/502) — IAM and authorization rulings — [docs/iam/](docs/iam/), [docs/authorization/](docs/authorization/) — blocked: owner decision
- [#723](../../issues/723) — secret rotation policies actually rotate — [docs/secrets/](docs/secrets/) — blocked: owner decision
- [#200](../../issues/200), [#201](../../issues/201), [#477](../../issues/477), [#524](../../issues/524), [#637](../../issues/637) — outcome and folder stated in each issue — blocked: owner decision

Blocked for other reasons:

- [#800](../../issues/800) — PIV attestation trust settings reach the go-xkms module route — [docs/iam/](docs/iam/) — blocked: see issue
- [#1127](../../issues/1127) — the public MCP tool set matches its specification and is guarded — [docs/mcp-server/](docs/mcp-server/) — blocked: see issue
- [#769](../../issues/769) — the scoped FIPS gate runs on push again — [docs/security-hardening/](docs/security-hardening/) — blocked: see issue
- [#1130](../../issues/1130) — the published tree carries no private host, path or lab value — [docs/release/](docs/release/) — blocked: see issue
- [#718](../../issues/718) — REST listener and IAM race tests are stable under load — [docs/testing/](docs/testing/) — blocked: see issue
- [#1258](../../issues/1258) — every `docs/` folder meets the handbook documentation rules — [docs/README.md](docs/README.md) — blocked: see issue
- [#486](../../issues/486), [#1168](../../issues/1168) — outcome and folder stated in each issue — blocked: see issue

## Later

- [#195](../../issues/195) — go-blockstore volumes in container orchestration — [docs/software-factory/](docs/software-factory/)
- [#28](../../issues/28) — declarative workload placement — [docs/scheduler/](docs/scheduler/)

## Dependencies

- Up (pinned to release tags, `make check-library-pins`): go-xkms v0.49.0-alpha, go-qrdb v0.30.1-alpha, go-quicraft v0.15.0-alpha, go-signal v0.2.0-alpha (`pkg/*` import, a `docs/00` L3 violation), go-libsignal, go-objstore, go-enclave, go-auth, go-truststrap, go-codec, go-frost, arduino-generic.
- Down: go-trusted-platform (pins v0.12.0-alpha), go-recountly.

## Non-goals

- Database, clustering and seal: go-qrdb. Hardware crypto: go-xkms. Provisioning, SDN and node-side attestation: go-trusted-platform (handbook `docs/00` §R).
