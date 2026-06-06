//go:build mcp

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

// Package mcp implements Vector Database MCP integration types and handlers.
package mcp

import (
	"context"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// MCPServer extends the MCP server with vector database capabilities.
// It provides integration between the MCP protocol and QuicBaaS's vector operations.
type MCPServer struct {
	logger       *logger.Logger
	vectorClient pb.VectorDBClient
	adminClient  pb.VectorAdminClient
	handlers     map[string]func(context.Context, map[string]interface{}) (*ToolResponse, error)
	tools        []Tool
}

// ToolResponse represents the response from a tool execution in MCP format.
type ToolResponse struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError"`
}

// ContentItem represents a single content item in a tool response.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema defines the JSON schema for tool input parameters.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// Property defines a single property in the input schema.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     string   `json:"default,omitempty"`
}

// NewMCPServer creates a new MCP server instance with vector database support.
// The grpcAddr parameter specifies the address of the gRPC vector service.
func NewMCPServer(vectorClient pb.VectorDBClient, grpcAddr string, log *logger.Logger) *MCPServer {
	if log == nil {
		log, _ = logger.New(logger.LogConfig{
			Level:     "info",
			Format:    "json",
			Output:    "stdout",
			Component: "mcp-vector",
		})
	}

	server := &MCPServer{
		logger:   log,
		handlers: make(map[string]func(context.Context, map[string]interface{}) (*ToolResponse, error)),
		tools:    make([]Tool, 0),
	}

	// Set clients if provided
	if vectorClient != nil {
		server.vectorClient = vectorClient
	}

	// Register all vector tools and handlers
	server.registerVectorTools()

	return server
}

// registerVectorTools registers all vector database tool handlers.
func (s *MCPServer) registerVectorTools() {
	// Get all vector tool definitions
	vectorTools := getAllVectorTools()
	s.tools = append(s.tools, vectorTools...)

	// Register handlers for each tool
	s.handlers["qrdb_vector_upsert"] = s.handleUpsertVector
	s.handlers["qrdb_vector_batch_upsert"] = s.handleBatchUpsertVectors
	s.handlers["qrdb_vector_get"] = s.handleGetVector
	s.handlers["qrdb_vector_delete"] = s.handleDeleteVector
	s.handlers["qrdb_vector_update_metadata"] = s.handleUpdateMetadata
	s.handlers["qrdb_vector_batch_delete"] = s.handleBatchDeleteVectors
	s.handlers["qrdb_vector_search_similar"] = s.handleSearchSimilar
	s.handlers["qrdb_vector_search_by_id"] = s.handleSearchByID
	s.handlers["qrdb_vector_list"] = s.handleListVectors
	s.handlers["qrdb_vector_list_namespaces"] = s.handleListNamespaces
	s.handlers["qrdb_vector_create_namespace"] = s.handleCreateNamespace
	s.handlers["quicbaas_vector_update_namespace"] = s.handleUpdateNamespace
	s.handlers["qrdb_vector_delete_namespace"] = s.handleDeleteNamespace
	s.handlers["qrdb_vector_get_namespace_info"] = s.handleGetNamespaceInfo
	s.handlers["quicbaas_vector_get_stats"] = s.handleGetVectorStats
	s.handlers["quicbaas_vector_health"] = s.handleVectorHealth
	s.handlers["qrdb_vector_rebuild_index"] = s.handleRebuildIndex
	s.handlers["quicbaas_vector_get_index_progress"] = s.handleGetIndexProgress

	// VectorAdmin handlers
	s.handlers["quicbaas_vector_admin_get_cluster_stats"] = s.handleGetVectorClusterStats
	s.handlers["quicbaas_vector_admin_compact_namespace"] = s.handleCompactNamespace
	s.handlers["quicbaas_vector_admin_get_compaction_progress"] = s.handleGetCompactionProgress
	s.handlers["quicbaas_vector_admin_export_vectors"] = s.handleExportVectors
	s.handlers["quicbaas_vector_admin_import_vectors"] = s.handleImportVectors
}

// ExecuteTool executes a registered vector tool handler.
func (s *MCPServer) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResponse, error) {
	handler, exists := s.handlers[name]
	if !exists {
		return &ToolResponse{
			Content: []ContentItem{
				{
					Type: "text",
					Text: "Tool not found: " + name,
				},
			},
			IsError: true,
		}, nil
	}

	return handler(ctx, args)
}

// GetTools returns all registered vector tools.
func (s *MCPServer) GetTools() []Tool {
	return s.tools
}
