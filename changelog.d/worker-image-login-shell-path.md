### Fixed

- **The worker image's Go toolchain was unreachable to every command the
  factory ran.** The agent execs tools as `sh -lc`, and a login shell sources
  `/etc/profile`, whose first line on the base image unconditionally overwrites
  `PATH` — discarding the `ENV PATH` that put `/usr/local/go/bin` on it.
  Measured on the image: `sh -c "command -v go"` resolved it, `sh -lc` did not.
  A `/etc/profile.d` entry now restores it, asserted at build time.

  The symptom was nothing like the cause. No missing-toolchain error was
  reported anywhere: Serena's Go language server simply failed to start, the
  reviewer lost symbol and reference lookup and returned a `comment` verdict
  saying it could not complete the review, and because a comment is neither an
  approval nor a request for changes the gate stayed at
  `insufficient_approvals`. The delivery job then waited in `reviewing` for a
  verdict that could never change and the stall detector ended it thirty
  minutes later, logging `job stalled` — which points at the agent. Five jobs
  died that way.

- **The image gate probed a shell the factory never uses**, which is why it
  stayed green throughout. Its checks ran under `sh -euc`, not a login shell,
  so they asserted a `PATH` production never saw and certified a toolchain that
  no real invocation could reach. Every probe now runs `sh -lec`.
