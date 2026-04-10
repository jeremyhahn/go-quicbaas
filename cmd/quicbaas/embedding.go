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
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

var (
	// Embedding flags.
	embeddingProvider           string
	embeddingMetadata           string
	embeddingInputType          string
	embeddingSource             string
	embeddingLanguage           string
	embeddingCacheBypass        bool
	embeddingIncludeUnavailable bool
)

// newEmbeddingCommands creates the embedding command group.
func newEmbeddingCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "embedding",
		Aliases: []string{"embed", "emb"},
		Short:   "Embedding generation and management commands",
		Long: `Embedding generation and management commands for interacting with QuicBaaS embedding service.

These commands allow you to generate embeddings from text, manage embedding providers,
and control the embedding cache. All commands connect to a running QuicBaaS server.

Supported providers:
  - voyage   : Voyage AI embedding models
  - openai   : OpenAI embedding models
  - ollama   : Local Ollama embedding models
  - lmstudio : Local LM Studio embedding models`,
	}

	// Add persistent flags for all embedding commands
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")
	cmd.PersistentFlags().StringVar(&outputFormat, "output", "table", "Output format (json, yaml, table, raw)")

	// TLS flags
	cmd.PersistentFlags().BoolVar(&tlsEnabled, "tls", false, "Enable TLS")
	cmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS client certificate file")
	cmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS client key file")
	cmd.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA certificate file")

	// Connection flags
	cmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retry attempts")
	cmd.PersistentFlags().DurationVar(&retryBackoff, "retry-backoff", 100*time.Millisecond, "Initial retry backoff duration")

	// Add embedding operation commands
	cmd.AddCommand(newEmbeddingGenerateCommand())
	cmd.AddCommand(newEmbeddingBatchCommand())

	// Add provider subcommands
	providerCmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage embedding providers",
	}
	providerCmd.AddCommand(newEmbeddingProviderInfoCommand())
	providerCmd.AddCommand(newEmbeddingProviderSetDefaultCommand())
	cmd.AddCommand(providerCmd)

	// Add providers list command (shorthand)
	cmd.AddCommand(newEmbeddingProvidersCommand())

	// Add cache subcommands
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage embedding cache",
	}
	cacheCmd.AddCommand(newEmbeddingCacheStatsCommand())
	cacheCmd.AddCommand(newEmbeddingCacheClearCommand())
	cmd.AddCommand(cacheCmd)

	return cmd
}

// createEmbeddingClient creates an SDK embedding client.
func createEmbeddingClient() (embedding.Client, error) {
	client, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return client.Embedding(), nil
}

// =============================================================================
// Embedding Generation Commands
// =============================================================================

// newEmbeddingGenerateCommand creates the generate command.
func newEmbeddingGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <text>",
		Short: "Generate an embedding for text",
		Long: `Generate an embedding vector for a single text input.

The text is processed by the configured embedding provider and the result
is cached for subsequent requests (unless --cache-bypass is specified).

Examples:
  # Generate embedding with default provider
  quicbaas embedding generate "Hello, world!"

  # Generate with specific provider
  quicbaas embedding generate "Sample text" --provider openai

  # Generate with metadata
  quicbaas embedding generate "Document content" --metadata '{"doc_id":"123","type":"article"}'

  # Generate with input type hint
  quicbaas embedding generate "search query" --input-type query

  # Bypass cache and force regeneration
  quicbaas embedding generate "Hello" --cache-bypass`,
		Args: cobra.ExactArgs(1),
		RunE: runEmbeddingGenerateCommand,
	}

	cmd.Flags().StringVar(&embeddingProvider, "provider", "", "Embedding provider (voyage, openai, ollama, lmstudio)")
	cmd.Flags().StringVar(&embeddingMetadata, "metadata", "", "Metadata as JSON string")
	cmd.Flags().StringVar(&embeddingInputType, "input-type", "", "Input type hint (document, query, code)")
	cmd.Flags().StringVar(&embeddingSource, "source", "", "Source identifier (file path, URL, etc.)")
	cmd.Flags().StringVar(&embeddingLanguage, "language", "", "Programming or natural language")
	cmd.Flags().BoolVar(&embeddingCacheBypass, "cache-bypass", false, "Bypass cache and force regeneration")

	return cmd
}

// runEmbeddingGenerateCommand executes the generate command.
func runEmbeddingGenerateCommand(cmd *cobra.Command, args []string) error {
	text := args[0]

	if text == "" {
		return errors.Newf(errors.ErrInvalidInput, "embedding.generate", "text cannot be empty")
	}

	client, err := createEmbeddingClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Parse metadata if provided
	var metadata map[string]string
	if embeddingMetadata != "" {
		if err := json.Unmarshal([]byte(embeddingMetadata), &metadata); err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "embedding.generate", "invalid metadata JSON")
		}
	}

	req := &embedding.GenerateEmbeddingRequest{
		Text:        text,
		Provider:    embedding.Provider(embeddingProvider),
		InputType:   embedding.InputType(embeddingInputType),
		Metadata:    metadata,
		Source:      embeddingSource,
		Language:    embeddingLanguage,
		CacheBypass: embeddingCacheBypass,
	}

	resp, err := client.GenerateEmbedding(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatEmbeddingGenerateResponse(resp)
}

// newEmbeddingBatchCommand creates the batch command.
func newEmbeddingBatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch <texts-json>",
		Short: "Generate embeddings for multiple texts",
		Long: `Generate embedding vectors for multiple texts in a single request.

This is more efficient than individual requests as it leverages provider batch APIs
and parallel processing. Cached embeddings are returned immediately while uncached
texts are batched together for efficient processing.

The input should be a JSON array of text items or a path to a JSON file.

Examples:
  # Batch generate from JSON array
  quicbaas embedding batch '["text1", "text2", "text3"]'

  # Batch generate with full item specification
  quicbaas embedding batch '[{"id":"doc1","text":"Hello"},{"id":"doc2","text":"World"}]'

  # Batch generate from file
  quicbaas embedding batch @texts.json

  # Batch with specific provider
  quicbaas embedding batch '["text1", "text2"]' --provider voyage`,
		Args: cobra.ExactArgs(1),
		RunE: runEmbeddingBatchCommand,
	}

	cmd.Flags().StringVar(&embeddingProvider, "provider", "", "Embedding provider (voyage, openai, ollama, lmstudio)")
	cmd.Flags().BoolVar(&embeddingCacheBypass, "cache-bypass", false, "Bypass cache and force regeneration")

	return cmd
}

// runEmbeddingBatchCommand executes the batch command.
func runEmbeddingBatchCommand(cmd *cobra.Command, args []string) error {
	input := args[0]

	// Check if input is a file reference
	var jsonData []byte
	var err error
	if strings.HasPrefix(input, "@") {
		filePath := strings.TrimPrefix(input, "@")
		jsonData, err = os.ReadFile(filePath)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "embedding.batch", "failed to read file: %s", filePath)
		}
	} else {
		jsonData = []byte(input)
	}

	// Try to parse as array of strings first
	var simpleTexts []string
	if err := json.Unmarshal(jsonData, &simpleTexts); err == nil {
		// Convert simple strings to batch items
		items := make([]*embedding.BatchEmbeddingItem, len(simpleTexts))
		for i, text := range simpleTexts {
			items[i] = &embedding.BatchEmbeddingItem{
				ID:   fmt.Sprintf("item-%d", i),
				Text: text,
			}
		}
		return runBatchGenerate(items)
	}

	// Try to parse as array of batch items
	var batchItems []*embedding.BatchEmbeddingItem
	if err := json.Unmarshal(jsonData, &batchItems); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "embedding.batch", "invalid JSON input")
	}

	return runBatchGenerate(batchItems)
}

// runBatchGenerate executes the batch embedding generation.
func runBatchGenerate(items []*embedding.BatchEmbeddingItem) error {
	if len(items) == 0 {
		return errors.Newf(errors.ErrInvalidInput, "embedding.batch", "at least one text item is required")
	}

	client, err := createEmbeddingClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &embedding.BatchGenerateEmbeddingsRequest{
		Items:       items,
		Provider:    embedding.Provider(embeddingProvider),
		CacheBypass: embeddingCacheBypass,
	}

	resp, err := client.BatchGenerateEmbeddings(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatEmbeddingBatchResponse(resp)
}

// =============================================================================
// Provider Commands
// =============================================================================

// newEmbeddingProvidersCommand creates the providers list command.
func newEmbeddingProvidersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "List all available embedding providers",
		Long: `List all registered embedding providers with their capabilities.

This includes provider name, model, dimensions, max tokens, and availability status.

Examples:
  # List available providers
  quicbaas embedding providers

  # Include unavailable providers
  quicbaas embedding providers --include-unavailable`,
		RunE: runEmbeddingProvidersCommand,
	}

	cmd.Flags().BoolVar(&embeddingIncludeUnavailable, "include-unavailable", false, "Include unavailable providers")

	return cmd
}

// runEmbeddingProvidersCommand executes the providers list command.
func runEmbeddingProvidersCommand(cmd *cobra.Command, args []string) error {
	client, err := createEmbeddingClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &embedding.ListProvidersRequest{
		IncludeUnavailable: embeddingIncludeUnavailable,
	}

	resp, err := client.ListProviders(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatEmbeddingProvidersResponse(resp)
}

// newEmbeddingProviderInfoCommand creates the provider info command.
func newEmbeddingProviderInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Get detailed information about a provider",
		Long: `Retrieve detailed information about a specific embedding provider.

This includes configuration, model details, rate limits, and current status.

Examples:
  quicbaas embedding provider info voyage
  quicbaas embedding provider info openai`,
		Args: cobra.ExactArgs(1),
		RunE: runEmbeddingProviderInfoCommand,
	}

	return cmd
}

// runEmbeddingProviderInfoCommand executes the provider info command.
func runEmbeddingProviderInfoCommand(cmd *cobra.Command, args []string) error {
	providerName := args[0]

	client, err := createEmbeddingClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &embedding.GetProviderInfoRequest{
		Provider: embedding.Provider(providerName),
	}

	resp, err := client.GetProviderInfo(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatEmbeddingProviderInfoResponse(resp)
}

// newEmbeddingProviderSetDefaultCommand creates the provider set-default command.
func newEmbeddingProviderSetDefaultCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-default <name>",
		Short: "Set the default embedding provider",
		Long: `Set the default embedding provider to use for requests
that don't explicitly specify a provider.

Examples:
  quicbaas embedding provider set-default voyage
  quicbaas embedding provider set-default openai`,
		Args: cobra.ExactArgs(1),
		RunE: runEmbeddingProviderSetDefaultCommand,
	}

	return cmd
}

// runEmbeddingProviderSetDefaultCommand executes the provider set-default command.
func runEmbeddingProviderSetDefaultCommand(cmd *cobra.Command, args []string) error {
	providerName := args[0]

	client, err := createEmbeddingClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &embedding.SetDefaultProviderRequest{
		Provider: embedding.Provider(providerName),
	}

	resp, err := client.SetDefaultProvider(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatEmbeddingSetDefaultResponse(resp)
}

// =============================================================================
// Cache Commands
// =============================================================================

// newEmbeddingCacheStatsCommand creates the cache stats command.
func newEmbeddingCacheStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Get embedding cache statistics",
		Long: `Retrieve statistics about the embedding cache.

This includes hit rate, size, evictions, and memory usage.

Examples:
  quicbaas embedding cache stats`,
		RunE: runEmbeddingCacheStatsCommand,
	}

	return cmd
}

// runEmbeddingCacheStatsCommand executes the cache stats command.
func runEmbeddingCacheStatsCommand(cmd *cobra.Command, args []string) error {
	client, err := createEmbeddingClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &embedding.GetEmbeddingCacheStatsRequest{}

	resp, err := client.GetCacheStats(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatEmbeddingCacheStatsResponse(resp)
}

// newEmbeddingCacheClearCommand creates the cache clear command.
func newEmbeddingCacheClearCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear the embedding cache",
		Long: `Invalidate all or specific embedding cache entries.

This can be scoped to a specific provider or clear the entire cache.

Examples:
  # Clear entire cache
  quicbaas embedding cache clear

  # Clear cache for specific provider
  quicbaas embedding cache clear --provider voyage`,
		RunE: runEmbeddingCacheClearCommand,
	}

	cmd.Flags().StringVar(&embeddingProvider, "provider", "", "Clear cache only for this provider")

	return cmd
}

// runEmbeddingCacheClearCommand executes the cache clear command.
func runEmbeddingCacheClearCommand(cmd *cobra.Command, args []string) error {
	client, err := createEmbeddingClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &embedding.ClearEmbeddingCacheRequest{
		Provider: embedding.Provider(embeddingProvider),
		All:      embeddingProvider == "",
	}

	resp, err := client.ClearCache(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatEmbeddingCacheClearResponse(resp)
}

// =============================================================================
// Output Formatting Functions
// =============================================================================

// metadataToSnakeCaseMap converts embedding Metadata to a snake_case map for JSON output.
func metadataToSnakeCaseMap(m *embedding.Metadata) map[string]interface{} {
	return map[string]interface{}{
		"model":           m.Model,
		"provider":        string(m.Provider),
		"dimensions":      m.Dimensions,
		"tokens":          m.Tokens,
		"source":          m.Source,
		"language":        m.Language,
		"input_type":      string(m.InputType),
		"tags":            m.Tags,
		"cache_hit":       m.CacheHit,
		"processing_time": m.ProcessingTime.String(),
	}
}

// providerInfoToSnakeCaseMap converts ProviderInfo to a snake_case map for JSON output.
func providerInfoToSnakeCaseMap(p *embedding.ProviderInfo) map[string]interface{} {
	info := map[string]interface{}{
		"name":                  p.Name,
		"type":                  string(p.Type),
		"model":                 p.Model,
		"dimensions":            p.Dimensions,
		"max_tokens":            p.MaxTokens,
		"max_batch_size":        p.MaxBatchSize,
		"status":                string(p.Status),
		"api_url":               p.APIURL,
		"rate_limit_per_minute": p.RateLimitPerMinute,
		"timeout":               p.Timeout.String(),
		"is_default":            p.IsDefault,
		"last_error":            p.LastError,
		"success_count":         p.SuccessCount,
		"error_count":           p.ErrorCount,
		"avg_latency":           p.AvgLatency.String(),
	}

	if p.LastErrorTime != nil {
		info["last_error_time"] = p.LastErrorTime.Format(time.RFC3339)
	}

	return info
}

// formatEmbeddingGenerateResponse formats the generate embedding response.
func formatEmbeddingGenerateResponse(resp *embedding.GenerateEmbeddingResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"embedding": resp.Embedding,
			"timestamp": resp.Timestamp,
		}
		if resp.Metadata != nil {
			output["metadata"] = metadataToSnakeCaseMap(resp.Metadata)
		}
		data, err := json.MarshalIndent(output, "", "  ")
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

	case cli.OutputFormatRaw:
		// Output just the embedding vector
		data, err := json.Marshal(resp.Embedding)
		if err != nil {
			return err
		}
		fmt.Println(string(data))

	default:
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nEMBEDDING GENERATED\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))

		if resp.Metadata != nil {
			fmt.Fprintf(tw, "Provider:\t%s\n", resp.Metadata.Provider)
			fmt.Fprintf(tw, "Model:\t%s\n", resp.Metadata.Model)
			fmt.Fprintf(tw, "Dimensions:\t%d\n", resp.Metadata.Dimensions)
			fmt.Fprintf(tw, "Tokens:\t%d\n", resp.Metadata.Tokens)
			fmt.Fprintf(tw, "Cache Hit:\t%v\n", resp.Metadata.CacheHit)
			fmt.Fprintf(tw, "Processing Time:\t%s\n", resp.Metadata.ProcessingTime)
		}

		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))

		// Show first few dimensions of embedding
		if len(resp.Embedding) > 0 {
			fmt.Fprintf(tw, "\nEmbedding (%d dimensions):\n", len(resp.Embedding))
			displayCount := 5
			if len(resp.Embedding) < displayCount {
				displayCount = len(resp.Embedding)
			}
			fmt.Fprintf(tw, "  [")
			for i := 0; i < displayCount; i++ {
				if i > 0 {
					fmt.Fprintf(tw, ", ")
				}
				fmt.Fprintf(tw, "%.6f", resp.Embedding[i])
			}
			if len(resp.Embedding) > displayCount {
				fmt.Fprintf(tw, ", ... %d more", len(resp.Embedding)-displayCount)
			}
			fmt.Fprintf(tw, "]\n")
		}

		tw.Flush()
	}

	return nil
}

// formatEmbeddingBatchResponse formats the batch generate response.
func formatEmbeddingBatchResponse(resp *embedding.BatchGenerateEmbeddingsResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		results := make([]map[string]interface{}, len(resp.Results))
		for i, r := range resp.Results {
			result := map[string]interface{}{
				"id":        r.ID,
				"embedding": r.Embedding,
				"error":     r.Error,
			}
			if r.Metadata != nil {
				result["metadata"] = metadataToSnakeCaseMap(r.Metadata)
			}
			results[i] = result
		}
		output := map[string]interface{}{
			"results":         results,
			"success_count":   resp.SuccessCount,
			"error_count":     resp.ErrorCount,
			"total_tokens":    resp.TotalTokens,
			"processing_time": resp.ProcessingTime.String(),
			"timestamp":       resp.Timestamp,
		}
		data, err := json.MarshalIndent(output, "", "  ")
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
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nBATCH EMBEDDING RESULTS\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "Success Count:\t%d\n", resp.SuccessCount)
		fmt.Fprintf(tw, "Error Count:\t%d\n", resp.ErrorCount)
		fmt.Fprintf(tw, "Total Tokens:\t%d\n", resp.TotalTokens)
		fmt.Fprintf(tw, "Processing Time:\t%s\n", resp.ProcessingTime)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		fmt.Fprintf(tw, "\n")

		fmt.Fprintf(tw, "ID\tDIMENSIONS\tCACHE\tERROR\n")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			strings.Repeat("-", 20),
			strings.Repeat("-", 10),
			strings.Repeat("-", 5),
			strings.Repeat("-", 30))

		for _, result := range resp.Results {
			cacheHit := "no"
			if result.Metadata != nil && result.Metadata.CacheHit {
				cacheHit = "yes"
			}
			errMsg := "-"
			if result.Error != "" {
				errMsg = result.Error
			}
			dims := len(result.Embedding)
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", result.ID, dims, cacheHit, errMsg)
		}

		tw.Flush()
	}

	return nil
}

// formatEmbeddingProvidersResponse formats the providers list response.
func formatEmbeddingProvidersResponse(resp *embedding.ListProvidersResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		providers := make([]map[string]interface{}, len(resp.Providers))
		for i, p := range resp.Providers {
			providers[i] = providerInfoToSnakeCaseMap(p)
		}
		output := map[string]interface{}{
			"providers":        providers,
			"default_provider": string(resp.DefaultProvider),
			"timestamp":        resp.Timestamp,
		}
		data, err := json.MarshalIndent(output, "", "  ")
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
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nEMBEDDING PROVIDERS\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 100))
		fmt.Fprintf(tw, "Default Provider: %s\n\n", resp.DefaultProvider)

		fmt.Fprintf(tw, "NAME\tTYPE\tMODEL\tDIMENSIONS\tMAX TOKENS\tSTATUS\tDEFAULT\n")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			strings.Repeat("-", 12),
			strings.Repeat("-", 10),
			strings.Repeat("-", 25),
			strings.Repeat("-", 10),
			strings.Repeat("-", 10),
			strings.Repeat("-", 12),
			strings.Repeat("-", 7))

		for _, p := range resp.Providers {
			defaultStr := ""
			if p.IsDefault {
				defaultStr = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				p.Name,
				p.Type,
				p.Model,
				p.Dimensions,
				p.MaxTokens,
				p.Status,
				defaultStr)
		}

		tw.Flush()
	}

	return nil
}

// formatEmbeddingProviderInfoResponse formats the provider info response.
func formatEmbeddingProviderInfoResponse(resp *embedding.GetProviderInfoResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"info":      providerInfoToSnakeCaseMap(resp.Info),
			"timestamp": resp.Timestamp,
		}
		data, err := json.MarshalIndent(output, "", "  ")
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
		info := resp.Info
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nPROVIDER INFORMATION\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "Name:\t%s\n", info.Name)
		fmt.Fprintf(tw, "Type:\t%s\n", info.Type)
		fmt.Fprintf(tw, "Model:\t%s\n", info.Model)
		fmt.Fprintf(tw, "Dimensions:\t%d\n", info.Dimensions)
		fmt.Fprintf(tw, "Max Tokens:\t%d\n", info.MaxTokens)
		fmt.Fprintf(tw, "Max Batch Size:\t%d\n", info.MaxBatchSize)
		fmt.Fprintf(tw, "Status:\t%s\n", info.Status)
		fmt.Fprintf(tw, "Is Default:\t%v\n", info.IsDefault)
		fmt.Fprintf(tw, "API URL:\t%s\n", info.APIURL)
		fmt.Fprintf(tw, "Timeout:\t%s\n", info.Timeout)

		if info.RateLimitPerMinute > 0 {
			fmt.Fprintf(tw, "Rate Limit:\t%d/min\n", info.RateLimitPerMinute)
		}

		fmt.Fprintf(tw, "\nStatistics:\n")
		fmt.Fprintf(tw, "  Success Count:\t%d\n", info.SuccessCount)
		fmt.Fprintf(tw, "  Error Count:\t%d\n", info.ErrorCount)
		fmt.Fprintf(tw, "  Avg Latency:\t%s\n", info.AvgLatency)

		if info.LastError != "" {
			fmt.Fprintf(tw, "\nLast Error:\n")
			fmt.Fprintf(tw, "  Message:\t%s\n", info.LastError)
			if info.LastErrorTime != nil {
				fmt.Fprintf(tw, "  Time:\t%s\n", info.LastErrorTime.Format(time.RFC3339))
			}
		}

		tw.Flush()
	}

	return nil
}

// formatEmbeddingSetDefaultResponse formats the set default provider response.
func formatEmbeddingSetDefaultResponse(resp *embedding.SetDefaultProviderResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"provider":          string(resp.Provider),
			"previous_provider": string(resp.PreviousProvider),
			"timestamp":         resp.Timestamp,
		}
		data, err := json.MarshalIndent(output, "", "  ")
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
		fmt.Printf("Default provider changed from '%s' to '%s'\n",
			resp.PreviousProvider, resp.Provider)
		fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
	}

	return nil
}

// formatEmbeddingCacheStatsResponse formats the cache stats response.
func formatEmbeddingCacheStatsResponse(resp *embedding.GetEmbeddingCacheStatsResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"enabled":             resp.Enabled,
			"size":                resp.Size,
			"capacity":            resp.Capacity,
			"hits":                resp.Hits,
			"misses":              resp.Misses,
			"hit_rate":            resp.HitRate,
			"evictions":           resp.Evictions,
			"memory_bytes":        resp.MemoryBytes,
			"compression_enabled": resp.CompressionEnabled,
			"compression_ratio":   resp.CompressionRatio,
			"timestamp":           resp.Timestamp,
		}
		data, err := json.MarshalIndent(output, "", "  ")
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
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nEMBEDDING CACHE STATISTICS\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "Enabled:\t%v\n", resp.Enabled)
		fmt.Fprintf(tw, "Size:\t%d / %d entries (%.1f%% full)\n",
			resp.Size, resp.Capacity,
			float64(resp.Size)/float64(resp.Capacity)*100)
		fmt.Fprintf(tw, "Hits:\t%d\n", resp.Hits)
		fmt.Fprintf(tw, "Misses:\t%d\n", resp.Misses)
		fmt.Fprintf(tw, "Hit Rate:\t%.2f%%\n", resp.HitRate*100)
		fmt.Fprintf(tw, "Evictions:\t%d\n", resp.Evictions)

		if resp.MemoryBytes > 0 {
			memMB := float64(resp.MemoryBytes) / (1024 * 1024)
			fmt.Fprintf(tw, "Memory Usage:\t%d bytes (%.2f MB)\n", resp.MemoryBytes, memMB)
		}

		fmt.Fprintf(tw, "Compression:\t%v\n", resp.CompressionEnabled)
		if resp.CompressionRatio > 0 {
			fmt.Fprintf(tw, "Compression Ratio:\t%.2f\n", resp.CompressionRatio)
		}

		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		tw.Flush()
	}

	return nil
}

// formatEmbeddingCacheClearResponse formats the cache clear response.
func formatEmbeddingCacheClearResponse(resp *embedding.ClearEmbeddingCacheResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"entries_cleared": resp.EntriesCleared,
			"provider":        string(resp.Provider),
			"timestamp":       resp.Timestamp,
		}
		data, err := json.MarshalIndent(output, "", "  ")
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
		if resp.Provider != "" {
			fmt.Printf("Cache cleared for provider '%s': %d entries removed\n",
				resp.Provider, resp.EntriesCleared)
		} else {
			fmt.Printf("Entire cache cleared: %d entries removed\n", resp.EntriesCleared)
		}
		fmt.Printf("Timestamp: %s\n", resp.Timestamp.Format(time.RFC3339))
	}

	return nil
}
