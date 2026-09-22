### Fixed

- **The worker image carries rsync.** `make build` copies the frontend tree
  with rsync before embedding it; the image did not ship it, so every
  go-quicbaas verification that reached a frontend rebuild failed with
  `rsync: command not found` and the rounds were charged to the work
  (go-quicbaas#62 exhausted its attempt budget on it, 2026-09-21). The
  image installs rsync and `make test-worker-image` probes for it.
