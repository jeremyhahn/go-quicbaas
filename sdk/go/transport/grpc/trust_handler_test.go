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

package grpc

import (
	"testing"

	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
)

func TestIsTrustService(t *testing.T) {
	tests := []struct {
		service  string
		expected bool
	}{
		{"quicbaas.Trust", true},
		{"quicbaas.Trust.X", true},
		{"quicbaas.Certificate", false},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isTrustService(tt.service); got != tt.expected {
			t.Errorf("isTrustService(%q) = %v, want %v", tt.service, got, tt.expected)
		}
	}
}

func TestTrustHandlerHandleRequest_UnsupportedMethod(t *testing.T) {
	handler := newTrustHandler(nil)
	_, err := handler.handleRequest(nil, "quicbaas.Trust", "PublishDANE", nil)
	if err == nil {
		t.Fatal("expected error for method not exposed by TrustService proto")
	}
}

func TestTrustHandlerHandleCrossSignedRootPool_InvalidRequestType(t *testing.T) {
	handler := newTrustHandler(nil)
	_, err := handler.handleCrossSignedRootPool(nil, nil, 42)
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestTrustHandlerHandleAutoImportAIA_InvalidRequestType(t *testing.T) {
	handler := newTrustHandler(nil)
	_, err := handler.handleAutoImportAIA(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestTrustHandlerHandleAutoImportAIA_EmptyChain(t *testing.T) {
	handler := newTrustHandler(nil)
	_, err := handler.handleAutoImportAIA(nil, nil, &trustclient.AutoImportAIARequest{})
	if err == nil {
		t.Error("expected error for empty chain")
	}
}
