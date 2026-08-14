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
	"strings"
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
				assert.Contains(t, resp.Body.String(), "metric is required")
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

	thirtyDays := 30 * 24 * time.Hour
	mockService.On("GetRetentionPolicy", mock.Anything, mock.Anything).
		Return(&service.TSGetRetentionPolicyResponse{
			Metric: "cpu.usage",
			Policy: &service.TSRetentionPolicy{
				Duration: thirtyDays,
			},
		}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/retention/cpu.usage", nil)
	resp := httptest.NewRecorder()

	// Setup chi context with URL params
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetRetentionPolicy(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "cpu.usage", response["metric"])
}

// TestHandleSetRetentionPolicy tests setting retention policy
func TestHandleSetRetentionPolicy(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	mockService.On("SetRetentionPolicy", mock.Anything, mock.Anything).
		Return(&service.TSSetRetentionPolicyResponse{Success: true}, nil).Once()

	reqBody := SetRetentionPolicyRequest{
		Policy: &TSRetentionPolicy{
			DurationSec: 86400 * 7, // 7 days
		},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/retention/cpu.usage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	// Setup chi context with URL params
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetRetentionPolicy(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.True(t, response["success"].(bool))
}

// TestHandleListMetrics tests listing available metrics
func TestHandleListMetrics(t *testing.T) {
	server, mockService := setupTimeSeriesTestServer(t)

	mockService.On("ListMetrics", mock.Anything, mock.Anything).
		Return(&service.TSListMetricsResponse{
			Metrics: []string{"cpu.usage", "memory.used", "disk.free"},
		}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics", nil)
	resp := httptest.NewRecorder()

	server.HandleListMetrics(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	var response map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &response)
	require.NoError(t, err)
	metrics := response["metrics"].([]interface{})
	assert.Len(t, metrics, 3)
}

func TestCov91_HandleWritePoint_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.WritePointFunc = func(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("write error")}
	}

	body := `{"metric":"cpu.usage","value":75.5,"timestamp":"2024-01-01T00:00:00Z","tags":{"host":"server1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleWriteBatch_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.WriteBatchFunc = func(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("batch error")}
	}

	body := `{"points":[{"metric":"cpu.usage","value":75.5,"timestamp":"2024-01-01T00:00:00Z","tags":{"host":"server1"}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleTSQuery_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.TSQueryFunc = func(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("query error")}
	}

	body := `{"metric":"cpu.usage","start_time":"2024-01-01T00:00:00Z","end_time":"2024-01-02T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleDeleteSeries_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.DeleteSeriesFunc = func(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("delete error")}
	}

	body := `{"metric":"cpu.usage"}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/timeseries/series/cpu.usage", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleDeleteSeries(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleGetRetentionPolicy_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetRetentionPolicyFunc = func(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("retention error")}
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/series/cpu.usage/retention", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleSetRetentionPolicy_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.SetRetentionPolicyFunc = func(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("set retention error")}
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	body := `{"policy":{"duration_sec":86400,"resolution_sec":60,"aggregation":"avg"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/series/cpu.usage/retention", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleListMetrics_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ListMetricsFunc = func(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("list metrics error")}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics", nil)
	w := httptest.NewRecorder()
	server.HandleListMetrics(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCov91_HandleWritePoint_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.WritePointFunc = func(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
		return &service.WritePointResponse{Success: true}, nil
	}

	body := `{"metric":"cpu.usage","value":75.5,"timestamp":"2024-01-01T00:00:00Z","tags":{"host":"server1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleWriteBatch_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.WriteBatchFunc = func(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
		return &service.WriteBatchResponse{Success: true, PointsWritten: len(req.Points)}, nil
	}

	body := `{"points":[{"metric":"cpu.usage","value":75.5,"timestamp":"2024-01-01T00:00:00Z","tags":{"host":"server1"}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleTSQuery_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.TSQueryFunc = func(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
		return &service.TSQueryResponse{
			Metric: req.Metric,
			Points: []*service.TSDataPoint{
				{Metric: "cpu.usage", Value: 75.5, Timestamp: time.Now(), Tags: map[string]string{"host": "server1"}},
			},
		}, nil
	}

	body := `{"metric":"cpu.usage","start_time":"2024-01-01T00:00:00Z","end_time":"2024-01-02T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleDeleteSeries_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.DeleteSeriesFunc = func(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
		return &service.TSDeleteResponse{Success: true}, nil
	}

	body := `{"metric":"cpu.usage"}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/timeseries/series/cpu.usage", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleDeleteSeries(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleGetRetentionPolicy_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.GetRetentionPolicyFunc = func(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
		return &service.TSGetRetentionPolicyResponse{
			Policy: &service.TSRetentionPolicy{
				Duration:    24 * time.Hour,
				Resolution:  time.Minute,
				Aggregation: "avg",
			},
		}, nil
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/series/cpu.usage/retention", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleSetRetentionPolicy_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.SetRetentionPolicyFunc = func(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
		return &service.TSSetRetentionPolicyResponse{Success: true}, nil
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	body := `{"policy":{"duration_sec":86400,"resolution_sec":60,"aggregation":"avg"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/series/cpu.usage/retention", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleListMetrics_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.ListMetricsFunc = func(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
		return &service.TSListMetricsResponse{
			Metrics: []string{"cpu.usage", "mem.usage", "disk.io"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics", nil)
	w := httptest.NewRecorder()
	server.HandleListMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCov91_HandleWritePoint_MissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"value":75.5,"timestamp":"2024-01-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleWritePoint_MissingTimestamp(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"metric":"cpu.usage","value":75.5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleWritePoint_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleWriteBatch_EmptyPoints(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"points":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleWriteBatch_MissingMetricInPoint(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"points":[{"value":75.5,"timestamp":"2024-01-01T00:00:00Z"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleWriteBatch_MissingTimestampInPoint(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"points":[{"metric":"cpu.usage","value":75.5}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write/batch", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleTSQuery_MissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"start_time":"2024-01-01T00:00:00Z","end_time":"2024-01-02T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleTSQuery_MissingStartTime(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"metric":"cpu.usage","end_time":"2024-01-02T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleTSQuery_MissingEndTime(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"metric":"cpu.usage","start_time":"2024-01-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleTSQuery_EndBeforeStart(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{"metric":"cpu.usage","start_time":"2024-01-02T00:00:00Z","end_time":"2024-01-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleDeleteSeries_MissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	body := `{}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/timeseries/series/cpu.usage", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleDeleteSeries(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleGetRetentionPolicy_MissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/series//retention", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleSetRetentionPolicy_MissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "")
	body := `{"policy":{"duration_sec":86400}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/series//retention", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleSetRetentionPolicy_MissingPolicy(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/series/cpu.usage/retention", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleSetRetentionPolicy_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/series/cpu.usage/retention", strings.NewReader("not-json"))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCov91_HandleTSQuery_WithGroupedResults(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()

	mockService.TSQueryFunc = func(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
		aggTotal := 75.5
		aggServer1 := 80.0
		aggServer2 := 70.0
		return &service.TSQueryResponse{
			Metric:          req.Metric,
			AggregatedValue: &aggTotal,
			GroupedResults: []*service.TSGroupedResult{
				{
					Tags:            map[string]string{"host": "server1"},
					AggregatedValue: &aggServer1,
					Points: []*service.TSDataPoint{
						{Metric: "cpu.usage", Value: 80.0, Timestamp: time.Now()},
					},
				},
				{
					Tags:            map[string]string{"host": "server2"},
					AggregatedValue: &aggServer2,
				},
			},
		}, nil
	}

	body := `{"metric":"cpu.usage","start_time":"2024-01-01T00:00:00Z","end_time":"2024-01-02T00:00:00Z","aggregation":"avg","group_by":["host"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", strings.NewReader(body))
	w := httptest.NewRecorder()
	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

type MockTimeSeriesService struct {
	mock.Mock
}

func (m *MockTimeSeriesService) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.WritePointResponse), args.Error(1)
}

func (m *MockTimeSeriesService) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.WriteBatchResponse), args.Error(1)
}

func (m *MockTimeSeriesService) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TSQueryResponse), args.Error(1)
}

func (m *MockTimeSeriesService) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TSDeleteResponse), args.Error(1)
}

func (m *MockTimeSeriesService) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TSGetRetentionPolicyResponse), args.Error(1)
}

func (m *MockTimeSeriesService) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TSSetRetentionPolicyResponse), args.Error(1)
}

func (m *MockTimeSeriesService) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TSListMetricsResponse), args.Error(1)
}

func TestHandleWritePointSuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Setup mock for WritePoint
	mockService.On("WritePoint", mock.Anything, mock.Anything).Return(&service.WritePointResponse{
		Success: true,
	}, nil).Once()

	ts := time.Now()
	writeReq := WritePointRequest{
		Metric:    "cpu.usage",
		Tags:      map[string]string{"host": "server1"},
		Value:     75.5,
		Timestamp: ts,
	}

	body, err := json.Marshal(writeReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp WritePointResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestHandleWritePointMissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	ts := time.Now()
	writeReq := WritePointRequest{
		Metric:    "",
		Tags:      map[string]string{"host": "server1"},
		Value:     75.5,
		Timestamp: ts,
	}

	body, err := json.Marshal(writeReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "metric is required")
}

func TestHandleWritePointMissingTimestamp(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	writeReq := WritePointRequest{
		Metric: "cpu.usage",
		Tags:   map[string]string{"host": "server1"},
		Value:  75.5,
		// Timestamp is zero value
	}

	body, err := json.Marshal(writeReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "timestamp is required")
}

func TestHandleWritePointInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON")
}

func TestHandleWritePointServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("WritePoint", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "WritePoint",
		Err:  stderrors.New("internal error"),
	}).Once()

	ts := time.Now()
	writeReq := WritePointRequest{
		Metric:    "cpu.usage",
		Value:     75.5,
		Timestamp: ts,
	}

	body, err := json.Marshal(writeReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWritePoint(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleWriteBatchSuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("WriteBatch", mock.Anything, mock.Anything).Return(&service.WriteBatchResponse{
		Success:       true,
		PointsWritten: 3,
	}, nil).Once()

	ts := time.Now()
	batchReq := WriteBatchRequest{
		Points: []WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5, Timestamp: ts},
			{Metric: "memory.usage", Value: 60.2, Timestamp: ts},
			{Metric: "disk.usage", Value: 45.0, Timestamp: ts},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp WriteBatchResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 3, resp.PointsWritten)
}

func TestHandleWriteBatchEmptyPoints(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	batchReq := WriteBatchRequest{
		Points: []WritePointRequest{},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "no points provided")
}

func TestHandleWriteBatchMissingMetricInPoint(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	ts := time.Now()
	batchReq := WriteBatchRequest{
		Points: []WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5, Timestamp: ts},
			{Metric: "", Value: 60.2, Timestamp: ts}, // Missing metric
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "metric is required for point 1")
}

func TestHandleWriteBatchMissingTimestampInPoint(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	ts := time.Now()
	batchReq := WriteBatchRequest{
		Points: []WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5, Timestamp: ts},
			{Metric: "memory.usage", Value: 60.2}, // Missing timestamp
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "timestamp is required for point 1")
}

func TestHandleWriteBatchInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/batch", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleWriteBatchServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("WriteBatch", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "WriteBatch",
		Err:  stderrors.New("internal error"),
	}).Once()

	ts := time.Now()
	batchReq := WriteBatchRequest{
		Points: []WritePointRequest{
			{Metric: "cpu.usage", Value: 75.5, Timestamp: ts},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleWriteBatch(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleTSQuerySuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	mockService.On("TSQuery", mock.Anything, mock.Anything).Return(&service.TSQueryResponse{
		Metric: "cpu.usage",
		Points: []*service.TSDataPoint{
			{Metric: "cpu.usage", Value: 75.5, Timestamp: now},
			{Metric: "cpu.usage", Value: 80.2, Timestamp: now.Add(time.Minute)},
		},
	}, nil).Once()

	start := now.Add(-time.Hour)
	end := now
	queryReq := TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: start,
		EndTime:   end,
	}

	body, err := json.Marshal(queryReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleTSQueryMissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	queryReq := TSQueryRequest{
		Metric:    "",
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
	}

	body, err := json.Marshal(queryReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "metric is required")
}

func TestHandleTSQueryMissingStartTime(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	queryReq := TSQueryRequest{
		Metric:  "cpu.usage",
		EndTime: now,
	}

	body, err := json.Marshal(queryReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "start_time is required")
}

func TestHandleTSQueryMissingEndTime(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	queryReq := TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: now.Add(-time.Hour),
	}

	body, err := json.Marshal(queryReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "end_time is required")
}

func TestHandleTSQueryEndBeforeStart(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	queryReq := TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: now,
		EndTime:   now.Add(-time.Hour), // End before start
	}

	body, err := json.Marshal(queryReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "end_time must be after start_time")
}

func TestHandleTSQueryInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleTSQueryServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("TSQuery", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "TSQuery",
		Err:  stderrors.New("internal error"),
	}).Once()

	now := time.Now()
	queryReq := TSQueryRequest{
		Metric:    "cpu.usage",
		StartTime: now.Add(-time.Hour),
		EndTime:   now,
	}

	body, err := json.Marshal(queryReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleTSQuery(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleDeleteSeriesSuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("DeleteSeries", mock.Anything, mock.Anything).Return(&service.TSDeleteResponse{
		Success: true,
	}, nil).Once()

	deleteReq := DeleteSeriesRequest{
		Metric: "cpu.usage",
	}

	body, err := json.Marshal(deleteReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleDeleteSeries(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DeleteSeriesResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestHandleDeleteSeriesMissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	deleteReq := DeleteSeriesRequest{
		Metric: "",
	}

	body, err := json.Marshal(deleteReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleDeleteSeries(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "metric is required")
}

func TestHandleDeleteSeriesServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("DeleteSeries", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "DeleteSeries",
		Err:  stderrors.New("internal error"),
	}).Once()

	deleteReq := DeleteSeriesRequest{
		Metric: "cpu.usage",
	}

	body, err := json.Marshal(deleteReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/timeseries/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleDeleteSeries(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetRetentionPolicySuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetRetentionPolicy", mock.Anything, mock.Anything).Return(&service.TSGetRetentionPolicyResponse{
		Policy: &service.TSRetentionPolicy{
			Duration:    30 * 24 * time.Hour,
			Resolution:  time.Minute,
			Aggregation: "avg",
		},
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/retention/cpu.usage", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GetRetentionPolicyResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "cpu.usage", resp.Metric)
	assert.NotNil(t, resp.Policy)
}

func TestHandleGetRetentionPolicyMissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/retention/", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "metric is required")
}

func TestHandleGetRetentionPolicyServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetRetentionPolicy", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "GetRetentionPolicy",
		Err:  stderrors.New("metric not found"),
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/retention/cpu.usage", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleGetRetentionPolicyNilPolicy(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetRetentionPolicy", mock.Anything, mock.Anything).Return(&service.TSGetRetentionPolicyResponse{
		Policy: nil, // No policy set
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/retention/cpu.usage", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GetRetentionPolicyResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Policy)
}

func TestHandleSetRetentionPolicySuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SetRetentionPolicy", mock.Anything, mock.Anything).Return(&service.TSSetRetentionPolicyResponse{
		Success: true,
	}, nil).Once()

	setReq := SetRetentionPolicyRequest{
		Policy: &TSRetentionPolicy{
			DurationSec:   2592000, // 30 days
			ResolutionSec: 60,      // 1 minute
			Aggregation:   "avg",
		},
	}

	body, err := json.Marshal(setReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/retention/cpu.usage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SetRetentionPolicyResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestHandleSetRetentionPolicyMissingMetric(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	setReq := SetRetentionPolicyRequest{
		Policy: &TSRetentionPolicy{DurationSec: 2592000},
	}

	body, err := json.Marshal(setReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/retention/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "metric is required")
}

func TestHandleSetRetentionPolicyMissingPolicy(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	setReq := SetRetentionPolicyRequest{
		Policy: nil,
	}

	body, err := json.Marshal(setReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/retention/cpu.usage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "policy is required")
}

func TestHandleSetRetentionPolicyInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/retention/cpu.usage", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSetRetentionPolicyServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SetRetentionPolicy", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "SetRetentionPolicy",
		Err:  stderrors.New("internal error"),
	}).Once()

	setReq := SetRetentionPolicyRequest{
		Policy: &TSRetentionPolicy{DurationSec: 2592000},
	}

	body, err := json.Marshal(setReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/timeseries/retention/cpu.usage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("metric", "cpu.usage")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSetRetentionPolicy(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleListMetricsSuccess(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ListMetrics", mock.Anything, mock.Anything).Return(&service.TSListMetricsResponse{
		Metrics:    []string{"cpu.usage", "memory.usage", "disk.usage"},
		NextCursor: "",
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics", nil)
	w := httptest.NewRecorder()

	server.HandleListMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListMetricsResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Metrics, 3)
}

func TestHandleListMetricsWithPrefix(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ListMetrics", mock.Anything, mock.MatchedBy(func(req *service.TSListMetricsRequest) bool {
		return req.Prefix == "cpu"
	})).Return(&service.TSListMetricsResponse{
		Metrics: []string{"cpu.usage", "cpu.system", "cpu.user"},
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics?prefix=cpu", nil)
	w := httptest.NewRecorder()

	server.HandleListMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListMetricsResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Metrics, 3)
}

func TestHandleListMetricsWithTags(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ListMetrics", mock.Anything, mock.Anything).Return(&service.TSListMetricsResponse{
		Metrics: []string{"cpu.usage"},
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics?tags={\"host\":\"server1\"}", nil)
	w := httptest.NewRecorder()

	server.HandleListMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleListMetricsWithInvalidTags(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Invalid tags should be ignored, still works
	mockService.On("ListMetrics", mock.Anything, mock.Anything).Return(&service.TSListMetricsResponse{
		Metrics: []string{"cpu.usage"},
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics?tags=invalid-json", nil)
	w := httptest.NewRecorder()

	server.HandleListMetrics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleListMetricsServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ListMetrics", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "ListMetrics",
		Err:  stderrors.New("internal error"),
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeseries/metrics", nil)
	w := httptest.NewRecorder()

	server.HandleListMetrics(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestConvertTSQueryResponseWithGroupedResults(t *testing.T) {
	now := time.Now()
	aggVal := 80.0
	totalAggVal := 77.75
	resp := &service.TSQueryResponse{
		Metric: "cpu.usage",
		Tags:   map[string]string{"host": "server1"},
		Points: []*service.TSDataPoint{
			{Metric: "cpu.usage", Value: 75.5, Timestamp: now},
		},
		GroupedResults: []*service.TSGroupedResult{
			{
				Tags:            map[string]string{"region": "us-east"},
				AggregatedValue: &aggVal,
				Points: []*service.TSDataPoint{
					{Metric: "cpu.usage", Value: 80.0, Timestamp: now},
				},
			},
		},
		AggregatedValue: &totalAggVal,
	}

	restResp := convertTSQueryResponse(resp)

	assert.Equal(t, "cpu.usage", restResp.Metric)
	assert.Len(t, restResp.Points, 1)
	assert.Len(t, restResp.GroupedResults, 1)
	assert.Len(t, restResp.GroupedResults[0].Points, 1)
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
