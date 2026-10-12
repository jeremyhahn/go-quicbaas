### Added

- **Prompts are tenant-scoped, versioned records (go-quicbaas#804).**
  `pkg/orchestration/prompts` gains a `Store` over each tenant's qrdb store
  (compare-and-swap writes, inline history bounded at 20 revisions, rollback
  and reset as new revisions, write audit with identifiers only), a
  `Resolver` that answers project → tenant → system → embedded while skipping
  unedited `follow_parent` copies, and a `Seeder`. Boot reconciles the system
  set onto the build's embedded defaults and seeds any tenant's missing
  prompts without touching an edit; creating a tenant copies the system set
  and removes the copy if creation rolls back. Each job resolves its set once
  at launch, refuses to launch when that fails, and records the versions it
  ran with in `JobStatus.Prompts` and a `prompts:resolved` audit event; the
  SDLC issue source resolves once per poll. The `prompts:read`,
  `prompts:write` and `prompts:history` permissions are registered for
  `admin` and `operator` (history also `auditor`). Transports, the GUI and
  the per-role charters follow in #805 to #807.
