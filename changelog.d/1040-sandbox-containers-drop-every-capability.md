### Security

- **Sandboxed agent containers drop every capability and have a pids limit** (#1040).
  Every worker, reviewer and triage container now starts with `--cap-drop ALL`.
  Only `CHOWN`, `DAC_OVERRIDE`, `FOWNER`, `SETGID` and `SETUID` are added back,
  which is what `sudo` and `apk` need. Each container also gets a
  `--pids-limit` (default 4096, counting threads). This covers the embedded
  agent and remote `quicbaas agent serve`, on the CLI, JSON-RPC and exec
  transports, the orchestrator's detached sandbox and the tool container.
  Passwordless `sudo` and a writable rootfs are kept, so root inside a worker
  still installs packages. It no longer holds `NET_RAW`, `MKNOD`, `SETFCAP` or
  the rest of Docker's default set. A caller's `ContainerConfig` cannot widen
  the policy. seccomp and AppArmor defaults stay in force.
  `ContainerSpec.PidsLimit` is measured by `ContainerConfigDigest`.
- Operators configure the policy with `sandbox_cap_add` / `sandbox_pids_limit`
  in the embedded-agent and remote agent configs, or with
  `--sandbox-cap-add` / `--sandbox-pids-limit` (`QUICBAAS_SANDBOX_CAP_ADD` /
  `QUICBAAS_SANDBOX_PIDS_LIMIT` in the agent-daemon unit and image). A policy
  that cannot be applied is refused at startup.
- `make test-worker-image` checks the image under the production flags,
  which it gets from `scripts/sandboxargs`. It asserts that `sudo apk add`
  works, that root's `CapEff` has no `NET_RAW` or `MKNOD`, and that a fork
  past the pids limit fails cleanly.
