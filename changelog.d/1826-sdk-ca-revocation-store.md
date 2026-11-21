### Added

- Go SDK `xkms/ca`: `RevocationStore` names the type of the CA's required `Params.RevocationStore`, the durable go-xkms storage backend that holds revocations and the CRL, so an SDK consumer can supply it without importing go-xkms (#1826).
