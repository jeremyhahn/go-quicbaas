# TimeSeries Write-Read Mismatch Investigation

## Critical Finding: Write-Read Storage Mismatch

**Issue**: TimeSeries writes succeed but queries return 0 points - identical pattern to vector service issue

### Root Cause

There are **TWO separate storage paths** for timeseries data:

1. **WRITE PATH - Via Raft/qrdb SDK**:
   - REST/gRPC handlers → timeSeriesService.WritePoint/WriteBatch
   - When `qrdbClient` is available, writes are delegated to: `client.TimeSeries().TSWritePoint/TSWriteBatch()` (quicbaas.go line 3860-3871, 3933-3947)
   - This writes data through qrdb SDK which stores to **KV store** via baasTransport.TSWritePointToStore()
   - Data stored with key prefix "ts:" in the distributed KV store

2. **QUERY PATH - Via Local PebbleDB**:
   - REST/gRPC handlers → timeSeriesService.TSQuery
   - Query always goes directly to: `t.tsStorage.QueryRich()` (quicbaas.go line 4045)
   - This reads from **local PebbleDB storage** at path: `config.Storage.DataDir/timeseries`
   - Local storage initialized in lifecycle_timeseries.go:41 via `qrdbtimeseries.NewWithDir(dataDir)`

### Why Queries Return 0 Points

The local PebbleDB storage (`s.timeSeriesStorage`) is only populated by:
- WritePoint (line 3866) - ONLY when `qrdbClient == nil` (fallback path)
- WriteBatch (line 3957) - ONLY when `qrdbClient == nil` (fallback path)

When Raft/qrdb is enabled (normal operation), **writes bypass the local storage entirely** and go to distributed KV only.

Queries always read from the local storage, which remains empty.

### Data Flow Comparison

**With Raft Available (Current - BROKEN)**:
```
Write: REST → Service → qrdbClient.TimeSeries().TSWritePoint() → KV Store
Query: REST → Service → localStorage.QueryRich() → Empty (returns 0 points)
```

**Without Raft (Fallback - Works)**:
```
Write: REST → Service → localStorage.WritePoint() → Local PebbleDB
Query: REST → Service → localStorage.QueryRich() → Local PebbleDB
```

### Implementation Details

#### Write Path in quicbaas.go (lines 3815-3874)
```go
func (t *timeSeriesService) WritePoint(...) {
    if t.svc.qrdbClient != nil {  // When Raft is available
        // Writes via qrdb SDK (goes to KV store)
        client.TimeSeries().TSWritePoint(ctx, sdkPoint)
    } else {
        // Fallback: write to local storage
        t.tsStorage.WritePoint(ctx, point)
    }
}
```

#### Query Path in quicbaas.go (lines 3995-4055)
```go
func (t *timeSeriesService) TSQuery(...) {
    // ALWAYS reads from local storage, never from KV store
    result, err := t.tsStorage.QueryRich(ctx, storageReq)
}
```

### Storage Interfaces

**Local Storage Path**:
- Type: `interfaces.TimeSeriesStorage` (timeSeriesStorageAdapter wrapping qrdbtimeseries.Storage)
- Stores to: Local PebbleDB via qrdbtimeseries
- Location: Set in lifecycle_timeseries.go:51
- Methods: WritePoint, WriteBatch, QueryRich, DeleteSeries, GetRetentionPolicy, SetRetentionPolicy, ListMetrics

**Distributed KV Path**:
- Via baasTransport.TSWritePointToStore()
- Stores in KV with prefix "ts:" + metric
- Methods: baasTransport.TSWrite, TSWritePoint, TSWriteBatch, TSQuery (lines 50-122)

### Configuration Points

1. **qrdbClient** - Set in service.go via WithQRDBClient() if Raft is available
   - When non-nil, triggers Raft write path
   
2. **tsStorage** - Set via WithTimeSeriesStorage() 
   - Always created in lifecycle_timeseries.go:51
   - Uses local PebbleDB storage

### Sharding Info
- Default shard ID: 1 (hardcoded in quicbaas.go line 3859, 3933)
- All timeseries data written to shard 1

### Related Code Locations

- **Service Write Logic**: /home/jhahn/sources/go-quicbaas/pkg/service/quicbaas.go (lines 3815-3974)
- **Service Query Logic**: /home/jhahn/sources/go-quicbaas/pkg/service/quicbaas.go (lines 3995-4055)
- **Transport Write**: /home/jhahn/sources/go-quicbaas/pkg/server/baas_transport_timeseries.go (lines 50-360)
- **Storage Adapter**: /home/jhahn/sources/go-quicbaas/pkg/server/lifecycle_timeseries.go (lines 99-248)
- **REST Handlers**: /home/jhahn/sources/go-quicbaas/pkg/api/rest/timeseries_handlers.go (lines 36-534)

### Integration Test Evidence

**Test File**: /home/jhahn/sources/go-quicbaas/test/integration/timeseries/multiprotocol_test.go

**Tests Affected**:
- TestMultiprotocol_WriteAndQueryPoint (line 85) - Writes succeed, queries return 0
- TestMultiprotocol_WriteBatch (line 165) - WriteBatch 4 points, query returns 0
- TestMultiprotocol_Aggregations (line 242) - WriteBatch then aggregation query fails
- TestMultiprotocol_Delete (line 316) - Can't delete what wasn't read
- TestMultiprotocol_ListMetrics (line 452) - Index not updated
- TestMultiprotocol_RetentionPolicy (line 385) - Policy queries from local storage

**Retry Logic**: Tests have 10-attempt retry loops (50ms each) because they're waiting for write to propagate, but local storage is never written to.

### Fix Strategy

Need to unify write and read paths. Options:

1. **Make Queries Read from KV Store** (Preferred):
   - Query path should also use qrdbClient.TimeSeries().TSQuery() when available
   - Similar to how writes are delegated to qrdb SDK
   - Ensures consistency: both writes and reads use same storage

2. **Make Writes Use Local Storage** (Not ideal for Raft):
   - Would lose Raft consensus protection
   - Defeats purpose of having qrdbClient

3. **Hybrid Approach**:
   - Writes to both local AND KV store (like vector service may have done)
   - More complex, potential consistency issues
