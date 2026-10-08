# go-quicbaas roadmap

> **Status: REFERENCE** · Owner: Software Department · Last verified: 2026-10-08
> Per handbook `software/conventions/roadmap.md`: the Software Department refreshes it; the project agent proposes changes by PR.

## Mission

The only composer: one SDK over the whole stack, plus the application layer it alone owns — agentic orchestration, messaging, RAG, scheduler and placement, service registry, attestation Verifier, IAM/OIDC, tenancy (handbook `docs/00` L1).

## Current milestone

`Public release` milestone (M15): 40 open, 0 closed. Release candidate projected **2027-01-14** through go-signal, or **2026-11-05** if go-signal does not gate it.

## Now

- #1155 — v0.14.0-alpha and `sdk/go` tags cut on 18a5b994 — open since 2026-10-03
- #1142 — SDLC webhook receiver back up — open
- #1149 — go-qrdb pin builds on current go-quicraft — open
- #1150 — every transport inherits the go-qrdb and go-xkms auth chain — open
- #482 — self-registration works and leaks no tenant (critical) — blocked
- #769 — scoped FIPS gate restored on push — open
- #1131 — docs-accuracy batch — handed to QuicBaaS

## Next

- #460, #378, #379 — admin detection and hardware-factor enrollment (critical) — escalated
- #22, #23 — audit signature fail-closed, signing key protected — blocked
- #32 — object storage and dynamic secrets tenant isolation — blocked
- #158 — REST rate limiting on by default — blocked
- #698, #718 — REST listener and IAM race tests stable under load — open
- #714, #716 — clean shutdown (inline init, push hubs) — open
- #723 — secret rotation policies actually rotate — open

## Later

- The owner-level IAM and authz rulings: #301, #329, #338, #348, #361, #392, #400, #402, #416, #422, #502, #509 (all `task:blocked`)
- #195 — go-blockstore volumes in container orchestration
- #28 — declarative workload placement

## Dependencies

- Up: go-xkms v0.49.0-alpha (latest v0.50.0-alpha), go-qrdb v0.30.1-alpha (v0.46.0-alpha), go-quicraft v0.15.0-alpha (v0.20.0), go-signal v0.2.0-alpha (`pkg/*` import, a `docs/00` L3 violation), go-libsignal, go-objstore, go-enclave, go-auth, go-truststrap, go-codec, go-frost, arduino-generic.
- Down: go-trusted-platform (pins v0.12.0-alpha), go-recountly.

## Non-goals

- Database, clustering and seal: go-qrdb. Hardware crypto: go-xkms. Provisioning, SDN and node-side attestation: go-trusted-platform.
