### Changed

- **The JavaScript scheduler integration test fails when no server is
  reachable.** `skipIfNoServer` is gone: `beforeAll` throws
  `ServerUnreachableError` naming `QUICBAAS_URL`. The suite now runs in the
  jest `node` environment. Under jsdom the old probe could never succeed
  (`AbortSignal.timeout` is missing), so not a single case ever ran.
- **`make integration-test-sdk-js[-<pkg>]` runs in Docker against a
  standalone server.** It builds the image unless `QUICBAAS_IMAGE` is
  supplied, starts `central` from
  `sdk/javascript/test/integration/docker-compose.yml`, and runs jest in a
  `node:22-bookworm-slim` container with `QUICBAAS_URL=https://central:8080`.
  Nothing runs on the host.
- **The JS integration compose starts a configured server.** `central` runs
  `quicbaasd start` on `sdk/javascript/test/integration/quicbaas.yaml`
  (REST only, scheduler on, anonymous dev posture), mounted read-only. The
  `QUICBAAS_MODE` / `QUICBAAS_BIND` variables configured nothing and are
  gone. The health check uses busybox `wget`, because the runtime image has
  no `curl`. Jest trusts the daemon's CA chain from its data volume.
