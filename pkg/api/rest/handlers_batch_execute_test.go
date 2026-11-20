//go:build rest

package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestHandleBatchExecute(t *testing.T) {
	t.Run("mixed operations", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("BatchExecute", mock.Anything, mock.MatchedBy(func(r *service.BatchExecuteRequest) bool {
			if len(r.Operations) != 2 {
				return false
			}
			put, del := r.Operations[0], r.Operations[1]
			return put.Type == service.BatchExecutePut && put.EntityType == "users" &&
				string(put.Key) == "a" && bytes.Equal(put.Value, []byte("hi")) &&
				del.Type == service.BatchExecuteDelete && del.EntityType == "users" &&
				string(del.Key) == "b" && del.Value == nil
		})).Return(&service.BatchExecuteResponse{
			Results: []*service.BatchExecuteResult{{Succeeded: true}, {Succeeded: false}},
		}, nil).Once()

		body := `{"operations":[{"type":"PUT","entity_type":"users","key":"a","value":"aGk="},{"type":"DELETE","entity_type":"users","key":"b"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/execute", strings.NewReader(body))
		w := httptest.NewRecorder()
		server.HandleBatchExecute(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp BatchExecuteResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Success)
		assert.Equal(t, []BatchExecuteResult{{Success: true}, {Success: false}}, resp.Results)
		mockService.AssertExpectations(t)
	})

	refused := []struct {
		name string
		body string
		want string
	}{
		{"invalid JSON", `{bad`, ""},
		{"empty operations", `{"operations":[]}`, ""},
		{"GET operation", `{"operations":[{"type":"GET","entity_type":"users","key":"a"}]}`, "type must be PUT or DELETE"},
		{"invalid base64", `{"operations":[{"type":"PUT","entity_type":"users","key":"a","value":"!!"}]}`, ""},
	}
	for _, c := range refused {
		t.Run("refused before the service: "+c.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/execute", strings.NewReader(c.body))
			w := httptest.NewRecorder()
			server.HandleBatchExecute(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if c.want != "" {
				assert.Contains(t, w.Body.String(), c.want)
			}
			mockService.AssertNotCalled(t, "BatchExecute", mock.Anything, mock.Anything)
		})
	}

	serviceErrors := []struct {
		name string
		err  error
		code int
	}{
		{"internal", errors.New(errors.ErrInternal, "batch", "service error"), http.StatusInternalServerError},
		{"invalid input", errors.New(errors.ErrInvalidInput, "batch", "bad op"), http.StatusBadRequest},
	}
	for _, c := range serviceErrors {
		t.Run("service error: "+c.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			mockService.On("BatchExecute", mock.Anything, mock.Anything).Return(nil, c.err).Once()

			body := `{"operations":[{"type":"DELETE","entity_type":"users","key":"b"}]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/execute", strings.NewReader(body))
			w := httptest.NewRecorder()
			server.HandleBatchExecute(w, req)

			assert.Equal(t, c.code, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}
