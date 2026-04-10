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

package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

type mockOllamaClient struct {
	OllamaHealthFunc         func(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error)
	OllamaListModelsFunc     func(ctx context.Context, req *pb.OllamaListModelsRequest) (*pb.OllamaListModelsResponse, error)
	OllamaGetModelInfoFunc   func(ctx context.Context, req *pb.OllamaGetModelInfoRequest) (*pb.OllamaGetModelInfoResponse, error)
	OllamaEmbedFunc          func(ctx context.Context, req *pb.OllamaEmbedRequest) (*pb.OllamaEmbedResponse, error)
	OllamaEmbedWithModelFunc func(ctx context.Context, req *pb.OllamaEmbedWithModelRequest) (*pb.OllamaEmbedResponse, error)
	OllamaEmbedBatchFunc     func(ctx context.Context, req *pb.OllamaEmbedBatchRequest) (*pb.OllamaEmbedBatchResponse, error)
	CloseFunc                func() error
}

func (m *mockOllamaClient) OllamaHealth(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error) {
	if m.OllamaHealthFunc != nil {
		return m.OllamaHealthFunc(ctx, req)
	}
	return &pb.OllamaHealthResponse{
		Available:   true,
		Version:     "0.1.24",
		BaseUrl:     "http://localhost:11434",
		LastChecked: timestamppb.Now(),
	}, nil
}

func (m *mockOllamaClient) OllamaListModels(ctx context.Context, req *pb.OllamaListModelsRequest) (*pb.OllamaListModelsResponse, error) {
	if m.OllamaListModelsFunc != nil {
		return m.OllamaListModelsFunc(ctx, req)
	}
	return &pb.OllamaListModelsResponse{
		Models: []*pb.OllamaModelInfo{
			{
				Name:       "nomic-embed-text",
				Size:       274000000,
				Digest:     "abc123def456789012345678901234567890",
				ModifiedAt: timestamppb.Now(),
			},
			{
				Name:       "llama2",
				Size:       7000000000,
				Digest:     "xyz789abc123456789012345678901234567",
				ModifiedAt: timestamppb.Now(),
			},
		},
		Count: 2,
	}, nil
}

func (m *mockOllamaClient) OllamaGetModelInfo(ctx context.Context, req *pb.OllamaGetModelInfoRequest) (*pb.OllamaGetModelInfoResponse, error) {
	if m.OllamaGetModelInfoFunc != nil {
		return m.OllamaGetModelInfoFunc(ctx, req)
	}
	return &pb.OllamaGetModelInfoResponse{
		Model: req.Model,
		Details: &pb.OllamaModelDetails{
			Family:            "nomic-bert",
			Format:            "gguf",
			ParameterSize:     "137M",
			QuantizationLevel: "F16",
			License:           "Apache 2.0",
		},
	}, nil
}

func (m *mockOllamaClient) OllamaEmbed(ctx context.Context, req *pb.OllamaEmbedRequest) (*pb.OllamaEmbedResponse, error) {
	if m.OllamaEmbedFunc != nil {
		return m.OllamaEmbedFunc(ctx, req)
	}
	return &pb.OllamaEmbedResponse{
		Model:          "nomic-embed-text",
		Dimensions:     768,
		Embedding:      []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7},
		ProcessingTime: durationpb.New(50 * time.Millisecond),
	}, nil
}

func (m *mockOllamaClient) OllamaEmbedWithModel(ctx context.Context, req *pb.OllamaEmbedWithModelRequest) (*pb.OllamaEmbedResponse, error) {
	if m.OllamaEmbedWithModelFunc != nil {
		return m.OllamaEmbedWithModelFunc(ctx, req)
	}
	return &pb.OllamaEmbedResponse{
		Model:          req.Model,
		Dimensions:     768,
		Embedding:      []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		ProcessingTime: durationpb.New(50 * time.Millisecond),
	}, nil
}

func (m *mockOllamaClient) OllamaEmbedBatch(ctx context.Context, req *pb.OllamaEmbedBatchRequest) (*pb.OllamaEmbedBatchResponse, error) {
	if m.OllamaEmbedBatchFunc != nil {
		return m.OllamaEmbedBatchFunc(ctx, req)
	}
	embeddings := make([]*pb.OllamaEmbeddingVector, len(req.Texts))
	for i := range req.Texts {
		embeddings[i] = &pb.OllamaEmbeddingVector{
			Values: []float32{0.1, 0.2, 0.3},
		}
	}
	return &pb.OllamaEmbedBatchResponse{
		Model:          req.Model,
		Count:          int32(len(req.Texts)),
		Dimensions:     768,
		Embeddings:     embeddings,
		ProcessingTime: durationpb.New(100 * time.Millisecond),
	}, nil
}

func (m *mockOllamaClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// =============================================================================
// Command Structure Tests
// =============================================================================

func TestNewOllamaCommands(t *testing.T) {
	cmd := newOllamaCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "ollama", cmd.Use)
	assert.Contains(t, cmd.Short, "Ollama integration")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "health", "health command should be registered")
	assert.Contains(t, commandNames, "models", "models command should be registered")
	assert.Contains(t, commandNames, "model", "model command should be registered")
	assert.Contains(t, commandNames, "embed", "embed command should be registered")
	assert.Contains(t, commandNames, "embed-batch", "embed-batch command should be registered")
}

func TestNewOllamaHealthCommand(t *testing.T) {
	cmd := newOllamaHealthCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "health", cmd.Use)
	assert.Contains(t, cmd.Short, "Check Ollama service health")

	// Verify flags exist
	assert.NotNil(t, cmd.Flags().Lookup("full-check"))
}

func TestNewOllamaModelsCommand(t *testing.T) {
	cmd := newOllamaModelsCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "models", cmd.Use)
	assert.Contains(t, cmd.Aliases, "list")
	assert.Contains(t, cmd.Short, "List available Ollama models")

	// Verify flags exist
	assert.NotNil(t, cmd.Flags().Lookup("prefix"))
	assert.NotNil(t, cmd.Flags().Lookup("list"))
}

func TestNewOllamaModelInfoCommand(t *testing.T) {
	cmd := newOllamaModelInfoCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "model info <model-name>", cmd.Use)
	assert.Contains(t, cmd.Short, "Get detailed information")
}

func TestNewOllamaEmbedCommand(t *testing.T) {
	cmd := newOllamaEmbedCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "embed <text>", cmd.Use)
	assert.Contains(t, cmd.Short, "Generate an embedding")

	// Verify flags exist
	assert.NotNil(t, cmd.Flags().Lookup("model"))
}

func TestNewOllamaEmbedBatchCommand(t *testing.T) {
	cmd := newOllamaEmbedBatchCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "embed-batch", cmd.Use)
	assert.Contains(t, cmd.Short, "Generate embeddings for multiple texts")

	// Verify flags exist
	assert.NotNil(t, cmd.Flags().Lookup("texts"))
	assert.NotNil(t, cmd.Flags().Lookup("model"))
}

// =============================================================================
// Run Command Tests
// =============================================================================

func TestRunOllamaHealthCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
		ollamaFullCheck = false
	}()
	outputFormat = "json"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaHealthCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaHealthCommand_TableFormat(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{
		OllamaHealthFunc: func(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error) {
			return &pb.OllamaHealthResponse{
				Available:   true,
				Version:     "0.1.24",
				BaseUrl:     "http://localhost:11434",
				LastChecked: timestamppb.Now(),
				Error:       "",
			}, nil
		},
	}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "table"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaHealthCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaHealthCommand_YAMLFormat(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "yaml"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaHealthCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaHealthCommand_Unavailable(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{
		OllamaHealthFunc: func(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error) {
			return &pb.OllamaHealthResponse{
				Available: false,
				BaseUrl:   "http://localhost:11434",
				Error:     "connection refused",
			}, nil
		},
	}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "table"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaHealthCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaHealthCommand_FactoryError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	ollamaClientFactory = func() (OllamaClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cmd := &cobra.Command{}
	err := runOllamaHealthCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRunOllamaHealthCommand_ServiceError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{
		OllamaHealthFunc: func(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error) {
			return nil, errors.Newf(errors.ErrInternal, "ollama.health", "service error")
		},
	}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldTimeout := timeout
	defer func() { timeout = oldTimeout }()
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaHealthCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestRunOllamaModelsCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
		ollamaNamePrefix = ""
	}()
	outputFormat = "json"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaModelsCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaModelsCommand_TableFormat(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "table"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaModelsCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaModelsCommand_FactoryError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	ollamaClientFactory = func() (OllamaClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cmd := &cobra.Command{}
	err := runOllamaModelsCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRunOllamaModelInfoCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "json"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaModelInfoCommand(cmd, []string{"nomic-embed-text"})
	assert.NoError(t, err)
}

func TestRunOllamaModelInfoCommand_TableFormat(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "table"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaModelInfoCommand(cmd, []string{"nomic-embed-text"})
	assert.NoError(t, err)
}

func TestRunOllamaModelInfoCommand_EmptyDetails(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{
		OllamaGetModelInfoFunc: func(ctx context.Context, req *pb.OllamaGetModelInfoRequest) (*pb.OllamaGetModelInfoResponse, error) {
			return &pb.OllamaGetModelInfoResponse{
				Model:   req.Model,
				Details: nil,
			}, nil
		},
	}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "table"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaModelInfoCommand(cmd, []string{"nomic-embed-text"})
	assert.NoError(t, err)
}

func TestRunOllamaModelInfoCommand_FactoryError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	ollamaClientFactory = func() (OllamaClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cmd := &cobra.Command{}
	err := runOllamaModelInfoCommand(cmd, []string{"nomic-embed-text"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRunOllamaEmbedCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
		ollamaModel = ""
	}()
	outputFormat = "json"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaEmbedCommand(cmd, []string{"Hello, world!"})
	assert.NoError(t, err)
}

func TestRunOllamaEmbedCommand_WithModel(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
		ollamaModel = ""
	}()
	outputFormat = "json"
	timeout = 30 * time.Second
	ollamaModel = "nomic-embed-text"

	cmd := &cobra.Command{}
	err := runOllamaEmbedCommand(cmd, []string{"Hello, world!"})
	assert.NoError(t, err)
}

func TestRunOllamaEmbedCommand_TableFormat(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
	}()
	outputFormat = "table"
	timeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaEmbedCommand(cmd, []string{"Hello, world!"})
	assert.NoError(t, err)
}

func TestRunOllamaEmbedCommand_EmptyText(t *testing.T) {
	cmd := &cobra.Command{}
	err := runOllamaEmbedCommand(cmd, []string{""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "text cannot be empty")
}

func TestRunOllamaEmbedCommand_FactoryError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	ollamaClientFactory = func() (OllamaClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cmd := &cobra.Command{}
	err := runOllamaEmbedCommand(cmd, []string{"test text"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRunOllamaEmbedBatchCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
		ollamaTexts = ""
		ollamaModel = ""
	}()
	outputFormat = "json"
	timeout = 30 * time.Second
	ollamaTexts = `["Hello", "World", "Test"]`

	cmd := &cobra.Command{}
	err := runOllamaEmbedBatchCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaEmbedBatchCommand_TableFormat(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	mockClient := &mockOllamaClient{}
	ollamaClientFactory = func() (OllamaClient, error) {
		return mockClient, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldFormat
		timeout = oldTimeout
		ollamaTexts = ""
	}()
	outputFormat = "table"
	timeout = 30 * time.Second
	ollamaTexts = `["Hello", "World"]`

	cmd := &cobra.Command{}
	err := runOllamaEmbedBatchCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaEmbedBatchCommand_InvalidJSON(t *testing.T) {
	// Reset globals
	defer func() { ollamaTexts = "" }()
	ollamaTexts = "invalid-json{"

	cmd := &cobra.Command{}
	err := runOllamaEmbedBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON array")
}

func TestRunOllamaEmbedBatchCommand_EmptyArray(t *testing.T) {
	// Reset globals
	defer func() { ollamaTexts = "" }()
	ollamaTexts = "[]"

	cmd := &cobra.Command{}
	err := runOllamaEmbedBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "texts array cannot be empty")
}

func TestRunOllamaEmbedBatchCommand_FactoryError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()

	ollamaClientFactory = func() (OllamaClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	// Reset globals
	defer func() { ollamaTexts = "" }()
	ollamaTexts = `["text1", "text2"]`

	cmd := &cobra.Command{}
	err := runOllamaEmbedBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// =============================================================================
// Format Response Tests
// =============================================================================

func TestFormatOllamaHealthResponse_AllFormats(t *testing.T) {
	resp := &pb.OllamaHealthResponse{
		Available:   true,
		Version:     "0.1.24",
		BaseUrl:     "http://localhost:11434",
		LastChecked: timestamppb.Now(),
		Error:       "",
	}

	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{
			name:    "json_format",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "yaml_format",
			format:  "yaml",
			wantErr: false,
		},
		{
			name:    "table_format",
			format:  "table",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatOllamaHealthResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatOllamaModelsResponse_AllFormats(t *testing.T) {
	resp := &pb.OllamaListModelsResponse{
		Models: []*pb.OllamaModelInfo{
			{
				Name:       "nomic-embed-text",
				Size:       274000000,
				Digest:     "abc123def456789012345678901234567890",
				ModifiedAt: timestamppb.Now(),
			},
		},
		Count: 1,
	}

	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{
			name:    "json_format",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "yaml_format",
			format:  "yaml",
			wantErr: false,
		},
		{
			name:    "table_format",
			format:  "table",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatOllamaModelsResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatOllamaModelInfoResponse_AllFormats(t *testing.T) {
	resp := &pb.OllamaGetModelInfoResponse{
		Model: "nomic-embed-text",
		Details: &pb.OllamaModelDetails{
			Family:            "nomic-bert",
			Format:            "gguf",
			ParameterSize:     "137M",
			QuantizationLevel: "F16",
			License:           "Apache 2.0",
		},
	}

	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{
			name:    "json_format",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "yaml_format",
			format:  "yaml",
			wantErr: false,
		},
		{
			name:    "table_format",
			format:  "table",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatOllamaModelInfoResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatOllamaEmbedResponse_AllFormats(t *testing.T) {
	resp := &pb.OllamaEmbedResponse{
		Model:          "nomic-embed-text",
		Dimensions:     768,
		Embedding:      []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7},
		ProcessingTime: durationpb.New(50 * time.Millisecond),
	}

	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{
			name:    "json_format",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "yaml_format",
			format:  "yaml",
			wantErr: false,
		},
		{
			name:    "table_format",
			format:  "table",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatOllamaEmbedResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatOllamaEmbedBatchResponse_AllFormats(t *testing.T) {
	resp := &pb.OllamaEmbedBatchResponse{
		Model:      "nomic-embed-text",
		Count:      2,
		Dimensions: 768,
		Embeddings: []*pb.OllamaEmbeddingVector{
			{Values: []float32{0.1, 0.2, 0.3}},
			{Values: []float32{0.4, 0.5, 0.6}},
		},
		ProcessingTime: durationpb.New(100 * time.Millisecond),
	}

	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{
			name:    "json_format",
			format:  "json",
			wantErr: false,
		},
		{
			name:    "yaml_format",
			format:  "yaml",
			wantErr: false,
		},
		{
			name:    "table_format",
			format:  "table",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatOllamaEmbedBatchResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// createOllamaClient Tests
// =============================================================================

func TestCreateOllamaClient_UnsupportedTransport(t *testing.T) {
	// Save original transport and restore after test
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "unsupported"

	client, err := createOllamaClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "unsupported transport")
}

// =============================================================================
// Command Help Tests
// =============================================================================

func TestOllamaCommandHelp(t *testing.T) {
	cmd := newOllamaCommands()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "ollama")
	assert.Contains(t, output, "Ollama integration")
}

func TestOllamaHealthCommandHelp(t *testing.T) {
	cmd := newOllamaHealthCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "health")
	assert.Contains(t, output, "--full-check")
}

func TestOllamaModelsCommandHelp(t *testing.T) {
	cmd := newOllamaModelsCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "models")
	assert.Contains(t, output, "--prefix")
}

func TestOllamaEmbedCommandHelp(t *testing.T) {
	cmd := newOllamaEmbedCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "embed")
	assert.Contains(t, output, "--model")
}
