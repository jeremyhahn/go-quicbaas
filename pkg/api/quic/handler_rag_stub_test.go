//go:build quic && !rag

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
)

// =============================================================================
// RAG Stub Handler Tests
// =============================================================================

func TestHandleRAGQuery_NotEnabled(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()
	req := &Request{
		Opcode:  OpRAGQuery,
		Payload: []byte("{}"),
	}
	resp := h.handleRAGQuery(ctx, req)
	if resp.Status != StatusNotFound {
		t.Fatalf("Expected StatusNotFound, got 0x%02X", resp.Status)
	}
	if string(resp.Payload) != "RAG not enabled - build with -tags rag" {
		t.Fatalf("Unexpected payload: %s", string(resp.Payload))
	}
}

func TestHandleRAGBuildContext_NotEnabled(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()
	req := &Request{
		Opcode:  OpRAGBuildContext,
		Payload: []byte("{}"),
	}
	resp := h.handleRAGBuildContext(ctx, req)
	if resp.Status != StatusNotFound {
		t.Fatalf("Expected StatusNotFound, got 0x%02X", resp.Status)
	}
	if string(resp.Payload) != "RAG not enabled - build with -tags rag" {
		t.Fatalf("Unexpected payload: %s", string(resp.Payload))
	}
}

// Test via HandleRequest to cover the dispatch path
func TestHandleRequest_RAGQuery_NotEnabled(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()
	req := &Request{
		Opcode:  OpRAGQuery,
		Payload: []byte("{}"),
	}
	resp := h.HandleRequest(ctx, req)
	if resp.Status != StatusNotFound {
		t.Fatalf("Expected StatusNotFound, got 0x%02X", resp.Status)
	}
}

func TestHandleRequest_RAGBuildContext_NotEnabled(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()
	req := &Request{
		Opcode:  OpRAGBuildContext,
		Payload: []byte("{}"),
	}
	resp := h.HandleRequest(ctx, req)
	if resp.Status != StatusNotFound {
		t.Fatalf("Expected StatusNotFound, got 0x%02X", resp.Status)
	}
}
