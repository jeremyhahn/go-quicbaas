### Added

- **`quicbaasd` installs a process-wide locked-memory budget at startup
  (go-quicbaas#1010).** New setting `server.secure_memory_budget_mib`
  (`QUICBAAS_SERVER_SECURE_MEMORY_BUDGET_MIB`): `0`, the default, derives the
  budget from the host (64 MiB, or `RLIMIT_MEMLOCK` less 2 MiB of headroom when
  that is smaller); a positive value is used as given. The daemon, and
  `quicbaasd init`, refuse to start with a typed `SecureMemoryBudgetError` when
  the budget is negative, exceeds `RLIMIT_MEMLOCK`, or cannot be derived.

### Changed

- **SDLC webhook secrets are wiped with the SDK's `secure.Wipe`
  (go-quicbaas#1010)** instead of a hand-rolled loop the compiler could drop.
