### Added

- **Every address key is settable by file, env and flag (go-quicbaas#185).**
  `quicbaasd start`, `node` and `cluster` take `--host`,
  `--raft-enrollment-acme-challenge-port`,
  `--raft-enrollment-advertise-noise-port`, `--rag-embedding-api-url`,
  `--cache-l2-redis-addr` and `--discovery-multicast-group`. The matching
  `QUICBAAS_*` environment names (the config key with `.` as `_`) now apply
  on both the daemon and the `pkg/config` loader. Precedence is flag > env >
  file > defaults.

### Changed

- Configuration validation rejects a
  `raft.raft_enrollment.acme.challenge_port` that is not a number from 1 to
  65535, a `raft.raft_enrollment.advertise_noise_port` outside 0 to 65535,
  and a `discovery.multicast.group` that is not a multicast IP.
