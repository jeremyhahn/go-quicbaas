### Added

- **The container runtime moved in from go-trusted-platform
  (go-quicbaas#1349).** `pkg/containerruntime` is gtp's `pkg/compute/podman`,
  brought over with its history and tests: the `PodmanManager` lifecycle
  model, attestor hooks, exec, attach and pty, and metrics. It runs only in
  the fleet node agent and is not wired yet; a test fails if `quicbaasd`
  links it. Its exec core is replaced by in-process runc `libcontainer` in a
  later change.
