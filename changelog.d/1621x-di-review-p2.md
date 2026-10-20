### Fixed

- The embedded transport returns `TransactionOperationFailedError`, naming the operation index and the service's error text, when a transaction operation fails; it no longer reports the transaction as a success with an empty value (#1621).
- The gRPC federated login callback fills `Username` from the user's username, not its display name, matching the embedded transport (#1621).
- The gRPC `ListProviders` fetches every page of providers instead of only the first 100; a failed page fails the call rather than returning a partial list (#1621).
