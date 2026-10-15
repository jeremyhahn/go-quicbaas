### Added

- **In-process container runtime core** (go-quicbaas#1349). `pkg/containerruntime`
  now runs containers with runc libcontainer v1.5.2 in-process, pulls images
  with go.podman.io/image/v5 (honouring the signature policy and digest pins)
  and unpacks root filesystems with go.podman.io/storage under the runtime's
  work directory. No external binaries or sockets are used.
- **Container init entry point** (go-quicbaas#1349). The agent's `main` calls
  `containerruntime.RunContainerInit` first on `linux && cgo` builds, so
  libcontainer can re-execute the agent as the container init through nsenter.
- **Make targets** (go-quicbaas#1349): `test-containerruntime`,
  `coverage-containerruntime` and `bench-containerruntime`.
- **Container runtime integration test** (go-quicbaas#1349).
  `make integration-test-containerruntime` runs `test/integration/containerruntime`
  in a privileged container against a local registry: containers that exit 0
  and 5, logs, stop, forced delete, digest pins, and that delete removes the
  root filesystem.

### Changed

- **No shell-outs from the container runtime** (go-quicbaas#1349). The crun,
  podman and skopeo exec paths are gone, and `RuntimeConfig.OCIRuntime` is
  removed. A test fails if a non-test file in the package imports `os/exec`.
  Builds without linux and cgo return `ErrRuntimeRequiresLinuxCgo`.
- **Container runtime fixes** (go-quicbaas#1349). Exit codes are the
  process's real ones (128 plus the signal for a killed process); Stop and
  Wait no longer poll with sleeps; `ContainerConfig.User` is applied; a
  digest-pinned image must resolve to its pin at pull and at create.
