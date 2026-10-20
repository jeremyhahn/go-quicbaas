### Fixed

- The DEK client's `ReencryptionProgress` now carries `Status` and `EstimatedCompletion` (zero time when the server sends none) from `GetDEKStatus` and `GetReencryptionProgress`; the gRPC and Unix transports restore the progress status from the re-encryption response's status, and the MCP transport decodes both verbs' results instead of failing with an invalid response type (#1641).
