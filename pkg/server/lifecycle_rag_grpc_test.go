//go:build rag && raft && rest && quic && mcp && vector && qrdb_vector && !integration

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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
)

const ragGRPCService = "quicbaas.rag.v1.RAGService"

// TestRegisterRAGService_ServesRAGService proves the node serves
// quicbaas.rag.v1.RAGService over gRPC (#1611), with Query and BuildContext.
func TestRegisterRAGService_ServesRAGService(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	srv.registerRAGService(grpcServer)

	info, ok := grpcServer.GetServiceInfo()[ragGRPCService]
	require.True(t, ok, "RAGService must be registered")
	var methods []string
	for _, m := range info.Methods {
		methods = append(methods, m.Name)
	}
	assert.ElementsMatch(t, []string{"Query", "BuildContext"}, methods)
}

func TestRegisterRAGService_NilQuicBaaSService(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.quicBaaSService = nil

	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	srv.registerRAGService(grpcServer)
	_, ok := grpcServer.GetServiceInfo()[ragGRPCService]
	assert.False(t, ok)
}
