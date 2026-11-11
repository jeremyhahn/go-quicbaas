### Fixed

- **GenerateKey carries its metadata and never leaves a key without it
  (go-quicbaas#2213).** The SDK `kms.GenerateKeyRequest` now carries
  `Description`, `Tags` and `Owner` over every transport, so a consumer no
  longer sets them with a second call. When the key's metadata cannot be
  stored, `GenerateKey` now deletes the new key and returns the error;
  before, it logged a warning and returned success with the metadata lost.
  The rollback also removes the rotation policy the failed call stored, so
  a retry of the same name does not inherit it.
