### Fixed

- **The embedded transport routes the quicbaas.IAM GDPR verbs** (#1554).
  The SDK IAM client sends `GrantConsent`, `WithdrawConsent`, `GetConsent`,
  `ListConsents`, `RequestExport`, `GetExportStatus`, `GenerateExport`,
  `RequestDeletion`, `CancelDeletion` and `GetDeletionStatus` to
  `quicbaas.IAM.GDPR`, and the embedded transport answered each with
  `UnknownMethodError`. Each verb now calls the `iam.GDPRManager` that the
  REST and gRPC servers call. As on gRPC, a request naming a tenant other than
  the bound one is refused before the manager is reached. As on REST, an export
  or erasure of another tenant or data subject is not found, and only a
  completed export is generated. An erasure with no scheduled date keeps the
  manager's default grace period; a date that is not in the future is refused.
