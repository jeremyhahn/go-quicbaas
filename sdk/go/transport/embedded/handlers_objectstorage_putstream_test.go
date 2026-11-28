// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package embedded

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	objclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	transferclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objecttransfer"
)

// memoryObjectStore is an ObjectStorage service double that keeps every
// PutObject request it is given, so a test can read back exactly what the
// stream handler sent and fetch the stored bytes again.
type memoryObjectStore struct {
	mockObjectStorageService
	mu      sync.Mutex
	puts    []*service.PutObjectRequest
	getReqs []*service.GetObjectRequest
}

func newMemoryObjectStore() *memoryObjectStore {
	m := &memoryObjectStore{}
	m.putObjectFunc = m.put
	m.getObjectFunc = m.get
	return m
}

func (m *memoryObjectStore) put(_ context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts = append(m.puts, req)
	return &service.PutObjectResponse{Key: req.Key, Version: uint64(len(m.puts)), Size: int64(len(req.Value))}, nil
}

func (m *memoryObjectStore) get(_ context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getReqs = append(m.getReqs, req)
	for i := len(m.puts) - 1; i >= 0; i-- {
		if m.puts[i].Key == req.Key {
			return &service.GetObjectResponse{Key: req.Key, Value: m.puts[i].Value, Size: int64(len(m.puts[i].Value))}, nil
		}
	}
	return nil, catalog.NewEntryNotFoundError(req.Key)
}

func (m *memoryObjectStore) storedPuts() []*service.PutObjectRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*service.PutObjectRequest(nil), m.puts...)
}

func (m *memoryObjectStore) getRequests() []*service.GetObjectRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*service.GetObjectRequest(nil), m.getReqs...)
}

func newObjectStreamTransport(t *testing.T, store service.ObjectStorageService) *Transport {
	t.Helper()
	tr, err := New(&mockQuicBaaS{objectStorageService: store})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	return tr
}

// recvStreamError reads the one message a failed stream handler leaves and
// returns it as the error the transport delivered.
func recvStreamError(t *testing.T, s interface{ Recv() (interface{}, error) }) error {
	t.Helper()
	msg, err := s.Recv()
	require.NoError(t, err)
	delivered, ok := msg.(error)
	require.Truef(t, ok, "stream delivered %T, want an error", msg)
	return delivered
}

func TestStreamHandlers_ObjectStreamsRegistered(t *testing.T) {
	for _, key := range []string{
		"quicbaas.ObjectStorage/StreamPutObject",
		"quicbaas.ObjectStorage/StreamGetObject",
		"quicbaas.ObjectTransfer/StreamFetchObjects",
	} {
		_, ok := streamHandlers[key]
		assert.Truef(t, ok, "stream handler %s is not registered", key)
	}
}

func TestStreamPutObject_CarriesEveryFieldAndGetReturnsBytes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	store := newMemoryObjectStore()
	client := objclient.NewClient(newObjectStreamTransport(t, store))

	payload := []byte("streamed object payload spanning several chunks")
	writer, err := client.StreamPutObject(ctx, objclient.StreamPutOptions{
		Key:         "ns:stream/obj",
		ContentType: "application/octet-stream",
		TenantID:    "tenant-a",
		Tags:        map[string]string{"env": "test"},
		ReplicationPolicy: &objclient.ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
			Mode:        objclient.ReplicationModeSync,
		},
		ArchivePolicy: &objclient.ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 30,
			TargetStore:      "cold",
		},
		RetentionPolicy: &objclient.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 365,
		},
		TargetBackend: "local-ssd",
		ChunkSize:     8,
	})
	require.NoError(t, err)

	n, err := writer.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)

	resp, err := writer.Close()
	require.NoError(t, err)
	assert.Equal(t, "ns:stream/obj", resp.Key)
	assert.Equal(t, int64(len(payload)), resp.Size)

	puts := store.storedPuts()
	require.Len(t, puts, 1, "a streamed put must reach the service exactly once")
	got := puts[0]
	assert.Equal(t, "ns:stream/obj", got.Key)
	assert.Equal(t, payload, got.Value)
	assert.Equal(t, "application/octet-stream", got.ContentType)
	assert.Equal(t, "tenant-a", got.TenantID)
	assert.Equal(t, map[string]string{"env": "test"}, got.Tags)
	assert.Equal(t, "local-ssd", got.TargetBackend)
	require.NotNil(t, got.ReplicationPolicy)
	assert.True(t, got.ReplicationPolicy.Enabled)
	assert.Equal(t, 3, got.ReplicationPolicy.MinReplicas)
	assert.Equal(t, catalog.ReplicationModeSync, got.ReplicationPolicy.Mode)
	require.NotNil(t, got.ArchivePolicy)
	assert.True(t, got.ArchivePolicy.Enabled)
	assert.Equal(t, 30, got.ArchivePolicy.ArchiveAfterDays)
	assert.Equal(t, "cold", got.ArchivePolicy.TargetStore)
	require.NotNil(t, got.RetentionPolicy)
	assert.True(t, got.RetentionPolicy.Enabled)
	assert.Equal(t, 365, got.RetentionPolicy.RetentionDays)

	reader, err := client.StreamGetObject(ctx, &objclient.GetObjectRequest{Key: "ns:stream/obj"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	meta, err := reader.ReadMetadata()
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), meta.Size)

	read := make([]byte, meta.Size)
	_, err = io.ReadFull(reader, read)
	require.NoError(t, err)
	assert.Equal(t, payload, read)
}

func TestStreamPutObject_RejectsAChunkOfTheWrongType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	store := newMemoryObjectStore()
	tr := newObjectStreamTransport(t, store)

	s, err := tr.RequestStream(ctx, "quicbaas.ObjectStorage", "StreamPutObject", nil)
	require.NoError(t, err)
	require.NoError(t, s.Send("not a chunk"))

	var invalid *InvalidRequestTypeError
	require.ErrorAs(t, recvStreamError(t, s), &invalid)
	assert.Empty(t, store.storedPuts())
}

func TestStreamPutObject_RejectsAStreamThatEndsEarly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	store := newMemoryObjectStore()
	tr := newObjectStreamTransport(t, store)

	s, err := tr.RequestStream(ctx, "quicbaas.ObjectStorage", "StreamPutObject", nil)
	require.NoError(t, err)
	require.NoError(t, s.CloseSend())

	var incomplete *IncompleteStreamError
	require.ErrorAs(t, recvStreamError(t, s), &incomplete)
	assert.Empty(t, store.storedPuts())
}

func TestStreamPutObject_ServiceNotConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })

	s, err := tr.RequestStream(ctx, "quicbaas.ObjectStorage", "StreamPutObject", nil)
	require.NoError(t, err)

	var notConfigured *ServiceNotConfiguredError
	require.ErrorAs(t, recvStreamError(t, s), &notConfigured)
}

func TestStreamPutObject_CanceledMidStreamStoresNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newMemoryObjectStore()
		tr, err := New(&mockQuicBaaS{objectStorageService: store})
		require.NoError(t, err)
		client := objclient.NewClient(tr)

		ctx, cancel := context.WithCancel(context.Background())
		writer, err := client.StreamPutObject(ctx, objclient.StreamPutOptions{Key: "ns:stream/canceled", ChunkSize: 4})
		require.NoError(t, err)
		_, err = writer.Write([]byte("first half"))
		require.NoError(t, err)

		cancel()
		_, err = writer.Close()
		require.Error(t, err, "a put whose context is canceled must not complete")

		// Every goroutine of the bubble, the stream handler included, has
		// finished or blocked durably: the handler's verdict is final.
		synctest.Wait()
		assert.Empty(t, store.storedPuts())
		require.NoError(t, tr.Close())
	})
}

func TestStreamGetObject_MissingKeyReturnsServiceNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	store := newMemoryObjectStore()
	tr := newObjectStreamTransport(t, store)
	req := &objclient.GetObjectRequest{Key: "ns:stream/missing", IncludeMetadata: true}

	s, err := tr.RequestStream(ctx, "quicbaas.ObjectStorage", "StreamGetObject", req)
	require.NoError(t, err)
	assert.True(t, catalog.IsNotFound(recvStreamError(t, s)), "the service's not-found error must survive the stream")

	reader, err := objclient.NewClient(tr).StreamGetObject(ctx, req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	_, err = reader.ReadMetadata()
	require.Error(t, err)

	gets := store.getRequests()
	require.Len(t, gets, 2)
	for _, got := range gets {
		assert.Equal(t, "ns:stream/missing", got.Key)
		assert.True(t, got.IncludeMetadata, "IncludeMetadata must reach the service")
	}
}

func TestStreamGetObject_InvalidRequestType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	tr := newObjectStreamTransport(t, newMemoryObjectStore())
	s, err := tr.RequestStream(ctx, "quicbaas.ObjectStorage", "StreamGetObject", "not a request")
	require.NoError(t, err)

	var invalid *InvalidRequestTypeError
	require.ErrorAs(t, recvStreamError(t, s), &invalid)
}

func TestStreamGetObject_ChunksAnObjectLargerThanOneChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	payload := bytes.Repeat([]byte("0123456789abcdef"), objectStreamChunkSize/8)
	store := newMemoryObjectStore()
	store.puts = []*service.PutObjectRequest{{Key: "ns:stream/large", Value: payload}}
	tr := newObjectStreamTransport(t, store)

	s, err := tr.RequestStream(ctx, "quicbaas.ObjectStorage", "StreamGetObject", &objclient.GetObjectRequest{Key: "ns:stream/large"})
	require.NoError(t, err)

	var got []byte
	for chunks := 0; ; chunks++ {
		require.Less(t, chunks, 3, "a two-chunk object must end after its second chunk")
		msg, err := s.Recv()
		require.NoError(t, err)
		chunk, ok := msg.(*objclient.ObjectChunk)
		require.Truef(t, ok, "stream delivered %T, want *objectstorage.ObjectChunk", msg)
		assert.Equal(t, int64(len(got)), chunk.Offset)
		assert.Equal(t, int64(len(payload)), chunk.TotalSize)
		got = append(got, chunk.Data...)
		if chunk.IsLast {
			break
		}
	}
	assert.Equal(t, payload, got)
}

// memoryObjectTransfer is an ObjectTransfer service double that serves the
// objects it holds and refuses an unknown object reference with the
// catalog's typed not-found error.
type memoryObjectTransfer struct {
	objects map[string][]byte
	mu      sync.Mutex
	reqs    []*service.StreamFetchObjectsRequest
}

func (m *memoryObjectTransfer) FetchObject(context.Context, *service.FetchObjectRequest) (*service.FetchObjectResponse, error) {
	return nil, NewStreamNotSupportedError("quicbaas.ObjectTransfer", "FetchObject")
}

func (m *memoryObjectTransfer) StreamFetchObjects(_ context.Context, req *service.StreamFetchObjectsRequest) (*service.StreamFetchObjectsResponse, error) {
	m.mu.Lock()
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()

	resp := &service.StreamFetchObjectsResponse{TotalCount: len(req.Items)}
	for _, item := range req.Items {
		data, ok := m.objects[item.ObjectRef]
		if !ok {
			return nil, catalog.NewEntryNotFoundError(item.ObjectRef)
		}
		resp.Objects = append(resp.Objects, service.FetchObjectResponse{
			ObjectRef:       item.ObjectRef,
			Data:            data,
			Checksum:        item.Checksum,
			Size:            int64(len(data)),
			CompressionAlgo: "none",
			Found:           true,
		})
		resp.SuccessCount++
	}
	return resp, nil
}

func (m *memoryObjectTransfer) requests() []*service.StreamFetchObjectsRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*service.StreamFetchObjectsRequest(nil), m.reqs...)
}

// objectTransferQuicBaaS serves an ObjectTransfer service from the shared
// QuicBaaS double.
type objectTransferQuicBaaS struct {
	*mockQuicBaaS
	transfer service.ObjectTransferService
}

func (m *objectTransferQuicBaaS) ObjectTransferService() service.ObjectTransferService {
	return m.transfer
}

func newObjectTransferTransport(t *testing.T, transfer service.ObjectTransferService) *Transport {
	t.Helper()
	tr, err := New(&objectTransferQuicBaaS{mockQuicBaaS: &mockQuicBaaS{}, transfer: transfer})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	return tr
}

func TestStreamFetchObjects_ReturnsEveryObject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	transfer := &memoryObjectTransfer{objects: map[string][]byte{
		"bucket:a": []byte("alpha"),
		"bucket:b": []byte("bravo"),
	}}
	client := transferclient.NewClient(newObjectTransferTransport(t, transfer))

	responses, err := client.StreamFetchObjects(ctx, &transferclient.StreamFetchObjectsRequest{
		ShardID: 7,
		Items: []transferclient.ObjectFetchItem{
			{ObjectRef: "bucket:a", Checksum: []byte("sum-a")},
			{ObjectRef: "bucket:b", Checksum: []byte("sum-b")},
		},
	})
	require.NoError(t, err)

	var got []*transferclient.FetchObjectResponse
	for done := false; !done; {
		select {
		case resp, open := <-responses:
			if !open {
				done = true
				continue
			}
			got = append(got, resp)
		case <-ctx.Done():
			t.Fatal("the response channel was not closed after the last object")
		}
	}

	require.Len(t, got, 2, "every requested object, and nothing else, must come back")
	for i, want := range []struct{ ref, data, sum string }{
		{"bucket:a", "alpha", "sum-a"},
		{"bucket:b", "bravo", "sum-b"},
	} {
		assert.Empty(t, got[i].ErrorMessage)
		assert.True(t, got[i].Found)
		assert.Equal(t, want.ref, got[i].ObjectRef)
		assert.Equal(t, []byte(want.data), got[i].Data)
		assert.Equal(t, []byte(want.sum), got[i].Checksum)
		assert.Equal(t, int64(len(want.data)), got[i].Size)
		assert.Equal(t, transferclient.CompressionNone, got[i].CompressionAlgo)
	}

	reqs := transfer.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, uint64(7), reqs[0].ShardID)
	assert.Equal(t, []service.ObjectFetchItem{
		{ObjectRef: "bucket:a", Checksum: []byte("sum-a")},
		{ObjectRef: "bucket:b", Checksum: []byte("sum-b")},
	}, reqs[0].Items)
}

func TestStreamFetchObjects_UnknownRefReturnsServiceError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	transfer := &memoryObjectTransfer{objects: map[string][]byte{"bucket:a": []byte("alpha")}}
	tr := newObjectTransferTransport(t, transfer)

	s, err := tr.RequestStream(ctx, "quicbaas.ObjectTransfer", "StreamFetchObjects", &transferclient.StreamFetchObjectsRequest{
		ShardID: 7,
		Items:   []transferclient.ObjectFetchItem{{ObjectRef: "bucket:unknown"}},
	})
	require.NoError(t, err)

	delivered := recvStreamError(t, s)
	var catalogErr *catalog.CatalogError
	require.True(t, stderrors.As(delivered, &catalogErr), "got %v, want the service's catalog error", delivered)
	assert.True(t, catalog.IsNotFound(delivered))
}

func TestStreamFetchObjects_InvalidRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	t.Run("service not configured", func(t *testing.T) {
		tr := newObjectTransferTransport(t, nil)
		s, err := tr.RequestStream(ctx, "quicbaas.ObjectTransfer", "StreamFetchObjects", &transferclient.StreamFetchObjectsRequest{ShardID: 1})
		require.NoError(t, err)

		var notConfigured *ServiceNotConfiguredError
		require.ErrorAs(t, recvStreamError(t, s), &notConfigured)
	})

	t.Run("wrong request type", func(t *testing.T) {
		tr := newObjectTransferTransport(t, &memoryObjectTransfer{})
		s, err := tr.RequestStream(ctx, "quicbaas.ObjectTransfer", "StreamFetchObjects", "not a request")
		require.NoError(t, err)

		var invalid *InvalidRequestTypeError
		require.ErrorAs(t, recvStreamError(t, s), &invalid)
	})
}
