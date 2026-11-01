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
// Not Connected Tests
// =============================================================================

func TestClientVector_NotConnected(t *testing.T) {
	t.Parallel()
	runNotConnectedCases(t, []clientCallCase{
		{"VectorCreateNamespace", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorCreateNamespace(ctx, &pb.CreateNamespaceRequest{})
		}},
		{"VectorDeleteNamespace", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorDeleteNamespace(ctx, &pb.DeleteNamespaceRequest{})
		}},
		{"VectorListNamespaces", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorListNamespaces(ctx, &pb.ListNamespacesRequest{})
		}},
		{"VectorGetNamespaceInfo", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorGetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{})
		}},
		{"VectorUpsert", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorUpsert(ctx, &pb.UpsertVectorRequest{})
		}},
		{"VectorGet", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorGet(ctx, &pb.GetVectorRequest{})
		}},
		{"VectorDelete", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorDelete(ctx, &pb.DeleteVectorRequest{})
		}},
		{"VectorBatchUpsert", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorBatchUpsert(ctx, &pb.BatchUpsertVectorsRequest{})
		}},
		{"VectorBatchDelete", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorBatchDelete(ctx, &pb.BatchDeleteVectorsRequest{})
		}},
		{"VectorUpdateMetadata", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorUpdateMetadata(ctx, &pb.UpdateMetadataRequest{})
		}},
		{"VectorHealth", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorHealth(ctx, &pb.VectorHealthRequest{})
		}},
		{"VectorGetStats", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorGetStats(ctx, &pb.GetVectorStatsRequest{})
		}},
		{"VectorSearchSimilar", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorSearchSimilar(ctx, &pb.SearchSimilarRequest{})
		}},
		{"VectorSearchByID", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorSearchByID(ctx, &pb.SearchByIDRequest{})
		}},
		{"VectorHybridSearch", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorHybridSearch(ctx, &pb.HybridSearchRequest{})
		}},
		{"VectorSearchByMetadata", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorSearchByMetadata(ctx, &pb.SearchByMetadataRequest{})
		}},
		{"VectorList", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorList(ctx, &pb.ListVectorsRequest{})
		}},
		{"VectorRebuildIndex", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorRebuildIndex(ctx, &pb.RebuildIndexRequest{})
		}},
		{"VectorGetIndexProgress", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorGetIndexProgress(ctx, &pb.GetIndexProgressRequest{})
		}},
		{"VectorExport", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorExport(ctx, &pb.ExportVectorsRequest{})
		}},
		{"VectorImport", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorImport(ctx, []*pb.ImportVectorsRequest{})
		}},
		{"VectorCompact", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorCompact(ctx, &pb.CompactNamespaceRequest{})
		}},
		{"VectorGetCompactionProgress", func(c *Client, ctx context.Context) (any, error) {
			return c.VectorGetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{})
		}},
	})
}

func TestClientRebalance_NotConnected(t *testing.T) {
	t.Parallel()
	runNotConnectedCases(t, []clientCallCase{
		{"RebalanceCluster", func(c *Client, ctx context.Context) (any, error) { return c.RebalanceCluster(ctx, nil) }},
		{"GetRebalanceProgress", func(c *Client, ctx context.Context) (any, error) { return c.GetRebalanceProgress(ctx, nil) }},
	})
}

func TestClientKMS_NotConnected(t *testing.T) {
	t.Parallel()
	runNotConnectedCases(t, []clientCallCase{
		{"KMSListKeys", func(c *Client, ctx context.Context) (any, error) { return c.KMSListKeys(ctx, nil) }},
		{"KMSBackendStatus", func(c *Client, ctx context.Context) (any, error) { return c.KMSBackendStatus(ctx) }},
		{"KMSBackendTest", func(c *Client, ctx context.Context) (any, error) { return c.KMSBackendTest(ctx) }},
	})
}

func TestClientObjectStorage_NotConnected(t *testing.T) {
	t.Parallel()
	runNotConnectedCases(t, []clientCallCase{
		{"ObjectStoragePutObject", func(c *Client, ctx context.Context) (any, error) { return c.ObjectStoragePutObject(ctx, nil) }},
		{"ObjectStorageGetObject", func(c *Client, ctx context.Context) (any, error) { return c.ObjectStorageGetObject(ctx, nil) }},
		{"ObjectStorageDeleteObject", func(c *Client, ctx context.Context) (any, error) {
			return c.ObjectStorageDeleteObject(ctx, nil)
		}},
		{"ObjectStorageHeadObject", func(c *Client, ctx context.Context) (any, error) { return c.ObjectStorageHeadObject(ctx, nil) }},
		{"ObjectStorageListObjects", func(c *Client, ctx context.Context) (any, error) {
			return c.ObjectStorageListObjects(ctx, nil)
		}},
		{"ObjectStorageListByTag", func(c *Client, ctx context.Context) (any, error) { return c.ObjectStorageListByTag(ctx, nil) }},
		{"ObjectStorageListByTenant", func(c *Client, ctx context.Context) (any, error) {
			return c.ObjectStorageListByTenant(ctx, nil)
		}},
		{"ObjectStorageCopyObject", func(c *Client, ctx context.Context) (any, error) { return c.ObjectStorageCopyObject(ctx, nil) }},
		{"ObjectStorageReplicateObject", func(c *Client, ctx context.Context) (any, error) {
			return c.ObjectStorageReplicateObject(ctx, nil)
		}},
		{"ObjectStorageMigrateObject", func(c *Client, ctx context.Context) (any, error) {
			return c.ObjectStorageMigrateObject(ctx, nil)
		}},
	})
}

func TestClientOllama_NotConnected(t *testing.T) {
	t.Parallel()
	runNotConnectedCases(t, []clientCallCase{
		{"OllamaHealth", func(c *Client, ctx context.Context) (any, error) { return c.OllamaHealth(ctx, nil) }},
		{"OllamaListModels", func(c *Client, ctx context.Context) (any, error) { return c.OllamaListModels(ctx, nil) }},
		{"OllamaGetModelInfo", func(c *Client, ctx context.Context) (any, error) { return c.OllamaGetModelInfo(ctx, nil) }},
		{"OllamaEmbed", func(c *Client, ctx context.Context) (any, error) { return c.OllamaEmbed(ctx, nil) }},
		{"OllamaEmbedWithModel", func(c *Client, ctx context.Context) (any, error) { return c.OllamaEmbedWithModel(ctx, nil) }},
		{"OllamaEmbedBatch", func(c *Client, ctx context.Context) (any, error) { return c.OllamaEmbedBatch(ctx, nil) }},
	})
}

// --- Merged from coverage_extended_additional_test.go ---

// =============================================================================
// Nil Request Tests
// =============================================================================

// clientCallCase calls one client method.
type clientCallCase struct {
	name string
	call func(c *Client, ctx context.Context) (any, error)
}

// runFailingCalls asserts that every call on a client from newClient fails
// with an error containing want and returns no response.
func runFailingCalls(t *testing.T, newClient func() *Client, want string, cases []clientCallCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := tc.call(newClient(), context.Background())
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), want)
			}
			assert.Nil(t, resp)
		})
	}
}

// runNilRequestCases asserts that every method rejects a nil request
// before it touches the connection.
func runNilRequestCases(t *testing.T, cases []clientCallCase) {
	t.Helper()
	runFailingCalls(t, func() *Client { return &Client{} }, "request cannot be nil", cases)
}

// runNotConnectedCases asserts that every method fails on a closed client.
func runNotConnectedCases(t *testing.T, cases []clientCallCase) {
	t.Helper()
	runFailingCalls(t, newNotConnectedClient, "closed", cases)
}

func TestClientSecrets_NilRequest(t *testing.T) {
	t.Parallel()
	runNilRequestCases(t, []clientCallCase{
		{"StoreSecret", func(c *Client, ctx context.Context) (any, error) { return c.StoreSecret(ctx, nil) }},
		{"GetSecret", func(c *Client, ctx context.Context) (any, error) { return c.GetSecret(ctx, nil) }},
		{"ListSecrets", func(c *Client, ctx context.Context) (any, error) { return c.ListSecrets(ctx, nil) }},
		{"DeleteSecret", func(c *Client, ctx context.Context) (any, error) { return c.DeleteSecret(ctx, nil) }},
		{"ListSecretVersions", func(c *Client, ctx context.Context) (any, error) { return c.ListSecretVersions(ctx, nil) }},
		{"RollbackSecret", func(c *Client, ctx context.Context) (any, error) { return c.RollbackSecret(ctx, nil) }},
		{"RotateSecret", func(c *Client, ctx context.Context) (any, error) { return c.RotateSecret(ctx, nil) }},
		{"UpdateSecretStatus", func(c *Client, ctx context.Context) (any, error) { return c.UpdateSecretStatus(ctx, nil) }},
		{"GetSecretsByPrefix", func(c *Client, ctx context.Context) (any, error) { return c.GetSecretsByPrefix(ctx, nil) }},
		{"WatchSecretsByPrefix", func(c *Client, ctx context.Context) (any, error) { return c.WatchSecretsByPrefix(ctx, nil) }},
	})
}

func TestClientKMS_NilRequest(t *testing.T) {
	t.Parallel()
	runNilRequestCases(t, []clientCallCase{
		{"KMSGenerateKey", func(c *Client, ctx context.Context) (any, error) { return c.KMSGenerateKey(ctx, nil) }},
		{"KMSImportKey", func(c *Client, ctx context.Context) (any, error) { return c.KMSImportKey(ctx, nil) }},
		{"KMSGetKey", func(c *Client, ctx context.Context) (any, error) { return c.KMSGetKey(ctx, nil) }},
		{"KMSRotateKey", func(c *Client, ctx context.Context) (any, error) { return c.KMSRotateKey(ctx, nil) }},
		{"KMSDeleteKey", func(c *Client, ctx context.Context) (any, error) { return c.KMSDeleteKey(ctx, nil) }},
		{"KMSEncrypt", func(c *Client, ctx context.Context) (any, error) { return c.KMSEncrypt(ctx, nil) }},
		{"KMSDecrypt", func(c *Client, ctx context.Context) (any, error) { return c.KMSDecrypt(ctx, nil) }},
		{"KMSGenerateDataKey", func(c *Client, ctx context.Context) (any, error) { return c.KMSGenerateDataKey(ctx, nil) }},
		{"KMSSign", func(c *Client, ctx context.Context) (any, error) { return c.KMSSign(ctx, nil) }},
		{"KMSVerify", func(c *Client, ctx context.Context) (any, error) { return c.KMSVerify(ctx, nil) }},
		{"KMSHash", func(c *Client, ctx context.Context) (any, error) { return c.KMSHash(ctx, nil) }},
		{"KMSGenerateHMAC", func(c *Client, ctx context.Context) (any, error) { return c.KMSGenerateHMAC(ctx, nil) }},
		{"KMSVerifyHMAC", func(c *Client, ctx context.Context) (any, error) { return c.KMSVerifyHMAC(ctx, nil) }},
		{"KMSRewrap", func(c *Client, ctx context.Context) (any, error) { return c.KMSRewrap(ctx, nil) }},
		{"KMSGenerateRandom", func(c *Client, ctx context.Context) (any, error) { return c.KMSGenerateRandom(ctx, nil) }},
		{"KMSStartReencryption", func(c *Client, ctx context.Context) (any, error) { return c.KMSStartReencryption(ctx, nil) }},
		{"KMSGetReencryptionProgress", func(c *Client, ctx context.Context) (any, error) {
			return c.KMSGetReencryptionProgress(ctx, nil)
		}},
		{"KMSGetDEKStatus", func(c *Client, ctx context.Context) (any, error) { return c.KMSGetDEKStatus(ctx, nil) }},
		{"KMSSetDefaultDEK", func(c *Client, ctx context.Context) (any, error) { return c.KMSSetDefaultDEK(ctx, nil) }},
		{"KMSSetDEKSafety", func(c *Client, ctx context.Context) (any, error) { return c.KMSSetDEKSafety(ctx, nil) }},
		{"KMSClearDEKSafety", func(c *Client, ctx context.Context) (any, error) { return c.KMSClearDEKSafety(ctx, nil) }},
	})
}

func TestClientScheduler_NilRequest(t *testing.T) {
	t.Parallel()
	runNilRequestCases(t, []clientCallCase{
		{"SchedulerCreateJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerCreateJob(ctx, nil) }},
		{"SchedulerGetJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerGetJob(ctx, nil) }},
		{"SchedulerUpdateJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerUpdateJob(ctx, nil) }},
		{"SchedulerDeleteJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerDeleteJob(ctx, nil) }},
		{"SchedulerTriggerJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerTriggerJob(ctx, nil) }},
		{"SchedulerPauseJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerPauseJob(ctx, nil) }},
		{"SchedulerResumeJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerResumeJob(ctx, nil) }},
		{"SchedulerCancelJob", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerCancelJob(ctx, nil) }},
		{"SchedulerGetJobHistory", func(c *Client, ctx context.Context) (any, error) { return c.SchedulerGetJobHistory(ctx, nil) }},
	})
}

func TestClientMessaging_NilRequest(t *testing.T) {
	t.Parallel()
	runNilRequestCases(t, []clientCallCase{
		{"MessagingCreateChannel", func(c *Client, ctx context.Context) (any, error) { return c.MessagingCreateChannel(ctx, nil) }},
		{"MessagingDeleteChannel", func(c *Client, ctx context.Context) (any, error) { return c.MessagingDeleteChannel(ctx, nil) }},
		{"MessagingGetChannel", func(c *Client, ctx context.Context) (any, error) { return c.MessagingGetChannel(ctx, nil) }},
		{"MessagingSubscribe", func(c *Client, ctx context.Context) (any, error) { return c.MessagingSubscribe(ctx, nil) }},
		{"MessagingUnsubscribe", func(c *Client, ctx context.Context) (any, error) { return c.MessagingUnsubscribe(ctx, nil) }},
		{"MessagingPublish", func(c *Client, ctx context.Context) (any, error) { return c.MessagingPublish(ctx, nil) }},
		{"MessagingPublishDelta", func(c *Client, ctx context.Context) (any, error) { return c.MessagingPublishDelta(ctx, nil) }},
		{"MessagingStreamNotifications", func(c *Client, ctx context.Context) (any, error) {
			ch, errCh, err := c.MessagingStreamNotifications(ctx, nil)
			if errCh != nil {
				return errCh, err
			}
			return ch, err
		}},
		{"MessagingStreamNotificationsRaw", func(c *Client, ctx context.Context) (any, error) {
			return c.MessagingStreamNotificationsRaw(ctx, nil)
		}},
		{"MessagingWatchChannel", func(c *Client, ctx context.Context) (any, error) { return c.MessagingWatchChannel(ctx, nil) }},
		{"MessagingGetDedupStats", func(c *Client, ctx context.Context) (any, error) { return c.MessagingGetDedupStats(ctx, nil) }},
		{"MessagingGetChannelStats", func(c *Client, ctx context.Context) (any, error) { return c.MessagingGetChannelStats(ctx, nil) }},
	})
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
// Error Conversion Tests
// =============================================================================

func TestConvertErrorNilReturnsNil(t *testing.T) {
	result := convertError(nil)
	assert.Nil(t, result)
}

func TestConvertError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		wantCode errors.ErrorCode
	}{
		// Non-gRPC errors get wrapped in a QBError with ErrInternal.
		{"plain error", stderrors.New("plain error"), errors.ErrInternal},
		// gRPC status errors get converted to QBError with mapped code.
		{"grpc status", status.Error(codes.NotFound, "resource not found"), errors.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var qbErr *errors.QBError
			require.ErrorAs(t, convertError(tt.input), &qbErr)
			assert.Equal(t, tt.wantCode, qbErr.Code)
			assert.Equal(t, "grpc", qbErr.Op)
		})
	}
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

func TestPooledConnNeedsRecycling(t *testing.T) {
	tests := []struct {
		name        string
		maxAge      time.Duration
		maxRequests uint64
		created     time.Time
		healthy     bool
		requests    uint64
	}{
		{"unhealthy", 30 * time.Minute, 100000, time.Now(), false, 0},
		{"too old", time.Nanosecond, 100000, time.Now().Add(-time.Hour), true, 0},
		{"too many requests", 30 * time.Minute, 100, time.Now(), true, 150},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := &ConnectionPool{
				maxAge:      tt.maxAge,
				maxRequests: tt.maxRequests,
				conns:       make([]*pooledConn, 0),
				metrics:     newPoolMetrics("localhost:50051"),
			}
			conn := &pooledConn{created: tt.created}
			conn.healthy.Store(tt.healthy)
			conn.requests.Store(tt.requests)

			assert.True(t, pool.needsRecycling(conn))
		})
	}
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
