### Fixed

- KMS `GenerateKey` passes the request's `Exportable` flag to go-xkms, so an
  exportable symmetric key can be exported and a non-exportable one is refused
  with go-xkms's typed `ErrBackendKeyNotExportable`. A symmetric key generated
  with no key type now defaults to `KeyTypeSecret`, the only type go-xkms's
  symmetric provider lists, and the go-xkms KMS backend generates and addresses
  every symmetric key under that type (`backend:secret:algo:name`) (#1768).
