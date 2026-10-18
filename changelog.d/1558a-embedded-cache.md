### Fixed

- The embedded transport routes the cache client's verbs under the proto names
  (`GetCacheStats`, `ClearCache`, `GetCacheConfig`, `UpdateCacheConfig`) and
  accepts and returns the SDK cache client's types, so `quicbaas.Cache` calls no
  longer end at `UnknownMethodError` (#1558).
