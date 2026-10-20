### Fixed

- `make coverage`, the CI test stage, keeps what `make test` gated (#1595,
  Codex review 5785): its `-race` pass takes the same 30-minute per-package
  `RACE_TIMEOUT` as `make race`, it runs the `TEST_CHECKS` validations that
  `make test` runs, and CI runs `make test-sdk-ts`, `make test-frontend` and
  `make test-sdk-js` again after it.
