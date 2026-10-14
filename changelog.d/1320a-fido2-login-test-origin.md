### Fixed

- **The FIDO2 login integration tests pass a WebAuthn origin (go-quicbaas#1320).**
  go-xkms's software authenticator refuses a ceremony without an origin, so
  passkey registration and assertion in `test/integration/login` failed before
  the server saw them. Every ceremony in the suite now uses `https://localhost`,
  which the integration server's relying party accepts.
