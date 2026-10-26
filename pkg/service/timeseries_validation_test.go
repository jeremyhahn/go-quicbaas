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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"encoding/json"

	"github.com/stretchr/testify/mock"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Time Series WritePoint Tests - Storage not configured
// =============================================================================

func TestTimeSeriesService_WritePoint_NoStorage(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.WritePoint(tenantctx.WithSystem(context.Background()), &WritePointRequest{
		Metric: "cpu.usage",
		Value:  42.0,
		Tags:   map[string]string{"host": "server1"},
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_WritePoint_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	// Storage not configured returns not-implemented before checking request
	resp, err := svc.WritePoint(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Time Series WriteBatch Tests - Storage not configured
// =============================================================================

func TestTimeSeriesService_WriteBatch_NoStorage(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.WriteBatch(tenantctx.WithSystem(context.Background()), &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu.usage", Value: 42.0},
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_WriteBatch_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.WriteBatch(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Time Series TSQuery Tests - Storage not configured
// =============================================================================

func TestTimeSeriesService_TSQuery_NoStorage(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.TSQuery(tenantctx.WithSystem(context.Background()), &TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Now(),
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_TSQuery_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.TSQuery(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Time Series DeleteSeries Tests - Storage not configured
// =============================================================================

func TestTimeSeriesService_DeleteSeries_NoStorage(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.DeleteSeries(tenantctx.WithSystem(context.Background()), &TSDeleteRequest{
		Metric: "cpu.usage",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_DeleteSeries_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.DeleteSeries(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Time Series GetRetentionPolicy Tests - Storage not configured
// =============================================================================

func TestTimeSeriesService_GetRetentionPolicy_NoStorage(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), &TSGetRetentionPolicyRequest{
		Metric: "cpu.usage",
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_GetRetentionPolicy_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Time Series SetRetentionPolicy Tests - Storage not configured
// =============================================================================

func TestTimeSeriesService_SetRetentionPolicy_NoStorage(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), &TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &TSRetentionPolicy{
			Duration: 30 * 24 * time.Hour,
		},
	})

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_SetRetentionPolicy_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Time Series ListMetrics Tests - Storage not configured
// =============================================================================

func TestTimeSeriesService_ListMetrics_NoStorage(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.ListMetrics(tenantctx.WithSystem(context.Background()), &TSListMetricsRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_ListMetrics_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	// ListMetrics handles nil request by creating a default
	resp, err := svc.ListMetrics(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GetRebalanceProgress Tests
// =============================================================================

func TestClusterService_GetRebalanceProgress_NilRequest(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.GetRebalanceProgress(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClusterService_GetRebalanceProgress_EmptyOperationID(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.GetRebalanceProgress(tenantctx.WithSystem(context.Background()), &GetRebalanceProgressRequest{
		OperationID: "",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "operation_id is required")
}

func TestClusterService_GetRebalanceProgress_OperationNotFound(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	resp, err := svc.GetRebalanceProgress(tenantctx.WithSystem(context.Background()), &GetRebalanceProgressRequest{
		OperationID: "non-existent-op-id",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "operation not found")
}

// =============================================================================
// Close Tests - Comprehensive shutdown scenarios
// =============================================================================

func TestQuicBaaS_Close_Idempotent(t *testing.T) {
	svc := createTestService(t)

	err := svc.Close()
	require.NoError(t, err)

	// Second close should return nil (already closed)
	err = svc.Close()
	require.NoError(t, err)
}

func TestTimeSeriesServiceImpl_Aggregate_ValidationError(t *testing.T) {
	svc := &quicBaaS{}
	tsSvc := &timeSeriesService{svc: svc}

	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), &AggregateRequest{})

	require.Error(t, err)
	// Just check it's an error about validation
	assert.NotNil(t, err)
}

func TestTimeSeriesServiceImpl_TimeSeriesQuery_ValidationError(t *testing.T) {
	svc := &quicBaaS{}
	tsSvc := &timeSeriesService{svc: svc}

	stream := make(chan *TimeSeriesQueryResponse, 1)

	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), &TimeSeriesQueryRequest{}, stream)

	require.Error(t, err)
	// Just check it's an error about validation
	assert.NotNil(t, err)
}

// Time-Series Service Tests - Aggregate validation (6.7% -> 15%).
func TestTimeSeriesServiceImpl_Aggregate_Success(t *testing.T) {
	entity1 := map[string]interface{}{
		"timestamp": float64(1000),
		"value":     100.0,
		"sensor":    "temp1",
	}
	entity2 := map[string]interface{}{
		"timestamp": float64(2000),
		"value":     200.0,
		"sensor":    "temp2",
	}

	data1, _ := json.Marshal(entity1)
	data2, _ := json.Marshal(entity2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{data1, data2}), nil)

	svc := &quicBaaS{
		qrdbClient:       qrdbProvider,
		config:           &ServiceConfig{EnableAuth: false},
		keyspaceResolver: NewKeyspaceResolver(),
		logger:           testLoggerCoverage(t),
	}

	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "sensors",
		FieldName:  "value",
		Function:   AggregateFunctionSum,
		StartTime:  1, // Must be > 0
		EndTime:    3000,
		Interval:   1000,
	}

	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestTimeSeriesServiceImpl_Aggregate_InvalidInput(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
		logger: testLoggerCoverage(t),
	}

	tsSvc := &timeSeriesService{svc: svc}

	tests := []struct {
		name string
		req  *AggregateRequest
	}{
		{
			name: "missing entity type",
			req:  &AggregateRequest{},
		},
		{
			name: "missing field name",
			req: &AggregateRequest{
				EntityType: "sensors",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), tt.req)
			assert.Error(t, err)
		})
	}
}

func TestTimeSeriesService_Aggregate_EmptyEntityType(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &AggregateRequest{
		EntityType: "",
		StartTime:  1,
		EndTime:    2,
	}

	resp, err := svc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "entity_type is required")
}

func TestTimeSeriesService_Aggregate_InvalidStartTime(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  0,
		EndTime:    100,
	}

	resp, err := svc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "start_time must be greater than 0")
}

func TestTimeSeriesService_TSQuery_MissingStartTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &TSQueryRequest{
		Metric:  "cpu.usage",
		EndTime: time.Now(),
	}

	resp, err := tsSvc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	mockTS.AssertNotCalled(t, "QueryRich")
}

func TestTimeSeriesService_TSQuery_MissingEndTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: time.Now().Add(-time.Hour),
	}

	resp, err := tsSvc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	mockTS.AssertNotCalled(t, "QueryRich")
}

func TestTimeSeriesService_TSQuery_WithAggregation(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	now := time.Now()
	aggValue := 77.75
	queryResult := &interfaces.QueryResult{
		Metric:          "cpu.usage",
		AggregatedValue: &aggValue,
		Points:          []*interfaces.DataPoint{},
	}

	mockTS.On("QueryRich", mock.Anything, mock.MatchedBy(func(qr *interfaces.QueryRequest) bool {
		return qr.Aggregation == interfaces.AggAvg
	})).Return(queryResult, nil).Once()

	req := &TSQueryRequest{
		Metric:      "cpu.usage",
		StartTime:   now.Add(-time.Hour),
		EndTime:     now,
		Aggregation: "avg",
	}

	resp, err := tsSvc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "cpu.usage", resp.Metric)
	require.NotNil(t, resp.AggregatedValue)
	assert.Equal(t, 77.75, *resp.AggregatedValue)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_TSQuery_WithGroupedResults(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	now := time.Now()
	aggVal1 := 80.0
	aggVal2 := 60.5

	queryResult := &interfaces.QueryResult{
		Metric: "cpu.usage",
		GroupedResults: []*interfaces.GroupedResult{
			{
				Tags:            map[string]string{"host": "server1"},
				AggregatedValue: &aggVal1,
				Points: []*interfaces.DataPoint{
					{Metric: "cpu.usage", Tags: map[string]string{"host": "server1"}, Value: 80.0, Timestamp: now},
				},
			},
			{
				Tags:            map[string]string{"host": "server2"},
				AggregatedValue: &aggVal2,
				Points:          []*interfaces.DataPoint{},
			},
		},
	}

	mockTS.On("QueryRich", mock.Anything, mock.AnythingOfType("*interfaces.QueryRequest")).
		Return(queryResult, nil).Once()

	req := &TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
		GroupBy:   []string{"host"},
	}

	resp, err := tsSvc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.GroupedResults, 2)

	// First group has points
	assert.Equal(t, map[string]string{"host": "server1"}, resp.GroupedResults[0].Tags)
	require.NotNil(t, resp.GroupedResults[0].AggregatedValue)
	assert.Equal(t, 80.0, *resp.GroupedResults[0].AggregatedValue)
	require.Len(t, resp.GroupedResults[0].Points, 1)
	assert.Equal(t, 80.0, resp.GroupedResults[0].Points[0].Value)

	// Second group has no points but has aggregated value
	assert.Equal(t, map[string]string{"host": "server2"}, resp.GroupedResults[1].Tags)
	require.NotNil(t, resp.GroupedResults[1].AggregatedValue)
	assert.Equal(t, 60.5, *resp.GroupedResults[1].AggregatedValue)
	assert.Empty(t, resp.GroupedResults[1].Points)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_TSQuery_WithAllQueryParams(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	now := time.Now()
	queryResult := &interfaces.QueryResult{
		Metric: "disk.io",
		Tags:   map[string]string{"device": "sda"},
		Points: []*interfaces.DataPoint{
			{Metric: "disk.io", Tags: map[string]string{"device": "sda"}, Value: 150.0, Timestamp: now},
		},
	}

	mockTS.On("QueryRich", mock.Anything, mock.MatchedBy(func(qr *interfaces.QueryRequest) bool {
		return qr.Metric == "disk.io" &&
			qr.Interval == 60*time.Second &&
			qr.Limit == 100 &&
			qr.Percentile == 95.0 &&
			qr.Tags["device"] == "sda"
	})).Return(queryResult, nil).Once()

	req := &TSQueryRequest{
		Metric:      "disk.io",
		StartTime:   now.Add(-time.Hour),
		EndTime:     now,
		Tags:        map[string]string{"device": "sda"},
		IntervalSec: 60,
		Limit:       100,
		Percentile:  95.0,
	}

	resp, err := tsSvc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "disk.io", resp.Metric)
	assert.Equal(t, map[string]string{"device": "sda"}, resp.Tags)
	require.Len(t, resp.Points, 1)
	assert.Equal(t, 150.0, resp.Points[0].Value)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_TSQuery_NilStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc, tsStorage: nil}

	now := time.Now()
	req := &TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
	}

	resp, err := tsSvc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_DeleteSeries_WithTimeRange(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	mockTS.On("DeleteSeries", mock.Anything, mock.MatchedBy(func(dr *interfaces.DeleteRequest) bool {
		return dr.Metric == "old.metric" &&
			!dr.StartTime.IsZero() &&
			!dr.EndTime.IsZero()
	})).Return(nil).Once()

	req := &TSDeleteRequest{
		Metric:    "old.metric",
		Tags:      map[string]string{"region": "us-east"},
		StartTime: &startTime,
		EndTime:   &endTime,
	}

	resp, err := tsSvc.DeleteSeries(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_DeleteSeries_StorageNotConfigured(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc, tsStorage: nil}

	req := &TSDeleteRequest{
		Metric: "cpu.usage",
	}

	resp, err := tsSvc.DeleteSeries(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_DeleteSeries_NilReq(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	resp, err := tsSvc.DeleteSeries(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestTimeSeriesService_DeleteSeries_EmptyMetricName(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &TSDeleteRequest{
		Metric: "",
	}

	resp, err := tsSvc.DeleteSeries(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestTimeSeriesService_DeleteSeries_OnlyStartTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	startTime := time.Now().Add(-48 * time.Hour)

	mockTS.On("DeleteSeries", mock.Anything, mock.MatchedBy(func(dr *interfaces.DeleteRequest) bool {
		return dr.Metric == "stale.metric" &&
			!dr.StartTime.IsZero() &&
			dr.EndTime.IsZero()
	})).Return(nil).Once()

	req := &TSDeleteRequest{
		Metric:    "stale.metric",
		StartTime: &startTime,
	}

	resp, err := tsSvc.DeleteSeries(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_DeleteSeries_OnlyEndTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	endTime := time.Now()

	mockTS.On("DeleteSeries", mock.Anything, mock.MatchedBy(func(dr *interfaces.DeleteRequest) bool {
		return dr.Metric == "archived.metric" &&
			dr.StartTime.IsZero() &&
			!dr.EndTime.IsZero()
	})).Return(nil).Once()

	req := &TSDeleteRequest{
		Metric:  "archived.metric",
		EndTime: &endTime,
	}

	resp, err := tsSvc.DeleteSeries(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_WriteBatch_ZeroTimestampDefaultsToNow(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	before := time.Now()

	mockTS.On("WriteBatch", mock.MatchedBy(func(points []interfaces.DataPoint) bool {
		if len(points) != 1 {
			return false
		}
		return !points[0].Timestamp.IsZero() && !points[0].Timestamp.Before(before)
	})).Return(nil).Once()

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{
				Metric: "mem.usage",
				Value:  2048.0,
			},
		},
	}

	resp, err := tsSvc.WriteBatch(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, 1, resp.PointsWritten)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_WritePoint_StorageNotConfigured(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc, tsStorage: nil}

	req := &WritePointRequest{
		Metric: "cpu.usage",
		Value:  42.0,
	}

	resp, err := tsSvc.WritePoint(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_WriteBatch_StorageNotConfigured(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc, tsStorage: nil}

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu.usage", Value: 42.0},
		},
	}

	resp, err := tsSvc.WriteBatch(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_SetRetentionPolicy_StorageNotConfigured(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc, tsStorage: nil}

	req := &TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &TSRetentionPolicy{Duration: 7 * 24 * time.Hour},
	}

	resp, err := tsSvc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_SetRetentionPolicy_NilReq(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	resp, err := tsSvc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestTimeSeriesService_SetRetentionPolicy_EmptyMetricName(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &TSSetRetentionPolicyRequest{
		Metric: "",
		Policy: &TSRetentionPolicy{Duration: 7 * 24 * time.Hour},
	}

	resp, err := tsSvc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestTimeSeriesService_SetRetentionPolicy_NilPolicyField(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: nil,
	}

	resp, err := tsSvc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestTimeSeriesService_GetRetentionPolicy_StorageNotConfigured(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc, tsStorage: nil}

	req := &TSGetRetentionPolicyRequest{Metric: "cpu.usage"}

	resp, err := tsSvc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
}

func TestTimeSeriesService_GetRetentionPolicy_NilReq(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	resp, err := tsSvc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestTimeSeriesService_GetRetentionPolicy_EmptyMetricName(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	req := &TSGetRetentionPolicyRequest{Metric: ""}

	resp, err := tsSvc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
}

func TestTimeSeriesService_GetRetentionPolicy_NilPolicyReturned(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	mockTS.On("GetRetentionPolicy", "new.metric").
		Return((*interfaces.RetentionPolicy)(nil), nil).Once()

	req := &TSGetRetentionPolicyRequest{Metric: "new.metric"}

	resp, err := tsSvc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "new.metric", resp.Metric)
	assert.Nil(t, resp.Policy)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesService_SetRetentionPolicy_WithAggregation(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockTS := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockTS}

	mockTS.On("SetRetentionPolicy", "cpu.usage", mock.MatchedBy(func(p *interfaces.RetentionPolicy) bool {
		return p.Duration == 30*24*time.Hour &&
			p.Resolution == 5*time.Minute &&
			p.Aggregation == interfaces.AggAvg
	})).Return(nil).Once()

	req := &TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &TSRetentionPolicy{
			Duration:    30 * 24 * time.Hour,
			Resolution:  5 * time.Minute,
			Aggregation: "avg",
		},
	}

	resp, err := tsSvc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockTS.AssertExpectations(t)
}

func TestTimeSeriesServiceImpl_TSQuery_NilRequest(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	_, err := svc.TSQuery(tenantctx.WithSystem(context.Background()), nil)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, qbErr.Error(), "request cannot be nil")
}

func TestTimeSeriesServiceImpl_TSQuery_EmptyMetric(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &TSQueryRequest{
		Metric: "", // Empty metric should trigger validation error
	}

	_, err := svc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, qbErr.Error(), "metric is required")
}

func TestTimeSeriesServiceImpl_TSQuery_ZeroStartTime(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &TSQueryRequest{
		Metric:    "test.metric",
		StartTime: time.Time{}, // Zero start time should trigger validation error
	}

	_, err := svc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, qbErr.Error(), "start_time is required")
}

func TestTimeSeriesServiceImpl_TSQuery_ZeroEndTime(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &TSQueryRequest{
		Metric:    "test.metric",
		StartTime: time.Now().Add(-1 * time.Hour),
		EndTime:   time.Time{}, // Zero end time should trigger validation error
	}

	_, err := svc.TSQuery(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, qbErr.Error(), "end_time is required")
}

func TestTimeSeriesServiceImpl_WritePoint_NilRequest(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	_, err := svc.WritePoint(tenantctx.WithSystem(context.Background()), nil)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_WritePoint_EmptyMetric(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &WritePointRequest{
		Metric: "",
		Value:  42.0,
	}

	_, err := svc.WritePoint(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_WriteBatch_NilRequest(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	_, err := svc.WriteBatch(tenantctx.WithSystem(context.Background()), nil)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_GetRetentionPolicy_NilRequest(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	_, err := svc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), nil)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_GetRetentionPolicy_EmptyMetric(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &TSGetRetentionPolicyRequest{
		Metric: "",
	}

	_, err := svc.GetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_SetRetentionPolicy_NilRequest(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	_, err := svc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), nil)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_SetRetentionPolicy_EmptyMetric(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &TSSetRetentionPolicyRequest{
		Metric: "",
		Policy: &TSRetentionPolicy{
			Duration: 24 * time.Hour,
		},
	}

	_, err := svc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_SetRetentionPolicy_NilPolicy(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &TSSetRetentionPolicyRequest{
		Metric: "test.metric",
		Policy: nil,
	}

	_, err := svc.SetRetentionPolicy(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_DeleteSeries_NilRequest(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	_, err := svc.DeleteSeries(tenantctx.WithSystem(context.Background()), nil)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesServiceImpl_DeleteSeries_EmptyMetric(t *testing.T) {
	svc := &timeSeriesService{
		tsStorage: newTestMockTimeSeriesStorage(),
	}

	req := &TSDeleteRequest{
		Metric: "",
	}

	_, err := svc.DeleteSeries(tenantctx.WithSystem(context.Background()), req)
	assert.Error(t, err)
	qbErr, ok := err.(*dberrors.QBError)
	require.True(t, ok)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}
