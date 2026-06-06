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
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Ollama command flags.
var (
	ollamaModel      string
	ollamaTexts      string
	ollamaFullCheck  bool
	ollamaNamePrefix string
)

// OllamaClient defines the interface for Ollama gRPC operations.
type OllamaClient interface {
	OllamaHealth(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error)
	OllamaListModels(ctx context.Context, req *pb.OllamaListModelsRequest) (*pb.OllamaListModelsResponse, error)
	OllamaGetModelInfo(ctx context.Context, req *pb.OllamaGetModelInfoRequest) (*pb.OllamaGetModelInfoResponse, error)
	OllamaEmbed(ctx context.Context, req *pb.OllamaEmbedRequest) (*pb.OllamaEmbedResponse, error)
	OllamaEmbedWithModel(ctx context.Context, req *pb.OllamaEmbedWithModelRequest) (*pb.OllamaEmbedResponse, error)
	OllamaEmbedBatch(ctx context.Context, req *pb.OllamaEmbedBatchRequest) (*pb.OllamaEmbedBatchResponse, error)
	Close() error
}

// OllamaClientFactory is a function type for creating Ollama clients.
type OllamaClientFactory func() (OllamaClient, error)

// ollamaClientFactory creates Ollama clients. Defaults to createOllamaGRPCClient.
var ollamaClientFactory OllamaClientFactory = createOllamaClient

// createOllamaClient creates a client for Ollama operations based on transport.
func createOllamaClient() (OllamaClient, error) {
	switch transport {
	case "grpc", "":
		return createOllamaGRPCClient()
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createOllamaClient",
			"unsupported transport: %s (supported: grpc)", transport,
		)
	}
}

// newOllamaCommands creates the ollama command group.
func newOllamaCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Ollama integration commands",
		Long: `Ollama integration commands for interacting with Ollama embedding models.

These commands allow you to check Ollama health, list available models,
get model information, and generate embeddings. All commands connect to
a running QuicBaaS server via gRPC.`,
	}

	// Add persistent flags for all ollama commands
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")
	cmd.PersistentFlags().StringVar(&outputFormat, "output", "table", "Output format (json, yaml, table)")

	// TLS flags
	cmd.PersistentFlags().BoolVar(&tlsEnabled, "tls", false, "Enable TLS")
	cmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS client certificate file")
	cmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS client key file")
	cmd.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA certificate file")

	// Connection flags
	cmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retry attempts")
	cmd.PersistentFlags().DurationVar(&retryBackoff, "retry-backoff", 100*time.Millisecond, "Initial retry backoff duration")

	// Add subcommands
	cmd.AddCommand(newOllamaHealthCommand())
	cmd.AddCommand(newOllamaModelsCommand())
	cmd.AddCommand(newOllamaModelInfoCommand())
	cmd.AddCommand(newOllamaEmbedCommand())
	cmd.AddCommand(newOllamaEmbedBatchCommand())

	return cmd
}

// =============================================================================
// Health Command
// =============================================================================

// newOllamaHealthCommand creates the ollama health command.
func newOllamaHealthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check Ollama service health",
		Long: `Check the health status of the Ollama service.

Returns availability status, version, and base URL of the Ollama instance.

Examples:
  # Check Ollama health
  quicbaas ollama health

  # Check with full model availability check
  quicbaas ollama health --full-check

  # Output as JSON
  quicbaas ollama health --output json`,
		RunE: runOllamaHealthCommand,
	}

	cmd.Flags().BoolVar(&ollamaFullCheck, "full-check", false, "Include full health check with model availability")

	return cmd
}

// runOllamaHealthCommand executes the ollama health command.
func runOllamaHealthCommand(cmd *cobra.Command, args []string) error {
	client, err := ollamaClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &pb.OllamaHealthRequest{
		FullCheck: ollamaFullCheck,
	}

	resp, err := client.OllamaHealth(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOllamaHealthResponse(resp)
}

// =============================================================================
// Models Command
// =============================================================================

// newOllamaModelsCommand creates the ollama models command.
func newOllamaModelsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "models",
		Aliases: []string{"list"},
		Short:   "List available Ollama models",
		Long: `List all available models from the Ollama service.

Returns model names, sizes, digests, and modification timestamps.

Examples:
  # List all models
  quicbaas ollama models

  # List models with name prefix filter
  quicbaas ollama models --prefix nomic

  # Output as JSON
  quicbaas ollama models --output json

  # Using alias
  quicbaas ollama models --list`,
		RunE: runOllamaModelsCommand,
	}

	cmd.Flags().BoolP("list", "l", false, "List models (same as running without flags)")
	cmd.Flags().StringVar(&ollamaNamePrefix, "prefix", "", "Filter models by name prefix")

	return cmd
}

// runOllamaModelsCommand executes the ollama models command.
func runOllamaModelsCommand(cmd *cobra.Command, args []string) error {
	client, err := ollamaClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &pb.OllamaListModelsRequest{
		NamePrefix: ollamaNamePrefix,
	}

	resp, err := client.OllamaListModels(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOllamaModelsResponse(resp)
}

// =============================================================================
// Model Info Command
// =============================================================================

// newOllamaModelInfoCommand creates the ollama model info command.
func newOllamaModelInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model info <model-name>",
		Short: "Get detailed information about an Ollama model",
		Long: `Retrieve detailed information about a specific Ollama model.

Returns license, format, family, parameter size, and quantization level.

Examples:
  # Get info for nomic-embed-text model
  quicbaas ollama model info nomic-embed-text

  # Output as JSON
  quicbaas ollama model info nomic-embed-text --output json

  # Output as YAML
  quicbaas ollama model info nomic-embed-text --output yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runOllamaModelInfoCommand,
	}

	return cmd
}

// runOllamaModelInfoCommand executes the ollama model info command.
func runOllamaModelInfoCommand(cmd *cobra.Command, args []string) error {
	modelName := args[0]

	client, err := ollamaClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &pb.OllamaGetModelInfoRequest{
		Model: modelName,
	}

	resp, err := client.OllamaGetModelInfo(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOllamaModelInfoResponse(resp)
}

// =============================================================================
// Embed Command
// =============================================================================

// newOllamaEmbedCommand creates the ollama embed command.
func newOllamaEmbedCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "embed <text>",
		Short: "Generate an embedding for text",
		Long: `Generate an embedding vector for the provided text.

Uses the default model unless a specific model is specified with --model.

Examples:
  # Generate embedding using default model
  quicbaas ollama embed "Hello, world!"

  # Generate embedding using specific model
  quicbaas ollama embed "Hello, world!" --model nomic-embed-text

  # Output as JSON
  quicbaas ollama embed "Hello, world!" --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOllamaEmbedCommand,
	}

	cmd.Flags().StringVar(&ollamaModel, "model", "", "Model to use for embedding (uses default if not specified)")

	return cmd
}

// runOllamaEmbedCommand executes the ollama embed command.
func runOllamaEmbedCommand(cmd *cobra.Command, args []string) error {
	text := args[0]

	if text == "" {
		return errors.Newf(errors.ErrInvalidInput, "embed", "text cannot be empty")
	}

	client, err := ollamaClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var resp *pb.OllamaEmbedResponse

	if ollamaModel != "" {
		req := &pb.OllamaEmbedWithModelRequest{
			Text:  text,
			Model: ollamaModel,
		}
		resp, err = client.OllamaEmbedWithModel(ctx, req)
	} else {
		req := &pb.OllamaEmbedRequest{
			Text: text,
		}
		resp, err = client.OllamaEmbed(ctx, req)
	}

	if err != nil {
		return formatError(err)
	}

	return formatOllamaEmbedResponse(resp)
}

// =============================================================================
// Embed Batch Command
// =============================================================================

// newOllamaEmbedBatchCommand creates the ollama embed-batch command.
func newOllamaEmbedBatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "embed-batch",
		Short: "Generate embeddings for multiple texts",
		Long: `Generate embedding vectors for multiple texts in a single request.

Texts should be provided as a JSON array.

Examples:
  # Generate embeddings for multiple texts
  quicbaas ollama embed-batch --texts '["Hello", "World", "Test"]'

  # Generate embeddings using specific model
  quicbaas ollama embed-batch --texts '["Hello", "World"]' --model nomic-embed-text

  # Output as JSON
  quicbaas ollama embed-batch --texts '["Hello", "World"]' --output json`,
		RunE: runOllamaEmbedBatchCommand,
	}

	cmd.Flags().StringVar(&ollamaTexts, "texts", "", "JSON array of texts to embed (required)")
	cmd.Flags().StringVar(&ollamaModel, "model", "", "Model to use for embedding (uses default if not specified)")

	if err := cmd.MarkFlagRequired("texts"); err != nil {
		stdlog.Fatalf("failed to mark 'texts' flag as required: %v", err)
	}

	return cmd
}

// runOllamaEmbedBatchCommand executes the ollama embed-batch command.
func runOllamaEmbedBatchCommand(cmd *cobra.Command, args []string) error {
	// Parse texts from JSON array
	var texts []string
	if err := json.Unmarshal([]byte(ollamaTexts), &texts); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "embed-batch", "invalid JSON array for texts")
	}

	if len(texts) == 0 {
		return errors.Newf(errors.ErrInvalidInput, "embed-batch", "texts array cannot be empty")
	}

	client, err := ollamaClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &pb.OllamaEmbedBatchRequest{
		Texts: texts,
		Model: ollamaModel,
	}

	resp, err := client.OllamaEmbedBatch(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOllamaEmbedBatchResponse(resp)
}

// =============================================================================
// Output Formatting Functions
// =============================================================================

// formatOllamaHealthResponse formats the health response.
func formatOllamaHealthResponse(resp *pb.OllamaHealthResponse) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		status := "unavailable"
		if resp.Available {
			status = "available"
		}
		fmt.Printf("Ollama Health Status\n")
		fmt.Printf("--------------------\n")
		fmt.Printf("Status:       %s\n", status)
		if resp.Version != "" {
			fmt.Printf("Version:      %s\n", resp.Version)
		}
		fmt.Printf("Base URL:     %s\n", resp.BaseUrl)
		if resp.LastChecked != nil {
			fmt.Printf("Last Checked: %s\n", resp.LastChecked.AsTime().Format(time.RFC3339))
		}
		if resp.Error != "" {
			fmt.Printf("Error:        %s\n", resp.Error)
		}
	}
	return nil
}

// formatOllamaModelsResponse formats the models list response.
func formatOllamaModelsResponse(resp *pb.OllamaListModelsResponse) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		fmt.Printf("Available Models (Total: %d)\n", resp.Count)
		fmt.Println("NAME                           SIZE          DIGEST                   MODIFIED")
		fmt.Println("---------------------------------------------------------------------------------")
		for _, model := range resp.Models {
			// Convert int64 to uint64 for formatBytes (Size is always non-negative)
			sizeStr := formatBytes(uint64(model.Size))
			digestStr := model.Digest
			if len(digestStr) > 20 {
				digestStr = digestStr[:20] + "..."
			}
			modifiedAt := ""
			if model.ModifiedAt != nil {
				modifiedAt = model.ModifiedAt.AsTime().Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%-30s %-13s %-24s %s\n", model.Name, sizeStr, digestStr, modifiedAt)
		}
	}
	return nil
}

// formatOllamaModelInfoResponse formats the model info response.
func formatOllamaModelInfoResponse(resp *pb.OllamaGetModelInfoResponse) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		fmt.Printf("Model: %s\n", resp.Model)
		fmt.Println("------------------")
		if resp.Details != nil {
			if resp.Details.Family != "" {
				fmt.Printf("Family:            %s\n", resp.Details.Family)
			}
			if resp.Details.Format != "" {
				fmt.Printf("Format:            %s\n", resp.Details.Format)
			}
			if resp.Details.ParameterSize != "" {
				fmt.Printf("Parameter Size:    %s\n", resp.Details.ParameterSize)
			}
			if resp.Details.QuantizationLevel != "" {
				fmt.Printf("Quantization:      %s\n", resp.Details.QuantizationLevel)
			}
			if resp.Details.License != "" {
				fmt.Printf("License:           %s\n", resp.Details.License)
			}
		}
	}
	return nil
}

// formatOllamaEmbedResponse formats the embed response.
func formatOllamaEmbedResponse(resp *pb.OllamaEmbedResponse) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		fmt.Printf("Embedding Generated\n")
		fmt.Printf("-------------------\n")
		fmt.Printf("Model:      %s\n", resp.Model)
		fmt.Printf("Dimensions: %d\n", resp.Dimensions)
		if resp.ProcessingTime != nil {
			fmt.Printf("Time:       %s\n", resp.ProcessingTime.AsDuration().String())
		}
		fmt.Printf("\nVector Preview (first 5 values):\n")
		fmt.Print("  [")
		for i, v := range resp.Embedding {
			if i > 0 {
				fmt.Print(", ")
			}
			if i >= 5 {
				fmt.Printf("... %d more", len(resp.Embedding)-5)
				break
			}
			fmt.Printf("%.6f", v)
		}
		fmt.Println("]")
	}
	return nil
}

// formatOllamaEmbedBatchResponse formats the batch embed response.
func formatOllamaEmbedBatchResponse(resp *pb.OllamaEmbedBatchResponse) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
	default:
		fmt.Printf("Batch Embeddings Generated\n")
		fmt.Printf("--------------------------\n")
		fmt.Printf("Model:      %s\n", resp.Model)
		fmt.Printf("Count:      %d\n", resp.Count)
		fmt.Printf("Dimensions: %d\n", resp.Dimensions)
		if resp.ProcessingTime != nil {
			fmt.Printf("Time:       %s\n", resp.ProcessingTime.AsDuration().String())
		}
		fmt.Printf("\nEmbeddings:\n")
		for i, emb := range resp.Embeddings {
			fmt.Printf("  [%d] ", i)
			if emb != nil && len(emb.Values) > 0 {
				fmt.Print("[")
				for j, v := range emb.Values {
					if j > 0 {
						fmt.Print(", ")
					}
					if j >= 3 {
						fmt.Printf("... %d more", len(emb.Values)-3)
						break
					}
					fmt.Printf("%.6f", v)
				}
				fmt.Println("]")
			} else {
				fmt.Println("[]")
			}
		}
	}
	return nil
}

// =============================================================================
// gRPC Client Creation
// =============================================================================

// createOllamaGRPCClient creates a gRPC client for Ollama operations.
func createOllamaGRPCClient() (*grpc.Client, error) {
	// Create logger for client
	logConfig := logger.LogConfig{
		Level:  "warn",
		Format: "json",
		Output: "stderr",
	}

	log, err := logger.New(logConfig)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrConfiguration,
			"createOllamaGRPCClient",
			"failed to create logger",
		)
	}

	// Build client options
	opts := []grpc.ClientOption{
		grpc.WithLogger(log),
		grpc.WithTimeout(timeout),
		grpc.WithRetry(maxRetries, retryBackoff),
		grpc.WithMaxRetryBackoff(5 * time.Second),
	}

	// Add TLS configuration. QuicBaaS bans insecure TLS — explicit CA
	// trust required.
	switch {
	case tlsEnabled && tlsCert != "" && tlsKey != "":
		opts = append(opts, grpc.WithTLS(tlsCert, tlsKey, tlsCA))
	case tlsEnabled && tlsCA != "":
		opt, err := grpcCAOnlyTLSOption(tlsCA)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrConfiguration,
				"createOllamaGRPCClient", "failed to load --tls-ca")
		}
		opts = append(opts, opt)
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createOllamaGRPCClient",
			"TLS is required for gRPC connections (use --tls-cert, --tls-key, and --tls-ca)",
		)
	}

	client, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createOllamaGRPCClient",
			"failed to connect to server at %s", address,
		)
	}

	return client, nil
}
