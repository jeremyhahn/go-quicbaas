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

// Package cli provides E2E integration tests for QuicBaaS CLI ollama commands.
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
// Ollama Transport Configuration
// =============================================================================

// ollamaTransportsToSkip defines which transports to skip for Ollama operations.
// The CLI factory only supports gRPC, but the server does not register the
// Ollama gRPC service in the current test environment.
// All transports are skipped until the server-side service registration is resolved.
var ollamaTransportsToSkip = []Transport{TransportGRPC, TransportREST, TransportQUIC, TransportMCP, TransportUnix}

// =============================================================================
// Ollama Health Command Definitions
// =============================================================================

// OllamaHealthCommand returns a command to check Ollama health.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaHealthCommand() CommandDef {
	return CommandDef{
		Name:           "ollama health",
		Args:           []string{"ollama", "health"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaHealthWithFullCheckCommand returns a health command with full check enabled.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaHealthWithFullCheckCommand() CommandDef {
	return CommandDef{
		Name:           "ollama health full-check",
		Args:           []string{"ollama", "health", "--full-check"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaHealthJSONCommand returns a health command with JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaHealthJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ollama health json",
		Args:           []string{"ollama", "health", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaHealthYAMLCommand returns a health command with YAML output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaHealthYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "ollama health yaml",
		Args:           []string{"ollama", "health", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaHealthTableCommand returns a health command with table output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaHealthTableCommand() CommandDef {
	return CommandDef{
		Name:           "ollama health table",
		Args:           []string{"ollama", "health", "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaHealthWithFullCheckAndJSONCommand returns health with full-check and JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaHealthWithFullCheckAndJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ollama health full-check json",
		Args:           []string{"ollama", "health", "--full-check", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// =============================================================================
// Ollama Models Command Definitions
// =============================================================================

// OllamaModelsCommand returns a command to list available Ollama models.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsCommand() CommandDef {
	return CommandDef{
		Name:           "ollama models",
		Args:           []string{"ollama", "models"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelsListAliasCommand returns models command using the 'list' alias.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsListAliasCommand() CommandDef {
	return CommandDef{
		Name:           "ollama list (alias)",
		Args:           []string{"ollama", "list"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelsWithPrefixCommand returns models command with name prefix filter.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsWithPrefixCommand(prefix string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama models prefix=%s", prefix),
		Args:           []string{"ollama", "models", "--prefix", prefix},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelsWithListFlagCommand returns models command with -l flag.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsWithListFlagCommand() CommandDef {
	return CommandDef{
		Name:           "ollama models -l",
		Args:           []string{"ollama", "models", "-l"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelsJSONCommand returns models command with JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ollama models json",
		Args:           []string{"ollama", "models", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelsYAMLCommand returns models command with YAML output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsYAMLCommand() CommandDef {
	return CommandDef{
		Name:           "ollama models yaml",
		Args:           []string{"ollama", "models", "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelsTableCommand returns models command with table output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsTableCommand() CommandDef {
	return CommandDef{
		Name:           "ollama models table",
		Args:           []string{"ollama", "models", "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelsWithPrefixAndJSONCommand returns models with prefix filter and JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelsWithPrefixAndJSONCommand(prefix string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama models prefix=%s json", prefix),
		Args:           []string{"ollama", "models", "--prefix", prefix, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// =============================================================================
// Ollama Model Info Command Definitions
// =============================================================================

// OllamaModelInfoCommand returns a command to get detailed model information.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelInfoCommand(modelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama model info %s", modelName),
		Args:           []string{"ollama", "model", "info", modelName},
		ExpectSuccess:  true,
		ExpectOutput:   []string{modelName},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelInfoJSONCommand returns model info command with JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelInfoJSONCommand(modelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama model info %s json", modelName),
		Args:           []string{"ollama", "model", "info", modelName, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelInfoYAMLCommand returns model info command with YAML output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelInfoYAMLCommand(modelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama model info %s yaml", modelName),
		Args:           []string{"ollama", "model", "info", modelName, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelInfoTableCommand returns model info command with table output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelInfoTableCommand(modelName string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama model info %s table", modelName),
		Args:           []string{"ollama", "model", "info", modelName, "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// =============================================================================
// Ollama Embed Command Definitions
// =============================================================================

// OllamaEmbedCommand returns a command to generate an embedding for text.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedCommand(text string) CommandDef {
	return CommandDef{
		Name:           "ollama embed",
		Args:           []string{"ollama", "embed", text},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedWithModelCommand returns embed command with specific model.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedWithModelCommand(text, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama embed model=%s", model),
		Args:           []string{"ollama", "embed", text, "--model", model},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedJSONCommand returns embed command with JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedJSONCommand(text string) CommandDef {
	return CommandDef{
		Name:           "ollama embed json",
		Args:           []string{"ollama", "embed", text, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedYAMLCommand returns embed command with YAML output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedYAMLCommand(text string) CommandDef {
	return CommandDef{
		Name:           "ollama embed yaml",
		Args:           []string{"ollama", "embed", text, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedTableCommand returns embed command with table output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedTableCommand(text string) CommandDef {
	return CommandDef{
		Name:           "ollama embed table",
		Args:           []string{"ollama", "embed", text, "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedWithModelAndJSONCommand returns embed with model and JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedWithModelAndJSONCommand(text, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama embed model=%s json", model),
		Args:           []string{"ollama", "embed", text, "--model", model, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// =============================================================================
// Ollama Embed Batch Command Definitions
// =============================================================================

// OllamaEmbedBatchCommand returns a command to generate embeddings for multiple texts.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchCommand(texts string) CommandDef {
	return CommandDef{
		Name:           "ollama embed-batch",
		Args:           []string{"ollama", "embed-batch", "--texts", texts},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedBatchWithModelCommand returns embed-batch command with specific model.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchWithModelCommand(texts, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama embed-batch model=%s", model),
		Args:           []string{"ollama", "embed-batch", "--texts", texts, "--model", model},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedBatchJSONCommand returns embed-batch command with JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchJSONCommand(texts string) CommandDef {
	return CommandDef{
		Name:           "ollama embed-batch json",
		Args:           []string{"ollama", "embed-batch", "--texts", texts, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedBatchYAMLCommand returns embed-batch command with YAML output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchYAMLCommand(texts string) CommandDef {
	return CommandDef{
		Name:           "ollama embed-batch yaml",
		Args:           []string{"ollama", "embed-batch", "--texts", texts, "--output", "yaml"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedBatchTableCommand returns embed-batch command with table output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchTableCommand(texts string) CommandDef {
	return CommandDef{
		Name:           "ollama embed-batch table",
		Args:           []string{"ollama", "embed-batch", "--texts", texts, "--output", "table"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedBatchWithModelAndJSONCommand returns embed-batch with model and JSON output.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchWithModelAndJSONCommand(texts, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("ollama embed-batch model=%s json", model),
		Args:           []string{"ollama", "embed-batch", "--texts", texts, "--model", model, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// =============================================================================
// Ollama Help Command Definitions
// =============================================================================

// OllamaHelpCommand returns a command to show ollama help.
// Help commands are CLI-local and work on all transports.
func OllamaHelpCommand() CommandDef {
	return CommandDef{
		Name:          "ollama help",
		Args:          []string{"ollama", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"health", "models", "embed"},
		Timeout:       10 * time.Second,
	}
}

// OllamaHealthHelpCommand shows health subcommand help.
// Help commands are CLI-local and work on all transports.
func OllamaHealthHelpCommand() CommandDef {
	return CommandDef{
		Name:          "ollama health help",
		Args:          []string{"ollama", "health", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"full-check", "output"},
		Timeout:       10 * time.Second,
	}
}

// OllamaModelsHelpCommand shows models subcommand help.
// Help commands are CLI-local and work on all transports.
func OllamaModelsHelpCommand() CommandDef {
	return CommandDef{
		Name:          "ollama models help",
		Args:          []string{"ollama", "models", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"prefix", "output"},
		Timeout:       10 * time.Second,
	}
}

// OllamaModelInfoHelpCommand shows model info subcommand help.
// Help commands are CLI-local and work on all transports.
func OllamaModelInfoHelpCommand() CommandDef {
	return CommandDef{
		Name:          "ollama model info help",
		Args:          []string{"ollama", "model", "info", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"output"},
		Timeout:       10 * time.Second,
	}
}

// OllamaEmbedHelpCommand shows embed subcommand help.
// Help commands are CLI-local and work on all transports.
func OllamaEmbedHelpCommand() CommandDef {
	return CommandDef{
		Name:          "ollama embed help",
		Args:          []string{"ollama", "embed", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"model", "output"},
		Timeout:       10 * time.Second,
	}
}

// OllamaEmbedBatchHelpCommand shows embed-batch subcommand help.
// Help commands are CLI-local and work on all transports.
func OllamaEmbedBatchHelpCommand() CommandDef {
	return CommandDef{
		Name:          "ollama embed-batch help",
		Args:          []string{"ollama", "embed-batch", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"texts", "model", "output"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Ollama Error Command Definitions
// =============================================================================

// OllamaModelInfoMissingModelNameCommand tests missing model name argument.
// Error validation is CLI-local and works on all transports.
func OllamaModelInfoMissingModelNameCommand() CommandDef {
	return CommandDef{
		Name:          "ollama model info missing model name",
		Args:          []string{"ollama", "model", "info"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// OllamaEmbedMissingTextCommand tests missing text argument.
// Error validation is CLI-local and works on all transports.
func OllamaEmbedMissingTextCommand() CommandDef {
	return CommandDef{
		Name:          "ollama embed missing text",
		Args:          []string{"ollama", "embed"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// OllamaEmbedEmptyTextCommand tests empty text argument.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedEmptyTextCommand() CommandDef {
	return CommandDef{
		Name:           "ollama embed empty text",
		Args:           []string{"ollama", "embed", ""},
		ExpectSuccess:  false,
		ExpectError:    []string{"empty"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedBatchMissingTextsCommand tests missing --texts flag.
// Error validation is CLI-local and works on all transports.
func OllamaEmbedBatchMissingTextsCommand() CommandDef {
	return CommandDef{
		Name:          "ollama embed-batch missing texts",
		Args:          []string{"ollama", "embed-batch"},
		ExpectSuccess: false,
		ExpectError:   []string{"texts"},
		Timeout:       10 * time.Second,
	}
}

// OllamaEmbedBatchInvalidJSONCommand tests invalid JSON for --texts.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchInvalidJSONCommand() CommandDef {
	return CommandDef{
		Name:           "ollama embed-batch invalid json",
		Args:           []string{"ollama", "embed-batch", "--texts", "not-valid-json"},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaEmbedBatchEmptyArrayCommand tests empty texts array.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaEmbedBatchEmptyArrayCommand() CommandDef {
	return CommandDef{
		Name:           "ollama embed-batch empty array",
		Args:           []string{"ollama", "embed-batch", "--texts", "[]"},
		ExpectSuccess:  false,
		ExpectError:    []string{"empty"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// OllamaModelInfoInvalidModelCommand tests invalid/non-existent model name.
// Ollama operations are ONLY supported via gRPC transport.
func OllamaModelInfoInvalidModelCommand() CommandDef {
	return CommandDef{
		Name:           "ollama model info invalid model",
		Args:           []string{"ollama", "model", "info", "nonexistent-model-xyz-123"},
		ExpectSuccess:  false,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: ollamaTransportsToSkip,
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestOllamaHelp tests ollama help commands.
func TestOllamaHelp(t *testing.T) {
	RunForAllTransports(t, "OllamaHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaHelpCommand())
	})

	RunForAllTransports(t, "OllamaHealthHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaHealthHelpCommand())
	})

	RunForAllTransports(t, "OllamaModelsHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaModelsHelpCommand())
	})

	RunForAllTransports(t, "OllamaModelInfoHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaModelInfoHelpCommand())
	})

	RunForAllTransports(t, "OllamaEmbedHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaEmbedHelpCommand())
	})

	RunForAllTransports(t, "OllamaEmbedBatchHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaEmbedBatchHelpCommand())
	})
}

// TestOllamaHealth tests ollama health commands.
func TestOllamaHealth(t *testing.T) {
	RunForAllTransports(t, "Health", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaHealthCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		// Health output should contain status information
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "HealthFullCheck", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaHealthWithFullCheckCommand()
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
	})

	RunForAllTransports(t, "HealthJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaHealthJSONCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		// Verify JSON output is valid
		var healthResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &healthResp)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "HealthYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaHealthYAMLCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "HealthTable", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaHealthTableCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "HealthFullCheckJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaHealthWithFullCheckAndJSONCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var healthResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &healthResp)
		require.NoError(t, err, "Output should be valid JSON")
	})
}

// TestOllamaModels tests ollama models list commands.
func TestOllamaModels(t *testing.T) {
	RunForAllTransports(t, "Models", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ModelsListAlias", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsListAliasCommand()
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
	})

	RunForAllTransports(t, "ModelsWithPrefix", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsWithPrefixCommand("nomic")
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
	})

	RunForAllTransports(t, "ModelsWithListFlag", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsWithListFlagCommand()
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
	})

	RunForAllTransports(t, "ModelsJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsJSONCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var modelsResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &modelsResp)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "ModelsYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsYAMLCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ModelsTable", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsTableCommand()
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ModelsWithPrefixAndJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelsWithPrefixAndJSONCommand("nomic")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var modelsResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &modelsResp)
		require.NoError(t, err, "Output should be valid JSON")
	})
}

// TestOllamaModelInfo tests ollama model info commands.
func TestOllamaModelInfo(t *testing.T) {
	// Use a common embedding model that should be available
	testModel := "nomic-embed-text"

	RunForAllTransports(t, "ModelInfo", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelInfoCommand(testModel)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.Contains(t, stdout, testModel)
	})

	RunForAllTransports(t, "ModelInfoJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelInfoJSONCommand(testModel)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var infoResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &infoResp)
		require.NoError(t, err, "Output should be valid JSON")
	})

	RunForAllTransports(t, "ModelInfoYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelInfoYAMLCommand(testModel)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "ModelInfoTable", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaModelInfoTableCommand(testModel)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})
}

// TestOllamaEmbed tests ollama embed commands.
func TestOllamaEmbed(t *testing.T) {
	testText := "Hello, world! This is a test embedding."

	RunForAllTransports(t, "Embed", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		// Embedding output should contain vector information
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "EmbedWithModel", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedWithModelCommand(testText, "nomic-embed-text")
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
	})

	RunForAllTransports(t, "EmbedJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedJSONCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var embedResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &embedResp)
		require.NoError(t, err, "Output should be valid JSON")
		// Verify embedding field exists
		assert.Contains(t, embedResp, "embedding")
	})

	RunForAllTransports(t, "EmbedYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedYAMLCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "EmbedTable", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedTableCommand(testText)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "EmbedWithModelAndJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedWithModelAndJSONCommand(testText, "nomic-embed-text")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var embedResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &embedResp)
		require.NoError(t, err, "Output should be valid JSON")
	})
}

// TestOllamaEmbedBatch tests ollama embed-batch commands.
func TestOllamaEmbedBatch(t *testing.T) {
	testTexts := `["Hello, world!", "This is a test.", "Another text for embedding."]`

	RunForAllTransports(t, "EmbedBatch", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedBatchCommand(testTexts)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "EmbedBatchWithModel", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedBatchWithModelCommand(testTexts, "nomic-embed-text")
		_, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
	})

	RunForAllTransports(t, "EmbedBatchJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedBatchJSONCommand(testTexts)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var batchResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &batchResp)
		require.NoError(t, err, "Output should be valid JSON")
		// Verify embeddings field exists
		assert.Contains(t, batchResp, "embeddings")
	})

	RunForAllTransports(t, "EmbedBatchYAML", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedBatchYAMLCommand(testTexts)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "EmbedBatchTable", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedBatchTableCommand(testTexts)
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		assert.NotEmpty(t, stdout)
	})

	RunForAllTransports(t, "EmbedBatchWithModelAndJSON", func(t *testing.T, runner *TestRunner) {
		cmd := OllamaEmbedBatchWithModelAndJSONCommand(testTexts, "nomic-embed-text")
		stdout, _, err := runner.Run(cmd)
		if runner.IsSkipped(cmd) {
			return
		}
		require.NoError(t, err)
		var batchResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &batchResp)
		require.NoError(t, err, "Output should be valid JSON")
	})
}

// TestOllamaErrors tests error handling for ollama commands.
func TestOllamaErrors(t *testing.T) {
	RunForAllTransports(t, "ModelInfoMissingModelName", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaModelInfoMissingModelNameCommand())
	})

	RunForAllTransports(t, "EmbedMissingText", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaEmbedMissingTextCommand())
	})

	RunForAllTransports(t, "EmbedEmptyText", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaEmbedEmptyTextCommand())
	})

	RunForAllTransports(t, "EmbedBatchMissingTexts", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaEmbedBatchMissingTextsCommand())
	})

	RunForAllTransports(t, "EmbedBatchInvalidJSON", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaEmbedBatchInvalidJSONCommand())
	})

	RunForAllTransports(t, "EmbedBatchEmptyArray", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaEmbedBatchEmptyArrayCommand())
	})

	RunForAllTransports(t, "ModelInfoInvalidModel", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(OllamaModelInfoInvalidModelCommand())
	})
}

// TestOllamaOutputFormats tests all output formats for all commands.
func TestOllamaOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	// Test health output formats
	for _, format := range formats {
		t.Run(fmt.Sprintf("Health_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Health_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("ollama health %s", format),
					Args:           []string{"ollama", "health", "--output", format},
					ExpectSuccess:  true,
					Timeout:        30 * time.Second,
					RequiresUnseal: true,
					SkipTransports: ollamaTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}

	// Test models output formats
	for _, format := range formats {
		t.Run(fmt.Sprintf("Models_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Models_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("ollama models %s", format),
					Args:           []string{"ollama", "models", "--output", format},
					ExpectSuccess:  true,
					Timeout:        30 * time.Second,
					RequiresUnseal: true,
					SkipTransports: ollamaTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}

	// Test model info output formats
	for _, format := range formats {
		t.Run(fmt.Sprintf("ModelInfo_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("ModelInfo_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("ollama model info %s", format),
					Args:           []string{"ollama", "model", "info", "nomic-embed-text", "--output", format},
					ExpectSuccess:  true,
					Timeout:        30 * time.Second,
					RequiresUnseal: true,
					SkipTransports: ollamaTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}

	// Test embed output formats
	for _, format := range formats {
		t.Run(fmt.Sprintf("Embed_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("Embed_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("ollama embed %s", format),
					Args:           []string{"ollama", "embed", "test embedding text", "--output", format},
					ExpectSuccess:  true,
					Timeout:        60 * time.Second,
					RequiresUnseal: true,
					SkipTransports: ollamaTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}

	// Test embed-batch output formats
	for _, format := range formats {
		t.Run(fmt.Sprintf("EmbedBatch_%s", format), func(t *testing.T) {
			RunForAllTransports(t, fmt.Sprintf("EmbedBatch_%s", format), func(t *testing.T, runner *TestRunner) {
				cmd := CommandDef{
					Name:           fmt.Sprintf("ollama embed-batch %s", format),
					Args:           []string{"ollama", "embed-batch", "--texts", `["text1", "text2"]`, "--output", format},
					ExpectSuccess:  true,
					Timeout:        120 * time.Second,
					RequiresUnseal: true,
					SkipTransports: ollamaTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}
}

// TestOllamaWorkflow tests a complete ollama workflow.
func TestOllamaWorkflow(t *testing.T) {
	RunForAllTransports(t, "CompleteWorkflow", func(t *testing.T, runner *TestRunner) {
		// Step 1: Check health
		healthCmd := OllamaHealthCommand()
		_, _, err := runner.Run(healthCmd)
		if runner.IsSkipped(healthCmd) {
			return
		}
		require.NoError(t, err)

		// Step 2: List available models
		modelsCmd := OllamaModelsJSONCommand()
		stdout, _, err := runner.Run(modelsCmd)
		require.NoError(t, err)

		var modelsResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &modelsResp)
		require.NoError(t, err)

		// Step 3: Get info for a model (if models are available)
		modelInfoCmd := OllamaModelInfoCommand("nomic-embed-text")
		_, _, err = runner.Run(modelInfoCmd)
		// This may fail if model is not available, which is acceptable
		t.Logf("Model info result: %v", err)

		// Step 4: Generate a single embedding
		embedCmd := OllamaEmbedJSONCommand("This is a test document for embedding generation.")
		stdout, _, err = runner.Run(embedCmd)
		require.NoError(t, err)

		var embedResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &embedResp)
		require.NoError(t, err)
		assert.Contains(t, embedResp, "embedding")

		// Step 5: Generate batch embeddings
		batchCmd := OllamaEmbedBatchJSONCommand(`["Document one", "Document two", "Document three"]`)
		stdout, _, err = runner.Run(batchCmd)
		require.NoError(t, err)

		var batchResp map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &batchResp)
		require.NoError(t, err)
		assert.Contains(t, batchResp, "embeddings")
	})
}

// =============================================================================
// Test Suites
// =============================================================================

// OllamaHelpSuite returns help commands for ollama.
func OllamaHelpSuite() []CommandDef {
	return []CommandDef{
		OllamaHelpCommand(),
		OllamaHealthHelpCommand(),
		OllamaModelsHelpCommand(),
		OllamaModelInfoHelpCommand(),
		OllamaEmbedHelpCommand(),
		OllamaEmbedBatchHelpCommand(),
	}
}

// OllamaHealthSuite returns health check commands for ollama.
func OllamaHealthSuite() []CommandDef {
	return []CommandDef{
		OllamaHealthCommand(),
		OllamaHealthWithFullCheckCommand(),
		OllamaHealthJSONCommand(),
		OllamaHealthYAMLCommand(),
		OllamaHealthTableCommand(),
		OllamaHealthWithFullCheckAndJSONCommand(),
	}
}

// OllamaModelsSuite returns model listing commands for ollama.
func OllamaModelsSuite() []CommandDef {
	return []CommandDef{
		OllamaModelsCommand(),
		OllamaModelsListAliasCommand(),
		OllamaModelsWithListFlagCommand(),
		OllamaModelsJSONCommand(),
		OllamaModelsYAMLCommand(),
		OllamaModelsTableCommand(),
	}
}

// OllamaModelInfoSuite returns model info commands for ollama.
func OllamaModelInfoSuite(modelName string) []CommandDef {
	return []CommandDef{
		OllamaModelInfoCommand(modelName),
		OllamaModelInfoJSONCommand(modelName),
		OllamaModelInfoYAMLCommand(modelName),
		OllamaModelInfoTableCommand(modelName),
	}
}

// OllamaEmbedSuite returns embedding commands for ollama.
func OllamaEmbedSuite(text string) []CommandDef {
	return []CommandDef{
		OllamaEmbedCommand(text),
		OllamaEmbedJSONCommand(text),
		OllamaEmbedYAMLCommand(text),
		OllamaEmbedTableCommand(text),
	}
}

// OllamaEmbedBatchSuite returns batch embedding commands for ollama.
func OllamaEmbedBatchSuite(texts string) []CommandDef {
	return []CommandDef{
		OllamaEmbedBatchCommand(texts),
		OllamaEmbedBatchJSONCommand(texts),
		OllamaEmbedBatchYAMLCommand(texts),
		OllamaEmbedBatchTableCommand(texts),
	}
}

// OllamaErrorSuite returns error case commands for ollama.
func OllamaErrorSuite() []CommandDef {
	return []CommandDef{
		OllamaModelInfoMissingModelNameCommand(),
		OllamaEmbedMissingTextCommand(),
		OllamaEmbedEmptyTextCommand(),
		OllamaEmbedBatchMissingTextsCommand(),
		OllamaEmbedBatchInvalidJSONCommand(),
		OllamaEmbedBatchEmptyArrayCommand(),
		OllamaModelInfoInvalidModelCommand(),
	}
}
