### Fixed

- The embedded transport routes the six `quicbaas.IAM.MFA` verbs the SDK IAM
  client sends (GetMFAStatus, BeginMFAEnrollment, CompleteMFAEnrollment,
  DisableMFA, GetBackupCodeStatus, GenerateBackupCodes) to the MFA manager the
  REST and gRPC transports call, with the gRPC tenant check, instead of
  failing with an unknown method (#1554).
