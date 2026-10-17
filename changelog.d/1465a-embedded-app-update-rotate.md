### Fixed

- **The embedded transport serves IAM App `Update` and `RotateSecret`
  (go-quicbaas#1465).** The Go SDK sent both verbs, but the embedded dispatch
  table registered only `Register`, `Get`, `List` and `Delete`. Both now call
  the tenant-scoped `AppManager` methods, with the same tenant targeting as
  the other App verbs.
