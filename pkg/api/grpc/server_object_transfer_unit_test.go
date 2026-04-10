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

package grpc

import (
	"context"
	"crypto/sha256"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newTestObjectTransferServer(t *testing.T) *objectTransferServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
	})
	require.NoError(t, err)
	return NewObjectTransferServer(log)
}

// mockObjectStore is a test implementation of ObjectStoreReader.
type mockObjectStore struct {
	readFunc func(objectRef string) ([]byte, error)
}

func (m *mockObjectStore) ReadObject(objectRef string) ([]byte, error) {
	if m.readFunc != nil {
		return m.readFunc(objectRef)
	}
	return nil, stderrors.New("not found")
}

func TestObjectTransferServer_RegisterUnregisterStore(t *testing.T) {
	srv := newTestObjectTransferServer(t)

	store := &mockObjectStore{}
	srv.RegisterStore(1, store)
	assert.Len(t, srv.stores, 1)

	srv.UnregisterStore(1)
	assert.Len(t, srv.stores, 0)
}

func TestObjectTransferServer_FetchObject(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(srv *objectTransferServer)
		request  *pb.FetchObjectRequest
		validate func(t *testing.T, resp *pb.FetchObjectResponse, err error)
	}{
		{
			name:    "nil_request",
			setup:   func(srv *objectTransferServer) {},
			request: nil,
			validate: func(t *testing.T, resp *pb.FetchObjectResponse, err error) {
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
			},
		},
		{
			name:  "empty_object_ref",
			setup: func(srv *objectTransferServer) {},
			request: &pb.FetchObjectRequest{
				ObjectRef: "",
			},
			validate: func(t *testing.T, resp *pb.FetchObjectResponse, err error) {
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
			},
		},
		{
			name:  "shard_not_found",
			setup: func(srv *objectTransferServer) {},
			request: &pb.FetchObjectRequest{
				ObjectRef: "obj-123",
				ShardId:   99,
			},
			validate: func(t *testing.T, resp *pb.FetchObjectResponse, err error) {
				require.NoError(t, err)
				assert.False(t, resp.Found)
				assert.Contains(t, resp.ErrorMessage, "shard not found")
			},
		},
		{
			name: "object_not_found_in_store",
			setup: func(srv *objectTransferServer) {
				srv.RegisterStore(1, &mockObjectStore{
					readFunc: func(objectRef string) ([]byte, error) {
						return nil, stderrors.New("object not found")
					},
				})
			},
			request: &pb.FetchObjectRequest{
				ObjectRef: "obj-123",
				ShardId:   1,
			},
			validate: func(t *testing.T, resp *pb.FetchObjectResponse, err error) {
				require.NoError(t, err)
				assert.False(t, resp.Found)
				assert.Contains(t, resp.ErrorMessage, "object not found")
			},
		},
		{
			name: "successful_fetch",
			setup: func(srv *objectTransferServer) {
				srv.RegisterStore(1, &mockObjectStore{
					readFunc: func(objectRef string) ([]byte, error) {
						return []byte("hello world"), nil
					},
				})
			},
			request: &pb.FetchObjectRequest{
				ObjectRef: "obj-123",
				ShardId:   1,
			},
			validate: func(t *testing.T, resp *pb.FetchObjectResponse, err error) {
				require.NoError(t, err)
				assert.True(t, resp.Found)
				assert.Equal(t, []byte("hello world"), resp.Data)
				assert.Equal(t, int64(11), resp.Size)
				// Verify checksum
				hash := sha256.Sum256([]byte("hello world"))
				assert.Equal(t, hash[:], resp.Checksum)
			},
		},
		{
			name: "checksum_mismatch",
			setup: func(srv *objectTransferServer) {
				srv.RegisterStore(1, &mockObjectStore{
					readFunc: func(objectRef string) ([]byte, error) {
						return []byte("hello world"), nil
					},
				})
			},
			request: &pb.FetchObjectRequest{
				ObjectRef: "obj-123",
				ShardId:   1,
				Checksum:  []byte("bad-checksum"),
			},
			validate: func(t *testing.T, resp *pb.FetchObjectResponse, err error) {
				require.NoError(t, err)
				assert.True(t, resp.Found)
				assert.Contains(t, resp.ErrorMessage, "checksum mismatch")
			},
		},
		{
			name: "checksum_matches",
			setup: func(srv *objectTransferServer) {
				srv.RegisterStore(1, &mockObjectStore{
					readFunc: func(objectRef string) ([]byte, error) {
						return []byte("hello"), nil
					},
				})
			},
			request: func() *pb.FetchObjectRequest {
				hash := sha256.Sum256([]byte("hello"))
				return &pb.FetchObjectRequest{
					ObjectRef: "obj-123",
					ShardId:   1,
					Checksum:  hash[:],
				}
			}(),
			validate: func(t *testing.T, resp *pb.FetchObjectResponse, err error) {
				require.NoError(t, err)
				assert.True(t, resp.Found)
				assert.Equal(t, []byte("hello"), resp.Data)
				assert.Empty(t, resp.ErrorMessage)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestObjectTransferServer(t)
			tt.setup(srv)
			resp, err := srv.FetchObject(context.Background(), tt.request)
			tt.validate(t, resp, err)
		})
	}
}

func TestFetchSingleObject(t *testing.T) {
	srv := newTestObjectTransferServer(t)

	t.Run("object_found", func(t *testing.T) {
		store := &mockObjectStore{
			readFunc: func(objectRef string) ([]byte, error) {
				return []byte("data"), nil
			},
		}
		item := &pb.ObjectFetchItem{ObjectRef: "obj-1"}
		resp := srv.fetchSingleObject(store, item)
		assert.True(t, resp.Found)
		assert.Equal(t, []byte("data"), resp.Data)
	})

	t.Run("object_not_found", func(t *testing.T) {
		store := &mockObjectStore{
			readFunc: func(objectRef string) ([]byte, error) {
				return nil, stderrors.New("not found")
			},
		}
		item := &pb.ObjectFetchItem{ObjectRef: "obj-missing"}
		resp := srv.fetchSingleObject(store, item)
		assert.False(t, resp.Found)
		assert.Contains(t, resp.ErrorMessage, "not found")
	})

	t.Run("checksum_mismatch", func(t *testing.T) {
		store := &mockObjectStore{
			readFunc: func(objectRef string) ([]byte, error) {
				return []byte("data"), nil
			},
		}
		item := &pb.ObjectFetchItem{
			ObjectRef: "obj-1",
			Checksum:  []byte("wrong"),
		}
		resp := srv.fetchSingleObject(store, item)
		assert.True(t, resp.Found)
		assert.Contains(t, resp.ErrorMessage, "checksum mismatch")
	})
}

func TestObjectStoreReaderFunc(t *testing.T) {
	called := false
	fn := ObjectStoreReaderFunc(func(objectRef string) ([]byte, error) {
		called = true
		return []byte("hello"), nil
	})

	data, err := fn.ReadObject("test-ref")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), data)
	assert.True(t, called)
}
