### Fixed

- **SSH sessions with server-held RSA keys sign with SHA-2 (go-quicbaas#1674).**
  The SDK's forward signer now signs with the algorithm the SSH server
  asks for (rsa-sha2-256 or rsa-sha2-512) instead of always `ssh-rsa`
  (SHA-1), which current OpenSSH servers refuse. The sign call takes an
  optional `algorithm` on every transport; one that does not fit the key
  is refused as invalid argument.
