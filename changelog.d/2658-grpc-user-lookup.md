### Fixed

- The Go SDK's gRPC transport now serves `quicbaas.IAM.User` `GetByEmail` (through `GetUser`) and `FindByUsername` (through the new `IAMService.GetUserByUsername` RPC), with the same authorization as `GetUser` (#2658).
