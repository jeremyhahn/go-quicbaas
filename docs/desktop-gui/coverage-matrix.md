# Console coverage matrix

This matrix records, for every REST route registered in `pkg/api/rest/routes.go`, whether the
QuicBaaS console (`cmd/quicbaas/frontend`) can reach it. The SDK file column names the
`@quicbaas/sdk` file (`sdk/typescript/src/`) that builds the route's `/api/v1/...` path. The
Console column is decided as follows. A route the SDK reaches is `covered: <file>` when frontend
code calls the SDK method that builds its path, naming that file (paths are relative to
`cmd/quicbaas/frontend/src/`), and `not covered` when no frontend code calls the method. A route
the SDK does not reach is `not covered`, or `not-UI: <reason>` when the route is not meant for a
console (protocol endpoints, probes, hooks and webhooks called by other programs); the reason is
given once per route group. An SDK file marked `-` (rest.ts†) is a route the first literal-path
match missed but whose path an SDK method builds from a prefix constant or a template (the
orchestrator's `API_PREFIX`, `FLEET_PREFIX`, `WORKER_IMAGE_PATH`, or a `*` path segment); it is
marked by the same rule. No route is marked `covered` without the file that calls it.

## REST routes (pkg/api/rest/routes.go)

### acme

not-UI: the ACME protocol, called by ACME clients.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/acme/finalize` | HandleACMEFinalizeOrder | - | not-UI: ACME protocol |
| POST | `/api/v1/acme/issue` | HandleACMEIssue | - | not-UI: ACME protocol |
| POST | `/api/v1/acme/new-order` | HandleACMENewOrder | - | not-UI: ACME protocol |
| POST | `/api/v1/acme/register-challenge` | HandleACMERegisterChallenge | - | not-UI: ACME protocol |
| POST | `/api/v1/acme/renew` | HandleACMERenew | - | not-UI: ACME protocol |

### agent-accounts

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/agent-accounts/` | HandleListAccounts | rest.ts | covered: views/orchestrator/AccountsView.svelte |
| POST | `/api/v1/agent-accounts/` | HandleCreateAccount | rest.ts | covered: views/orchestrator/AccountsView.svelte |
| DELETE | `/api/v1/agent-accounts/{id}` | HandleDeleteAccount | rest.ts | covered: views/orchestrator/AccountsView.svelte |
| GET | `/api/v1/agent-accounts/{id}` | HandleGetAccount | rest.ts | not covered |
| PUT | `/api/v1/agent-accounts/{id}` | HandleUpdateAccount | rest.ts | covered: components/orchestrator/AccountEditDialog.svelte |
| POST | `/api/v1/agent-accounts/{id}/default` | HandleSetDefaultAccount | rest.ts | covered: views/orchestrator/AccountsView.svelte |
| POST | `/api/v1/agent-accounts/{id}/materialize` | HandleMaterializeAccountCredential | - | not covered |
| POST | `/api/v1/agent-accounts/{id}/reservations` | HandleReserveAccountSession | - | not covered |
| DELETE | `/api/v1/agent-accounts/{id}/reservations/{rid}` | HandleReleaseAccountSession | - | not covered |
| POST | `/api/v1/agent-accounts/{id}/revoke` | HandleRevokeAccount | rest.ts | covered: views/orchestrator/AccountsView.svelte |
| POST | `/api/v1/agent-accounts/{id}/rotate` | HandleRotateAccountCredential | rest.ts | covered: views/orchestrator/AccountsView.svelte |
| GET | `/api/v1/agent-accounts/{id}/usage` | HandleGetAccountUsage | rest.ts | covered: views/orchestrator/AccountsView.svelte |

### attestation

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/attestation/attest-container` | HandleAttestationAttestContainer | - | not covered |
| POST | `/api/v1/attestation/verify` | HandleAttestationVerify | - | not covered |
| POST | `/api/v1/attestation/verify-chain` | HandleAttestationVerifyChain | - | not covered |
| POST | `/api/v1/attestation/appraise-quote` | HandleAttestationAppraiseQuote | - | not covered |
| GET | `/api/v1/attestation/endorsements` | HandleAttestationListEndorsements | - | not covered |
| PUT | `/api/v1/attestation/endorsements/{name}` | HandleAttestationPutEndorsement | - | not covered |
| DELETE | `/api/v1/attestation/endorsements/{name}` | HandleAttestationDeleteEndorsement | - | not covered |
| GET | `/api/v1/attestation/reference-values` | HandleAttestationListReferenceValues | - | not covered |
| GET | `/api/v1/attestation/reference-values/{name}` | HandleAttestationGetReferenceValues | - | not covered |
| PUT | `/api/v1/attestation/reference-values/{name}` | HandleAttestationPutReferenceValues | - | not covered |
| DELETE | `/api/v1/attestation/reference-values/{name}` | HandleAttestationDeleteReferenceValues | - | not covered |

### barrier

The console's BarrierView adapts the SDK barrier client to three calls: status, Shamir
initialize and unseal with one share.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/barrier/enable-encryption` | HandleBarrierEnableEncryption | - | not covered |
| POST | `/api/v1/barrier/initialize` | HandleBarrierInitialize | rest.ts | not covered |
| DELETE | `/api/v1/barrier/recovery` | HandleBarrierRecoveryDelete | rest.ts | not covered |
| POST | `/api/v1/barrier/recovery/generate` | HandleBarrierRecoveryGenerate | rest.ts | not covered |
| POST | `/api/v1/barrier/recovery/recover` | HandleBarrierRecoveryRecover | rest.ts | not covered |
| GET | `/api/v1/barrier/recovery/status` | HandleBarrierRecoveryStatus | rest.ts | not covered |
| POST | `/api/v1/barrier/rekey` | HandleBarrierRekey | rest.ts | not covered |
| POST | `/api/v1/barrier/root-token` | HandleBarrierRootToken | rest.ts | not covered |
| POST | `/api/v1/barrier/seal` | HandleBarrierSeal | rest.ts | not covered |
| POST | `/api/v1/barrier/shamir/initialize` | HandleBarrierShamirInitialize | rest.ts | covered: views/security/BarrierView.svelte |
| POST | `/api/v1/barrier/shamir/unseal-share` | HandleBarrierShamirUnsealShare | rest.ts | covered: views/security/BarrierView.svelte |
| POST | `/api/v1/barrier/shamir/unseal-shares` | HandleBarrierShamirUnsealShares | rest.ts | not covered |
| GET | `/api/v1/barrier/status` | HandleBarrierStatus | rest.ts | covered: views/security/BarrierView.svelte |
| POST | `/api/v1/barrier/unseal` | HandleBarrierUnseal | rest.ts | not covered |

### batch

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/batch/delete` | HandleBatchDelete | - | not covered |
| POST | `/api/v1/batch/get` | HandleBatchGet | - | not covered |
| POST | `/api/v1/batch/put` | HandleBatchPut | - | not covered |

### bootstrap

RestBootstrapClient builds `/api/v1/bootstrap/<mode>` from the mode name.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/bootstrap/auto` | HandleBootstrapAuto | - (rest.ts†) | covered: views/security/BootstrapView.svelte |
| POST | `/api/v1/bootstrap/dane` | HandleBootstrapDane | - (rest.ts†) | covered: views/security/BootstrapView.svelte |
| POST | `/api/v1/bootstrap/noise` | HandleBootstrapNoise | - (rest.ts†) | covered: views/security/BootstrapView.svelte |
| POST | `/api/v1/bootstrap/spki` | HandleBootstrapSpki | - (rest.ts†) | covered: views/security/BootstrapView.svelte |

### cache

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/cache/clear` | HandleClearCache | - | not covered |
| GET | `/api/v1/cache/config` | HandleGetCacheConfig | - | not covered |
| PUT | `/api/v1/cache/config` | HandleUpdateCacheConfig | - | not covered |
| GET | `/api/v1/cache/stats` | HandleGetCacheStats | - | not covered |

### certificate

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/certificate/ca` | HandleGetCACertificate | rest.ts | not covered |
| GET | `/api/v1/certificate/ca/bundle` | HandleCABundle | rest.ts | covered: views/security/CertificatesView.svelte |
| GET | `/api/v1/certificate/ca/info` | HandleCAInfo | rest.ts | covered: views/security/CertificatesView.svelte |
| GET | `/api/v1/certificate/ca/list` | HandleListCertificates | - (rest.ts†) | covered: views/security/CertificatesView.svelte |
| POST | `/api/v1/certificate/chain` | HandleCertificateChain | - | not covered |
| POST | `/api/v1/certificate/cross-sign` | HandleCertificateCrossSign | - | not covered |
| POST | `/api/v1/certificate/device-identity` | HandleCertificateDeviceIdentity | - | not covered |
| POST | `/api/v1/certificate/issue` | HandleIssueCertificate | rest.ts | covered: views/security/CertificatesView.svelte |
| POST | `/api/v1/certificate/revoke` | HandleRevokeCertificate | rest.ts | covered: views/security/CertificatesView.svelte |
| POST | `/api/v1/certificate/sign-csr` | HandleSignCSR | rest.ts | covered: views/security/CertificatesView.svelte |
| POST | `/api/v1/certificate/tls-config` | HandleGetTLSConfig | - | not covered |
| POST | `/api/v1/certificate/verify` | HandleVerifyCertificate | rest.ts | covered: views/security/CertificatesView.svelte |

### cluster

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/cluster/leader` | HandleGetClusterLeader | - | not covered |
| GET | `/api/v1/cluster/leader/{shardID}` | HandleGetLeader | - | not covered |
| DELETE | `/api/v1/cluster/migrate/` | HandleCancelMigration | - | not covered |
| POST | `/api/v1/cluster/migrate/` | HandleStartMigration | - | not covered |
| GET | `/api/v1/cluster/migrate/status` | HandleGetMigrationStatus | - | not covered |
| GET | `/api/v1/cluster/nodes/` | HandleListNodes | - | not covered |
| POST | `/api/v1/cluster/nodes/` | HandleAddNode | - | not covered |
| GET | `/api/v1/cluster/nodes/non-leader` | HandleListNonLeaderNodes | - | not covered |
| DELETE | `/api/v1/cluster/nodes/{nodeId}` | HandleRemoveNode | - | not covered |
| PUT | `/api/v1/cluster/nodes/{nodeId}/replace` | HandleReplaceNode | - | not covered |
| POST | `/api/v1/cluster/rebalance/` | HandleStartRebalance | - | not covered |
| GET | `/api/v1/cluster/rebalance/plan` | HandleGetRebalancePlan | - | not covered |
| GET | `/api/v1/cluster/rebalance/status` | HandleGetRebalanceStatus | - | not covered |
| GET | `/api/v1/cluster/shards` | HandleClusterShards | - | not covered |
| GET | `/api/v1/cluster/status` | HandleClusterStatus | rest.ts | covered: views/cluster/ClusterDashboard.svelte |
| POST | `/api/v1/cluster/upgrade/` | HandleStartUpgrade | - | not covered |
| GET | `/api/v1/cluster/upgrade/status` | HandleGetUpgradeStatus | - | not covered |

### crypto

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/crypto/datakey/{name}` | HandleCryptoDataKey | rest.ts | covered: views/security/CryptoView.svelte |
| POST | `/api/v1/crypto/decrypt/{name}` | HandleCryptoDecrypt | rest.ts | covered: views/security/CryptoView.svelte |
| POST | `/api/v1/crypto/decrypt/{name}/batch` | HandleCryptoBatchDecrypt | - | not covered |
| POST | `/api/v1/crypto/encrypt/{name}` | HandleCryptoEncrypt | rest.ts | covered: views/security/CryptoView.svelte |
| POST | `/api/v1/crypto/encrypt/{name}/batch` | HandleCryptoBatchEncrypt | - | not covered |
| POST | `/api/v1/crypto/hash` | HandleCryptoHash | rest.ts | covered: views/security/CryptoView.svelte |
| POST | `/api/v1/crypto/random` | HandleCryptoRandom | rest.ts | covered: views/security/CryptoView.svelte |
| POST | `/api/v1/crypto/rewrap/{name}` | HandleCryptoRewrap | rest.ts | not covered |
| POST | `/api/v1/crypto/sign/{name}` | HandleCryptoSign | rest.ts | covered: views/security/CryptoView.svelte |
| POST | `/api/v1/crypto/verify/{name}` | HandleCryptoVerify | rest.ts | covered: views/security/CryptoView.svelte |

### embedding

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/embedding/batch` | HandleEmbeddingBatchGenerate | - | not covered |
| DELETE | `/api/v1/embedding/cache` | HandleEmbeddingClearCache | - | not covered |
| GET | `/api/v1/embedding/cache/stats` | HandleEmbeddingGetCacheStats | - | not covered |
| POST | `/api/v1/embedding/generate` | HandleEmbeddingGenerate | rest.ts | covered: views/ai/EmbeddingsView.svelte |
| GET | `/api/v1/embedding/providers` | HandleEmbeddingListProviders | - (rest.ts†) | covered: views/ai/EmbeddingsView.svelte |
| PUT | `/api/v1/embedding/providers/default` | HandleEmbeddingSetDefaultProvider | rest.ts | covered: views/ai/EmbeddingsView.svelte |
| PUT | `/api/v1/embedding/providers/{name}/status` | HandleEmbeddingSetProviderStatus | rest.ts | covered: views/ai/EmbeddingsView.svelte |
| GET | `/api/v1/embedding/providers/{provider}` | HandleEmbeddingGetProviderInfo | rest.ts | not covered |

### entities

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/entities/{type}/` | HandleList | - | not covered |
| POST | `/api/v1/entities/{type}/` | HandlePut | - | not covered |
| DELETE | `/api/v1/entities/{type}/{id}` | HandleDelete | - | not covered |
| GET | `/api/v1/entities/{type}/{id}` | HandleGet | - | not covered |
| PUT | `/api/v1/entities/{type}/{id}` | HandlePut | - | not covered |

### entity-types

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/entity-types` | HandleListEntityTypes | - | not covered |

### health

not-UI: liveness and readiness probes, called by load balancers and orchestrators.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/health` | HandleHealth | - | not-UI: health probe |
| GET | `/api/v1/health/live` | HandleLiveness | - | not-UI: health probe |
| GET | `/api/v1/health/ready` | HandleReadiness | - | not-UI: health probe |

### hooks

not-UI: agent-runtime hook events, posted by running sessions.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/hooks/task-completed` | HandleHookTaskCompleted | - | not-UI: agent-runtime hook |
| POST | `/api/v1/hooks/task-created` | HandleHookTaskCreated | - | not-UI: agent-runtime hook |
| POST | `/api/v1/hooks/teammate-idle` | HandleHookTeammateIdle | - | not-UI: agent-runtime hook |

### init

CeremonyView adapts the SDK ceremony client; it lists officers with its own `fetch` of
`/api/v1/init/officers`, which the SDK does not expose.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/init/claim-cert/begin` | HandleCeremonyClaimCertBegin | rest.ts | not covered |
| POST | `/api/v1/init/claim-cert/complete` | HandleCeremonyClaimCertComplete | rest.ts | covered: views/security/CeremonyView.svelte |
| POST | `/api/v1/init/enroll-external-officer` | HandleCeremonyEnrollExternalOfficer | rest.ts | covered: views/security/CeremonyView.svelte |
| POST | `/api/v1/init/initialize` | HandleCeremonyInitialize | rest.ts | not covered |
| POST | `/api/v1/init/officer-share/begin` | HandleCeremonyBeginClaimShare | rest.ts | not covered |
| POST | `/api/v1/init/officer-share/complete` | HandleCeremonyCompleteClaimShare | rest.ts | covered: views/security/CeremonyView.svelte |
| GET | `/api/v1/init/officers` | HandleCeremonyListOfficers | - | covered: views/security/CeremonyView.svelte (direct fetch, not the SDK) |
| POST | `/api/v1/init/sign-csr` | HandleCeremonySignCSR | rest.ts | not covered |
| GET | `/api/v1/init/status` | HandleCeremonyStatus | rest.ts | not covered |

### kms

RestDEKClient builds the `/api/v1/kms/deks/<path>[/action]` paths the `*` routes serve.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/kms/algorithms` | HandleGetSupportedAlgorithms | - | not covered |
| GET | `/api/v1/kms/backend/status` | HandleBackendStatus | - | not covered |
| POST | `/api/v1/kms/backend/test` | HandleBackendTest | - | not covered |
| POST | `/api/v1/kms/decrypt/{name}` | HandleKMSDecrypt | rest.ts | not covered |
| GET | `/api/v1/kms/deks/` | HandleListDEKs | rest.ts | covered: views/security/DEKView.svelte |
| POST | `/api/v1/kms/deks/` | HandleCreateDEK | rest.ts | covered: views/security/DEKView.svelte |
| DELETE | `/api/v1/kms/deks/*` | handleDEKDeleteAction | - (rest.ts†) | covered: views/security/DEKView.svelte |
| GET | `/api/v1/kms/deks/*` | handleDEKGetAction | - (rest.ts†) | covered: views/security/DEKView.svelte |
| POST | `/api/v1/kms/deks/*` | handleDEKPostAction | - (rest.ts†) | covered: views/security/DEKView.svelte |
| PUT | `/api/v1/kms/deks/*` | handleDEKPutAction | - (rest.ts†) | covered: views/security/DEKView.svelte |
| PUT | `/api/v1/kms/deks/default` | HandleSetDefaultDEKFromBody | - | not covered |
| POST | `/api/v1/kms/encrypt/{name}` | HandleKMSEncrypt | rest.ts | not covered |
| GET | `/api/v1/kms/keys` | HandleListKeys | rest.ts | covered: views/security/KMSView.svelte |
| POST | `/api/v1/kms/keys` | HandleGenerateKey | rest.ts | covered: views/security/KMSView.svelte |
| POST | `/api/v1/kms/keys/import` | HandleImportKey | - | not covered |
| POST | `/api/v1/kms/keys/unwrap` | HandleUnwrapKey | - | not covered |
| DELETE | `/api/v1/kms/keys/{name}` | HandleDeleteKey | rest.ts | covered: views/security/KMSView.svelte |
| GET | `/api/v1/kms/keys/{name}` | HandleGetKey | rest.ts | not covered |
| PUT | `/api/v1/kms/keys/{name}/default` | HandleSetDefaultKey | rest.ts | covered: views/security/KMSView.svelte |
| POST | `/api/v1/kms/keys/{name}/export` | HandleExportKey | - | not covered |
| GET | `/api/v1/kms/keys/{name}/metadata` | HandleGetKeyMetadata | - | not covered |
| PUT | `/api/v1/kms/keys/{name}/metadata` | HandleUpdateKeyMetadata | - | not covered |
| GET | `/api/v1/kms/keys/{name}/policy` | HandleGetKeyPolicy | - | not covered |
| PUT | `/api/v1/kms/keys/{name}/policy` | HandleSetKeyPolicy | - | not covered |
| GET | `/api/v1/kms/keys/{name}/public` | HandleGetPublicKey | - | not covered |
| POST | `/api/v1/kms/keys/{name}/revoke` | HandleRevokeKey | - | not covered |
| POST | `/api/v1/kms/keys/{name}/rotate` | HandleRotateKey | rest.ts | covered: views/security/KMSView.svelte |
| DELETE | `/api/v1/kms/keys/{name}/rotation-policy` | HandleDeleteKeyRotationPolicy | - | not covered |
| GET | `/api/v1/kms/keys/{name}/rotation-policy` | HandleGetKeyRotationPolicy | - | not covered |
| PUT | `/api/v1/kms/keys/{name}/rotation-policy` | HandleSetKeyRotationPolicy | - | not covered |
| DELETE | `/api/v1/kms/keys/{name}/safety` | HandleClearKeySafety | - | not covered |
| PUT | `/api/v1/kms/keys/{name}/safety` | HandleSetKeySafety | - | not covered |
| PUT | `/api/v1/kms/keys/{name}/status` | HandleUpdateKeyStatus | rest.ts | covered: views/security/KMSView.svelte |
| GET | `/api/v1/kms/keys/{name}/versions` | HandleListKeyVersions | rest.ts | covered: views/security/KMSView.svelte |
| GET | `/api/v1/kms/safety/check` | HandleSafetyCheck | - | not covered |
| GET | `/api/v1/kms/safety/report` | HandleSafetyReport | - | not covered |

### kv

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/kv/{type}/` | HandleKVList | rest.ts | covered: views/data/KVStoreView.svelte |
| DELETE | `/api/v1/kv/{type}/{key}` | HandleKVDelete | rest.ts | covered: views/data/KVStoreView.svelte |
| GET | `/api/v1/kv/{type}/{key}` | HandleKVGet | rest.ts | not covered |
| POST | `/api/v1/kv/{type}/{key}` | HandleKVPut | rest.ts | not covered |
| PUT | `/api/v1/kv/{type}/{key}` | HandleKVPut | rest.ts | covered: views/data/KVStoreView.svelte |

### messaging

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/messaging/channels` | HandleListMessagingChannels | rest.ts | covered: views/async/MessagingView.svelte |
| POST | `/api/v1/messaging/channels` | HandleCreateMessagingChannel | rest.ts | covered: views/async/MessagingView.svelte |
| DELETE | `/api/v1/messaging/channels/{name}` | HandleDeleteMessagingChannel | rest.ts | covered: views/async/MessagingView.svelte |
| GET | `/api/v1/messaging/channels/{name}` | HandleGetMessagingChannel | rest.ts | not covered |
| GET | `/api/v1/messaging/dedup/stats/{channel}` | HandleMessagingDedupStats | - | not covered |
| POST | `/api/v1/messaging/publish-delta/{channel}` | HandleMessagingPublishDelta | - | not covered |
| POST | `/api/v1/messaging/publish/{channel}` | HandleMessagingPublish | rest.ts | covered: views/async/MessagingView.svelte |
| GET | `/api/v1/messaging/stats` | HandleMessagingStats | - | not covered |
| GET | `/api/v1/messaging/stats/{channel}` | HandleMessagingChannelStats | - | not covered |
| POST | `/api/v1/messaging/subscribe` | HandleMessagingSubscribe | - | not covered |
| DELETE | `/api/v1/messaging/subscribe/{id}` | HandleMessagingUnsubscribe | - | not covered |
| POST | `/api/v1/messaging/watch` | HandleMessagingWatch | - | not covered |

### metrics

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/metrics/performance` | HandlePerformanceMetrics | - | not covered |

### ollama

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/ollama/embed` | HandleOllamaEmbed | - | not covered |
| POST | `/api/v1/ollama/embed/batch` | HandleOllamaEmbedBatch | - | not covered |
| GET | `/api/v1/ollama/health` | HandleOllamaHealth | - | not covered |
| GET | `/api/v1/ollama/models` | HandleOllamaModelList | - | not covered |
| GET | `/api/v1/ollama/models/{model}` | HandleOllamaModelInfo | - | not covered |

### fleet

RestOrchestratorClient builds these paths from `FLEET_PREFIX`. not-UI for the heartbeat: it is
posted by the fleet node's own agent.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/fleet` | HandleListFleetNodes | rest.ts | covered: stores/fleetStore.svelte.ts |
| POST | `/api/v1/fleet` | HandleRegisterFleetNode | rest.ts | covered: components/orchestrator/RegisterFleetNodeDialog.svelte |
| DELETE | `/api/v1/fleet/{nodeID}` | HandleRemoveFleetNode | - (rest.ts†) | covered: components/orchestrator/FleetList.svelte |
| GET | `/api/v1/fleet/{nodeID}` | HandleGetFleetNode | - (rest.ts†) | not covered |
| GET | `/api/v1/fleet/{nodeID}/backends/{backendName}/models` | HandleListFleetNodeBackendModels | - (rest.ts†) | covered: views/orchestrator/InferenceView.svelte |
| POST | `/api/v1/fleet/{nodeID}/disable` | HandleDisableFleetNode | - (rest.ts†) | covered: views/orchestrator/FleetNodeDetailView.svelte |
| POST | `/api/v1/fleet/{nodeID}/enable` | HandleEnableFleetNode | - (rest.ts†) | covered: views/orchestrator/FleetNodeDetailView.svelte |
| POST | `/api/v1/fleet/{nodeID}/heartbeat` | HandleFleetNodeHeartbeat | - | not-UI: fleet node heartbeat |

### orchestrator

RestOrchestratorClient builds these paths from `API_PREFIX` (`/api/v1/orchestrator`). not-UI for
`hooks`: hook events posted by running sessions. The SDK's `decomposeTask` posts to
`/api/v1/orchestrator/tasks/{taskID}/decompose`, which routes.go does not register, so the
`tasks/decompose` route is not covered (a defect for slice follow-up).

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/orchestrator/attention` | HandleListAttention | - (rest.ts†) | covered: stores/attentionStore.svelte.ts |
| POST | `/api/v1/orchestrator/attention/{taskID}/resolve` | HandleResolveAttention | - (rest.ts†) | covered: stores/attentionStore.svelte.ts |
| GET | `/api/v1/orchestrator/backends` | HandleOrchestratorListBackends | - (rest.ts†) | covered: components/orchestrator/SpawnDialog.svelte |
| GET | `/api/v1/orchestrator/cost` | HandleOrchestratorCost | - (rest.ts†) | covered: views/orchestrator/CostView.svelte |
| GET | `/api/v1/orchestrator/spend-policy` | HandleGetTenantSpendPolicy | - (rest.ts†) | covered: components/orchestrator/TenantSpendLimitsEditor.svelte |
| PUT | `/api/v1/orchestrator/spend-policy` | HandleSetTenantSpendPolicy | - (rest.ts†) | covered: components/orchestrator/TenantSpendLimitsEditor.svelte |
| GET | `/api/v1/orchestrator/cost-settings` | HandleGetCostSettings | - (rest.ts†) | covered: components/orchestrator/CostSettingsPanel.svelte |
| PUT | `/api/v1/orchestrator/cost-settings` | HandleSetCostSettings | - (rest.ts†) | covered: components/orchestrator/CostSettingsPanel.svelte |
| POST | `/api/v1/orchestrator/discover` | HandleOrchestratorDiscover | - | not covered |
| GET | `/api/v1/orchestrator/health` | HandleOrchestratorHealth | - (rest.ts†) | covered: stores/orchestrator.svelte.ts |
| POST | `/api/v1/orchestrator/hooks` | HandleOrchestratorHookEvent | - | not-UI: agent-runtime hook |
| POST | `/api/v1/orchestrator/inference` | HandleOrchestratorInference | - (rest.ts†) | covered: views/orchestrator/InferenceView.svelte |
| GET | `/api/v1/orchestrator/jobs` | HandleOrchestratorListJobs | - (rest.ts†) | covered: views/orchestrator/JobsView.svelte |
| POST | `/api/v1/orchestrator/jobs` | HandleOrchestratorStartJob | - (rest.ts†) | covered: components/orchestrator/StartJobDialog.svelte |
| DELETE | `/api/v1/orchestrator/jobs/{jobID}` | HandleOrchestratorCancelJob | - (rest.ts†) | covered: views/orchestrator/JobsView.svelte |
| GET | `/api/v1/orchestrator/jobs/{jobID}` | HandleOrchestratorGetJobStatus | - (rest.ts†) | covered: stores/orchestrator.svelte.ts |
| POST | `/api/v1/orchestrator/jobs/{jobID}/children` | HandleOrchestratorSpawnChildJob | - | not covered |
| GET | `/api/v1/orchestrator/jobs/{jobID}/events` | HandleOrchestratorSubscribeJobEvents | - (rest.ts†) | covered: stores/job-events.svelte.ts |
| GET | `/api/v1/orchestrator/runners` | HandleOrchestratorListRunners | - (rest.ts†) | covered: views/orchestrator/JobDetailView.svelte |
| GET | `/api/v1/orchestrator/sessions` | HandleOrchestratorListSessions | - (rest.ts†) | covered: stores/sessionStore.svelte.ts |
| POST | `/api/v1/orchestrator/sessions` | HandleOrchestratorSpawn | - (rest.ts†) | covered: components/orchestrator/SpawnDialog.svelte |
| DELETE | `/api/v1/orchestrator/sessions/{sessionID}` | HandleOrchestratorCloseSession | - (rest.ts†) | covered: stores/sessionStore.svelte.ts |
| GET | `/api/v1/orchestrator/sessions/{sessionID}` | HandleOrchestratorSessionStatus | - | not covered |
| GET | `/api/v1/orchestrator/sessions/{sessionID}/events` | HandleOrchestratorSessionEvents | - (rest.ts†) | covered: stores/sessionStore.svelte.ts |
| GET | `/api/v1/orchestrator/sessions/{sessionID}/history` | HandleOrchestratorSessionHistory | - (rest.ts†) | covered: stores/sessionStore.svelte.ts |
| POST | `/api/v1/orchestrator/sessions/{sessionID}/prompt` | HandleOrchestratorPrompt | - (rest.ts†) | covered: components/orchestrator/PromptComposer.svelte |
| POST | `/api/v1/orchestrator/sessions/{sessionID}/rotate-account` | HandleOrchestratorRotateSessionAccount | - (rest.ts†) | covered: components/orchestrator/SessionDetail.svelte |
| GET | `/api/v1/orchestrator/tasks` | HandleOrchestratorListTasks | - (rest.ts†) | covered: views/orchestrator/TasksView.svelte |
| POST | `/api/v1/orchestrator/tasks` | HandleOrchestratorCreateTask | - (rest.ts†) | covered: components/orchestrator/CreateTaskDialog.svelte |
| POST | `/api/v1/orchestrator/tasks/decompose` | HandleOrchestratorDecomposeTask | - | not covered |
| GET | `/api/v1/orchestrator/tasks/{taskID}` | HandleOrchestratorGetTask | - (rest.ts†) | not covered |
| DELETE | `/api/v1/orchestrator/tasks/{taskID}` | HandleOrchestratorDeleteTask | - (rest.ts†) | not covered |
| GET | `/api/v1/orchestrator/tasks/{taskID}/artifacts` | HandleOrchestratorListArtifacts | - | not covered |
| POST | `/api/v1/orchestrator/tasks/{taskID}/artifacts` | HandleOrchestratorSubmitArtifacts | - | not covered |
| GET | `/api/v1/orchestrator/tasks/{taskID}/artifacts/{name}` | HandleOrchestratorGetArtifact | - | not covered |
| POST | `/api/v1/orchestrator/tasks/{taskID}/claim` | HandleOrchestratorClaimTask | - (rest.ts†) | covered: views/orchestrator/TasksView.svelte |
| POST | `/api/v1/orchestrator/tasks/{taskID}/complete` | HandleOrchestratorCompleteTask | - (rest.ts†) | covered: views/orchestrator/TasksView.svelte |
| POST | `/api/v1/orchestrator/tasks/{taskID}/block` | HandleOrchestratorBlockTask | - | not covered |
| POST | `/api/v1/orchestrator/tasks/{taskID}/readmit` | HandleOrchestratorReadmitTask | - (rest.ts†) | covered: views/orchestrator/JobDetailView.svelte |
| GET | `/api/v1/orchestrator/workflows` | HandleOrchestratorListWorkflows | - (rest.ts†) | covered: views/orchestrator/WorkflowsView.svelte |
| POST | `/api/v1/orchestrator/workflows` | HandleOrchestratorCreateWorkflow | - (rest.ts†) | covered: views/orchestrator/WorkflowsView.svelte |
| DELETE | `/api/v1/orchestrator/workflows/{name}` | HandleOrchestratorDeleteWorkflow | - (rest.ts†) | covered: views/orchestrator/WorkflowsView.svelte |
| GET | `/api/v1/orchestrator/workflows/{name}` | HandleOrchestratorGetWorkflow | - (rest.ts†) | not covered |
| PUT | `/api/v1/orchestrator/workflows/{name}` | HandleOrchestratorUpdateWorkflow | - (rest.ts†) | covered: views/orchestrator/WorkflowsView.svelte |

### agents

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/agents` | HandleListAgents | rest.ts | covered: components/orchestrator/StartJobDialog.svelte |
| POST | `/api/v1/agents` | HandleCreateAgent | rest.ts | not covered |
| GET | `/api/v1/agents/{agentID}` | HandleGetAgent | rest.ts | covered: components/orchestrator/AgentSpendLimitsEditor.svelte |
| PUT | `/api/v1/agents/{agentID}` | HandleUpdateAgent | rest.ts | covered: components/orchestrator/AgentSpendLimitsEditor.svelte |
| DELETE | `/api/v1/agents/{agentID}` | HandleDeleteAgent | rest.ts | not covered |
| GET | `/api/v1/agents/{agentID}/used-by` | HandleAgentUsedBy | rest.ts | not covered |

### agent-pools

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/agent-pools` | HandleListAgentPools | rest.ts | covered: components/orchestrator/StartJobDialog.svelte |
| POST | `/api/v1/agent-pools` | HandleCreateAgentPool | rest.ts | not covered |
| GET | `/api/v1/agent-pools/{poolID}` | HandleGetAgentPool | rest.ts | not covered |
| PUT | `/api/v1/agent-pools/{poolID}` | HandleUpdateAgentPool | rest.ts | not covered |
| DELETE | `/api/v1/agent-pools/{poolID}` | HandleDeleteAgentPool | rest.ts | not covered |
| GET | `/api/v1/agent-pools/{poolID}/used-by` | HandleAgentPoolUsedBy | rest.ts | not covered |

### parameters

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/parameters/` | HandleListParameters | - | not covered |
| DELETE | `/api/v1/parameters/*` | HandleDeleteParameter | - | not covered |
| GET | `/api/v1/parameters/*` | HandleGetParameter | - | not covered |
| PUT | `/api/v1/parameters/*` | HandleSetParameter | - | not covered |
| POST | `/api/v1/parameters/batch/delete` | HandleDeleteParametersBatch | - | not covered |
| POST | `/api/v1/parameters/batch/get` | HandleGetParametersBatch | - | not covered |
| POST | `/api/v1/parameters/batch/set` | HandleSetParametersBatch | - | not covered |
| POST | `/api/v1/parameters/search` | HandleSearchParametersByTags | - | not covered |

### projects

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/projects/` | HandleListProjects | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| POST | `/api/v1/projects/` | HandleCreateProject | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| GET | `/api/v1/projects/by-name/{name}` | HandleGetProjectByName | - | not covered |
| DELETE | `/api/v1/projects/{id}` | HandleDeleteProject | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| GET | `/api/v1/projects/{id}` | HandleGetProject | rest.ts | not covered |
| PUT | `/api/v1/projects/{id}` | HandleUpdateProject | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| DELETE | `/api/v1/projects/{id}/permissions/{node_id}` | HandleRevokeFleetNodeRole | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| PUT | `/api/v1/projects/{id}/permissions/{node_id}` | HandleGrantFleetNodeRole | rest.ts | covered: views/orchestrator/ProjectsView.svelte |

### prompts

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/prompts/{set}` | HandleListPrompts | - | not covered |
| DELETE | `/api/v1/prompts/{set}/{name}` | HandleResetPrompt | - | not covered |
| GET | `/api/v1/prompts/{set}/{name}` | HandleGetPrompt | - | not covered |
| PUT | `/api/v1/prompts/{set}/{name}` | HandlePutPrompt | - | not covered |
| GET | `/api/v1/prompts/{set}/{name}/diff` | HandleDiffPrompt | - | not covered |
| GET | `/api/v1/prompts/{set}/{name}/effective` | HandleGetEffectivePrompt | - | not covered |
| GET | `/api/v1/prompts/{set}/{name}/history` | HandleGetPromptHistory | - | not covered |
| POST | `/api/v1/prompts/{set}/{name}/rollback` | HandleRollbackPrompt | - | not covered |

### query

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/query/range` | HandleRangeQuery | - | not covered |
| GET | `/api/v1/query/{type}/{index}` | HandleQuery | - | not covered |

### registry

not-UI: the service registry, called by service instances to register, renew and resolve.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/registry/instances` | HandleRegistryRegister | - | not-UI: service registry |
| POST | `/api/v1/registry/leases/{lease_id}/renew` | HandleRegistryRenew | - | not-UI: service registry |
| DELETE | `/api/v1/registry/leases/{lease_id}` | HandleRegistryDeregister | - | not-UI: service registry |
| GET | `/api/v1/registry/services/{service}` | HandleRegistryResolve | - | not-UI: service registry |
| GET | `/api/v1/registry/services/{service}/watch` | HandleRegistryWatch | - | not-UI: service registry |

### scheduler

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/scheduler/jobs` | HandleListSchedulerJobs | rest.ts | covered: views/async/SchedulerView.svelte |
| POST | `/api/v1/scheduler/jobs` | HandleCreateSchedulerJob | rest.ts | covered: views/async/SchedulerView.svelte |
| DELETE | `/api/v1/scheduler/jobs/{id}` | HandleDeleteSchedulerJob | rest.ts | covered: views/async/SchedulerView.svelte |
| GET | `/api/v1/scheduler/jobs/{id}` | HandleGetSchedulerJob | rest.ts | not covered |
| PUT | `/api/v1/scheduler/jobs/{id}` | HandleUpdateSchedulerJob | rest.ts | covered: views/async/SchedulerView.svelte |
| POST | `/api/v1/scheduler/jobs/{id}/cancel` | HandleCancelSchedulerJob | - | not covered |
| GET | `/api/v1/scheduler/jobs/{id}/history` | HandleGetSchedulerJobHistory | - | not covered |
| POST | `/api/v1/scheduler/jobs/{id}/pause` | HandlePauseSchedulerJob | rest.ts | covered: views/async/SchedulerView.svelte |
| POST | `/api/v1/scheduler/jobs/{id}/resume` | HandleResumeSchedulerJob | rest.ts | covered: views/async/SchedulerView.svelte |
| POST | `/api/v1/scheduler/jobs/{id}/trigger` | HandleTriggerSchedulerJob | rest.ts | covered: views/async/SchedulerView.svelte |
| GET | `/api/v1/scheduler/status` | HandleGetSchedulerStatus | rest.ts | not covered |
| POST | `/api/v1/scheduler/nodes` | HandleRegisterPlacementNode | rest.ts | not covered |
| GET | `/api/v1/scheduler/nodes` | HandleListPlacementNodes | rest.ts | not covered |
| GET | `/api/v1/scheduler/nodes/{id}` | HandleGetPlacementNode | rest.ts | not covered |
| DELETE | `/api/v1/scheduler/nodes/{id}` | HandleDeregisterPlacementNode | rest.ts | not covered |
| POST | `/api/v1/scheduler/nodes/{id}/renew` | HandleRenewPlacementNode | rest.ts | not covered |
| POST | `/api/v1/scheduler/nodes/{id}/cordon` | HandleCordonPlacementNode | rest.ts | not covered |
| POST | `/api/v1/scheduler/nodes/{id}/uncordon` | HandleUncordonPlacementNode | rest.ts | not covered |
| POST | `/api/v1/scheduler/nodes/{id}/drain` | HandleDrainPlacementNode | rest.ts | not covered |
| POST | `/api/v1/scheduler/placements` | HandlePlaceWorkload | rest.ts | not covered |
| GET | `/api/v1/scheduler/placements` | HandleListPlacements | rest.ts | not covered |
| GET | `/api/v1/scheduler/placements/{id}` | HandleGetPlacement | rest.ts | not covered |
| DELETE | `/api/v1/scheduler/placements/{id}` | HandleReleasePlacement | rest.ts | not covered |

### sdlc

not-UI for the webhook: it is called by the forge.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/sdlc/webhook/{projectID}` | HandleSDLCWebhook | - | not-UI: forge webhook |
| GET | `/api/v1/sdlc/forges` | HandleListSDLCForges | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| GET | `/api/v1/sdlc/projects/{projectID}/binding` | HandleGetSDLCBinding | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| PUT | `/api/v1/sdlc/projects/{projectID}/binding` | HandleBindSDLCProject | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| DELETE | `/api/v1/sdlc/projects/{projectID}/binding` | HandleUnbindSDLCProject | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| POST | `/api/v1/sdlc/projects/{projectID}/onboarding` | HandleOnboardSDLCProject | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| POST | `/api/v1/sdlc/repos/{repo}/onboarding` | HandleOnboardSDLCRepo | - | not covered |
| GET | `/api/v1/sdlc/projects/{projectID}/conformance` | HandleGetSDLCConformance | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| GET | `/api/v1/sdlc/projects/{projectID}/open-work` | HandleGetSDLCOpenWork | rest.ts | not covered |
| GET | `/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer` | HandleGetSDLCReviewer | rest.ts | covered: components/orchestrator/SdlcReviewerPicker.svelte |
| PUT | `/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer` | HandleSetSDLCReviewer | rest.ts | covered: components/orchestrator/SdlcReviewerPicker.svelte |
| DELETE | `/api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer` | HandleClearSDLCReviewer | rest.ts | covered: components/orchestrator/SdlcReviewerPicker.svelte |
| POST | `/api/v1/sdlc/projects/{projectID}/issues` | HandleFileSDLCIssue | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| GET | `/api/v1/sdlc/projects/{projectID}/collaborators` | HandleListSDLCCollaborators | rest.ts | covered: views/orchestrator/ProjectsView.svelte |
| POST | `/api/v1/sdlc/projects/{projectID}/adopt` | HandleAdoptSDLCIssue | rest.ts | not covered |
| GET | `/api/v1/sdlc/tasks/{taskID}/delivery` | HandleGetSDLCDelivery | rest.ts | not covered |
| GET | `/api/v1/sdlc/worker-image` | HandleGetSDLCWorkerImage | rest.ts | covered: stores/workerImage.svelte.ts |
| POST | `/api/v1/sdlc/worker-image/refresh` | HandleRefreshSDLCWorkerImage | - (rest.ts†) | covered: stores/workerImage.svelte.ts |

### secrets-prefix, secrets-versions, secrets-watch

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/secrets-prefix/*` | HandleGetSecretsByPrefix | - | not covered |
| GET | `/api/v1/secrets-versions/*` | HandleListSecretVersions | - (rest.ts†) | covered: views/security/SecretsView.svelte |
| GET | `/api/v1/secrets-watch/*` | HandleWatchSecrets | - | not covered |

### secrets

RestSecretsClient builds `/api/v1/secrets/<path>` for the `*` routes (POST stores, and with
`?action=rotate` rotates).

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/secrets/` | HandleListSecrets | rest.ts | covered: views/security/SecretsView.svelte |
| DELETE | `/api/v1/secrets/*` | HandleDeleteSecret | - (rest.ts†) | covered: views/security/SecretsView.svelte |
| GET | `/api/v1/secrets/*` | HandleGetSecretOrVersions | - (rest.ts†) | covered: views/security/SecretsView.svelte |
| POST | `/api/v1/secrets/*` | handleSecretsPostAction | - (rest.ts†) | covered: views/security/SecretsView.svelte |
| PUT | `/api/v1/secrets/*` | handleSecretsPutAction | - | not covered |
| POST | `/api/v1/secrets/dynamic/credentials/{role}` | HandleGenerateDynamicCredential | rest.ts | covered: views/security/DynamicSecretsView.svelte |
| GET | `/api/v1/secrets/dynamic/leases` | HandleListDynamicSecretLeases | - (rest.ts†) | covered: views/security/DynamicSecretsView.svelte |
| DELETE | `/api/v1/secrets/dynamic/leases/{id}` | HandleRevokeDynamicSecretLease | rest.ts | covered: views/security/DynamicSecretsView.svelte |
| POST | `/api/v1/secrets/dynamic/leases/{id}/renew` | HandleRenewDynamicSecretLease | rest.ts | covered: views/security/DynamicSecretsView.svelte |
| GET | `/api/v1/secrets/dynamic/roles` | HandleListDynamicSecretRoles | rest.ts | covered: views/security/DynamicSecretsView.svelte |
| POST | `/api/v1/secrets/dynamic/roles` | HandleCreateDynamicSecretRole | rest.ts | covered: views/security/DynamicSecretsView.svelte |
| DELETE | `/api/v1/secrets/dynamic/roles/{name}` | HandleDeleteDynamicSecretRole | rest.ts | covered: views/security/DynamicSecretsView.svelte |
| GET | `/api/v1/secrets/dynamic/roles/{name}` | HandleGetDynamicSecretRole | rest.ts | not covered |

### ssh

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/ssh/authorized` | HandleEmitAuthorized | rest.ts | not covered |
| GET | `/api/v1/ssh/authorized/keys` | HandleListAuthorizedKeys | - | not covered |
| POST | `/api/v1/ssh/authorized/keys` | HandleAddAuthorizedKey | - | not covered |
| DELETE | `/api/v1/ssh/authorized/keys/{scope}/{owner}/{name}` | HandleRemoveAuthorizedKey | - | not covered |
| GET | `/api/v1/ssh/authorized/keys/{scope}/{owner}/{name}` | HandleGetAuthorizedKey | - | not covered |
| POST | `/api/v1/ssh/authorized/verify` | HandleVerifyAuthorized | rest.ts | not covered |
| GET | `/api/v1/ssh/keys` | HandleListSSHKeys | rest.ts | covered: views/security/SSHKeysView.svelte |
| POST | `/api/v1/ssh/keys` | HandleGenerateSSHKey | rest.ts | covered: views/security/ssh/GenerateKeyModal.svelte |
| POST | `/api/v1/ssh/keys/import` | HandleImportSSHKey | rest.ts | covered: views/security/ssh/ImportKeyModal.svelte |
| DELETE | `/api/v1/ssh/keys/{scope}/{owner}/{name}` | HandleDeleteSSHKey | rest.ts | covered: views/security/SSHKeysView.svelte |
| GET | `/api/v1/ssh/keys/{scope}/{owner}/{name}` | HandleGetSSHKey | rest.ts | not covered |
| GET | `/api/v1/ssh/keys/{scope}/{owner}/{name}/public` | HandleExportSSHKeyPublic | rest.ts | not covered |
| POST | `/api/v1/ssh/keys/{scope}/{owner}/{name}/sign` | HandleSignSSHDigest | rest.ts | not covered |
| GET | `/api/v1/ssh/known-hosts` | HandleListKnownHosts | rest.ts | covered: views/security/ssh/KnownHostsPanel.svelte |
| POST | `/api/v1/ssh/known-hosts` | HandleAddKnownHost | rest.ts | covered: views/security/ssh/KnownHostsPanel.svelte |
| DELETE | `/api/v1/ssh/known-hosts/{host}` | HandleRemoveKnownHost | rest.ts | covered: views/security/ssh/KnownHostsPanel.svelte |
| POST | `/api/v1/ssh/known-hosts/{host}/verify` | HandleVerifyKnownHost | rest.ts | not covered |

### timeseries

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/timeseries/metrics` | HandleListMetrics | rest.ts | covered: views/data/TimeSeriesView.svelte |
| POST | `/api/v1/timeseries/query` | HandleTSQuery | rest.ts | covered: views/data/TimeSeriesView.svelte |
| GET | `/api/v1/timeseries/retention/{metric}` | HandleGetRetentionPolicy | - | not covered |
| PUT | `/api/v1/timeseries/retention/{metric}` | HandleSetRetentionPolicy | - | not covered |
| DELETE | `/api/v1/timeseries/series` | HandleDeleteSeries | rest.ts | covered: views/data/TimeSeriesView.svelte |
| POST | `/api/v1/timeseries/write` | HandleWritePoint | rest.ts | covered: views/data/TimeSeriesView.svelte |
| POST | `/api/v1/timeseries/write-batch` | HandleWriteBatch | - | not covered |

### transaction

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/transaction` | HandleTransaction | - | not covered |

### trust

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/trust/auto-import-aia` | HandleTrustAutoImportAIA | rest.ts | covered: views/security/TrustView.svelte |
| GET | `/api/v1/trust/cross-signed-roots` | HandleTrustCrossSignedRootPool | rest.ts | covered: views/security/TrustView.svelte |
| POST | `/api/v1/trust/publish-dane` | HandleTrustPublishDANE | rest.ts | covered: views/security/TrustView.svelte |

### vector

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/vector/admin/compact` | HandleCompactNamespace | - | not covered |
| GET | `/api/v1/vector/admin/compact/progress` | HandleGetCompactionProgress | - | not covered |
| POST | `/api/v1/vector/admin/export/{namespace}` | HandleExportVectors | - | not covered |
| POST | `/api/v1/vector/admin/import/{namespace}` | HandleImportVectors | - | not covered |
| GET | `/api/v1/vector/admin/stats/global` | HandleGetGlobalStats | - | not covered |
| GET | `/api/v1/vector/cluster/stats` | HandleGetGlobalStats | - | not covered |
| GET | `/api/v1/vector/health` | HandleVectorHealth | - | not covered |
| POST | `/api/v1/vector/hybrid-search` | HandleHybridSearch | - | not covered |
| GET | `/api/v1/vector/namespaces/` | HandleListNamespaces | rest.ts | covered: views/data/VectorsView.svelte |
| POST | `/api/v1/vector/namespaces/` | HandleCreateNamespace | rest.ts | covered: views/data/VectorsView.svelte |
| DELETE | `/api/v1/vector/namespaces/{namespace}` | HandleDeleteNamespace | rest.ts | covered: views/data/VectorsView.svelte |
| GET | `/api/v1/vector/namespaces/{namespace}` | HandleGetNamespaceInfo | rest.ts | not covered |
| PUT | `/api/v1/vector/namespaces/{namespace}` | HandleUpdateNamespace | rest.ts | not covered |
| GET | `/api/v1/vector/namespaces/{namespace}/index/progress` | HandleGetIndexProgress | - | not covered |
| POST | `/api/v1/vector/namespaces/{namespace}/index/rebuild` | HandleRebuildIndex | - | not covered |
| POST | `/api/v1/vector/namespaces/{namespace}/search/by-id/{id}` | HandleSearchByID | - | not covered |
| POST | `/api/v1/vector/namespaces/{namespace}/search/similar` | HandleSearchSimilar | rest.ts | covered: views/data/VectorsView.svelte |
| GET | `/api/v1/vector/namespaces/{namespace}/stats` | HandleGetNamespaceStats | - | not covered |
| GET | `/api/v1/vector/namespaces/{namespace}/vectors/` | HandleListVectors | - | not covered |
| POST | `/api/v1/vector/namespaces/{namespace}/vectors/` | HandleUpsertVector | - | not covered |
| DELETE | `/api/v1/vector/namespaces/{namespace}/vectors/batch` | HandleBatchDeleteVectors | - | not covered |
| POST | `/api/v1/vector/namespaces/{namespace}/vectors/batch` | HandleBatchUpsertVectors | - | not covered |
| DELETE | `/api/v1/vector/namespaces/{namespace}/vectors/{id}` | HandleDeleteVector | - | not covered |
| GET | `/api/v1/vector/namespaces/{namespace}/vectors/{id}` | HandleGetVector | - | not covered |
| PUT | `/api/v1/vector/namespaces/{namespace}/vectors/{id}` | HandleUpsertVector | - | not covered |
| PUT | `/api/v1/vector/namespaces/{namespace}/vectors/{id}/metadata` | HandleUpdateMetadata | - | not covered |
| POST | `/api/v1/vector/search-by-metadata` | HandleSearchByMetadata | - | not covered |

### version

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/version` | HandleVersion | - | not covered |

### xkms

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/xkms/backends` | HandleXKMSListBackends | - | not covered |
| GET | `/api/v1/xkms/backends/{id}` | HandleXKMSGetBackend | - | not covered |
| GET | `/api/v1/xkms/ca/bundle` | HandleXKMSGetCABundle | - | not covered |
| GET | `/api/v1/xkms/ca/cert` | HandleXKMSGetCACertificate | - | not covered |
| POST | `/api/v1/xkms/ca/crl` | HandleXKMSGenerateCRL | rest.ts | covered: views/security/CertificatesView.svelte |
| POST | `/api/v1/xkms/ca/issue` | HandleXKMSIssueCertificate | - | not covered |
| POST | `/api/v1/xkms/ca/revoke` | HandleXKMSRevokeCertificate | - | not covered |
| GET | `/api/v1/xkms/ca/revoked/{serial}` | HandleXKMSIsRevoked | rest.ts | covered: views/security/CertificatesView.svelte |
| POST | `/api/v1/xkms/ca/sign` | HandleXKMSSignCSR | - | not covered |
| GET | `/api/v1/xkms/certs` | HandleXKMSListCerts | - | not covered |
| POST | `/api/v1/xkms/certs` | HandleXKMSSaveCert | - | not covered |
| DELETE | `/api/v1/xkms/certs/{id}` | HandleXKMSDeleteCert | - | not covered |
| GET | `/api/v1/xkms/certs/{id}` | HandleXKMSGetCert | - | not covered |
| HEAD | `/api/v1/xkms/certs/{id}` | HandleXKMSCertExists | - | not covered |
| POST | `/api/v1/xkms/fido2/authenticate/begin` | HandleXKMSFIDO2BeginAuthentication | - | not covered |
| POST | `/api/v1/xkms/fido2/authenticate/finish` | HandleXKMSFIDO2FinishAuthentication | - | not covered |
| POST | `/api/v1/xkms/fido2/register/begin` | HandleXKMSFIDO2BeginRegistration | - | not covered |
| POST | `/api/v1/xkms/fido2/register/finish` | HandleXKMSFIDO2FinishRegistration | - | not covered |
| POST | `/api/v1/xkms/fido2/credentials/begin` | HandleXKMSFIDO2BeginAddCredential | - | not covered |
| GET | `/api/v1/xkms/health` | HandleXKMSHealth | - | not covered |
| GET | `/api/v1/xkms/keys` | HandleXKMSListKeys | - | not covered |
| POST | `/api/v1/xkms/keys` | HandleXKMSGenerateKey | - | not covered |
| DELETE | `/api/v1/xkms/keys/{id}` | HandleXKMSDeleteKey | - | not covered |
| GET | `/api/v1/xkms/keys/{id}` | HandleXKMSGetKey | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/decrypt` | HandleXKMSDecrypt | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/encrypt` | HandleXKMSEncrypt | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/rotate` | HandleXKMSRotateKey | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/sign` | HandleXKMSSign | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/verify` | HandleXKMSVerify | - | not covered |
| GET | `/api/v1/xkms/keys/{id}/versions` | HandleXKMSListKeyVersions | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/versions/disable-all` | HandleXKMSDisableAllKeyVersions | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/versions/enable-all` | HandleXKMSEnableAllKeyVersions | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/versions/{version}/disable` | HandleXKMSDisableKeyVersion | - | not covered |
| POST | `/api/v1/xkms/keys/{id}/versions/{version}/enable` | HandleXKMSEnableKeyVersion | - | not covered |
| GET | `/api/v1/xkms/pin/lockout` | HandleXKMSGetLockoutStatus | - | not covered |
| POST | `/api/v1/xkms/pin/lockout/reset` | HandleXKMSResetLockout | - | not covered |
| POST | `/api/v1/xkms/pin/so/change` | HandleXKMSChangeSOPIN | - | not covered |
| POST | `/api/v1/xkms/pin/so/set` | HandleXKMSSetSOPIN | - | not covered |
| POST | `/api/v1/xkms/pin/so/verify` | HandleXKMSVerifySOPIN | - | not covered |
| POST | `/api/v1/xkms/pin/user/change` | HandleXKMSChangeUserPIN | - | not covered |
| POST | `/api/v1/xkms/pin/user/set` | HandleXKMSSetUserPIN | - | not covered |
| POST | `/api/v1/xkms/pin/user/verify` | HandleXKMSVerifyUserPIN | - | not covered |
| GET | `/api/v1/xkms/piv/slots` | HandleXKMSListPIVSlots | - | not covered |
| DELETE | `/api/v1/xkms/piv/slots/{slot}/cert` | HandleXKMSDeletePIVCertificate | - | not covered |
| GET | `/api/v1/xkms/piv/slots/{slot}/cert` | HandleXKMSGetPIVCertificate | - | not covered |
| PUT | `/api/v1/xkms/piv/slots/{slot}/cert` | HandleXKMSStorePIVCertificate | - | not covered |
| POST | `/api/v1/xkms/piv/slots/{slot}/csr` | HandleXKMSGeneratePIVCSR | - | not covered |
| POST | `/api/v1/xkms/piv/slots/{slot}/key` | HandleXKMSGeneratePIVKey | - | not covered |
| POST | `/api/v1/xkms/seal` | HandleXKMSSeal | - | not covered |
| GET | `/api/v1/xkms/seal/capability` | HandleXKMSCanSeal | - | not covered |
| POST | `/api/v1/xkms/unseal` | HandleXKMSUnseal | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/authorized` | HandleEmitAuthorized | - | not covered |
| GET | `/api/v1/xkms/v1/ssh/authorized/keys` | HandleListAuthorizedKeys | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/authorized/keys` | HandleAddAuthorizedKey | - | not covered |
| DELETE | `/api/v1/xkms/v1/ssh/authorized/keys/{scope}/{owner}/{name}` | HandleRemoveAuthorizedKey | - | not covered |
| GET | `/api/v1/xkms/v1/ssh/authorized/keys/{scope}/{owner}/{name}` | HandleGetAuthorizedKey | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/authorized/verify` | HandleVerifyAuthorized | - | not covered |
| GET | `/api/v1/xkms/v1/ssh/keys` | HandleListSSHKeys | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/keys` | HandleGenerateSSHKey | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/keys/import` | HandleImportSSHKey | - | not covered |
| DELETE | `/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}` | HandleDeleteSSHKey | - | not covered |
| GET | `/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}` | HandleGetSSHKey | - | not covered |
| GET | `/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}/public` | HandleExportSSHKeyPublic | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/keys/{scope}/{owner}/{name}/sign` | HandleSignSSHDigest | - | not covered |
| GET | `/api/v1/xkms/v1/ssh/known-hosts` | HandleListKnownHosts | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/known-hosts` | HandleAddKnownHost | - | not covered |
| DELETE | `/api/v1/xkms/v1/ssh/known-hosts/{host}` | HandleRemoveKnownHost | - | not covered |
| POST | `/api/v1/xkms/v1/ssh/known-hosts/{host}/verify` | HandleVerifyKnownHost | - | not covered |

### admin

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/admin/object-backends` | HandleCreateObjectBackend | - | not covered |
| DELETE | `/api/v1/admin/object-backends/{name}` | HandleDeleteObjectBackend | - | not covered |
| PUT | `/api/v1/admin/object-backends/{name}` | HandleUpdateObjectBackend | - | not covered |
| POST | `/api/v1/admin/object-backends/{name}/compact` | HandleCompactObjectBackend | - | not covered |
| PUT | `/api/v1/admin/object-backends/{name}/status` | HandleSetObjectBackendStatus | - | not covered |
| POST | `/api/v1/admin/objects/repair` | HandleRepairObject | - | not covered |
| POST | `/api/v1/admin/storage/gc` | HandleRunGarbageCollection | - | not covered |
| GET | `/api/v1/admin/storage/gc/progress` | HandleGetGarbageCollectionProgress | - | not covered |
| GET | `/api/v1/admin/storage/stats` | HandleGetStorageStats | - | not covered |

### backups

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/backups` | HandleListBackups | - | not covered |
| POST | `/api/v1/backups` | HandleCreateBackup | - | not covered |
| DELETE | `/api/v1/backups/{id}` | HandleDeleteBackup | - | not covered |
| POST | `/api/v1/backups/{id}/restore` | HandleRestoreBackup | - | not covered |
| GET | `/api/v1/backups/{id}/status` | HandleBackupStatus | - | not covered |
| POST | `/api/v1/backups/{id}/verify` | HandleVerifyBackup | - | not covered |

### object-backends

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/object-backends` | HandleListObjectBackends | - | not covered |
| GET | `/api/v1/object-backends/{name}/health` | HandleGetObjectBackendHealth | - | not covered |

### object-transfer

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/api/v1/object-transfer/fetch` | HandleFetchObject | - | not covered |
| POST | `/api/v1/object-transfer/stream-fetch` | HandleStreamFetchObjects | - | not covered |

### objects

RestObjectsClient builds `/api/v1/objects/<key>` for the `*` routes (PUT stores).

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/objects/` | HandleListObjects | - (rest.ts†) | covered: views/data/ObjectsView.svelte |
| DELETE | `/api/v1/objects/*` | handleObjectAction | - (rest.ts†) | covered: views/data/ObjectsView.svelte |
| GET | `/api/v1/objects/*` | handleObjectAction | - (rest.ts†) | covered: views/data/ObjectsView.svelte |
| HEAD | `/api/v1/objects/*` | handleObjectAction | - | not covered |
| POST | `/api/v1/objects/*` | handleObjectAction | - | not covered |
| PUT | `/api/v1/objects/*` | handleObjectAction | - (rest.ts†) | covered: views/data/ObjectsView.svelte |
| POST | `/api/v1/objects/batch` | HandleBatchPutObjects | - | not covered |
| POST | `/api/v1/objects/batch/delete` | HandleBatchDeleteObjects | - | not covered |
| POST | `/api/v1/objects/batch/get` | HandleBatchGetObjects | - | not covered |
| GET | `/api/v1/objects/by-tag` | HandleListByTag | - | not covered |
| GET | `/api/v1/objects/by-tenant/{tenant}` | HandleListByTenant | - | not covered |

### tenants

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| GET | `/api/v1/tenants/{tenant}/quotas` | HandleGetTenantQuotas | - | not covered |
| PUT | `/api/v1/tenants/{tenant}/quotas` | HandleSetTenantQuotas | - | not covered |
| GET | `/api/v1/tenants/{tenant}/usage` | HandleGetTenantUsage | - | not covered |

### /v1 (unprefixed barrier and ceremony routes)

not-UI: unprefixed `/v1` wire routes for the CLI and node init; the console uses the
`/api/v1/barrier` and `/api/v1/init` routes.

| Method | Path | Handler | SDK file | Console |
|---|---|---|---|---|
| POST | `/v1/barrier/enable-encryption` | HandleBarrierWireEnableEncryption | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/initialize` | HandleBarrierInitialize | - | not-UI: unprefixed /v1 wire route |
| DELETE | `/v1/barrier/recovery` | HandleBarrierRecoveryDelete | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/recovery/generate` | HandleBarrierRecoveryGenerate | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/recovery/recover` | HandleBarrierRecoveryRecover | - | not-UI: unprefixed /v1 wire route |
| GET | `/v1/barrier/recovery/status` | HandleBarrierRecoveryStatus | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/rekey` | HandleBarrierRekey | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/root-token` | HandleBarrierRootToken | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/seal` | HandleBarrierSeal | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/shamir/initialize` | HandleBarrierShamirInitialize | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/shamir/unseal-share` | HandleBarrierShamirUnsealShare | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/shamir/unseal-shares` | HandleBarrierShamirUnsealShares | - | not-UI: unprefixed /v1 wire route |
| GET | `/v1/barrier/status` | HandleBarrierStatus | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/barrier/unseal` | HandleBarrierUnseal | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/ceremony/claim-cert/begin` | HandleCeremonyClaimCertBegin | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/ceremony/claim-cert/complete` | HandleCeremonyClaimCertComplete | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/ceremony/initialize` | HandleCeremonyInitialize | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/ceremony/officer-share/begin` | HandleCeremonyBeginClaimShare | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/ceremony/officer-share/complete` | HandleCeremonyCompleteClaimShare | - | not-UI: unprefixed /v1 wire route |
| GET | `/v1/ceremony/officers` | HandleCeremonyListOfficers | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/ceremony/sign-csr` | HandleCeremonySignCSR | - | not-UI: unprefixed /v1 wire route |
| GET | `/v1/ceremony/status` | HandleCeremonyStatus | - | not-UI: unprefixed /v1 wire route |
| POST | `/v1/init/enroll-external-officer` | HandleCeremonyEnrollExternalOfficer | - | not-UI: unprefixed /v1 wire route |

## Still to audit

- The IAM routes registered outside `routes.go` (users, tenants, roles, policies, auth, invites
  and the tenant barrier), which IAMView, TenantBarrierView, LoginView and RegisterView call
  (#2372 slice 2).
- The gRPC RPCs the console reaches through the SDK's gRPC-web and Wails transports (#2372
  slice 2).
- The desktop (Wails) bindings (#2372 slice 3).
- RAG (`/api/v1/rag/...`) and the orchestrator's `deployFleetNode`: the SDK and the console call
  them, but their routes are not in the routes.go table above.
