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
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

const ollamaBufSize = 1024 * 1024

// =============================================================================
// Minimal Mock OllamaService Server
// =============================================================================

type minimalOllamaServer struct {
	pb.UnimplementedOllamaServiceServer
}

func (m *minimalOllamaServer) Health(_ context.Context, _ *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error) {
	return &pb.OllamaHealthResponse{
		Available: true,
		Version:   "0.3.0",
	}, nil
}

func (m *minimalOllamaServer) ListModels(_ context.Context, _ *pb.OllamaListModelsRequest) (*pb.OllamaListModelsResponse, error) {
	return &pb.OllamaListModelsResponse{
		Models: []*pb.OllamaModelInfo{
			{
				Name: "llama3",
			},
		},
		Count: 1,
	}, nil
}

func (m *minimalOllamaServer) GetModelInfo(_ context.Context, req *pb.OllamaGetModelInfoRequest) (*pb.OllamaGetModelInfoResponse, error) {
	return &pb.OllamaGetModelInfoResponse{
		Model: req.Model,
	}, nil
}

func (m *minimalOllamaServer) Embed(_ context.Context, _ *pb.OllamaEmbedRequest) (*pb.OllamaEmbedResponse, error) {
	return &pb.OllamaEmbedResponse{
		Embedding: []float32{0.1, 0.2, 0.3},
	}, nil
}

func (m *minimalOllamaServer) EmbedWithModel(_ context.Context, _ *pb.OllamaEmbedWithModelRequest) (*pb.OllamaEmbedResponse, error) {
	return &pb.OllamaEmbedResponse{
		Embedding: []float32{0.4, 0.5, 0.6},
	}, nil
}

func (m *minimalOllamaServer) EmbedBatch(_ context.Context, _ *pb.OllamaEmbedBatchRequest) (*pb.OllamaEmbedBatchResponse, error) {
	return &pb.OllamaEmbedBatchResponse{
		Embeddings: []*pb.OllamaEmbeddingVector{
			{Values: []float32{0.1, 0.2}},
			{Values: []float32{0.3, 0.4}},
		},
		Count: 2,
	}, nil
}

// =============================================================================
// Test Server Setup
// =============================================================================

func setupOllamaTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(ollamaBufSize)
	server := grpc.NewServer()
	pb.RegisterOllamaServiceServer(server, &minimalOllamaServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func setupConnectedOllamaClient(t *testing.T) (*Client, *grpc.Server) {
	t.Helper()
	server, lis := setupOllamaTestServer(t)
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := &Client{
		conn: conn,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      1,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}

	return client, server
}

// =============================================================================
// Ollama Client Connected Tests
// =============================================================================

func TestClientOllamaHealth_Connected(t *testing.T) {
	client, server := setupConnectedOllamaClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.OllamaHealth(context.Background(), &pb.OllamaHealthRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Available)
	assert.Equal(t, "0.3.0", resp.Version)
}

func TestClientOllamaListModels_Connected(t *testing.T) {
	client, server := setupConnectedOllamaClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.OllamaListModels(context.Background(), &pb.OllamaListModelsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Models, 1)
	assert.Equal(t, "llama3", resp.Models[0].Name)
}

func TestClientOllamaGetModelInfo_Connected(t *testing.T) {
	client, server := setupConnectedOllamaClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.OllamaGetModelInfo(context.Background(), &pb.OllamaGetModelInfoRequest{
		Model: "llama3",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "llama3", resp.Model)
}

func TestClientOllamaEmbed_Connected(t *testing.T) {
	client, server := setupConnectedOllamaClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.OllamaEmbed(context.Background(), &pb.OllamaEmbedRequest{
		Text: "hello world",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embedding, 3)
}

func TestClientOllamaEmbedWithModel_Connected(t *testing.T) {
	client, server := setupConnectedOllamaClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.OllamaEmbedWithModel(context.Background(), &pb.OllamaEmbedWithModelRequest{
		Model: "llama3",
		Text:  "hello world",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embedding, 3)
}

func TestClientOllamaEmbedBatch_Connected(t *testing.T) {
	client, server := setupConnectedOllamaClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.OllamaEmbedBatch(context.Background(), &pb.OllamaEmbedBatchRequest{
		Texts: []string{"hello", "world"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embeddings, 2)
}
