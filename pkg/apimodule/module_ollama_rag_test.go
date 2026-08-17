//go:build rag

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

	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestModule_OllamaService_EnabledUnderRAGTag proves that, compiled with
// -tags rag, a composed facade whose Ollama() accessor returns a configured
// service.OllamaService actually serves — buildOllamaServer's rag-tagged
// implementation (module_ollama_rag.go) constructs the real
// grpcapi.NewOllamaServer handler, matching pkg/server/lifecycle_rag.go's
// registerOllamaService on the legacy gRPC transport.
func TestModule_OllamaService_EnabledUnderRAGTag(t *testing.T) {
	composed := &mocks.MockQuicBaaS{
		OllamaServiceFunc: func() service.OllamaService {
			return &mocks.MockOllamaService{}
		},
	}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)

	client := pb.NewOllamaServiceClient(startBufconnModule(t, m))
	resp, err := client.Health(context.Background(), &pb.OllamaHealthRequest{})
	require.NoError(t, err, "a configured OllamaService must serve under the rag build tag")
	require.NotNil(t, resp)
}
