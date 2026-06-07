//go:build quic

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

package quic

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockTrustService is a configurable service.TrustService.
type mockTrustService struct {
	crossSignedFn func(ctx context.Context, req *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error)
	autoImportFn  func(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error)
	publishDANEFn func(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error)
}

func (m *mockTrustService) CrossSignedRootPool(ctx context.Context, req *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error) {
	if m.crossSignedFn != nil {
		return m.crossSignedFn(ctx, req)
	}
	return &service.CrossSignedRootPoolResponse{RootPEMs: [][]byte{[]byte("root")}}, nil
}

func (m *mockTrustService) AutoImportAIA(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error) {
	if m.autoImportFn != nil {
		return m.autoImportFn(ctx, req)
	}
	return &service.AutoImportAIAResponse{Imported: len(req.ChainDER)}, nil
}

func (m *mockTrustService) PublishDANE(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error) {
	if m.publishDANEFn != nil {
		return m.publishDANEFn(ctx, req)
	}
	return &service.PublishDANEResponse{Published: 4}, nil
}

// mockTrustEnabledService overrides Trust() on the base mockService.
type mockTrustEnabledService struct {
	mockService
	trustSvc service.TrustService
}

func (m *mockTrustEnabledService) Trust() service.TrustService {
	return m.trustSvc
}

func createTrustTestHandler(trustSvc service.TrustService) *Handler {
	svc := &mockTrustEnabledService{trustSvc: trustSvc}
	return NewHandler(svc, nil, nil, nil, createCertificateTestLogger())
}

func TestHandleTrustCrossSignedRootPool_Success(t *testing.T) {
	trustSvc := &mockTrustService{
		crossSignedFn: func(ctx context.Context, req *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error) {
			return &service.CrossSignedRootPoolResponse{
				RootPEMs: [][]byte{[]byte("root-a"), []byte("root-b")},
			}, nil
		},
	}
	handler := createTrustTestHandler(trustSvc)

	payload, err := EncodePayload(&service.CrossSignedRootPoolRequest{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := &Request{Opcode: OpTrustCrossSignedRootPool, Payload: payload}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleTrustCrossSignedRootPool_NilService(t *testing.T) {
	handler := createTrustTestHandler(nil)
	payload, _ := EncodePayload(&service.CrossSignedRootPoolRequest{})
	req := &Request{Opcode: OpTrustCrossSignedRootPool, Payload: payload}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil trust service")
	}
}

func TestHandleTrustCrossSignedRootPool_ServiceError(t *testing.T) {
	trustSvc := &mockTrustService{
		crossSignedFn: func(ctx context.Context, req *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error) {
			return nil, stderrors.New("boom")
		},
	}
	handler := createTrustTestHandler(trustSvc)
	payload, _ := EncodePayload(&service.CrossSignedRootPoolRequest{})
	req := &Request{Opcode: OpTrustCrossSignedRootPool, Payload: payload}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleTrustAutoImportAIA_Success(t *testing.T) {
	trustSvc := &mockTrustService{
		autoImportFn: func(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error) {
			return &service.AutoImportAIAResponse{Imported: len(req.ChainDER)}, nil
		},
	}
	handler := createTrustTestHandler(trustSvc)

	payload, err := EncodePayload(&service.AutoImportAIARequest{
		ChainDER: [][]byte{{0x30, 0x82}, {0x30, 0x82}},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := &Request{Opcode: OpTrustAutoImportAIA, Payload: payload}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleTrustAutoImportAIA_NilService(t *testing.T) {
	handler := createTrustTestHandler(nil)
	payload, _ := EncodePayload(&service.AutoImportAIARequest{ChainDER: [][]byte{{0x30}}})
	req := &Request{Opcode: OpTrustAutoImportAIA, Payload: payload}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil trust service")
	}
}

func TestHandleTrustAutoImportAIA_DecodeError(t *testing.T) {
	handler := createTrustTestHandler(&mockTrustService{})
	req := &Request{Opcode: OpTrustAutoImportAIA, Payload: []byte("not json")}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected decode error")
	}
}

func TestHandleTrustAutoImportAIA_ServiceError(t *testing.T) {
	trustSvc := &mockTrustService{
		autoImportFn: func(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error) {
			return nil, stderrors.New("import boom")
		},
	}
	handler := createTrustTestHandler(trustSvc)
	payload, _ := EncodePayload(&service.AutoImportAIARequest{ChainDER: [][]byte{{0x30}}})
	req := &Request{Opcode: OpTrustAutoImportAIA, Payload: payload}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleTrustPublishDANE_Success(t *testing.T) {
	trustSvc := &mockTrustService{
		publishDANEFn: func(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error) {
			return &service.PublishDANEResponse{Published: 4}, nil
		},
	}
	handler := createTrustTestHandler(trustSvc)

	payload, err := EncodePayload(&service.PublishDANERequest{
		Name: "kms.example.com", Port: 443, CertDER: []byte{0x30, 0x82},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := &Request{Opcode: OpTrustPublishDANE, Payload: payload}
	resp := handler.handleTrustPublishDANE(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleTrustPublishDANE_NilService(t *testing.T) {
	handler := createTrustTestHandler(nil)
	payload, _ := EncodePayload(&service.PublishDANERequest{Name: "h", Port: 443, CertDER: []byte{0x30}})
	req := &Request{Opcode: OpTrustPublishDANE, Payload: payload}
	resp := handler.handleTrustPublishDANE(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error response for nil trust service")
	}
}

func TestHandleTrustPublishDANE_ServiceError(t *testing.T) {
	trustSvc := &mockTrustService{
		publishDANEFn: func(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error) {
			return nil, service.ErrTrustNoDANEPublisher
		},
	}
	handler := createTrustTestHandler(trustSvc)
	payload, _ := EncodePayload(&service.PublishDANERequest{Name: "h", Port: 443, CertDER: []byte{0x30}})
	req := &Request{Opcode: OpTrustPublishDANE, Payload: payload}
	resp := handler.handleTrustPublishDANE(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error response for not-configured publisher")
	}
}

func TestHandleTrustPublishDANE_DecodeError(t *testing.T) {
	handler := createTrustTestHandler(&mockTrustService{})
	req := &Request{Opcode: OpTrustPublishDANE, Payload: []byte("{not json")}
	resp := handler.handleTrustPublishDANE(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error response for decode failure")
	}
}
