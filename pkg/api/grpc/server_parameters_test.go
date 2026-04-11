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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockParameterStoreService is a mock implementation of ParameterStoreService for testing.
type mockParameterStoreService struct {
	getParameterFunc     func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error)
	setParameterFunc     func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error)
	deleteParameterFunc  func(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error)
	getParametersFunc    func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error)
	setParametersFunc    func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error)
	deleteParametersFunc func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error)
	listParametersFunc   func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error)
	searchByTagsFunc     func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error)
}

func (m *mockParameterStoreService) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	if m.getParameterFunc != nil {
		return m.getParameterFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockParameterStoreService) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	if m.setParameterFunc != nil {
		return m.setParameterFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockParameterStoreService) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	if m.deleteParameterFunc != nil {
		return m.deleteParameterFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockParameterStoreService) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	if m.getParametersFunc != nil {
		return m.getParametersFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockParameterStoreService) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	if m.setParametersFunc != nil {
		return m.setParametersFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockParameterStoreService) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	if m.deleteParametersFunc != nil {
		return m.deleteParametersFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockParameterStoreService) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	if m.listParametersFunc != nil {
		return m.listParametersFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockParameterStoreService) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	if m.searchByTagsFunc != nil {
		return m.searchByTagsFunc(ctx, req)
	}
	return nil, nil
}

func newTestParameterStoreServer(t *testing.T, mock *mockParameterStoreService) *ParameterStoreServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return NewParameterStoreServer(mock, log)
}

// =============================================================================
// GetParameter Tests
// =============================================================================

func TestParameterStoreServer_GetParameter_Success(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	mock := &mockParameterStoreService{
		getParameterFunc: func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
			if req.Path != "/app/database/host" {
				t.Errorf("unexpected path: %s", req.Path)
			}
			if !req.WithDecryption {
				t.Errorf("expected WithDecryption to be true")
			}
			return &interfaces.GetParameterResponse{
				Parameter: &interfaces.ParameterValue{
					Path:         "/app/database/host",
					Value:        []byte("localhost:5432"),
					Type:         "string",
					Description:  "Database host",
					Tags:         map[string]string{"env": "dev"},
					IsEncrypted:  false,
					AuditEnabled: true,
					CreatedAt:    now,
					UpdatedAt:    now,
					ExpiresAt:    &expiresAt,
					CreatedBy:    "admin",
					UpdatedBy:    "admin",
				},
				Timestamp: now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.GetParameter(ctx, &pb.GetParameterRequest{
		Path:           "/app/database/host",
		WithDecryption: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Parameter == nil {
		t.Fatal("expected parameter to be returned")
	}

	if resp.Parameter.Path != "/app/database/host" {
		t.Errorf("expected path /app/database/host, got %s", resp.Parameter.Path)
	}

	if string(resp.Parameter.Value) != "localhost:5432" {
		t.Errorf("expected value localhost:5432, got %s", string(resp.Parameter.Value))
	}

	if resp.Parameter.Type != pb.ParameterType_PARAMETER_TYPE_STRING {
		t.Errorf("expected type STRING, got %v", resp.Parameter.Type)
	}

	if resp.Parameter.Description != "Database host" {
		t.Errorf("expected description 'Database host', got %s", resp.Parameter.Description)
	}

	if !resp.Parameter.AuditEnabled {
		t.Error("expected AuditEnabled to be true")
	}
}

func TestParameterStoreServer_GetParameter_EmptyPath(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.GetParameter(ctx, &pb.GetParameterRequest{
		Path: "",
	})

	if err == nil {
		t.Fatal("expected error for empty path")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestParameterStoreServer_GetParameter_NotFound(t *testing.T) {
	mock := &mockParameterStoreService{
		getParameterFunc: func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "GetParameter",
				Err:  &errors.MessageError{Message: "parameter not found: " + req.Path},
			}
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.GetParameter(ctx, &pb.GetParameterRequest{
		Path: "/nonexistent/path",
	})

	if err == nil {
		t.Fatal("expected error for not found parameter")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound, got %v", st.Code())
	}
}

// =============================================================================
// SetParameter Tests
// =============================================================================

func TestParameterStoreServer_SetParameter_Success(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		setParameterFunc: func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
			if req.Path != "/app/config/timeout" {
				t.Errorf("unexpected path: %s", req.Path)
			}
			if string(req.Value) != "30" {
				t.Errorf("unexpected value: %s", string(req.Value))
			}
			if req.Type != "number" {
				t.Errorf("unexpected type: %s", req.Type)
			}
			return &interfaces.SetParameterResponse{
				Path:      req.Path,
				Created:   true,
				Timestamp: now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.SetParameter(ctx, &pb.SetParameterRequest{
		Path:         "/app/config/timeout",
		Value:        []byte("30"),
		Type:         pb.ParameterType_PARAMETER_TYPE_NUMBER,
		Description:  "Request timeout in seconds",
		Tags:         map[string]string{"app": "web"},
		TtlSeconds:   3600,
		AuditEnabled: true,
		Overwrite:    true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Path != "/app/config/timeout" {
		t.Errorf("expected path /app/config/timeout, got %s", resp.Path)
	}

	if !resp.Created {
		t.Error("expected Created to be true")
	}
}

func TestParameterStoreServer_SetParameter_EmptyPath(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.SetParameter(ctx, &pb.SetParameterRequest{
		Path:  "",
		Value: []byte("test"),
	})

	if err == nil {
		t.Fatal("expected error for empty path")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestParameterStoreServer_SetParameter_NilValue(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.SetParameter(ctx, &pb.SetParameterRequest{
		Path:  "/app/test",
		Value: nil,
	})

	if err == nil {
		t.Fatal("expected error for nil value")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// =============================================================================
// DeleteParameter Tests
// =============================================================================

func TestParameterStoreServer_DeleteParameter_Success(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		deleteParameterFunc: func(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
			if req.Path != "/app/old/config" {
				t.Errorf("unexpected path: %s", req.Path)
			}
			return &interfaces.DeleteParameterResponse{
				Path:      req.Path,
				Deleted:   true,
				Timestamp: now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.DeleteParameter(ctx, &pb.DeleteParameterRequest{
		Path: "/app/old/config",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Path != "/app/old/config" {
		t.Errorf("expected path /app/old/config, got %s", resp.Path)
	}

	if !resp.Deleted {
		t.Error("expected Deleted to be true")
	}
}

func TestParameterStoreServer_DeleteParameter_EmptyPath(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.DeleteParameter(ctx, &pb.DeleteParameterRequest{
		Path: "",
	})

	if err == nil {
		t.Fatal("expected error for empty path")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// =============================================================================
// GetParameters (Batch) Tests
// =============================================================================

func TestParameterStoreServer_GetParameters_Success(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		getParametersFunc: func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
			if len(req.Paths) != 2 {
				t.Errorf("expected 2 paths, got %d", len(req.Paths))
			}
			return &interfaces.GetParametersResponse{
				Parameters: []*interfaces.ParameterValue{
					{
						Path:      "/app/db/host",
						Value:     []byte("localhost"),
						Type:      "string",
						CreatedAt: now,
						UpdatedAt: now,
					},
					{
						Path:      "/app/db/port",
						Value:     []byte("5432"),
						Type:      "number",
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
				Errors:    nil,
				Timestamp: now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.GetParameters(ctx, &pb.GetParametersRequest{
		Paths:          []string{"/app/db/host", "/app/db/port"},
		WithDecryption: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(resp.Parameters))
	}
}

func TestParameterStoreServer_GetParameters_EmptyPaths(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.GetParameters(ctx, &pb.GetParametersRequest{
		Paths: []string{},
	})

	if err == nil {
		t.Fatal("expected error for empty paths")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestParameterStoreServer_GetParameters_PartialErrors(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		getParametersFunc: func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
			return &interfaces.GetParametersResponse{
				Parameters: []*interfaces.ParameterValue{
					{
						Path:      "/app/exists",
						Value:     []byte("value"),
						Type:      "string",
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
				Errors: map[string]string{
					"/app/notfound": "parameter not found",
				},
				Timestamp: now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.GetParameters(ctx, &pb.GetParametersRequest{
		Paths: []string{"/app/exists", "/app/notfound"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(resp.Parameters))
	}

	if len(resp.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(resp.Errors))
	}

	if _, ok := resp.Errors["/app/notfound"]; !ok {
		t.Error("expected error for /app/notfound")
	}
}

// =============================================================================
// SetParameters (Batch) Tests
// =============================================================================

func TestParameterStoreServer_SetParameters_Success(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		setParametersFunc: func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
			if len(req.Parameters) != 2 {
				t.Errorf("expected 2 parameters, got %d", len(req.Parameters))
			}
			return &interfaces.SetParametersResponse{
				Results: []*interfaces.SetParameterResult{
					{Path: "/app/key1", Created: true, Success: true},
					{Path: "/app/key2", Created: false, Success: true},
				},
				Timestamp: now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.SetParameters(ctx, &pb.SetParametersRequest{
		Parameters: []*pb.SetParameterItem{
			{Path: "/app/key1", Value: []byte("value1")},
			{Path: "/app/key2", Value: []byte("value2")},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}

	if !resp.Results[0].Created {
		t.Error("expected first parameter to be created")
	}
}

func TestParameterStoreServer_SetParameters_EmptyParameters(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.SetParameters(ctx, &pb.SetParametersRequest{
		Parameters: []*pb.SetParameterItem{},
	})

	if err == nil {
		t.Fatal("expected error for empty parameters")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestParameterStoreServer_SetParameters_InvalidItem(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	// Test missing path
	_, err := server.SetParameters(ctx, &pb.SetParametersRequest{
		Parameters: []*pb.SetParameterItem{
			{Path: "", Value: []byte("value")},
		},
	})

	if err == nil {
		t.Fatal("expected error for missing path")
	}

	// Test missing value
	_, err = server.SetParameters(ctx, &pb.SetParametersRequest{
		Parameters: []*pb.SetParameterItem{
			{Path: "/app/test", Value: nil},
		},
	})

	if err == nil {
		t.Fatal("expected error for missing value")
	}
}

// =============================================================================
// DeleteParameters (Batch) Tests
// =============================================================================

func TestParameterStoreServer_DeleteParameters_Success(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		deleteParametersFunc: func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
			if len(req.Paths) != 2 {
				t.Errorf("expected 2 paths, got %d", len(req.Paths))
			}
			return &interfaces.DeleteParametersResponse{
				Results: []*interfaces.DeleteParameterResult{
					{Path: "/app/key1", Deleted: true},
					{Path: "/app/key2", Deleted: true},
				},
				DeletedCount: 2,
				Timestamp:    now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.DeleteParameters(ctx, &pb.DeleteParametersRequest{
		Paths: []string{"/app/key1", "/app/key2"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}

	if resp.DeletedCount != 2 {
		t.Errorf("expected DeletedCount 2, got %d", resp.DeletedCount)
	}
}

func TestParameterStoreServer_DeleteParameters_EmptyPaths(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.DeleteParameters(ctx, &pb.DeleteParametersRequest{
		Paths: []string{},
	})

	if err == nil {
		t.Fatal("expected error for empty paths")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// =============================================================================
// ListParameters Tests
// =============================================================================

func TestParameterStoreServer_ListParameters_Success(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		listParametersFunc: func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
			if req.Prefix != "/app/" {
				t.Errorf("unexpected prefix: %s", req.Prefix)
			}
			if !req.Recursive {
				t.Error("expected recursive to be true")
			}
			return &interfaces.ListParametersResponse{
				Parameters: []*interfaces.ParameterValue{
					{
						Path:      "/app/config/host",
						Value:     []byte("localhost"),
						Type:      "string",
						CreatedAt: now,
						UpdatedAt: now,
					},
					{
						Path:      "/app/config/port",
						Value:     []byte("8080"),
						Type:      "number",
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
				NextCursor: "cursor123",
				HasMore:    true,
				TotalCount: 10,
				Timestamp:  now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.ListParameters(ctx, &pb.ListParametersRequest{
		Prefix:    "/app/",
		Recursive: true,
		Limit:     100,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(resp.Parameters))
	}

	if resp.NextCursor != "cursor123" {
		t.Errorf("expected cursor 'cursor123', got %s", resp.NextCursor)
	}

	if !resp.HasMore {
		t.Error("expected HasMore to be true")
	}

	if resp.TotalCount != 10 {
		t.Errorf("expected TotalCount 10, got %d", resp.TotalCount)
	}
}

func TestParameterStoreServer_ListParameters_EmptyResult(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		listParametersFunc: func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
			return &interfaces.ListParametersResponse{
				Parameters: []*interfaces.ParameterValue{},
				NextCursor: "",
				HasMore:    false,
				TotalCount: 0,
				Timestamp:  now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.ListParameters(ctx, &pb.ListParametersRequest{
		Prefix: "/nonexistent/",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Parameters) != 0 {
		t.Errorf("expected 0 parameters, got %d", len(resp.Parameters))
	}

	if resp.HasMore {
		t.Error("expected HasMore to be false")
	}
}

// =============================================================================
// SearchByTags Tests
// =============================================================================

func TestParameterStoreServer_SearchByTags_Success(t *testing.T) {
	now := time.Now()

	mock := &mockParameterStoreService{
		searchByTagsFunc: func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
			if req.Tags["env"] != "production" {
				t.Errorf("unexpected tag value: %s", req.Tags["env"])
			}
			return &interfaces.SearchByTagsResponse{
				Parameters: []*interfaces.ParameterValue{
					{
						Path:      "/app/prod/host",
						Value:     []byte("prod.example.com"),
						Type:      "string",
						Tags:      map[string]string{"env": "production"},
						CreatedAt: now,
						UpdatedAt: now,
					},
				},
				NextCursor: "",
				HasMore:    false,
				Timestamp:  now,
			}, nil
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	resp, err := server.SearchByTags(ctx, &pb.SearchByTagsRequest{
		Tags:       map[string]string{"env": "production"},
		WithValues: true,
		Limit:      50,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(resp.Parameters))
	}

	if resp.Parameters[0].Tags["env"] != "production" {
		t.Error("expected tag 'env' = 'production'")
	}
}

func TestParameterStoreServer_SearchByTags_EmptyTags(t *testing.T) {
	mock := &mockParameterStoreService{}
	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.SearchByTags(ctx, &pb.SearchByTagsRequest{
		Tags: map[string]string{},
	})

	if err == nil {
		t.Fatal("expected error for empty tags")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// =============================================================================
// Conversion Helper Tests
// =============================================================================

func TestProtoParameterTypeToService(t *testing.T) {
	tests := []struct {
		input    pb.ParameterType
		expected string
	}{
		{pb.ParameterType_PARAMETER_TYPE_STRING, "string"},
		{pb.ParameterType_PARAMETER_TYPE_NUMBER, "number"},
		{pb.ParameterType_PARAMETER_TYPE_JSON, "json"},
		{pb.ParameterType_PARAMETER_TYPE_BINARY, "binary"},
		{pb.ParameterType_PARAMETER_TYPE_UNSPECIFIED, "string"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := protoParameterTypeToService(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestServiceParameterTypeToProto(t *testing.T) {
	tests := []struct {
		input    string
		expected pb.ParameterType
	}{
		{"string", pb.ParameterType_PARAMETER_TYPE_STRING},
		{"number", pb.ParameterType_PARAMETER_TYPE_NUMBER},
		{"json", pb.ParameterType_PARAMETER_TYPE_JSON},
		{"binary", pb.ParameterType_PARAMETER_TYPE_BINARY},
		{"unknown", pb.ParameterType_PARAMETER_TYPE_STRING},
		{"", pb.ParameterType_PARAMETER_TYPE_STRING},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := serviceParameterTypeToProto(tt.input)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestConvertParameterToProto_Nil(t *testing.T) {
	result := convertParameterToProto(nil)
	if result != nil {
		t.Error("expected nil result for nil input")
	}
}

func TestConvertParameterToProto_WithExpiration(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	param := &interfaces.ParameterValue{
		Path:        "/test/path",
		Value:       []byte("test-value"),
		Type:        "string",
		Description: "Test description",
		Tags:        map[string]string{"key": "value"},
		IsEncrypted: true,
		KMSKeyID:    "key-123",
		ExpiresAt:   &expiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   "user1",
		UpdatedBy:   "user2",
	}

	result := convertParameterToProto(param)

	if result.Path != param.Path {
		t.Errorf("expected path %s, got %s", param.Path, result.Path)
	}

	if !result.Encrypted {
		t.Error("expected Encrypted to be true")
	}

	if result.KmsKeyId != param.KMSKeyID {
		t.Errorf("expected KmsKeyId %s, got %s", param.KMSKeyID, result.KmsKeyId)
	}

	if result.ExpiresAt != expiresAt.UnixNano() {
		t.Errorf("expected ExpiresAt %d, got %d", expiresAt.UnixNano(), result.ExpiresAt)
	}
}

func TestConvertParameterToProto_WithoutExpiration(t *testing.T) {
	now := time.Now()

	param := &interfaces.ParameterValue{
		Path:      "/test/path",
		Value:     []byte("test-value"),
		Type:      "string",
		ExpiresAt: nil,
		CreatedAt: now,
		UpdatedAt: now,
	}

	result := convertParameterToProto(param)

	if result.ExpiresAt != 0 {
		t.Errorf("expected ExpiresAt 0, got %d", result.ExpiresAt)
	}
}

func TestTimeToUnixNano(t *testing.T) {
	// Test with valid time
	now := time.Now()
	result := timeToUnixNano(now)
	if result != now.UnixNano() {
		t.Errorf("expected %d, got %d", now.UnixNano(), result)
	}

	// Test with zero time
	zeroTime := time.Time{}
	result = timeToUnixNano(zeroTime)
	if result != 0 {
		t.Errorf("expected 0 for zero time, got %d", result)
	}
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func TestParameterStoreServer_ServiceError(t *testing.T) {
	mock := &mockParameterStoreService{
		getParameterFunc: func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "GetParameter",
				Err:  &errors.MessageError{Message: "internal error"},
			}
		},
	}

	server := newTestParameterStoreServer(t, mock)
	ctx := context.Background()

	_, err := server.GetParameter(ctx, &pb.GetParameterRequest{
		Path: "/test/path",
	})

	if err == nil {
		t.Fatal("expected error")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}

	if st.Code() != codes.Internal {
		t.Errorf("expected Internal, got %v", st.Code())
	}
}

func TestParameterStoreServer_SetParameter_AllTypes(t *testing.T) {
	now := time.Now()

	types := []struct {
		pbType  pb.ParameterType
		svcType string
	}{
		{pb.ParameterType_PARAMETER_TYPE_STRING, "string"},
		{pb.ParameterType_PARAMETER_TYPE_NUMBER, "number"},
		{pb.ParameterType_PARAMETER_TYPE_JSON, "json"},
		{pb.ParameterType_PARAMETER_TYPE_BINARY, "binary"},
	}

	for _, tt := range types {
		t.Run(tt.svcType, func(t *testing.T) {
			mock := &mockParameterStoreService{
				setParameterFunc: func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
					if req.Type != tt.svcType {
						t.Errorf("expected type %s, got %s", tt.svcType, req.Type)
					}
					return &interfaces.SetParameterResponse{
						Path:      req.Path,
						Created:   true,
						Timestamp: now,
					}, nil
				},
			}

			server := newTestParameterStoreServer(t, mock)
			ctx := context.Background()

			_, err := server.SetParameter(ctx, &pb.SetParameterRequest{
				Path:  "/test/path",
				Value: []byte("test"),
				Type:  tt.pbType,
			})

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// =============================================================================
// NewParameterStoreServer Tests
// =============================================================================

func TestNewParameterStoreServer(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	mock := &mockParameterStoreService{}
	server := NewParameterStoreServer(mock, log)

	if server == nil {
		t.Fatal("expected server to be created")
	}

	if server.parameterSvc == nil {
		t.Error("expected parameterSvc to be set")
	}

	if server.logger == nil {
		t.Error("expected logger to be set")
	}
}
