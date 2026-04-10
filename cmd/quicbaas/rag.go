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
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

// =============================================================================
// RAG Command Flags
// =============================================================================

var (
	// RAG query flags.
	ragNamespace        string
	ragTopK             int
	ragMaxTokens        int
	ragMinScore         float32
	ragModel            string
	ragTemplate         string
	ragIncludeCitations bool

	// RAG context flags.
	ragResultsFile        string
	ragTruncationStrategy string
)

// =============================================================================
// RAG Client Factory
// =============================================================================

// createRAGClient creates the SDK RAG client.
func createRAGClient() (rag.Client, error) {
	client, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return client.RAG(), nil
}

// =============================================================================
// RAG Commands
// =============================================================================

// newRAGCommands creates the RAG command group.
func newRAGCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rag",
		Short: "Retrieval-Augmented Generation commands",
		Long: `RAG commands for combining vector search with context generation.

These commands allow you to perform RAG operations that combine embedding generation,
vector similarity search, and context building for use with language models.

RAG operations are supported on all transports (gRPC, REST, QUIC, Unix socket, MCP).`,
	}

	cmd.PersistentFlags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")
	cmd.PersistentFlags().StringVar(&outputFormat, "output", "table", "Output format (json, yaml, table)")

	cmd.PersistentFlags().BoolVar(&tlsEnabled, "tls", false, "Enable TLS")
	cmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS client certificate file")
	cmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS client key file")
	cmd.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA certificate file")

	cmd.AddCommand(newRAGQueryCommand())
	cmd.AddCommand(newRAGContextCommand())

	return cmd
}

// newRAGQueryCommand creates the rag query command.
func newRAGQueryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <query-text>",
		Short: "Perform an all-in-one RAG query",
		Long: `Perform an all-in-one RAG query that:
1. Generates an embedding for the query text using Ollama
2. Searches for similar vectors in the specified namespace
3. Builds formatted context from the search results

Examples:
  # Basic RAG query
  quicbaas rag query "How do I configure logging?" --namespace docs

  # Query with custom parameters
  quicbaas rag query "What is the database schema?" --namespace code \
    --top-k 20 --max-tokens 8000 --min-score 0.75

  # Query with specific embedding model
  quicbaas rag query "Explain the authentication flow" --namespace docs \
    --model nomic-embed-text

  # Query with JSON template and citations
  quicbaas rag query "List all API endpoints" --namespace api \
    --template json --include-citations

  # Output as JSON
  quicbaas rag query "What are the configuration options?" --namespace docs \
    --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runRAGQueryCommand,
	}

	cmd.Flags().StringVar(&ragNamespace, "namespace", "", "Vector namespace to search (required)")
	cmd.Flags().IntVar(&ragTopK, "top-k", 10, "Number of results to retrieve")
	cmd.Flags().IntVar(&ragMaxTokens, "max-tokens", 4000, "Maximum tokens in generated context")
	cmd.Flags().Float32Var(&ragMinScore, "min-score", 0.7, "Minimum similarity score threshold")
	cmd.Flags().StringVar(&ragModel, "model", "", "Embedding model to use (default: server's default model)")
	cmd.Flags().StringVar(&ragTemplate, "template", "markdown", "Context template format (markdown, json, xml, plain)")
	cmd.Flags().BoolVar(&ragIncludeCitations, "include-citations", false, "Include source citations in response")

	if err := cmd.MarkFlagRequired("namespace"); err != nil {
		stdlog.Fatalf("failed to mark 'namespace' flag as required: %v", err)
	}

	return cmd
}

// runRAGQueryCommand executes the rag query command.
func runRAGQueryCommand(cmd *cobra.Command, args []string) error {
	queryText := args[0]

	if queryText == "" {
		return errors.Newf(errors.ErrInvalidInput, "rag query", "query text cannot be empty")
	}

	ragClient, err := createRAGClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &rag.QueryRequest{
		Query:            queryText,
		Namespace:        ragNamespace,
		TopK:             ragTopK,
		MaxTokens:        ragMaxTokens,
		MinScore:         ragMinScore,
		Model:            ragModel,
		Template:         ragTemplate,
		IncludeCitations: ragIncludeCitations,
	}

	resp, err := ragClient.Query(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatRAGQueryResponse(resp)
}

// newRAGContextCommand creates the rag context command.
func newRAGContextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Build context from provided search results",
		Long: `Build formatted context from user-provided search results.

This command allows you to build context from your own search results,
useful when you want to use pre-computed embeddings or external search.

The results file should be JSON with the following format:
[
  {
    "content": "The text content to include",
    "score": 0.95,
    "metadata": {
      "file_path": "/path/to/file.go",
      "start_line": 10,
      "end_line": 20
    }
  }
]

Examples:
  # Build context from results file
  quicbaas rag context --results results.json

  # Build with custom max tokens
  quicbaas rag context --results results.json --max-tokens 8000

  # Build with JSON template
  quicbaas rag context --results results.json --template json

  # Build with smart truncation and citations
  quicbaas rag context --results results.json \
    --truncation smart --include-citations`,
		RunE: runRAGContextCommand,
	}

	cmd.Flags().StringVar(&ragResultsFile, "results", "", "JSON file containing search results (required)")
	cmd.Flags().IntVar(&ragMaxTokens, "max-tokens", 4000, "Maximum tokens in generated context")
	cmd.Flags().StringVar(&ragTemplate, "template", "markdown", "Context template format (markdown, json, xml, plain)")
	cmd.Flags().StringVar(&ragTruncationStrategy, "truncation", "smart", "Truncation strategy (smart, hard, none)")
	cmd.Flags().BoolVar(&ragIncludeCitations, "include-citations", false, "Include source citations in response")

	if err := cmd.MarkFlagRequired("results"); err != nil {
		stdlog.Fatalf("failed to mark 'results' flag as required: %v", err)
	}

	return cmd
}

// runRAGContextCommand executes the rag context command.
func runRAGContextCommand(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(ragResultsFile)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "rag context", "failed to read results file: %s", ragResultsFile)
	}

	// Parse JSON into intermediate format to handle type conversion
	var rawResults []struct {
		Content  string            `json:"content"`
		Score    float32           `json:"score"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(data, &rawResults); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "rag context", "failed to parse results JSON")
	}

	if len(rawResults) == 0 {
		return errors.Newf(errors.ErrInvalidInput, "rag context", "results file contains no results")
	}

	// Convert to SDK ResultInput type
	results := make([]rag.ResultInput, len(rawResults))
	for i, r := range rawResults {
		results[i] = rag.ResultInput{
			Content:  r.Content,
			Score:    r.Score,
			Metadata: r.Metadata,
		}
	}

	ragClient, err := createRAGClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &rag.BuildContextRequest{
		Results:            results,
		MaxTokens:          ragMaxTokens,
		Template:           ragTemplate,
		TruncationStrategy: ragTruncationStrategy,
		IncludeCitations:   ragIncludeCitations,
	}

	resp, err := ragClient.BuildContext(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatRAGBuildContextResponse(resp)
}

// =============================================================================
// Output Formatting
// =============================================================================

// formatRAGQueryResponse formats the RAG query response.
func formatRAGQueryResponse(resp *rag.QueryResponse) error {
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
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		fmt.Printf("RAG Query Results\n")
		fmt.Printf("%s\n", strings.Repeat("=", 60))
		fmt.Printf("Results: %d\n", resp.ResultCount)
		fmt.Printf("Tokens: %d\n", resp.Tokens)
		fmt.Printf("Truncated: %v\n", resp.Truncated)
		fmt.Println()

		if len(resp.Citations) > 0 {
			fmt.Printf("Citations:\n")
			fmt.Printf("%s\n", strings.Repeat("-", 60))
			for _, c := range resp.Citations {
				fmt.Printf("  [%d] %s (lines %d-%d, score: %.4f)\n",
					c.Index, c.FilePath, c.StartLine, c.EndLine, c.Score)
			}
			fmt.Println()
		}

		fmt.Printf("Context:\n")
		fmt.Printf("%s\n", strings.Repeat("-", 60))
		fmt.Println(resp.Context)
	}
	return nil
}

// formatRAGBuildContextResponse formats the RAG build context response.
func formatRAGBuildContextResponse(resp *rag.BuildContextResponse) error {
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
	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		fmt.Printf("Context Build Results\n")
		fmt.Printf("%s\n", strings.Repeat("=", 60))
		fmt.Printf("Results processed: %d\n", resp.ResultCount)
		fmt.Printf("Tokens: %d\n", resp.Tokens)
		fmt.Printf("Truncated: %v\n", resp.Truncated)
		fmt.Println()

		if len(resp.Citations) > 0 {
			fmt.Printf("Citations:\n")
			fmt.Printf("%s\n", strings.Repeat("-", 60))
			for _, c := range resp.Citations {
				fmt.Printf("  [%d] %s (lines %d-%d, score: %.4f)\n",
					c.Index, c.FilePath, c.StartLine, c.EndLine, c.Score)
			}
			fmt.Println()
		}

		fmt.Printf("Context:\n")
		fmt.Printf("%s\n", strings.Repeat("-", 60))
		fmt.Println(resp.Context)
	}
	return nil
}
