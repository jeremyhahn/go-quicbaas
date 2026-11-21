//go:build rest

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// conditionalPutRequest builds a POST to a key's conditional-write route
// with the chi {type} and {key} parameters set.
func conditionalPutRequest(t *testing.T, entityType, key, body string) *http.Request {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", entityType)
	rctx.URLParams.Add("key", key)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kv/"+entityType+"/"+key+"/compare-and-swap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// decodeApplied reads the {"applied": bool} body of a conditional write.
func decodeApplied(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	var resp ConditionalPutResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Applied
}

func TestHandleCompareAndSwap(t *testing.T) {
	stored := []byte("v1")
	tests := []struct {
		name   string
		expect []byte
		want   bool
	}{
		{name: "matching expect is applied", expect: []byte("v1"), want: true},
		{name: "stale expect is not applied", expect: []byte("v0"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			// The mock stands in for qrdb's match: applied only when expect equals the stored value.
			mockService.On("CompareAndSwap", mock.Anything, mock.MatchedBy(func(r *service.CompareAndSwapRequest) bool {
				return r.EntityType == "locks" && r.ID == hashStringKey("locks", "7") &&
					bytes.Equal(r.Expect, mustWrapKV(t, "7", tt.expect)) && bytes.Equal(r.Set, mustWrapKV(t, "7", []byte("v2")))
			})).Return(&service.ConditionalPutResponse{Applied: bytes.Equal(tt.expect, stored)}, nil)

			body, err := json.Marshal(CompareAndSwapRequest{Expect: tt.expect, Set: []byte("v2")})
			require.NoError(t, err)
			w := httptest.NewRecorder()
			server.HandleCompareAndSwap(w, conditionalPutRequest(t, "locks", "7", string(body)))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tt.want, decodeApplied(t, w))
		})
	}
}

func TestHandleCompareAndSwap_BadInput(t *testing.T) {
	tests := []struct {
		name string
		id   string
		body string
	}{
		{name: "missing key", id: "", body: `{"expect":"djE=","set":"djI="}`},
		{name: "invalid JSON body", id: "7", body: `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			w := httptest.NewRecorder()
			server.HandleCompareAndSwap(w, conditionalPutRequest(t, "locks", tt.id, tt.body))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			mockService.AssertNotCalled(t, "CompareAndSwap", mock.Anything, mock.Anything)
		})
	}
}

func TestHandleCompareAndSwap_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()
	mockService.On("CompareAndSwap", mock.Anything, mock.Anything).Return(nil, service.ErrRaft)

	w := httptest.NewRecorder()
	server.HandleCompareAndSwap(w, conditionalPutRequest(t, "locks", "7", `{"expect":"djE=","set":"djI="}`))

	assert.GreaterOrEqual(t, w.Code, http.StatusInternalServerError)
}

func TestHandlePutIfAbsent(t *testing.T) {
	tests := []struct {
		name    string
		present bool
		want    bool
	}{
		{name: "absent key is applied", present: false, want: true},
		{name: "present key is not applied", present: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			mockService.On("PutIfAbsent", mock.Anything, mock.MatchedBy(func(r *service.PutIfAbsentRequest) bool {
				return r.EntityType == "locks" && r.ID == hashStringKey("locks", "7") && bytes.Equal(r.Data, mustWrapKV(t, "7", []byte("owner-a")))
			})).Return(&service.ConditionalPutResponse{Applied: !tt.present}, nil)

			body, err := json.Marshal(PutIfAbsentRequest{Data: []byte("owner-a")})
			require.NoError(t, err)
			w := httptest.NewRecorder()
			server.HandlePutIfAbsent(w, conditionalPutRequest(t, "locks", "7", string(body)))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tt.want, decodeApplied(t, w))
		})
	}
}

func TestHandlePutIfAbsent_BadInput(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		body       string
	}{
		{name: "missing entity type", entityType: "", body: `{"data":"djE="}`},
		{name: "invalid JSON body", entityType: "locks", body: `not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			w := httptest.NewRecorder()
			server.HandlePutIfAbsent(w, conditionalPutRequest(t, tt.entityType, "7", tt.body))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			mockService.AssertNotCalled(t, "PutIfAbsent", mock.Anything, mock.Anything)
		})
	}
}

func TestHandlePutIfAbsent_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()
	mockService.On("PutIfAbsent", mock.Anything, mock.Anything).Return(nil, service.ErrRaft)

	w := httptest.NewRecorder()
	server.HandlePutIfAbsent(w, conditionalPutRequest(t, "locks", "7", `{"data":"djE="}`))

	assert.GreaterOrEqual(t, w.Code, http.StatusInternalServerError)
}

// mustWrapKV is wrapKVValue for a test.
func mustWrapKV(t *testing.T, key string, value []byte) []byte {
	t.Helper()
	wrapped, err := wrapKVValue(key, value)
	require.NoError(t, err)
	return wrapped
}

// TestHandleCompareAndSwap_AddressesTheRecordAKVPutWrote pins the keyspace:
// a compare-and-swap whose expect is the value a KV put stored names the
// same ID and the same stored bytes the put did.
func TestHandleCompareAndSwap_AddressesTheRecordAKVPutWrote(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	var put *service.PutRequest
	mockService.On("Put", mock.Anything, mock.MatchedBy(func(r *service.PutRequest) bool {
		put = r
		return true
	})).Return(&service.PutResponse{ID: 1, Timestamp: 1}, nil)
	putBody, err := json.Marshal(map[string]interface{}{"value": []byte("v1")})
	require.NoError(t, err)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", "locks")
	rctx.URLParams.Add("key", "door")
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/kv/locks/door", bytes.NewReader(putBody))
	putReq = putReq.WithContext(context.WithValue(putReq.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleKVPut(w, putReq)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, put)

	mockService.On("CompareAndSwap", mock.Anything, mock.MatchedBy(func(r *service.CompareAndSwapRequest) bool {
		return r.EntityType == put.EntityType && r.ID == put.ID && bytes.Equal(r.Expect, put.Data)
	})).Return(&service.ConditionalPutResponse{Applied: true}, nil)
	casBody, err := json.Marshal(CompareAndSwapRequest{Expect: []byte("v1"), Set: []byte("v2")})
	require.NoError(t, err)
	w = httptest.NewRecorder()
	server.HandleCompareAndSwap(w, conditionalPutRequest(t, "locks", "door", string(casBody)))

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, decodeApplied(t, w))
}
