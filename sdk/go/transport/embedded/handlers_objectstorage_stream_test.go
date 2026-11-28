// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"io"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	objclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// newObjectStorageStreamClient returns the real SDK object storage client over
// an embedded transport backed by objSvc.
func newObjectStorageStreamClient(t *testing.T, objSvc service.ObjectStorageService) objclient.Client {
	t.Helper()
	tr, err := New(&mockQuicBaaS{objectStorageService: objSvc})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	return objclient.NewClient(tr)
}

// readFullWithin reads exactly len(buf) bytes from r, or fails when ctx ends
// first.
func readFullWithin(ctx context.Context, t *testing.T, r io.Reader, buf []byte) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(r, buf)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatal("stream did not deliver the object before the context ended")
		return ctx.Err()
	}
}

// TestStreamGetObject_DeliversObjectInChunks downloads an object larger than
// one 64 KiB chunk through the SDK client and checks every byte arrives, with
// the size the service reported.
func TestStreamGetObject_DeliversObjectInChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 10*1024) // 160 KiB, three chunks
	var gotReq *service.GetObjectRequest
	objSvc := &mockObjectStorageService{
		getObjectFunc: func(_ context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			gotReq = req
			return &service.GetObjectResponse{Key: req.Key, Value: payload, Size: int64(len(payload))}, nil
		},
	}
	client := newObjectStorageStreamClient(t, objSvc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reader, err := client.StreamGetObject(ctx, &objclient.GetObjectRequest{Key: "docs/big", TenantID: "acme"})
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	meta, err := reader.ReadMetadata()
	require.NoError(t, err)
	require.Equal(t, int64(len(payload)), meta.Size)

	got := make([]byte, len(payload))
	require.NoError(t, readFullWithin(ctx, t, reader, got))
	require.Equal(t, payload, got)
	require.Equal(t, "docs/big", gotReq.Key)
	require.Equal(t, "acme", gotReq.TenantID)
}

// TestStreamGetObject_MissingObjectIsNotFound checks a service not-found
// reaches the reader as a not-found error, not as an unreadable chunk.
func TestStreamGetObject_MissingObjectIsNotFound(t *testing.T) {
	objSvc := &mockObjectStorageService{
		getObjectFunc: func(_ context.Context, _ *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, api.NewNotFoundWithCauseError("objectstorage.GetObject", "object", fs.ErrNotExist)
		},
	}
	client := newObjectStorageStreamClient(t, objSvc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reader, err := client.StreamGetObject(ctx, &objclient.GetObjectRequest{Key: "docs/missing"})
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	err = readFullWithin(ctx, t, reader, make([]byte, 1))
	require.Error(t, err)
	require.True(t, errors.IsNotFound(err), "want not-found, got %v", err)
}

// TestStreamGetObject_EmptyObjectSendsOneLastChunk mirrors the gRPC server: an
// empty object is one empty chunk marked last.
func TestStreamGetObject_EmptyObjectSendsOneLastChunk(t *testing.T) {
	objSvc := &mockObjectStorageService{
		getObjectFunc: func(_ context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return &service.GetObjectResponse{Key: req.Key}, nil
		},
	}
	tr, err := New(&mockQuicBaaS{objectStorageService: objSvc})
	require.NoError(t, err)
	defer func() { require.NoError(t, tr.Close()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := tr.RequestStream(ctx, "quicbaas.ObjectStorage", "StreamGetObject", &objclient.GetObjectRequest{Key: "docs/empty"})
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	msg, err := s.Recv()
	require.NoError(t, err)
	chunk, ok := msg.(*objclient.ObjectChunk)
	require.True(t, ok, "want *objectstorage.ObjectChunk, got %T", msg)
	require.True(t, chunk.IsLast)
	require.Empty(t, chunk.Data)
	require.Zero(t, chunk.TotalSize)
}

// TestStreamGetObject_RejectsBadRequests covers the wrong request type, a
// typed-nil request, and an unconfigured object storage service.
func TestStreamGetObject_RejectsBadRequests(t *testing.T) {
	ctx := inProcessTenant(context.Background())
	svc := &mockQuicBaaS{objectStorageService: &mockObjectStorageService{}}

	var typeErr *InvalidRequestTypeError
	err := handleObjStreamGetObject(ctx, svc, "not-a-request", newStream(ctx, 1))
	require.ErrorAs(t, err, &typeErr)

	err = handleObjStreamGetObject(ctx, svc, (*objclient.GetObjectRequest)(nil), newStream(ctx, 1))
	require.ErrorAs(t, err, &typeErr)

	var notConfigured *ServiceNotConfiguredError
	err = handleObjStreamGetObject(ctx, &mockQuicBaaS{}, &objclient.GetObjectRequest{Key: "k"}, newStream(ctx, 1))
	require.ErrorAs(t, err, &notConfigured)
}
