### Added

- **`sdk/go/xkms/backend` re-exports `ErrKeyNotFound` and `ErrKeyAlreadyExists`.**
  They are the go-xkms key backends' own sentinels, so a consumer can match
  a missing key or a taken key ID with `errors.Is` without importing go-xkms
  (#1507).
