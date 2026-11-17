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
	"math"
	"testing"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
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

func TestRAGQueryRequestToProto_MinScorePresence(t *testing.T) {
	omitted, err := ragQueryRequestToProto("op", &rag.QueryRequest{Query: "q", Namespace: "n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if omitted.MinScore != nil {
		t.Fatalf("an unset MinScore must not be sent, got %v", *omitted.MinScore)
	}

	zero, err := ragQueryRequestToProto("op", &rag.QueryRequest{Query: "q", Namespace: "n", MinScoreSet: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zero.MinScore == nil || *zero.MinScore != 0 {
		t.Fatalf("an explicit 0 must be sent as 0, got %v", zero.MinScore)
	}

	half, err := ragQueryRequestToProto("op", &rag.QueryRequest{Query: "q", Namespace: "n", MinScore: 0.5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if half.MinScore == nil || *half.MinScore != 0.5 {
		t.Fatalf("a non-zero MinScore must be sent, got %v", half.MinScore)
	}
}

func TestRAGTemplateAndTruncationToProto(t *testing.T) {
	for name, want := range map[string]pb.RAGTemplate{
		"": pb.RAGTemplate_RAG_TEMPLATE_UNSPECIFIED, "markdown": pb.RAGTemplate_RAG_TEMPLATE_MARKDOWN,
		"json": pb.RAGTemplate_RAG_TEMPLATE_JSON, "xml": pb.RAGTemplate_RAG_TEMPLATE_XML,
		"plain": pb.RAGTemplate_RAG_TEMPLATE_PLAIN,
	} {
		got, err := ragTemplateToProto("op", name)
		if err != nil || got != want {
			t.Fatalf("template %q: got %v, %v; want %v", name, got, err, want)
		}
	}
	if _, err := ragTemplateToProto("op", "yaml"); err == nil {
		t.Fatal("an unknown template must be refused")
	}
	for name, want := range map[string]pb.RAGTruncationStrategy{
		"": pb.RAGTruncationStrategy_RAG_TRUNCATION_UNSPECIFIED, "smart": pb.RAGTruncationStrategy_RAG_TRUNCATION_SMART,
		"hard": pb.RAGTruncationStrategy_RAG_TRUNCATION_HARD, "none": pb.RAGTruncationStrategy_RAG_TRUNCATION_NONE,
	} {
		got, err := ragTruncationToProto("op", name)
		if err != nil || got != want {
			t.Fatalf("truncation %q: got %v, %v; want %v", name, got, err, want)
		}
	}
	if _, err := ragTruncationToProto("op", "drop"); err == nil {
		t.Fatal("an unknown truncation strategy must be refused")
	}
}

func TestRAGQueryRequestToProto_RefusesOutOfRangeTopK(t *testing.T) {
	if _, err := ragQueryRequestToProto("op", &rag.QueryRequest{Query: "q", Namespace: "n", TopK: math.MaxInt32 + 1}); err == nil {
		t.Fatal("a top_k past int32 must be refused")
	}
}

func TestRAGCitationsFromProto(t *testing.T) {
	if got := ragCitationsFromProto(nil); got != nil {
		t.Fatalf("no citations must map to nil, got %+v", got)
	}
	got := ragCitationsFromProto([]*pb.RAGCitation{{Index: 2, FilePath: "c.go", StartLine: 3, EndLine: 7, Score: 0.75}})
	want := rag.Citation{Index: 2, FilePath: "c.go", StartLine: 3, EndLine: 7, Score: 0.75}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
}
