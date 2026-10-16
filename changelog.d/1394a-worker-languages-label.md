### Added

- **The worker image declares its serena languages** in the
  `io.quicbaas.worker.languages` label (`go python typescript bash yaml
  markdown cpp rust ruby`), written once as the Dockerfile's
  `WORKER_LANGUAGES` default. The build fails when a listed language's
  server is not on the PATH, and the worker image refresh refuses a
  candidate whose label is missing, empty or names an unknown language, so
  the factory, which refuses an image without the label, is never handed
  one (#1394).
