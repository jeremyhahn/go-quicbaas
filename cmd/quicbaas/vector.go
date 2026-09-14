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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

var (
	// Vector namespace flags.
	vectorDimension   uint32
	vectorIndexConfig map[string]string
	vectorMetadata    map[string]string

	// Vector operation flags.
	vectorData          string
	vectorMetadataInput map[string]string
	vectorFile          string

	// Vector search flags.
	vectorK              int
	vectorEfSearch       int
	vectorMinScore       float32
	vectorIncludeVectors bool
	vectorIncludeMeta    bool
	vectorFilters        []string
	vectorFilterOp       string
	vectorRerank         string
	vectorOffset         int

	// Vector index flags.
	vectorAsync     bool
	vectorNamespace string

	// compactTimeout is the deliberate per-command override for `vector admin
	// compact`, whose synchronous run is expected to outlast the root
	// --timeout. It is the ONE command with its own timeout variable; every
	// other verb reads globalTimeout, which the root --timeout binds (#246).
	compactTimeout time.Duration

	// Vector update metadata flags.
	vectorMetadataJSON string
	vectorMerge        bool
	vectorReplace      bool

	// Pagination flags.
	vectorLimit     int
	vectorPrefix    string
	vectorPageToken string
)

// createVectorClient creates the SDK vector client.
func createVectorClient() (vector.Client, error) {
	client, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return client.Vector(), nil
}

// newVectorCommands creates the vector command group.
func newVectorCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "vector",
		Aliases: []string{"vec"},
		Short:   "Vector similarity search commands",
		Long: `Vector similarity search commands for interacting with QuicBaaS vector storage.

These commands allow you to manage vector namespaces, store embeddings, and perform
similarity searches. All commands connect to a running QuicBaaS server via gRPC.`,
	}

	// Add persistent flags for all vector commands
	cmd.PersistentFlags().StringVar(&outputFormat, "output", "table", "Output format (json, yaml, table, raw)")

	// TLS flags
	cmd.PersistentFlags().BoolVar(&tlsEnabled, "tls", false, "Enable TLS")
	cmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS client certificate file")
	cmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS client key file")
	cmd.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA certificate file")

	// Connection flags
	cmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retry attempts")
	cmd.PersistentFlags().DurationVar(&retryBackoff, "retry-backoff", 100*time.Millisecond, "Initial retry backoff duration")

	// Add namespace subcommands
	namespaceCmd := &cobra.Command{
		Use:   "namespace",
		Short: "Manage vector namespaces",
	}
	namespaceCmd.AddCommand(newVectorNamespaceCreateCommand())
	namespaceCmd.AddCommand(newVectorNamespaceDeleteCommand())
	namespaceCmd.AddCommand(newVectorNamespaceListCommand())
	namespaceCmd.AddCommand(newVectorNamespaceInfoCommand())
	cmd.AddCommand(namespaceCmd)

	// Add index subcommands
	indexCmd := &cobra.Command{
		Use:   "index",
		Short: "Manage vector indexes",
	}
	indexCmd.AddCommand(newVectorIndexRebuildCommand())
	indexCmd.AddCommand(newVectorIndexProgressCommand())
	indexCmd.AddCommand(newVectorIndexStatsCommand())
	cmd.AddCommand(indexCmd)

	// Add vector operation commands
	cmd.AddCommand(newVectorUpsertCommand())
	cmd.AddCommand(newVectorBatchUpsertCommand())
	cmd.AddCommand(newVectorGetCommand())
	cmd.AddCommand(newVectorDeleteCommand())
	cmd.AddCommand(newVectorBatchDeleteCommand())
	cmd.AddCommand(newVectorListCommand())
	cmd.AddCommand(newVectorUpdateMetadataCommand())

	// Add search commands
	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search for vectors",
	}
	searchCmd.AddCommand(newVectorSearchSimilarCommand())
	searchCmd.AddCommand(newVectorSearchByIDCommand())
	searchCmd.AddCommand(newVectorHybridSearchCommand())
	searchCmd.AddCommand(newVectorSearchByMetadataCommand())
	cmd.AddCommand(searchCmd)

	// Add admin commands
	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Admin operations (export, import, compact, etc.)",
	}
	adminCmd.AddCommand(newVectorExportCommand())
	adminCmd.AddCommand(newVectorImportCommand())
	adminCmd.AddCommand(newVectorCompactCommand())
	adminCmd.AddCommand(newVectorCompactionProgressCommand())
	cmd.AddCommand(adminCmd)

	return cmd
}

// =============================================================================
// Namespace Management Commands
// =============================================================================

// newVectorNamespaceCreateCommand creates the namespace create command.
func newVectorNamespaceCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new vector namespace",
		Long: `Create a new vector namespace with specified dimensions.

All vectors in a namespace must have the same dimensionality.

Examples:
  # Create a namespace for 1536-dimensional OpenAI embeddings
  quicbaas vector namespace create embeddings --dimension 1536

  # Create with custom index configuration
  quicbaas vector namespace create embeddings --dimension 768 --index-config M=16,efConstruction=200

  # Create with metadata
  quicbaas vector namespace create embeddings --dimension 384 --metadata model=all-MiniLM-L6-v2,purpose=search`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorNamespaceCreateCommand,
	}

	cmd.Flags().Uint32Var(&vectorDimension, "dimension", 0, "Vector dimensionality (required)")
	cmd.Flags().StringToStringVar(&vectorIndexConfig, "index-config", nil, "Index configuration as key=value pairs")
	cmd.Flags().StringToStringVar(&vectorMetadata, "metadata", nil, "Namespace metadata as key=value pairs")

	if err := cmd.MarkFlagRequired("dimension"); err != nil {
		stdlog.Fatalf("failed to mark 'dimension' flag as required: %v", err)
	}

	return cmd
}

// runVectorNamespaceCreateCommand executes the namespace create command.
func runVectorNamespaceCreateCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	if vectorDimension == 0 {
		return errors.Newf(errors.ErrInvalidInput, "namespace create", "dimension must be greater than 0")
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Create SDK request
	req := &vector.CreateNamespaceRequest{
		Config: &vector.NamespaceConfig{
			Namespace:   namespace,
			Dimensions:  int32(vectorDimension),
			IndexParams: vectorIndexConfig,
			Metadata:    vectorMetadata,
		},
	}

	resp, err := vectorClient.CreateNamespace(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorNamespaceCreateResponse(resp)
}

// newVectorNamespaceDeleteCommand creates the namespace delete command.
func newVectorNamespaceDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a vector namespace",
		Long: `Delete a vector namespace and all its vectors.

WARNING: This operation cannot be undone!

Examples:
  quicbaas vector namespace delete embeddings`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorNamespaceDeleteCommand,
	}

	return cmd
}

// runVectorNamespaceDeleteCommand executes the namespace delete command.
func runVectorNamespaceDeleteCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.DeleteNamespaceRequest{
		Namespace: namespace,
	}

	resp, err := vectorClient.DeleteNamespace(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorNamespaceDeleteResponse(resp)
}

// newVectorNamespaceListCommand creates the namespace list command.
func newVectorNamespaceListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all vector namespaces",
		Long: `List all available vector namespaces.

Examples:
  # List all namespaces
  quicbaas vector namespace list

  # List namespaces with prefix
  quicbaas vector namespace list --prefix embed

  # List with pagination
  quicbaas vector namespace list --limit 10`,
		RunE: runVectorNamespaceListCommand,
	}

	cmd.Flags().StringVar(&vectorPrefix, "prefix", "", "Filter namespaces by prefix")
	cmd.Flags().IntVar(&vectorLimit, "limit", 100, "Maximum number of namespaces to return")

	return cmd
}

// runVectorNamespaceListCommand executes the namespace list command.
func runVectorNamespaceListCommand(cmd *cobra.Command, args []string) error {
	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.ListNamespacesRequest{
		Prefix:   vectorPrefix,
		PageSize: int32(vectorLimit),
	}

	resp, err := vectorClient.ListNamespaces(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorNamespaceListResponse(resp)
}

// newVectorNamespaceInfoCommand creates the namespace info command.
func newVectorNamespaceInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Get information about a vector namespace",
		Long: `Retrieve detailed information about a specific vector namespace.

Examples:
  quicbaas vector namespace info embeddings`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorNamespaceInfoCommand,
	}

	return cmd
}

// runVectorNamespaceInfoCommand executes the namespace info command.
func runVectorNamespaceInfoCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.GetNamespaceInfoRequest{
		Namespace: namespace,
	}

	resp, err := vectorClient.GetNamespaceInfo(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorNamespaceInfoResponse(resp)
}

// =============================================================================
// Vector CRUD Commands
// =============================================================================

// newVectorUpsertCommand creates the upsert command.
func newVectorUpsertCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upsert <namespace> <id>",
		Short: "Insert or update a vector",
		Long: `Insert or update a vector with optional metadata.

The vector should be provided as comma-separated floats.

Examples:
  # Insert a 3-dimensional vector
  quicbaas vector upsert embeddings vec1 --vector 0.1,0.2,0.3

  # Insert with metadata
  quicbaas vector upsert embeddings vec1 --vector 0.1,0.2,0.3 --metadata title=example,author=john

  # Insert a 1536-dimensional OpenAI embedding (truncated for example)
  quicbaas vector upsert embeddings vec42 --vector 0.001,0.002,...`,
		Args: cobra.ExactArgs(2),
		RunE: runVectorUpsertCommand,
	}

	cmd.Flags().StringVar(&vectorData, "vector", "", "Vector as comma-separated floats (required)")
	cmd.Flags().StringToStringVar(&vectorMetadataInput, "metadata", nil, "Vector metadata as key=value pairs")

	if err := cmd.MarkFlagRequired("vector"); err != nil {
		stdlog.Fatalf("failed to mark 'vector' flag as required: %v", err)
	}

	return cmd
}

// runVectorUpsertCommand executes the upsert command.
func runVectorUpsertCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]
	id := args[1]

	// Parse vector data
	vectorValues, err := parseVectorData(vectorData)
	if err != nil {
		return err
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Create SDK request
	req := &vector.UpsertRequest{
		Vector: &vector.VectorEntity{
			ID:        id,
			Namespace: namespace,
			Vector:    vectorValues,
			Metadata:  vectorMetadataInput,
		},
	}

	resp, err := vectorClient.Upsert(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorUpsertResponse(resp)
}

// newVectorBatchUpsertCommand creates the batch-upsert command.
func newVectorBatchUpsertCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch-upsert <namespace>",
		Short: "Insert or update multiple vectors",
		Long: `Insert or update multiple vectors from a JSON file.

The JSON file should contain an array of vector objects with id, vector, and optional metadata.

Examples:
  # Batch upsert from file
  quicbaas vector batch-upsert embeddings --file vectors.json

File format:
  [
    {
      "id": "vec1",
      "vector": [0.1, 0.2, 0.3],
      "metadata": {"title": "example"}
    },
    {
      "id": "vec2",
      "vector": [0.4, 0.5, 0.6]
    }
  ]`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorBatchUpsertCommand,
	}

	cmd.Flags().StringVar(&vectorFile, "file", "", "JSON file containing vectors (required)")

	if err := cmd.MarkFlagRequired("file"); err != nil {
		stdlog.Fatalf("failed to mark 'file' flag as required: %v", err)
	}

	return cmd
}

// runVectorBatchUpsertCommand executes the batch-upsert command.
func runVectorBatchUpsertCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	// Read and parse JSON file
	data, err := os.ReadFile(vectorFile)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "batch-upsert", "failed to read file: %s", vectorFile)
	}

	var vectors []struct {
		ID       string                 `json:"id"`
		Vector   []float32              `json:"vector"`
		Metadata map[string]interface{} `json:"metadata,omitempty"`
	}

	if err := json.Unmarshal(data, &vectors); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "batch-upsert", "failed to parse JSON")
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Convert to SDK entities
	entities := make([]*vector.VectorEntity, len(vectors))
	for i, v := range vectors {
		// Convert metadata to string map
		metadata := make(map[string]string)
		for k, val := range v.Metadata {
			metadata[k] = fmt.Sprintf("%v", val)
		}

		entities[i] = &vector.VectorEntity{
			ID:        v.ID,
			Namespace: namespace,
			Vector:    v.Vector,
			Metadata:  metadata,
		}
	}

	req := &vector.BatchUpsertRequest{
		Vectors: entities,
	}

	resp, err := vectorClient.BatchUpsert(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorBatchUpsertResponse(resp)
}

// newVectorGetCommand creates the get command.
func newVectorGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <namespace> <id>",
		Short: "Retrieve a vector by ID",
		Long: `Retrieve a vector by ID with optional vector and metadata.

Examples:
  # Get vector with all data
  quicbaas vector get embeddings vec1

  # Get only metadata (exclude vector)
  quicbaas vector get embeddings vec1 --no-vector

  # Get as JSON
  quicbaas vector get embeddings vec1 --output json`,
		Args: cobra.ExactArgs(2),
		RunE: runVectorGetCommand,
	}

	cmd.Flags().BoolVar(&vectorIncludeVectors, "include-vector", true, "Include vector data in response")
	cmd.Flags().BoolVar(&vectorIncludeMeta, "include-metadata", true, "Include metadata in response")

	return cmd
}

// runVectorGetCommand executes the get command.
func runVectorGetCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]
	id := args[1]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.GetRequest{
		Namespace:       namespace,
		ID:              id,
		IncludeVector:   vectorIncludeVectors,
		IncludeMetadata: vectorIncludeMeta,
	}

	resp, err := vectorClient.Get(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorGetResponse(resp)
}

// newVectorDeleteCommand creates the delete command.
func newVectorDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <namespace> <id>",
		Short: "Delete a vector",
		Long: `Delete a vector from a namespace.

Examples:
  quicbaas vector delete embeddings vec1`,
		Args: cobra.ExactArgs(2),
		RunE: runVectorDeleteCommand,
	}

	return cmd
}

// runVectorDeleteCommand executes the delete command.
func runVectorDeleteCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]
	id := args[1]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.DeleteRequest{
		Namespace: namespace,
		ID:        id,
	}

	resp, err := vectorClient.Delete(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorDeleteResponse(resp)
}

// newVectorBatchDeleteCommand creates the batch-delete command.
func newVectorBatchDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch-delete <namespace> <ids...>",
		Short: "Delete multiple vectors",
		Long: `Delete multiple vectors by their IDs.

Examples:
  # Delete multiple vectors
  quicbaas vector batch-delete embeddings vec1 vec2 vec3

  # Delete from IDs in file (one per line)
  quicbaas vector batch-delete embeddings --file ids.txt`,
		Args: cobra.MinimumNArgs(1),
		RunE: runVectorBatchDeleteCommand,
	}

	cmd.Flags().StringVar(&vectorFile, "file", "", "File containing vector IDs (one per line)")

	return cmd
}

// runVectorBatchDeleteCommand executes the batch-delete command.
func runVectorBatchDeleteCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	var ids []string
	if vectorFile != "" {
		// Read IDs from file
		data, err := os.ReadFile(vectorFile)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "batch-delete", "failed to read file: %s", vectorFile)
		}
		ids = strings.Split(strings.TrimSpace(string(data)), "\n")
	} else {
		// Use IDs from command line args
		if len(args) < 2 {
			return errors.Newf(errors.ErrInvalidInput, "batch-delete", "at least one vector ID required")
		}
		ids = args[1:]
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.BatchDeleteRequest{
		Namespace: namespace,
		IDs:       ids,
	}

	resp, err := vectorClient.BatchDelete(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorBatchDeleteResponse(resp)
}

// newVectorListCommand creates the list command.
func newVectorListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <namespace>",
		Short: "List vectors in a namespace",
		Long: `List all vectors in a namespace with pagination.

Examples:
  # List first 100 vectors
  quicbaas vector list embeddings

  # List with pagination
  quicbaas vector list embeddings --limit 50

  # List excluding vector data
  quicbaas vector list embeddings --include-vectors=false`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorListCommand,
	}

	cmd.Flags().IntVar(&vectorLimit, "limit", 100, "Maximum number of vectors to return")
	cmd.Flags().StringVar(&vectorPageToken, "page-token", "", "Page token for pagination")
	cmd.Flags().BoolVar(&vectorIncludeVectors, "include-vectors", false, "Include vector data in response")
	cmd.Flags().BoolVar(&vectorIncludeMeta, "include-metadata", true, "Include metadata in response")

	return cmd
}

// runVectorListCommand executes the list command.
func runVectorListCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.ListVectorsRequest{
		Namespace:       namespace,
		PageSize:        int32(vectorLimit),
		PageToken:       vectorPageToken,
		IncludeVectors:  vectorIncludeVectors,
		IncludeMetadata: vectorIncludeMeta,
	}

	resp, err := vectorClient.List(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorListResponse(resp)
}

// newVectorUpdateMetadataCommand creates the update-metadata command.
func newVectorUpdateMetadataCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-metadata <namespace> <id>",
		Short: "Update vector metadata",
		Long: `Update the metadata of an existing vector.

By default, new metadata is merged with existing metadata.
Use --replace to completely replace the metadata.

Examples:
  # Add/update metadata fields
  quicbaas vector update-metadata embeddings vec1 --metadata key1=value1,key2=value2

  # Replace all metadata
  quicbaas vector update-metadata embeddings vec1 --metadata key1=value1 --replace

  # Update with JSON metadata
  quicbaas vector update-metadata embeddings vec1 --metadata-json '{"key1":"value1","key2":"value2"}'`,
		Args: cobra.ExactArgs(2),
		RunE: runVectorUpdateMetadataCommand,
	}

	cmd.Flags().StringToStringVar(&vectorMetadata, "metadata", nil, "Metadata as key=value pairs")
	cmd.Flags().StringVar(&vectorMetadataJSON, "metadata-json", "", "Metadata as JSON string")
	cmd.Flags().BoolVar(&vectorMerge, "merge", true, "Merge with existing metadata (default true)")
	cmd.Flags().BoolVar(&vectorReplace, "replace", false, "Replace existing metadata (opposite of --merge)")

	return cmd
}

// runVectorUpdateMetadataCommand executes the update-metadata command.
func runVectorUpdateMetadataCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]
	id := args[1]

	// Build metadata from either flag format
	metadata := make(map[string]string)

	// Check JSON metadata first
	if vectorMetadataJSON != "" {
		if err := json.Unmarshal([]byte(vectorMetadataJSON), &metadata); err != nil {
			return NewInvalidMetadataJSONError("update-metadata", err)
		}
	}

	// Merge key=value pairs (they take precedence)
	for k, v := range vectorMetadata {
		metadata[k] = v
	}

	if len(metadata) == 0 {
		return NewNoMetadataProvidedError("update-metadata")
	}

	// Determine merge behavior
	merge := vectorMerge && !vectorReplace

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.UpdateMetadataRequest{
		Namespace: namespace,
		ID:        id,
		Metadata:  metadata,
		Merge:     merge,
	}

	resp, err := vectorClient.UpdateMetadata(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorUpdateMetadataResponse(resp)
}

// formatVectorUpdateMetadataResponse formats the update metadata response.
func formatVectorUpdateMetadataResponse(resp *vector.UpdateMetadataResponse) error {
	if resp.Success {
		fmt.Printf("Metadata updated successfully (version: %d)\n", resp.Version)
	} else {
		fmt.Println("Failed to update metadata")
	}
	return nil
}

// =============================================================================
// Vector Search Commands
// =============================================================================

// newVectorSearchSimilarCommand creates the search similar command.
func newVectorSearchSimilarCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "similar <namespace>",
		Short: "Search for similar vectors",
		Long: `Perform k-nearest neighbor similarity search.

The query vector should be provided as comma-separated floats.

Examples:
  # Search for 10 most similar vectors
  quicbaas vector search similar embeddings --vector 0.1,0.2,0.3 --k 10

  # Search with minimum score threshold
  quicbaas vector search similar embeddings --vector 0.1,0.2,0.3 --k 10 --min-score 0.8

  # Search with custom HNSW efSearch parameter
  quicbaas vector search similar embeddings --vector 0.1,0.2,0.3 --k 10 --ef-search 100

  # Search excluding vector data (metadata only)
  quicbaas vector search similar embeddings --vector 0.1,0.2,0.3 --k 10 --include-vectors=false`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorSearchSimilarCommand,
	}

	cmd.Flags().StringVar(&vectorData, "vector", "", "Query vector as comma-separated floats (required)")
	cmd.Flags().IntVar(&vectorK, "k", 10, "Number of nearest neighbors to return")
	cmd.Flags().IntVar(&vectorEfSearch, "ef-search", 100, "HNSW efSearch parameter (higher = more accurate, slower)")
	cmd.Flags().Float32Var(&vectorMinScore, "min-score", 0, "Minimum similarity score threshold")
	cmd.Flags().BoolVar(&vectorIncludeVectors, "include-vectors", false, "Include vector data in results")
	cmd.Flags().BoolVar(&vectorIncludeMeta, "include-metadata", true, "Include metadata in results")

	if err := cmd.MarkFlagRequired("vector"); err != nil {
		stdlog.Fatalf("failed to mark 'vector' flag as required: %v", err)
	}

	return cmd
}

// runVectorSearchSimilarCommand executes the search similar command.
func runVectorSearchSimilarCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	// Parse vector data
	queryVector, err := parseVectorData(vectorData)
	if err != nil {
		return err
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     queryVector,
		K:               int32(vectorK),
		EfSearch:        int32(vectorEfSearch),
		MinScore:        vectorMinScore,
		IncludeVectors:  vectorIncludeVectors,
		IncludeMetadata: vectorIncludeMeta,
	}

	resp, err := vectorClient.SearchSimilar(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorSearchSimilarResponse(resp)
}

// newVectorSearchByIDCommand creates the search by-id command.
func newVectorSearchByIDCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "by-id <namespace> <id>",
		Short: "Find similar vectors to an existing vector",
		Long: `Find vectors similar to an existing vector identified by ID.

This is a convenience method that retrieves the vector and performs similarity search.

Examples:
  # Find similar vectors
  quicbaas vector search by-id embeddings vec1 --k 10

  # Find similar with minimum score
  quicbaas vector search by-id embeddings vec1 --k 10 --min-score 0.7`,
		Args: cobra.ExactArgs(2),
		RunE: runVectorSearchByIDCommand,
	}

	cmd.Flags().IntVar(&vectorK, "k", 10, "Number of nearest neighbors to return")
	cmd.Flags().IntVar(&vectorEfSearch, "ef-search", 100, "HNSW efSearch parameter")
	cmd.Flags().Float32Var(&vectorMinScore, "min-score", 0, "Minimum similarity score threshold")
	cmd.Flags().BoolVar(&vectorIncludeVectors, "include-vectors", false, "Include vector data in results")
	cmd.Flags().BoolVar(&vectorIncludeMeta, "include-metadata", true, "Include metadata in results")

	return cmd
}

// runVectorSearchByIDCommand executes the search by-id command.
func runVectorSearchByIDCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]
	id := args[1]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.SearchByIDRequest{
		Namespace:       namespace,
		ID:              id,
		K:               int32(vectorK),
		EfSearch:        int32(vectorEfSearch),
		MinScore:        vectorMinScore,
		IncludeVectors:  vectorIncludeVectors,
		IncludeMetadata: vectorIncludeMeta,
		IncludeSelf:     false,
	}

	resp, err := vectorClient.SearchByID(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorSearchByIDResponse(resp)
}

// newVectorHybridSearchCommand creates the hybrid search command.
func newVectorHybridSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hybrid <namespace>",
		Short: "Hybrid search combining similarity and metadata filtering",
		Long: `Perform hybrid search that combines k-NN similarity search with metadata filtering.

First performs similarity search, then filters results by metadata criteria.
Optionally applies reranking for improved relevance.

Examples:
  # Hybrid search with metadata filter
  quicbaas vector search hybrid embeddings --vector 0.1,0.2,0.3 --k 10 --filter type=document

  # Multiple filters with AND logic
  quicbaas vector search hybrid embeddings --vector 0.1,0.2,0.3 --k 20 --filter type=article --filter category=tech

  # With reranking algorithm
  quicbaas vector search hybrid embeddings --vector 0.1,0.2,0.3 --k 10 --filter type=doc --rerank rrf

  # With minimum score threshold
  quicbaas vector search hybrid embeddings --vector 0.1,0.2,0.3 --k 10 --min-score 0.8 --filter status=active`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorHybridSearchCommand,
	}

	cmd.Flags().StringVar(&vectorData, "vector", "", "Query vector as comma-separated floats (required)")
	cmd.Flags().IntVar(&vectorK, "k", 10, "Number of nearest neighbors to return")
	cmd.Flags().IntVar(&vectorEfSearch, "ef-search", 100, "HNSW efSearch parameter")
	cmd.Flags().Float32Var(&vectorMinScore, "min-score", 0, "Minimum similarity score threshold")
	cmd.Flags().StringArrayVar(&vectorFilters, "filter", nil, "Metadata filters as key=value pairs (repeatable)")
	cmd.Flags().StringVar(&vectorFilterOp, "filter-op", "AND", "Filter operator (AND or OR)")
	cmd.Flags().StringVar(&vectorRerank, "rerank", "", "Reranking algorithm (none, rrf, cross_encoder, colbert)")
	cmd.Flags().BoolVar(&vectorIncludeVectors, "include-vectors", false, "Include vector data in results")
	cmd.Flags().BoolVar(&vectorIncludeMeta, "include-metadata", true, "Include metadata in results")

	if err := cmd.MarkFlagRequired("vector"); err != nil {
		stdlog.Fatalf("failed to mark 'vector' flag as required: %v", err)
	}

	return cmd
}

// runVectorHybridSearchCommand executes the hybrid search command.
func runVectorHybridSearchCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	// Parse vector data
	queryVector, err := parseVectorData(vectorData)
	if err != nil {
		return err
	}

	// Parse metadata filters
	filters := make([]*vector.MetadataFilter, 0, len(vectorFilters))
	for _, f := range vectorFilters {
		key, value, found := parseFilter(f)
		if !found {
			return NewInvalidFilterFormatError("hybrid search", f)
		}
		filters = append(filters, &vector.MetadataFilter{
			Key:      key,
			Operator: vector.FilterOperatorEquals,
			Value:    value,
		})
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.HybridSearchRequest{
		Namespace:          namespace,
		QueryVector:        queryVector,
		K:                  int32(vectorK),
		EfSearch:           int32(vectorEfSearch),
		MinScore:           vectorMinScore,
		MetadataFilters:    filters,
		FilterOperator:     vectorFilterOp,
		RerankingAlgorithm: vectorRerank,
		IncludeVectors:     vectorIncludeVectors,
		IncludeMetadata:    vectorIncludeMeta,
	}

	resp, err := vectorClient.HybridSearch(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorHybridSearchResponse(resp)
}

// newVectorSearchByMetadataCommand creates the search by-metadata command.
func newVectorSearchByMetadataCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "by-metadata <namespace>",
		Short: "Search vectors by metadata without query vector",
		Long: `Search for vectors based solely on metadata filters without providing a query vector.

Useful for finding vectors by attributes like category, type, or status.

Examples:
  # Find all documents of a specific type
  quicbaas vector search by-metadata embeddings --filter type=document

  # Multiple filters with AND logic
  quicbaas vector search by-metadata embeddings --filter type=article --filter status=active

  # Multiple filters with OR logic
  quicbaas vector search by-metadata embeddings --filter category=tech --filter category=science --filter-op OR

  # With pagination
  quicbaas vector search by-metadata embeddings --filter type=doc --limit 50 --offset 100`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorSearchByMetadataCommand,
	}

	cmd.Flags().StringArrayVar(&vectorFilters, "filter", nil, "Metadata filters as key=value pairs (required, repeatable)")
	cmd.Flags().StringVar(&vectorFilterOp, "filter-op", "AND", "Filter operator (AND or OR)")
	cmd.Flags().IntVar(&vectorLimit, "limit", 100, "Maximum number of results")
	cmd.Flags().IntVar(&vectorOffset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&vectorIncludeVectors, "include-vectors", false, "Include vector data in results")
	cmd.Flags().BoolVar(&vectorIncludeMeta, "include-metadata", true, "Include metadata in results")

	if err := cmd.MarkFlagRequired("filter"); err != nil {
		stdlog.Fatalf("failed to mark 'filter' flag as required: %v", err)
	}

	return cmd
}

// runVectorSearchByMetadataCommand executes the search by-metadata command.
func runVectorSearchByMetadataCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	// Parse metadata filters
	if len(vectorFilters) == 0 {
		return NewFilterRequiredError("search by-metadata")
	}

	filters := make([]*vector.MetadataFilter, 0, len(vectorFilters))
	for _, f := range vectorFilters {
		key, value, found := parseFilter(f)
		if !found {
			return NewInvalidFilterFormatError("search by-metadata", f)
		}
		filters = append(filters, &vector.MetadataFilter{
			Key:      key,
			Operator: vector.FilterOperatorEquals,
			Value:    value,
		})
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.SearchByMetadataRequest{
		Namespace:       namespace,
		MetadataFilters: filters,
		FilterOperator:  vectorFilterOp,
		Limit:           int32(vectorLimit),
		Offset:          int32(vectorOffset),
		IncludeVectors:  vectorIncludeVectors,
		IncludeMetadata: vectorIncludeMeta,
	}

	resp, err := vectorClient.SearchByMetadata(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorSearchByMetadataResponse(resp)
}

// parseFilter parses a key=value filter string.
func parseFilter(filter string) (key, value string, found bool) {
	for i, c := range filter {
		if c == '=' {
			return filter[:i], filter[i+1:], true
		}
	}
	return "", "", false
}

// formatVectorHybridSearchResponse formats hybrid search results.
func formatVectorHybridSearchResponse(resp *vector.HybridSearchResponse) error {
	if resp == nil || len(resp.Results) == 0 {
		fmt.Println("No results found")
		return nil
	}

	fmt.Printf("Results: %d (from %d candidates, %d after filtering)\n",
		len(resp.Results), resp.TotalCandidates, resp.TotalFiltered)
	if resp.RerankingApplied {
		fmt.Println("Reranking: applied")
	}
	fmt.Printf("Search time: %dms\n\n", resp.SearchTimeMs)

	for _, result := range resp.Results {
		var id string
		if result.Vector != nil {
			id = result.Vector.ID
		}
		fmt.Printf("  Rank %d: ID=%s Score=%.4f\n",
			result.Rank, id, result.Score)
		if result.Vector != nil && len(result.Vector.Metadata) > 0 {
			fmt.Printf("    Metadata: %v\n", result.Vector.Metadata)
		}
		if result.Vector != nil && len(result.Vector.Vector) > 0 {
			fmt.Printf("    Vector: [%d dimensions]\n", len(result.Vector.Vector))
		}
	}
	return nil
}

// formatVectorSearchByMetadataResponse formats metadata search results.
func formatVectorSearchByMetadataResponse(resp *vector.SearchByMetadataResponse) error {
	if resp == nil || len(resp.Results) == 0 {
		fmt.Println("No results found")
		return nil
	}

	fmt.Printf("Results: %d (total: %d)\n", len(resp.Results), resp.TotalCount)
	fmt.Printf("Search time: %dms\n\n", resp.SearchTimeMs)

	for i, vec := range resp.Results {
		fmt.Printf("  %d: ID=%s\n", i+1, vec.ID)
		if len(vec.Metadata) > 0 {
			fmt.Printf("     Metadata: %v\n", vec.Metadata)
		}
		if len(vec.Vector) > 0 {
			fmt.Printf("     Vector: [%d dimensions]\n", len(vec.Vector))
		}
	}

	if resp.NextCursor != "" {
		fmt.Printf("\nNext cursor: %s\n", resp.NextCursor)
	}
	return nil
}

// =============================================================================
// Vector Index Commands
// =============================================================================

// newVectorIndexRebuildCommand creates the index rebuild command.
func newVectorIndexRebuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild <namespace>",
		Short: "Rebuild the vector index",
		Long: `Rebuild the vector index for a namespace.

This may be necessary after bulk imports or to apply new index configuration.

Examples:
  # Rebuild index synchronously
  quicbaas vector index rebuild embeddings

  # Rebuild asynchronously
  quicbaas vector index rebuild embeddings --async

  # Rebuild with new configuration
  quicbaas vector index rebuild embeddings --index-config M=32,efConstruction=400`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorIndexRebuildCommand,
	}

	cmd.Flags().BoolVar(&vectorAsync, "async", false, "Run rebuild asynchronously")
	cmd.Flags().StringToStringVar(&vectorIndexConfig, "index-config", nil, "New index configuration as key=value pairs")

	return cmd
}

// runVectorIndexRebuildCommand executes the index rebuild command.
func runVectorIndexRebuildCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.RebuildIndexRequest{
		Namespace:   namespace,
		IndexParams: vectorIndexConfig,
		Async:       vectorAsync,
	}

	resp, err := vectorClient.RebuildIndex(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorIndexRebuildResponse(resp)
}

// newVectorIndexProgressCommand creates the index progress command.
func newVectorIndexProgressCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "progress <operation-id>",
		Short: "Get index rebuild progress",
		Long: `Retrieve the progress of an index rebuild operation.

Examples:
  # Get progress of a rebuild operation
  quicbaas vector index progress op-123456

  # Use with namespace flag to filter
  quicbaas vector index progress op-123456 --namespace embeddings`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorIndexProgressCommand,
	}

	cmd.Flags().StringVar(&vectorNamespace, "namespace", "", "Namespace to filter by")

	return cmd
}

// runVectorIndexProgressCommand executes the index progress command.
func runVectorIndexProgressCommand(cmd *cobra.Command, args []string) error {
	operationID := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.GetIndexProgressRequest{
		OperationID: operationID,
		Namespace:   vectorNamespace,
	}

	resp, err := vectorClient.GetIndexProgress(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorIndexProgressResponse(resp)
}

// newVectorIndexStatsCommand creates the index stats command.
func newVectorIndexStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats <namespace>",
		Short: "Get vector index statistics",
		Long: `Retrieve statistics about the vector index.

Examples:
  quicbaas vector index stats embeddings`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorIndexStatsCommand,
	}

	return cmd
}

// runVectorIndexStatsCommand executes the index stats command.
func runVectorIndexStatsCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Use GetStats for detailed statistics
	req := &vector.GetStatsRequest{
		Namespace: namespace,
	}

	resp, err := vectorClient.GetStats(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatVectorIndexStatsResponse(resp)
}

// =============================================================================
// Helper Functions
// =============================================================================

// parseVectorData parses comma-separated float values into a vector.
func parseVectorData(data string) ([]float32, error) {
	if data == "" {
		return nil, errors.Newf(errors.ErrInvalidInput, "parseVectorData", "vector data cannot be empty")
	}

	parts := strings.Split(data, ",")
	vectorValues := make([]float32, len(parts))

	for i, part := range parts {
		part = strings.TrimSpace(part)
		val, err := strconv.ParseFloat(part, 32)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrInvalidInput,
				"parseVectorData",
				"invalid float value at position %d: %s", i, part,
			)
		}
		vectorValues[i] = float32(val)
	}

	return vectorValues, nil
}

// =============================================================================
// Output Formatting Functions - Using SDK Types Directly
// =============================================================================

func formatVectorNamespaceCreateResponse(resp *vector.CreateNamespaceResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Printf("Namespace '%s' created successfully\n", resp.Namespace)
			fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
		} else {
			fmt.Println("Failed to create namespace")
		}
	}
	return nil
}

func formatVectorNamespaceDeleteResponse(resp *vector.DeleteNamespaceResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Deleted {
			fmt.Printf("Namespace deleted successfully\n")
			fmt.Printf("Vectors deleted: %d\n", resp.VectorsDeleted)
			fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
		} else {
			fmt.Println("Failed to delete namespace")
		}
	}
	return nil
}

func formatVectorNamespaceListResponse(resp *vector.ListNamespacesResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Namespaces (Total: %d):\n", resp.TotalCount)
		fmt.Println("NAME                    DIMENSION    VECTORS      CREATED")
		fmt.Println("---------------------------------------------------------------")
		for _, ns := range resp.Namespaces {
			var namespace string
			var dimension int32
			if ns.Config != nil {
				namespace = ns.Config.Namespace
				dimension = ns.Config.Dimensions
			}
			fmt.Printf("%-23s %-12d %-12d %s\n",
				namespace,
				dimension,
				ns.VectorCount,
				ns.CreatedAt.Format("2006-01-02 15:04:05"))
		}
	}
	return nil
}

func formatVectorNamespaceInfoResponse(resp *vector.GetNamespaceInfoResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		info := resp.Info
		if info == nil {
			fmt.Println("Namespace not found")
			return nil
		}

		var namespace string
		var dimension int32
		var indexType string
		var indexConfig map[string]string
		var metadata map[string]string

		if info.Config != nil {
			namespace = info.Config.Namespace
			dimension = info.Config.Dimensions
			indexType = string(info.Config.IndexType)
			indexConfig = info.Config.IndexParams
			metadata = info.Config.Metadata
		}

		fmt.Printf("Namespace: %s\n", namespace)
		fmt.Printf("Dimension: %d\n", dimension)
		fmt.Printf("Vector Count: %d\n", info.VectorCount)
		fmt.Printf("Index Type: %s\n", indexType)
		fmt.Printf("Created: %s\n", info.CreatedAt.Format(time.RFC3339))
		fmt.Printf("Updated: %s\n", info.UpdatedAt.Format(time.RFC3339))
		if len(indexConfig) > 0 {
			fmt.Println("\nIndex Configuration:")
			for k, v := range indexConfig {
				fmt.Printf("  %s: %s\n", k, v)
			}
		}
		if len(metadata) > 0 {
			fmt.Println("\nMetadata:")
			for k, v := range metadata {
				fmt.Printf("  %s: %v\n", k, v)
			}
		}
	}
	return nil
}

func formatVectorUpsertResponse(resp *vector.UpsertResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Printf("Vector upserted successfully\n")
			fmt.Printf("Version: %d\n", resp.Version)
			fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
		} else {
			fmt.Println("Failed to upsert vector")
		}
	}
	return nil
}

func formatVectorBatchUpsertResponse(resp *vector.BatchUpsertResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		success := resp.VectorsFailed == 0
		if success {
			fmt.Printf("Batch upsert successful\n")
			fmt.Printf("Inserted: %d vectors\n", resp.VectorsSucceeded)
			fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
		} else {
			fmt.Printf("Batch upsert completed with errors\n")
			fmt.Printf("Succeeded: %d vectors\n", resp.VectorsSucceeded)
			fmt.Printf("Failed: %d vectors\n", resp.VectorsFailed)
		}
	}
	return nil
}

func formatVectorGetResponse(resp *vector.GetResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if !resp.Found {
			fmt.Println("Vector not found")
			return nil
		}
		if resp.Vector != nil {
			fmt.Printf("ID: %s\n", resp.Vector.ID)
			fmt.Printf("Version: %d\n", resp.Vector.Version)
			fmt.Printf("Created: %s\n", resp.Vector.CreatedAt.Format(time.RFC3339))
			fmt.Printf("Updated: %s\n", resp.Vector.UpdatedAt.Format(time.RFC3339))
			if len(resp.Vector.Vector) > 0 {
				fmt.Printf("Vector (%d dimensions): [", len(resp.Vector.Vector))
				for i, v := range resp.Vector.Vector {
					if i > 0 {
						fmt.Print(", ")
					}
					if i >= 5 {
						fmt.Printf("... %d more", len(resp.Vector.Vector)-5)
						break
					}
					fmt.Printf("%.4f", v)
				}
				fmt.Println("]")
			}
			if len(resp.Vector.Metadata) > 0 {
				fmt.Println("\nMetadata:")
				for k, v := range resp.Vector.Metadata {
					fmt.Printf("  %s: %v\n", k, v)
				}
			}
		}
	}
	return nil
}

func formatVectorDeleteResponse(resp *vector.DeleteResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Deleted {
			fmt.Println("Vector deleted successfully")
			fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
		} else {
			fmt.Println("Failed to delete vector")
		}
	}
	return nil
}

func formatVectorBatchDeleteResponse(resp *vector.BatchDeleteResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		success := len(resp.Errors) == 0
		if success {
			fmt.Printf("Batch delete successful\n")
			fmt.Printf("Deleted: %d vectors\n", resp.DeletedCount)
			fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
		} else {
			fmt.Printf("Batch delete completed with errors\n")
			fmt.Printf("Deleted: %d vectors\n", resp.DeletedCount)
			fmt.Printf("Errors: %d\n", len(resp.Errors))
		}
	}
	return nil
}

func formatVectorListResponse(resp *vector.ListVectorsResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Vectors (Total: %d):\n", resp.TotalCount)
		fmt.Println("ID                      VERSION    CREATED              UPDATED")
		fmt.Println("---------------------------------------------------------------")
		for _, v := range resp.Vectors {
			fmt.Printf("%-23s %-10d %-20s %s\n",
				v.ID,
				v.Version,
				v.CreatedAt.Format("2006-01-02 15:04:05"),
				v.UpdatedAt.Format("2006-01-02 15:04:05"))
		}
		if resp.NextPageToken != "" {
			fmt.Printf("\nNext page token: %s\n", resp.NextPageToken)
		}
	}
	return nil
}

func formatVectorSearchSimilarResponse(resp *vector.SearchSimilarResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Search Results (%d matches, query took %dms):\n", len(resp.Results), resp.SearchTimeMs)
		fmt.Println("RANK  SCORE       DISTANCE    METADATA")
		fmt.Println("------------------------------------------------------------")
		for _, result := range resp.Results {
			metaStr := ""
			if result.Vector != nil && len(result.Vector.Metadata) > 0 {
				parts := []string{}
				for k, v := range result.Vector.Metadata {
					parts = append(parts, fmt.Sprintf("%s=%v", k, v))
				}
				metaStr = strings.Join(parts, ", ")
			}
			fmt.Printf("%-5d %-11.6f %-11.6f %s\n",
				result.Rank,
				result.Score,
				result.Distance,
				metaStr)
		}
	}
	return nil
}

func formatVectorSearchByIDResponse(resp *vector.SearchByIDResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Search Results (%d matches, query took %dms):\n", len(resp.Results), resp.SearchTimeMs)
		fmt.Println("RANK  SCORE       DISTANCE    METADATA")
		fmt.Println("------------------------------------------------------------")
		for _, result := range resp.Results {
			metaStr := ""
			if result.Vector != nil && len(result.Vector.Metadata) > 0 {
				parts := []string{}
				for k, v := range result.Vector.Metadata {
					parts = append(parts, fmt.Sprintf("%s=%v", k, v))
				}
				metaStr = strings.Join(parts, ", ")
			}
			fmt.Printf("%-5d %-11.6f %-11.6f %s\n",
				result.Rank,
				result.Score,
				result.Distance,
				metaStr)
		}
	}
	return nil
}

func formatVectorIndexRebuildResponse(resp *vector.RebuildIndexResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Println("Index rebuild initiated successfully")
		fmt.Printf("Operation ID: %s\n", resp.OperationID)
		fmt.Printf("Status: %s\n", resp.Status)
		if resp.EstimatedDurationMs > 0 {
			fmt.Printf("Estimated Duration: %dms\n", resp.EstimatedDurationMs)
		}
	}
	return nil
}

func formatVectorIndexProgressResponse(resp *vector.GetIndexProgressResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Operation ID: %s\n", resp.OperationID)
		fmt.Printf("Namespace: %s\n", resp.Namespace)
		fmt.Printf("Status: %s\n", resp.Status)
		fmt.Printf("Progress: %.1f%%\n", resp.PercentComplete)
		fmt.Printf("Vectors Indexed: %d / %d\n", resp.VectorsIndexed, resp.TotalVectors)
		if resp.ElapsedTimeMs > 0 {
			fmt.Printf("Elapsed Time: %dms\n", resp.ElapsedTimeMs)
		}
		if resp.EstimatedRemainingMs > 0 {
			fmt.Printf("Estimated Time Remaining: %dms\n", resp.EstimatedRemainingMs)
		}
		if resp.ErrorMessage != "" {
			fmt.Printf("Error: %s\n", resp.ErrorMessage)
		}
	}
	return nil
}

func formatVectorIndexStatsResponse(resp *vector.GetStatsResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Namespace: %s\n", resp.Namespace)
		fmt.Printf("Index Type: hnsw\n")
		fmt.Printf("Vector Count: %d\n", resp.VectorCount)
		fmt.Printf("Index Size: %d bytes (%.2f MB)\n", resp.IndexSizeBytes, float64(resp.IndexSizeBytes)/(1024*1024))
		fmt.Printf("Storage Size: %d bytes (%.2f MB)\n", resp.StorageSizeBytes, float64(resp.StorageSizeBytes)/(1024*1024))
	}
	return nil
}

// =============================================================================
// Admin Commands
// =============================================================================

var (
	// Export flags.
	vectorExportFormat      string
	vectorExportCompression string
	vectorExportBatchSize   int
	vectorExportOutput      string

	// Import flags.
	vectorImportFormat    string
	vectorImportOverwrite bool
	vectorImportInput     string
)

// newVectorExportCommand creates the export command.
func newVectorExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <namespace>",
		Short: "Export vectors from a namespace",
		Long: `Export vectors from a namespace for backup or migration.

Vectors are exported in batches and can be saved to a file or stdout.

Examples:
  # Export to stdout
  quicbaas vector admin export embeddings

  # Export to a file
  quicbaas vector admin export embeddings --output backup.json

  # Export with gzip compression
  quicbaas vector admin export embeddings --compression gzip --output backup.json.gz`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorExportCommand,
	}

	cmd.Flags().StringVar(&vectorExportFormat, "format", "json", "Export format (json)")
	cmd.Flags().StringVar(&vectorExportCompression, "compression", "none", "Compression (none, gzip)")
	cmd.Flags().IntVar(&vectorExportBatchSize, "batch-size", 1000, "Vectors per batch")
	cmd.Flags().StringVarP(&vectorExportOutput, "output", "o", "", "Output file (defaults to stdout)")

	return cmd
}

// runVectorExportCommand executes the export command.
func runVectorExportCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.ExportRequest{
		Namespace:   namespace,
		Format:      vectorExportFormat,
		Compression: vectorExportCompression,
		BatchSize:   int32(vectorExportBatchSize),
	}

	responses, err := vectorClient.Export(ctx, req)
	if err != nil {
		return formatError(err)
	}

	// Determine output destination
	var output *os.File
	if vectorExportOutput != "" {
		output, err = os.Create(vectorExportOutput)
		if err != nil {
			return errors.Newf(errors.ErrInternal, "export", "failed to create output file: %v", err)
		}
		defer output.Close()
	} else {
		output = os.Stdout
	}

	// Write all batch data
	var totalExported int64
	for _, resp := range responses {
		if _, err := output.Write(resp.BatchData); err != nil {
			return errors.Newf(errors.ErrInternal, "export", "failed to write data: %v", err)
		}
		totalExported += resp.VectorsExported
	}

	// Print summary to stderr if outputting to file
	if vectorExportOutput != "" {
		fmt.Fprintf(os.Stderr, "Exported %d vectors to %s\n", totalExported, vectorExportOutput)
	}

	return nil
}

// newVectorImportCommand creates the import command.
func newVectorImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <namespace>",
		Short: "Import vectors into a namespace",
		Long: `Import vectors into a namespace from a backup or migration file.

Examples:
  # Import from a file
  quicbaas vector admin import embeddings --input backup.json

  # Import with overwrite
  quicbaas vector admin import embeddings --input backup.json --overwrite`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorImportCommand,
	}

	cmd.Flags().StringVar(&vectorImportFormat, "format", "json", "Import format (json)")
	cmd.Flags().BoolVar(&vectorImportOverwrite, "overwrite", false, "Overwrite existing vectors")
	cmd.Flags().StringVarP(&vectorImportInput, "input", "i", "", "Input file (required)")

	if err := cmd.MarkFlagRequired("input"); err != nil {
		stdlog.Fatalf("failed to mark 'input' flag as required: %v", err)
	}

	return cmd
}

// runVectorImportCommand executes the import command.
func runVectorImportCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	// Read input file
	data, err := os.ReadFile(vectorImportInput)
	if err != nil {
		return errors.Newf(errors.ErrInternal, "import", "failed to read input file: %v", err)
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Create import request
	batches := []*vector.ImportRequest{
		{
			Namespace:         namespace,
			Format:            vectorImportFormat,
			BatchData:         data,
			OverwriteExisting: vectorImportOverwrite,
		},
	}

	resp, err := vectorClient.Import(ctx, batches)
	if err != nil {
		return formatError(err)
	}

	// Print results
	fmt.Printf("Import completed:\n")
	fmt.Printf("  Vectors imported: %d\n", resp.VectorsImported)
	fmt.Printf("  Vectors skipped: %d\n", resp.VectorsSkipped)
	fmt.Printf("  Vectors failed: %d\n", resp.VectorsFailed)
	fmt.Printf("  Duration: %dms\n", resp.DurationMs)

	return nil
}

// =============================================================================
// Compaction Commands
// =============================================================================

// newVectorCompactCommand creates the compact command.
func newVectorCompactCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compact <namespace>",
		Short: "Compact a namespace to reclaim disk space",
		Long: `Compact a namespace to reclaim disk space by removing tombstones
and old MVCC versions.

By default, compaction runs synchronously and waits for completion.
Use --async to start compaction in the background and use the progress
command to monitor it.

Examples:
  # Compact synchronously (wait for completion)
  quicbaas vector admin compact embeddings

  # Compact asynchronously (return immediately)
  quicbaas vector admin compact embeddings --async

  # Set a timeout for synchronous compaction
  quicbaas vector admin compact embeddings --timeout 10m`,
		Args: cobra.ExactArgs(1),
		RunE: runVectorCompactCommand,
	}

	cmd.Flags().BoolVar(&vectorAsync, "async", false, "Run compaction asynchronously")
	cmd.Flags().DurationVar(&compactTimeout, "timeout", 5*time.Minute, "Timeout for synchronous compaction")

	return cmd
}

// runVectorCompactCommand executes the compact command.
func runVectorCompactCommand(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), compactTimeout)
	defer cancel()

	req := &vector.CompactRequest{
		Namespace: namespace,
		Async:     vectorAsync,
	}

	resp, err := vectorClient.Compact(ctx, req)
	if err != nil {
		return formatError(err)
	}

	fmt.Printf("Compaction started:\n")
	fmt.Printf("  Operation ID: %s\n", resp.OperationID)
	fmt.Printf("  Namespace: %s\n", namespace)
	if !resp.Started.IsZero() {
		fmt.Printf("  Started: %s\n", resp.Started.Format(time.RFC3339))
	}
	if vectorAsync {
		fmt.Printf("  Status: in_progress\n")
		fmt.Printf("\nUse 'quicbaas vector admin progress --operation-id %s' to monitor progress.\n", resp.OperationID)
	} else {
		fmt.Printf("  Status: completed\n")
	}

	return nil
}

// newVectorCompactionProgressCommand creates the compaction progress command.
func newVectorCompactionProgressCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "progress",
		Short: "Get compaction progress",
		Long: `Get the progress of a compaction operation.

You can query by operation ID or namespace. If querying by namespace,
returns the progress of the most recent compaction operation.

Examples:
  # Get progress by operation ID
  quicbaas vector admin progress --operation-id abc-123-def

  # Get progress by namespace (most recent operation)
  quicbaas vector admin progress --namespace embeddings`,
		RunE: runVectorCompactionProgressCommand,
	}

	cmd.Flags().StringVar(&vectorOperationID, "operation-id", "", "Operation ID to check")
	cmd.Flags().StringVarP(&vectorNamespace, "namespace", "n", "", "Namespace to check")

	return cmd
}

var vectorOperationID string

// runVectorCompactionProgressCommand executes the compaction progress command.
func runVectorCompactionProgressCommand(cmd *cobra.Command, args []string) error {
	if vectorOperationID == "" && vectorNamespace == "" {
		return errors.Newf(errors.ErrInvalidInput, "progress", "either --operation-id or --namespace is required")
	}

	vectorClient, err := createVectorClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &vector.GetCompactionProgressRequest{
		OperationID: vectorOperationID,
		Namespace:   vectorNamespace,
	}

	resp, err := vectorClient.GetCompactionProgress(ctx, req)
	if err != nil {
		return formatError(err)
	}

	status := resp.Status
	if status == "" {
		status = "unknown"
	}

	fmt.Printf("Compaction Progress:\n")
	fmt.Printf("  Operation ID: %s\n", resp.OperationID)
	fmt.Printf("  Namespace: %s\n", resp.Namespace)
	fmt.Printf("  Status: %s\n", status)
	fmt.Printf("  Progress: %.1f%%\n", resp.PercentComplete)
	fmt.Printf("  Bytes compacted: %d\n", resp.BytesCompacted)
	fmt.Printf("  Total bytes: %d\n", resp.TotalBytes)
	fmt.Printf("  Space reclaimed: %d bytes\n", resp.SpaceReclaimedBytes)
	fmt.Printf("  Elapsed time: %dms\n", resp.ElapsedTimeMs)

	return nil
}
