### Fixed

- Self-registration finish and cleanup now take a durable conditional claim on the tenant record, so cleanup on any node cannot delete an account whose registration completed concurrently (#1657).
- IAM storage reads a missing key as not found, so the first create on an empty store no longer answers already exists (#1938).
