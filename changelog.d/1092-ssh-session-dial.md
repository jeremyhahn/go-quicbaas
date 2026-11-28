### Added

- **The Go SDK's `ssh.Session().Dial` opens SSH sessions with server-held
  keys (go-quicbaas#1092).** It used to report `CodeUnimplemented`. It now
  dials from the caller's own process, as go-xkms does: each signature is
  made on the server through the forward-signing `Keys().Signer`, and the
  host key is checked against the server's pins (`HostKeyScope` and
  `HostKeyOwner`, defaulting to the key's scope and owner). An unpinned host
  or a changed host key refuses the session. The server never carries the
  SSH stream. Host-key checks keep working across key re-exchanges, and
  cancelling `Dial` interrupts a handshake stalled on a silent peer.
  `Console` still reports `CodeUnimplemented`.
