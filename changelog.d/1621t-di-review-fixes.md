### Fixed

- The embedded transport refuses a transaction compare with `UnrepresentableTransactionCompareError` before either branch runs: the client's compare carries no entity type, and the service would read its key with an empty entity type and take the wrong branch (#1621).
- Closing a gRPC ObjectTransfer fetch stream now cancels the RPC before returning the pooled connection, so a partly read stream no longer keeps the RPC and a blocked `Recv` alive; a second `Close` stays a no-op (#1621).
