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
// Time Series request validation
// =============================================================================

// TestTimeSeriesService_StorageNotConfigured checks that every time-series
// method of the service facade returns ErrNotImplemented when no time-series
// storage is configured.
func TestTimeSeriesService_StorageNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name string
		call func() (any, error)
	}{
		{"WritePoint", func() (any, error) {
			return svc.WritePoint(ctx, &WritePointRequest{Metric: "cpu.usage", Value: 42.0, Tags: map[string]string{"host": "server1"}})
		}},
		{"WriteBatch", func() (any, error) {
			return svc.WriteBatch(ctx, &WriteBatchRequest{Points: []*WritePointRequest{{Metric: "cpu.usage", Value: 42.0}}})
		}},
		{"TSQuery", func() (any, error) {
			return svc.TSQuery(ctx, &TSQueryRequest{Metric: "cpu.usage", StartTime: time.Now().Add(-1 * time.Hour), EndTime: time.Now()})
		}},
		{"DeleteSeries", func() (any, error) {
			return svc.DeleteSeries(ctx, &TSDeleteRequest{Metric: "cpu.usage"})
		}},
		{"GetRetentionPolicy", func() (any, error) {
			return svc.GetRetentionPolicy(ctx, &TSGetRetentionPolicyRequest{Metric: "cpu.usage"})
		}},
		{"SetRetentionPolicy", func() (any, error) {
			return svc.SetRetentionPolicy(ctx, &TSSetRetentionPolicyRequest{Metric: "cpu.usage", Policy: &TSRetentionPolicy{Duration: 30 * 24 * time.Hour}})
		}},
		{"ListMetrics", func() (any, error) {
			return svc.ListMetrics(ctx, &TSListMetricsRequest{})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.call()
			require.Error(t, err)
			assert.Nil(t, resp)

			var dErr *dberrors.QBError
			require.True(t, dberrors.As(err, &dErr))
			assert.Equal(t, dberrors.ErrNotImplemented, dErr.Code)
		})
	}
}

// TestTimeSeriesService_InvalidRequest checks that each time-series method
// rejects a nil or incomplete request with ErrInvalidInput before it reaches
// the storage.
func TestTimeSeriesService_InvalidRequest(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())

	tests := []struct {
		name    string
		call    func(ts *timeSeriesService) (any, error)
		wantMsg string
	}{
		{"WritePoint nil request", func(ts *timeSeriesService) (any, error) {
			return ts.WritePoint(ctx, nil)
		}, ""},
		{"WritePoint empty metric", func(ts *timeSeriesService) (any, error) {
			return ts.WritePoint(ctx, &WritePointRequest{Value: 42.0})
		}, ""},
		{"WriteBatch nil request", func(ts *timeSeriesService) (any, error) {
			return ts.WriteBatch(ctx, nil)
		}, ""},
		{"TSQuery nil request", func(ts *timeSeriesService) (any, error) {
			return ts.TSQuery(ctx, nil)
		}, "request cannot be nil"},
		{"TSQuery empty metric", func(ts *timeSeriesService) (any, error) {
			return ts.TSQuery(ctx, &TSQueryRequest{})
		}, "metric is required"},
		{"TSQuery zero start time", func(ts *timeSeriesService) (any, error) {
			return ts.TSQuery(ctx, &TSQueryRequest{Metric: "test.metric"})
		}, "start_time is required"},
		{"TSQuery zero end time", func(ts *timeSeriesService) (any, error) {
			return ts.TSQuery(ctx, &TSQueryRequest{Metric: "test.metric", StartTime: time.Now().Add(-1 * time.Hour)})
		}, "end_time is required"},
		{"DeleteSeries nil request", func(ts *timeSeriesService) (any, error) {
			return ts.DeleteSeries(ctx, nil)
		}, ""},
		{"DeleteSeries empty metric", func(ts *timeSeriesService) (any, error) {
			return ts.DeleteSeries(ctx, &TSDeleteRequest{})
		}, ""},
		{"GetRetentionPolicy nil request", func(ts *timeSeriesService) (any, error) {
			return ts.GetRetentionPolicy(ctx, nil)
		}, ""},
		{"GetRetentionPolicy empty metric", func(ts *timeSeriesService) (any, error) {
			return ts.GetRetentionPolicy(ctx, &TSGetRetentionPolicyRequest{})
		}, ""},
		{"SetRetentionPolicy nil request", func(ts *timeSeriesService) (any, error) {
			return ts.SetRetentionPolicy(ctx, nil)
		}, ""},
		{"SetRetentionPolicy empty metric", func(ts *timeSeriesService) (any, error) {
			return ts.SetRetentionPolicy(ctx, &TSSetRetentionPolicyRequest{Policy: &TSRetentionPolicy{Duration: 7 * 24 * time.Hour}})
		}, ""},
		{"SetRetentionPolicy nil policy", func(ts *timeSeriesService) (any, error) {
			return ts.SetRetentionPolicy(ctx, &TSSetRetentionPolicyRequest{Metric: "cpu.usage"})
		}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &timeSeriesService{
				svc:       &quicBaaS{logger: createTestLogger(t)},
				tsStorage: newTestMockTimeSeriesStorage(),
			}

			resp, err := tt.call(ts)
			require.Error(t, err)
			assert.Nil(t, resp)

			var dErr *dberrors.QBError
			require.True(t, dberrors.As(err, &dErr))
			assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
			if tt.wantMsg != "" {
				assert.Contains(t, err.Error(), tt.wantMsg)
			}
		})
	}
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

func TestTimeSeriesServiceImpl_TimeSeriesQuery_ValidationError(t *testing.T) {
	svc := &quicBaaS{}
	tsSvc := &timeSeriesService{svc: svc}

	stream := make(chan *TimeSeriesQueryResponse, 1)

	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), &TimeSeriesQueryRequest{}, stream)

	require.Error(t, err)
	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	assert.Contains(t, err.Error(), "entity_type is required")
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
		qrdbClient: qrdbProvider,
		config:     &ServiceConfig{EnableAuth: false},
		logger:     testLoggerCoverage(t),
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

func TestTimeSeriesService_Aggregate_InvalidInput(t *testing.T) {
	svc := createTestService(t, withMockRaftNode(&mockRaftNode{}))
	defer svc.Close()

	tests := []struct {
		name    string
		req     *AggregateRequest
		wantMsg string
	}{
		{"missing entity type", &AggregateRequest{StartTime: 1, EndTime: 2}, "entity_type is required"},
		{"zero start time", &AggregateRequest{EntityType: "metrics", EndTime: 100}, "start_time must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.Aggregate(tenantctx.WithSystem(context.Background()), tt.req)
			require.Error(t, err)
			require.Nil(t, resp)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
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

// TestTimeSeriesService_DeleteSeries_TimeRange checks that each bound of the
// delete time range reaches the storage only when the request sets it.
func TestTimeSeriesService_DeleteSeries_TimeRange(t *testing.T) {
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	tests := []struct {
		name      string
		req       *TSDeleteRequest
		wantStart bool
		wantEnd   bool
	}{
		{"start and end", &TSDeleteRequest{
			Metric:    "old.metric",
			Tags:      map[string]string{"region": "us-east"},
			StartTime: &startTime,
			EndTime:   &endTime,
		}, true, true},
		{"start only", &TSDeleteRequest{Metric: "stale.metric", StartTime: &startTime}, true, false},
		{"end only", &TSDeleteRequest{Metric: "archived.metric", EndTime: &endTime}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTS := newTestMockTimeSeriesStorage()
			tsSvc := &timeSeriesService{svc: &quicBaaS{logger: createTestLogger(t)}, tsStorage: mockTS}

			mockTS.On("DeleteSeries", mock.Anything, mock.MatchedBy(func(dr *interfaces.DeleteRequest) bool {
				return dr.Metric == tt.req.Metric &&
					dr.StartTime.IsZero() != tt.wantStart &&
					dr.EndTime.IsZero() != tt.wantEnd
			})).Return(nil).Once()

			resp, err := tsSvc.DeleteSeries(tenantctx.WithSystem(context.Background()), tt.req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.True(t, resp.Success)
			mockTS.AssertExpectations(t)
		})
	}
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
