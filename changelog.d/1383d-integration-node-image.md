### Changed

- **The integration stack's nodes run the hardened `integration` image
  (#1383).** The devcontainer's `integration` stage is now the node image:
  `base` plus the `quicbaasd` and `quicbaas` binaries, which the build-only
  `integration-build` stage compiles with `Dockerfile.node`'s tags and flags,
  together with the node entrypoint and config. In
  `test/integration/docker-compose.integration.yml`, `test-node1` to
  `test-node5` and `test-selfreg` run `${QUICBAAS_INTEGRATION_IMAGE}`. Each
  node is read-only with a `/tmp` tmpfs, has every capability dropped (nodes 1
  to 5 add back `NET_ADMIN` and `NET_RAW` for multicast), runs with
  `no-new-privileges`, and has the devcontainer ulimits.
  `make integration-test` and `make integration-bench` build the image first,
  through the new `make docker-build-integration-image`, and export its tag. The
  test driver and the other compose files keep their own Dockerfiles.
