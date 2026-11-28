### Fixed

- The embedded transport now serves the unary ObjectTransfer `FetchObject` verb instead of refusing it as an unknown method. The shard, object reference, checksum and request id reach the service unchanged; every response field, including `Found` and `ErrorMessage`, reaches the client; service errors keep their type; and a service that returns no response is reported as the typed `EmptyServiceResponseError` rather than read as `Found: false` (#1562).
