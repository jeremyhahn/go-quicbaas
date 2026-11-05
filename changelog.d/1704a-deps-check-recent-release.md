### Changed

- `make deps-check` reports a direct requirement behind a release younger than 7 days, with its versions, the release time and its age, and does not fail on it; a requirement behind a release 7 days or older, or one whose release time the go command does not give, still fails unless go.mod carries a reason for it (#1704).
