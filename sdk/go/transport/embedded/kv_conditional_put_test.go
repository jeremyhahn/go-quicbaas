package embedded

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// conditionalStore applies qrdb's conditional-write rules to one value, so
// the transport tests can check what reaches the service and what comes back.
type conditionalStore struct {
	value   []byte
	present bool
}

func (s *conditionalStore) swap(expect, set []byte) bool {
	if expect == nil {
		if s.present {
			return false
		}
	} else if !s.present || !bytes.Equal(s.value, expect) {
		return false
	}
	s.value, s.present = set, true
	return true
}

func newConditionalTransport(t *testing.T, store *conditionalStore) *Transport {
	t.Helper()
	mock := &mockQuicBaaS{
		compareAndSwapFunc: func(ctx context.Context, req *service.CompareAndSwapRequest) (*service.ConditionalPutResponse, error) {
			return &service.ConditionalPutResponse{Applied: store.swap(req.Expect, req.Set)}, nil
		},
		putIfAbsentFunc: func(ctx context.Context, req *service.PutIfAbsentRequest) (*service.ConditionalPutResponse, error) {
			return &service.ConditionalPutResponse{Applied: store.swap(nil, req.Data)}, nil
		},
	}
	tr, err := New(mock)
	require.NoError(t, err)
	return tr
}

func requestApplied(t *testing.T, tr *Transport, method string, req interface{}) bool {
	t.Helper()
	resp, err := tr.Request(context.Background(), "quicbaas.KV", method, req)
	require.NoError(t, err)
	out, ok := resp.(*service.ConditionalPutResponse)
	require.True(t, ok, "want *service.ConditionalPutResponse, got %T", resp)
	return out.Applied
}

func TestTransport_CompareAndSwap_SwapsOnMatch(t *testing.T) {
	store := &conditionalStore{value: []byte("v1"), present: true}
	tr := newConditionalTransport(t, store)

	applied := requestApplied(t, tr, "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "users", ID: 1, Expect: []byte("v1"), Set: []byte("v2")})

	assert.True(t, applied)
	assert.Equal(t, []byte("v2"), store.value)
}

func TestTransport_CompareAndSwap_MismatchReturnsFalse(t *testing.T) {
	store := &conditionalStore{value: []byte("v1"), present: true}
	tr := newConditionalTransport(t, store)

	applied := requestApplied(t, tr, "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "users", ID: 1, Expect: []byte("stale"), Set: []byte("v2")})

	assert.False(t, applied)
	assert.Equal(t, []byte("v1"), store.value)
}

func TestTransport_PutIfAbsent_FalseOnPresentKey(t *testing.T) {
	store := &conditionalStore{}
	tr := newConditionalTransport(t, store)

	first := requestApplied(t, tr, "PutIfAbsent", &service.PutIfAbsentRequest{EntityType: "users", ID: 1, Data: []byte("first")})
	second := requestApplied(t, tr, "PutIfAbsent", &service.PutIfAbsentRequest{EntityType: "users", ID: 1, Data: []byte("second")})

	assert.True(t, first)
	assert.False(t, second)
	assert.Equal(t, []byte("first"), store.value)
}

func TestTransport_CompareAndSwap_PutIfAbsent_InvalidRequestType(t *testing.T) {
	tr := newConditionalTransport(t, &conditionalStore{})

	for _, method := range []string{"CompareAndSwap", "PutIfAbsent"} {
		t.Run(method, func(t *testing.T) {
			resp, err := tr.Request(context.Background(), "quicbaas.KV", method, &service.PutRequest{})
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}
