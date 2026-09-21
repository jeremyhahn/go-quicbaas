### Added

- **`sdlc:verification` audit event per verification command** — its name,
  whether it passed, its output size and its first line of stderr — so a
  job that dies at verification says what make said. Until now the job
  carried the turn, the checkout and the teardown but not that.
