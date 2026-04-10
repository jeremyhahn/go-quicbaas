//go:build rest

package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestHandleGetRebalanceStatus_WithOperationID tests HandleGetRebalanceStatus
// when an operation_id query parameter is provided.
func TestHandleGetRebalanceStatus_WithOperationID(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetRebalanceProgress", mock.Anything, mock.Anything).Return(&service.GetRebalanceProgressResponse{
		OperationID:    "op-123",
		Status:         "in_progress",
		Progress:       45.5,
		ShardsMoved:    10,
		TotalShards:    22,
		ElapsedSeconds: 120,
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/rebalance/status?operation_id=op-123", nil)
	w := httptest.NewRecorder()

	server.HandleGetRebalanceStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "op-123")
	assert.Contains(t, w.Body.String(), "in_progress")
}

// TestHandleGetRebalanceStatus_ServiceError tests HandleGetRebalanceStatus
// when the service returns an error.
func TestHandleGetRebalanceStatus_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetRebalanceProgress", mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/rebalance/status?operation_id=op-err", nil)
	w := httptest.NewRecorder()

	server.HandleGetRebalanceStatus(w, req)

	assert.True(t, w.Code >= 400)
}
