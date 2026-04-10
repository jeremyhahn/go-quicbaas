//go:build quic

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

package quic

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Parameter Store Service
// =============================================================================

type mockParameterStoreService struct {
	getParameterFunc     func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error)
	setParameterFunc     func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error)
	deleteParameterFunc  func(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error)
	listParametersFunc   func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error)
	getParametersFunc    func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error)
	setParametersFunc    func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error)
	deleteParametersFunc func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error)
	searchByTagsFunc     func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error)
}

func (m *mockParameterStoreService) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	if m.getParameterFunc != nil {
		return m.getParameterFunc(ctx, req)
	}
	return &interfaces.GetParameterResponse{
		Parameter: &interfaces.ParameterValue{
			Path:        req.Path,
			Value:       []byte("test-value"),
			Type:        "string",
			Description: "Test parameter",
			Tags:        map[string]string{"env": "test"},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	if m.setParameterFunc != nil {
		return m.setParameterFunc(ctx, req)
	}
	return &interfaces.SetParameterResponse{
		Path:      req.Path,
		Created:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	if m.deleteParameterFunc != nil {
		return m.deleteParameterFunc(ctx, req)
	}
	return &interfaces.DeleteParameterResponse{
		Path:      req.Path,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	if m.listParametersFunc != nil {
		return m.listParametersFunc(ctx, req)
	}
	return &interfaces.ListParametersResponse{
		Parameters: []*interfaces.ParameterValue{
			{Path: "/test/param1", Value: []byte("value1"), Type: "string"},
			{Path: "/test/param2", Value: []byte("value2"), Type: "string"},
		},
		HasMore:   false,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	if m.getParametersFunc != nil {
		return m.getParametersFunc(ctx, req)
	}
	params := make([]*interfaces.ParameterValue, 0, len(req.Paths))
	for _, path := range req.Paths {
		params = append(params, &interfaces.ParameterValue{
			Path:  path,
			Value: []byte("test-value"),
			Type:  "string",
		})
	}
	return &interfaces.GetParametersResponse{
		Parameters: params,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockParameterStoreService) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	if m.setParametersFunc != nil {
		return m.setParametersFunc(ctx, req)
	}
	results := make([]*interfaces.SetParameterResult, 0, len(req.Parameters))
	for _, p := range req.Parameters {
		results = append(results, &interfaces.SetParameterResult{
			Path:    p.Path,
			Created: true,
			Success: true,
		})
	}
	return &interfaces.SetParametersResponse{
		Results:   results,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	if m.deleteParametersFunc != nil {
		return m.deleteParametersFunc(ctx, req)
	}
	results := make([]*interfaces.DeleteParameterResult, 0, len(req.Paths))
	for _, path := range req.Paths {
		results = append(results, &interfaces.DeleteParameterResult{
			Path:    path,
			Deleted: true,
		})
	}
	return &interfaces.DeleteParametersResponse{
		Results:      results,
		DeletedCount: len(req.Paths),
		Timestamp:    time.Now(),
	}, nil
}

func (m *mockParameterStoreService) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	if m.searchByTagsFunc != nil {
		return m.searchByTagsFunc(ctx, req)
	}
	return &interfaces.SearchByTagsResponse{
		Parameters: []*interfaces.ParameterValue{
			{Path: "/test/tagged1", Value: []byte("value1"), Type: "string", Tags: req.Tags},
		},
		HasMore:   false,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockParameterStoreService) Close() error {
	return nil
}

// mockServiceWithParameterStore extends mockService with parameter store support.
type mockServiceWithParameterStore struct {
	mockService
	paramSvc service.ParameterStoreService
}

func (m *mockServiceWithParameterStore) ParameterStoreService() service.ParameterStoreService {
	return m.paramSvc
}

func createParameterTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-param-test",
	}
	log, _ := logger.New(cfg)
	return log
}

// =============================================================================
// Handler Tests - Success Paths
// =============================================================================

func TestHandleParameterGet_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.GetParameterRequest{
		Path:           "/test/param",
		WithDecryption: true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleParameterSet_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.SetParameterRequest{
		Path:      "/test/param",
		Value:     []byte("test-value"),
		Type:      "string",
		Overwrite: true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterSet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleParameterDelete_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.DeleteParameterRequest{
		Path: "/test/param",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleParameterList_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.ListParametersRequest{
		Prefix:    "/test",
		Recursive: true,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleParameterBatchGet_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.GetParametersRequest{
		Paths:          []string{"/test/param1", "/test/param2"},
		WithDecryption: true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterBatchGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleParameterBatchSet_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{
			{Path: "/test/param1", Value: []byte("value1"), Type: "string", Overwrite: true},
			{Path: "/test/param2", Value: []byte("value2"), Type: "string", Overwrite: true},
		},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterBatchSet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleParameterBatchDelete_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.DeleteParametersRequest{
		Paths: []string{"/test/param1", "/test/param2"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterBatchDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleParameterSearch_Success(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, err := EncodePayload(&interfaces.SearchByTagsRequest{
		Tags:  map[string]string{"env": "test"},
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpParameterSearch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// =============================================================================
// Handler Tests - Nil Service Errors
// =============================================================================

func TestHandleParameterGet_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.GetParameterRequest{Path: "/test/param"})
	req := &Request{
		Opcode:  OpParameterGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

func TestHandleParameterSet_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.SetParameterRequest{Path: "/test/param", Value: []byte("value")})
	req := &Request{
		Opcode:  OpParameterSet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

func TestHandleParameterDelete_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.DeleteParameterRequest{Path: "/test/param"})
	req := &Request{
		Opcode:  OpParameterDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

func TestHandleParameterList_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.ListParametersRequest{Prefix: "/test"})
	req := &Request{
		Opcode:  OpParameterList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

func TestHandleParameterBatchGet_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.GetParametersRequest{Paths: []string{"/test/param"}})
	req := &Request{
		Opcode:  OpParameterBatchGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

func TestHandleParameterBatchSet_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.SetParametersRequest{})
	req := &Request{
		Opcode:  OpParameterBatchSet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

func TestHandleParameterBatchDelete_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.DeleteParametersRequest{Paths: []string{"/test"}})
	req := &Request{
		Opcode:  OpParameterBatchDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

func TestHandleParameterSearch_NilService(t *testing.T) {
	svc := &mockServiceWithParameterStore{paramSvc: nil}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.SearchByTagsRequest{Tags: map[string]string{"env": "test"}})
	req := &Request{
		Opcode:  OpParameterSearch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil parameter store service")
	}
}

// =============================================================================
// Handler Tests - Decode Errors
// =============================================================================

func TestHandleParameterGet_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterGet,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleParameterSet_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterSet,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleParameterDelete_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterDelete,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleParameterList_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterList,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleParameterBatchGet_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterBatchGet,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleParameterBatchSet_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterBatchSet,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleParameterBatchDelete_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterBatchDelete,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleParameterSearch_DecodeError(t *testing.T) {
	paramSvc := &mockParameterStoreService{}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	req := &Request{
		Opcode:  OpParameterSearch,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// =============================================================================
// Handler Tests - Service Errors
// =============================================================================

func TestHandleParameterGet_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		getParameterFunc: func(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.GetParameterRequest{Path: "/test/param"})
	req := &Request{
		Opcode:  OpParameterGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleParameterSet_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		setParameterFunc: func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.SetParameterRequest{Path: "/test/param", Value: []byte("value")})
	req := &Request{
		Opcode:  OpParameterSet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleParameterDelete_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		deleteParameterFunc: func(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.DeleteParameterRequest{Path: "/test/param"})
	req := &Request{
		Opcode:  OpParameterDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleParameterList_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		listParametersFunc: func(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.ListParametersRequest{Prefix: "/test"})
	req := &Request{
		Opcode:  OpParameterList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleParameterBatchGet_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		getParametersFunc: func(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.GetParametersRequest{Paths: []string{"/test/param"}})
	req := &Request{
		Opcode:  OpParameterBatchGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleParameterBatchSet_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		setParametersFunc: func(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{{Path: "/test", Value: []byte("v")}},
	})
	req := &Request{
		Opcode:  OpParameterBatchSet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleParameterBatchDelete_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		deleteParametersFunc: func(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.DeleteParametersRequest{Paths: []string{"/test"}})
	req := &Request{
		Opcode:  OpParameterBatchDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleParameterSearch_ServiceError(t *testing.T) {
	paramSvc := &mockParameterStoreService{
		searchByTagsFunc: func(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
			return nil, &errors.MessageError{Message: "service error"}
		},
	}
	svc := &mockServiceWithParameterStore{paramSvc: paramSvc}
	handler := NewHandler(svc, nil, nil, nil, nil, createParameterTestLogger())

	payload, _ := EncodePayload(&interfaces.SearchByTagsRequest{Tags: map[string]string{"env": "test"}})
	req := &Request{
		Opcode:  OpParameterSearch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}
