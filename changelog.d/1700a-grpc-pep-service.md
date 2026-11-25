### Fixed

- The node's gRPC port serves PEPService. Check and CheckBatch take permissions:resolve, the grant their REST routes take. CheckBatch evaluates in the caller's own tenant and refuses any other tenant. Subscribe stays denied (#1700).
