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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Test Mock TimeSeriesStorage Definition (local to avoid circular imports)
// =============================================================================

// testMockTimeSeriesStorage is a mock implementation of interfaces.TimeSeriesStorage for testing.
type testMockTimeSeriesStorage struct {
	mock.Mock
}

var _ interfaces.TimeSeriesStorage = (*testMockTimeSeriesStorage)(nil)

func newTestMockTimeSeriesStorage() *testMockTimeSeriesStorage {
	return &testMockTimeSeriesStorage{}
}

func (m *testMockTimeSeriesStorage) WritePoint(ctx context.Context, point *interfaces.DataPoint) error {
	args := m.Called(ctx, point)
	return args.Error(0)
}

func (m *testMockTimeSeriesStorage) WriteBatch(points []interfaces.DataPoint) error {
	args := m.Called(points)
	return args.Error(0)
}

func (m *testMockTimeSeriesStorage) QueryRich(ctx context.Context, req *interfaces.QueryRequest) (*interfaces.QueryResult, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.QueryResult), args.Error(1)
}

func (m *testMockTimeSeriesStorage) DeleteSeries(ctx context.Context, req *interfaces.DeleteRequest) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

func (m *testMockTimeSeriesStorage) GetRetentionPolicy(metric string) (*interfaces.RetentionPolicy, error) {
	args := m.Called(metric)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.RetentionPolicy), args.Error(1)
}

func (m *testMockTimeSeriesStorage) SetRetentionPolicy(metric string, policy *interfaces.RetentionPolicy) error {
	args := m.Called(metric, policy)
	return args.Error(0)
}

func (m *testMockTimeSeriesStorage) ListMetrics(prefix string, limit int) ([]string, error) {
	args := m.Called(prefix, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// =============================================================================
// WritePoint Tests with Mock Storage
// =============================================================================

func TestTimeSeriesService_WritePoint_Success_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	mockStorage.On("WritePoint", mock.Anything, mock.AnythingOfType("*interfaces.DataPoint")).
		Return(nil).Once()

	req := &WritePointRequest{
		Metric: "cpu.usage",
		Tags:   map[string]string{"host": "server1"},
		Value:  75.5,
	}

	resp, err := tsSvc.WritePoint(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.False(t, resp.Timestamp.IsZero())
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_WritePoint_Success_WithTimestamp(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	specificTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	mockStorage.On("WritePoint", mock.Anything, mock.MatchedBy(func(dp *interfaces.DataPoint) bool {
		return dp.Timestamp.Equal(specificTime)
	})).Return(nil).Once()

	req := &WritePointRequest{
		Metric:    "memory.used",
		Tags:      map[string]string{"host": "server2"},
		Value:     2048.0,
		Timestamp: specificTime,
	}

	resp, err := tsSvc.WritePoint(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_WritePoint_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	storageErr := errors.New("disk write failed")
	mockStorage.On("WritePoint", mock.Anything, mock.AnythingOfType("*interfaces.DataPoint")).
		Return(storageErr).Once()

	req := &WritePointRequest{
		Metric: "disk.io",
		Value:  100.0,
	}

	resp, err := tsSvc.WritePoint(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_WritePoint_EmptyMetric_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	req := &WritePointRequest{
		Metric: "",
		Value:  50.0,
	}

	resp, err := tsSvc.WritePoint(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	// Storage should not be called when validation fails
	mockStorage.AssertNotCalled(t, "WritePoint")
}

func TestTimeSeriesService_WritePoint_NilRequest_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	resp, err := tsSvc.WritePoint(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	mockStorage.AssertNotCalled(t, "WritePoint")
}

// =============================================================================
// WriteBatch Tests with Mock Storage
// =============================================================================

func TestTimeSeriesService_WriteBatch_Success_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	mockStorage.On("WriteBatch", mock.AnythingOfType("[]interfaces.DataPoint")).
		Return(nil).Once()

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5, Tags: map[string]string{"host": "server1"}},
			{Metric: "memory.used", Value: 2048.0, Tags: map[string]string{"host": "server1"}},
			{Metric: "disk.io", Value: 100.0, Tags: map[string]string{"host": "server1"}},
		},
	}

	resp, err := tsSvc.WriteBatch(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, 3, resp.PointsWritten)
	assert.False(t, resp.Timestamp.IsZero())
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_WriteBatch_EmptyPoints(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{},
	}

	resp, err := tsSvc.WriteBatch(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, 0, resp.PointsWritten)
	// Storage should not be called for empty batch
	mockStorage.AssertNotCalled(t, "WriteBatch")
}

func TestTimeSeriesService_WriteBatch_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	storageErr := errors.New("batch write failed")
	mockStorage.On("WriteBatch", mock.AnythingOfType("[]interfaces.DataPoint")).
		Return(storageErr).Once()

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5},
		},
	}

	resp, err := tsSvc.WriteBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_WriteBatch_NilPointInBatch(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5},
			nil, // nil point
			{Metric: "memory.used", Value: 2048.0},
		},
	}

	resp, err := tsSvc.WriteBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	assert.Contains(t, dErr.Error(), "item 1")
	mockStorage.AssertNotCalled(t, "WriteBatch")
}

func TestTimeSeriesService_WriteBatch_EmptyMetricInBatch(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	req := &WriteBatchRequest{
		Points: []*WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5},
			{Metric: "", Value: 100.0}, // empty metric
		},
	}

	resp, err := tsSvc.WriteBatch(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	assert.Contains(t, dErr.Error(), "item 1")
	mockStorage.AssertNotCalled(t, "WriteBatch")
}

func TestTimeSeriesService_WriteBatch_NilRequest_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	resp, err := tsSvc.WriteBatch(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	mockStorage.AssertNotCalled(t, "WriteBatch")
}

// =============================================================================
// TSQuery Tests with Mock Storage
// =============================================================================

func TestTimeSeriesService_TSQuery_Success_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	now := time.Now()
	queryResult := &interfaces.QueryResult{
		Points: []*interfaces.DataPoint{
			{Metric: "cpu.usage", Value: 75.5, Timestamp: now.Add(-time.Minute)},
			{Metric: "cpu.usage", Value: 80.0, Timestamp: now},
		},
	}

	mockStorage.On("QueryRich", mock.Anything, mock.AnythingOfType("*interfaces.QueryRequest")).
		Return(queryResult, nil).Once()

	req := &TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
	}

	resp, err := tsSvc.TSQuery(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Points, 2)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_TSQuery_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	storageErr := errors.New("query failed")
	mockStorage.On("QueryRich", mock.Anything, mock.AnythingOfType("*interfaces.QueryRequest")).
		Return(nil, storageErr).Once()

	now := time.Now()
	req := &TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
	}

	resp, err := tsSvc.TSQuery(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_TSQuery_NilRequest_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	resp, err := tsSvc.TSQuery(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	mockStorage.AssertNotCalled(t, "QueryRich")
}

func TestTimeSeriesService_TSQuery_EmptyMetric_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	now := time.Now()
	req := &TSQueryRequest{
		Metric:    "",
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
	}

	resp, err := tsSvc.TSQuery(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInvalidInput, dErr.Code)
	mockStorage.AssertNotCalled(t, "QueryRich")
}

// =============================================================================
// DeleteSeries Tests with Mock Storage
// =============================================================================

func TestTimeSeriesService_DeleteSeries_Success_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	mockStorage.On("DeleteSeries", mock.Anything, mock.AnythingOfType("*interfaces.DeleteRequest")).
		Return(nil).Once()

	req := &TSDeleteRequest{
		Metric: "cpu.usage",
	}

	resp, err := tsSvc.DeleteSeries(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_DeleteSeries_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	storageErr := errors.New("delete failed")
	mockStorage.On("DeleteSeries", mock.Anything, mock.AnythingOfType("*interfaces.DeleteRequest")).
		Return(storageErr).Once()

	req := &TSDeleteRequest{
		Metric: "cpu.usage",
	}

	resp, err := tsSvc.DeleteSeries(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)

	var dErr *dberrors.QBError
	require.True(t, dberrors.As(err, &dErr))
	assert.Equal(t, dberrors.ErrInternal, dErr.Code)
	mockStorage.AssertExpectations(t)
}

// =============================================================================
// GetRetentionPolicy Tests with Mock Storage
// =============================================================================

func TestTimeSeriesService_GetRetentionPolicy_Success_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	policy := &interfaces.RetentionPolicy{
		Duration: 30 * 24 * time.Hour,
	}
	mockStorage.On("GetRetentionPolicy", "cpu.usage").
		Return(policy, nil).Once()

	req := &TSGetRetentionPolicyRequest{
		Metric: "cpu.usage",
	}

	resp, err := tsSvc.GetRetentionPolicy(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Policy)
	assert.Equal(t, 30*24*time.Hour, resp.Policy.Duration)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_GetRetentionPolicy_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	storageErr := errors.New("policy not found")
	mockStorage.On("GetRetentionPolicy", "unknown.metric").
		Return(nil, storageErr).Once()

	req := &TSGetRetentionPolicyRequest{
		Metric: "unknown.metric",
	}

	resp, err := tsSvc.GetRetentionPolicy(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	mockStorage.AssertExpectations(t)
}

// =============================================================================
// SetRetentionPolicy Tests with Mock Storage
// =============================================================================

func TestTimeSeriesService_SetRetentionPolicy_Success_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	mockStorage.On("SetRetentionPolicy", "cpu.usage", mock.AnythingOfType("*interfaces.RetentionPolicy")).
		Return(nil).Once()

	req := &TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &TSRetentionPolicy{
			Duration: 7 * 24 * time.Hour,
		},
	}

	resp, err := tsSvc.SetRetentionPolicy(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_SetRetentionPolicy_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	storageErr := errors.New("failed to set policy")
	mockStorage.On("SetRetentionPolicy", "cpu.usage", mock.AnythingOfType("*interfaces.RetentionPolicy")).
		Return(storageErr).Once()

	req := &TSSetRetentionPolicyRequest{
		Metric: "cpu.usage",
		Policy: &TSRetentionPolicy{
			Duration: 7 * 24 * time.Hour,
		},
	}

	resp, err := tsSvc.SetRetentionPolicy(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	mockStorage.AssertExpectations(t)
}

// =============================================================================
// ListMetrics Tests with Mock Storage
// =============================================================================

func TestTimeSeriesService_ListMetrics_Success_WithStorage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	metrics := []string{"cpu.usage", "cpu.idle", "cpu.system"}
	mockStorage.On("ListMetrics", "cpu.", 100).
		Return(metrics, nil).Once()

	req := &TSListMetricsRequest{
		Prefix: "cpu.",
		Limit:  100,
	}

	resp, err := tsSvc.ListMetrics(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Metrics, 3)
	assert.Contains(t, resp.Metrics, "cpu.usage")
	mockStorage.AssertExpectations(t)
}

func TestTimeSeriesService_ListMetrics_StorageError(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	mockStorage := newTestMockTimeSeriesStorage()
	tsSvc := &timeSeriesService{svc: svc, tsStorage: mockStorage}

	storageErr := errors.New("list failed")
	mockStorage.On("ListMetrics", "", 100).
		Return(nil, storageErr).Once()

	req := &TSListMetricsRequest{
		Prefix: "",
		Limit:  100,
	}

	resp, err := tsSvc.ListMetrics(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	mockStorage.AssertExpectations(t)
}
