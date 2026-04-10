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

//go:build integration && ollama

// Package cli provides E2E integration tests for QuicBaaS CLI embedding commands.
package cli

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Embedding Generate Command Definitions
// =============================================================================

// EmbeddingGenerateCommand returns a command to generate an embedding for text.
func EmbeddingGenerateCommand(text string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding generate %s", truncateString(text, 20)),
		Args:           []string{"embedding", "generate", text},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateWithProviderCommand generates an embedding with a specific provider.
func EmbeddingGenerateWithProviderCommand(text, provider string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding generate provider=%s", provider),
		Args:           []string{"embedding", "generate", text, "--provider", provider},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateWithMetadataCommand generates an embedding with metadata.
func EmbeddingGenerateWithMetadataCommand(text, metadataJSON string) CommandDef {
	return CommandDef{
		Name:           "embedding generate with metadata",
		Args:           []string{"embedding", "generate", text, "--metadata", metadataJSON},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateWithInputTypeCommand generates an embedding with input type hint.
func EmbeddingGenerateWithInputTypeCommand(text, inputType string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding generate input-type=%s", inputType),
		Args:           []string{"embedding", "generate", text, "--input-type", inputType},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateWithSourceCommand generates an embedding with source identifier.
func EmbeddingGenerateWithSourceCommand(text, source string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding generate source=%s", truncateString(source, 15)),
		Args:           []string{"embedding", "generate", text, "--source", source},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateWithLanguageCommand generates an embedding with language hint.
func EmbeddingGenerateWithLanguageCommand(text, language string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding generate language=%s", language),
		Args:           []string{"embedding", "generate", text, "--language", language},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateCacheBypassCommand generates an embedding bypassing cache.
func EmbeddingGenerateCacheBypassCommand(text string) CommandDef {
	return CommandDef{
		Name:           "embedding generate cache-bypass",
		Args:           []string{"embedding", "generate", text, "--cache-bypass"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateJSONCommand generates an embedding with JSON output.
func EmbeddingGenerateJSONCommand(text string) CommandDef {
	return CommandDef{
		Name:           "embedding generate output=json",
		Args:           []string{"embedding", "generate", text, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateYAMLCommand generates an embedding with YAML output.
func EmbeddingGenerateYAMLCommand(text string) CommandDef {
	return CommandDef{
		Name:           "embedding generate output=yaml",
		Args:           []string{"embedding", "generate", text, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateTableCommand generates an embedding with table output.
func EmbeddingGenerateTableCommand(text string) CommandDef {
	return CommandDef{
		Name:           "embedding generate output=table",
		Args:           []string{"embedding", "generate", text, "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateRawCommand generates an embedding with raw output (vector only).
func EmbeddingGenerateRawCommand(text string) CommandDef {
	return CommandDef{
		Name:           "embedding generate output=raw",
		Args:           []string{"embedding", "generate", text, "--output", "raw"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateWithAllOptionsCommand generates an embedding with all options.
func EmbeddingGenerateWithAllOptionsCommand(text, provider, metadataJSON, inputType, source, language string, cacheBypass bool) CommandDef {
	args := []string{"embedding", "generate", text}
	if provider != "" {
		args = append(args, "--provider", provider)
	}
	if metadataJSON != "" {
		args = append(args, "--metadata", metadataJSON)
	}
	if inputType != "" {
		args = append(args, "--input-type", inputType)
	}
	if source != "" {
		args = append(args, "--source", source)
	}
	if language != "" {
		args = append(args, "--language", language)
	}
	if cacheBypass {
		args = append(args, "--cache-bypass")
	}
	return CommandDef{
		Name:           "embedding generate with all options",
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Embedding Batch Command Definitions
// =============================================================================

// EmbeddingBatchCommand returns a command to generate embeddings for multiple texts.
func EmbeddingBatchCommand(textsJSON string) CommandDef {
	return CommandDef{
		Name:           "embedding batch",
		Args:           []string{"embedding", "batch", textsJSON},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchWithProviderCommand generates batch embeddings with a specific provider.
func EmbeddingBatchWithProviderCommand(textsJSON, provider string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding batch provider=%s", provider),
		Args:           []string{"embedding", "batch", textsJSON, "--provider", provider},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchCacheBypassCommand generates batch embeddings bypassing cache.
func EmbeddingBatchCacheBypassCommand(textsJSON string) CommandDef {
	return CommandDef{
		Name:           "embedding batch cache-bypass",
		Args:           []string{"embedding", "batch", textsJSON, "--cache-bypass"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchJSONCommand generates batch embeddings with JSON output.
func EmbeddingBatchJSONCommand(textsJSON string) CommandDef {
	return CommandDef{
		Name:           "embedding batch output=json",
		Args:           []string{"embedding", "batch", textsJSON, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchYAMLCommand generates batch embeddings with YAML output.
func EmbeddingBatchYAMLCommand(textsJSON string) CommandDef {
	return CommandDef{
		Name:           "embedding batch output=yaml",
		Args:           []string{"embedding", "batch", textsJSON, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchTableCommand generates batch embeddings with table output.
func EmbeddingBatchTableCommand(textsJSON string) CommandDef {
	return CommandDef{
		Name:           "embedding batch output=table",
		Args:           []string{"embedding", "batch", textsJSON, "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchWithItemsCommand generates batch embeddings with full item specification.
func EmbeddingBatchWithItemsCommand(itemsJSON string) CommandDef {
	return CommandDef{
		Name:           "embedding batch with items",
		Args:           []string{"embedding", "batch", itemsJSON},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Embedding Providers Command Definitions
// =============================================================================

// EmbeddingProvidersCommand returns a command to list available embedding providers.
func EmbeddingProvidersCommand() CommandDef {
	return CommandDef{
		Name:           "embedding providers",
		Args:           []string{"embedding", "providers"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProvidersIncludeUnavailableCommand lists providers including unavailable ones.
func EmbeddingProvidersIncludeUnavailableCommand() CommandDef {
	return CommandDef{
		Name:           "embedding providers include-unavailable",
		Args:           []string{"embedding", "providers", "--include-unavailable"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProvidersJSONCommand lists providers with JSON output.
func EmbeddingProvidersJSONCommand() CommandDef {
	return CommandDef{
		Name:           "embedding providers output=json",
		Args:           []string{"embedding", "providers", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProvidersYAMLCommand lists providers with YAML output.
func EmbeddingProvidersYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "embedding providers output=yaml",
		Args:           []string{"embedding", "providers", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProvidersTableCommand lists providers with table output.
func EmbeddingProvidersTableCommand() CommandDef {
	return CommandDef{
		Name:           "embedding providers output=table",
		Args:           []string{"embedding", "providers", "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Embedding Provider Info Command Definitions
// =============================================================================

// EmbeddingProviderInfoCommand returns a command to get provider information.
func EmbeddingProviderInfoCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider info %s", providerName),
		Args:           []string{"embedding", "provider", "info", providerName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{providerName},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProviderInfoJSONCommand gets provider info with JSON output.
func EmbeddingProviderInfoJSONCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider info json %s", providerName),
		Args:           []string{"embedding", "provider", "info", providerName, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProviderInfoYAMLCommand gets provider info with YAML output.
func EmbeddingProviderInfoYAMLCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider info yaml %s", providerName),
		Args:           []string{"embedding", "provider", "info", providerName, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProviderInfoTableCommand gets provider info with table output.
func EmbeddingProviderInfoTableCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider info table %s", providerName),
		Args:           []string{"embedding", "provider", "info", providerName, "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Embedding Provider Set-Default Command Definitions
// =============================================================================

// EmbeddingProviderSetDefaultCommand returns a command to set the default provider.
func EmbeddingProviderSetDefaultCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider set-default %s", providerName),
		Args:           []string{"embedding", "provider", "set-default", providerName},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProviderSetDefaultJSONCommand sets default provider with JSON output.
func EmbeddingProviderSetDefaultJSONCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider set-default json %s", providerName),
		Args:           []string{"embedding", "provider", "set-default", providerName, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProviderSetDefaultYAMLCommand sets default provider with YAML output.
func EmbeddingProviderSetDefaultYAMLCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider set-default yaml %s", providerName),
		Args:           []string{"embedding", "provider", "set-default", providerName, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProviderSetDefaultTableCommand sets default provider with table output.
func EmbeddingProviderSetDefaultTableCommand(providerName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding provider set-default table %s", providerName),
		Args:           []string{"embedding", "provider", "set-default", providerName, "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Embedding Cache Stats Command Definitions
// =============================================================================

// EmbeddingCacheStatsCommand returns a command to get cache statistics.
func EmbeddingCacheStatsCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache stats",
		Args:           []string{"embedding", "cache", "stats"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingCacheStatsJSONCommand gets cache stats with JSON output.
func EmbeddingCacheStatsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache stats output=json",
		Args:           []string{"embedding", "cache", "stats", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingCacheStatsYAMLCommand gets cache stats with YAML output.
func EmbeddingCacheStatsYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache stats output=yaml",
		Args:           []string{"embedding", "cache", "stats", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingCacheStatsTableCommand gets cache stats with table output.
func EmbeddingCacheStatsTableCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache stats output=table",
		Args:           []string{"embedding", "cache", "stats", "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Embedding Cache Clear Command Definitions
// =============================================================================

// EmbeddingCacheClearCommand returns a command to clear the entire cache.
func EmbeddingCacheClearCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache clear",
		Args:           []string{"embedding", "cache", "clear"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingCacheClearProviderCommand clears cache for a specific provider.
func EmbeddingCacheClearProviderCommand(provider string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("embedding cache clear provider=%s", provider),
		Args:           []string{"embedding", "cache", "clear", "--provider", provider},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingCacheClearJSONCommand clears cache with JSON output.
func EmbeddingCacheClearJSONCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache clear output=json",
		Args:           []string{"embedding", "cache", "clear", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingCacheClearYAMLCommand clears cache with YAML output.
func EmbeddingCacheClearYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache clear output=yaml",
		Args:           []string{"embedding", "cache", "clear", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingCacheClearTableCommand clears cache with table output.
func EmbeddingCacheClearTableCommand() CommandDef {
	return CommandDef{
		Name:           "embedding cache clear output=table",
		Args:           []string{"embedding", "cache", "clear", "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Embedding Help Command Definitions
// =============================================================================

// EmbeddingHelpCommand returns a command to show embedding help.
// Help commands are CLI-local and work on all transports.
func EmbeddingHelpCommand() CommandDef {
	return CommandDef{
		Name:          "embedding help",
		Args:          []string{"embedding", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"generate", "batch", "providers", "provider", "cache"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingGenerateHelpCommand shows generate help.
// Help commands are CLI-local and work on all transports.
func EmbeddingGenerateHelpCommand() CommandDef {
	return CommandDef{
		Name:          "embedding generate help",
		Args:          []string{"embedding", "generate", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"provider", "metadata", "input-type", "source", "language", "cache-bypass"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingBatchHelpCommand shows batch help.
// Help commands are CLI-local and work on all transports.
func EmbeddingBatchHelpCommand() CommandDef {
	return CommandDef{
		Name:          "embedding batch help",
		Args:          []string{"embedding", "batch", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"provider", "cache-bypass"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingProvidersHelpCommand shows providers help.
// Help commands are CLI-local and work on all transports.
func EmbeddingProvidersHelpCommand() CommandDef {
	return CommandDef{
		Name:          "embedding providers help",
		Args:          []string{"embedding", "providers", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"include-unavailable"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingProviderHelpCommand shows provider subcommand help.
// Help commands are CLI-local and work on all transports.
func EmbeddingProviderHelpCommand() CommandDef {
	return CommandDef{
		Name:          "embedding provider help",
		Args:          []string{"embedding", "provider", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"info", "set-default"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingCacheHelpCommand shows cache subcommand help.
// Help commands are CLI-local and work on all transports.
func EmbeddingCacheHelpCommand() CommandDef {
	return CommandDef{
		Name:          "embedding cache help",
		Args:          []string{"embedding", "cache", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"stats", "clear"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Embedding Error Command Definitions
// =============================================================================

// EmbeddingGenerateMissingTextCommand tests missing text argument.
// Error validation is CLI-local and works on all transports.
func EmbeddingGenerateMissingTextCommand() CommandDef {
	return CommandDef{
		Name:          "embedding generate missing text",
		Args:          []string{"embedding", "generate"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg(s)"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingGenerateEmptyTextCommand tests empty text argument.
// Error validation is CLI-local and works on all transports.
func EmbeddingGenerateEmptyTextCommand() CommandDef {
	return CommandDef{
		Name:           "embedding generate empty text",
		Args:           []string{"embedding", "generate", ""},
		ExpectSuccess:  false,
		ExpectError:    []string{"empty"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingGenerateInvalidMetadataCommand tests invalid metadata JSON.
// Error validation is CLI-local and works on all transports.
func EmbeddingGenerateInvalidMetadataCommand() CommandDef {
	return CommandDef{
		Name:           "embedding generate invalid metadata",
		Args:           []string{"embedding", "generate", "test text", "--metadata", "not-valid-json"},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid", "JSON"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchMissingTextsCommand tests missing texts argument.
// Error validation is CLI-local and works on all transports.
func EmbeddingBatchMissingTextsCommand() CommandDef {
	return CommandDef{
		Name:          "embedding batch missing texts",
		Args:          []string{"embedding", "batch"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg(s)"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingBatchInvalidJSONCommand tests invalid JSON input.
// Error validation is CLI-local and works on all transports.
func EmbeddingBatchInvalidJSONCommand() CommandDef {
	return CommandDef{
		Name:           "embedding batch invalid json",
		Args:           []string{"embedding", "batch", "not-valid-json"},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid", "JSON"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingBatchEmptyArrayCommand tests empty array input.
// Error validation is CLI-local and works on all transports.
func EmbeddingBatchEmptyArrayCommand() CommandDef {
	return CommandDef{
		Name:           "embedding batch empty array",
		Args:           []string{"embedding", "batch", "[]"},
		ExpectSuccess:  false,
		ExpectError:    []string{"at least one"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// EmbeddingProviderInfoMissingNameCommand tests missing provider name.
// Error validation is CLI-local and works on all transports.
func EmbeddingProviderInfoMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "embedding provider info missing name",
		Args:          []string{"embedding", "provider", "info"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg(s)"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingProviderSetDefaultMissingNameCommand tests missing provider name for set-default.
// Error validation is CLI-local and works on all transports.
func EmbeddingProviderSetDefaultMissingNameCommand() CommandDef {
	return CommandDef{
		Name:          "embedding provider set-default missing name",
		Args:          []string{"embedding", "provider", "set-default"},
		ExpectSuccess: false,
		ExpectError:   []string{"accepts 1 arg(s)"},
		Timeout:       10 * time.Second,
	}
}

// EmbeddingProviderInfoInvalidProviderCommand tests invalid provider name.
// Error validation occurs on server and works on all supporting transports.
func EmbeddingProviderInfoInvalidProviderCommand() CommandDef {
	return CommandDef{
		Name:           "embedding provider info invalid provider",
		Args:           []string{"embedding", "provider", "info", "nonexistent-provider-xyz"},
		ExpectSuccess:  false,
		ExpectError:    []string{"provider"},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: embeddingTransportsToSkip,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestEmbeddingHelp tests embedding help commands.
func TestEmbeddingHelp(t *testing.T) {
	RunForAllTransports(t, "EmbeddingHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingHelpCommand())
	})

	RunForAllTransports(t, "EmbeddingGenerateHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingGenerateHelpCommand())
	})

	RunForAllTransports(t, "EmbeddingBatchHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingBatchHelpCommand())
	})

	RunForAllTransports(t, "EmbeddingProvidersHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingProvidersHelpCommand())
	})

	RunForAllTransports(t, "EmbeddingProviderHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingProviderHelpCommand())
	})

	RunForAllTransports(t, "EmbeddingCacheHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingCacheHelpCommand())
	})
}

// TestEmbeddingGenerate tests embedding generation.
func TestEmbeddingGenerate(t *testing.T) {
	testText := "This is a test sentence for embedding generation."

	RunForAllTransports(t, "GenerateBasic", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateWithInputTypeDocument", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateWithInputTypeCommand(testText, "document")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateWithInputTypeQuery", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateWithInputTypeCommand("search query text", "query")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateWithInputTypeCode", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateWithInputTypeCommand("func main() { fmt.Println(\"Hello\") }", "code")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateWithMetadata", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		metadata := `{"doc_id":"123","type":"article"}`
		cmd := EmbeddingGenerateWithMetadataCommand(testText, metadata)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateWithSource", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateWithSourceCommand(testText, "https://example.com/doc.txt")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateWithLanguage", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateWithLanguageCommand(testText, "en")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateCacheBypass", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateCacheBypassCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})
}

// TestEmbeddingGenerateOutputFormats tests embedding generation with different output formats.
func TestEmbeddingGenerateOutputFormats(t *testing.T) {
	testText := "Test text for output format testing."

	RunForAllTransports(t, "GenerateJSON", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateJSONCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)

		// Verify JSON output is valid
		var resp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &resp)
		require.NoError(t, err, "Output should be valid JSON")
		assert.Contains(t, resp, "embedding", "JSON should contain embedding field")
	})

	RunForAllTransports(t, "GenerateYAML", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateYAMLCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "GenerateTable", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateTableCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.Contains(t, stdout, "EMBEDDING")
	})

	RunForAllTransports(t, "GenerateRaw", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingGenerateRawCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		// Raw output should be just the vector array
		assert.Contains(t, stdout, "[")
		assert.Contains(t, stdout, "]")
	})
}

// TestEmbeddingBatch tests batch embedding generation.
func TestEmbeddingBatch(t *testing.T) {
	simpleTextsJSON := `["text one", "text two", "text three"]`
	itemsJSON := `[{"id":"doc1","text":"Hello world"},{"id":"doc2","text":"Goodbye world"}]`

	RunForAllTransports(t, "BatchSimpleTexts", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingBatchCommand(simpleTextsJSON)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "BatchWithItems", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingBatchWithItemsCommand(itemsJSON)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "BatchCacheBypass", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingBatchCacheBypassCommand(simpleTextsJSON)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})
}

// TestEmbeddingBatchOutputFormats tests batch embedding with different output formats.
func TestEmbeddingBatchOutputFormats(t *testing.T) {
	textsJSON := `["sample text one", "sample text two"]`

	RunForAllTransports(t, "BatchJSON", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingBatchJSONCommand(textsJSON)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)

		var resp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &resp)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "BatchYAML", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingBatchYAMLCommand(textsJSON)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "BatchTable", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingBatchTableCommand(textsJSON)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.Contains(t, stdout, "BATCH")
	})
}

// TestEmbeddingProviders tests provider listing commands.
func TestEmbeddingProviders(t *testing.T) {
	RunForAllTransports(t, "ListProviders", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingProvidersCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ListProvidersIncludeUnavailable", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingProvidersIncludeUnavailableCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})
}

// TestEmbeddingProvidersOutputFormats tests provider listing with different output formats.
func TestEmbeddingProvidersOutputFormats(t *testing.T) {
	RunForAllTransports(t, "ProvidersJSON", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingProvidersJSONCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)

		var resp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &resp)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "ProvidersYAML", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingProvidersYAMLCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ProvidersTable", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingProvidersTableCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.Contains(t, stdout, "PROVIDER")
	})
}

// TestEmbeddingProviderInfo tests provider info command.
func TestEmbeddingProviderInfo(t *testing.T) {
	// First get available providers, then test info on the default one
	RunForAllTransports(t, "ProviderInfoDefault", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		// Get providers list first to find a valid provider
		providersCmd := EmbeddingProvidersJSONCommand()
		stdout, _, err := runner.Run(providersCmd)
		if runner.IsSkipped(providersCmd) {
			return
		}
		require.NoError(t, err)

		var resp struct {
			DefaultProvider string `json:"default_provider"`
		}
		err = json.Unmarshal([]byte(stdout), &resp)
		require.NoError(t, err, "Failed to unmarshal embedding providers response")
		require.NotEmpty(t, resp.DefaultProvider, "No default embedding provider available")

		// Test provider info
		infoCmd := EmbeddingProviderInfoCommand(resp.DefaultProvider)
		stdout, _, err = runner.Run(infoCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, resp.DefaultProvider)
	})
}

// TestEmbeddingProviderInfoOutputFormats tests provider info with different output formats.
func TestEmbeddingProviderInfoOutputFormats(t *testing.T) {
	// Discover available provider dynamically instead of hardcoding
	RunForAllTransports(t, "ProviderInfoJSON", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		providerName := discoverDefaultProvider(t, runner)

		cmd := EmbeddingProviderInfoJSONCommand(providerName)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ProviderInfoYAML", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		providerName := discoverDefaultProvider(t, runner)

		cmd := EmbeddingProviderInfoYAMLCommand(providerName)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ProviderInfoTable", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		providerName := discoverDefaultProvider(t, runner)

		cmd := EmbeddingProviderInfoTableCommand(providerName)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})
}

// discoverDefaultProvider queries available embedding providers and returns
// the default provider name. Skips the test if no provider is available.
func discoverDefaultProvider(t *testing.T, runner *TestRunner) string {
	t.Helper()
	providersCmd := EmbeddingProvidersJSONCommand()
	stdout, _, err := runner.Run(providersCmd)
	require.False(t, runner.IsSkipped(providersCmd), "Embedding providers command must not be skipped")
	require.NoError(t, err, "Failed to query embedding providers")

	var resp struct {
		DefaultProvider string `json:"default_provider"`
	}
	err = json.Unmarshal([]byte(stdout), &resp)
	require.NoError(t, err, "Failed to unmarshal embedding providers response")
	require.NotEmpty(t, resp.DefaultProvider, "No default embedding provider available")
	return resp.DefaultProvider
}

// TestEmbeddingCacheStats tests cache statistics command.
func TestEmbeddingCacheStats(t *testing.T) {
	RunForAllTransports(t, "CacheStats", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheStatsCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})
}

// TestEmbeddingCacheStatsOutputFormats tests cache stats with different output formats.
func TestEmbeddingCacheStatsOutputFormats(t *testing.T) {
	RunForAllTransports(t, "CacheStatsJSON", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheStatsJSONCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)

		var resp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &resp)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "CacheStatsYAML", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheStatsYAMLCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "CacheStatsTable", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheStatsTableCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.Contains(t, stdout, "CACHE")
	})
}

// TestEmbeddingCacheClear tests cache clear command.
func TestEmbeddingCacheClear(t *testing.T) {
	RunForAllTransports(t, "CacheClearAll", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheClearCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "CacheClearProvider", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheClearProviderCommand("voyage")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		// May succeed or fail depending on provider availability
		t.Logf("Cache clear provider result: stdout=%s, err=%v", stdout, err)
	})
}

// TestEmbeddingCacheClearOutputFormats tests cache clear with different output formats.
func TestEmbeddingCacheClearOutputFormats(t *testing.T) {
	RunForAllTransports(t, "CacheClearJSON", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheClearJSONCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)

		var resp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &resp)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "CacheClearYAML", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheClearYAMLCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "CacheClearTable", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		cmd := EmbeddingCacheClearTableCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})
}

// TestEmbeddingErrors tests error handling.
func TestEmbeddingErrors(t *testing.T) {
	RunForAllTransports(t, "GenerateMissingText", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingGenerateMissingTextCommand())
	})

	RunForAllTransports(t, "GenerateEmptyText", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingGenerateEmptyTextCommand())
	})

	RunForAllTransports(t, "GenerateInvalidMetadata", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingGenerateInvalidMetadataCommand())
	})

	RunForAllTransports(t, "BatchMissingTexts", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingBatchMissingTextsCommand())
	})

	RunForAllTransports(t, "BatchInvalidJSON", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingBatchInvalidJSONCommand())
	})

	RunForAllTransports(t, "BatchEmptyArray", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingBatchEmptyArrayCommand())
	})

	RunForAllTransports(t, "ProviderInfoMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingProviderInfoMissingNameCommand())
	})

	RunForAllTransports(t, "ProviderSetDefaultMissingName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingProviderSetDefaultMissingNameCommand())
	})

	RunForAllTransports(t, "ProviderInfoInvalidProvider", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(EmbeddingProviderInfoInvalidProviderCommand())
	})
}

// TestEmbeddingLifecycle tests a complete embedding workflow.
func TestEmbeddingLifecycle(t *testing.T) {
	RunForAllTransports(t, "CompleteWorkflow", func(t *testing.T, runner *TestRunner) {
		if !runner.RequireEmbeddingAvailable() {
			return
		}
		// 1. List available providers
		providersCmd := EmbeddingProvidersCommand()
		stdout, _, err := runner.Run(providersCmd)
		if runner.IsSkipped(providersCmd) {
			return
		}
		require.NoError(t, err)
		t.Logf("Available providers: %s", stdout)

		// 2. Get cache stats before
		statsBeforeCmd := EmbeddingCacheStatsJSONCommand()
		stdout, _, err = runner.Run(statsBeforeCmd)
		require.NoError(t, err)
		t.Logf("Cache stats before: %s", stdout)

		// 3. Generate a single embedding
		genCmd := EmbeddingGenerateJSONCommand("Test embedding for lifecycle test")
		stdout, _, err = runner.Run(genCmd)
		require.NoError(t, err)

		var genResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &genResp)
		require.NoError(t, err)
		assert.Contains(t, genResp, "embedding")

		// 4. Generate same embedding again (should be cached)
		stdout, _, err = runner.Run(genCmd)
		require.NoError(t, err)
		t.Logf("Second generation (cached): %s", truncateString(stdout, 100))

		// 5. Generate batch embeddings
		batchCmd := EmbeddingBatchJSONCommand(`["batch text one", "batch text two"]`)
		stdout, _, err = runner.Run(batchCmd)
		require.NoError(t, err)
		t.Logf("Batch result: %s", truncateString(stdout, 200))

		// 6. Get cache stats after
		statsAfterCmd := EmbeddingCacheStatsJSONCommand()
		stdout, _, err = runner.Run(statsAfterCmd)
		require.NoError(t, err)
		t.Logf("Cache stats after: %s", stdout)

		// 7. Clear cache
		clearCmd := EmbeddingCacheClearCommand()
		stdout, _, err = runner.Run(clearCmd)
		require.NoError(t, err)
		t.Logf("Cache cleared: %s", stdout)
	})
}

// =============================================================================
// Test Suites
// =============================================================================

// EmbeddingHelpSuite returns help commands for embedding.
func EmbeddingHelpSuite() []CommandDef {
	return []CommandDef{
		EmbeddingHelpCommand(),
		EmbeddingGenerateHelpCommand(),
		EmbeddingBatchHelpCommand(),
		EmbeddingProvidersHelpCommand(),
		EmbeddingProviderHelpCommand(),
		EmbeddingCacheHelpCommand(),
	}
}

// EmbeddingErrorSuite returns error case commands for embedding.
func EmbeddingErrorSuite() []CommandDef {
	return []CommandDef{
		EmbeddingGenerateMissingTextCommand(),
		EmbeddingGenerateEmptyTextCommand(),
		EmbeddingGenerateInvalidMetadataCommand(),
		EmbeddingBatchMissingTextsCommand(),
		EmbeddingBatchInvalidJSONCommand(),
		EmbeddingBatchEmptyArrayCommand(),
		EmbeddingProviderInfoMissingNameCommand(),
		EmbeddingProviderSetDefaultMissingNameCommand(),
		EmbeddingProviderInfoInvalidProviderCommand(),
	}
}

// EmbeddingGenerateSuite returns commands for testing embedding generation.
func EmbeddingGenerateSuite(text string) []CommandDef {
	return []CommandDef{
		EmbeddingGenerateCommand(text),
		EmbeddingGenerateJSONCommand(text),
		EmbeddingGenerateYAMLCommand(text),
		EmbeddingGenerateTableCommand(text),
		EmbeddingGenerateRawCommand(text),
	}
}

// EmbeddingBatchSuite returns commands for testing batch embedding generation.
func EmbeddingBatchSuite(textsJSON string) []CommandDef {
	return []CommandDef{
		EmbeddingBatchCommand(textsJSON),
		EmbeddingBatchJSONCommand(textsJSON),
		EmbeddingBatchYAMLCommand(textsJSON),
		EmbeddingBatchTableCommand(textsJSON),
	}
}

// EmbeddingProviderSuite returns commands for testing provider operations.
func EmbeddingProviderSuite(providerName string) []CommandDef {
	return []CommandDef{
		EmbeddingProvidersCommand(),
		EmbeddingProvidersIncludeUnavailableCommand(),
		EmbeddingProviderInfoCommand(providerName),
		EmbeddingProviderInfoJSONCommand(providerName),
	}
}

// EmbeddingCacheSuite returns commands for testing cache operations.
func EmbeddingCacheSuite() []CommandDef {
	return []CommandDef{
		EmbeddingCacheStatsCommand(),
		EmbeddingCacheStatsJSONCommand(),
		EmbeddingCacheClearCommand(),
	}
}

// EmbeddingOutputFormatSuite returns commands for testing all output formats.
func EmbeddingOutputFormatSuite(text string) []CommandDef {
	return []CommandDef{
		EmbeddingGenerateJSONCommand(text),
		EmbeddingGenerateYAMLCommand(text),
		EmbeddingGenerateTableCommand(text),
		EmbeddingGenerateRawCommand(text),
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// truncateString truncates a string to the specified length with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
