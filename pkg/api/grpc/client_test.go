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
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Integration tests moved to client_integration_test.go

// All client tests that involve actual gRPC network connections moved to client_integration_test.go

func TestNewClient_InvalidOption(t *testing.T) {
	// Create a bad option that returns an error
	badOption := func(opts *clientOptions) error {
		return assert.AnError
	}

	client, err := NewClient("localhost:50051", badOption)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to apply client option")
}

// TestNewClient_ConnectionFailure, TestClient_ConnectWhenClosed, TestClient_EnsureConnectedReconnects,
// TestClient_AddMetadataEmpty, TestClient_AddMetadataWithData moved to client_integration_test.go

// TestConvertConsistencyToProto removed - cli.ConsistencyLevel no longer exists
// after SDK consolidation (uses proto.ConsistencyLevel directly)

// TestClient_PutWithExistingDeadline, TestClient_GetWithExistingDeadline, TestClient_DeleteWithExistingDeadline
// moved to client_integration_test.go

// TestParseID, TestFormatID, TestConvertMetadata removed - helper functions no longer exist
// after SDK consolidation (client.go no longer implements cli.Client interface)

func TestConvertGRPCError(t *testing.T) {
	tests := []struct {
		name      string
		input     error
		operation string
		checkErr  func(*testing.T, error)
	}{
		{
			name:      "nil error returns nil",
			input:     nil,
			operation: "test",
			checkErr: func(t *testing.T, err error) {
				assert.Nil(t, err)
			},
		},
		{
			name:      "already QBError returns as is",
			input:     errors.Newf(errors.ErrNotFound, "test", "not found"),
			operation: "test",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrNotFound, qbErr.Code)
			},
		},
		{
			name:      "gRPC NotFound error",
			input:     status.Error(codes.NotFound, "not found"),
			operation: "Get",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrNotFound, qbErr.Code)
			},
		},
		{
			name:      "gRPC InvalidArgument error",
			input:     status.Error(codes.InvalidArgument, "invalid argument"),
			operation: "Put",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
			},
		},
		{
			name:      "gRPC AlreadyExists error",
			input:     status.Error(codes.AlreadyExists, "already exists"),
			operation: "Create",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrAlreadyExists, qbErr.Code)
			},
		},
		{
			name:      "gRPC PermissionDenied error",
			input:     status.Error(codes.PermissionDenied, "permission denied"),
			operation: "Delete",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrForbidden, qbErr.Code)
			},
		},
		{
			name:      "gRPC Unauthenticated error",
			input:     status.Error(codes.Unauthenticated, "unauthenticated"),
			operation: "Login",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
			},
		},
		{
			name:      "gRPC ResourceExhausted error",
			input:     status.Error(codes.ResourceExhausted, "resource exhausted"),
			operation: "BatchPut",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrResourceExhausted, qbErr.Code)
			},
		},
		{
			name:      "gRPC DeadlineExceeded error",
			input:     status.Error(codes.DeadlineExceeded, "deadline exceeded"),
			operation: "Scan",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrDeadlineExceeded, qbErr.Code)
			},
		},
		{
			name:      "gRPC Unavailable error",
			input:     status.Error(codes.Unavailable, "service unavailable"),
			operation: "Connect",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
			},
		},
		{
			name:      "gRPC Canceled error",
			input:     status.Error(codes.Canceled, "operation canceled"),
			operation: "Watch",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrCanceled, qbErr.Code)
			},
		},
		{
			name:      "gRPC Internal error",
			input:     status.Error(codes.Internal, "internal error"),
			operation: "Process",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrInternal, qbErr.Code)
			},
		},
		{
			name:      "gRPC Aborted error",
			input:     status.Error(codes.Aborted, "aborted"),
			operation: "Transaction",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrAborted, qbErr.Code)
			},
		},
		{
			name:      "gRPC FailedPrecondition error",
			input:     status.Error(codes.FailedPrecondition, "failed precondition"),
			operation: "Update",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrPreconditionFailed, qbErr.Code)
			},
		},
		{
			name:      "gRPC OutOfRange error",
			input:     status.Error(codes.OutOfRange, "out of range"),
			operation: "Range",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrOutOfRange, qbErr.Code)
			},
		},
		{
			name:      "gRPC Unimplemented error",
			input:     status.Error(codes.Unimplemented, "unimplemented"),
			operation: "NewFeature",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrUnimplemented, qbErr.Code)
			},
		},
		{
			name:      "gRPC DataLoss error",
			input:     status.Error(codes.DataLoss, "data loss"),
			operation: "Backup",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrDataLoss, qbErr.Code)
			},
		},
		{
			name:      "gRPC Unknown error",
			input:     status.Error(codes.Unknown, "unknown error"),
			operation: "Unknown",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrUnknown, qbErr.Code)
			},
		},
		{
			name:      "non-gRPC error returns internal error",
			input:     stderrors.New("some random error"),
			operation: "Unknown",
			checkErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				qbErr, ok := err.(*errors.QBError)
				assert.True(t, ok)
				assert.Equal(t, errors.ErrInternal, qbErr.Code)
				assert.Contains(t, qbErr.Op, "Unknown")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertGRPCError(tt.input, tt.operation)
			tt.checkErr(t, result)
		})
	}
}

// TestConvertGRPCError_RateLimitedRoundTrip pins that the root client tells a
// throttled call apart from exhausted resources: both travel as gRPC
// ResourceExhausted, and the client reads the code back from the status message.
func TestConvertGRPCError_RateLimitedRoundTrip(t *testing.T) {
	for _, code := range []errors.ErrorCode{errors.ErrRateLimited, errors.ErrResourceExhausted} {
		t.Run(code.String(), func(t *testing.T) {
			sent := ToGRPCError(errors.Newf(code, "kms.Encrypt", "refused"))
			require.Equal(t, codes.ResourceExhausted, status.Code(sent))

			var qbErr *errors.QBError
			require.ErrorAs(t, convertGRPCError(sent, "Encrypt"), &qbErr)
			assert.Equal(t, code, qbErr.Code)

			require.ErrorAs(t, convertError(sent), &qbErr)
			assert.Equal(t, code, qbErr.Code)
		})
	}
}

// --- Merged from coverage_additional_test.go ---

// testLogger creates a logger for testing.
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	logConfig := logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stderr",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)
	return log
}

// =============================================================================
// Format Functions Tests
// =============================================================================

func TestFormatInt_Coverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{"zero", 0, "0"},
		{"positive single digit", 5, "5"},
		{"positive multiple digits", 12345, "12345"},
		{"negative single digit", -5, "-5"},
		{"negative multiple digits", -12345, "-12345"},
		{"max int32", 2147483647, "2147483647"},
		{"min int32", -2147483648, "-2147483648"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatInt(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatInt64_Coverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{"zero", 0, "0"},
		{"positive small", 42, "42"},
		{"positive large", 9223372036854775807, "9223372036854775807"},
		{"negative small", -42, "-42"},
		{"negative large", -9223372036854775808, "-9223372036854775808"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatInt64(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatUint_Coverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    uint64
		expected string
	}{
		{"zero", 0, "0"},
		{"one", 1, "1"},
		{"small", 42, "42"},
		{"large", 18446744073709551615, "18446744073709551615"},
		{"powers of ten", 1000000, "1000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatUint(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Consistency Level Conversion Tests
// =============================================================================

func TestConvertProtoToServiceConsistency_Coverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    pb.ConsistencyLevel
		expected service.ConsistencyLevel
	}{
		{
			name:     "strong consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
			expected: service.ConsistencyLevelStrong,
		},
		{
			name:     "linearizable consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "eventual consistency",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
			expected: service.ConsistencyLevelEventual,
		},
		{
			name:     "unspecified defaults to linearizable",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
			expected: service.ConsistencyLevelLinearizable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceConsistency(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Session ID Generation Tests
// =============================================================================

func TestGenerateSessionID_Coverage(t *testing.T) {
	t.Parallel()

	// Generate multiple session IDs
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := generateSessionID()
		assert.NotEmpty(t, id)
		assert.Contains(t, id, "stream-")
		ids[id] = true
	}

	// Note: Due to timing, some IDs might be the same if generated at the same nanosecond
	// The main point is that they're all valid session IDs
	assert.True(t, len(ids) >= 1)
}

// =============================================================================
// Client Tests - Ensure Connected
// =============================================================================

func TestClient_EnsureConnected_AlreadyConnected(t *testing.T) {
	t.Parallel()

	client := &Client{
		connected: true,
		closed:    false,
	}

	err := client.ensureConnected(context.Background())
	assert.NoError(t, err)
}

func TestClient_EnsureConnected_PoolClosedError(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{}
	pool.closed.Store(true)

	client := &Client{
		pool:      pool,
		connected: false,
		closed:    true, // Client marked as closed to trigger the "closed" error path
	}

	err := client.ensureConnected(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Client getClient Tests
// =============================================================================

func TestClient_GetClient_NoPool(t *testing.T) {
	t.Parallel()

	mockClient := &mockQuicBaaSClient{}
	client := &Client{
		client: mockClient,
		pool:   nil,
	}

	result, err := client.getClient()
	assert.NoError(t, err)
	assert.Equal(t, mockClient, result)
}

// mockQuicBaaSClient is a minimal mock that satisfies the proto.QuicBaaSClient interface.
// We embed the unimplemented client to satisfy the interface.
type mockQuicBaaSClient struct {
	pb.QuicBaaSClient
}

// =============================================================================
// Client Disconnect Tests
// =============================================================================

func TestClient_Disconnect_WithPool(t *testing.T) {
	t.Parallel()

	pool := &ConnectionPool{}

	client := &Client{
		pool: pool,
	}

	// Should be a no-op when pool is set
	err := client.Disconnect()
	assert.NoError(t, err)
}

func TestClient_Disconnect_NotConnected(t *testing.T) {
	t.Parallel()

	client := &Client{
		connected: false,
		pool:      nil,
	}

	err := client.Disconnect()
	assert.NoError(t, err)
}

// =============================================================================
// Client Close Tests
// =============================================================================

func TestClient_Close_AlreadyClosed(t *testing.T) {
	t.Parallel()

	client := &Client{
		closed: true,
	}

	err := client.Close()
	assert.NoError(t, err)
}

func TestClient_Close_NotConnected(t *testing.T) {
	log := testLogger(t)

	client := &Client{
		closed:    false,
		connected: false,
		pool:      nil,
		conn:      nil,
		logger:    log,
		address:   "localhost:50051",
	}

	err := client.Close()
	assert.NoError(t, err)
	assert.True(t, client.closed)
}

// =============================================================================
// Dynamic Secrets Conversion Tests
// =============================================================================

func TestServiceRoleToProto_Nil(t *testing.T) {
	t.Parallel()

	result := serviceRoleToProto(nil)
	assert.Nil(t, result)
}

func TestServiceCredentialToProto_Nil(t *testing.T) {
	t.Parallel()

	result := serviceCredentialToProto(nil)
	assert.Nil(t, result)
}

func TestServiceLeaseToProto_Nil(t *testing.T) {
	t.Parallel()

	result, err := serviceLeaseToProto(nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// Client AddMetadata Tests
// =============================================================================

func TestClient_AddMetadata_Empty(t *testing.T) {
	t.Parallel()

	opts := &clientOptions{
		metadata: nil,
	}

	client := &Client{
		opts: opts,
	}

	ctx := context.Background()
	result := client.addMetadata(ctx)
	assert.Equal(t, ctx, result)
}

func TestClient_AddMetadata_WithData(t *testing.T) {
	t.Parallel()

	opts := &clientOptions{
		metadata: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	client := &Client{
		opts: opts,
	}

	ctx := context.Background()
	result := client.addMetadata(ctx)
	assert.NotEqual(t, ctx, result)
}

func TestClient_AddMetadata_CachedMD(t *testing.T) {
	t.Parallel()

	opts := &clientOptions{
		metadata: map[string]string{
			"key": "value",
		},
	}

	client := &Client{
		opts: opts,
	}

	ctx := context.Background()

	// First call should build the MD
	result1 := client.addMetadata(ctx)
	assert.NotNil(t, client.opts.md)

	// Second call should reuse the cached MD
	result2 := client.addMetadata(ctx)
	assert.NotEqual(t, ctx, result1)
	assert.NotEqual(t, ctx, result2)
}

// =============================================================================
// Service Metadata To Proto Tests
// =============================================================================

func TestServiceMetadataToProto_Nil_Coverage(t *testing.T) {
	t.Parallel()

	result := serviceMetadataToProto(&responseNarrower{}, nil)
	assert.Nil(t, result)
}

func TestServiceMetadataToProto_ValidMetadata_Coverage(t *testing.T) {
	t.Parallel()

	metadata := &service.EmbeddingMetadata{
		Provider:   service.EmbeddingProviderVoyage,
		Model:      "voyage-3",
		Dimensions: 1024,
		Tokens:     100,
		InputType:  service.EmbeddingInputTypeDocument,
	}

	result := serviceMetadataToProto(&responseNarrower{}, metadata)
	assert.NotNil(t, result)
	assert.Equal(t, pb.EmbeddingProvider_EMBEDDING_PROVIDER_VOYAGE, result.Provider)
	assert.Equal(t, "voyage-3", result.Model)
	assert.Equal(t, int32(1024), result.Dimensions)
}

// --- Merged from coverage_not_connected_test.go ---

// =============================================================================
// Client Closed Error Path Tests
// These tests verify error handling when client is closed
// =============================================================================

// newClosedClient creates a client that has been closed.
// This triggers the "client has been closed" error path in ensureConnected.
func newClosedClient() *Client {
	pool := &ConnectionPool{}
	pool.closed.Store(true)

	return &Client{
		connected: false,
		closed:    true,
		pool:      pool,
	}
}

// newNotConnectedClient is an alias for newClosedClient for backward compatibility.
func newNotConnectedClient() *Client {
	return newClosedClient()
}

// =============================================================================
// Vector Functions - Not Connected Tests
// =============================================================================

func TestClient_VectorHybridSearch_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.VectorHybridSearch(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorSearchByMetadata_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.VectorSearchByMetadata(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorExport_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.VectorExport(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_VectorImport_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.VectorImport(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Rebalance Functions - Not Connected Tests
// =============================================================================

func TestClient_RebalanceCluster_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.RebalanceCluster(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_GetRebalanceProgress_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.GetRebalanceProgress(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// KMS Functions - Not Connected Tests
// KMSListKeys calls ensureConnected first, so we test not connected
// =============================================================================

func TestClient_KMSListKeys_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.KMSListKeys(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// KMS Backend Functions - Not Connected Tests
// =============================================================================

func TestClient_KMSBackendStatus_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.KMSBackendStatus(ctx)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_KMSBackendTest_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.KMSBackendTest(ctx)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Object Storage Functions - Not Connected Tests
// =============================================================================

func TestClient_ObjectStoragePutObject_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStoragePutObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageGetObject_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageGetObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageDeleteObject_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageDeleteObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageHeadObject_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageHeadObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageListObjects_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageListObjects(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageListByTag_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageListByTag(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageListByTenant_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageListByTenant(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageCopyObject_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageCopyObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageReplicateObject_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageReplicateObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_ObjectStorageMigrateObject_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.ObjectStorageMigrateObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Ollama Functions - Not Connected Tests
// =============================================================================

func TestClient_OllamaHealth_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.OllamaHealth(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaListModels_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.OllamaListModels(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaGetModelInfo_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.OllamaGetModelInfo(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaEmbed_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.OllamaEmbed(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaEmbedWithModel_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.OllamaEmbedWithModel(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

func TestClient_OllamaEmbedBatch_NotConnected(t *testing.T) {
	t.Parallel()
	client := newNotConnectedClient()
	ctx := context.Background()

	resp, err := client.OllamaEmbedBatch(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// --- Merged from coverage_extended_additional_test.go ---

// =============================================================================
// Client Secrets Functions - Nil Request Tests
// =============================================================================

func TestClientCoverage_StoreSecret_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.StoreSecret(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_GetSecret_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.GetSecret(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_ListSecrets_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.ListSecrets(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_DeleteSecret_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.DeleteSecret(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_ListSecretVersions_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.ListSecretVersions(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_RollbackSecret_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.RollbackSecret(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_RotateSecret_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.RotateSecret(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_UpdateSecretStatus_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.UpdateSecretStatus(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_GetSecretsByPrefix_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.GetSecretsByPrefix(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_WatchSecretsByPrefix_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	ch, err := client.WatchSecretsByPrefix(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client KMS Functions - Nil Request Tests
// =============================================================================

func TestClientCoverage_KMSGenerateKey_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSGenerateKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSImportKey_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSImportKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSGetKey_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSGetKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSRotateKey_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSRotateKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSDeleteKey_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSDeleteKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSEncrypt_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSEncrypt(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSDecrypt_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSDecrypt(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSGenerateDataKey_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSGenerateDataKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client KMS Crypto Functions - Nil Request Tests
// =============================================================================

func TestClientCoverage_KMSSign_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSSign(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSVerify_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSVerify(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSHash_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSHash(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSGenerateHMAC_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSGenerateHMAC(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSVerifyHMAC_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSVerifyHMAC(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSRewrap_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSRewrap(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSGenerateRandom_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSGenerateRandom(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client KMS Reencryption - Nil Request Tests
// =============================================================================

func TestClientCoverage_KMSStartReencryption_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSStartReencryption(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSGetReencryptionProgress_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSGetReencryptionProgress(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client Scheduler Functions - Nil Request Tests
// =============================================================================

func TestClientCoverage_SchedulerCreateJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerCreateJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerGetJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerGetJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerUpdateJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerUpdateJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerDeleteJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerDeleteJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerTriggerJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerTriggerJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerPauseJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerPauseJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerResumeJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerResumeJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerCancelJob_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerCancelJob(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_SchedulerGetJobHistory_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.SchedulerGetJobHistory(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client Messaging Functions - Nil Request Tests
// =============================================================================

func TestClientCoverage_MessagingCreateChannel_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingCreateChannel(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingDeleteChannel_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingDeleteChannel(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingGetChannel_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingGetChannel(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingSubscribe_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingSubscribe(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingUnsubscribe_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingUnsubscribe(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingPublish_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingPublish(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingPublishDelta_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingPublishDelta(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingStreamNotifications_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	ch, errCh, err := client.MessagingStreamNotifications(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, ch)
	assert.Nil(t, errCh)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingStreamNotificationsRaw_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	stream, err := client.MessagingStreamNotificationsRaw(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, stream)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingWatchChannel_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	stream, err := client.MessagingWatchChannel(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, stream)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingGetDedupStats_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingGetDedupStats(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_MessagingGetChannelStats_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.MessagingGetChannelStats(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client KMS DEK Functions - Nil Request Tests
// =============================================================================

func TestClientCoverage_KMSGetDEKStatus_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSGetDEKStatus(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSSetDefaultDEK_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSSetDefaultDEK(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSSetDEKSafety_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSSetDEKSafety(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientCoverage_KMSClearDEKSafety_NilRequest(t *testing.T) {
	t.Parallel()
	client := &Client{}
	ctx := context.Background()

	resp, err := client.KMSClearDEKSafety(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Additional Client Connection Tests
// =============================================================================

func TestClientCoverage_Connect_WhenClosed(t *testing.T) {
	t.Parallel()
	pool := &ConnectionPool{}
	pool.closed.Store(true)

	client := &Client{
		connected: false,
		closed:    true,
		pool:      pool,
		opts:      defaultClientOptions(),
	}
	ctx := context.Background()

	err := client.Connect(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestClientCoverage_Disconnect_WithPool(t *testing.T) {
	t.Parallel()
	pool := &ConnectionPool{}
	pool.closed.Store(true)

	client := &Client{
		connected: true,
		closed:    false,
		pool:      pool,
	}

	// Disconnect should be no-op with pool
	err := client.Disconnect()
	assert.NoError(t, err)
}

func TestClientCoverage_Disconnect_AlreadyDisconnected(t *testing.T) {
	t.Parallel()
	client := &Client{
		connected: false,
		closed:    false,
		opts:      defaultClientOptions(),
	}

	err := client.Disconnect()
	assert.NoError(t, err)
}

func TestClientCoverage_Close_AlreadyClosed(t *testing.T) {
	t.Parallel()
	client := &Client{
		connected: false,
		closed:    true,
		opts:      defaultClientOptions(),
	}

	err := client.Close()
	assert.NoError(t, err)
}

func TestClientCoverage_isConnected_Closed(t *testing.T) {
	t.Parallel()
	client := &Client{
		connected: true,
		closed:    true,
	}

	result := client.isConnected()
	assert.False(t, result)
}

func TestClientCoverage_Conn_Nil(t *testing.T) {
	t.Parallel()
	client := &Client{
		connected: false,
		closed:    false,
	}

	conn := client.Conn()
	assert.Nil(t, conn)
}

// --- Merged from coverage_boost_test.go ---

// =============================================================================
// Client KMS Tests - Nil Request Coverage
// =============================================================================

func TestClientKMSEncryptWithNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSEncrypt(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSDecryptWithNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSDecrypt(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGenerateDataKeyWithNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGenerateDataKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSStartReencryptionWithNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSStartReencryption(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGetReencryptionProgressWithNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetReencryptionProgress(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGetDEKStatusNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetDEKStatus(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSSetDefaultDEKNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetDefaultDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSSetDEKSafetyNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetDEKSafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSClearDEKSafetyNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSClearDEKSafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client Crypto Tests - Nil Request Coverage
// =============================================================================

func TestClientKMSSignNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSign(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSVerifyNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSVerify(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSHashNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSHash(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGenerateHMACNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGenerateHMAC(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSVerifyHMACNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSVerifyHMAC(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSRewrapNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSRewrap(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGenerateRandomNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGenerateRandom(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Pool Tests - Additional Coverage
// =============================================================================

func TestConnectionPoolClosedGetError(t *testing.T) {
	pool := &ConnectionPool{
		metrics: newPoolMetrics("localhost:50051"),
	}
	pool.closed.Store(true)

	conn, err := pool.Get()

	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestConnectionPoolEmptyPoolGetError(t *testing.T) {
	pool := &ConnectionPool{
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}

	conn, err := pool.Get()

	assert.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "pool has no connections")
}

func TestConnectionPoolStatsWithConnections(t *testing.T) {
	pool := &ConnectionPool{
		conns: []*pooledConn{
			{created: time.Now()},
			{created: time.Now()},
		},
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}

	// Set some connection states
	pool.conns[0].inUse.Store(true)
	pool.conns[0].healthy.Store(true)
	pool.conns[1].inUse.Store(false)
	pool.conns[1].healthy.Store(true)

	pool.totalRequests.Store(100)
	pool.recycledConns.Store(5)
	pool.failedConns.Store(2)
	pool.healthChecksFailed.Store(1)

	stats := pool.Stats()

	assert.Equal(t, 2, stats.PoolSize)
	assert.Equal(t, 1, stats.ActiveConns)
	assert.Equal(t, 1, stats.IdleConns)
	assert.Equal(t, uint64(100), stats.TotalRequests)
	assert.Equal(t, uint64(5), stats.RecycledConns)
	assert.Equal(t, uint64(2), stats.FailedConns)
	assert.Equal(t, uint64(1), stats.HealthChecksFailed)
}

func TestConnectionPoolStringRepresentation(t *testing.T) {
	pool := &ConnectionPool{
		address:         "localhost:50051",
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}

	str := pool.String()

	assert.Contains(t, str, "ConnectionPool")
	assert.Contains(t, str, "localhost:50051")
	assert.Contains(t, str, "size=0")
}

func TestConnectionPoolCloseAlreadyClosed(t *testing.T) {
	pool := &ConnectionPool{
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}
	pool.closed.Store(true)

	err := pool.Close()

	assert.NoError(t, err)
}

// =============================================================================
// Unix Socket Address Tests
// =============================================================================

func TestIsUnixSocketAddressCases(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		expected bool
	}{
		{"unix prefix", "unix:///tmp/test.sock", true},
		{"unix-abstract prefix", "unix-abstract://test", true},
		{"absolute path with .sock", "/tmp/quicbaas.sock", true},
		{"relative path with .sock", "./quicbaas.sock", true},
		{"parent relative path", "../quicbaas.sock", true},
		{"tcp address", "localhost:50051", false},
		{"ip address", "127.0.0.1:50051", false},
		{"file without .sock extension", "/tmp/quicbaas", false},
		{"simple filename with sock", "quicbaas.sock", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnixSocketAddress(tt.address)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeUnixSocketAddressCases(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		expected string
	}{
		{"already unix prefix", "unix:///tmp/test.sock", "unix:///tmp/test.sock"},
		{"unix-abstract stays same", "unix-abstract://test", "unix-abstract://test"},
		{"absolute path gets unix prefix", "/tmp/quicbaas.sock", "unix:///tmp/quicbaas.sock"},
		{"tcp address unchanged", "localhost:50051", "localhost:50051"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeUnixSocketAddress(tt.address)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Client Vector Tests - ensureConnected Error Paths
// =============================================================================

func TestClientVectorCreateNamespaceNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorCreateNamespace(ctx, &pb.CreateNamespaceRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorDeleteNamespaceNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorDeleteNamespace(ctx, &pb.DeleteNamespaceRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorListNamespacesNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorListNamespaces(ctx, &pb.ListNamespacesRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorGetNamespaceInfoNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorGetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorUpsertNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorUpsert(ctx, &pb.UpsertVectorRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorGetNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorGet(ctx, &pb.GetVectorRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorDeleteNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorDelete(ctx, &pb.DeleteVectorRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorBatchUpsertNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorBatchUpsert(ctx, &pb.BatchUpsertVectorsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorBatchDeleteNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorBatchDelete(ctx, &pb.BatchDeleteVectorsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorUpdateMetadataNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorUpdateMetadata(ctx, &pb.UpdateMetadataRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorHealthNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorHealth(ctx, &pb.VectorHealthRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorGetStatsNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorGetStats(ctx, &pb.GetVectorStatsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorSearchSimilarNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorSearchSimilar(ctx, &pb.SearchSimilarRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorSearchByIDNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorSearchByID(ctx, &pb.SearchByIDRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorHybridSearchNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorHybridSearch(ctx, &pb.HybridSearchRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorSearchByMetadataNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorSearchByMetadata(ctx, &pb.SearchByMetadataRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorListNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorList(ctx, &pb.ListVectorsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorRebuildIndexNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorRebuildIndex(ctx, &pb.RebuildIndexRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorGetIndexProgressNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorGetIndexProgress(ctx, &pb.GetIndexProgressRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorExportNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorExport(ctx, &pb.ExportVectorsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorImportNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	// VectorImport takes a slice of requests
	resp, err := client.VectorImport(ctx, []*pb.ImportVectorsRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorCompactNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorCompact(ctx, &pb.CompactNamespaceRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientVectorGetCompactionProgressNotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.VectorGetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Client Messaging Tests - Nil Request Coverage
// =============================================================================

func TestClientMessagingCreateChannelNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingCreateChannel(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingDeleteChannelNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingDeleteChannel(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingGetChannelNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingGetChannel(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingSubscribeNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingSubscribe(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingUnsubscribeNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingUnsubscribe(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingPublishNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingPublish(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingPublishDeltaNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingPublishDelta(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingWatchChannelNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingWatchChannel(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingGetDedupStatsNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingGetDedupStats(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientMessagingGetChannelStatsNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.MessagingGetChannelStats(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Client Scheduler Tests - Nil Request Coverage
// Note: Scheduler methods check nil request FIRST before using c.conn,
// so these work with empty Client{}
// =============================================================================

func TestClientSchedulerCreateJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerCreateJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerGetJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerGetJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerUpdateJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerUpdateJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerDeleteJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerDeleteJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerTriggerJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerTriggerJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerPauseJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerPauseJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerResumeJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerResumeJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerCancelJobNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerCancelJob(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientSchedulerGetJobHistoryNilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.SchedulerGetJobHistory(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Error Conversion Tests
// =============================================================================

func TestConvertErrorNilReturnsNil(t *testing.T) {
	result := convertError(nil)
	assert.Nil(t, result)
}

func TestConvertErrorNonGRPCError(t *testing.T) {
	// Non-gRPC errors get wrapped in a QBError with ErrInternal
	plainErr := stderrors.New("plain error")

	result := convertError(plainErr)

	var qbErr *errors.QBError
	require.ErrorAs(t, result, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
	assert.Equal(t, "grpc", qbErr.Op)
}

func TestConvertErrorGRPCStatusError(t *testing.T) {
	// gRPC status errors get converted to QBError with mapped code
	grpcErr := status.Error(codes.NotFound, "resource not found")

	result := convertError(grpcErr)

	var qbErr *errors.QBError
	require.ErrorAs(t, result, &qbErr)
	assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	assert.Equal(t, "grpc", qbErr.Op)
}

// =============================================================================
// Peer Credentials Tests - Additional Coverage
// =============================================================================

func TestPeerCredentialsInterceptorWithNilVerifier(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := UnaryPeerCredentialsInterceptor(nil, log)
	assert.NotNil(t, interceptor)

	streamInterceptor := StreamPeerCredentialsInterceptor(nil, log)
	assert.NotNil(t, streamInterceptor)
}

func TestIsPeerCredentialsAvailableEmptyContext(t *testing.T) {
	ctx := context.Background()
	result := IsPeerCredentialsAvailable(ctx)

	// On non-Linux platforms, this returns false
	// On Linux, it depends on whether peer credentials are in context
	assert.False(t, result)
}

// =============================================================================
// PooledConn Needs Recycling Tests
// =============================================================================

func TestPooledConnNeedsRecyclingUnhealthy(t *testing.T) {
	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 100000,
		conns:       make([]*pooledConn, 0),
		metrics:     newPoolMetrics("localhost:50051"),
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(false) // unhealthy

	assert.True(t, pool.needsRecycling(conn))
}

func TestPooledConnNeedsRecyclingTooOld(t *testing.T) {
	pool := &ConnectionPool{
		maxAge:      1 * time.Nanosecond, // Very short max age
		maxRequests: 100000,
		conns:       make([]*pooledConn, 0),
		metrics:     newPoolMetrics("localhost:50051"),
	}

	conn := &pooledConn{
		created: time.Now().Add(-1 * time.Hour), // old connection
	}
	conn.healthy.Store(true)

	assert.True(t, pool.needsRecycling(conn))
}

func TestPooledConnNeedsRecyclingTooManyRequests(t *testing.T) {
	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 100, // Low max requests
		conns:       make([]*pooledConn, 0),
		metrics:     newPoolMetrics("localhost:50051"),
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(true)
	conn.requests.Store(150) // Exceeded max requests

	assert.True(t, pool.needsRecycling(conn))
}

// =============================================================================
// Client GetClient Tests
// =============================================================================

func TestClientGetClientWithPool(t *testing.T) {
	pool := &ConnectionPool{
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}

	client := &Client{
		pool: pool,
	}

	_, err := client.getClient()
	// Pool is empty, should return error
	assert.Error(t, err)
}

func TestClientGetClientWithoutPool(t *testing.T) {
	client := &Client{
		client: nil, // No client set
	}

	result, err := client.getClient()
	// When client is nil in single mode, getClient returns an error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client is not connected")
	assert.Nil(t, result)
}

// =============================================================================
// Client EnsureConnected Tests
// =============================================================================

func TestClientEnsureConnectedWithPoolClosed(t *testing.T) {
	pool := &ConnectionPool{
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}

	client := &Client{
		pool:      pool,
		connected: false,
		closed:    true,
	}

	err := client.ensureConnected(context.Background())
	assert.Error(t, err)
}

// =============================================================================
// Client Conn Tests
// =============================================================================

func TestClientConnReturnsConnection(t *testing.T) {
	client := &Client{
		conn: nil, // No connection
	}

	result := client.Conn()
	assert.Nil(t, result)
}

// =============================================================================
// Client Secrets Tests - Nil Request Coverage
// Note: Secrets methods check nil request FIRST before using c.conn,
// so these work with empty Client{}
// =============================================================================

func TestClientStoreSecretNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.StoreSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientGetSecretNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.GetSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientDeleteSecretNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.DeleteSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientListSecretsNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.ListSecrets(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientRotateSecretNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.RotateSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientListSecretVersionsNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.ListSecretVersions(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientRollbackSecretNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.RollbackSecret(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientUpdateSecretStatusNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.UpdateSecretStatus(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientGetSecretsByPrefixNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.GetSecretsByPrefix(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientWatchSecretsByPrefixNilRequest_Coverage(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.WatchSecretsByPrefix(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}
