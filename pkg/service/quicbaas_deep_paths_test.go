// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestWriteBatch_NilPointInBatch_Validation(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5},
			nil, // nil point should return validation error
		},
	}

	resp, err := tsSvc.WriteBatch(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestWriteBatch_EmptyMetricInBatch_Validation(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "", Value: 100.0}, // empty metric
		},
	}

	resp, err := tsSvc.WriteBatch(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

// =============================================================================
// Aggregate - Unsupported function, default value field, non-numeric skip
// =============================================================================

func TestAggregate_FunctionAndValueShape(t *testing.T) {
	tests := []struct {
		name       string
		entity     map[string]interface{}
		fieldName  string
		function   AggregateFunction
		wantErr    bool
		wantResult float64
		wantCount  uint64
	}{
		{
			name:     "unsupported function",
			entity:   map[string]interface{}{"timestamp": float64(10), "value": float64(100)},
			function: AggregateFunction(999),
			wantErr:  true,
		},
		{
			name:       "empty field name defaults to value",
			entity:     map[string]interface{}{"timestamp": float64(10), "value": float64(42.0)},
			function:   AggregateFunctionSum,
			wantResult: 42.0,
			wantCount:  1,
		},
		{
			name:      "non-numeric field is skipped",
			entity:    map[string]interface{}{"timestamp": float64(10), "status": "active"},
			fieldName: "status",
			function:  AggregateFunctionSum,
		},
		{
			name:     "missing default value field is skipped",
			entity:   map[string]interface{}{"timestamp": float64(10), "status": "ok"},
			function: AggregateFunctionCount,
		},
		{
			name:     "min with no values",
			function: AggregateFunctionMin,
		},
		{
			name:     "max with no values",
			function: AggregateFunctionMax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var scan [][]byte
			if tt.entity != nil {
				raw, err := json.Marshal(tt.entity)
				require.NoError(t, err)
				scan = [][]byte{raw}
			}

			qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult(scan), nil)
			svc := &quicBaaS{logger: createTestLogger(t), qrdbClient: qrdbProvider}
			tsSvc := &timeSeriesService{svc: svc}

			req := &AggregateRequest{
				EntityType: "metrics",
				StartTime:  1,
				EndTime:    100,
				FieldName:  tt.fieldName,
				Function:   tt.function,
			}

			resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, resp)
				var qbErr *errors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantResult, resp.Result)
			assert.Equal(t, tt.wantCount, resp.Count)
		})
	}
}

// =============================================================================
// TimeSeriesQuery - Entity ID extraction, limit application
// =============================================================================

func TestTimeSeriesQuery_EntityIDExtraction(t *testing.T) {
	log := createTestLogger(t)

	// Entity with "id" field
	data1 := map[string]interface{}{
		"timestamp": float64(10),
		"value":     float64(100),
		"id":        float64(42),
	}
	// Entity with "entity_id" field
	data2 := map[string]interface{}{
		"timestamp": float64(20),
		"value":     float64(200),
		"entity_id": float64(99),
	}
	// Entity without any ID field
	data3 := map[string]interface{}{
		"timestamp": float64(30),
		"value":     float64(300),
	}

	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)
	json3, _ := json.Marshal(data3)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2, json3}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	var responses []*TimeSeriesQueryResponse
	for resp := range stream {
		responses = append(responses, resp)
	}

	require.Len(t, responses, 1)
	require.Len(t, responses[0].DataPoints, 3)

	// Map iteration order is non-deterministic, so collect IDs and check
	// that all expected IDs are present regardless of order.
	ids := make(map[uint64]bool, 3)
	for _, dp := range responses[0].DataPoints {
		ids[dp.ID] = true
	}
	assert.True(t, ids[42], "expected entity with id=42")
	assert.True(t, ids[99], "expected entity with entity_id=99")
	assert.True(t, ids[0], "expected entity without ID field (id=0)")
}

func TestTimeSeriesQuery_LimitApplication(t *testing.T) {
	log := createTestLogger(t)

	// Create more data points than the limit
	var jsonEntries [][]byte
	for i := 1; i <= 5; i++ {
		data := map[string]interface{}{
			"timestamp": float64(i),
			"value":     float64(i * 100),
		}
		j, _ := json.Marshal(data)
		jsonEntries = append(jsonEntries, j)
	}

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult(jsonEntries), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Limit:      3, // Only want 3 of the 5 data points
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	var totalPoints int
	for resp := range stream {
		totalPoints += len(resp.DataPoints)
	}

	assert.Equal(t, 3, totalPoints)
}

func TestTimeSeriesQuery_ContextCancellation(t *testing.T) {
	log := createTestLogger(t)

	// Create enough data to require multiple batches (batch size is 100)
	var jsonEntries [][]byte
	for i := 1; i <= 250; i++ {
		data := map[string]interface{}{
			"timestamp": float64(i),
			"value":     float64(i),
		}
		j, _ := json.Marshal(data)
		jsonEntries = append(jsonEntries, j)
	}

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult(jsonEntries), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	// Cancel context before draining the stream
	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	// Use unbuffered channel so stream <- blocks on second batch
	stream := make(chan *TimeSeriesQueryResponse)

	var queryErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		queryErr = tsSvc.TimeSeriesQuery(ctx, &TimeSeriesQueryRequest{
			EntityType: "metrics",
			StartTime:  1,
			EndTime:    1000,
		}, stream)
	}()

	// Read first batch, then cancel
	<-stream
	cancel()
	wg.Wait()

	assert.Error(t, queryErr)
}

// =============================================================================
// CreateUser - Type assertion failure, auth manager nil
// =============================================================================

// =============================================================================
// DeleteUser - Nil auth manager, invalid user store type, user not found
// =============================================================================

// =============================================================================
// GetMigrationProgress - Non-migration type, ETA calculation, not found
// =============================================================================

func TestGetMigrationProgress_InvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		req      *GetMigrationProgressRequest
		wantCode errors.ErrorCode
	}{
		{name: "nil request", req: nil, wantCode: errors.ErrInvalidInput},
		{name: "empty migration id", req: &GetMigrationProgressRequest{MigrationID: ""}, wantCode: errors.ErrInvalidInput},
		{name: "unknown migration id", req: &GetMigrationProgressRequest{MigrationID: "nonexistent-id"}, wantCode: errors.ErrNotFound},
		{name: "job is not a migration", req: &GetMigrationProgressRequest{MigrationID: "upgrade-job-1"}, wantCode: errors.ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &quicBaaS{logger: createTestLogger(t)}
			opsSvc := newOperationsService(svc)

			opsSvc.jobsMu.Lock()
			opsSvc.jobs["upgrade-job-1"] = &operationJob{
				ID:     "upgrade-job-1",
				Type:   "upgrade",
				Status: "running",
			}
			opsSvc.jobsMu.Unlock()

			resp, err := opsSvc.GetMigrationProgress(tenantctx.WithSystem(context.Background()), tt.req)
			require.Error(t, err)
			assert.Nil(t, resp)

			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tt.wantCode, qbErr.Code)
		})
	}
}

func TestGetMigrationProgress_ETACalculation(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	opsSvc := newOperationsService(svc)

	// Insert a migration job with partial progress
	opsSvc.jobsMu.Lock()
	opsSvc.jobs["mig-eta"] = &operationJob{
		ID:            "mig-eta",
		Type:          "migration",
		Status:        "running",
		Progress:      50.0, // 50% done
		StartTime:     time.Now().Add(-10 * time.Second),
		ItemsMigrated: 500,
		TotalItems:    1000,
	}
	opsSvc.jobsMu.Unlock()

	resp, err := opsSvc.GetMigrationProgress(tenantctx.WithSystem(context.Background()), &GetMigrationProgressRequest{
		MigrationID: "mig-eta",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "mig-eta", resp.MigrationID)
	assert.Equal(t, 50.0, resp.Progress)
	assert.True(t, resp.ETA > 0, "ETA should be positive for partially complete migration")
}

func TestGetMigrationProgress_ZeroProgress_NoETA(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	opsSvc := newOperationsService(svc)

	opsSvc.jobsMu.Lock()
	opsSvc.jobs["mig-zero"] = &operationJob{
		ID:        "mig-zero",
		Type:      "migration",
		Status:    "initiated",
		Progress:  0.0,
		StartTime: time.Now().Add(-5 * time.Second),
	}
	opsSvc.jobsMu.Unlock()

	resp, err := opsSvc.GetMigrationProgress(tenantctx.WithSystem(context.Background()), &GetMigrationProgressRequest{
		MigrationID: "mig-zero",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int64(0), resp.ETA) // no ETA when progress is 0
}

// =============================================================================
// Delete - Raft error, cache invalidation with cacheManager
// =============================================================================

func TestDelete_RaftError(t *testing.T) {
	deleteErr := stderrors.New("raft proposal failed")
	qrdbProvider := newSvcBoostMockQRDBProviderWithErrors(nil, deleteErr, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), &DeleteRequest{
		EntityType: "users",
		ID:         1,
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestDelete_CustomShardID(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), &DeleteRequest{
		EntityType: "users",
		ID:         1,
		ShardID:    5,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Equal(t, uint64(5), qrdbProvider.lastShardID.Load())
}

// =============================================================================
// List - Raft error, result parsing, pagination, parse error skip
// =============================================================================

func TestList_RaftError(t *testing.T) {
	scanErr := stderrors.New("raft read failed")
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, scanErr)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), &ListRequest{
		EntityType: "users",
	}, stream)
	require.Error(t, err)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestList_NilResult(t *testing.T) {
	raftNode := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	svc := createTestService(t, withMockRaftNode(raftNode))
	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), &ListRequest{
		EntityType: "users",
	}, stream)
	require.NoError(t, err)

	// One empty page, then the stream closes
	page, ok := <-stream
	require.True(t, ok)
	assert.Empty(t, page.Entities)
	assert.Zero(t, page.TotalCount)
	assert.Empty(t, page.NextPageToken)
	_, ok = <-stream
	assert.False(t, ok)
}

func TestList_CustomShardID(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), &ListRequest{
		EntityType: "users",
		ShardID:    7,
	}, stream)
	require.NoError(t, err)

	// Drain channel
	for range stream {
	}

	assert.Equal(t, uint64(7), qrdbProvider.lastShardID.Load())
}

func TestList_WithPagination(t *testing.T) {
	scanData := make(map[string][]byte, 20)
	for i := 1; i <= 20; i++ {
		scanData[fmt.Sprintf("users:%d", i)] = []byte(`{}`)
	}
	svc := createTestService(t, withQRDBClientProvider(newSvcBoostMockQRDBProvider(scanData, nil)))
	ctx := tenantctx.WithSystem(context.Background())

	var gotIDs []uint64
	token := ""
	for page := 1; page <= 4; page++ {
		resp := listOnePage(t, svc, ctx, &ListRequest{EntityType: "users", PageSize: 5, PageToken: token})
		require.Len(t, resp.Entities, 5, "page %d", page)
		assert.Equal(t, uint64(20), resp.TotalCount)
		for _, e := range resp.Entities {
			gotIDs = append(gotIDs, e.ID)
		}
		if page < 4 {
			require.NotEmpty(t, resp.NextPageToken, "page %d", page)
		} else {
			assert.Empty(t, resp.NextPageToken, "last page")
		}
		token = resp.NextPageToken
	}

	want := make([]uint64, 0, 20)
	for i := uint64(1); i <= 20; i++ {
		want = append(want, i)
	}
	assert.Equal(t, want, gotIDs, "pages cover every entity once, in ID order")
}

func TestList_InvalidPageToken(t *testing.T) {
	scanData := map[string][]byte{"users:1": []byte(`{}`), "users:2": []byte(`{}`)}
	svc := createTestService(t, withQRDBClientProvider(newSvcBoostMockQRDBProvider(scanData, nil)))
	ctx := tenantctx.WithSystem(context.Background())

	for name, token := range map[string]string{
		"unparsable":   "not-a-cursor",
		"out of range": "2",
	} {
		t.Run(name, func(t *testing.T) {
			stream := make(chan *ListResponse, 1)
			err := svc.List(ctx, &ListRequest{EntityType: "users", PageSize: 1, PageToken: token}, stream)
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err))
			var tokenErr *InvalidPageTokenError
			assert.ErrorAs(t, err, &tokenErr)
			assert.Empty(t, stream, "no page is sent for an invalid token")
		})
	}
}

// listOnePage runs List and requires that it sends exactly one page.
func listOnePage(t *testing.T, svc QuicBaaS, ctx context.Context, req *ListRequest) *ListResponse {
	t.Helper()
	stream := make(chan *ListResponse, 2)
	require.NoError(t, svc.List(ctx, req, stream))
	var pages []*ListResponse
	for resp := range stream {
		pages = append(pages, resp)
	}
	require.Len(t, pages, 1, "List sends exactly one page")
	return pages[0]
}

// =============================================================================
// ListMetrics - Storage error, default limit
// =============================================================================

func TestListMetrics_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	mockTS.On("ListMetrics", "", 1000).
		Return(nil, stderrors.New("storage read error")).Once()

	resp, err := tsSvc.ListMetrics(tenantctx.WithSystem(context.Background()), &TSListMetricsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
	mockTS.AssertExpectations(t)
}

func TestListMetrics_NilRequest(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	mockTS.On("ListMetrics", "", 1000).
		Return([]string{"cpu.usage", "memory.used"}, nil).Once()

	resp, err := tsSvc.ListMetrics(tenantctx.WithSystem(context.Background()), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Metrics, 2)
	mockTS.AssertExpectations(t)
}

func TestListMetrics_WithPrefixAndLimit(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	mockTS.On("ListMetrics", "cpu.", 50).
		Return([]string{"cpu.usage", "cpu.idle"}, nil).Once()

	resp, err := tsSvc.ListMetrics(tenantctx.WithSystem(context.Background()), &TSListMetricsRequest{
		Prefix: "cpu.",
		Limit:  50,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Metrics, 2)
	mockTS.AssertExpectations(t)
}

func TestListMetrics_StorageNotConfigured(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc, tsStorage: nil}

	resp, err := tsSvc.ListMetrics(tenantctx.WithSystem(context.Background()), &TSListMetricsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

// =============================================================================
// Backup functions - Type assertion failures, error paths
// =============================================================================

func TestBackupService_ManagerFailures(t *testing.T) {
	type backupCall func(ctx context.Context, s *backupService) (interface{}, error)

	createBackup := func(ctx context.Context, s *backupService) (interface{}, error) {
		return s.CreateBackup(ctx, &CreateBackupRequest{Name: "full"})
	}
	restoreBackup := func(ctx context.Context, s *backupService) (interface{}, error) {
		return s.RestoreBackup(ctx, &RestoreBackupRequest{BackupID: "backup-123"})
	}
	deleteBackup := func(ctx context.Context, s *backupService) (interface{}, error) {
		return s.DeleteBackup(ctx, &DeleteBackupRequest{BackupID: "backup-123"})
	}

	tests := []struct {
		name     string
		mgr      *mockBackupManager
		call     backupCall
		wantCode errors.ErrorCode
	}{
		{name: "create: nil manager", call: createBackup, wantCode: errors.ErrNotImplemented},
		{
			name: "create: manager error",
			mgr: &mockBackupManager{
				createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
					return nil, stderrors.New("backup creation failed")
				},
			},
			call:     createBackup,
			wantCode: errors.ErrInternal,
		},
		{
			name: "create: manager returns wrong type",
			mgr: &mockBackupManager{
				createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
					return "not-a-backup-struct", nil
				},
			},
			call:     createBackup,
			wantCode: errors.ErrInternal,
		},
		{name: "restore: nil manager", call: restoreBackup, wantCode: errors.ErrNotImplemented},
		{
			name: "restore: manager error",
			mgr: &mockBackupManager{
				restoreBackupFn: func(ctx context.Context, backupID string, opts interface{}) error {
					return stderrors.New("restore failed")
				},
			},
			call:     restoreBackup,
			wantCode: errors.ErrInternal,
		},
		{name: "delete: nil manager", call: deleteBackup, wantCode: errors.ErrNotImplemented},
		{
			name: "delete: manager error",
			mgr: &mockBackupManager{
				deleteBackupFn: func(ctx context.Context, backupID string) error {
					return stderrors.New("delete failed")
				},
			},
			call:     deleteBackup,
			wantCode: errors.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &quicBaaS{logger: createTestLogger(t)}
			if tt.mgr != nil {
				svc.backupMgr = tt.mgr
			}
			backupSvc := &backupService{svc: svc}

			resp, err := tt.call(tenantctx.WithSystem(context.Background()), backupSvc)
			require.Error(t, err)
			assert.Nil(t, resp)

			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tt.wantCode, qbErr.Code)
		})
	}
}

// =============================================================================
// Cache service - Type assertion failures for non-*cache.CacheManager
// =============================================================================

// mockCacheInterface implements interfaces.CacheManager but is NOT *cache.CacheManager
type mockCacheInterface struct{}

func (m *mockCacheInterface) Get(ctx context.Context, key string) ([]byte, error) { return nil, nil }
func (m *mockCacheInterface) Set(ctx context.Context, key string, value []byte) error {
	return nil
}
func (m *mockCacheInterface) Delete(ctx context.Context, key string) error { return nil }
func (m *mockCacheInterface) Clear()                                       {}
func (m *mockCacheInterface) Close() error                                 { return nil }

func TestCacheService_ManagerFailures(t *testing.T) {
	methods := []struct {
		name string
		call func(ctx context.Context, s *cacheService) (interface{}, error)
	}{
		{"GetCacheStats", func(ctx context.Context, s *cacheService) (interface{}, error) {
			return s.GetCacheStats(ctx, &GetCacheStatsRequest{})
		}},
		{"ClearCache", func(ctx context.Context, s *cacheService) (interface{}, error) {
			return s.ClearCache(ctx, &ClearCacheRequest{})
		}},
		{"GetCacheConfig", func(ctx context.Context, s *cacheService) (interface{}, error) {
			return s.GetCacheConfig(ctx, &GetCacheConfigRequest{})
		}},
		{"UpdateCacheConfig", func(ctx context.Context, s *cacheService) (interface{}, error) {
			return s.UpdateCacheConfig(ctx, &UpdateCacheConfigRequest{})
		}},
	}
	failures := []struct {
		name     string
		mgr      *mockCacheInterface
		wantCode errors.ErrorCode
	}{
		{name: "nil manager", wantCode: errors.ErrNotImplemented},
		{name: "manager of wrong type", mgr: &mockCacheInterface{}, wantCode: errors.ErrInternal},
	}

	for _, m := range methods {
		for _, f := range failures {
			t.Run(m.name+": "+f.name, func(t *testing.T) {
				svc := &quicBaaS{logger: createTestLogger(t)}
				if f.mgr != nil {
					svc.cacheManager = f.mgr
				}
				cacheSvc := &cacheService{svc: svc}

				resp, err := m.call(tenantctx.WithSystem(context.Background()), cacheSvc)
				require.Error(t, err)
				assert.Nil(t, resp)

				var qbErr *errors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, f.wantCode, qbErr.Code)
			})
		}
	}
}
