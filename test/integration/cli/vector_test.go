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

//go:build integration

// Package cli provides E2E integration tests for QuicBaaS CLI vector commands.
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Vector Namespace Command Definitions
// =============================================================================

// allTransportsSkip is used for unimplemented features that are not yet available
// on any transport. HybridSearch and SearchByMetadata are not implemented on the server.
var allTransportsSkip = []Transport{TransportGRPC, TransportREST, TransportQUIC, TransportMCP, TransportUnix}

// VectorNamespaceCreateCommand returns a command to create a namespace.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceCreateCommand(name string, dimension int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("vector namespace create %s", name),
		Args: []string{
			"vector", "namespace", "create", name,
			"--dimension", fmt.Sprintf("%d", dimension),
		},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceCreateWithConfigCommand creates namespace with index config.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceCreateWithConfigCommand(name string, dimension int, indexConfig map[string]string) CommandDef {
	args := []string{
		"vector", "namespace", "create", name,
		"--dimension", fmt.Sprintf("%d", dimension),
	}
	for k, v := range indexConfig {
		args = append(args, "--index-config", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector namespace create with config %s", name),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceCreateWithMetadataCommand creates namespace with metadata.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceCreateWithMetadataCommand(name string, dimension int, metadata map[string]string) CommandDef {
	args := []string{
		"vector", "namespace", "create", name,
		"--dimension", fmt.Sprintf("%d", dimension),
	}
	for k, v := range metadata {
		args = append(args, "--metadata", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector namespace create with metadata %s", name),
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceListCommand returns a command to list namespaces.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceListCommand() CommandDef {
	return CommandDef{
		Name:           "vector namespace list",
		Args:           []string{"vector", "namespace", "list"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceListWithPrefixCommand lists namespaces with prefix filter.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceListWithPrefixCommand(prefix string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector namespace list prefix=%s", prefix),
		Args:           []string{"vector", "namespace", "list", "--prefix", prefix},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceListWithLimitCommand lists namespaces with limit.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceListWithLimitCommand(limit int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector namespace list limit=%d", limit),
		Args:           []string{"vector", "namespace", "list", "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceListJSONCommand lists namespaces in JSON format.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceListJSONCommand() CommandDef {
	return CommandDef{
		Name:           "vector namespace list json",
		Args:           []string{"vector", "namespace", "list", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceInfoCommand returns a command to get namespace info.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceInfoCommand(name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector namespace info %s", name),
		Args:           []string{"vector", "namespace", "info", name},
		ExpectSuccess:  true,
		ExpectOutput:   []string{name},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceInfoJSONCommand gets namespace info in JSON format.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceInfoJSONCommand(name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector namespace info json %s", name),
		Args:           []string{"vector", "namespace", "info", name, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorNamespaceDeleteCommand returns a command to delete a namespace.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorNamespaceDeleteCommand(name string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector namespace delete %s", name),
		Args:           []string{"vector", "namespace", "delete", name},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// =============================================================================
// Vector CRUD Command Definitions
// =============================================================================

// VectorUpsertCommand returns a command to upsert a vector.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorUpsertCommand(namespace, id, vector string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector upsert %s/%s", namespace, id),
		Args:           []string{"vector", "upsert", namespace, id, "--vector", vector},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorUpsertWithMetadataCommand upserts a vector with metadata.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorUpsertWithMetadataCommand(namespace, id, vector string, metadata map[string]string) CommandDef {
	args := []string{"vector", "upsert", namespace, id, "--vector", vector}
	for k, v := range metadata {
		args = append(args, "--metadata", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector upsert with metadata %s/%s", namespace, id),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorBatchUpsertCommand returns a command for batch upsert.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorBatchUpsertCommand(namespace, file string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector batch-upsert %s from %s", namespace, file),
		Args:           []string{"vector", "batch-upsert", namespace, "--file", file},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorGetCommand returns a command to get a vector.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorGetCommand(namespace, id string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector get %s/%s", namespace, id),
		Args:           []string{"vector", "get", namespace, id},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorGetWithOptionsCommand gets a vector with include options.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorGetWithOptionsCommand(namespace, id string, includeVector, includeMetadata bool) CommandDef {
	args := []string{"vector", "get", namespace, id}
	args = append(args, fmt.Sprintf("--include-vector=%t", includeVector))
	args = append(args, fmt.Sprintf("--include-metadata=%t", includeMetadata))
	return CommandDef{
		Name:           fmt.Sprintf("vector get with options %s/%s", namespace, id),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorGetJSONCommand gets a vector in JSON format.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorGetJSONCommand(namespace, id string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector get json %s/%s", namespace, id),
		Args:           []string{"vector", "get", namespace, id, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorDeleteCommand returns a command to delete a vector.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorDeleteCommand(namespace, id string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector delete %s/%s", namespace, id),
		Args:           []string{"vector", "delete", namespace, id},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorBatchDeleteCommand returns a command for batch delete.
// Vector batch delete is supported via gRPC, Unix, and MCP transports.
// The REST SDK transport does not properly map vector batch operations.
func VectorBatchDeleteCommand(namespace string, ids []string) CommandDef {
	args := []string{"vector", "batch-delete", namespace}
	args = append(args, ids...)
	return CommandDef{
		Name:           fmt.Sprintf("vector batch-delete %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorBatchDeleteFromFileCommand batch deletes from file.
// Vector batch delete is supported via gRPC, Unix, and MCP transports.
// The REST SDK transport does not properly map vector batch operations.
func VectorBatchDeleteFromFileCommand(namespace, file string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector batch-delete from file %s", namespace),
		Args:           []string{"vector", "batch-delete", namespace, "--file", file},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorListCommand returns a command to list vectors.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorListCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector list %s", namespace),
		Args:           []string{"vector", "list", namespace},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorListWithOptionsCommand lists vectors with options.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorListWithOptionsCommand(namespace string, limit int, includeVectors, includeMetadata bool) CommandDef {
	args := []string{
		"vector", "list", namespace,
		"--limit", fmt.Sprintf("%d", limit),
		fmt.Sprintf("--include-vectors=%t", includeVectors),
		fmt.Sprintf("--include-metadata=%t", includeMetadata),
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector list with options %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorListWithPaginationCommand lists vectors with pagination.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorListWithPaginationCommand(namespace string, limit int, pageToken string) CommandDef {
	args := []string{"vector", "list", namespace, "--limit", fmt.Sprintf("%d", limit)}
	if pageToken != "" {
		args = append(args, "--page-token", pageToken)
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector list paginated %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorUpdateMetadataCommand updates vector metadata.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorUpdateMetadataCommand(namespace, id string, metadata map[string]string) CommandDef {
	args := []string{"vector", "update-metadata", namespace, id}
	for k, v := range metadata {
		args = append(args, "--metadata", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector update-metadata %s/%s", namespace, id),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorUpdateMetadataJSONCommand updates metadata using JSON.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorUpdateMetadataJSONCommand(namespace, id, metadataJSON string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector update-metadata json %s/%s", namespace, id),
		Args:           []string{"vector", "update-metadata", namespace, id, "--metadata-json", metadataJSON},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorUpdateMetadataReplaceCommand replaces all metadata.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorUpdateMetadataReplaceCommand(namespace, id string, metadata map[string]string) CommandDef {
	args := []string{"vector", "update-metadata", namespace, id, "--replace"}
	for k, v := range metadata {
		args = append(args, "--metadata", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector update-metadata replace %s/%s", namespace, id),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// =============================================================================
// Vector Search Command Definitions
// =============================================================================

// VectorSearchSimilarCommand searches for similar vectors.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorSearchSimilarCommand(namespace, vector string, k int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("vector search similar %s k=%d", namespace, k),
		Args: []string{
			"vector", "search", "similar", namespace,
			"--vector", vector,
			"--k", fmt.Sprintf("%d", k),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorSearchSimilarWithOptionsCommand searches with all options.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorSearchSimilarWithOptionsCommand(namespace, vector string, k, efSearch int, minScore float32, includeVectors, includeMetadata bool) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("vector search similar with options %s", namespace),
		Args: []string{
			"vector", "search", "similar", namespace,
			"--vector", vector,
			"--k", fmt.Sprintf("%d", k),
			"--ef-search", fmt.Sprintf("%d", efSearch),
			"--min-score", fmt.Sprintf("%.4f", minScore),
			fmt.Sprintf("--include-vectors=%t", includeVectors),
			fmt.Sprintf("--include-metadata=%t", includeMetadata),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorSearchSimilarJSONCommand searches with JSON output.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorSearchSimilarJSONCommand(namespace, vector string, k int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("vector search similar json %s", namespace),
		Args: []string{
			"vector", "search", "similar", namespace,
			"--vector", vector,
			"--k", fmt.Sprintf("%d", k),
			"--output", "json",
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorSearchByIDCommand searches by vector ID.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorSearchByIDCommand(namespace, id string, k int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("vector search by-id %s/%s", namespace, id),
		Args: []string{
			"vector", "search", "by-id", namespace, id,
			"--k", fmt.Sprintf("%d", k),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorSearchByIDWithOptionsCommand searches by ID with options.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorSearchByIDWithOptionsCommand(namespace, id string, k, efSearch int, minScore float32) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("vector search by-id with options %s/%s", namespace, id),
		Args: []string{
			"vector", "search", "by-id", namespace, id,
			"--k", fmt.Sprintf("%d", k),
			"--ef-search", fmt.Sprintf("%d", efSearch),
			"--min-score", fmt.Sprintf("%.4f", minScore),
		},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorHybridSearchCommand performs hybrid search.
// HybridSearch is not yet implemented on the server (method not implemented).
func VectorHybridSearchCommand(namespace, vector string, k int, filters []string) CommandDef {
	args := []string{
		"vector", "search", "hybrid", namespace,
		"--vector", vector,
		"--k", fmt.Sprintf("%d", k),
	}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector search hybrid %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: allTransportsSkip, // HybridSearch not implemented on server
	}
}

// VectorHybridSearchWithOptionsCommand performs hybrid search with all options.
// HybridSearch is not yet implemented on the server (method not implemented).
func VectorHybridSearchWithOptionsCommand(namespace, vector string, k, efSearch int, minScore float32, filters []string, filterOp, rerank string) CommandDef {
	args := []string{
		"vector", "search", "hybrid", namespace,
		"--vector", vector,
		"--k", fmt.Sprintf("%d", k),
		"--ef-search", fmt.Sprintf("%d", efSearch),
		"--min-score", fmt.Sprintf("%.4f", minScore),
	}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	if filterOp != "" {
		args = append(args, "--filter-op", filterOp)
	}
	if rerank != "" {
		args = append(args, "--rerank", rerank)
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector search hybrid with options %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: allTransportsSkip, // HybridSearch not implemented on server
	}
}

// VectorSearchByMetadataCommand searches by metadata only.
// SearchByMetadata is not yet implemented on the server (method not implemented).
func VectorSearchByMetadataCommand(namespace string, filters []string) CommandDef {
	args := []string{"vector", "search", "by-metadata", namespace}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector search by-metadata %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: allTransportsSkip, // SearchByMetadata not implemented on server
	}
}

// VectorSearchByMetadataWithOptionsCommand searches by metadata with options.
// SearchByMetadata is not yet implemented on the server (method not implemented).
func VectorSearchByMetadataWithOptionsCommand(namespace string, filters []string, filterOp string, limit, offset int) CommandDef {
	args := []string{"vector", "search", "by-metadata", namespace}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}
	if filterOp != "" {
		args = append(args, "--filter-op", filterOp)
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit))
	if offset > 0 {
		args = append(args, "--offset", fmt.Sprintf("%d", offset))
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector search by-metadata with options %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: allTransportsSkip, // SearchByMetadata not implemented on server
	}
}

// =============================================================================
// Vector Index Command Definitions
// =============================================================================

// VectorIndexRebuildCommand rebuilds the index.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorIndexRebuildCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector index rebuild %s", namespace),
		Args:           []string{"vector", "index", "rebuild", namespace},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorIndexRebuildAsyncCommand rebuilds index asynchronously.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorIndexRebuildAsyncCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector index rebuild async %s", namespace),
		Args:           []string{"vector", "index", "rebuild", namespace, "--async"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorIndexRebuildWithConfigCommand rebuilds with new config.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorIndexRebuildWithConfigCommand(namespace string, indexConfig map[string]string) CommandDef {
	args := []string{"vector", "index", "rebuild", namespace}
	for k, v := range indexConfig {
		args = append(args, "--index-config", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector index rebuild with config %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorIndexProgressCommand gets index rebuild progress.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorIndexProgressCommand(operationID string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector index progress %s", operationID),
		Args:           []string{"vector", "index", "progress", operationID},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorIndexProgressWithNamespaceCommand gets progress filtered by namespace.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorIndexProgressWithNamespaceCommand(operationID, namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector index progress with namespace %s", operationID),
		Args:           []string{"vector", "index", "progress", operationID, "--namespace", namespace},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorIndexStatsCommand gets index statistics.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorIndexStatsCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector index stats %s", namespace),
		Args:           []string{"vector", "index", "stats", namespace},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// =============================================================================
// Vector Admin Command Definitions
// =============================================================================

// VectorExportCommand exports vectors.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorExportCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector admin export %s", namespace),
		Args:           []string{"vector", "admin", "export", namespace},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorExportWithOptionsCommand exports with options.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorExportWithOptionsCommand(namespace, format, compression, output string, batchSize int) CommandDef {
	args := []string{
		"vector", "admin", "export", namespace,
		"--format", format,
		"--compression", compression,
		"--batch-size", fmt.Sprintf("%d", batchSize),
	}
	if output != "" {
		args = append(args, "--output", output)
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector admin export with options %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorImportCommand imports vectors.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorImportCommand(namespace, input string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector admin import %s", namespace),
		Args:           []string{"vector", "admin", "import", namespace, "--input", input},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorImportWithOptionsCommand imports with options.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorImportWithOptionsCommand(namespace, input, format string, overwrite bool) CommandDef {
	args := []string{
		"vector", "admin", "import", namespace,
		"--input", input,
		"--format", format,
	}
	if overwrite {
		args = append(args, "--overwrite")
	}
	return CommandDef{
		Name:           fmt.Sprintf("vector admin import with options %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorCompactCommand compacts a namespace.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorCompactCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector admin compact %s", namespace),
		Args:           []string{"vector", "admin", "compact", namespace},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorCompactAsyncCommand compacts asynchronously.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorCompactAsyncCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("vector admin compact async %s", namespace),
		Args:           []string{"vector", "admin", "compact", namespace, "--async"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// VectorCompactionProgressCommand gets compaction progress.
// Vector operations are supported via gRPC, Unix, and MCP transports.
func VectorCompactionProgressCommand(operationID, namespace string) CommandDef {
	args := []string{"vector", "admin", "progress"}
	if operationID != "" {
		args = append(args, "--operation-id", operationID)
	}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	return CommandDef{
		Name:           "vector admin progress",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: vectorTransportsToSkip,
	}
}

// =============================================================================
// Vector Help Command Definitions
// =============================================================================

// VectorHelpCommand returns a command to show vector help.
// Help commands are CLI-local and work on all transports.
func VectorHelpCommand() CommandDef {
	return CommandDef{
		Name:          "vector help",
		Args:          []string{"vector", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"namespace", "search", "upsert", "get"},
		Timeout:       10 * time.Second,
	}
}

// VectorNamespaceHelpCommand shows namespace help.
// Help commands are CLI-local and work on all transports.
func VectorNamespaceHelpCommand() CommandDef {
	return CommandDef{
		Name:          "vector namespace help",
		Args:          []string{"vector", "namespace", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"create", "delete", "list", "info"},
		Timeout:       10 * time.Second,
	}
}

// VectorSearchHelpCommand shows search help.
// Help commands are CLI-local and work on all transports.
func VectorSearchHelpCommand() CommandDef {
	return CommandDef{
		Name:          "vector search help",
		Args:          []string{"vector", "search", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"similar", "by-id", "hybrid", "by-metadata"},
		Timeout:       10 * time.Second,
	}
}

// VectorIndexHelpCommand shows index help.
// Help commands are CLI-local and work on all transports.
func VectorIndexHelpCommand() CommandDef {
	return CommandDef{
		Name:          "vector index help",
		Args:          []string{"vector", "index", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"rebuild", "progress", "stats"},
		Timeout:       10 * time.Second,
	}
}

// VectorAdminHelpCommand shows admin help.
// Help commands are CLI-local and work on all transports.
func VectorAdminHelpCommand() CommandDef {
	return CommandDef{
		Name:          "vector admin help",
		Args:          []string{"vector", "admin", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"export", "import", "compact"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Vector Error Command Definitions
// =============================================================================

// VectorNamespaceCreateMissingDimensionCommand tests missing dimension.
// Error validation is CLI-local and works on all transports.
func VectorNamespaceCreateMissingDimensionCommand(name string) CommandDef {
	return CommandDef{
		Name:          "vector namespace create missing dimension",
		Args:          []string{"vector", "namespace", "create", name},
		ExpectSuccess: false,
		ExpectError:   []string{"dimension"},
		Timeout:       10 * time.Second,
	}
}

// VectorNamespaceCreateZeroDimensionCommand tests zero dimension.
// Error validation is CLI-local and works on all transports.
func VectorNamespaceCreateZeroDimensionCommand(name string) CommandDef {
	return CommandDef{
		Name:          "vector namespace create zero dimension",
		Args:          []string{"vector", "namespace", "create", name, "--dimension", "0"},
		ExpectSuccess: false,
		ExpectError:   []string{"dimension"},
		Timeout:       10 * time.Second,
	}
}

// VectorUpsertMissingVectorCommand tests missing vector data.
// Error validation is CLI-local and works on all transports.
func VectorUpsertMissingVectorCommand(namespace, id string) CommandDef {
	return CommandDef{
		Name:          "vector upsert missing vector",
		Args:          []string{"vector", "upsert", namespace, id},
		ExpectSuccess: false,
		ExpectError:   []string{"vector"},
		Timeout:       10 * time.Second,
	}
}

// VectorSearchMissingVectorCommand tests search without vector.
// Error validation is CLI-local and works on all transports.
func VectorSearchMissingVectorCommand(namespace string) CommandDef {
	return CommandDef{
		Name:          "vector search similar missing vector",
		Args:          []string{"vector", "search", "similar", namespace, "--k", "10"},
		ExpectSuccess: false,
		ExpectError:   []string{"vector"},
		Timeout:       10 * time.Second,
	}
}

// VectorSearchByMetadataMissingFilterCommand tests search without filter.
// Error validation is CLI-local and works on all transports.
func VectorSearchByMetadataMissingFilterCommand(namespace string) CommandDef {
	return CommandDef{
		Name:          "vector search by-metadata missing filter",
		Args:          []string{"vector", "search", "by-metadata", namespace},
		ExpectSuccess: false,
		ExpectError:   []string{"filter"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestVectorHelp tests vector help commands.
func TestVectorHelp(t *testing.T) {
	RunForAllTransports(t, "VectorHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorHelpCommand())
	})

	RunForAllTransports(t, "VectorNamespaceHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorNamespaceHelpCommand())
	})

	RunForAllTransports(t, "VectorSearchHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorSearchHelpCommand())
	})

	RunForAllTransports(t, "VectorIndexHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorIndexHelpCommand())
	})

	RunForAllTransports(t, "VectorAdminHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorAdminHelpCommand())
	})
}

// TestVectorNamespaceLifecycle tests complete namespace lifecycle.
func TestVectorNamespaceLifecycle(t *testing.T) {
	RunForAllTransports(t, "NamespaceLifecycle", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("test_ns_%d", time.Now().UnixNano())

		// Create namespace
		createCmd := VectorNamespaceCreateCommand(nsName, 128)
		stdout, _, err := runner.Run(createCmd)
		if runner.IsSkipped(createCmd) {
			return
		}
		require.NoError(t, err)
		assert.Contains(t, stdout, nsName)

		// List namespaces
		listCmd := VectorNamespaceListCommand()
		stdout, _, err = runner.Run(listCmd)
		require.NoError(t, err)

		// Get namespace info
		infoCmd := VectorNamespaceInfoCommand(nsName)
		stdout, _, err = runner.Run(infoCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, nsName)

		// Get namespace info JSON
		infoJSONCmd := VectorNamespaceInfoJSONCommand(nsName)
		stdout, _, err = runner.Run(infoJSONCmd)
		require.NoError(t, err)
		var nsInfo map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &nsInfo)
		require.NoError(t, err)

		// Delete namespace
		deleteCmd := VectorNamespaceDeleteCommand(nsName)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// TestVectorNamespaceCreateOptions tests namespace creation with various options.
func TestVectorNamespaceCreateOptions(t *testing.T) {
	RunForAllTransports(t, "CreateWithIndexConfig", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("idx_cfg_%d", time.Now().UnixNano())
		indexConfig := map[string]string{
			"M":              "16",
			"efConstruction": "200",
		}

		cmd := VectorNamespaceCreateWithConfigCommand(nsName, 768, indexConfig)
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)

		// Cleanup
		cleanupVectorNamespace(t, runner, nsName)
	})

	RunForAllTransports(t, "CreateWithMetadata", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("meta_%d", time.Now().UnixNano())
		metadata := map[string]string{
			"model":   "all-MiniLM-L6-v2",
			"purpose": "search",
		}

		cmd := VectorNamespaceCreateWithMetadataCommand(nsName, 384, metadata)
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)

		cleanupVectorNamespace(t, runner, nsName)
	})
}

// TestVectorCRUD tests vector CRUD operations.
func TestVectorCRUD(t *testing.T) {
	RunForAllTransports(t, "VectorCRUD", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("crud_%d", time.Now().UnixNano())
		vectorID := "vec-001"
		vectorData := "0.1,0.2,0.3,0.4"

		// Create namespace first
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Upsert vector
		upsertCmd := VectorUpsertCommand(nsName, vectorID, vectorData)
		_, _, err = runner.Run(upsertCmd)
		require.NoError(t, err)

		// Get vector
		getCmd := VectorGetCommand(nsName, vectorID)
		_, _, err = runner.Run(getCmd)
		require.NoError(t, err)

		// Get vector JSON
		getJSONCmd := VectorGetJSONCommand(nsName, vectorID)
		stdout, _, err := runner.Run(getJSONCmd)
		require.NoError(t, err)
		var vectorResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &vectorResp)
		require.NoError(t, err)

		// List vectors
		listCmd := VectorListCommand(nsName)
		_, _, err = runner.Run(listCmd)
		require.NoError(t, err)

		// Delete vector
		deleteCmd := VectorDeleteCommand(nsName, vectorID)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// TestVectorUpsertWithMetadata tests upserting with metadata.
func TestVectorUpsertWithMetadata(t *testing.T) {
	RunForAllTransports(t, "UpsertWithMetadata", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("meta_%d", time.Now().UnixNano())
		vectorID := "vec-meta-001"
		vectorData := "0.1,0.2,0.3,0.4"
		metadata := map[string]string{
			"title":    "Test Document",
			"category": "testing",
		}

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Upsert with metadata
		upsertCmd := VectorUpsertWithMetadataCommand(nsName, vectorID, vectorData, metadata)
		_, _, err = runner.Run(upsertCmd)
		require.NoError(t, err)

		// Verify metadata
		getCmd := VectorGetJSONCommand(nsName, vectorID)
		stdout, _, err := runner.Run(getCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, "title")
		assert.Contains(t, stdout, "category")
	})
}

// TestVectorSearch tests vector search operations.
func TestVectorSearch(t *testing.T) {
	RunForAllTransports(t, "SearchOperations", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("search_%d", time.Now().UnixNano())
		queryVector := "1.0,0.0,0.0,0.0"

		// Create namespace and add vectors
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Add test vectors
		vectors := []struct {
			id   string
			data string
			meta map[string]string
		}{
			{"vec-1", "1.0,0.0,0.0,0.0", map[string]string{"type": "document"}},
			{"vec-2", "0.0,1.0,0.0,0.0", map[string]string{"type": "article"}},
			{"vec-3", "0.0,0.0,1.0,0.0", map[string]string{"type": "document"}},
		}

		for _, v := range vectors {
			upsertCmd := VectorUpsertWithMetadataCommand(nsName, v.id, v.data, v.meta)
			_, _, err = runner.Run(upsertCmd)
			require.NoError(t, err)
		}

		// Search similar
		searchCmd := VectorSearchSimilarCommand(nsName, queryVector, 3)
		_, _, err = runner.Run(searchCmd)
		require.NoError(t, err)

		// Search similar JSON
		searchJSONCmd := VectorSearchSimilarJSONCommand(nsName, queryVector, 3)
		stdout, _, err := runner.Run(searchJSONCmd)
		require.NoError(t, err)
		var searchResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &searchResp)
		require.NoError(t, err)

		// Search by ID
		searchByIDCmd := VectorSearchByIDCommand(nsName, "vec-1", 3)
		_, _, err = runner.Run(searchByIDCmd)
		require.NoError(t, err)

		// Hybrid search with filters
		hybridCmd := VectorHybridSearchCommand(nsName, queryVector, 10, []string{"type=document"})
		_, _, err = runner.Run(hybridCmd)
		require.NoError(t, err)

		// Search by metadata
		metaSearchCmd := VectorSearchByMetadataCommand(nsName, []string{"type=document"})
		_, _, err = runner.Run(metaSearchCmd)
		require.NoError(t, err)
	})
}

// TestVectorSearchOptions tests search with various options.
func TestVectorSearchOptions(t *testing.T) {
	RunForAllTransports(t, "SearchWithOptions", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("search_opts_%d", time.Now().UnixNano())
		queryVector := "1.0,0.0,0.0,0.0"

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Add vector
		upsertCmd := VectorUpsertCommand(nsName, "vec-1", queryVector)
		_, _, err = runner.Run(upsertCmd)
		require.NoError(t, err)

		// Search with all options
		searchCmd := VectorSearchSimilarWithOptionsCommand(nsName, queryVector, 10, 100, 0.5, true, true)
		_, _, err = runner.Run(searchCmd)
		require.NoError(t, err)

		// Search by ID with options
		searchByIDCmd := VectorSearchByIDWithOptionsCommand(nsName, "vec-1", 5, 50, 0.7)
		_, _, err = runner.Run(searchByIDCmd)
		require.NoError(t, err)

		// Hybrid search with options
		hybridCmd := VectorHybridSearchWithOptionsCommand(nsName, queryVector, 10, 100, 0.5, nil, "AND", "rrf")
		_, _, err = runner.Run(hybridCmd)
		// May fail if reranking not supported
		t.Logf("Hybrid search with rerank result: %v", err)

		// Metadata search with options
		metaCmd := VectorSearchByMetadataWithOptionsCommand(nsName, []string{"key=value"}, "OR", 50, 10)
		_, _, err = runner.Run(metaCmd)
		// May fail if no matching data
		t.Logf("Metadata search result: %v", err)
	})
}

// TestVectorList tests vector list with various options.
func TestVectorList(t *testing.T) {
	RunForAllTransports(t, "ListVectors", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("list_%d", time.Now().UnixNano())

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Add vectors
		for i := 0; i < 5; i++ {
			upsertCmd := VectorUpsertCommand(nsName, fmt.Sprintf("vec-%d", i), "0.1,0.2,0.3,0.4")
			_, _, err = runner.Run(upsertCmd)
			require.NoError(t, err)
		}

		// List with default options
		listCmd := VectorListCommand(nsName)
		_, _, err = runner.Run(listCmd)
		require.NoError(t, err)

		// List with options
		listOptsCmd := VectorListWithOptionsCommand(nsName, 10, false, true)
		_, _, err = runner.Run(listOptsCmd)
		require.NoError(t, err)

		// List with pagination
		listPageCmd := VectorListWithPaginationCommand(nsName, 2, "")
		_, _, err = runner.Run(listPageCmd)
		require.NoError(t, err)
	})
}

// TestVectorUpdateMetadata tests metadata update operations.
func TestVectorUpdateMetadata(t *testing.T) {
	RunForAllTransports(t, "UpdateMetadata", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("meta_update_%d", time.Now().UnixNano())
		vectorID := "vec-meta"
		vectorData := "0.1,0.2,0.3,0.4"

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Create vector with initial metadata
		initialMeta := map[string]string{"version": "1"}
		upsertCmd := VectorUpsertWithMetadataCommand(nsName, vectorID, vectorData, initialMeta)
		_, _, err = runner.Run(upsertCmd)
		require.NoError(t, err)

		// Update metadata (merge)
		updateMeta := map[string]string{"status": "active"}
		updateCmd := VectorUpdateMetadataCommand(nsName, vectorID, updateMeta)
		_, _, err = runner.Run(updateCmd)
		require.NoError(t, err)

		// Update metadata with JSON
		metadataJSON := `{"author":"test"}`
		updateJSONCmd := VectorUpdateMetadataJSONCommand(nsName, vectorID, metadataJSON)
		_, _, err = runner.Run(updateJSONCmd)
		require.NoError(t, err)

		// Replace all metadata
		replaceMeta := map[string]string{"replaced": "true"}
		replaceCmd := VectorUpdateMetadataReplaceCommand(nsName, vectorID, replaceMeta)
		_, _, err = runner.Run(replaceCmd)
		require.NoError(t, err)
	})
}

// TestVectorIndex tests index operations.
func TestVectorIndex(t *testing.T) {
	RunForAllTransports(t, "IndexOperations", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("index_%d", time.Now().UnixNano())

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Add some vectors
		for i := 0; i < 3; i++ {
			upsertCmd := VectorUpsertCommand(nsName, fmt.Sprintf("vec-%d", i), "0.1,0.2,0.3,0.4")
			_, _, err = runner.Run(upsertCmd)
			require.NoError(t, err)
		}

		// Get index stats
		statsCmd := VectorIndexStatsCommand(nsName)
		_, _, err = runner.Run(statsCmd)
		require.NoError(t, err)

		// Rebuild index async
		rebuildCmd := VectorIndexRebuildAsyncCommand(nsName)
		stdout, _, err := runner.Run(rebuildCmd)
		// Extract operation ID if available
		t.Logf("Rebuild output: %s", stdout)

		// Rebuild with config
		indexConfig := map[string]string{"M": "32"}
		rebuildConfigCmd := VectorIndexRebuildWithConfigCommand(nsName, indexConfig)
		_, _, err = runner.Run(rebuildConfigCmd)
		t.Logf("Rebuild with config result: %v", err)
	})
}

// TestVectorErrors tests error handling.
func TestVectorErrors(t *testing.T) {
	RunForAllTransports(t, "NamespaceCreateMissingDimension", func(t *testing.T, runner *TestRunner) {
		nsName := fmt.Sprintf("err_%d", time.Now().UnixNano())
		runner.RunAndAssert(VectorNamespaceCreateMissingDimensionCommand(nsName))
	})

	RunForAllTransports(t, "NamespaceCreateZeroDimension", func(t *testing.T, runner *TestRunner) {
		nsName := fmt.Sprintf("err_%d", time.Now().UnixNano())
		runner.RunAndAssert(VectorNamespaceCreateZeroDimensionCommand(nsName))
	})

	RunForAllTransports(t, "UpsertMissingVector", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorUpsertMissingVectorCommand("nonexistent", "vec-1"))
	})

	RunForAllTransports(t, "SearchMissingVector", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorSearchMissingVectorCommand("nonexistent"))
	})

	RunForAllTransports(t, "SearchByMetadataMissingFilter", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(VectorSearchByMetadataMissingFilterCommand("nonexistent"))
	})
}

// TestVectorOutputFormats tests all output formats.
func TestVectorOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("NamespaceList_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("NamespaceList_%s", format), func(t *testing.T, runner *TestRunner) {
				if !runner.RequireVectorAvailable() {
					return
				}
				cmd := CommandDef{
					Name:           fmt.Sprintf("vector namespace list %s", format),
					Args:           []string{"vector", "namespace", "list", "--output", format},
					ExpectSuccess:  true,
					Timeout:        30 * time.Second,
					RequiresUnseal: true,
					SkipTransports: vectorTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}
}

// TestVectorBatchDelete tests batch delete operations.
func TestVectorBatchDelete(t *testing.T) {
	RunForAllTransports(t, "BatchDelete", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireVectorAvailable() {
			return
		}
		nsName := fmt.Sprintf("batch_del_%d", time.Now().UnixNano())

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(nsName, 4)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		require.NoError(t, err)
		defer cleanupVectorNamespace(t, runner, nsName)

		// Add vectors
		ids := []string{"del-1", "del-2", "del-3"}
		for _, id := range ids {
			upsertCmd := VectorUpsertCommand(nsName, id, "0.1,0.2,0.3,0.4")
			_, _, err = runner.Run(upsertCmd)
			require.NoError(t, err)
		}

		// Batch delete - check if skipped for this transport
		deleteCmd := VectorBatchDeleteCommand(nsName, ids)
		if runner.IsSkipped(deleteCmd) {
			t.Logf("Batch delete not supported for this transport, skipping")
			return
		}
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// cleanupVectorNamespace attempts to delete a test namespace.
func cleanupVectorNamespace(t *testing.T, runner *TestRunner, nsName string) {
	t.Helper()

	deleteCmd := CommandDef{
		Name:           fmt.Sprintf("cleanup namespace %s", nsName),
		Args:           []string{"vector", "namespace", "delete", nsName},
		SkipTransports: vectorTransportsToSkip,
	}
	_, _, _ = runner.Run(deleteCmd)
}

// VectorNamespaceSuite returns commands for namespace lifecycle testing.
func VectorNamespaceSuite(nsName string, dimension int) []CommandDef {
	return []CommandDef{
		VectorNamespaceCreateCommand(nsName, dimension),
		VectorNamespaceListCommand(),
		VectorNamespaceInfoCommand(nsName),
		VectorNamespaceDeleteCommand(nsName),
	}
}

// VectorCRUDSuite returns commands for vector CRUD testing.
func VectorCRUDSuite(nsName, vectorID, vectorData string) []CommandDef {
	return []CommandDef{
		VectorUpsertCommand(nsName, vectorID, vectorData),
		VectorGetCommand(nsName, vectorID),
		VectorListCommand(nsName),
		VectorDeleteCommand(nsName, vectorID),
	}
}

// VectorSearchSuite returns commands for vector search testing.
func VectorSearchSuite(nsName, queryVector string) []CommandDef {
	return []CommandDef{
		VectorSearchSimilarCommand(nsName, queryVector, 10),
		VectorSearchByMetadataCommand(nsName, []string{"type=document"}),
	}
}

// VectorHelpSuite returns help commands for vector.
func VectorHelpSuite() []CommandDef {
	return []CommandDef{
		VectorHelpCommand(),
		VectorNamespaceHelpCommand(),
		VectorSearchHelpCommand(),
		VectorIndexHelpCommand(),
		VectorAdminHelpCommand(),
	}
}

// VectorErrorSuite returns error case commands for vector.
func VectorErrorSuite() []CommandDef {
	return []CommandDef{
		VectorNamespaceCreateMissingDimensionCommand("error-test"),
		VectorUpsertMissingVectorCommand("error-ns", "vec-1"),
		VectorSearchMissingVectorCommand("error-ns"),
		VectorSearchByMetadataMissingFilterCommand("error-ns"),
	}
}

// generateTestVector generates a test vector string with given dimensions.
func generateTestVector(dimensions int) string {
	values := make([]string, dimensions)
	for i := 0; i < dimensions; i++ {
		values[i] = fmt.Sprintf("0.%d", i%10)
	}
	return strings.Join(values, ",")
}
