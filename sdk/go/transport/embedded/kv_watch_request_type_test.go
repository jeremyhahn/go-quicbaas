package embedded

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
)

// kvWatchTestTimeout bounds every wait in these tests so a broken stream
// fails the test instead of hanging it.
const kvWatchTestTimeout = 5 * time.Second

// kvWatchHub is a service double for the KV Watch stream: Watch records the
// request and the response channel, and every Put publishes the written value
// as one event on the open watch, followed by an optional delete event.
type kvWatchHub struct {
	opened chan *service.WatchRequest

	mu          sync.Mutex
	stream      chan<- *service.WatchResponse
	key         []byte
	alsoDeletes bool
}

func newKVWatchTransport(t *testing.T, alsoDeletes bool) (*Transport, *kvWatchHub) {
	t.Helper()
	hub := &kvWatchHub{opened: make(chan *service.WatchRequest, 1), alsoDeletes: alsoDeletes}
	mock := &mockQuicBaaS{
		watchFunc: func(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error) {
			hub.mu.Lock()
			hub.stream = stream
			hub.key = req.Key
			hub.mu.Unlock()
			hub.opened <- req
			return 1, nil
		},
		putFunc: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			hub.publish(req.Data)
			return &service.PutResponse{ID: req.ID}, nil
		},
	}
	transport, err := New(mock)
	require.NoError(t, err)
	return transport, hub
}

func (h *kvWatchHub) publish(value []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	events := []*service.WatchEvent{{
		Type:      service.WatchEventTypePut,
		Key:       h.key,
		Value:     value,
		Revision:  3,
		Timestamp: 1700000000000000000,
	}}
	if h.alsoDeletes {
		events = append(events, &service.WatchEvent{
			Type:      service.WatchEventTypeDelete,
			Key:       h.key,
			PrevValue: value,
			Revision:  4,
			Timestamp: 1700000000000000001,
		})
	}
	h.stream <- &service.WatchResponse{WatchID: 1, Events: events, Revision: 4}
}

func waitWatchOpened(t *testing.T, hub *kvWatchHub) *service.WatchRequest {
	t.Helper()
	select {
	case req := <-hub.opened:
		return req
	case <-time.After(kvWatchTestTimeout):
		require.FailNow(t, "the Watch request never reached the embedded handler")
		return nil
	}
}

func nextWatchEvent(t *testing.T, w kv.Watcher) *kv.WatchEvent {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		require.True(t, ok, "watch closed before delivering an event: %v", w.Err())
		return ev
	case <-time.After(kvWatchTestTimeout):
		require.FailNow(t, "no watch event delivered", "watcher error: %v", w.Err())
		return nil
	}
}

// TestKVClient_Watch_ReachesEmbeddedHandler opens a Watch with the real kv
// client over the embedded transport, writes the key and receives its event:
// the request reaches the handler typed, with every option mapped onto the
// service request.
func TestKVClient_Watch_ReachesEmbeddedHandler(t *testing.T) {
	transport, hub := newKVWatchTransport(t, false)
	client := kv.NewClient(transport)
	ctx, cancel := context.WithTimeout(context.Background(), kvWatchTestTimeout)
	defer cancel()

	w, err := client.Watch(ctx, "config/db",
		kv.WithStartRevision(5), kv.WithPrevKV(), kv.WithProgressNotify(), kv.WithFilterDelete())
	require.NoError(t, err)
	defer w.Close()

	req := waitWatchOpened(t, hub)
	assert.Equal(t, []byte("config/db"), req.Key)
	assert.False(t, req.IsPrefix)
	assert.Equal(t, int64(5), req.StartRevision)
	assert.True(t, req.PrevKV)
	assert.True(t, req.ProgressNotify)
	assert.Equal(t, []service.WatchEventType{service.WatchEventTypeDelete}, req.Filters)

	_, err = client.Put(ctx, &service.PutRequest{EntityType: "config", ID: 1, Data: []byte("v1")})
	require.NoError(t, err)

	ev := nextWatchEvent(t, w)
	assert.Equal(t, kv.EventPut, ev.Type)
	assert.Equal(t, "config/db", ev.Key)
	assert.Equal(t, []byte("v1"), ev.Value)
	assert.Equal(t, uint64(3), ev.Revision)
	assert.Equal(t, time.Unix(0, 1700000000000000000), ev.Timestamp)
	assert.NoError(t, w.Err())
}

// TestKVClient_WatchPrefix_ReachesEmbeddedHandler checks the prefix form and
// that a put filter still drops put events: only the delete is delivered.
func TestKVClient_WatchPrefix_ReachesEmbeddedHandler(t *testing.T) {
	transport, hub := newKVWatchTransport(t, true)
	client := kv.NewClient(transport)
	ctx, cancel := context.WithTimeout(context.Background(), kvWatchTestTimeout)
	defer cancel()

	w, err := client.WatchPrefix(ctx, "users/", kv.WithFilterPut())
	require.NoError(t, err)
	defer w.Close()

	req := waitWatchOpened(t, hub)
	assert.Equal(t, []byte("users/"), req.Key)
	assert.True(t, req.IsPrefix)
	assert.Equal(t, int64(0), req.StartRevision)
	assert.Equal(t, []service.WatchEventType{service.WatchEventTypePut}, req.Filters)

	_, err = client.Put(ctx, &service.PutRequest{EntityType: "users", ID: 2, Data: []byte("alice")})
	require.NoError(t, err)

	ev := nextWatchEvent(t, w)
	assert.Equal(t, kv.EventDelete, ev.Type)
	assert.Equal(t, "users/", ev.Key)
	assert.Equal(t, []byte("alice"), ev.PrevValue)
	assert.Equal(t, uint64(4), ev.Revision)
}

// TestKVClient_Watch_ContextCancelClosesStream checks that cancelling the
// watch context closes the event channel.
func TestKVClient_Watch_ContextCancelClosesStream(t *testing.T) {
	transport, hub := newKVWatchTransport(t, false)
	client := kv.NewClient(transport)
	ctx, cancel := context.WithCancel(context.Background())

	w, err := client.Watch(ctx, "config/db")
	require.NoError(t, err)
	defer w.Close()
	waitWatchOpened(t, hub)

	cancel()
	timeout := time.After(kvWatchTestTimeout)
	for {
		select {
		case _, ok := <-w.Events():
			if !ok {
				assert.Error(t, w.Err())
				return
			}
		case <-timeout:
			require.FailNow(t, "the watch stream stayed open after its context was cancelled")
		}
	}
}

// TestTransport_KVWatch_WrongRequestType checks that the Watch stream handler
// still refuses a request of another type with the typed error.
func TestTransport_KVWatch_WrongRequestType(t *testing.T) {
	transport, _ := newKVWatchTransport(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), kvWatchTestTimeout)
	defer cancel()

	stream, err := transport.RequestStream(ctx, "quicbaas.KV", "Watch", &service.GetRequest{ID: 1})
	require.NoError(t, err)
	defer stream.Close()

	requireStreamInvalidRequestType(t, stream)
}
