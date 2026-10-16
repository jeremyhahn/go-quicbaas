### Added

- **Design note for fleet placement, drift and actuation** (go-quicbaas#761).
  `docs/agent-fleet/placement.md` maps the archived wp13 placement and wp14 actuation commits
  onto today's fleet registry, host affinity and capacity admission. It covers
  the desired-state model, the drift report's refusal rules, actuation that is
  automatic for fungible work and needs per-move consent for everything else,
  the salvage slices, and the open questions. Documentation only. No code
  changes.
