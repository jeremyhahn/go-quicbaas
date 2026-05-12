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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
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

	resp, err := tsSvc.WriteBatch(context.Background(), req)
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

	resp, err := tsSvc.WriteBatch(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

// =============================================================================
// Aggregate - Unsupported function, default value field, non-numeric skip
// =============================================================================

func TestAggregate_UnsupportedFunction(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	json1, _ := json.Marshal(data1)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunction(999), // invalid function
	}

	resp, err := tsSvc.Aggregate(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestAggregate_DefaultValueField(t *testing.T) {
	log := createTestLogger(t)

	// No FieldName set, should use "value" field by default
	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(42.0)}
	json1, _ := json.Marshal(data1)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		FieldName:  "", // empty, should use "value" as default
		Function:   AggregateFunctionSum,
	}

	resp, err := tsSvc.Aggregate(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 42.0, resp.Result)
	assert.Equal(t, uint64(1), resp.Count)
}

func TestAggregate_NonNumericFieldSkip(t *testing.T) {
	log := createTestLogger(t)

	// Entity where the custom field is a string (not numeric)
	data1 := map[string]interface{}{"timestamp": float64(10), "status": "active"}
	json1, _ := json.Marshal(data1)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		FieldName:  "status", // non-numeric field
		Function:   AggregateFunctionSum,
	}

	resp, err := tsSvc.Aggregate(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, float64(0), resp.Result)
	assert.Equal(t, uint64(0), resp.Count) // skipped because field is not numeric
}

func TestAggregate_MissingDefaultValueField(t *testing.T) {
	log := createTestLogger(t)

	// No FieldName and no "value" field in entity -- should be skipped
	data1 := map[string]interface{}{"timestamp": float64(10), "status": "ok"}
	json1, _ := json.Marshal(data1)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		FieldName:  "", // no field specified, and entity has no "value" field
		Function:   AggregateFunctionCount,
	}

	resp, err := tsSvc.Aggregate(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, float64(0), resp.Result) // count of 0 entities with "value" field
}

func TestAggregate_MinWithEmptyValues(t *testing.T) {
	log := createTestLogger(t)

	// No matching entities after filtering
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionMin,
	}

	resp, err := tsSvc.Aggregate(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, float64(0), resp.Result) // Min with no values = 0
	assert.Equal(t, uint64(0), resp.Count)
}

func TestAggregate_MaxWithEmptyValues(t *testing.T) {
	log := createTestLogger(t)

	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionMax,
	}

	resp, err := tsSvc.Aggregate(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, float64(0), resp.Result) // Max with no values = 0
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
	err := tsSvc.TimeSeriesQuery(context.Background(), req, stream)
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
	err := tsSvc.TimeSeriesQuery(context.Background(), req, stream)
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
	ctx, cancel := context.WithCancel(context.Background())
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

func TestCreateUser_NilAuthManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, authManager: nil}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.CreateUser(context.Background(), &AuthCreateUserRequest{
		Username: "alice",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrConfiguration, qbErr.Code)
}

func TestCreateUser_EmptyUsername(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, authManager: &mockAuthManager{}}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.CreateUser(context.Background(), &AuthCreateUserRequest{
		Username: "",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestCreateUser_AuthManagerError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, authManager: &mockAuthManager{
		createUserFn: func(username, password string, roles []string) (*auth.User, error) {
			return nil, stderrors.New("user already exists")
		},
	}}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.CreateUser(context.Background(), &AuthCreateUserRequest{
		Username: "alice",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestCreateUser_TypeAssertionFailure(t *testing.T) {
	log := createTestLogger(t)
	// Return a non-*auth.User type from CreateUser
	svc := &quicBaaS{logger: log, authManager: &svcBoostAuthManager{}}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.CreateUser(context.Background(), &AuthCreateUserRequest{
		Username: "alice",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestCreateUser_Success(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, authManager: &mockAuthManager{
		createUserFn: func(username, password string, roles []string) (*auth.User, error) {
			return &auth.User{
				ID:       "user-123",
				Username: username,
				Roles:    roles,
			}, nil
		},
	}}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.CreateUser(context.Background(), &AuthCreateUserRequest{
		Username: "alice",
		Roles:    []string{"admin"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "user-123", resp.UserID)
}

// =============================================================================
// DeleteUser - Nil auth manager, invalid user store type, user not found
// =============================================================================

func TestDeleteUser_NilAuthManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, authManager: nil}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.DeleteUser(context.Background(), &AuthDeleteUserRequest{
		Username: "alice",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrConfiguration, qbErr.Code)
}

func TestDeleteUser_EmptyUsername(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, authManager: &mockAuthManager{}}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.DeleteUser(context.Background(), &AuthDeleteUserRequest{
		Username: "",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestDeleteUser_InvalidUserStoreType(t *testing.T) {
	log := createTestLogger(t)
	// Return a user store that is NOT *auth.UserStore
	svc := &quicBaaS{logger: log, authManager: &svcBoostAuthManager{
		userStoreReturn: &svcBoostUserStore{},
	}}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.DeleteUser(context.Background(), &AuthDeleteUserRequest{
		Username: "alice",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestDeleteUser_UserNotFound(t *testing.T) {
	log := createTestLogger(t)
	userStore := auth.NewUserStore()
	svc := &quicBaaS{logger: log, authManager: &mockAuthManager{
		userStoreReturn: userStore,
	}}
	rbacSvc := &rbacService{svc: svc}

	resp, err := rbacSvc.DeleteUser(context.Background(), &AuthDeleteUserRequest{
		Username: "nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotFound, qbErr.Code)
}

// =============================================================================
// GetMigrationProgress - Non-migration type, ETA calculation, not found
// =============================================================================

func TestGetMigrationProgress_NilRequest(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	opsSvc := newOperationsService(svc)

	resp, err := opsSvc.GetMigrationProgress(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestGetMigrationProgress_EmptyMigrationID(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	opsSvc := newOperationsService(svc)

	resp, err := opsSvc.GetMigrationProgress(context.Background(), &GetMigrationProgressRequest{
		MigrationID: "",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestGetMigrationProgress_NotFound(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	opsSvc := newOperationsService(svc)

	resp, err := opsSvc.GetMigrationProgress(context.Background(), &GetMigrationProgressRequest{
		MigrationID: "nonexistent-id",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotFound, qbErr.Code)
}

func TestGetMigrationProgress_NonMigrationType(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	opsSvc := newOperationsService(svc)

	// Insert a job that is NOT of type "migration"
	opsSvc.jobsMu.Lock()
	opsSvc.jobs["upgrade-job-1"] = &operationJob{
		ID:     "upgrade-job-1",
		Type:   "upgrade",
		Status: "running",
	}
	opsSvc.jobsMu.Unlock()

	resp, err := opsSvc.GetMigrationProgress(context.Background(), &GetMigrationProgressRequest{
		MigrationID: "upgrade-job-1",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
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

	resp, err := opsSvc.GetMigrationProgress(context.Background(), &GetMigrationProgressRequest{
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

	resp, err := opsSvc.GetMigrationProgress(context.Background(), &GetMigrationProgressRequest{
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
	resp, err := svc.Delete(context.Background(), &DeleteRequest{
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
	resp, err := svc.Delete(context.Background(), &DeleteRequest{
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
	err := svc.List(context.Background(), &ListRequest{
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
	err := svc.List(context.Background(), &ListRequest{
		EntityType: "users",
	}, stream)
	require.NoError(t, err)

	// Stream should be closed with no responses
	_, ok := <-stream
	assert.False(t, ok)
}

func TestList_CustomShardID(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	stream := make(chan *ListResponse, 10)
	err := svc.List(context.Background(), &ListRequest{
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
	// Return multiple key-value pairs via QRDB scan mock
	scanData := map[string][]byte{
		"users:1": []byte(`{"name":"alice"}`),
		"users:2": []byte(`{"name":"bob"}`),
		"users:3": []byte(`{"name":"charlie"}`),
		"users:4": []byte(`{"name":"dave"}`),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	stream := make(chan *ListResponse, 10)
	err := svc.List(context.Background(), &ListRequest{
		EntityType: "users",
		PageSize:   2, // 2 entities per page
	}, stream)
	require.NoError(t, err)

	var responses []*ListResponse
	for resp := range stream {
		responses = append(responses, resp)
	}

	require.True(t, len(responses) >= 1)
	// First page should have next page token
	if len(responses) > 1 {
		assert.NotEmpty(t, responses[0].NextPageToken)
	}
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

	resp, err := tsSvc.ListMetrics(context.Background(), &TSListMetricsRequest{})
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

	resp, err := tsSvc.ListMetrics(context.Background(), nil)
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

	resp, err := tsSvc.ListMetrics(context.Background(), &TSListMetricsRequest{
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

	resp, err := tsSvc.ListMetrics(context.Background(), &TSListMetricsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

// =============================================================================
// Backup functions - Type assertion failures, error paths
// =============================================================================

func TestCreateBackup_NilBackupManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, backupMgr: nil}
	backupSvc := &backupService{svc: svc}

	resp, err := backupSvc.CreateBackup(context.Background(), &CreateBackupRequest{Name: "full"})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

func TestCreateBackup_ManagerError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, backupMgr: &mockBackupManager{
		createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
			return nil, stderrors.New("backup creation failed")
		},
	}}
	backupSvc := &backupService{svc: svc}

	resp, err := backupSvc.CreateBackup(context.Background(), &CreateBackupRequest{Name: "full"})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestCreateBackup_TypeAssertionFailure(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, backupMgr: &mockBackupManager{
		createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
			return "not-a-backup-struct", nil // wrong type
		},
	}}
	backupSvc := &backupService{svc: svc}

	resp, err := backupSvc.CreateBackup(context.Background(), &CreateBackupRequest{Name: "incremental"})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestRestoreBackup_NilBackupManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, backupMgr: nil}
	backupSvc := &backupService{svc: svc}

	resp, err := backupSvc.RestoreBackup(context.Background(), &RestoreBackupRequest{
		BackupID: "backup-123",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

func TestRestoreBackup_ManagerError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, backupMgr: &mockBackupManager{
		restoreBackupFn: func(ctx context.Context, backupID string, opts interface{}) error {
			return stderrors.New("restore failed")
		},
	}}
	backupSvc := &backupService{svc: svc}

	resp, err := backupSvc.RestoreBackup(context.Background(), &RestoreBackupRequest{
		BackupID: "backup-123",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestDeleteBackup_NilBackupManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, backupMgr: nil}
	backupSvc := &backupService{svc: svc}

	resp, err := backupSvc.DeleteBackup(context.Background(), &DeleteBackupRequest{
		BackupID: "backup-123",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

func TestDeleteBackup_ManagerError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, backupMgr: &mockBackupManager{
		deleteBackupFn: func(ctx context.Context, backupID string) error {
			return stderrors.New("delete failed")
		},
	}}
	backupSvc := &backupService{svc: svc}

	resp, err := backupSvc.DeleteBackup(context.Background(), &DeleteBackupRequest{
		BackupID: "backup-123",
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
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

func TestGetCacheStats_NilCacheManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: nil}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.GetCacheStats(context.Background(), &GetCacheStatsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

func TestGetCacheStats_TypeAssertionFailure(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: &mockCacheInterface{}}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.GetCacheStats(context.Background(), &GetCacheStatsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestClearCache_NilCacheManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: nil}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.ClearCache(context.Background(), &ClearCacheRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

func TestClearCache_TypeAssertionFailure(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: &mockCacheInterface{}}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.ClearCache(context.Background(), &ClearCacheRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestGetCacheConfig_NilCacheManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: nil}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.GetCacheConfig(context.Background(), &GetCacheConfigRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

func TestGetCacheConfig_TypeAssertionFailure(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: &mockCacheInterface{}}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.GetCacheConfig(context.Background(), &GetCacheConfigRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestUpdateCacheConfig_NilCacheManager(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: nil}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.UpdateCacheConfig(context.Background(), &UpdateCacheConfigRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotImplemented, qbErr.Code)
}

func TestUpdateCacheConfig_TypeAssertionFailure(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, cacheManager: &mockCacheInterface{}}
	cacheSvc := &cacheService{svc: svc}

	resp, err := cacheSvc.UpdateCacheConfig(context.Background(), &UpdateCacheConfigRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}
