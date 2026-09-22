### Fixed

- `quicbaas agent deploy` now verifies the target's SSH host key against the
  pin its own help text told operators to add. The command documented
  `quicbaas ssh known-hosts add` as the production path, but never read the
  known-hosts store: every deploy failed with `invalid spec
  field=ssh_host_key: host-key verification is required`, leaving
  `--ssh-insecure-host-key` as the only working route. The deploy now resolves
  the pin through the same SDK SSH client the `ssh known-hosts` commands use,
  under the same scope/owner as `--ssh-key-scope` / `--ssh-key-owner`, and
  accepts a presented key only when its algorithm and SHA256 fingerprint match
  a pin — several pins for one host (key rotation) all count. An unpinned host
  fails before the dial with the `known-hosts add` command that fixes it; a
  host whose key does not match fails with both fingerprints named.
