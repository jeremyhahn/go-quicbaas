### Changed

- **`quicbaas_factory_spend_deferrals_total` gains a `reason` label
  (go-quicbaas#1058).** The label set is now `{tenant, project, axis, reason}`.
  `reason` is the quota refusal that deferred the project: `token_limit`,
  `cost_limit` or `provider_hold`. Any other value, including an empty reason
  and `within_limits`, is recorded as `other`, so the set is closed.
  `Metrics.RecordFactorySpendDeferral` takes the reason and normalizes it
  through `metrics.SpendDeferralReasonLabel`. Queries that aggregate the
  counter by its old three labels still sum correctly.
