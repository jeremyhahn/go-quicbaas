### Fixed

- `test/buildgates` builds again. #1744 and #1745 each added a `makeDryRun` helper to the package, so `make vet`, `make lint` and its tests failed with `makeDryRun redeclared`. The lint-scope helper, which dry-runs the repository Makefile with every variable at its default, is now `repoMakeDryRun`; `makeDryRun` stays the stage-tag helper that empties `TEST_CHECKS` (#1764).
