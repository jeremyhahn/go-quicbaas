### Fixed

- The Go SDK's gRPC transport now serves `quicbaas.IAM.User` `GetByEmail` (through `GetUser`) and `FindByUsername` (through the new `IAMService.GetUserByUsername` RPC), both requiring `users:read`, the grant the QRDBService user reads take; before this both were denied under the default `RequireAuthz`. An empty tenant in `FindByUsername` uses the caller's own tenant (#2658).
