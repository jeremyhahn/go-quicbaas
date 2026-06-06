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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

func TestIsRAGService(t *testing.T) {
	cases := map[string]bool{
		"quicbaas.RAG": true,
		"RAG":          true,
		"quicbaas.KV":  false,
		"":             false,
	}
	for svc, want := range cases {
		if got := isRAGService(svc); got != want {
			t.Errorf("isRAGService(%q) = %v, want %v", svc, got, want)
		}
	}
}

func TestNewRAGHandler(t *testing.T) {
	if newRAGHandler(nil) == nil {
		t.Fatal("newRAGHandler returned nil")
	}
}

func TestRAGHandler_HandleRequest_UnknownMethod(t *testing.T) {
	h := newRAGHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.RAG", "Bogus", nil); err == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestRAGHandler_HandleQuery_InvalidRequestType(t *testing.T) {
	h := newRAGHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.RAG", "Query", "wrong"); err == nil {
		t.Fatal("expected invalid-request-type error")
	}
}

func TestRAGHandler_HandleBuildContext_InvalidRequestType(t *testing.T) {
	h := newRAGHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.RAG", "BuildContext", "wrong"); err == nil {
		t.Fatal("expected invalid-request-type error")
	}
}

func TestConvertRAGQueryResponse(t *testing.T) {
	m := map[string]interface{}{
		"context":      "ctx",
		"tokens":       float64(42),
		"truncated":    true,
		"result_count": float64(3),
		"citations": []interface{}{
			map[string]interface{}{"index": float64(0), "file_path": "a.go", "start_line": float64(1), "end_line": float64(5), "score": float64(0.9)},
		},
	}
	resp, err := convertRAGQueryResponse(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Context != "ctx" || resp.Tokens != 42 || !resp.Truncated || resp.ResultCount != 3 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(resp.Citations) != 1 || resp.Citations[0].FilePath != "a.go" || resp.Citations[0].EndLine != 5 {
		t.Fatalf("unexpected citations: %+v", resp.Citations)
	}
}

func TestConvertRAGQueryResponse_Empty(t *testing.T) {
	resp, err := convertRAGQueryResponse(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Context != "" || resp.Tokens != 0 || len(resp.Citations) != 0 {
		t.Fatalf("expected zero-value response, got %+v", resp)
	}
}

func TestConvertRAGBuildContextResponse(t *testing.T) {
	m := map[string]interface{}{
		"context":      "built",
		"tokens":       float64(10),
		"truncated":    false,
		"result_count": float64(2),
		"citations": []interface{}{
			map[string]interface{}{"index": float64(1), "file_path": "b.go", "score": float64(0.5)},
		},
	}
	resp, err := convertRAGBuildContextResponse(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Context != "built" || resp.Tokens != 10 || resp.ResultCount != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(resp.Citations) != 1 || resp.Citations[0].Index != 1 || resp.Citations[0].Score != 0.5 {
		t.Fatalf("unexpected citations: %+v", resp.Citations)
	}
}

func TestConvertCitation(t *testing.T) {
	c := convertCitation(map[string]interface{}{
		"index": float64(2), "file_path": "c.go", "start_line": float64(3),
		"end_line": float64(7), "score": float64(0.75),
	})
	if c.Index != 2 || c.FilePath != "c.go" || c.StartLine != 3 || c.EndLine != 7 || c.Score != 0.75 {
		t.Fatalf("unexpected citation: %+v", c)
	}
}

func TestConvertCitation_MissingFields(t *testing.T) {
	c := convertCitation(map[string]interface{}{})
	if c != (rag.Citation{}) {
		t.Fatalf("expected zero-value citation, got %+v", c)
	}
}
