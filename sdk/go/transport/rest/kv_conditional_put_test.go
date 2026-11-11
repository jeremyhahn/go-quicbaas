package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// conditionalPutServer serves the two conditional-write routes over one
// in-memory value per path prefix, so a test can drive a conflict end to end.
type conditionalPutServer struct {
	mu     sync.Mutex
	stored map[string][]byte
}

func (s *conditionalPutServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Expect []byte `json:"expect"`
		Set    []byte `json:"set"`
		Data   []byte `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	applied := false
	switch r.URL.Path {
	case "/api/v1/kv/locks/7/compare-and-swap":
		held, ok := s.stored["locks/7"]
		if ok && bytes.Equal(held, body.Expect) {
			s.stored["locks/7"] = body.Set
			applied = true
		}
	case "/api/v1/kv/locks/7/put-if-absent":
		if _, ok := s.stored["locks/7"]; !ok {
			s.stored["locks/7"] = body.Data
			applied = true
		}
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"applied": applied}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func newConditionalPutHandler(t *testing.T, stored map[string][]byte) *kvHandler {
	t.Helper()
	server := httptest.NewServer(&conditionalPutServer{stored: stored})
	t.Cleanup(server.Close)
	return newKVHandler(&Transport{
		baseURL: server.URL,
		client:  server.Client(),
		headers: map[string]string{},
	})
}

func appliedOf(t *testing.T, resp interface{}) bool {
	t.Helper()
	condResp, ok := resp.(*service.ConditionalPutResponse)
	require.True(t, ok, "response type %T", resp)
	return condResp.Applied
}

func TestKVHandler_CompareAndSwap(t *testing.T) {
	handler := newConditionalPutHandler(t, map[string][]byte{"locks/7": []byte("v1")})

	resp, err := handler.handleRequest(context.Background(), "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "locks", ID: 7, Expect: []byte("v1"), Set: []byte("v2")})
	require.NoError(t, err)
	assert.True(t, appliedOf(t, resp), "matching expect must apply")

	// The value is now v2, so the same expect is stale.
	resp, err = handler.handleRequest(context.Background(), "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "locks", ID: 7, Expect: []byte("v1"), Set: []byte("v3")})
	require.NoError(t, err)
	assert.False(t, appliedOf(t, resp), "stale expect must not apply")
}

func TestKVHandler_CompareAndSwap_Errors(t *testing.T) {
	handler := newConditionalPutHandler(t, map[string][]byte{})

	_, err := handler.handleRequest(context.Background(), "CompareAndSwap", &service.PutRequest{})
	assert.True(t, errors.IsInvalidInput(err), "wrong request type: %v", err)

	_, err = handler.handleRequest(context.Background(), "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "other", ID: 1})
	assert.Error(t, err, "a 404 from the server must surface as an error")
}

func TestKVHandler_PutIfAbsent(t *testing.T) {
	handler := newConditionalPutHandler(t, map[string][]byte{})

	resp, err := handler.handleRequest(context.Background(), "PutIfAbsent",
		&service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner-a")})
	require.NoError(t, err)
	assert.True(t, appliedOf(t, resp), "absent key must apply")

	resp, err = handler.handleRequest(context.Background(), "PutIfAbsent",
		&service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner-b")})
	require.NoError(t, err)
	assert.False(t, appliedOf(t, resp), "present key must not apply")
}

func TestKVHandler_PutIfAbsent_Errors(t *testing.T) {
	handler := newConditionalPutHandler(t, map[string][]byte{})

	_, err := handler.handleRequest(context.Background(), "PutIfAbsent", (*service.PutIfAbsentRequest)(nil))
	assert.True(t, errors.IsInvalidInput(err), "nil request: %v", err)

	_, err = handler.handleRequest(context.Background(), "PutIfAbsent",
		&service.PutIfAbsentRequest{EntityType: "other", ID: 1})
	assert.Error(t, err, "a 404 from the server must surface as an error")
}
