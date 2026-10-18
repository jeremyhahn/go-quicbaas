### Fixed

- The embedded transport routes `quicbaas.IAM.Policy/Simulate` and
  `quicbaas.IAM.PEP/CheckBatch` to the policy engine the gRPC and REST
  transports already call, in the tenant the call is bound to; a request
  naming another tenant is refused (#1564).
