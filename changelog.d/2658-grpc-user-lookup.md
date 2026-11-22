### Fixed

- The Go SDK's gRPC transport now serves `quicbaas.IAM.User` `GetByEmail` (through `GetUser`) and `FindByUsername` (through the new `IAMService.GetUserByUsername` RPC), both requiring `users:read`, the grant the QRDBService user reads take; before this both were denied under the default `RequireAuthz` (#2658).
