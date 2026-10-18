package embedded

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// kvMapStore is an in-memory KV behind mockQuicBaaS, so a test can observe
// whether a request wrote or deleted anything.
type kvMapStore struct {
	mu   sync.Mutex
	data map[uint64][]byte
}

func newKVMapTransport(t *testing.T, seed map[uint64][]byte) (*Transport, *kvMapStore) {
	t.Helper()
	store := &kvMapStore{data: seed}
	mock := &mockQuicBaaS{
		putFunc: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			store.mu.Lock()
			defer store.mu.Unlock()
			store.data[req.ID] = req.Data
			return &service.PutResponse{ID: req.ID}, nil
		},
		getFunc: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			store.mu.Lock()
			defer store.mu.Unlock()
			data, ok := store.data[req.ID]
			return &service.GetResponse{ID: req.ID, Data: data, Found: ok}, nil
		},
		deleteFunc: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			store.mu.Lock()
			defer store.mu.Unlock()
			_, ok := store.data[req.ID]
			delete(store.data, req.ID)
			return &service.DeleteResponse{Deleted: ok}, nil
		},
	}
	transport, err := New(mock)
	require.NoError(t, err)
	return transport, store
}

func (s *kvMapStore) has(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[id]
	return ok
}

// deadTransportContexts returns a cancelled context and one past its
// deadline, each paired with the error errors.Is must match (#1583).
func deadTransportContexts(t *testing.T) map[string]struct {
	ctx  context.Context
	want error
} {
	t.Helper()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Unix(0, 0))
	t.Cleanup(cancelExpired)
	return map[string]struct {
		ctx  context.Context
		want error
	}{
		"cancelled": {ctx: cancelled, want: context.Canceled},
		"expired":   {ctx: expired, want: context.DeadlineExceeded},
	}
}

func TestTransport_KVPut_DeadContext_WritesNothing(t *testing.T) {
	for name, tc := range deadTransportContexts(t) {
		t.Run(name, func(t *testing.T) {
			transport, store := newKVMapTransport(t, map[uint64][]byte{})

			_, err := transport.Request(tc.ctx, "quicbaas.KV", "Put", &service.PutRequest{ID: 7, Data: []byte(`x`)})

			require.ErrorIs(t, err, tc.want)
			assert.False(t, store.has(7), "a dead context must not write")
		})
	}
}

func TestTransport_KVGet_DeadContext_Refused(t *testing.T) {
	for name, tc := range deadTransportContexts(t) {
		t.Run(name, func(t *testing.T) {
			transport, _ := newKVMapTransport(t, map[uint64][]byte{7: []byte(`x`)})

			resp, err := transport.Request(tc.ctx, "quicbaas.KV", "Get", &service.GetRequest{ID: 7})

			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, resp)
		})
	}
}

func TestTransport_KVDelete_DeadContext_DeletesNothing(t *testing.T) {
	for name, tc := range deadTransportContexts(t) {
		t.Run(name, func(t *testing.T) {
			transport, _ := newKVMapTransport(t, map[uint64][]byte{7: []byte(`x`)})

			_, err := transport.Request(tc.ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{ID: 7})

			require.ErrorIs(t, err, tc.want)
			resp, err := transport.Request(context.Background(), "quicbaas.KV", "Get", &service.GetRequest{ID: 7})
			require.NoError(t, err)
			assert.True(t, resp.(*service.GetResponse).Found, "a dead context must not delete")
		})
	}
}

func TestTransport_KV_LiveContext_Succeeds(t *testing.T) {
	ctx := context.Background()
	transport, store := newKVMapTransport(t, map[uint64][]byte{})

	_, err := transport.Request(ctx, "quicbaas.KV", "Put", &service.PutRequest{ID: 7, Data: []byte(`x`)})
	require.NoError(t, err)

	resp, err := transport.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{ID: 7})
	require.NoError(t, err)
	assert.Equal(t, []byte(`x`), resp.(*service.GetResponse).Data)

	_, err = transport.Request(ctx, "quicbaas.KV", "Delete", &service.DeleteRequest{ID: 7})
	require.NoError(t, err)
	assert.False(t, store.has(7))
}
