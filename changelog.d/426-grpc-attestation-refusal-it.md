### Changed

- **The attestation-metadata refusal is pinned on the gRPC wire
  (go-quicbaas#426).** A new integration test in `test/integration/login`
  dials `qrdb.v1.UserAuthFactorService` on the quicbaas gRPC listener through
  the go-qrdb SDK. It asserts that an `Add` or `Rotate` asserting
  `attest_level` or `attest_aaguid` fails with `InvalidArgument` and a message
  naming the key, that a refused `Rotate` leaves the old factor active, and
  that a factor without reserved metadata reads back with `attest_level=none`.
  A go-qrdb bump that turned the refusal into `Internal` now fails CI.
