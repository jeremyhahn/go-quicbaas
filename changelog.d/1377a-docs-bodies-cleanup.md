### Changed

- **Design and plan records moved out of `docs/` (go-quicbaas#1377).** The
  built and committed content of `docs/design/` and
  `docs/identity-refactor/PLAN.md` now lives in the owning feature folders.
  Plan-only material is dropped:
  - `docs/design/prompt-sets.md` → `docs/software-factory/prompt-sets.md`;
  - `docs/design/autonomous-control-loop.md` → `docs/orchestration/autonomy-and-dispatch.md`;
  - `docs/design/per-tenant-barrier-broker-api.md` → `docs/barrier/architecture.md`;
  - `docs/design/messaging-roadmap.md` → `docs/messaging/architecture.md`;
  - `docs/design/trusted-service-plane.md` → `docs/service-plane/architecture.md`;
  - `docs/identity-refactor/PLAN.md` → `docs/identity-refactor/architecture.md`;
  - `docs/design/orchestration-remaining-work.md` and
    `docs/design/shard-consolidation.md` are removed: the first was plan-only
    and the second is superseded by the multi-tenancy shard allocation.
- **Docs carry no fleet host names, dated rulings or waivers.** The
  embeddings NPU page names its measurement workstation by hardware class.
  The public-releasability page states the host and path classes by role. The
  SDLC and admission pages state owner rulings as rules.
