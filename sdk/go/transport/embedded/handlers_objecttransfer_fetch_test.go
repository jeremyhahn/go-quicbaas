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
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	transferclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objecttransfer"
)

// fetchFailedError is the service error the fake returns when told to fail.
type fetchFailedError struct{ ref string }

func (e *fetchFailedError) Error() string { return "fetch failed: " + e.ref }

// scriptedObjectTransfer answers FetchObject with a fixed response or error
// and records every request it receives.
type scriptedObjectTransfer struct {
	resp *service.FetchObjectResponse
	err  error

	mu   sync.Mutex
	reqs []*service.FetchObjectRequest
}

func (f *scriptedObjectTransfer) FetchObject(_ context.Context, req *service.FetchObjectRequest) (*service.FetchObjectResponse, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	return f.resp, f.err
}

func (f *scriptedObjectTransfer) StreamFetchObjects(context.Context, *service.StreamFetchObjectsRequest) (*service.StreamFetchObjectsResponse, error) {
	return nil, &fetchFailedError{ref: "stream"}
}

func (f *scriptedObjectTransfer) requests() []*service.FetchObjectRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*service.FetchObjectRequest(nil), f.reqs...)
}

func fetchContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestFetchObject_Registered(t *testing.T) {
	_, ok := unaryHandlers["quicbaas.ObjectTransfer/FetchObject"]
	assert.True(t, ok)
}

func TestFetchObject_RequestFieldsReachTheService(t *testing.T) {
	transfer := &scriptedObjectTransfer{resp: &service.FetchObjectResponse{ObjectRef: "bucket:key", Found: true}}
	client := transferclient.NewClient(newObjectTransferTransport(t, transfer))

	_, err := client.FetchObject(fetchContext(t), &transferclient.FetchObjectRequest{
		ShardID:   7,
		ObjectRef: "bucket:key",
		Checksum:  []byte{0xde, 0xad},
		RequestID: "req-1",
	})
	require.NoError(t, err)

	reqs := transfer.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, &service.FetchObjectRequest{
		ShardID:   7,
		ObjectRef: "bucket:key",
		Checksum:  []byte{0xde, 0xad},
		RequestID: "req-1",
	}, reqs[0])
}

func TestFetchObject_ResponseFieldsReachTheClient(t *testing.T) {
	tests := []struct {
		name string
		resp *service.FetchObjectResponse
		want *transferclient.FetchObjectResponse
	}{
		{
			name: "found",
			resp: &service.FetchObjectResponse{
				ObjectRef:       "bucket:key",
				Data:            []byte("payload"),
				Checksum:        []byte{0x01, 0x02},
				Size:            42,
				Compressed:      true,
				CompressionAlgo: "zstd",
				Found:           true,
			},
			want: &transferclient.FetchObjectResponse{
				ObjectRef:       "bucket:key",
				Data:            []byte("payload"),
				Checksum:        []byte{0x01, 0x02},
				Size:            42,
				Compressed:      true,
				CompressionAlgo: transferclient.CompressionZstd,
				Found:           true,
			},
		},
		{
			name: "not found",
			resp: &service.FetchObjectResponse{
				ObjectRef:    "bucket:missing",
				Found:        false,
				ErrorMessage: "object not found",
			},
			want: &transferclient.FetchObjectResponse{
				ObjectRef:    "bucket:missing",
				Found:        false,
				ErrorMessage: "object not found",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := transferclient.NewClient(newObjectTransferTransport(t, &scriptedObjectTransfer{resp: tt.resp}))

			got, err := client.FetchObject(fetchContext(t), &transferclient.FetchObjectRequest{ShardID: 1, ObjectRef: tt.want.ObjectRef})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFetchObject_ServiceErrorIsReturned(t *testing.T) {
	client := transferclient.NewClient(newObjectTransferTransport(t, &scriptedObjectTransfer{err: &fetchFailedError{ref: "bucket:key"}}))

	got, err := client.FetchObject(fetchContext(t), &transferclient.FetchObjectRequest{ShardID: 1, ObjectRef: "bucket:key"})
	require.Error(t, err)
	assert.Nil(t, got)

	var failed *fetchFailedError
	require.ErrorAs(t, err, &failed)
	assert.Equal(t, "bucket:key", failed.ref)
}

func TestFetchObject_InvalidRequests(t *testing.T) {
	ctx := fetchContext(t)

	t.Run("service not configured", func(t *testing.T) {
		tr := newObjectTransferTransport(t, nil)
		_, err := tr.Request(ctx, "quicbaas.ObjectTransfer", "FetchObject", &transferclient.FetchObjectRequest{ShardID: 1, ObjectRef: "bucket:key"})

		var notConfigured *ServiceNotConfiguredError
		require.ErrorAs(t, err, &notConfigured)
	})

	invalid := map[string]interface{}{
		"wrong request type": "not a request",
		"nil request":        (*transferclient.FetchObjectRequest)(nil),
	}
	for name, req := range invalid {
		t.Run(name, func(t *testing.T) {
			transfer := &scriptedObjectTransfer{resp: &service.FetchObjectResponse{Found: true}}
			tr := newObjectTransferTransport(t, transfer)
			_, err := tr.Request(ctx, "quicbaas.ObjectTransfer", "FetchObject", req)

			var invalidType *InvalidRequestTypeError
			require.ErrorAs(t, err, &invalidType)
			assert.Empty(t, transfer.requests(), "an invalid request must not reach the service")
		})
	}

	t.Run("nil service response", func(t *testing.T) {
		client := transferclient.NewClient(newObjectTransferTransport(t, &scriptedObjectTransfer{}))
		got, err := client.FetchObject(ctx, &transferclient.FetchObjectRequest{ShardID: 1, ObjectRef: "bucket:key"})
		assert.Nil(t, got)

		var empty *EmptyServiceResponseError
		require.ErrorAs(t, err, &empty)
		assert.Equal(t, "FetchObject", empty.Method)
	})
}

func TestEmptyServiceResponseError_NamesTheMethod(t *testing.T) {
	err := NewEmptyServiceResponseError("quicbaas.ObjectTransfer", "FetchObject")
	assert.Equal(t, "quicbaas.ObjectTransfer", err.Service)
	assert.Contains(t, err.Error(), "quicbaas.ObjectTransfer/FetchObject")
	assert.Contains(t, err.Error(), "no response")
}
