### Changed

- **A malformed daemon environment value fails `quicbaasd` startup
  (go-quicbaas#185).** The variables `quicbaasd` parses itself used to be
  dropped (`QUICBAAS_SERVER_{WRITE,READ,IDLE}_TIMEOUT`), read as false
  (`QUICBAAS_ENABLE_SCHEDULER`, `QUICBAAS_{TIMESERIES,VECTOR,BACKUP}_ENABLED`
  with any value outside `true`/`1`/`yes`/`on`), or exit the process from
  inside a helper (`QUICBAAS_SERVER_SHARDS`,
  `QUICBAAS_SERVER_ENTITY_SHARD_PINS`). Each now fails startup as an invalid
  configuration with `*config.InvalidEnvValueError` naming the variable and
  value, using the loader's rules: booleans accept `true`/`1`/`yes`/`on` and
  `false`/`0`/`no`/`off` in any case. `config.EnvBool` and
  `config.EnvDuration` are exported for this.
