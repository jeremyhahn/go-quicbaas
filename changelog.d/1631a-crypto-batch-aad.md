### Security

- Crypto `EncryptBatch` and `DecryptBatch` now bind each item to its own `Context` (additional authenticated data) instead of dropping it: items that share one context are processed in a single batch call, items with differing contexts one by one, and any item failure fails the whole call with no partial results. `EncryptBatch` also reports the key's real version, read the same way single `Encrypt` reads it, instead of a fixed 1; both verbs now read the version from the KMS key metadata (#1631).
