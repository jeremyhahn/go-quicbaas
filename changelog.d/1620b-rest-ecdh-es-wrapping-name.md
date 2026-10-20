### Fixed

- The Go SDK REST transport now sends the RFC 7518 §4.6 name `ECDH-ES+A256KW` for the KMS ECDH-ES wrapping algorithm, as the gRPC transport does. It used to send `ECDH-ES-A256KW`, which the service refuses, so KMS ExportKey and UnwrapKey with ECDH-ES failed over REST (#1620).
