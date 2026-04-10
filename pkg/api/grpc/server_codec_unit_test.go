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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// extractCodecPreference Unit Tests
// =============================================================================

func TestExtractCodecPreference_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		expected string
	}{
		{
			name:     "no_metadata_returns_empty",
			metadata: nil,
			expected: "",
		},
		{
			name:     "empty_metadata_returns_empty",
			metadata: map[string]string{},
			expected: "",
		},
		{
			name:     "json_codec",
			metadata: map[string]string{"x-quicbaas-codec": "json"},
			expected: "json",
		},
		{
			name:     "yaml_codec",
			metadata: map[string]string{"x-quicbaas-codec": "yaml"},
			expected: "yaml",
		},
		{
			name:     "msgpack_codec",
			metadata: map[string]string{"x-quicbaas-codec": "msgpack"},
			expected: "msgpack",
		},
		{
			name:     "invalid_codec_returns_empty",
			metadata: map[string]string{"x-quicbaas-codec": "invalid"},
			expected: "",
		},
		{
			name:     "unknown_codec_returns_empty",
			metadata: map[string]string{"x-quicbaas-codec": "protobuf"},
			expected: "",
		},
		{
			name:     "missing_codec_header_returns_empty",
			metadata: map[string]string{"x-other-header": "value"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.metadata != nil {
				md := metadata.New(tt.metadata)
				ctx = metadata.NewIncomingContext(context.Background(), md)
			} else {
				ctx = context.Background()
			}

			result := extractCodecPreference(ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// GetOptimized Unit Tests
// =============================================================================

func TestServer_GetOptimized_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Get", mock.Anything, mock.Anything).
		Return(&service.GetResponse{
			ID:        1,
			Found:     true,
			Data:      []byte(`{"name":"test"}`),
			Version:   1,
			Timestamp: 1234567890,
		}, nil).Once()

	resp, err := srv.GetOptimized(context.Background(), &pb.GetRequest{
		EntityType: "users",
		Id:         1,
	})

	require.NoError(t, err)
	assert.True(t, resp.Found)
	assert.Equal(t, uint64(1), resp.Id)
	mockSvc.AssertExpectations(t)
}

func TestServer_GetOptimized_WithCodecPreference(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Get", mock.Anything, mock.Anything).
		Return(&service.GetResponse{
			ID:        1,
			Found:     true,
			Data:      []byte(`{"name":"test"}`),
			Version:   1,
			Timestamp: 1234567890,
		}, nil).Once()

	// Create context with codec preference
	md := metadata.New(map[string]string{"x-quicbaas-codec": "json"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := srv.GetOptimized(ctx, &pb.GetRequest{
		EntityType: "users",
		Id:         1,
	})

	require.NoError(t, err)
	assert.True(t, resp.Found)
	mockSvc.AssertExpectations(t)
}

func TestServer_GetOptimized_NotFound(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Get", mock.Anything, mock.Anything).
		Return(&service.GetResponse{
			ID:    999,
			Found: false,
		}, nil).Once()

	_, err := srv.GetOptimized(context.Background(), &pb.GetRequest{
		EntityType: "users",
		Id:         999,
	})

	// Server returns NotFound error when entity is not found
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	mockSvc.AssertExpectations(t)
}

func TestServer_GetOptimized_Error(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Get", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.GetOptimized(context.Background(), &pb.GetRequest{
		EntityType: "users",
		Id:         1,
	})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestServer_GetOptimized_WithAllCodecTypes(t *testing.T) {
	codecs := []string{"json", "yaml", "msgpack"}

	for _, codec := range codecs {
		t.Run("codec_"+codec, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)

			mockSvc.On("Get", mock.Anything, mock.Anything).
				Return(&service.GetResponse{
					ID:        1,
					Found:     true,
					Data:      []byte(`{"name":"test"}`),
					Version:   1,
					Timestamp: 1234567890,
				}, nil).Once()

			md := metadata.New(map[string]string{"x-quicbaas-codec": codec})
			ctx := metadata.NewIncomingContext(context.Background(), md)

			resp, err := srv.GetOptimized(ctx, &pb.GetRequest{
				EntityType: "users",
				Id:         1,
			})

			require.NoError(t, err)
			assert.True(t, resp.Found)
			mockSvc.AssertExpectations(t)
		})
	}
}
