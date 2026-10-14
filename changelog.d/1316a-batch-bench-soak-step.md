### Fixed

- **Catalog storage benchmarks build valid paths (go-quicbaas#1316).**
  `BenchmarkPebbleStoragePutEntry` and `BenchmarkPebbleStorageGetEntry`
  built keys with `string(rune(i))`, which appends control characters,
  spaces or slashes, so `PutEntry` refused them with `PATH_INVALID`. The keys
  now use the decimal index. `BenchmarkBatchProcessor_ImportUsers` gets the
  same fix for its generated email addresses.
