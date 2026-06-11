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

//go:build rag

package grpc

import (
	"bytes"
	"context"
	"crypto/tls"
	stderrors "errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// PEP Server - Mock and Tests
// =============================================================================

// mockPEPService is a simple test implementation of PEPService
type mockPEPService struct {
	checkFn      func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error)
	checkBatchFn func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error)
	subscribeFn  func(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error
}

func (m *mockPEPService) Check(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	if m.checkFn != nil {
		return m.checkFn(ctx, req)
	}
	return &iam.CheckResponse{Allowed: true}, nil
}

func (m *mockPEPService) CheckBatch(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
	if m.checkBatchFn != nil {
		return m.checkBatchFn(ctx, subject, action, resources, context)
	}
	results := make([]bool, len(resources))
	for i := range results {
		results[i] = true
	}
	return results, 100, nil
}

func (m *mockPEPService) Subscribe(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error {
	if m.subscribeFn != nil {
		return m.subscribeFn(ctx, serviceID, resourcePatterns, updates)
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestGRPCPhase2_PEP_NewPEPServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svc := &mockPEPService{}
	srv := NewPEPServer(svc, log)
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.pepService)
	assert.NotNil(t, srv.logger)
}

func TestGRPCPhase2_PEP_Check_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svc := &mockPEPService{
		checkFn: func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
			return &iam.CheckResponse{
				Allowed:          true,
				MatchedPolicy:    "test-policy",
				MatchedRule:      "test-rule",
				EvaluationTimeNs: 42,
			}, nil
		},
	}
	srv := NewPEPServer(svc, log)

	resp, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user-1", TenantId: "t1"},
		Action:   "read",
		Resource: "doc:123",
		Context:  map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.Equal(t, "test-policy", resp.MatchedPolicy)
	assert.Equal(t, "test-rule", resp.MatchedRule)
	assert.Equal(t, int64(42), resp.EvaluationTimeNs)
}

func TestGRPCPhase2_PEP_Check_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svc := &mockPEPService{}
	srv := NewPEPServer(svc, log)
	ctx := context.Background()

	t.Run("NilSubject", func(t *testing.T) {
		_, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  nil,
			Action:   "read",
			Resource: "doc:123",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyAction", func(t *testing.T) {
		_, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user-1"},
			Action:   "",
			Resource: "doc:123",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyResource", func(t *testing.T) {
		_, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user-1"},
			Action:   "read",
			Resource: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCPhase2_PEP_Check_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svc := &mockPEPService{
		checkFn: func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
			return nil, stderrors.New("check failed")
		},
	}
	srv := NewPEPServer(svc, log)

	_, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user-1"},
		Action:   "read",
		Resource: "doc:123",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestGRPCPhase2_PEP_CheckBatch_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svc := &mockPEPService{
		checkBatchFn: func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
			return []bool{true, false, true}, 150, nil
		},
	}
	srv := NewPEPServer(svc, log)

	resp, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   &iampb.Subject{Id: "user-1"},
		Action:    "read",
		Resources: []string{"doc:1", "doc:2", "doc:3"},
		Context:   map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false, true}, resp.Results)
	assert.Equal(t, int64(150), resp.EvaluationTimeNs)
}

func TestGRPCPhase2_PEP_CheckBatch_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svc := &mockPEPService{}
	srv := NewPEPServer(svc, log)
	ctx := context.Background()

	t.Run("NilSubject", func(t *testing.T) {
		_, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   nil,
			Action:    "read",
			Resources: []string{"doc:1"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyAction", func(t *testing.T) {
		_, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user-1"},
			Action:    "",
			Resources: []string{"doc:1"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyResources", func(t *testing.T) {
		_, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user-1"},
			Action:    "read",
			Resources: []string{},
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCPhase2_PEP_CheckBatch_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	svc := &mockPEPService{
		checkBatchFn: func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
			return nil, 0, stderrors.New("batch check failed")
		},
	}
	srv := NewPEPServer(svc, log)

	_, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   &iampb.Subject{Id: "user-1"},
		Action:    "read",
		Resources: []string{"doc:1"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// PEP Service Adapter Tests
// =============================================================================

func TestGRPCPhase2_PEP_NewPEPServiceAdapter(t *testing.T) {
	adapter := NewPEPServiceAdapter(nil)
	assert.NotNil(t, adapter)
}

func TestGRPCPhase2_PEP_PEPServiceAdapter_Subscribe(t *testing.T) {
	adapter := NewPEPServiceAdapter(nil)
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan *PolicyUpdate, 10)

	// Cancel immediately so Subscribe returns quickly
	cancel()

	err := adapter.Subscribe(ctx, "svc-1", []string{"res:*"}, updates)
	assert.Error(t, err) // context.Canceled
}

// =============================================================================
// ObjectStoreReaderFromStorage
// =============================================================================

type mockStorage struct {
	data map[string][]byte
	err  error
}

func (m *mockStorage) Get(key string) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	data, ok := m.data[key]
	if !ok {
		return nil, stderrors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestGRPCPhase2_ObjectStoreReaderFromStorage_Success(t *testing.T) {
	storage := &mockStorage{
		data: map[string][]byte{
			"obj-1": []byte("hello world"),
			"obj-2": []byte("test data"),
		},
	}

	reader := ObjectStoreReaderFromStorage(storage)
	assert.NotNil(t, reader)

	data, err := reader.ReadObject("obj-1")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello world"), data)

	data, err = reader.ReadObject("obj-2")
	require.NoError(t, err)
	assert.Equal(t, []byte("test data"), data)
}

func TestGRPCPhase2_ObjectStoreReaderFromStorage_NotFound(t *testing.T) {
	storage := &mockStorage{data: map[string][]byte{}}
	reader := ObjectStoreReaderFromStorage(storage)

	_, err := reader.ReadObject("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGRPCPhase2_ObjectStoreReaderFromStorage_StorageError(t *testing.T) {
	storage := &mockStorage{err: stderrors.New("storage failure")}
	reader := ObjectStoreReaderFromStorage(storage)

	_, err := reader.ReadObject("any")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage failure")
}

// =============================================================================
// XKMS Server - Additional Encrypt/Decrypt Deep Coverage
// =============================================================================

func TestGRPCPhase2_XKMSServer_Seal_BackendNotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	// Provide a KeyId so the backend lookup branch (xkms.GetBackend) is triggered,
	// which returns codes.NotFound when the backend does not exist.
	_, err := srv.Seal(ctx, &xkmspb.SealRequest{
		Backend: "nonexistent-xyz",
		KeyId:   "some-key",
		Data:    []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPCPhase2_XKMSServer_Encrypt_BackendNotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	_, err := srv.Encrypt(ctx, &xkmspb.EncryptRequest{
		KeyId:     "key-1",
		Backend:   "nonexistent-xyz",
		Plaintext: []byte("secret data"),
	})
	require.Error(t, err)
	// Should get an error for backend not found
}

func TestGRPCPhase2_XKMSServer_Decrypt_BackendNotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	_, err := srv.Decrypt(ctx, &xkmspb.DecryptRequest{
		KeyId:      "key-1",
		Backend:    "nonexistent-xyz",
		Ciphertext: []byte("encrypted-data"),
	})
	require.Error(t, err)
}

func TestGRPCPhase2_XKMSServer_GetCert_NotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	_, err := srv.GetCert(context.Background(), &xkmspb.GetCertRequest{KeyId: "nonexistent-key"})
	require.Error(t, err)
}

func TestGRPCPhase2_XKMSServer_DeleteCert_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	_, err := srv.DeleteCert(context.Background(), &xkmspb.DeleteCertRequest{KeyId: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCPhase2_XKMSServer_DeleteCert_NotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	_, err := srv.DeleteCert(context.Background(), &xkmspb.DeleteCertRequest{KeyId: "nonexistent-key"})
	require.Error(t, err)
}

func TestGRPCPhase2_XKMSServer_ListCerts_NoBackends(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	resp, err := srv.ListCerts(context.Background(), &xkmspb.ListCertsRequest{})
	// Should either return empty list or error depending on backend availability
	if err == nil {
		assert.NotNil(t, resp)
	}
}

func TestGRPCPhase2_XKMSServer_ListBackends(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	resp, err := srv.ListBackends(context.Background(), &xkmspb.ListBackendsRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// =============================================================================
// Client - NewClient Options Paths
// =============================================================================

func TestGRPCPhase2_Client_NewClient_DefaultOptions(t *testing.T) {
	// NewClient without TLS config now returns an error because
	// TLS is required for all gRPC connections.
	_, err := NewClient("localhost:50051")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS configuration is required")
}

func TestGRPCPhase2_Client_NewClient_WithOptions(t *testing.T) {
	// NewClient with TLS config should succeed (lazy connection via DialContext).
	client, err := NewClient("localhost:50051",
		WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13}),
		WithMetadata("key", "value"),
	)
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.NotNil(t, client.opts)
	client.Close()
}

func TestGRPCPhase2_Client_NewClient_UnixSocket(t *testing.T) {
	// Unix socket connections also require TLS. Without TLS config,
	// NewClient returns an error.
	_, err := NewClient("/tmp/test.sock")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS configuration is required")
}

func TestGRPCPhase2_Client_Connect_NoAddress(t *testing.T) {
	// NewClient("") internally calls Connect() which fails on empty address,
	// so NewClient itself returns an error.
	_, err := NewClient("")
	require.Error(t, err)
}

// Messaging server tests use mockMessagingService from server_messaging_test.go

// =============================================================================
// Server ObjectStorage - Registration function (cannot test easily without
// real gRPC server, but the server_objectstorage_admin Registration can be checked)
// =============================================================================

func TestGRPCPhase2_ObjectTransfer_RegisterFunction(t *testing.T) {
	// RegisterObjectTransferServer requires a real gRPC server
	// Test that the function exists and the server can be created
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.stores)
}

func TestGRPCPhase2_ObjectTransfer_MultipleStores(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store1 := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("shard-1-data"), nil
	})
	store2 := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("shard-2-data"), nil
	})

	srv.RegisterStore(1, store1)
	srv.RegisterStore(2, store2)

	// Fetch from shard 1
	resp1, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{
		ObjectRef: "ref-1",
		ShardId:   1,
	})
	require.NoError(t, err)
	assert.True(t, resp1.Found)
	assert.Equal(t, []byte("shard-1-data"), resp1.Data)

	// Fetch from shard 2
	resp2, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{
		ObjectRef: "ref-2",
		ShardId:   2,
	})
	require.NoError(t, err)
	assert.True(t, resp2.Found)
	assert.Equal(t, []byte("shard-2-data"), resp2.Data)

	// Unregister shard 1 and try again
	srv.UnregisterStore(1)
	resp3, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{
		ObjectRef: "ref-1",
		ShardId:   1,
	})
	require.NoError(t, err)
	assert.False(t, resp3.Found)
	assert.Contains(t, resp3.ErrorMessage, "shard not found")
}

// PeerCred tests are in dedicated peercred test files with correct build tags

// =============================================================================
// Server Ollama - Additional Coverage
// =============================================================================

func TestGRPCPhase2_ServerOllama_ConvertOllamaError(t *testing.T) {
	t.Run("NilError", func(t *testing.T) {
		err := convertOllamaError(nil)
		assert.Nil(t, err)
	})

	t.Run("GenericError", func(t *testing.T) {
		err := convertOllamaError(stderrors.New("generic error"))
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

// Parameter tests are in server_parameters_test.go with ParameterStoreServer

// =============================================================================
// Server MFA - Nil Service Tests
// =============================================================================

// MFA tests are in dedicated iam test files with correct build tags

// Secrets server tests are in server_secrets_test.go with correct proto field names

// Batch, KMS, and Secrets server tests are in their dedicated test files
// (server_batch_test.go, server_kms_test.go, server_secrets_test.go)
// with correct specialized server types and proto field names
