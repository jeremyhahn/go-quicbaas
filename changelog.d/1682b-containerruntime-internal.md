### Changed

- The in-process container runtime is internal (#1682). It moved from
  `pkg/containerruntime` to `internal/containerruntime`, so code outside
  this module cannot import it, and `pkg/fleet` no longer exports
  `InProcessContainerRuntime`, `SetInProcessContainerRuntime`,
  `ErrInProcessRuntimeUnavailable` or `ErrInProcessRuntimeNoRoot`. The
  fleet node agent still runs jobs through it; no CLI flag or
  configuration value selects it.
