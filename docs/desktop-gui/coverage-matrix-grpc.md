# Console coverage matrix: gRPC RPCs

This matrix records, for each of the 553 RPCs in the 41 services under `api/proto/`, whether the desktop console
(`cmd/quicbaas`) reaches it. A file named in a `covered` cell calls code that invokes the operation: a view,
component, lib or store file under `cmd/quicbaas/frontend/src/` (paths are written from there, for example
`views/security/KMSView.svelte`), or a Go desktop binding `cmd/quicbaas/bindings_*.go`. The TypeScript SDK's
gRPC-web transport (`sdk/typescript/src/grpc_web.ts`) is a stub whose methods throw "not implemented", so the console
reaches these operations through the SDK's REST or Wails transports, on the REST route for the same operation; a cell
counts as covered when the SDK method the file calls is that operation. The marks are:

- `covered: <file>`: the named console file calls the operation.
- `not covered`: a console user would use this operation, but no console code reaches it.
- `not-UI: <reason>`: the RPC is not meant for a console (cluster-internal, node runtime or agent runtime traffic).
  Whole services are decided once; a few internal RPCs of mixed services are marked one by one.
- `unaudited`: not yet decided.

## quicbaas.account.v1.AccountService

| RPC | Proto file | Console |
|---|---|---|
| CreateAccount | api/proto/account.proto | covered: views/orchestrator/AccountsView.svelte |
| GetAccount | api/proto/account.proto | not covered |
| ListAccounts | api/proto/account.proto | covered: views/orchestrator/AccountsView.svelte |
| UpdateAccount | api/proto/account.proto | covered: components/orchestrator/AccountEditDialog.svelte |
| DeleteAccount | api/proto/account.proto | covered: views/orchestrator/AccountsView.svelte |
| RotateAccountCredential | api/proto/account.proto | covered: views/orchestrator/AccountsView.svelte |
| RevokeAccount | api/proto/account.proto | covered: views/orchestrator/AccountsView.svelte |
| SetDefaultAccount | api/proto/account.proto | covered: views/orchestrator/AccountsView.svelte |
| GetAccountUsage | api/proto/account.proto | covered: views/orchestrator/AccountsView.svelte |
| MaterializeAccountCredential | api/proto/account.proto | not-UI: the lane runtime materializes a credential for a session |
| ReserveAccountSession | api/proto/account.proto | not-UI: the orchestrator's session accounting |
| ReleaseAccountSession | api/proto/account.proto | not-UI: the orchestrator's session accounting |

## quicbaas.agents.v1.AgentCatalogService

| RPC | Proto file | Console |
|---|---|---|
| ListAgents | api/proto/agents.proto | covered: components/orchestrator/StartJobDialog.svelte |
| GetAgent | api/proto/agents.proto | covered: components/orchestrator/AgentSpendLimitsEditor.svelte |
| CreateAgent | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |
| UpdateAgent | api/proto/agents.proto | covered: components/orchestrator/AgentSpendLimitsEditor.svelte |
| DeleteAgent | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |
| AgentUsedBy | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |
| ListAgentPools | api/proto/agents.proto | covered: components/orchestrator/StartJobDialog.svelte |
| GetAgentPool | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |
| CreateAgentPool | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |
| UpdateAgentPool | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |
| DeleteAgentPool | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |
| AgentPoolUsedBy | api/proto/agents.proto | covered: cmd/quicbaas/bindings_agent_catalog.go |

## quicbaas.attestation.v1.AttestationService

Service: not-UI, machine attestation of nodes and containers, called by nodes and the verifier.

| RPC | Proto file | Console |
|---|---|---|
| Verify | api/proto/attestation.proto | not-UI: machine attestation |
| VerifyChain | api/proto/attestation.proto | not-UI: machine attestation |
| AttestContainer | api/proto/attestation.proto | not-UI: machine attestation |
| PutEndorsement | api/proto/attestation.proto | not-UI: machine attestation |
| ListEndorsements | api/proto/attestation.proto | not-UI: machine attestation |
| DeleteEndorsement | api/proto/attestation.proto | not-UI: machine attestation |
| PutReferenceValues | api/proto/attestation.proto | not-UI: machine attestation |
| GetReferenceValues | api/proto/attestation.proto | not-UI: machine attestation |
| ListReferenceValues | api/proto/attestation.proto | not-UI: machine attestation |
| DeleteReferenceValues | api/proto/attestation.proto | not-UI: machine attestation |
| AppraiseQuote | api/proto/attestation.proto | not-UI: machine attestation |

## quicbaas.barrier.v1.Barrier

| RPC | Proto file | Console |
|---|---|---|
| BarrierInitialize | api/proto/barrier.proto | covered: views/security/BarrierView.svelte |
| BarrierUnseal | api/proto/barrier.proto | not covered |
| BarrierSeal | api/proto/barrier.proto | not covered |
| BarrierStatus | api/proto/barrier.proto | covered: views/security/BarrierView.svelte |
| BarrierShamirInitialize | api/proto/barrier.proto | covered: views/security/BarrierView.svelte |
| BarrierShamirUnsealShare | api/proto/barrier.proto | covered: views/security/BarrierView.svelte |
| BarrierShamirUnsealShares | api/proto/barrier.proto | not covered |
| BarrierRekey | api/proto/barrier.proto | not covered |
| BarrierRecoveryGenerate | api/proto/barrier.proto | not covered |
| BarrierRecoveryRecover | api/proto/barrier.proto | not covered |
| BarrierRecoveryDelete | api/proto/barrier.proto | not covered |
| BarrierRecoveryStatus | api/proto/barrier.proto | not covered |
| BarrierRootToken | api/proto/barrier.proto | not covered |
| BarrierEnableEncryption | api/proto/barrier.proto | not covered |

## quicbaas.ceremony.v1.Ceremony

| RPC | Proto file | Console |
|---|---|---|
| CeremonyInitialize | api/proto/ceremony.proto | not covered |
| CeremonyStatus | api/proto/ceremony.proto | not covered |
| CeremonyClaimCertBegin | api/proto/ceremony.proto | not covered |
| CeremonyClaimCertComplete | api/proto/ceremony.proto | covered: views/security/CeremonyView.svelte |
| CeremonyBeginClaimShare | api/proto/ceremony.proto | covered: views/security/CeremonyView.svelte |
| CeremonyCompleteClaimShare | api/proto/ceremony.proto | covered: views/security/CeremonyView.svelte |
| CeremonySignCSR | api/proto/ceremony.proto | not covered |
| CeremonyListOfficers | api/proto/ceremony.proto | covered: views/security/CeremonyView.svelte |
| CeremonyEnrollExternalOfficer | api/proto/ceremony.proto | covered: views/security/CeremonyView.svelte |

## quicbaas.certificate.v1.CertificateService

| RPC | Proto file | Console |
|---|---|---|
| IssueCertificate | api/proto/certificate.proto | covered: views/security/CertificatesView.svelte |
| SignCSR | api/proto/certificate.proto | covered: views/security/CertificatesView.svelte |
| RevokeCertificate | api/proto/certificate.proto | covered: views/security/CertificatesView.svelte |
| VerifyCertificate | api/proto/certificate.proto | covered: views/security/CertificatesView.svelte |
| GetCACertificate | api/proto/certificate.proto | covered: views/security/CertificatesView.svelte |
| GetTLSConfig | api/proto/certificate.proto | not covered |
| CrossSign | api/proto/certificate.proto | not covered |
| Chain | api/proto/certificate.proto | not covered |
| IssueDeviceIdentity | api/proto/certificate.proto | not covered |

## quicbaas.certificate.v1.TrustService

| RPC | Proto file | Console |
|---|---|---|
| CrossSignedRootPool | api/proto/certificate.proto | covered: views/security/TrustView.svelte |
| AutoImportAIA | api/proto/certificate.proto | covered: views/security/TrustView.svelte |
| PublishDANE | api/proto/certificate.proto | covered: views/security/TrustView.svelte |

## quicbaas.dynamicsecrets.v1.DynamicSecretsService

| RPC | Proto file | Console |
|---|---|---|
| CreateRole | api/proto/dynamic_secrets.proto | covered: views/security/DynamicSecretsView.svelte |
| GetRole | api/proto/dynamic_secrets.proto | not covered |
| ListRoles | api/proto/dynamic_secrets.proto | covered: views/security/DynamicSecretsView.svelte |
| DeleteRole | api/proto/dynamic_secrets.proto | covered: views/security/DynamicSecretsView.svelte |
| GenerateCredential | api/proto/dynamic_secrets.proto | covered: views/security/DynamicSecretsView.svelte |
| RenewLease | api/proto/dynamic_secrets.proto | covered: views/security/DynamicSecretsView.svelte |
| RevokeLease | api/proto/dynamic_secrets.proto | covered: views/security/DynamicSecretsView.svelte |
| ListLeases | api/proto/dynamic_secrets.proto | covered: views/security/DynamicSecretsView.svelte |

## quicbaas.embedding.v1.EmbeddingService

| RPC | Proto file | Console |
|---|---|---|
| EmbeddingGenerate | api/proto/embedding.proto | covered: views/ai/EmbeddingsView.svelte |
| EmbeddingBatchGenerate | api/proto/embedding.proto | not covered |
| EmbeddingListProviders | api/proto/embedding.proto | covered: views/ai/EmbeddingsView.svelte |
| EmbeddingGetProviderInfo | api/proto/embedding.proto | not covered |
| EmbeddingSetDefaultProvider | api/proto/embedding.proto | covered: views/ai/EmbeddingsView.svelte |
| EmbeddingGetCacheStats | api/proto/embedding.proto | not covered |
| EmbeddingClearCache | api/proto/embedding.proto | not covered |

## quicbaas.fido2.v1.FIDO2Service

| RPC | Proto file | Console |
|---|---|---|
| BeginRegistration | api/proto/fido2.proto | covered: views/orchestrator/RegisterView.svelte |
| FinishRegistration | api/proto/fido2.proto | covered: views/orchestrator/RegisterView.svelte |
| BeginAddCredential | api/proto/fido2.proto | not covered |
| BeginAuthentication | api/proto/fido2.proto | not covered |
| FinishAuthentication | api/proto/fido2.proto | not covered |

## quicbaas.fleet.runtime.v1.FleetRuntime

Service: not-UI, the fleet node's agent runtime channel to the server.

| RPC | Proto file | Console |
|---|---|---|
| Enroll | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| Spawn | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| Prompt | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| Status | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| Close | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| Heartbeat | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| Infer | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| ListModels | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| ExecSession | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| SigningRelay | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |
| RemoveWorkspace | api/proto/fleet_runtime.proto | not-UI: fleet node runtime |

## quicbaas.iam.v1.IAMService

| RPC | Proto file | Console |
|---|---|---|
| CreateTenant | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| GetTenant | api/proto/iam/iam.proto | not covered |
| UpdateTenant | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| DeleteTenant | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| ListTenants | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| ProvisionTenant | api/proto/iam/iam.proto | not covered |
| CreateUser | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| GetUser | api/proto/iam/iam.proto | not covered |
| UpdateUser | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| DeleteUser | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| ListUsers | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| ApproveUser | api/proto/iam/iam.proto | not covered |
| ListWebAuthnCredentials | api/proto/iam/iam.proto | not covered |
| DeleteWebAuthnCredential | api/proto/iam/iam.proto | not covered |
| DeleteWebAuthnCredentials | api/proto/iam/iam.proto | not covered |
| GetInvite | api/proto/iam/iam.proto | covered: views/security/InviteActivateView.svelte |
| ActivateFIDO2Begin | api/proto/iam/iam.proto | not covered |
| ActivateFIDO2Finish | api/proto/iam/iam.proto | not covered |
| ActivatePIV | api/proto/iam/iam.proto | covered: views/security/InviteActivateView.svelte |
| CreateRole | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| GetRole | api/proto/iam/iam.proto | not covered |
| UpdateRole | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| DeleteRole | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| ListRoles | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| AssignRole | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| RevokeRole | api/proto/iam/iam.proto | not covered |
| ReconcileRoles | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| RegisterApp | api/proto/iam/iam.proto | not covered |
| GetApp | api/proto/iam/iam.proto | not covered |
| UpdateApp | api/proto/iam/iam.proto | not covered |
| DeleteApp | api/proto/iam/iam.proto | not covered |
| ListApps | api/proto/iam/iam.proto | not covered |
| RotateAppSecret | api/proto/iam/iam.proto | not covered |
| CreatePolicy | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| GetPolicy | api/proto/iam/iam.proto | not covered |
| UpdatePolicy | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| DeletePolicy | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| ListPolicies | api/proto/iam/iam.proto | covered: views/security/IAMView.svelte |
| ValidatePolicy | api/proto/iam/iam.proto | not covered |
| TestPolicy | api/proto/iam/iam.proto | not covered |
| CreateAdminAssignment | api/proto/iam/iam.proto | not covered |
| GetAdminAssignment | api/proto/iam/iam.proto | not covered |
| UpdateAdminAssignment | api/proto/iam/iam.proto | not covered |
| DeleteAdminAssignment | api/proto/iam/iam.proto | not covered |
| ListAdminAssignments | api/proto/iam/iam.proto | not covered |
| CheckAdminPermission | api/proto/iam/iam.proto | not-UI: an authorization probe the services call |
| StartImpersonation | api/proto/iam/iam.proto | not covered |
| EndImpersonation | api/proto/iam/iam.proto | not covered |
| GetActiveImpersonations | api/proto/iam/iam.proto | not covered |
| GrantConsent | api/proto/iam/iam.proto | not covered |
| WithdrawConsent | api/proto/iam/iam.proto | not covered |
| GetConsent | api/proto/iam/iam.proto | not covered |
| ListUserConsents | api/proto/iam/iam.proto | not covered |
| RequestExport | api/proto/iam/iam.proto | not covered |
| GetExportStatus | api/proto/iam/iam.proto | not covered |
| GenerateExport | api/proto/iam/iam.proto | not covered |
| RequestDeletion | api/proto/iam/iam.proto | not covered |
| CancelDeletion | api/proto/iam/iam.proto | not covered |
| ProcessDeletion | api/proto/iam/iam.proto | not covered |
| GetDeletionStatus | api/proto/iam/iam.proto | not covered |

## quicbaas.iam.v1.PEPService

Service: not-UI, the policy enforcement point the services call on each request.

| RPC | Proto file | Console |
|---|---|---|
| Check | api/proto/iam/iam.proto | not-UI: policy enforcement point |
| CheckBatch | api/proto/iam/iam.proto | not-UI: policy enforcement point |
| Subscribe | api/proto/iam/iam.proto | not-UI: policy enforcement point |

## quicbaas.iam.v1.MFAService

| RPC | Proto file | Console |
|---|---|---|
| BeginEnrollment | api/proto/iam/iam.proto | not covered |
| CompleteEnrollment | api/proto/iam/iam.proto | not covered |
| CancelEnrollment | api/proto/iam/iam.proto | not covered |
| BeginChallenge | api/proto/iam/iam.proto | not covered |
| VerifyChallenge | api/proto/iam/iam.proto | not covered |
| VerifyCode | api/proto/iam/iam.proto | not covered |
| GetStatus | api/proto/iam/iam.proto | not covered |
| Disable | api/proto/iam/iam.proto | not covered |
| GenerateBackupCodes | api/proto/iam/iam.proto | not covered |
| VerifyBackupCode | api/proto/iam/iam.proto | not covered |
| GetBackupCodeStatus | api/proto/iam/iam.proto | not covered |

## quicbaas.iam.v1.FederationService

| RPC | Proto file | Console |
|---|---|---|
| CreateProvider | api/proto/iam/iam.proto | not covered |
| GetProvider | api/proto/iam/iam.proto | not covered |
| UpdateProvider | api/proto/iam/iam.proto | not covered |
| DeleteProvider | api/proto/iam/iam.proto | not covered |
| ListProviders | api/proto/iam/iam.proto | not covered |
| BeginFederatedLogin | api/proto/iam/iam.proto | not covered |
| HandleFederatedCallback | api/proto/iam/iam.proto | not covered |
| LinkAccount | api/proto/iam/iam.proto | not covered |
| UnlinkAccount | api/proto/iam/iam.proto | not covered |
| GetUserLinks | api/proto/iam/iam.proto | not covered |

## quicbaas.iam.v1.SessionService

| RPC | Proto file | Console |
|---|---|---|
| ListSessions | api/proto/iam/iam.proto | not covered |
| GetSession | api/proto/iam/iam.proto | not covered |
| RevokeSession | api/proto/iam/iam.proto | not covered |
| RevokeAllSessions | api/proto/iam/iam.proto | not covered |
| RevokeOtherSessions | api/proto/iam/iam.proto | not covered |

## quicbaas.iam.v1.AuditService

| RPC | Proto file | Console |
|---|---|---|
| QueryAuditEvents | api/proto/iam/iam.proto | not covered |
| GetAuditEvent | api/proto/iam/iam.proto | not covered |
| RecordAuditEvent | api/proto/iam/iam.proto | not-UI: the services write audit events |

## quicbaas.kms.v1.KeyManagement

| RPC | Proto file | Console |
|---|---|---|
| GenerateKey | api/proto/kms.proto | covered: views/security/KMSView.svelte |
| ImportKey | api/proto/kms.proto | not covered |
| GetKey | api/proto/kms.proto | not covered |
| GetPublicKey | api/proto/kms.proto | not covered |
| ListKeys | api/proto/kms.proto | covered: views/security/KMSView.svelte |
| ListKeyVersions | api/proto/kms.proto | covered: views/security/KMSView.svelte |
| RotateKey | api/proto/kms.proto | covered: views/security/KMSView.svelte |
| RevokeKey | api/proto/kms.proto | not covered |
| DeleteKey | api/proto/kms.proto | covered: views/security/KMSView.svelte |
| UpdateKeyStatus | api/proto/kms.proto | covered: views/security/KMSView.svelte |
| GetKeyMetadata | api/proto/kms.proto | not covered |
| UpdateKeyMetadata | api/proto/kms.proto | not covered |
| SetKeyPolicy | api/proto/kms.proto | not covered |
| GetKeyPolicy | api/proto/kms.proto | not covered |
| DeleteKeyPolicy | api/proto/kms.proto | not covered |
| SetKeyRotationPolicy | api/proto/kms.proto | not covered |
| GetKeyRotationPolicy | api/proto/kms.proto | not covered |
| DeleteKeyRotationPolicy | api/proto/kms.proto | not covered |
| GetSupportedAlgorithms | api/proto/kms.proto | not covered |
| BackendStatus | api/proto/kms.proto | not covered |
| BackendTest | api/proto/kms.proto | not covered |
| ExportKey | api/proto/kms.proto | not covered |
| UnwrapKey | api/proto/kms.proto | not covered |
| SetKeySafety | api/proto/kms.proto | not covered |
| ClearKeySafety | api/proto/kms.proto | not covered |
| SafetyCheck | api/proto/kms.proto | not covered |
| SafetyReport | api/proto/kms.proto | not covered |

## quicbaas.kms.v1.SecretsManagement

| RPC | Proto file | Console |
|---|---|---|
| StoreSecret | api/proto/kms.proto | covered: views/security/SecretsView.svelte |
| GetSecret | api/proto/kms.proto | covered: views/security/SecretsView.svelte |
| ListSecrets | api/proto/kms.proto | covered: views/security/SecretsView.svelte |
| DeleteSecret | api/proto/kms.proto | covered: views/security/SecretsView.svelte |
| GetSecretVersion | api/proto/kms.proto | not covered |
| ListSecretVersions | api/proto/kms.proto | covered: views/security/SecretsView.svelte |
| RollbackSecret | api/proto/kms.proto | not covered |
| RotateSecret | api/proto/kms.proto | covered: views/security/SecretsView.svelte |
| UpdateSecretStatus | api/proto/kms.proto | not covered |
| SetRotationPolicy | api/proto/kms.proto | not covered |
| GetRotationPolicy | api/proto/kms.proto | not covered |
| GetSecretsByPrefix | api/proto/kms.proto | not covered |
| WatchSecretsByPrefix | api/proto/kms.proto | not covered |
| EnsureSecret | api/proto/kms.proto | not covered |

## quicbaas.kms.v1.Crypto

| RPC | Proto file | Console |
|---|---|---|
| Encrypt | api/proto/kms.proto | covered: views/security/CryptoView.svelte |
| Decrypt | api/proto/kms.proto | covered: views/security/CryptoView.svelte |
| Rewrap | api/proto/kms.proto | not covered |
| EncryptBatch | api/proto/kms.proto | not covered |
| DecryptBatch | api/proto/kms.proto | not covered |
| GenerateDataKey | api/proto/kms.proto | covered: views/security/CryptoView.svelte |
| GenerateWrappedDataKey | api/proto/kms.proto | not covered |
| Sign | api/proto/kms.proto | covered: views/security/CryptoView.svelte |
| Verify | api/proto/kms.proto | covered: views/security/CryptoView.svelte |
| GenerateHMAC | api/proto/kms.proto | not covered |
| VerifyHMAC | api/proto/kms.proto | not covered |
| GenerateRandom | api/proto/kms.proto | covered: views/security/CryptoView.svelte |
| Hash | api/proto/kms.proto | covered: views/security/CryptoView.svelte |

## quicbaas.kms.v1.DEKManagement

| RPC | Proto file | Console |
|---|---|---|
| CreateDEK | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| GetDEK | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| ListDEKs | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| RotateDEK | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| DeleteDEK | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| GetDEKStatus | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| SetDefaultDEK | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| SetDEKSafety | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| ClearDEKSafety | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| StartReencryption | api/proto/kms.proto | covered: views/security/DEKView.svelte |
| GetReencryptionProgress | api/proto/kms.proto | covered: views/security/DEKView.svelte |

## quicbaas.login.v1.Login

| RPC | Proto file | Console |
|---|---|---|
| BeginLogin | api/proto/login.proto | covered: views/orchestrator/LoginView.svelte |
| FinishLogin | api/proto/login.proto | covered: views/orchestrator/LoginView.svelte |

## quicbaas.messaging.v1.MessagingService

| RPC | Proto file | Console |
|---|---|---|
| CreateChannel | api/proto/messaging.proto | covered: views/async/MessagingView.svelte |
| DeleteChannel | api/proto/messaging.proto | covered: views/async/MessagingView.svelte |
| GetChannel | api/proto/messaging.proto | not covered |
| ListChannels | api/proto/messaging.proto | covered: views/async/MessagingView.svelte |
| Subscribe | api/proto/messaging.proto | not covered |
| Unsubscribe | api/proto/messaging.proto | not covered |
| Publish | api/proto/messaging.proto | covered: views/async/MessagingView.svelte |
| PublishDelta | api/proto/messaging.proto | not covered |
| StreamNotifications | api/proto/messaging.proto | not covered |
| WatchChannel | api/proto/messaging.proto | not covered |
| GetStats | api/proto/messaging.proto | not covered |
| GetDedupStats | api/proto/messaging.proto | not covered |
| GetChannelStats | api/proto/messaging.proto | not covered |

## quicbaas.v1.ObjectTransferService

Service: not-UI, node-to-node streaming object transfer.

| RPC | Proto file | Console |
|---|---|---|
| FetchObject | api/proto/object_transfer.proto | not-UI: node-to-node object transfer |
| StreamFetchObjects | api/proto/object_transfer.proto | not-UI: node-to-node object transfer |

## quicbaas.objectstorage.v1.ObjectStorageService

| RPC | Proto file | Console |
|---|---|---|
| PutObject | api/proto/objectstorage.proto | covered: views/data/ObjectsView.svelte |
| GetObject | api/proto/objectstorage.proto | covered: views/data/ObjectsView.svelte |
| DeleteObject | api/proto/objectstorage.proto | covered: views/data/ObjectsView.svelte |
| HeadObject | api/proto/objectstorage.proto | not covered |
| ListObjects | api/proto/objectstorage.proto | covered: views/data/ObjectsView.svelte |
| ListByTag | api/proto/objectstorage.proto | not covered |
| ListByTenant | api/proto/objectstorage.proto | not covered |
| CopyObject | api/proto/objectstorage.proto | not covered |
| ReplicateObject | api/proto/objectstorage.proto | not covered |
| MigrateObject | api/proto/objectstorage.proto | not covered |
| ArchiveObject | api/proto/objectstorage.proto | not covered |
| RestoreObject | api/proto/objectstorage.proto | not covered |
| VerifyObject | api/proto/objectstorage.proto | not covered |
| SetObjectPolicy | api/proto/objectstorage.proto | not covered |
| GetObjectPolicy | api/proto/objectstorage.proto | not covered |
| ListBackends | api/proto/objectstorage.proto | not covered |
| GetBackendHealth | api/proto/objectstorage.proto | not covered |
| GetObjectStorageTenantUsage | api/proto/objectstorage.proto | not covered |
| GetObjectStorageTenantQuotas | api/proto/objectstorage.proto | not covered |
| SetObjectStorageTenantQuotas | api/proto/objectstorage.proto | not covered |
| StreamGetObject | api/proto/objectstorage.proto | not covered |
| StreamPutObject | api/proto/objectstorage.proto | not covered |
| BatchPutObjects | api/proto/objectstorage.proto | not covered |
| BatchDeleteObjects | api/proto/objectstorage.proto | not covered |
| BatchGetObjects | api/proto/objectstorage.proto | not covered |

## quicbaas.objectstorage.v1.ObjectStorageAdmin

| RPC | Proto file | Console |
|---|---|---|
| CreateBackend | api/proto/objectstorage.proto | not covered |
| UpdateBackend | api/proto/objectstorage.proto | not covered |
| DeleteBackend | api/proto/objectstorage.proto | not covered |
| SetBackendStatus | api/proto/objectstorage.proto | not covered |
| GetStorageStats | api/proto/objectstorage.proto | not covered |
| RunGarbageCollection | api/proto/objectstorage.proto | not covered |
| GetGarbageCollectionProgress | api/proto/objectstorage.proto | not covered |
| RepairObject | api/proto/objectstorage.proto | not covered |
| CompactBackend | api/proto/objectstorage.proto | not covered |

## quicbaas.ollama.v1.OllamaService

Service: not-UI, the server's internal embedding backend adapter; a console user reaches embeddings through EmbeddingService.

| RPC | Proto file | Console |
|---|---|---|
| Health | api/proto/ollama.proto | not-UI: internal embedding backend adapter |
| ListModels | api/proto/ollama.proto | not-UI: internal embedding backend adapter |
| GetModelInfo | api/proto/ollama.proto | not-UI: internal embedding backend adapter |
| Embed | api/proto/ollama.proto | not-UI: internal embedding backend adapter |
| EmbedWithModel | api/proto/ollama.proto | not-UI: internal embedding backend adapter |
| EmbedBatch | api/proto/ollama.proto | not-UI: internal embedding backend adapter |

## quicbaas.orchestrator.v1.OrchestratorService

| RPC | Proto file | Console |
|---|---|---|
| SpawnSession | api/proto/orchestrator.proto | covered: components/orchestrator/SpawnDialog.svelte |
| PromptSession | api/proto/orchestrator.proto | covered: components/orchestrator/PromptComposer.svelte |
| SessionStatus | api/proto/orchestrator.proto | not covered |
| CloseSession | api/proto/orchestrator.proto | covered: stores/sessionStore.svelte.ts |
| ListSessions | api/proto/orchestrator.proto | covered: stores/sessionStore.svelte.ts |
| RotateSessionAccount | api/proto/orchestrator.proto | covered: components/orchestrator/SessionDetail.svelte |
| CreateTask | api/proto/orchestrator.proto | covered: components/orchestrator/CreateTaskDialog.svelte |
| GetTask | api/proto/orchestrator.proto | covered: cmd/quicbaas/bindings_orchestrator.go |
| ListTasks | api/proto/orchestrator.proto | covered: stores/orchestrator.svelte.ts |
| ClaimTask | api/proto/orchestrator.proto | covered: views/orchestrator/TasksView.svelte |
| CompleteTask | api/proto/orchestrator.proto | covered: views/orchestrator/TasksView.svelte |
| BlockTask | api/proto/orchestrator.proto | not covered |
| ReadmitTask | api/proto/orchestrator.proto | covered: views/orchestrator/JobDetailView.svelte |
| DeleteTask | api/proto/orchestrator.proto | not covered |
| DecomposeTask | api/proto/orchestrator.proto | covered: views/orchestrator/TasksView.svelte |
| StartJob | api/proto/orchestrator.proto | covered: components/orchestrator/StartJobDialog.svelte |
| GetJobStatus | api/proto/orchestrator.proto | covered: stores/orchestrator.svelte.ts |
| CancelJob | api/proto/orchestrator.proto | covered: views/orchestrator/JobsView.svelte |
| ListJobs | api/proto/orchestrator.proto | covered: views/orchestrator/JobsView.svelte |
| SubscribeJobEvents | api/proto/orchestrator.proto | covered: stores/job-events.svelte.ts |
| ListBackends | api/proto/orchestrator.proto | covered: views/orchestrator/InferenceView.svelte |
| ListRunners | api/proto/orchestrator.proto | covered: views/orchestrator/JobDetailView.svelte |
| ListWorkflows | api/proto/orchestrator.proto | covered: views/orchestrator/WorkflowsView.svelte |
| CreateWorkflow | api/proto/orchestrator.proto | covered: views/orchestrator/WorkflowsView.svelte |
| GetWorkflow | api/proto/orchestrator.proto | not covered |
| UpdateWorkflow | api/proto/orchestrator.proto | covered: views/orchestrator/WorkflowsView.svelte |
| DeleteWorkflow | api/proto/orchestrator.proto | covered: views/orchestrator/WorkflowsView.svelte |
| Health | api/proto/orchestrator.proto | covered: views/SystemDashboard.svelte |
| RegisterFleetNode | api/proto/orchestrator.proto | covered: components/orchestrator/RegisterFleetNodeDialog.svelte |
| GetFleetNode | api/proto/orchestrator.proto | covered: cmd/quicbaas/bindings_orchestrator.go |
| ListFleetNodes | api/proto/orchestrator.proto | covered: stores/fleetStore.svelte.ts |
| RemoveFleetNode | api/proto/orchestrator.proto | covered: components/orchestrator/FleetList.svelte |
| FleetNodeHeartbeat | api/proto/orchestrator.proto | not-UI: the fleet node's liveness beat |
| ListBackendModels | api/proto/orchestrator.proto | covered: views/orchestrator/InferenceView.svelte |
| PushEvents | api/proto/orchestrator.proto | not-UI: the agent runtime pushes session events |
| SubscribeSessionEvents | api/proto/orchestrator.proto | covered: stores/sessionStore.svelte.ts |
| GetSessionConversation | api/proto/orchestrator.proto | covered: stores/sessionStore.svelte.ts |
| GetSpendReport | api/proto/orchestrator.proto | covered: views/orchestrator/CostView.svelte |
| GetTenantSpendPolicy | api/proto/orchestrator.proto | covered: components/orchestrator/TenantSpendLimitsEditor.svelte |
| SetTenantSpendPolicy | api/proto/orchestrator.proto | covered: components/orchestrator/TenantSpendLimitsEditor.svelte |
| Inference | api/proto/orchestrator.proto | covered: views/orchestrator/InferenceView.svelte |
| InferenceStream | api/proto/orchestrator.proto | covered: views/orchestrator/InferenceView.svelte |
| SpawnChildJob | api/proto/orchestrator.proto | not-UI: a running agent spawns its child job |
| SubmitTaskArtifacts | api/proto/orchestrator.proto | not-UI: a running agent submits its artifacts |
| ListTaskArtifacts | api/proto/orchestrator.proto | not covered |
| GetTaskArtifact | api/proto/orchestrator.proto | not covered |
| GetCostSettings | api/proto/orchestrator.proto | covered: components/orchestrator/CostSettingsPanel.svelte |
| SetCostSettings | api/proto/orchestrator.proto | covered: components/orchestrator/CostSettingsPanel.svelte |

## quicbaas.parameterstore.v1.ParameterStore

| RPC | Proto file | Console |
|---|---|---|
| GetParameter | api/proto/parameter_store.proto | not covered |
| SetParameter | api/proto/parameter_store.proto | not covered |
| DeleteParameter | api/proto/parameter_store.proto | not covered |
| GetParameters | api/proto/parameter_store.proto | not covered |
| SetParameters | api/proto/parameter_store.proto | not covered |
| DeleteParameters | api/proto/parameter_store.proto | not covered |
| ListParameters | api/proto/parameter_store.proto | not covered |
| SearchByTags | api/proto/parameter_store.proto | not covered |

## quicbaas.piv.v1.PIVService

| RPC | Proto file | Console |
|---|---|---|
| ListPIVSlots | api/proto/piv.proto | not covered |
| GetPIVCertificate | api/proto/piv.proto | not covered |
| StorePIVCertificate | api/proto/piv.proto | not covered |
| DeletePIVCertificate | api/proto/piv.proto | not covered |
| GeneratePIVKey | api/proto/piv.proto | not covered |
| ImportPIVCertificate | api/proto/piv.proto | not covered |
| ExportPIVCertificate | api/proto/piv.proto | not covered |
| GeneratePIVCSR | api/proto/piv.proto | not covered |

## quicbaas.project.v1.ProjectService

| RPC | Proto file | Console |
|---|---|---|
| CreateProject | api/proto/project.proto | covered: views/orchestrator/ProjectsView.svelte |
| GetProject | api/proto/project.proto | not covered |
| GetProjectByName | api/proto/project.proto | not covered |
| UpdateProject | api/proto/project.proto | covered: views/orchestrator/ProjectsView.svelte |
| DeleteProject | api/proto/project.proto | covered: views/orchestrator/ProjectsView.svelte |
| ListProjects | api/proto/project.proto | covered: views/orchestrator/ProjectsView.svelte |
| GrantFleetNodeRole | api/proto/project.proto | covered: views/orchestrator/ProjectsView.svelte |
| RevokeFleetNodeRole | api/proto/project.proto | covered: views/orchestrator/ProjectsView.svelte |

## quicbaas.prompt.v1.PromptService

| RPC | Proto file | Console |
|---|---|---|
| ListPrompts | api/proto/prompt.proto | not covered |
| GetPrompt | api/proto/prompt.proto | not covered |
| GetEffectivePrompt | api/proto/prompt.proto | not covered |
| DiffPrompt | api/proto/prompt.proto | not covered |
| GetPromptHistory | api/proto/prompt.proto | not covered |
| PutPrompt | api/proto/prompt.proto | not covered |
| RollbackPrompt | api/proto/prompt.proto | not covered |
| ResetPrompt | api/proto/prompt.proto | not covered |

## quicbaas.v1.QuicBaaS

| RPC | Proto file | Console |
|---|---|---|
| Put | api/proto/quicbaas.proto | covered: views/data/KVStoreView.svelte |
| Get | api/proto/quicbaas.proto | not covered |
| Delete | api/proto/quicbaas.proto | covered: views/data/KVStoreView.svelte |
| List | api/proto/quicbaas.proto | covered: views/data/KVStoreView.svelte |
| ListEntityTypes | api/proto/quicbaas.proto | not covered |
| Watch | api/proto/quicbaas.proto | not covered |
| Query | api/proto/quicbaas.proto | not covered |
| ScanIndex | api/proto/quicbaas.proto | not covered |
| ClusterStatus | api/proto/quicbaas.proto | covered: views/cluster/ClusterDashboard.svelte |
| GetLeader | api/proto/quicbaas.proto | not covered |
| AddNode | api/proto/quicbaas.proto | not covered |
| RemoveNode | api/proto/quicbaas.proto | not covered |
| CreateShard | api/proto/quicbaas.proto | not covered |
| Aggregate | api/proto/quicbaas.proto | not covered |
| TimeSeriesQuery | api/proto/quicbaas.proto | not covered |
| TSWritePoint | api/proto/quicbaas.proto | covered: views/data/TimeSeriesView.svelte |
| TSWriteBatch | api/proto/quicbaas.proto | not covered |
| TSQuery | api/proto/quicbaas.proto | covered: views/data/TimeSeriesView.svelte |
| TSDeleteSeries | api/proto/quicbaas.proto | covered: views/data/TimeSeriesView.svelte |
| TSGetRetentionPolicy | api/proto/quicbaas.proto | not covered |
| TSSetRetentionPolicy | api/proto/quicbaas.proto | not covered |
| TSListMetrics | api/proto/quicbaas.proto | covered: views/data/TimeSeriesView.svelte |
| BatchPut | api/proto/quicbaas.proto | not covered |
| BatchGet | api/proto/quicbaas.proto | not covered |
| BatchDelete | api/proto/quicbaas.proto | not covered |
| StreamBatchPut | api/proto/quicbaas.proto | not covered |
| Transaction | api/proto/quicbaas.proto | not covered |
| Health | api/proto/quicbaas.proto | covered: views/SystemDashboard.svelte |
| CreateBackup | api/proto/quicbaas.proto | not covered |
| ListBackups | api/proto/quicbaas.proto | not covered |
| RestoreBackup | api/proto/quicbaas.proto | not covered |
| DeleteBackup | api/proto/quicbaas.proto | not covered |
| VerifyBackup | api/proto/quicbaas.proto | not covered |
| Login | api/proto/quicbaas.proto | not covered |
| Logout | api/proto/quicbaas.proto | covered: views/MainApp.svelte |
| RefreshToken | api/proto/quicbaas.proto | covered: stores/auth.svelte.ts |
| ValidateToken | api/proto/quicbaas.proto | covered: stores/auth.svelte.ts |
| IntrospectToken | api/proto/quicbaas.proto | not covered |
| CreateRole | api/proto/quicbaas.proto | not covered |
| DeleteRole | api/proto/quicbaas.proto | not covered |
| ListRoles | api/proto/quicbaas.proto | not covered |
| AssignRole | api/proto/quicbaas.proto | not covered |
| RevokeRole | api/proto/quicbaas.proto | not covered |
| MigrateData | api/proto/quicbaas.proto | not covered |
| GetMigrationProgress | api/proto/quicbaas.proto | not covered |
| RebalanceCluster | api/proto/quicbaas.proto | not covered |
| GetRebalanceProgress | api/proto/quicbaas.proto | not covered |
| ReplaceNode | api/proto/quicbaas.proto | not covered |
| UpgradeNode | api/proto/quicbaas.proto | not covered |
| GetUpgradeStatus | api/proto/quicbaas.proto | not covered |
| GetCacheStats | api/proto/quicbaas.proto | not covered |
| ClearCache | api/proto/quicbaas.proto | not covered |
| GetCacheConfig | api/proto/quicbaas.proto | not covered |
| UpdateCacheConfig | api/proto/quicbaas.proto | not covered |
| SealStatus | api/proto/quicbaas.proto | not covered |
| SealInit | api/proto/quicbaas.proto | not covered |
| Unseal | api/proto/quicbaas.proto | not covered |
| Seal | api/proto/quicbaas.proto | not covered |
| SealRekey | api/proto/quicbaas.proto | not covered |

## quicbaas.rag.v1.RAGService

| RPC | Proto file | Console |
|---|---|---|
| Query | api/proto/rag.proto | covered: views/ai/RAGView.svelte |
| BuildContext | api/proto/rag.proto | covered: views/ai/RAGView.svelte |

## quicbaas.registry.v1.RegistryService

Service: not-UI, service instances register and resolve each other.

| RPC | Proto file | Console |
|---|---|---|
| Register | api/proto/registry.proto | not-UI: service registry for instances |
| Renew | api/proto/registry.proto | not-UI: service registry for instances |
| Deregister | api/proto/registry.proto | not-UI: service registry for instances |
| Resolve | api/proto/registry.proto | not-UI: service registry for instances |
| Watch | api/proto/registry.proto | not-UI: service registry for instances |

## quicbaas.scheduler.v1.SchedulerService

| RPC | Proto file | Console |
|---|---|---|
| CreateJob | api/proto/scheduler.proto | covered: views/async/SchedulerView.svelte |
| GetJob | api/proto/scheduler.proto | not covered |
| UpdateJob | api/proto/scheduler.proto | covered: views/async/SchedulerView.svelte |
| DeleteJob | api/proto/scheduler.proto | covered: views/async/SchedulerView.svelte |
| ListJobs | api/proto/scheduler.proto | covered: views/async/SchedulerView.svelte |
| TriggerJob | api/proto/scheduler.proto | covered: views/async/SchedulerView.svelte |
| PauseJob | api/proto/scheduler.proto | covered: views/async/SchedulerView.svelte |
| ResumeJob | api/proto/scheduler.proto | covered: views/async/SchedulerView.svelte |
| CancelJob | api/proto/scheduler.proto | not covered |
| GetJobHistory | api/proto/scheduler.proto | not covered |
| GetSchedulerStatus | api/proto/scheduler.proto | not covered |
| RegisterNode | api/proto/scheduler.proto | not-UI: a scheduler node's own membership |
| RenewNode | api/proto/scheduler.proto | not-UI: a scheduler node's own membership |
| DeregisterNode | api/proto/scheduler.proto | not-UI: a scheduler node's own membership |
| GetNode | api/proto/scheduler.proto | not covered |
| ListNodes | api/proto/scheduler.proto | not covered |
| CordonNode | api/proto/scheduler.proto | not covered |
| UncordonNode | api/proto/scheduler.proto | not covered |
| DrainNode | api/proto/scheduler.proto | not covered |
| Place | api/proto/scheduler.proto | not-UI: placement calls by the services |
| Release | api/proto/scheduler.proto | not-UI: placement calls by the services |
| GetPlacement | api/proto/scheduler.proto | not covered |
| ListPlacements | api/proto/scheduler.proto | not covered |

## quicbaas.sdlc.v1.SDLCService

| RPC | Proto file | Console |
|---|---|---|
| BindProject | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| UnbindProject | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| GetBinding | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| GetDelivery | api/proto/sdlc.proto | not covered |
| ListForges | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| Onboard | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| OnboardRepo | api/proto/sdlc.proto | not covered |
| GetConformance | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| GetOpenWork | api/proto/sdlc.proto | not covered |
| GetReviewerSelection | api/proto/sdlc.proto | covered: components/orchestrator/SdlcReviewerPicker.svelte |
| SetReviewerOverride | api/proto/sdlc.proto | covered: components/orchestrator/SdlcReviewerPicker.svelte |
| ClearReviewerOverride | api/proto/sdlc.proto | covered: components/orchestrator/SdlcReviewerPicker.svelte |
| GetWorkerImageStatus | api/proto/sdlc.proto | covered: stores/workerImage.svelte.ts |
| RefreshWorkerImage | api/proto/sdlc.proto | covered: stores/workerImage.svelte.ts |
| FileIssue | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| ListCollaborators | api/proto/sdlc.proto | covered: views/orchestrator/ProjectsView.svelte |
| AdoptIssue | api/proto/sdlc.proto | not covered |
| ListAttention | api/proto/sdlc.proto | covered: stores/attentionStore.svelte.ts |
| ResolveAttention | api/proto/sdlc.proto | covered: stores/attentionStore.svelte.ts |

## quicbaas.ssh.v1.SSHService

| RPC | Proto file | Console |
|---|---|---|
| GenerateSSHKey | api/proto/ssh.proto | covered: views/security/ssh/GenerateKeyModal.svelte |
| ImportSSHKey | api/proto/ssh.proto | covered: views/security/ssh/ImportKeyModal.svelte |
| GetSSHKey | api/proto/ssh.proto | covered: cmd/quicbaas/bindings_ssh.go |
| ListSSHKeys | api/proto/ssh.proto | covered: views/security/SSHKeysView.svelte |
| DeleteSSHKey | api/proto/ssh.proto | covered: views/security/SSHKeysView.svelte |
| ExportSSHPublicKey | api/proto/ssh.proto | not covered |
| SignSSHDigest | api/proto/ssh.proto | not-UI: the signing relay for agent git and ssh |
| AddSSHKnownHost | api/proto/ssh.proto | covered: views/security/ssh/KnownHostsPanel.svelte |
| ListSSHKnownHosts | api/proto/ssh.proto | covered: views/security/ssh/KnownHostsPanel.svelte |
| RemoveSSHKnownHost | api/proto/ssh.proto | covered: views/security/ssh/KnownHostsPanel.svelte |
| AddSSHAuthorizedKey | api/proto/ssh.proto | not covered |
| ListSSHAuthorizedKeys | api/proto/ssh.proto | not covered |
| RemoveSSHAuthorizedKey | api/proto/ssh.proto | not covered |
| EmitSSHAuthorizedKey | api/proto/ssh.proto | not-UI: sshd's AuthorizedKeysCommand output |

## quicbaas.tenant.v1.TenantBarrier

| RPC | Proto file | Console |
|---|---|---|
| Init | api/proto/tenant_barrier.proto | covered: views/security/TenantBarrierView.svelte |
| Unseal | api/proto/tenant_barrier.proto | covered: views/security/TenantBarrierView.svelte |
| UnsealShare | api/proto/tenant_barrier.proto | covered: views/security/TenantBarrierView.svelte |
| Seal | api/proto/tenant_barrier.proto | covered: views/security/TenantBarrierView.svelte |
| Status | api/proto/tenant_barrier.proto | covered: views/security/TenantBarrierView.svelte |
| EnableEncryption | api/proto/tenant_barrier.proto | not covered |
| UpdateQuotas | api/proto/tenant_barrier.proto | not covered |
| UpdateEntitlements | api/proto/tenant_barrier.proto | not covered |

## quicbaas.vector.v1.VectorDB

| RPC | Proto file | Console |
|---|---|---|
| UpsertVector | api/proto/vector.proto | not covered |
| BatchUpsertVectors | api/proto/vector.proto | not covered |
| GetVector | api/proto/vector.proto | not covered |
| UpdateMetadata | api/proto/vector.proto | not covered |
| DeleteVector | api/proto/vector.proto | not covered |
| BatchDeleteVectors | api/proto/vector.proto | not covered |
| SearchSimilar | api/proto/vector.proto | covered: views/data/VectorsView.svelte |
| SearchByID | api/proto/vector.proto | not covered |
| HybridSearch | api/proto/vector.proto | not covered |
| SearchByMetadata | api/proto/vector.proto | not covered |
| ListVectors | api/proto/vector.proto | not covered |
| ListNamespaces | api/proto/vector.proto | covered: views/data/VectorsView.svelte |
| CreateNamespace | api/proto/vector.proto | covered: views/data/VectorsView.svelte |
| UpdateNamespace | api/proto/vector.proto | not covered |
| DeleteNamespace | api/proto/vector.proto | covered: views/data/VectorsView.svelte |
| GetNamespaceInfo | api/proto/vector.proto | not covered |
| GetVectorStats | api/proto/vector.proto | not covered |
| Health | api/proto/vector.proto | not covered |
| RebuildIndex | api/proto/vector.proto | not covered |
| GetIndexProgress | api/proto/vector.proto | not covered |

## quicbaas.vector.v1.VectorAdmin

| RPC | Proto file | Console |
|---|---|---|
| GetClusterStats | api/proto/vector.proto | not covered |
| CompactNamespace | api/proto/vector.proto | not covered |
| GetCompactionProgress | api/proto/vector.proto | not covered |
| ExportVectors | api/proto/vector.proto | not covered |
| ImportVectors | api/proto/vector.proto | not covered |
| CreateScalarIndex | api/proto/vector.proto | not covered |
| DropScalarIndex | api/proto/vector.proto | not covered |
| ListScalarIndexes | api/proto/vector.proto | not covered |
