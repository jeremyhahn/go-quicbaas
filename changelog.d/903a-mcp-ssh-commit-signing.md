### Added

- **`quicbaas-git-sign`, git's `gpg.ssh.program` over MCP
  (go-quicbaas#903).** The new helper (`make build-git-sign`) signs commits
  with the session's key held in quicbaas. It reaches the key through the
  session's own MCP client certificate (`quicbaas_ssh_key_export` and
  `quicbaas_ssh_key_sign`), so it never holds private key material. It builds
  and armors the SSHSIG itself, and verifies the signature locally before
  writing it. It accepts only `-Y sign -n git -f <key>`; any other invocation
  is a typed `ErrInvalidInvocation` and writes no signature. A signing
  session that carries a helper (`sdlc.SigningHelper`) has
  `gpg.ssh.program` set on the commit and the train merge, and only those
  commands receive the helper's environment. The agent socket is unchanged.
- **Every SSH signature is audited.** `SignDigest` records an `ssh.key.sign`
  event on every transport, for success and failure alike. The event names
  the calling principal, its tenant and the key.

### Notes

- An agent session's floor does not include `keys:get` or `keys:sign`, so
  until signing sessions are granted those permissions the helper is refused
  with `*rbac.PermissionDeniedError`.
