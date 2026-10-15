### Added

- **Two runtimes can no longer share a work dir (go-quicbaas#1349).** The work
  dir scopes the owner label the container sweep reaps by, so two runtimes on
  one dir would reap each other's containers. `quicbaas fleet serve` and the
  embedded fleet node (when `embedded_fleet_node.data_dir` is set) now take an
  exclusive `flock` on `<work dir>/runtime.lock` at startup and hold it until
  shutdown; a second runtime refuses to start with `fleet.WorkDirLockedError`,
  which names the dir and the holder's PID. The lock dies with the process, so
  a crash never leaves it stale.
