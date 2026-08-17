//go:build !rag

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

package apimodule

import (
	"context"
	"testing"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestModule_OllamaService_UnavailableWithoutRAGTag proves that, compiled
// WITHOUT -tags rag, OllamaService answers Unavailable even when the
// composed facade's Ollama() accessor returns a fully configured
// service.OllamaService: buildOllamaServer's !rag implementation
// (module_ollama_norag.go) is an unconditional nil, matching
// pkg/server/lifecycle_rag_noop.go's registerOllamaService no-op — the
// capability requires the rag build tag regardless of configuration.
func TestModule_OllamaService_UnavailableWithoutRAGTag(t *testing.T) {
	composed := &mocks.MockQuicBaaS{
		OllamaServiceFunc: func() service.OllamaService {
			return &mocks.MockOllamaService{}
		},
	}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}

	client := pb.NewOllamaServiceClient(startBufconnModule(t, m))
	_, err = client.Health(context.Background(), &pb.OllamaHealthRequest{})
	requireUnavailable(t, err, "OllamaService (rag tag not compiled)")
}
