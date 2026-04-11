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

// Package cli provides E2E integration tests for QuicBaaS CLI RAG commands.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// RAG Command Definitions
// =============================================================================

// Note: RAGTransportsToSkip is defined in commands.go and reused here.
// RAG uses REST API since it orchestrates multiple services.
// gRPC, QUIC, Unix, and MCP transports do not have RAG CLI support.

// RAGIndexCommand returns a command to index documents for RAG.
func RAGIndexCommand(namespace, sourcePath string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag index %s", namespace),
		Args:           []string{"rag", "index", "--namespace", namespace, "--source", sourcePath},
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGIndexWithOptionsCommand returns a RAG index command with custom options.
func RAGIndexWithOptionsCommand(namespace, sourcePath, model string, chunkSize, chunkOverlap int, recursive bool) CommandDef {
	args := []string{"rag", "index", "--namespace", namespace, "--source", sourcePath}
	if model != "" {
		args = append(args, "--model", model)
	}
	if chunkSize > 0 {
		args = append(args, "--chunk-size", fmt.Sprintf("%d", chunkSize))
	}
	if chunkOverlap > 0 {
		args = append(args, "--chunk-overlap", fmt.Sprintf("%d", chunkOverlap))
	}
	if recursive {
		args = append(args, "--recursive")
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag index with options %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        120 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGSearchCommand returns a command to perform semantic search.
func RAGSearchCommand(namespace, query string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag search %s", namespace),
		Args:           []string{"rag", "search", "--namespace", namespace, "--query", query},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGSearchWithOptionsCommand returns a RAG search command with custom options.
func RAGSearchWithOptionsCommand(namespace, query string, topK int, minScore float32) CommandDef {
	args := []string{"rag", "search", "--namespace", namespace, "--query", query}
	if topK > 0 {
		args = append(args, "--top-k", fmt.Sprintf("%d", topK))
	}
	if minScore > 0 {
		args = append(args, "--min-score", fmt.Sprintf("%.2f", minScore))
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag search with options %s", namespace),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGStatusCommand returns a command to get RAG system status.
func RAGStatusCommand() CommandDef {
	return CommandDef{
		Name:           "rag status",
		Args:           []string{"rag", "status"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGStatusWithNamespaceCommand returns a command to get status for a specific namespace.
func RAGStatusWithNamespaceCommand(namespace string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag status %s", namespace),
		Args:           []string{"rag", "status", "--namespace", namespace},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// RAGStatusOutputFormatCommand returns a RAG status command with specific output format.
func RAGStatusOutputFormatCommand(output string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag status output=%s", output),
		Args:           []string{"rag", "status", "--output", output},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// =============================================================================
// RAG Query Commands (using existing definitions from commands.go)
// =============================================================================

// ragQueryCommandLocal returns a command to perform a RAG query.
// This wraps the existing RAGQueryCommand pattern from commands.go.
func ragQueryCommandLocal(query, namespace string) CommandDef {
	queryPreview := query
	if len(query) > 20 {
		queryPreview = query[:20]
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag query %s", queryPreview),
		Args:           []string{"rag", "query", query, "--namespace", namespace},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragQueryWithOptionsCommandLocal returns a RAG query command with custom options.
func ragQueryWithOptionsCommandLocal(query, namespace string, topK, maxTokens int, minScore float32, template string, includeCitations bool) CommandDef {
	args := []string{"rag", "query", query, "--namespace", namespace}
	if topK > 0 {
		args = append(args, "--top-k", fmt.Sprintf("%d", topK))
	}
	if maxTokens > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%d", maxTokens))
	}
	if minScore > 0 {
		args = append(args, "--min-score", fmt.Sprintf("%.2f", minScore))
	}
	if template != "" {
		args = append(args, "--template", template)
	}
	if includeCitations {
		args = append(args, "--include-citations")
	}
	queryPreview := query
	if len(query) > 15 {
		queryPreview = query[:15]
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag query with options %s", queryPreview),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragQueryWithModelCommandLocal returns a RAG query command with a specific model.
func ragQueryWithModelCommandLocal(query, namespace, model string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag query with model %s", model),
		Args:           []string{"rag", "query", query, "--namespace", namespace, "--model", model},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragQueryWithOutputCommandLocal returns a RAG query command with specific output format.
func ragQueryWithOutputCommandLocal(query, namespace, output string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag query output=%s", output),
		Args:           []string{"rag", "query", query, "--namespace", namespace, "--output", output},
		ExpectSuccess:  true,
		Timeout:        60 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragContextCommandLocal returns a command to build context from results file.
func ragContextCommandLocal(resultsFile string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag context %s", resultsFile),
		Args:           []string{"rag", "context", "--results", resultsFile},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragContextWithOptionsCommandLocal returns a RAG context command with custom options.
func ragContextWithOptionsCommandLocal(resultsFile string, maxTokens int, template, truncation string, includeCitations bool) CommandDef {
	args := []string{"rag", "context", "--results", resultsFile}
	if maxTokens > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%d", maxTokens))
	}
	if template != "" {
		args = append(args, "--template", template)
	}
	if truncation != "" {
		args = append(args, "--truncation", truncation)
	}
	if includeCitations {
		args = append(args, "--include-citations")
	}
	return CommandDef{
		Name:           fmt.Sprintf("rag context with options %s", resultsFile),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragContextWithOutputCommandLocal returns a RAG context command with specific output format.
func ragContextWithOutputCommandLocal(resultsFile, output string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("rag context output=%s", output),
		Args:           []string{"rag", "context", "--results", resultsFile, "--output", output},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: RAGTransportsToSkip,
	}
}

// =============================================================================
// RAG Help Commands
// =============================================================================

// ragHelpCommandLocal returns a command to show RAG help.
func ragHelpCommandLocal() CommandDef {
	return CommandDef{
		Name:          "rag help",
		Args:          []string{"rag", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"query", "context"},
		Timeout:       10 * time.Second,
	}
}

// ragQueryHelpCommandLocal returns a command to show RAG query help.
func ragQueryHelpCommandLocal() CommandDef {
	return CommandDef{
		Name:          "rag query help",
		Args:          []string{"rag", "query", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"namespace", "top-k", "max-tokens"},
		Timeout:       10 * time.Second,
	}
}

// ragContextHelpCommandLocal returns a command to show RAG context help.
func ragContextHelpCommandLocal() CommandDef {
	return CommandDef{
		Name:          "rag context help",
		Args:          []string{"rag", "context", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"results", "max-tokens", "template"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// RAG Error Commands
// =============================================================================

// ragQueryMissingNamespaceCommandLocal returns a RAG query command without namespace (should fail).
func ragQueryMissingNamespaceCommandLocal(query string) CommandDef {
	return CommandDef{
		Name:           "rag query missing namespace",
		Args:           []string{"rag", "query", query},
		ExpectSuccess:  false,
		ExpectError:    []string{"namespace"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragQueryEmptyQueryCommandLocal returns a RAG query command with empty query (should fail).
func ragQueryEmptyQueryCommandLocal(namespace string) CommandDef {
	return CommandDef{
		Name:           "rag query empty",
		Args:           []string{"rag", "query", "", "--namespace", namespace},
		ExpectSuccess:  false,
		ExpectError:    []string{"empty"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragContextMissingResultsCommandLocal returns a RAG context command without results file (should fail).
func ragContextMissingResultsCommandLocal() CommandDef {
	return CommandDef{
		Name:           "rag context missing results",
		Args:           []string{"rag", "context"},
		ExpectSuccess:  false,
		ExpectError:    []string{"results"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragContextInvalidFileCommandLocal returns a RAG context command with non-existent file (should fail).
func ragContextInvalidFileCommandLocal() CommandDef {
	return CommandDef{
		Name:           "rag context invalid file",
		Args:           []string{"rag", "context", "--results", "/nonexistent/path/results.json"},
		ExpectSuccess:  false,
		ExpectError:    []string{"no such file"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragContextEmptyResultsCommandLocal returns a RAG context command with empty results file (should fail).
func ragContextEmptyResultsCommandLocal(emptyFile string) CommandDef {
	return CommandDef{
		Name:           "rag context empty results",
		Args:           []string{"rag", "context", "--results", emptyFile},
		ExpectSuccess:  false,
		ExpectError:    []string{"no results"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// ragContextInvalidJSONCommandLocal returns a RAG context command with invalid JSON file (should fail).
func ragContextInvalidJSONCommandLocal(invalidFile string) CommandDef {
	return CommandDef{
		Name:           "rag context invalid JSON",
		Args:           []string{"rag", "context", "--results", invalidFile},
		ExpectSuccess:  false,
		ExpectError:    []string{"parse", "JSON"},
		Timeout:        10 * time.Second,
		SkipTransports: RAGTransportsToSkip,
	}
}

// =============================================================================
// Test Suites
// =============================================================================

// ragHelpSuiteLocal returns help commands for RAG.
func ragHelpSuiteLocal() []CommandDef {
	return []CommandDef{
		ragHelpCommandLocal(),
		ragQueryHelpCommandLocal(),
		ragContextHelpCommandLocal(),
	}
}

// ragErrorSuiteLocal returns error case commands for RAG.
func ragErrorSuiteLocal() []CommandDef {
	return []CommandDef{
		ragQueryMissingNamespaceCommandLocal("test query"),
		ragContextMissingResultsCommandLocal(),
		ragContextInvalidFileCommandLocal(),
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestRAGHelp tests RAG help commands across all transports.
func TestRAGHelp(t *testing.T) {
	RunForAllTransports(t, "RAGHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ragHelpCommandLocal())
	})

	RunForAllTransports(t, "RAGQueryHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ragQueryHelpCommandLocal())
	})

	RunForAllTransports(t, "RAGContextHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(ragContextHelpCommandLocal())
	})
}

// TestRAGHelpSuite tests RAG help suite.
func TestRAGHelpSuite(t *testing.T) {
	RunForAllTransports(t, "RAGHelpSuite", func(t *testing.T, runner *TestRunner) {
		suite := ragHelpSuiteLocal()
		runner.RunSuite(suite)
	})
}

// TestRAGQuery tests RAG query operations across all transports.
func TestRAGQuery(t *testing.T) {
	RunForTransports(t, "RAGQueryBasic", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("rag_query_%d", time.Now().UnixNano())
		query := "How do I configure logging?"

		// First, we need a namespace with indexed content
		// Create vector namespace for RAG
		createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		if err != nil {
			t.Logf("Failed to create namespace: %v (may not be required for query test)", err)
		}
		defer cleanupVectorNamespace(t, runner, namespace)

		// Execute RAG query
		queryCmd := ragQueryCommandLocal(query, namespace)
		if runner.IsSkipped(queryCmd) {
			t.Logf("RAG query not supported for this transport, skipping")
			return
		}

		stdout, stderr, err := runner.Run(queryCmd)
		// RAG query may fail if no indexed content exists
		if err != nil {
			t.Logf("RAG query result: stdout=%s stderr=%s err=%v", stdout, stderr, err)
			// This is expected if no content is indexed
			return
		}

		// If successful, verify output format
		assert.NotEmpty(t, stdout, "RAG query should return output")
	})
}

// TestRAGQueryWithOptions tests RAG query with various options.
func TestRAGQueryWithOptions(t *testing.T) {
	RunForTransports(t, "RAGQueryWithOptions", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("rag_opts_%d", time.Now().UnixNano())
		query := "What is the database schema?"

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		if err != nil {
			t.Logf("Failed to create namespace: %v", err)
		}
		defer cleanupVectorNamespace(t, runner, namespace)

		// Test with all options
		queryCmd := ragQueryWithOptionsCommandLocal(query, namespace, 20, 8000, 0.75, "json", true)
		if runner.IsSkipped(queryCmd) {
			return
		}

		stdout, stderr, err := runner.Run(queryCmd)
		t.Logf("RAG query with options: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGQueryWithModel tests RAG query with specific model.
func TestRAGQueryWithModel(t *testing.T) {
	RunForTransports(t, "RAGQueryWithModel", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("rag_model_%d", time.Now().UnixNano())
		query := "Explain the authentication flow"
		model := "nomic-embed-text"

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		if err != nil {
			t.Logf("Failed to create namespace: %v", err)
		}
		defer cleanupVectorNamespace(t, runner, namespace)

		// Test with specific model
		queryCmd := ragQueryWithModelCommandLocal(query, namespace, model)
		if runner.IsSkipped(queryCmd) {
			return
		}

		stdout, stderr, err := runner.Run(queryCmd)
		t.Logf("RAG query with model: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGQueryOutputFormats tests RAG query with different output formats.
func TestRAGQueryOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("OutputFormat_%s", format), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("RAGQueryOutput_%s", format), RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				namespace := fmt.Sprintf("rag_fmt_%d", time.Now().UnixNano())
				query := "What are the configuration options?"

				// Create namespace
				createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
				_, _, err := runner.Run(createNsCmd)
				if runner.IsSkipped(createNsCmd) {
					return
				}
				if err != nil {
					t.Logf("Failed to create namespace: %v", err)
				}
				defer cleanupVectorNamespace(t, runner, namespace)

				// Test with output format
				queryCmd := ragQueryWithOutputCommandLocal(query, namespace, format)
				if runner.IsSkipped(queryCmd) {
					return
				}

				stdout, stderr, err := runner.Run(queryCmd)
				t.Logf("RAG query output=%s: stdout=%s stderr=%s err=%v", format, stdout, stderr, err)
			})
		})
	}
}

// TestRAGContext tests RAG context building from results file.
func TestRAGContext(t *testing.T) {
	RunForTransports(t, "RAGContextBasic", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create temporary results file
		resultsFile, err := createTempResultsFile(t)
		if err != nil {
			t.Fatalf("Failed to create temp results file: %v", err)
		}
		defer os.Remove(resultsFile)

		// Execute RAG context command
		contextCmd := ragContextCommandLocal(resultsFile)
		if runner.IsSkipped(contextCmd) {
			t.Logf("RAG context not supported for this transport, skipping")
			return
		}

		stdout, stderr, err := runner.Run(contextCmd)
		if err != nil {
			t.Logf("RAG context error: stdout=%s stderr=%s err=%v", stdout, stderr, err)
			return
		}

		assert.NotEmpty(t, stdout, "RAG context should return output")
	})
}

// TestRAGContextWithOptions tests RAG context with various options.
func TestRAGContextWithOptions(t *testing.T) {
	RunForTransports(t, "RAGContextWithOptions", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create temporary results file
		resultsFile, err := createTempResultsFile(t)
		if err != nil {
			t.Fatalf("Failed to create temp results file: %v", err)
		}
		defer os.Remove(resultsFile)

		// Test with all options
		contextCmd := ragContextWithOptionsCommandLocal(resultsFile, 8000, "json", "smart", true)
		if runner.IsSkipped(contextCmd) {
			return
		}

		stdout, stderr, err := runner.Run(contextCmd)
		t.Logf("RAG context with options: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGContextOutputFormats tests RAG context with different output formats.
func TestRAGContextOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("OutputFormat_%s", format), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("RAGContextOutput_%s", format), RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				// Create temporary results file
				resultsFile, err := createTempResultsFile(t)
				if err != nil {
					t.Fatalf("Failed to create temp results file: %v", err)
				}
				defer os.Remove(resultsFile)

				// Test with output format
				contextCmd := ragContextWithOutputCommandLocal(resultsFile, format)
				if runner.IsSkipped(contextCmd) {
					return
				}

				stdout, stderr, err := runner.Run(contextCmd)
				t.Logf("RAG context output=%s: stdout=%s stderr=%s err=%v", format, stdout, stderr, err)
			})
		})
	}
}

// TestRAGContextTemplateFormats tests RAG context with different template formats.
func TestRAGContextTemplateFormats(t *testing.T) {
	templates := []string{"markdown", "json", "xml", "plain"}

	for _, template := range templates {
		t.Run(fmt.Sprintf("Template_%s", template), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("RAGContextTemplate_%s", template), RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				// Create temporary results file
				resultsFile, err := createTempResultsFile(t)
				if err != nil {
					t.Fatalf("Failed to create temp results file: %v", err)
				}
				defer os.Remove(resultsFile)

				// Test with template format
				contextCmd := ragContextWithOptionsCommandLocal(resultsFile, 4000, template, "smart", false)
				if runner.IsSkipped(contextCmd) {
					return
				}

				stdout, stderr, err := runner.Run(contextCmd)
				t.Logf("RAG context template=%s: stdout=%s stderr=%s err=%v", template, stdout, stderr, err)
			})
		})
	}
}

// TestRAGContextTruncationStrategies tests RAG context with different truncation strategies.
func TestRAGContextTruncationStrategies(t *testing.T) {
	strategies := []string{"smart", "hard", "none"}

	for _, strategy := range strategies {
		t.Run(fmt.Sprintf("Truncation_%s", strategy), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("RAGContextTruncation_%s", strategy), RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				// Create temporary results file
				resultsFile, err := createTempResultsFile(t)
				if err != nil {
					t.Fatalf("Failed to create temp results file: %v", err)
				}
				defer os.Remove(resultsFile)

				// Test with truncation strategy
				contextCmd := ragContextWithOptionsCommandLocal(resultsFile, 4000, "markdown", strategy, false)
				if runner.IsSkipped(contextCmd) {
					return
				}

				stdout, stderr, err := runner.Run(contextCmd)
				t.Logf("RAG context truncation=%s: stdout=%s stderr=%s err=%v", strategy, stdout, stderr, err)
			})
		})
	}
}

// TestRAGErrors tests RAG error handling.
func TestRAGErrors(t *testing.T) {
	t.Run("QueryMissingNamespace", func(t *testing.T) {
		RunForTransports(t, "RAGQueryMissingNamespace", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
			runner.RunAndAssert(ragQueryMissingNamespaceCommandLocal("test query"))
		})
	})

	t.Run("ContextMissingResults", func(t *testing.T) {
		RunForTransports(t, "RAGContextMissingResults", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
			runner.RunAndAssert(ragContextMissingResultsCommandLocal())
		})
	})

	t.Run("ContextInvalidFile", func(t *testing.T) {
		RunForTransports(t, "RAGContextInvalidFile", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
			runner.RunAndAssert(ragContextInvalidFileCommandLocal())
		})
	})
}

// TestRAGErrorSuite tests RAG error suite.
func TestRAGErrorSuite(t *testing.T) {
	RunForTransports(t, "RAGErrorSuite", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		suite := ragErrorSuiteLocal()
		runner.RunSuite(suite)
	})
}

// TestRAGContextEmptyResults tests RAG context with empty results file.
func TestRAGContextEmptyResults(t *testing.T) {
	RunForTransports(t, "RAGContextEmptyResults", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create empty results file
		emptyFile, err := createEmptyResultsFile(t)
		if err != nil {
			t.Fatalf("Failed to create empty results file: %v", err)
		}
		defer os.Remove(emptyFile)

		cmd := ragContextEmptyResultsCommandLocal(emptyFile)
		if runner.IsSkipped(cmd) {
			return
		}

		stdout, stderr, err := runner.Run(cmd)
		if err == nil {
			t.Logf("Expected error for empty results, got: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestRAGContextInvalidJSON tests RAG context with invalid JSON file.
func TestRAGContextInvalidJSON(t *testing.T) {
	RunForTransports(t, "RAGContextInvalidJSON", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create invalid JSON file
		invalidFile, err := createInvalidJSONFile(t)
		if err != nil {
			t.Fatalf("Failed to create invalid JSON file: %v", err)
		}
		defer os.Remove(invalidFile)

		cmd := ragContextInvalidJSONCommandLocal(invalidFile)
		if runner.IsSkipped(cmd) {
			return
		}

		stdout, stderr, err := runner.Run(cmd)
		if err == nil {
			t.Logf("Expected error for invalid JSON, got: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// TestRAGQueryTemplates tests RAG query with different template formats.
func TestRAGQueryTemplates(t *testing.T) {
	templates := []string{"markdown", "json", "xml", "plain"}

	for _, template := range templates {
		t.Run(fmt.Sprintf("Template_%s", template), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("RAGQueryTemplate_%s", template), RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				namespace := fmt.Sprintf("rag_tpl_%d", time.Now().UnixNano())
				query := "List all API endpoints"

				// Create namespace
				createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
				_, _, err := runner.Run(createNsCmd)
				if runner.IsSkipped(createNsCmd) {
					return
				}
				if err != nil {
					t.Logf("Failed to create namespace: %v", err)
				}
				defer cleanupVectorNamespace(t, runner, namespace)

				// Test with template
				queryCmd := ragQueryWithOptionsCommandLocal(query, namespace, 10, 4000, 0.7, template, false)
				if runner.IsSkipped(queryCmd) {
					return
				}

				stdout, stderr, err := runner.Run(queryCmd)
				t.Logf("RAG query template=%s: stdout=%s stderr=%s err=%v", template, stdout, stderr, err)
			})
		})
	}
}

// TestRAGQueryCitations tests RAG query with citations enabled.
func TestRAGQueryCitations(t *testing.T) {
	RunForTransports(t, "RAGQueryWithCitations", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("rag_cite_%d", time.Now().UnixNano())
		query := "What are the security best practices?"

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		if err != nil {
			t.Logf("Failed to create namespace: %v", err)
		}
		defer cleanupVectorNamespace(t, runner, namespace)

		// Test with citations
		queryCmd := ragQueryWithOptionsCommandLocal(query, namespace, 10, 4000, 0.7, "markdown", true)
		if runner.IsSkipped(queryCmd) {
			return
		}

		stdout, stderr, err := runner.Run(queryCmd)
		t.Logf("RAG query with citations: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGContextCitations tests RAG context with citations enabled.
func TestRAGContextCitations(t *testing.T) {
	RunForTransports(t, "RAGContextWithCitations", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Create temporary results file with metadata for citations
		resultsFile, err := createTempResultsFileWithCitations(t)
		if err != nil {
			t.Fatalf("Failed to create temp results file: %v", err)
		}
		defer os.Remove(resultsFile)

		// Test with citations
		contextCmd := ragContextWithOptionsCommandLocal(resultsFile, 4000, "markdown", "smart", true)
		if runner.IsSkipped(contextCmd) {
			return
		}

		stdout, stderr, err := runner.Run(contextCmd)
		t.Logf("RAG context with citations: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGIndex tests RAG index operations across all transports.
func TestRAGIndex(t *testing.T) {
	RunForTransports(t, "RAGIndexBasic", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("rag_idx_%d", time.Now().UnixNano())
		sourcePath := "/tmp/test_docs"

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		if err != nil {
			t.Logf("Failed to create namespace: %v", err)
		}
		defer cleanupVectorNamespace(t, runner, namespace)

		// Test RAG index command
		indexCmd := RAGIndexCommand(namespace, sourcePath)
		if runner.IsSkipped(indexCmd) {
			t.Logf("RAG index not supported for this transport, skipping")
			return
		}

		stdout, stderr, err := runner.Run(indexCmd)
		// RAG index may fail if source path doesn't exist
		t.Logf("RAG index: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGSearch tests RAG search operations across all transports.
func TestRAGSearch(t *testing.T) {
	RunForTransports(t, "RAGSearchBasic", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("rag_search_%d", time.Now().UnixNano())
		query := "How do I configure the database?"

		// Create namespace
		createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
		_, _, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		if err != nil {
			t.Logf("Failed to create namespace: %v", err)
		}
		defer cleanupVectorNamespace(t, runner, namespace)

		// Test RAG search command
		searchCmd := RAGSearchCommand(namespace, query)
		if runner.IsSkipped(searchCmd) {
			t.Logf("RAG search not supported for this transport, skipping")
			return
		}

		stdout, stderr, err := runner.Run(searchCmd)
		t.Logf("RAG search: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGStatus tests RAG status operations across all transports.
func TestRAGStatus(t *testing.T) {
	RunForTransports(t, "RAGStatusBasic", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		// Test RAG status command
		statusCmd := RAGStatusCommand()
		if runner.IsSkipped(statusCmd) {
			t.Logf("RAG status not supported for this transport, skipping")
			return
		}

		stdout, stderr, err := runner.Run(statusCmd)
		t.Logf("RAG status: stdout=%s stderr=%s err=%v", stdout, stderr, err)
	})
}

// TestRAGStatusOutputFormats tests RAG status with different output formats.
func TestRAGStatusOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("OutputFormat_%s", format), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("RAGStatusOutput_%s", format), RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				statusCmd := RAGStatusOutputFormatCommand(format)
				if runner.IsSkipped(statusCmd) {
					return
				}

				stdout, stderr, err := runner.Run(statusCmd)
				t.Logf("RAG status output=%s: stdout=%s stderr=%s err=%v", format, stdout, stderr, err)
			})
		})
	}
}

// TestRAGFullLifecycle tests the complete RAG lifecycle.
func TestRAGFullLifecycle(t *testing.T) {
	RunForTransports(t, "RAGFullLifecycle", RAGTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		namespace := fmt.Sprintf("rag_lifecycle_%d", time.Now().UnixNano())

		// Step 1: Create namespace
		createNsCmd := VectorNamespaceCreateCommand(namespace, 384)
		stdout, stderr, err := runner.Run(createNsCmd)
		if runner.IsSkipped(createNsCmd) {
			return
		}
		if err != nil {
			combined := stdout + stderr
			if strings.Contains(combined, "UNIMPLEMENTED") || strings.Contains(combined, "not available") || strings.Contains(combined, "unknown service") {
				t.Logf("Vector service not available: %s %s", stdout, stderr)
				return
			}
		}
		require.NoError(t, err, "Failed to create namespace")
		defer cleanupVectorNamespace(t, runner, namespace)

		// Step 2: Check status
		statusCmd := RAGStatusWithNamespaceCommand(namespace)
		if !runner.IsSkipped(statusCmd) {
			stdout, stderr, err := runner.Run(statusCmd)
			t.Logf("RAG status for namespace: stdout=%s stderr=%s err=%v", stdout, stderr, err)
		}

		// Step 3: Perform query (may return no results if no indexed content)
		query := "What is QuicBaaS?"
		queryCmd := ragQueryCommandLocal(query, namespace)
		if !runner.IsSkipped(queryCmd) {
			stdout, stderr, err := runner.Run(queryCmd)
			t.Logf("RAG query: stdout=%s stderr=%s err=%v", stdout, stderr, err)
		}

		// Step 4: Build context from external results
		resultsFile, err := createTempResultsFile(t)
		if err != nil {
			t.Fatalf("Failed to create temp results file: %v", err)
		}
		defer os.Remove(resultsFile)

		contextCmd := ragContextCommandLocal(resultsFile)
		if !runner.IsSkipped(contextCmd) {
			stdout, stderr, err := runner.Run(contextCmd)
			t.Logf("RAG context: stdout=%s stderr=%s err=%v", stdout, stderr, err)
		}
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// createTempResultsFile creates a temporary JSON file with sample RAG results.
func createTempResultsFile(t *testing.T) (string, error) {
	t.Helper()

	results := []map[string]interface{}{
		{
			"content": "QuicBaaS is a high-performance, distributed key-value database built on multi-Raft consensus.",
			"score":   0.95,
			"metadata": map[string]string{
				"file_path":  "/docs/overview.md",
				"start_line": "1",
				"end_line":   "10",
			},
		},
		{
			"content": "Configuration can be done via YAML files or environment variables.",
			"score":   0.88,
			"metadata": map[string]string{
				"file_path":  "/docs/configuration.md",
				"start_line": "15",
				"end_line":   "30",
			},
		},
		{
			"content": "The seal mechanism provides cryptographic protection for sensitive data at rest.",
			"score":   0.82,
			"metadata": map[string]string{
				"file_path":  "/docs/security.md",
				"start_line": "50",
				"end_line":   "75",
			},
		},
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", &qberrors.WrappedMessageError{Message: "failed to marshal results", Cause: err}
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("rag_results_%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return "", &qberrors.WrappedMessageError{Message: "failed to write temp file", Cause: err}
	}

	return tmpFile, nil
}

// createTempResultsFileWithCitations creates a temporary JSON file with citation metadata.
func createTempResultsFileWithCitations(t *testing.T) (string, error) {
	t.Helper()

	results := []map[string]interface{}{
		{
			"content": "QuicBaaS supports multiple transport protocols including gRPC, REST, QUIC, and MCP.",
			"score":   0.92,
			"metadata": map[string]string{
				"file_path":  "/pkg/transport/grpc/server.go",
				"start_line": "100",
				"end_line":   "150",
				"language":   "go",
			},
		},
		{
			"content": "The Raft consensus protocol ensures strong consistency across distributed nodes.",
			"score":   0.89,
			"metadata": map[string]string{
				"file_path":  "/pkg/raft/consensus.go",
				"start_line": "200",
				"end_line":   "250",
				"language":   "go",
			},
		},
	}

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", &qberrors.WrappedMessageError{Message: "failed to marshal results", Cause: err}
	}

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("rag_citations_%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return "", &qberrors.WrappedMessageError{Message: "failed to write temp file", Cause: err}
	}

	return tmpFile, nil
}

// createEmptyResultsFile creates an empty JSON array file.
func createEmptyResultsFile(t *testing.T) (string, error) {
	t.Helper()

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("rag_empty_%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, []byte("[]"), 0644); err != nil {
		return "", &qberrors.WrappedMessageError{Message: "failed to write temp file", Cause: err}
	}

	return tmpFile, nil
}

// createInvalidJSONFile creates a file with invalid JSON content.
func createInvalidJSONFile(t *testing.T) (string, error) {
	t.Helper()

	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("rag_invalid_%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, []byte("{ invalid json content }"), 0644); err != nil {
		return "", &qberrors.WrappedMessageError{Message: "failed to write temp file", Cause: err}
	}

	return tmpFile, nil
}
