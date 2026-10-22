### Changed

- CI cancels superseded pull request runs with a literal
  `cancel-in-progress: true`, and each main commit runs in its own group so
  a main run is never cancelled (#1762).
- A pull request that changes only documentation (docs/, changelog.d/, a
  top-level `*.md` or any `README.md`) builds the toolchain image alone and
  runs lint and CI Gate; build, tests, security scans and integration are
  skipped (#1762).
- The docs-only filter is `scripts/docs-only.sh`, and test/buildgates covers
  its classification (mixed changes, embedded Markdown, renames between docs
  and code, a failed diff, push and dispatch events) and the job wiring that
  skips only those stages (#1762).
