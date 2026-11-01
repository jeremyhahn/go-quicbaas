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
	root := newOllamaCommands()
	require.NotNil(t, root)
	assert.Equal(t, "ollama", root.Use)
	assert.Contains(t, root.Short, "Ollama integration")

	tests := []struct {
		name    string
		use     string
		short   string
		aliases []string
		flags   []string
	}{
		{name: "health", use: "health", short: "Check Ollama service health", flags: []string{"full-check"}},
		{name: "models", use: "models", short: "List available Ollama models", aliases: []string{"list"}, flags: []string{"prefix", "list"}},
		{name: "model", use: "model info <model-name>", short: "Get detailed information"},
		{name: "embed", use: "embed <text>", short: "Generate an embedding", flags: []string{"model"}},
		{name: "embed-batch", use: "embed-batch", short: "Generate embeddings for multiple texts", flags: []string{"texts", "model"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd *cobra.Command
			for _, sub := range root.Commands() {
				if sub.Name() == tt.name {
					cmd = sub
				}
			}
			require.NotNil(t, cmd, "subcommand %q must be registered", tt.name)
			assert.Equal(t, tt.use, cmd.Use)
			assert.Contains(t, cmd.Short, tt.short)
			for _, alias := range tt.aliases {
				assert.Contains(t, cmd.Aliases, alias)
			}
			for _, flag := range tt.flags {
				assert.NotNil(t, cmd.Flags().Lookup(flag), "flag --%s", flag)
			}
		})
	}
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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		ollamaFullCheck = false
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "table"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "yaml"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "table"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaHealthCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunOllamaCommands_FactoryError(t *testing.T) {
	origFactory := ollamaClientFactory
	defer func() { ollamaClientFactory = origFactory }()
	ollamaClientFactory = func() (OllamaClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	defer func() { ollamaTexts = "" }()
	ollamaTexts = `["text1", "text2"]`

	tests := []struct {
		name string
		run  func(*cobra.Command, []string) error
		args []string
	}{
		{name: "health", run: runOllamaHealthCommand},
		{name: "models", run: runOllamaModelsCommand},
		{name: "model info", run: runOllamaModelInfoCommand, args: []string{"nomic-embed-text"}},
		{name: "embed", run: runOllamaEmbedCommand, args: []string{"test text"}},
		{name: "embed-batch", run: runOllamaEmbedBatchCommand},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(&cobra.Command{}, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "connection refused")
		})
	}
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
	oldTimeout := globalTimeout
	defer func() { globalTimeout = oldTimeout }()
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		ollamaNamePrefix = ""
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "table"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaModelsCommand(cmd, []string{})
	assert.NoError(t, err)
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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "table"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "table"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runOllamaModelInfoCommand(cmd, []string{"nomic-embed-text"})
	assert.NoError(t, err)
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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		ollamaModel = ""
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		ollamaModel = ""
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second
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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "table"
	globalTimeout = 30 * time.Second

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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		ollamaTexts = ""
		ollamaModel = ""
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second
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
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		ollamaTexts = ""
	}()
	outputFormat = "table"
	globalTimeout = 30 * time.Second
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
