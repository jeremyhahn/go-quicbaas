# Accounts — Engineering

> **Status: NORMATIVE** · Last verified: 2026-10-11
> Exported surface, invariants, errors, configuration, tests and known gaps. Feature folder: `docs/accounts/`. Conflicts with this document are defects.

The design is in [architecture.md](architecture.md). This page is the
contract a change has to keep. All code is in `pkg/orchestration` unless a path
says otherwise.

## Exported surface

### Account pool

| Symbol | File | Notes |
| --- | --- | --- |
| `Account`, `AccountStatus`, `AccountRefreshPolicy` | `account_types.go` | Metadata only; no credential field. `MarshalJSON` adds the derived `credential_health`. |
| `AccountCredential` | `account_types.go` | The envelope. `Generation` names the copy. |
| `AccountCredentialExpiryWarning` | `account_types.go:126` | 24h: the `expiring` health threshold |
| `AccountStore`, `UpdateIfUnchanged` | `account_store.go` | Compare-and-swap writes; five re-apply attempts in the service |
| `AccountSecretStore` | `account_secret_store.go` | `Replace` stamps the generation |
| `AccountResolver`, `ResolvedAccountCredential` | `account_resolver.go:146` | `maxClaimAttempts` = 8 (`:210`) |
| `AccountStrategyRegistry`, `NewBalancedAccountStrategyRegistry` | `account_strategy*.go` | `least_usage` (default), `static` |
| `AccountSessionReserver`, `NewAccountSessionCounter` | `account_balancing.go:223` | `AccountSessionReservationTTL` 15m (`:188`), `AccountCredentialExpiryBuffer` 2m (`:59`) |
| `AccountTokenManager`, `RegisterOAuthRefreshers`, `WithAccountProviders` | `account_token_manager.go` | Single-flight renewal per tenant and account |
| `OAuth2TokenRefresher` | `account_refresher_oauth2.go` | RFC 6749 §6 |
| `AccountCredentialLeaser`, `AccountDeliveryGrantor`, `DefaultAccountDeliveryTTL` | `account_lease.go` | 1h (`:88`) |
| `AccountLimitClassifier`, `DefaultAccountLimitBackoff`, `MaxAccountLimitBackoff` | `account_limit_classifier.go` | 5h (`:73`), 24h (`:78`) |
| `TurnAccountAuthRejectedError` | `account_auth_rejection.go:175` | |
| `RotateSessionAccount` | `orchestrator_account_rotation.go:333` | |
| `SessionRecord.CanRotateAccount`, `OpenAccountSegment` | `session_store.go:378`, `:444` | |
| `reportAccountPoolExhausted` | `account_pool_exhaustion.go:92` | Audit, metric, log |
| `service.AccountService` | `pkg/service/account_service.go` | Tenant-enforced CRUD, release, reservations |
| `service.NewAccountSecretResolver` | `pkg/service/account_secret_ref.go:49` | Resolves in the account's tenant |
| `materialize` | `pkg/service/account_materialize.go:301` | The release verb's body |
| `agent.NewAccountPool` | `pkg/agent` | The daemon's resolver over `pkg/orchestration/fleet` |

### Spend and cost

| Symbol | File | Notes |
| --- | --- | --- |
| `ConsumptionLedger`, `ConsumptionEntry` | `account_ledger.go` | Guarded-create writes; `SpendUnattributed` is `-` |
| `AccountSpendPolicy` | `account_spend_policy.go:43` | `Quota`, `Breaker` |
| `SpendLimits`, `QuotaWindow`, `QuotaAuthority` | `account_quota.go:125`, `:89` | Sliding windows |
| `BreakerPolicy` | `account_breaker.go:80` | |
| `SpendAuthority` | `account_spend_authority.go:87` | `spendProbeTTL` 1m (`:69`) |
| `TenantSpendPolicy`, `TenantSpendPolicyStore` | `tenant_spend_policy.go:46` | `orch/spendpolicy/{tenant}` |
| `ScopeSpendReader`, `NewScopeSpendAdmission`, `WithScopeSpendReporting` | `admission_spend.go:80`, `:117`, `:105` | |
| `AdmissionAxisProjectSpend`, `AdmissionAxisTenantSpend` | `admission_spend.go:53`, `:57` | `project_spend`, `tenant_spend` |
| `PriceBook` | `price_book.go` | `Stamp`, `Lists` |
| `AccountBilling`, `FeeShare` | `account_billing.go:92`, `:136` | 30.436875-day month |
| `CostSettings` | `cost_settings.go:48` | `orch/costsettings/{tenant}` |
| `ReadSpendReport` | `spend_report.go` | |
| `config.PricingConfig` | `pkg/config/pricing.go` | |

### Job admission

| Symbol | File | Notes |
| --- | --- | --- |
| `JobLease`, `DefaultJobLeaseTTL` | `job_lease.go:91`, `:75` | 90s |
| `CountsAgainstBudget`, `HoldsSlot`, `HoldsWorker`, `ClaimsWorker` | `job_lease.go:215`, `:231`, `:241`, `:255` | |
| `NewTransactionJobLeaseStore`, `NewProcessJobLeaseStore`, `NewProjectCapacityJobLeaseStore` | `job_lease.go:441`, `:703`, `:872` | |
| `JobLeaseStore.Rebudget` | `job_lease.go:904` (project), `:1036` (fleet), `job_lease_backend_capacity.go:135` (backend) | |
| `CountLiveJobLeases` | `task_dispatcher.go:971` | A budget count |
| `JobRun.Park` | `job_run.go:1308` | |
| `operatorFaultClasses`, `OperatorFaultClass`, `ParkAwaitsAnAnswer` | `task_attempt.go:322`, `:352`, `:340` | |
| `resolverFaultClasses` | `task_attempt.go:359` | |
| `DefaultAttemptBudget`, `MaxConsecutiveEnvironmentFaults` | `task_attempt.go:919`, `:930` | Both 3 |
| `Task.ChargedAttempts`, `Task.BudgetExhausted`, `Task.AttemptHistory` | `task_attempt.go:943`, `:962`, `:1535` | |
| `NewJobReaper` | `job_reaper.go:137` | |
| `NewJobStallDetector`, `DefaultJobIdleThresholds`, `JobIdleThresholdsFrom` | `job_stall.go:292`, `:74`, `:182` | |
| `KnownJobStates` | `job_runner.go:347` | |
| `DefaultShutdownDrainTimeout` | `shutdown.go:30` | 10s |

## Invariants

A change that breaks one of these is a defect, whatever its tests say.

**Credentials**

1. No response other than the release verb's carries credential material. No
   lease, log line or audit event carries any field of `AccountCredential`.
2. A spawn RPC to a fleet node never carries a credential; it carries selectors.
3. The released envelope carries no refresh token, token endpoint or client id.
4. A secret reference resolves in the account's own tenant, never the caller's.
5. A referenced account is `refresh_policy: none`; its release is `single_use`;
   `rotate` on it is refused.
6. A failed secret read never exposes the secrets service's error, through the
   message or the error chain.
7. The daemon never persists a released credential.
8. A release without a lease manager wired is the only unleased release.
   Otherwise a release that cannot obtain a grant fails.

**Records**

9. Every write of an existing account record is a compare-and-swap against the
   bytes read. A store without compare-and-swap is refused.
10. `Account.Default` is derived from the scope pointer and never stored.
11. `defaultMu` is a leaf lock.
12. Deleting the default promotes nobody.
13. No automatic stamp replaces `revoked`.

**Selection and spend**

14. The session cap is applied before the tenant-wide fallback decision.
15. A resolution that selects an account reserves on it.
16. Spend is checked before the credential is resolved on both release paths.
17. Every spend reader fails closed with an error, never with an admit and never
    with a refusal.
18. An unconfigured spend policy is unmetered; an unknown model is unpriced; an
    unconfigured billing mode is unknown. None of them is zero.
19. The ledger is append-only except for late pricing, which prices and never
    re-prices.
20. Quota planes are written before the attribution plane.

**Job admission**

21. No job side effect happens before its lease is acquired.
22. A task-bound job keeps its lease until `concludeAttempt` has written the
    task.
23. A task accepts a conclusion only from the job named in `SpawnedJobID`.
24. An unrecognized fault class or terminal reason is charged.
25. The shutdown drain never cancels the loops it waits for.

## Errors

Every error is a typed struct with a sentinel reachable through `errors.Is`.

**Transport mapping.** REST maps service errors in `accountErrorStatus`
(`pkg/api/rest/handlers_account.go:774`) and gRPC in `convertAccountError`
(`pkg/api/grpc/server_account.go:499`):

| Error | REST | gRPC |
| --- | --- | --- |
| `service.AccountNotFoundError` | 404 | `NotFound` |
| `service.AccountInvalidError` (every write-time validation failure) | 400 | `InvalidArgument` |
| `AccountSessionCountChangedError` (`ErrAccountSessionCountChanged`) | 412 | `Aborted` |
| `AccountsSaturatedError` (`ErrAccountsSaturated`) | 409 | `ResourceExhausted` |
| `service.AccountNotReleasableError` (revoked, errored or held account) | 409 | `FailedPrecondition` |
| `AccountSecretRefUnresolvedError` | 409, `fault_class: credential_not_found` | `FailedPrecondition` |
| `lease.LeaseRevokedError` (a revoked delivery grant presented) | 409 | `Internal` (see Known gaps) |
| `service.AccountProjectScopeError` | 403 | `PermissionDenied` |
| `AccountVersionConflictError` (`ErrAccountVersionConflict`), including an overtaken rotation | 409 | `Aborted` |
| `service.AccountUsageTrackerNotConfiguredError`, `AccountTokenManagerNotConfiguredError`, `AccountSessionReserverNotConfiguredError`, `AccountSecretResolverNotConfiguredError` | 503 | `Unavailable` |
| Anything else | 500 | `Internal` |

**Write-time validation** (all surface as `AccountInvalidError`, with the
domain sentinel reachable through `errors.Is`):
`AccountRefreshTokenRequiredError` (an `oauth2` account with no refresh token),
`AccountProviderEndpointUnresolvedError` (no token endpoint from the account or
its provider), `AccountStaticTokenRefreshableError` (a `none` account carrying a
refresh token), `AccountCredentialEmptyError` (a `none` account whose credential
holds no `access_token`, `env` or `files`: nothing a spawn could use; on rotate
this also stops an empty envelope from erasing the stored one),
`AccountCredentialModeInvalidError` (an unknown refresh policy).

**Resolution errors** surface on the spawn path as fault classes, not
transport statuses:

| Error | Fault class | Accounting |
| --- | --- | --- |
| `NoAccountAvailableError` | `no_provider_account` | Refunded, blocked |
| `AccountRefresherNotConfiguredError` | `account_refresher_missing` | Refunded, blocked |
| `AccountSecretRefUnresolvedError` (`ErrAccountCredentialNotFound`) | `account_credential_missing` | Refunded, blocked; the account goes to `error` |
| `AccountsCoolingDownError` (`account_errors.go:336`) | `accounts_cooling_down` | Refunded, requeued at `AvailableAt` |
| `AccountsSaturatedError` (`:379`) | `accounts_saturated` | Refunded, requeued |
| `AccountsSpentError` (`:416`) | `accounts_spent` | Refunded, requeued at `AvailableAt` |
| `AccountLimitedError` (`:561`), `TurnAccountLimitedError` | `account_limited` | Refunded, requeued |
| `TurnAccountAuthRejectedError` (`ErrAccountAuthRejected`) | `account_auth_rejected` | Refunded, requeued; the account goes to `error` |
| `AccountQuotaExhaustedError` | `account_quota_exhausted` | Requeued until the window frees |
| `AccountBreakerOpenError`, `AccountBreakerTrialHeldError` | `account_breaker_tripped` | Requeued |
| `AgentSpendExhaustedError` | — | Refused at spawn for a named Agent |
| `AccountSessionCountError`, `AccountCredentialExpiringError`, `AccountLeaseScopeError`, `AccountCredentialSourceChangedError` | — | Fail the resolution or write |

**Admission** (`admission_spend.go`): `AdmissionConfigError` (nil quota or
tenants store, or a project with no tenant), `AdmissionUndecidableError` (a
spend policy or window that could not be read).

**Job admission**: `JobLeaseError` (`ErrJobLeaseInvalidInput`,
`ErrJobLeaseUnreadable`, `ErrJobLeaseSubjectMismatch`, `ErrJobLeaseBackend`),
`JobLeaseTakenError` (`:1158`), `JobSupersededError` (`:1194`),
`JobLeaseNotFoundError`, `ProjectCapacityExceededError` (`:1228`, also a refused
`Rebudget`, on which the job waits), `FleetCapacityExceededError`
(`ErrFleetCapacityExceeded`), `AttemptConcludedError`, `JobStalledError`,
`JobIdleThresholdError`, `DrainTimeoutError` (`ErrDrainTimeout`),
`ServiceShuttingDownError` (`ErrServiceShuttingDown`), `StoreClosedError`
(`ErrStoreClosed`).

## Configuration

All keys are under `orchestration` in `quicbaas.yaml` (`pkg/config/config.go`).

```yaml
orchestration:
  account_delivery_ttl: 1h        # config.go:1439; delivery-lease backstop
  account_providers:              # config.go:1509
    - name: claude
      override: true              # required to override a built-in
      token_endpoint: https://auth.example.com/v1/oauth/token
      client_id: <client-id>
    - name: acme-llm              # a provider QuicBaaS does not ship
      display_name: ACME LLM
      token_endpoint: https://id.acme.example/oauth2/token
      access_token_env_key: ACME_LLM_TOKEN
      scopes: [inference]
  reap_lease_ttl: 90s             # config.go:1517; 0 = default
  idle_thresholds:                # config.go:1525; 0 = default, negative = do not watch
    executing: 3h
    reviewing: 30m
  pricing:                        # config.go:1589; config.PricingConfig
    version: "v1"                 # required once any model is priced
    models:
      - model: claude-opus-4-5-20251101
        aliases: [claude-opus-4-5]
        provider: anthropic
        input_per_mtok: 5.00
        output_per_mtok: 25.00
        cache_read_per_mtok: 0.50
        cache_write_per_mtok: 6.25
      - model: qwen3-8b
        engine: openvino          # engine-specific beats the generic entry
        provider: local
        power_watts: 12
    electricity_price_per_kwh: 0.12   # pricing.go:82; default 0.12
    engine_power_watts:               # pricing.go:87
      ollama: 250
    engine_devices:                   # pricing.go:97; no default binding
      ollama: gpu
      openvino: npu
    telemetry_retention: 336h         # pricing.go:103; 14 days, zero is the default
    tenants:                          # pricing.go:108
      acme:
        electricity_price_per_kwh: 0.30
        models:
          - {model: claude-opus-4-5-20251101, provider: anthropic, input_per_mtok: 4, output_per_mtok: 20}
```

**Provider validation** (`config.Validator`) rejects an empty name, a duplicate
name, a name that shadows a built-in without `override: true`, `override: true`
on a name that is not built in, a token endpoint that is relative, not `https`
or has no host, and a custom provider that declares no defaults.

**Thresholds.** An `idle_thresholds` key that is not a state in
`KnownJobStates()` is refused at config validation and again at wiring.

**Pricing.** A malformed table fails startup. Names match exactly, plus
aliases.

The runtime cost settings and the tenant spend policy are not configuration;
they are KV records set through the API ([api.md](api.md)).

## Metrics

| Metric | Labels |
| --- | --- |
| `quicbaas_orch_account_pool_exhausted_total` (`pkg/metrics/metrics.go:342`) | `tenant`, `provider`, `reason` (`cooling_down`, `saturated`, `spent`, …) |
| `quicbaas_orch_account_limit_holds_total` | per hold |
| `quicbaas_factory_tokens_total` | `tenant`, `project`, `kind`, `provider`, `backend`, `model`, `direction` (`input`, `output`, `cache_read`, `cache_write`) |
| `quicbaas_factory_cost_micros_total` | the same, with `basis` = `equivalent` or `billed` |
| `quicbaas_factory_spend_deferrals_total` | `tenant`, `project`, `axis` (`project_spend`, `tenant_spend`), `reason` (`token_limit`, `cost_limit`, `provider_hold`; any other quota reason, empty or `within_limits` is `other`), counted when a deferral starts or changes |

Every label is bounded. `model` is a name the price table lists (aliases
included, `PriceBook.Lists`) or `other`. `provider` is a vendor the table names,
`local` or `unknown`. `project` and `kind` are `none` for a turn with neither.
Host, agent, job, task and session are never labels; they are on the ledger
entry and in the spend report. The token and cost counters are incremented
after the ledger holds the turn, so the metrics and the spend report sum the
same turns.

## Wiring

`pkg/server/lifecycle_orchestration.go`:

- builds the account store, secret store, provider registry, token manager,
  OAuth refreshers, resolver and strategy registry, and hands the same token
  manager to `service.AccountService`;
- builds the ledger over the tenant-routed orchestration KV, so entries live
  inside the owning tenant's barrier, and attaches the spend authority to both
  release paths (`WithSpendAuthority` on the resolver, `SetSpendAuthority` on
  the account service);
- wires the scope spend reader into admission (`:618`);
- selects the job lease store, starts the sweep goroutine and registers the
  drain step.

`pkg/server/ledger_pricing.go` registers the `ledger.pricing` system job
(`pkg/scheduler/handlers/ledger_pricing.go`).

On a worker, `cmd/quicbaas/fleet_serve.go` composes the resolver with
`agent.NewAccountPool` and attaches it to every ACP backend with
`Runtime.SetAccountResolver`.

## Tests

| Area | Tests |
| --- | --- |
| Store, defaults, locking | `pkg/orchestration/account_store*_test.go`, including `TestAccountStore_SetDefault_ConcurrentMutualDisplacement_DoesNotDeadlock` |
| End-to-end pool | `pkg/orchestration/account_spawn_e2e_test.go`: `TestAccountPool_Anthropic_StaticToken_EndToEnd`, `TestAccountPool_OpenAI_StaticToken_EndToEnd`, `TestAccountPool_OAuthRefresh_EndToEnd` (against a live local token endpoint) |
| Provider registry | `TestBuiltinAccountProviders_*`: fails if a vendor endpoint or client id is hardcoded |
| Delivery leases | `TestAccountCredentialLeaser_NoSecretInLeaseOrAudit` |
| Release verb | `TestMaterialize_AuditNamesTheGrantAndNeverTheCredential` |
| REST | `pkg/api/rest/handlers_account*_test.go` |
| gRPC | `pkg/api/grpc/server_account*_test.go` |
| MCP | `pkg/api/mcp/account_tools*_test.go` |
| Embedded | `sdk/go/transport/embedded/handlers_account*_test.go` |
| SDK transports | `sdk/go/transport/{rest,grpc,mcp}/account_handler_test.go`, `sdk/go/client/account/client_test.go` |
| CLI | `cmd/quicbaas/account_test.go` |
| Integration | `test/integration/cli/account_test.go`, `test/integration/fleet_enrollment/account_release_test.go` |

Run a package's unit tests with `make test-orchestration` (or the matching
`test-$PACKAGE` target) and integration tests with
`make integration-test-cli`.

## Known gaps

Each item is a defect or limitation in the code, recorded here rather than
fixed in a docs change.

1. **Stale spend-policy help.** The CLI help
   (`cmd/quicbaas/orchestrator_spend_policy.go:50-51`) and the MCP tool
   description (`pkg/api/mcp/orchestrator_tools.go:331`) say a tenant's limits
   are not enforced at admission. They are (`admission_spend.go:132`, wired at
   `pkg/server/lifecycle_orchestration.go:618`).
2. **A proto comment cites a missing document.** `account.proto:70` points at
   `docs/agent-orchestration/accounts.md`, which does not exist. The page is
   now [architecture.md](architecture.md#the-release-verb).
3. **The reservation table is per control-plane node.** It lives in process
   memory, so `max_concurrent_sessions` holds per node in a deployment with
   more than one control-plane node.
4. **No inbox reason for an exhausted pool.** Exhaustion is audited, counted
   and logged, but the attention inbox has no `account_pool_exhausted` reason.
5. **`CodexExecBackend` has no account resolver.** `SetAccountResolver` exists
   only on `ACPBackend` (`acp_backend.go:272`), so a codex spawn on the control
   plane cannot use the pool.
6. **Usage needs orchestration.** `GetAccountUsage` answers 503
   (`AccountUsageTrackerNotConfiguredError`) when orchestration is disabled,
   although the account CRUD verbs work.
7. **Limit detection falls back to a text heuristic** when the CLI emits no
   structured signal. QuicBaaS does not sit between the CLI and the provider.
8. **No interactive OAuth enrollment, and no vendor endpoint or client id is
   shipped.** `oauth2` renewal needs both values supplied by the operator.
9. **Telemetry retention is not enforced by the engine.** The retention policy
   is set on every `fleet.device.*` series, but the time-series engine stores
   it without expiring points.
10. **`quicbaas orchestrator rotate-account` lacks `--available-at`.** The
    request field `available_at_unix` is reachable from REST, gRPC, MCP and the
    SDK only.
11. **TypeScript SDK gaps.** `AccountsClient` (`accounts.ts`) has no
    materialize, reserve or release. The non-REST TypeScript transports return
    `notImpl` for rotate-account, the spend report, the spend policy and cost
    settings.
12. **Web UI gaps.** The spawn dialog's `account_allow` takes effect only
    alongside a pin; there is no allow-list picker and no rotate action.
13. **A lab host in CLI help.** `cmd/quicbaas/orchestrator_cost.go:78` uses a
    real-looking host and port as its `--host` example; it should use a
    placeholder such as `gpu-host.internal`. The `SpendHost.Host` comment
    (`pkg/orchestration/spend_report.go:297`) has the same problem.
14. **gRPC does not map a revoked delivery grant.** REST answers a presented
    revoked lease with 409 so a daemon drops the grant and asks for a fresh
    one. `convertAccountError` has no case for `lease.LeaseRevokedError`, so the
    same refusal over gRPC is `Internal`.
15. **A spend-capped release answers 500.** The release verb checks spend
    before it resolves the credential (`pkg/service/account_materialize.go:364-368`).
    The refusals it can return (`AccountQuotaExhaustedError`,
    `AccountBreakerOpenError`, `AccountBreakerTrialHeldError`,
    `AccountLimitedError`, `AccountSpendUnknownVerdictError`,
    `SpendAuthorityConfigError`) have no case in `accountErrorStatus`
    (`pkg/api/rest/handlers_account.go:842`) or `convertAccountError`, so a
    daemon sees 500 or `Internal` instead of a conflict it could back off from.
16. **A handler comment names the wrong permission.**
    `pkg/api/rest/handlers_orchestrator_spend_policy.go:60` says
    `orchestrator:spend_policy`; the route requires `spend_policy:write`.
