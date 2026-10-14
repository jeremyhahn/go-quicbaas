### Changed

- **A malformed environment override fails the config load (go-quicbaas#185).**
  `config.Loader.Load` (the SDK `NewConfigLoader`) used to drop a
  `QUICBAAS_*` value it could not parse and start on the file or default
  value. A port or count that is not an integer, a duration
  `time.ParseDuration` rejects, or a boolean other than `true`/`1`/`yes`/`on`
  or `false`/`0`/`no`/`off` now returns `*config.InvalidEnvValueError`
  (matching `config.ErrInvalidEnvValue`) naming the variable and value; every
  malformed variable is reported in one error.
