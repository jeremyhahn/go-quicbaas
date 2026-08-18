# Time Series — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/timeseries/`.

## Overview

Retention policies control how long time series data is kept. The retention enforcer runs as a background process that periodically identifies and deletes expired data via Raft consensus.

## Configuration

### Retention Policy

Set retention policies per metric:

```bash
# Set 30-day retention with 1-minute resolution
quicbaas ts set-retention --metric cpu.usage \
  --duration-sec 2592000 \
  --resolution-sec 60

# Set 90-day retention with 5-minute resolution
quicbaas ts set-retention --metric memory.used \
  --duration-sec 7776000 \
  --resolution-sec 300
```

### Policy Parameters

| Parameter | Description |
|-----------|-------------|
| `duration-sec` | How long to keep data in seconds (0 = infinite) |
| `resolution-sec` | Data point granularity in seconds |

### Enforcer Configuration

```go
type EnforcerConfig struct {
    CheckInterval time.Duration  // How often to check (default: 1 hour)
    BatchSize     int            // Max series per run (default: 1000)
    DryRun        bool           // Log only, no deletions (default: false)
    StopTimeout   time.Duration  // Graceful shutdown timeout (default: 30s)
}
```

## Background Enforcement

### Process Flow

```
+-----------------+     +------------------+     +------------------+
| List All        |---->| Check Retention  |---->| Delete Expired   |
| Metrics         |     | Policy           |     | via Raft         |
+-----------------+     +------------------+     +------------------+
        ^                                               |
        |                                               |
        +----------- Wait CheckInterval ---------------+
```

### Enforcement Steps

1. **List Metrics**: Enumerate all known time series metrics
2. **Get Policy**: Retrieve retention policy for each metric
3. **Calculate Expiration**: `expirationTime = now - policy.Duration`
4. **Delete Expired**: Submit delete requests via Raft consensus
5. **Track Results**: Update metrics and status

### Skipped Metrics

The enforcer skips metrics with:
- No retention policy configured
- Duration set to 0 (infinite retention)

## Dry Run Mode

Enable dry run to see what would be deleted without making changes:

```go
config := &retention.EnforcerConfig{
    DryRun: true,
}
```

Dry run logs show:
```
[DRY RUN] Would delete expired data for metric=cpu.usage expiration_time=2024-01-01T00:00:00Z
```

## Metrics and Monitoring

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `quicbaas_retention_enforcement_runs_total` | Counter | Total enforcement runs |
| `quicbaas_retention_enforcement_duration_seconds` | Histogram | Run duration |
| `quicbaas_retention_series_deleted_total` | Counter | Series deleted |
| `quicbaas_retention_points_deleted_total` | Counter | Points deleted (estimated) |
| `quicbaas_retention_enforcement_errors_total` | Counter | Errors encountered |
| `quicbaas_retention_metrics_processed_total` | Counter | Metrics examined |
| `quicbaas_retention_active_enforcements` | Gauge | Currently running |
| `quicbaas_retention_last_enforcement_timestamp_seconds` | Gauge | Last run timestamp |
| `quicbaas_retention_dry_runs_total` | Counter | Dry run executions |
| `quicbaas_retention_expired_series_found_total` | Counter | Expired series found |
| `quicbaas_retention_skipped_infinite_metrics_total` | Counter | Skipped (infinite retention) |

### Status API

Query enforcer status programmatically:

```go
status := enforcer.Status()
// Returns:
// - State: running/stopped
// - LastRunTime
// - LastRunResult
// - NextRunTime
// - TotalRuns
// - TotalDeleted
// - TotalErrors
```

## Manual Enforcement

Trigger enforcement manually:

```go
result, err := enforcer.RunOnce(ctx)
if err != nil {
    log.Error("Enforcement failed", err)
}
fmt.Printf("Deleted %d series, %d points\n", result.SeriesDeleted, result.PointsDeleted)
```

## Lifecycle Management

### Starting the Enforcer

```go
enforcer, err := retention.NewEnforcer(config, service, storage, logger)
if err != nil {
    return err
}
if err := enforcer.Start(ctx); err != nil {
    return err
}
```

### Stopping the Enforcer

```go
if err := enforcer.Stop(ctx); err != nil {
    log.Warn("Enforcer stop timeout", err)
}
```

### Error Handling

The enforcer handles errors gracefully:
- Non-fatal errors are logged and accumulated
- Enforcement continues to next metric on error
- Total errors tracked in status and metrics

## Best Practices

1. **Set Appropriate Intervals**: Default 1-hour check interval suits most workloads
2. **Use Batch Limits**: Prevent long-running enforcement by limiting batch size
3. **Test with Dry Run**: Verify retention policies before enabling deletions
4. **Monitor Metrics**: Track deletion rates and errors via Prometheus
5. **Coordinate with Backups**: Schedule enforcement after backup windows
