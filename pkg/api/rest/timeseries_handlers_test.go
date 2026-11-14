//go:build rest && timeseries

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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// setupTimeSeriesTestServer creates a test server with mocked service
func setupTimeSeriesTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()

	mockService := testutil.NewMockQuicBaaS()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)

	server, err := NewServer(mockService, DefaultConfig(), log)
	require.NoError(t, err)

	return server, mockService
}

// TestHandleWritePoint tests writing a single data point
func TestHandleWritePoint(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	tests := []struct {
		name           string
		requestBody    interface{}
		setupMock      func()
		expectedStatus int
		checkResponse  func(t *testing.T, resp *httptest.ResponseRecorder)
	}{
		{
			name: "valid_write_point",
			requestBody: WritePointRequest{
				Metric:    "cpu.usage",
				Tags:      map[string]string{"host": "server1", "region": "us-east-1"},
				Value:     75.5,
				Timestamp: time.Now(),
			},
			setupMock: func() {
				mockService.On("WritePoint", mock.Anything, mock.Anything).
					Return(&service.WritePointResponse{Success: true}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response WritePointResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.True(t, response.Success)
				assert.Empty(t, response.Error)
			},
		},
		{
			name: "missing_metric",
			requestBody: WritePointRequest{
				Value:     75.5,
				Timestamp: time.Now(),
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "metric is required")
			},
		},
		{
			name: "missing_timestamp",
			requestBody: WritePointRequest{
				Metric: "cpu.usage",
				Value:  75.5,
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "timestamp is required")
			},
		},
		{
			name:           "invalid_json",
			requestBody:    "invalid json",
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "invalid JSON")
			},
		},
		{
			name: "service_error",
			requestBody: WritePointRequest{
				Metric:    "cpu.usage",
				Value:     75.5,
				Timestamp: time.Now(),
			},
			setupMock: func() {
				mockService.On("WritePoint", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "WritePoint",
					Err:  stderrors.New("internal error"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			// Prepare request
			var reqBody []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				reqBody = []byte(str)
			} else {
				reqBody, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			// Execute handler
			server.HandleWritePoint(resp, req)

			// Check status code
			assert.Equal(t, tt.expectedStatus, resp.Code)

			// Run additional response checks
			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}
		})
	}
}

// TestHandleWriteBatch tests writing multiple data points
func TestHandleWriteBatch(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	tests := []struct {
		name           string
		requestBody    interface{}
		setupMock      func()
		expectedStatus int
		checkResponse  func(t *testing.T, resp *httptest.ResponseRecorder)
	}{
		{
			name: "valid_batch_write",
			requestBody: WriteBatchRequest{
				Points: []WritePointRequest{
					{
						Metric:    "cpu.usage",
						Tags:      map[string]string{"host": "server1"},
						Value:     75.5,
						Timestamp: time.Now(),
					},
					{
						Metric:    "cpu.usage",
						Tags:      map[string]string{"host": "server2"},
						Value:     82.3,
						Timestamp: time.Now(),
					},
					{
						Metric:    "memory.used",
						Tags:      map[string]string{"host": "server1"},
						Value:     4096,
						Timestamp: time.Now(),
					},
				},
			},
			setupMock: func() {
				mockService.On("WriteBatch", mock.Anything, mock.Anything).
					Return(&service.WriteBatchResponse{PointsWritten: 3, Success: true}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response WriteBatchResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.True(t, response.Success)
				assert.Equal(t, 3, response.PointsWritten)
				assert.Empty(t, response.Error)
			},
		},
		{
			name: "empty_batch",
			requestBody: WriteBatchRequest{
				Points: []WritePointRequest{},
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "no points provided")
			},
		},
		{
			name: "invalid_point_in_batch",
			requestBody: WriteBatchRequest{
				Points: []WritePointRequest{
					{
						Metric:    "cpu.usage",
						Tags:      map[string]string{"host": "server1"},
						Value:     75.5,
						Timestamp: time.Now(),
					},
					{
						// Missing metric
						Value:     82.3,
						Timestamp: time.Now(),
					},
				},
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "metric is required for point 1")
			},
		},
		{
			name: "missing_timestamp_in_point",
			requestBody: WriteBatchRequest{
				Points: []WritePointRequest{
					{Metric: "cpu.usage", Value: 75.5, Timestamp: time.Now()},
					{Metric: "memory.usage", Value: 60.2},
				},
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "timestamp is required for point 1")
			},
		},
		{
			name:           "invalid_json",
			requestBody:    "invalid",
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service_error",
			requestBody: WriteBatchRequest{
				Points: []WritePointRequest{
					{Metric: "cpu.usage", Value: 75.5, Timestamp: time.Now()},
				},
			},
			setupMock: func() {
				mockService.On("WriteBatch", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "WriteBatch",
					Err:  stderrors.New("internal error"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			// Prepare request
			var reqBody []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				reqBody = []byte(str)
			} else {
				reqBody, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/batch", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			// Execute handler
			server.HandleWriteBatch(resp, req)

			// Check status code
			assert.Equal(t, tt.expectedStatus, resp.Code)

			// Run additional response checks
			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}
		})
	}
}

// TestHandleTSQuery tests time series queries
func TestHandleTSQuery(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	tests := []struct {
		name           string
		requestBody    interface{}
		setupMock      func()
		expectedStatus int
		checkResponse  func(t *testing.T, resp *httptest.ResponseRecorder)
	}{
		{
			name: "valid_query",
			requestBody: TSQueryRequest{
				Metric:    "cpu.usage",
				StartTime: startTime,
				EndTime:   now,
				Tags:      map[string]string{"host": "server1"},
			},
			setupMock: func() {
				mockService.On("TSQuery", mock.Anything, mock.Anything).
					Return(&service.TSQueryResponse{
						Points: []*service.TSDataPoint{
							{
								Metric:    "cpu.usage",
								Value:     75.5,
								Timestamp: startTime.Add(30 * time.Minute),
								Tags:      map[string]string{"host": "server1"},
							},
						},
					}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response TSQueryResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Len(t, response.Points, 1)
			},
		},
		{
			name: "missing_metric",
			requestBody: TSQueryRequest{
				StartTime: startTime,
				EndTime:   now,
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "metric is required")
			},
		},
		{
			name: "missing_time_range",
			requestBody: TSQueryRequest{
				Metric: "cpu.usage",
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "start_time is required")
			},
		},
		{
			name: "query_with_aggregation",
			requestBody: TSQueryRequest{
				Metric:      "cpu.usage",
				StartTime:   startTime,
				EndTime:     now,
				Aggregation: "avg",
				IntervalSec: 300, // 5 minutes
			},
			setupMock: func() {
				mockService.On("TSQuery", mock.Anything, mock.Anything).
					Return(&service.TSQueryResponse{
						Points: []*service.TSDataPoint{
							{
								Metric:    "cpu.usage",
								Value:     77.5,
								Timestamp: startTime,
							},
						},
					}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response TSQueryResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.NotEmpty(t, response.Points)
			},
		},
		{
			name: "missing_end_time",
			requestBody: TSQueryRequest{
				Metric:    "cpu.usage",
				StartTime: startTime,
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "end_time is required")
			},
		},
		{
			name: "end_before_start",
			requestBody: TSQueryRequest{
				Metric:    "cpu.usage",
				StartTime: now,
				EndTime:   startTime,
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "end_time must be after start_time")
			},
		},
		{
			name:           "invalid_json",
			requestBody:    "invalid",
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service_error",
			requestBody: TSQueryRequest{
				Metric:    "cpu.usage",
				StartTime: startTime,
				EndTime:   now,
			},
			setupMock: func() {
				mockService.On("TSQuery", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "TSQuery",
					Err:  stderrors.New("internal error"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "grouped_results",
			requestBody: TSQueryRequest{
				Metric:      "cpu.usage",
				StartTime:   startTime,
				EndTime:     now,
				Aggregation: "avg",
				GroupBy:     []string{"host"},
			},
			setupMock: func() {
				aggTotal := 75.5
				aggServer1 := 80.0
				aggServer2 := 70.0
				mockService.On("TSQuery", mock.Anything, mock.Anything).
					Return(&service.TSQueryResponse{
						Metric:          "cpu.usage",
						AggregatedValue: &aggTotal,
						GroupedResults: []*service.TSGroupedResult{
							{
								Tags:            map[string]string{"host": "server1"},
								AggregatedValue: &aggServer1,
								Points: []*service.TSDataPoint{
									{Metric: "cpu.usage", Value: 80.0, Timestamp: startTime},
								},
							},
							{
								Tags:            map[string]string{"host": "server2"},
								AggregatedValue: &aggServer2,
							},
						},
					}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response TSQueryResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Equal(t, "cpu.usage", response.Metric)
				require.NotNil(t, response.AggregatedValue)
				assert.Equal(t, 75.5, *response.AggregatedValue)
				require.Len(t, response.GroupedResults, 2)
				assert.Len(t, response.GroupedResults[0].Points, 1)
				assert.Empty(t, response.GroupedResults[1].Points)
				require.NotNil(t, response.GroupedResults[1].AggregatedValue)
				assert.Equal(t, 70.0, *response.GroupedResults[1].AggregatedValue)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			// Prepare request
			var reqBody []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				reqBody = []byte(str)
			} else {
				reqBody, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			// Execute handler
			server.HandleTSQuery(resp, req)

			// Check status code
			assert.Equal(t, tt.expectedStatus, resp.Code)

			// Run additional response checks
			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}
		})
	}
}

// TestHandleDeleteSeries tests deleting time series data
func TestHandleDeleteSeries(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	tests := []struct {
		name           string
		requestBody    interface{}
		setupMock      func()
		expectedStatus int
		checkResponse  func(t *testing.T, resp *httptest.ResponseRecorder)
	}{
		{
			name: "valid_delete",
			requestBody: map[string]interface{}{
				"metric": "cpu.usage",
				"tags":   map[string]string{"host": "server1"},
			},
			setupMock: func() {
				mockService.On("DeleteSeries", mock.Anything, mock.Anything).
					Return(&service.TSDeleteResponse{Success: true}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response map[string]interface{}
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.True(t, response["success"].(bool))
			},
		},
		{
			name: "missing_metric",
			requestBody: map[string]interface{}{
				"tags": map[string]string{"host": "server1"},
			},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "metric is required")
			},
		},
		{
			name:        "service_error",
			requestBody: DeleteSeriesRequest{Metric: "cpu.usage"},
			setupMock: func() {
				mockService.On("DeleteSeries", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "DeleteSeries",
					Err:  stderrors.New("internal error"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			reqBody, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/timeseries", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			server.HandleDeleteSeries(resp, req)

			assert.Equal(t, tt.expectedStatus, resp.Code)

			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}
		})
	}
}

// TestHandleGetRetentionPolicy tests getting retention policy
func TestHandleGetRetentionPolicy(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	tests := []struct {
		name           string
		metric         string
		setupMock      func()
		expectedStatus int
		checkResponse  func(t *testing.T, resp *httptest.ResponseRecorder)
	}{
		{
			name:   "valid_policy",
			metric: "cpu.usage",
			setupMock: func() {
				mockService.On("GetRetentionPolicy", mock.Anything, mock.Anything).
					Return(&service.TSGetRetentionPolicyResponse{
						Policy: &service.TSRetentionPolicy{
							Duration:    30 * 24 * time.Hour,
							Resolution:  time.Minute,
							Aggregation: "avg",
						},
					}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response GetRetentionPolicyResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Equal(t, "cpu.usage", response.Metric)
				assert.NotNil(t, response.Policy)
			},
		},
		{
			name:   "nil_policy",
			metric: "cpu.usage",
			setupMock: func() {
				mockService.On("GetRetentionPolicy", mock.Anything, mock.Anything).
					Return(&service.TSGetRetentionPolicyResponse{Policy: nil}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response GetRetentionPolicyResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Nil(t, response.Policy)
			},
		},
		{
			name:           "missing_metric",
			metric:         "",
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "metric is required")
			},
		},
		{
			name:   "service_not_found",
			metric: "cpu.usage",
			setupMock: func() {
				mockService.On("GetRetentionPolicy", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "GetRetentionPolicy",
					Err:  stderrors.New("metric not found"),
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/retention/"+tt.metric, nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("metric", tt.metric)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			resp := httptest.NewRecorder()

			server.HandleGetRetentionPolicy(resp, req)

			assert.Equal(t, tt.expectedStatus, resp.Code)

			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}
		})
	}
}

// TestHandleSetRetentionPolicy tests setting retention policy
func TestHandleSetRetentionPolicy(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	validPolicy := SetRetentionPolicyRequest{
		Policy: &TSRetentionPolicy{
			DurationSec:   2592000, // 30 days
			ResolutionSec: 60,      // 1 minute
			Aggregation:   "avg",
		},
	}

	tests := []struct {
		name           string
		metric         string
		requestBody    interface{}
		setupMock      func()
		expectedStatus int
		checkResponse  func(t *testing.T, resp *httptest.ResponseRecorder)
	}{
		{
			name:        "valid_policy",
			metric:      "cpu.usage",
			requestBody: validPolicy,
			setupMock: func() {
				mockService.On("SetRetentionPolicy", mock.Anything, mock.Anything).
					Return(&service.TSSetRetentionPolicyResponse{Success: true}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				var response SetRetentionPolicyResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.True(t, response.Success)
			},
		},
		{
			name:           "missing_metric",
			metric:         "",
			requestBody:    validPolicy,
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "metric is required")
			},
		},
		{
			name:           "missing_policy",
			metric:         "cpu.usage",
			requestBody:    SetRetentionPolicyRequest{Policy: nil},
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *httptest.ResponseRecorder) {
				assert.Contains(t, resp.Body.String(), "policy is required")
			},
		},
		{
			name:           "invalid_json",
			metric:         "cpu.usage",
			requestBody:    "invalid",
			setupMock:      func() {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "service_error",
			metric:      "cpu.usage",
			requestBody: validPolicy,
			setupMock: func() {
				mockService.On("SetRetentionPolicy", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "SetRetentionPolicy",
					Err:  stderrors.New("internal error"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			var reqBody []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				reqBody = []byte(str)
			} else {
				reqBody, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/retention/"+tt.metric, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("metric", tt.metric)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			resp := httptest.NewRecorder()

			server.HandleSetRetentionPolicy(resp, req)

			assert.Equal(t, tt.expectedStatus, resp.Code)

			if tt.checkResponse != nil {
				tt.checkResponse(t, resp)
			}
		})
	}
}

// TestHandleListMetrics tests listing available metrics
func TestHandleListMetrics(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	tests := []struct {
		name           string
		query          string
		setupMock      func()
		expectedStatus int
		expectedCount  int
	}{
		{
			name:  "all_metrics",
			query: "",
			setupMock: func() {
				mockService.On("ListMetrics", mock.Anything, mock.Anything).
					Return(&service.TSListMetricsResponse{
						Metrics: []string{"cpu.usage", "memory.used", "disk.free"},
					}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			expectedCount:  3,
		},
		{
			name:  "with_prefix",
			query: "?prefix=cpu",
			setupMock: func() {
				mockService.On("ListMetrics", mock.Anything, mock.MatchedBy(func(req *service.TSListMetricsRequest) bool {
					return req.Prefix == "cpu"
				})).Return(&service.TSListMetricsResponse{
					Metrics: []string{"cpu.usage", "cpu.system"},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			expectedCount:  2,
		},
		{
			name:  "with_tags",
			query: `?tags={"host":"server1"}`,
			setupMock: func() {
				mockService.On("ListMetrics", mock.Anything, mock.MatchedBy(func(req *service.TSListMetricsRequest) bool {
					return req.Tags["host"] == "server1"
				})).Return(&service.TSListMetricsResponse{
					Metrics: []string{"cpu.usage"},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			// An unparseable tags parameter is ignored, not rejected.
			name:  "invalid_tags_ignored",
			query: "?tags=invalid-json",
			setupMock: func() {
				mockService.On("ListMetrics", mock.Anything, mock.MatchedBy(func(req *service.TSListMetricsRequest) bool {
					return req.Tags == nil
				})).Return(&service.TSListMetricsResponse{
					Metrics: []string{"cpu.usage"},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			expectedCount:  1,
		},
		{
			name:  "service_error",
			query: "",
			setupMock: func() {
				mockService.On("ListMetrics", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "ListMetrics",
					Err:  stderrors.New("internal error"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics"+tt.query, nil)
			resp := httptest.NewRecorder()

			server.HandleListMetrics(resp, req)

			assert.Equal(t, tt.expectedStatus, resp.Code)

			if tt.expectedStatus == http.StatusOK {
				var response ListMetricsResponse
				err := json.Unmarshal(resp.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Len(t, response.Metrics, tt.expectedCount)
			}
		})
	}
}

func TestConvertTSQueryResponseEmptyPoints(t *testing.T) {
	resp := &service.TSQueryResponse{
		Metric:         "cpu.usage",
		Points:         []*service.TSDataPoint{},
		GroupedResults: []*service.TSGroupedResult{},
	}

	restResp := convertTSQueryResponse(resp)

	assert.Equal(t, "cpu.usage", restResp.Metric)
	assert.Nil(t, restResp.Points)
	assert.Nil(t, restResp.GroupedResults)
}
