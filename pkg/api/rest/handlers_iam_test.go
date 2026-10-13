//go:build rest

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestHandleBatchGetSuccessBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("BatchGet", mock.Anything, mock.Anything).Return(&service.BatchGetResponse{
		Responses: []*service.GetResponse{
			{ID: 1, Found: true, Data: []byte(`{"name":"Alice"}`), Timestamp: 12345},
			{ID: 2, Found: false},
		},
	}, nil).Once()

	batchReq := BatchGetRequest{
		Operations: []BatchGetOperation{
			{EntityType: "user", ID: 1},
			{EntityType: "user", ID: 2},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleBatchGet(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BatchGetResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Len(t, resp.Responses, 2)
	assert.True(t, resp.Responses[0].Found)
	assert.False(t, resp.Responses[1].Found)
}

func TestHandleBatchGetEmptyOperationsBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	batchReq := BatchGetRequest{
		Operations: []BatchGetOperation{},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleBatchGet(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "batch operations cannot be empty")
}

func TestHandleBatchGetMissingEntityTypeBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	batchReq := BatchGetRequest{
		Operations: []BatchGetOperation{
			{EntityType: "", ID: 1},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleBatchGet(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "entity_type is required")
}

func TestHandleBatchGetMissingIDBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	batchReq := BatchGetRequest{
		Operations: []BatchGetOperation{
			{EntityType: "user", ID: 0},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleBatchGet(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "id is required")
}

func TestHandleBatchGetInvalidJSONBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleBatchGet(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON body")
}

func TestHandleBatchGetInvalidConsistencyLevelBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	batchReq := BatchGetRequest{
		Operations: []BatchGetOperation{
			{EntityType: "user", ID: 1},
		},
		ConsistencyLevel: "invalid-level",
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleBatchGet(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleBatchGetServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("BatchGet", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "BatchGet",
		Err:  stderrors.New("internal error"),
	}).Once()

	batchReq := BatchGetRequest{
		Operations: []BatchGetOperation{
			{EntityType: "user", ID: 1},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleBatchGet(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleHealthUnhealthyBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
		Status: service.HealthStatusUnhealthy,
		Details: map[string]*service.ComponentHealth{
			"raft": {Status: service.HealthStatusUnhealthy, Message: "not ready"},
		},
		UptimeSeconds: 100,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	server.HandleHealth(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp HealthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "unhealthy", resp.Status)
}

func TestHandleHealthServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Health", mock.Anything, mock.Anything).Return(nil, stderrors.New("health check failed")).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	server.HandleHealth(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleLivenessNotAliveBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Liveness", mock.Anything, mock.Anything).Return(&service.LivenessResponse{
		Alive: false,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
	w := httptest.NewRecorder()

	server.HandleLiveness(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleLivenessErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Liveness", mock.Anything, mock.Anything).Return(nil, stderrors.New("liveness error")).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
	w := httptest.NewRecorder()

	server.HandleLiveness(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleReadinessNotReadyBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Readiness", mock.Anything, mock.Anything).Return(&service.ReadinessResponse{
		Ready:  false,
		Reason: "cluster not ready",
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
	w := httptest.NewRecorder()

	server.HandleReadiness(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp HealthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "unhealthy", resp.Status)
}

func TestHandleReadinessErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Readiness", mock.Anything, mock.Anything).Return(nil, stderrors.New("readiness error")).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
	w := httptest.NewRecorder()

	server.HandleReadiness(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleClusterStatusNilResponseBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	w := httptest.NewRecorder()

	server.HandleClusterStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "cluster status unavailable")
}

func TestHandleClusterStatusServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "ClusterStatus",
		Err:  stderrors.New("internal error"),
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil)
	w := httptest.NewRecorder()

	server.HandleClusterStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleClusterShardsSuccessBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
		ClusterID: "test-cluster",
		Nodes: []*service.NodeInfo{
			{NodeID: 1, Address: "127.0.0.1:9999", IsLeader: true, HealthStatus: service.HealthStatusHealthy},
		},
		Shards: []*service.ShardInfo{
			{ShardID: 1, LeaderID: 1, Replicas: []uint64{1, 2, 3}, Status: service.HealthStatusHealthy},
			{ShardID: 2, LeaderID: 2, Replicas: []uint64{2, 3, 1}, Status: service.HealthStatusHealthy},
		},
		Timestamp: 12345,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards", nil)
	w := httptest.NewRecorder()

	server.HandleClusterShards(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleClusterShardsNilResponseBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/shards", nil)
	w := httptest.NewRecorder()

	server.HandleClusterShards(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetLeaderNilResponseBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetLeader", mock.Anything, mock.Anything).Return(nil, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/leader/1", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("shardID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetLeader(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "leader information unavailable")
}

func TestHandleGetLeaderServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetLeader", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "GetLeader",
		Err:  stderrors.New("shard not found"),
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/leader/1", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("shardID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetLeader(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandlePutServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Put", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrLeaderNotAvailable,
		Op:   "Put",
		Err:  stderrors.New("leader not available"),
	}).Once()

	putReq := PutRequest{
		EntityType: "users",
		ID:         1,
		Data:       json.RawMessage(`{"name":"test"}`),
	}
	body, err := json.Marshal(putReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/entities/users/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandlePut(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleDeleteServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Delete", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrTimeout,
		Op:   "Delete",
		Err:  stderrors.New("operation timed out"),
	}).Once()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/entities/users/1", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "users")
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDelete(w, req)

	assert.Equal(t, http.StatusRequestTimeout, w.Code)
}

func TestHandleListServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			close(stream)
		}).Return(nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	// Stream closed without error returns OK with empty entities
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleListWithEntitiesBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			stream <- &service.ListResponse{
				Entities: []*service.Entity{
					{ID: 1, EntityType: "users", Data: []byte(`{"name":"Alice"}`)},
					{ID: 2, EntityType: "users", Data: []byte(`{"name":"Bob"}`)},
				},
				NextPageToken: "next-page",
				TotalCount:    10,
			}
			close(stream)
		}).Return(nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Entities, 2)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "next-page", resp.NextCursor)
}

func TestHandleQueryServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Query", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.QueryResponse)
			close(stream)
		}).Return(&errors.QBError{
		Code: errors.ErrInternal,
		Op:   "Query",
		Err:  stderrors.New("internal error"),
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/query/users/email?value=test@example.com", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	// The stream closing first must not hide the error Query returned.
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleQueryWithResultsBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Query", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.QueryResponse)
			stream <- &service.QueryResponse{
				Entities: []*service.Entity{
					{ID: 1, EntityType: "users", Data: []byte(`{"name":"Alice"}`)},
				},
			}
			close(stream)
		}).Return(nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/query/users/email?value=test@example.com", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp QueryResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 1)
	assert.Equal(t, "email", resp.Index)
}

func TestHandleRangeQueryServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ScanResponse)
			close(stream)
		}).Return(&errors.QBError{
		Code: errors.ErrInternal,
		Op:   "ScanIndex",
		Err:  stderrors.New("internal error"),
	}).Once()

	rangeReq := RangeQueryRequest{
		EntityType: "users",
		Index:      "age",
		Start:      20,
		End:        30,
	}
	body, err := json.Marshal(rangeReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/range", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	// The stream closing first must not hide the error ScanIndex returned.
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleRangeQueryWithMultipleResultsBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ScanResponse)
			// Send multiple batches
			stream <- &service.ScanResponse{
				Entities: []*service.Entity{
					{ID: 1, EntityType: "users", Data: []byte(`{"age":25}`)},
				},
				TotalCount: 2,
			}
			stream <- &service.ScanResponse{
				Entities: []*service.Entity{
					{ID: 2, EntityType: "users", Data: []byte(`{"age":28}`)},
				},
				TotalCount: 2,
			}
			close(stream)
		}).Return(nil).Once()

	rangeReq := RangeQueryRequest{
		EntityType: "users",
		Index:      "age",
		Start:      20,
		End:        30,
	}
	body, err := json.Marshal(rangeReq)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/range", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp QueryResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Results, 2)
}

func TestHandleServiceErrorInvalidInputBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "test",
		Err:  stderrors.New("invalid input"),
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleServiceErrorValidationBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrValidation,
		Op:   "test",
		Err:  stderrors.New("validation failed"),
	})

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleServiceErrorForbiddenBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrForbidden,
		Op:   "test",
		Err:  stderrors.New("access denied"),
	})

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleServiceErrorNotAllowedBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrNotAllowed,
		Op:   "test",
		Err:  stderrors.New("operation not allowed"),
	})

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleServiceErrorUnauthorizedBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrUnauthorized,
		Op:   "test",
		Err:  stderrors.New("unauthorized"),
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleServiceErrorConflictBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrConflict,
		Op:   "test",
		Err:  stderrors.New("conflict"),
	})

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleServiceErrorAlreadyExistsBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrAlreadyExists,
		Op:   "test",
		Err:  stderrors.New("already exists"),
	})

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleServiceErrorUnavailableBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrUnavailable,
		Op:   "test",
		Err:  stderrors.New("service unavailable"),
	})

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleServiceErrorStorageBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrStorage,
		Op:   "test",
		Err:  stderrors.New("storage error"),
	})

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleServiceErrorUnknownBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrUnknown,
		Op:   "test",
		Err:  stderrors.New("unknown error"),
	})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleServiceErrorDeadlineExceededBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrDeadlineExceeded,
		Op:   "test",
		Err:  stderrors.New("deadline exceeded"),
	})

	assert.Equal(t, http.StatusRequestTimeout, w.Code)
}

func TestHandleServiceErrorNilErrFieldBoost(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	server.handleServiceError(w, req, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "test",
		Err:  nil, // Test nil Err field
	})

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestConvertToServiceConsistencyEventualBoost(t *testing.T) {
	result := convertToServiceConsistency(ConsistencyEventual)
	assert.Equal(t, service.ConsistencyLevelEventual, result)
}

func TestConvertToServiceConsistencyStrongBoost(t *testing.T) {
	result := convertToServiceConsistency(ConsistencyStrong)
	assert.Equal(t, service.ConsistencyLevelStrong, result)
}

func TestConvertToServiceConsistencyDefaultBoost(t *testing.T) {
	result := convertToServiceConsistency("")
	assert.Equal(t, service.ConsistencyLevelLinearizable, result)
}

func TestConvertToServiceConsistencyUnknownBoost(t *testing.T) {
	result := convertToServiceConsistency(ConsistencyLevel("unknown"))
	assert.Equal(t, service.ConsistencyLevelLinearizable, result)
}

func TestConvertWriteModeSyncBoost(t *testing.T) {
	result := convertWriteMode("sync")
	assert.Equal(t, service.WriteModeSync, result)
}

func TestConvertWriteModeDefaultBoost(t *testing.T) {
	result := convertWriteMode("unknown")
	assert.Equal(t, service.WriteModeSync, result)
}

func TestConvertHealthStatusDegradedBoost(t *testing.T) {
	result := convertHealthStatus(service.HealthStatusDegraded)
	assert.Equal(t, "degraded", result)
}

func TestConvertHealthStatusUnknownBoost(t *testing.T) {
	result := convertHealthStatus(service.HealthStatus(999))
	assert.Equal(t, "unknown", result)
}

func TestConvertHealthStatusToStringDegradedBoost(t *testing.T) {
	result := convertHealthStatusToString(service.HealthStatusDegraded)
	assert.Equal(t, "degraded", result)
}

func TestConvertHealthStatusToStringUnknownBoost(t *testing.T) {
	result := convertHealthStatusToString(service.HealthStatus(999))
	assert.Equal(t, "unknown", result)
}

func TestInterfaceToBytesNilBoost(t *testing.T) {
	result := interfaceToBytes(nil)
	assert.Nil(t, result)
}

func TestInterfaceToBytesStringBoost(t *testing.T) {
	result := interfaceToBytes("test")
	assert.Equal(t, []byte("test"), result)
}

func TestInterfaceToBytesBytesBoost(t *testing.T) {
	input := []byte("test")
	result := interfaceToBytes(input)
	assert.Equal(t, input, result)
}

func TestInterfaceToBytesIntBoost(t *testing.T) {
	result := interfaceToBytes(123)
	assert.Equal(t, []byte("123"), result)
}

func TestHandleImportKeyKMSManagerNilBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("KMSManager").Return(nil)

	body := `{"key_material":"dGVzdA==","wrapping_key_id":"wrap-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleImportKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleImportKeyWrongReturnTypeBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("ImportKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("wrong-type", nil).Once()
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{"key_material":"dGVzdA==","wrapping_key_id":"wrap-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleImportKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "unexpected response type")
}

func TestHandleListKeysKMSManagerNilBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys", nil)
	w := httptest.NewRecorder()

	server.HandleListKeys(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleListKeysServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("ListKeys", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "ListKeys",
		Err:  stderrors.New("internal error"),
	}).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys", nil)
	w := httptest.NewRecorder()

	server.HandleListKeys(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleListKeysWithPaginationBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Create more keys than the limit
	keys := make([]interface{}, 150)
	for i := 0; i < 150; i++ {
		keys[i] = &kms.KeyMetadata{
			KeyID:     fmt.Sprintf("key-%d", i),
			Algorithm: "AES-256-GCM",
			Version:   1,
			State:     kms.KeyStateEnabled,
			Created:   time.Now(),
			Updated:   time.Now(),
		}
	}

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("ListKeys", mock.Anything, "").Return(keys, nil).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys?limit=100", nil)
	w := httptest.NewRecorder()

	server.HandleListKeys(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ListKeysRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Keys, 100)
	assert.True(t, resp.HasMore)
	assert.Equal(t, 150, resp.TotalCount)
}

func TestHandleDeleteKeyDisableErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("DisableKey", mock.Anything, "test-key").Return(&errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "DisableKey",
		Err:  stderrors.New("key not found"),
	}).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/test-key", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteKey(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleUpdateKeyStatusEnableErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("EnableKey", mock.Anything, "test-key").Return(&errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "EnableKey",
		Err:  stderrors.New("key not found"),
	}).Once()
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{"status":"enabled"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleUpdateKeyStatusDisableErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("DisableKey", mock.Anything, "test-key").Return(&errors.QBError{
		Code: errors.ErrInternal,
		Op:   "DisableKey",
		Err:  stderrors.New("internal error"),
	}).Once()
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{"status":"disabled"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleKMSEncryptServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Mock Encrypt to return an error
	mockService.On("Encrypt", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "Encrypt",
		Err:  stderrors.New("key not found"),
	}).Once()

	body := `{"plaintext":"dGVzdA=="}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/encrypt/test-key", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleKMSEncrypt(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleKMSDecryptServiceErrorBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Mock Decrypt to return an error
	mockService.On("Decrypt", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "Decrypt",
		Err:  stderrors.New("invalid ciphertext"),
	}).Once()

	body := `{"ciphertext":"dGVzdA=="}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/decrypt/test-key", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleKMSDecrypt(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleBackendStatusKMSManagerNilBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("KMSManager").Return(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/backend/status", nil)
	w := httptest.NewRecorder()

	server.HandleBackendStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleBackendStatusUnavailableBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GetBackendType").Return(config.BackendTypeSoftware)
	mockKMSMgr.On("IsAvailable", mock.Anything).Return(false).Once()
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/backend/status", nil)
	w := httptest.NewRecorder()

	server.HandleBackendStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BackendStatusRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "unavailable", resp.Status)
}

func TestHandleBackendTestKMSManagerNilBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("KMSManager").Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/backend/test", nil)
	w := httptest.NewRecorder()

	server.HandleBackendTest(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleBackendTestUnavailableBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("IsAvailable", mock.Anything).Return(false).Once()
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/backend/test", nil)
	w := httptest.NewRecorder()

	server.HandleBackendTest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BackendTestRESTResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "not available")
}

func TestRequestTimeoutErrorBoost(t *testing.T) {
	err := &RequestTimeoutError{Timeout: 30 * time.Second}
	assert.Contains(t, err.Error(), "30")
}

func TestForbiddenErrorBoost(t *testing.T) {
	err := &ForbiddenError{Reason: "access denied"}
	assert.Contains(t, err.Error(), "access denied")
}

func TestUnauthorizedErrorBoost(t *testing.T) {
	err := &UnauthorizedError{Message: "invalid token"}
	assert.Contains(t, err.Error(), "invalid token")
}

func TestConflictErrorBoost(t *testing.T) {
	err := &ConflictError{Message: "resource already exists"}
	assert.Contains(t, err.Error(), "already exists")
}

func TestInternalServerErrorBoost(t *testing.T) {
	err := &InternalServerError{Message: "internal error"}
	assert.Contains(t, err.Error(), "internal error")
}

func TestKmsMetadataToResponseWithLastUsedAtBoost(t *testing.T) {
	lastUsed := time.Now()
	metadata := &kms.KeyMetadata{
		KeyID:              "test-key",
		Name:               "Test Key",
		Description:        "A test key",
		Algorithm:          "AES-256-GCM",
		State:              kms.KeyStateEnabled,
		Usage:              []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
		Version:            2,
		Exportable:         true,
		DeletionProtection: true,
		Created:            time.Now(),
		Updated:            time.Now(),
		LastUsedAt:         &lastUsed,
		Tags:               map[string]string{"env": "test"},
		Owner:              "test-owner",
	}

	resp := kmsMetadataToResponseFull(metadata)

	assert.Equal(t, "test-key", resp.KeyID)
	assert.Equal(t, "Test Key", resp.Name)
	assert.Equal(t, "A test key", resp.Description)
	assert.True(t, resp.Exportable)
	assert.True(t, resp.DeletionProtection)
	assert.NotZero(t, resp.LastUsedAt)
	assert.Len(t, resp.Usage, 2)
}

func TestKmsMetadataToResponseWithoutLastUsedAtBoost(t *testing.T) {
	metadata := &kms.KeyMetadata{
		KeyID:      "test-key",
		Algorithm:  "AES-256-GCM",
		State:      kms.KeyStateEnabled,
		Version:    1,
		Created:    time.Now(),
		Updated:    time.Now(),
		LastUsedAt: nil,
	}

	resp := kmsMetadataToResponseFull(metadata)

	assert.Equal(t, "test-key", resp.KeyID)
	assert.Zero(t, resp.LastUsedAt)
}

func TestHandleListDEKsSuccessBoost(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ListDEKs", mock.Anything, mock.Anything).Return(&service.ListDEKsResponse{
		DEKs: []*service.DEKMetadata{
			{Path: "/dek/1", Algorithm: "AES-256-GCM"},
		},
		TotalCount: 1,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks", nil)
	w := httptest.NewRecorder()

	server.HandleListDEKs(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
