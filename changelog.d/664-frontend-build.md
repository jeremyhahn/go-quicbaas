### Fixed

- **The frontend build is reproducible (go-quicbaas#664).** `make
  build-frontend` installs with `npm ci` from the tracked
  `cmd/quicbaas/frontend/package-lock.json` and never rewrites it. Before,
  it ran `npm install` with the lock excluded. The build root mirrors the
  repository layout, so the lock's `file:../../../sdk/typescript`
  dependencies resolve outside the checkout. Its suffix is a hash of the
  checkout path, so concurrent worktrees no longer share one build
  directory. `make test-frontend` now runs a new `make install-frontend`
  first, so it works in a fresh checkout. The dead `_build-frontend` target
  is removed, and the tracked SDK and frontend `dist/` trees are regenerated
  from current source.
