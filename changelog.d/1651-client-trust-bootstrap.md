### Added

- `quicbaas bootstrap` fetches the CA bundle on the client through the SDK: `auto`, `dane`, `noise`, `spki` and `direct` verbs, with flags for each method's material. It replaces a placeholder that always failed (#1651).
- Noise trust bootstrap can use the approved P-256 suite: `--noise-p256-static-key` and `--noise-suite` on the CLI, `noise_p256_static_key` and `noise_suite` on `POST /api/v1/bootstrap/{auto,noise}` and in the TypeScript SDK's `BootstrapBundleRequest`, and the P-256 key and suite symbols re-exported from the Go SDK's `xkms/trust/bootstrap` (#1651).

### Fixed

- A Noise bootstrap suite error (missing or invalid key, no usable suite, suite unavailable in this build) is a 400, not a 500 (#1651).
