### Fixed

- gRPC PEP `Check` carries the request tenant: the SDK's gRPC transport sends the `CheckRequest`'s tenant on the wire, and the server evaluates the check in the caller's bound tenant, refusing any other tenant with PermissionDenied (#2663).
