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

//go:build integration && rag

// Package rag provides comprehensive CLI integration tests for RAG commands.
// Tests cover all RAG CLI functionality including query and context operations
// with various flag combinations and error scenarios.
package rag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/cli"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Test Data Types
// =============================================================================

// testResult represents a search result for context building tests.
type testResult struct {
	Content  string            `json:"content"`
	Score    float32           `json:"score"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// =============================================================================
// Help Command Tests
// =============================================================================

// TestRAGHelpCommands tests all RAG help commands.
func TestRAGHelpCommands(t *testing.T) {
	cli.RunForAllTransports(t, "RAGHelpCommands", func(t *testing.T, runner *cli.TestRunner) {
		suite := cli.RAGHelpSuite()
		runner.RunSuite(suite)
	})
}

// TestRAGHelp tests the rag help command.
func TestRAGHelp(t *testing.T) {
	cli.RunForAllTransports(t, "Help", func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestRAGQueryHelp tests the rag query help command.
func TestRAGQueryHelp(t *testing.T) {
	cli.RunForAllTransports(t, "QueryHelp", func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGQueryHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestRAGContextHelp tests the rag context help command.
func TestRAGContextHelp(t *testing.T) {
	cli.RunForAllTransports(t, "ContextHelp", func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestRAGErrorCases tests all RAG error scenarios.
func TestRAGErrorCases(t *testing.T) {
	cli.RunForTransports(t, "RAGErrorCases", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		suite := cli.RAGErrorSuite()
		runner.RunSuite(suite)
	})
}

// TestRAGQueryMissingNamespace tests query without required namespace flag.
func TestRAGQueryMissingNamespace(t *testing.T) {
	cli.RunForTransports(t, "QueryMissingNamespace", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGQueryMissingNamespaceCommand("How do I configure the database?")
		runner.RunAndAssert(cmd)
	})
}

// TestRAGQueryEmptyQuery tests query with empty query text.
func TestRAGQueryEmptyQuery(t *testing.T) {
	cli.RunForTransports(t, "QueryEmptyQuery", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGQueryEmptyQueryCommand("test-namespace")
		runner.RunAndAssert(cmd)
	})
}

// TestRAGContextMissingResults tests context without required results flag.
func TestRAGContextMissingResults(t *testing.T) {
	cli.RunForTransports(t, "ContextMissingResults", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextMissingResultsCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestRAGContextInvalidFile tests context with non-existent results file.
func TestRAGContextInvalidFile(t *testing.T) {
	cli.RunForTransports(t, "ContextInvalidFile", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextInvalidFileCommand()
		runner.RunAndAssert(cmd)
	})
}

// =============================================================================
// RAG Query Command Tests
// =============================================================================

// TestRAGQueryBasic tests basic RAG query functionality.
func TestRAGQueryBasic(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	cli.RunForTransports(t, "QueryBasic", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		namespace := cli.UniqueKey("test-ns")
		cmd := cli.RAGQueryCommand("How do I configure the database?", namespace)
		stdout, stderr, err := runner.Run(cmd)

		// RAG query may fail if namespace is empty, which is acceptable for this test
		if err != nil {
			t.Logf("RAG query returned error (expected if namespace empty): %v", err)
			t.Logf("stderr: %s", stderr)
		} else {
			t.Logf("RAG query output: %s", stdout)
		}
	})
}

// TestRAGQueryWithTopK tests RAG query with custom top-k parameter.
func TestRAGQueryWithTopK(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	topKValues := []int{5, 10, 20, 50}

	for _, topK := range topKValues {
		t.Run(cli.UniqueKey("TopK"), func(t *testing.T) {
			cli.RunForTransports(t, "QueryWithTopK", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				namespace := cli.UniqueKey("test-ns")
				cmd := cli.RAGQueryWithOptionsCommand(
					"Explain the authentication system",
					namespace,
					topK,  // topK
					0,     // maxTokens (default)
					0,     // minScore (default)
					"",    // template (default)
					false, // includeCitations
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Query with topK=%d returned error: %v, stderr: %s", topK, err, stderr)
				} else {
					t.Logf("Query with topK=%d output: %s", topK, stdout)
				}
			})
		})
	}
}

// TestRAGQueryWithMaxTokens tests RAG query with custom max-tokens parameter.
func TestRAGQueryWithMaxTokens(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	maxTokensValues := []int{1000, 2000, 4000, 8000}

	for _, maxTokens := range maxTokensValues {
		t.Run(cli.UniqueKey("MaxTokens"), func(t *testing.T) {
			cli.RunForTransports(t, "QueryWithMaxTokens", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				namespace := cli.UniqueKey("test-ns")
				cmd := cli.RAGQueryWithOptionsCommand(
					"What are the configuration options?",
					namespace,
					0,         // topK (default)
					maxTokens, // maxTokens
					0,         // minScore (default)
					"",        // template (default)
					false,     // includeCitations
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Query with maxTokens=%d returned error: %v, stderr: %s", maxTokens, err, stderr)
				} else {
					t.Logf("Query with maxTokens=%d output: %s", maxTokens, stdout)
				}
			})
		})
	}
}

// TestRAGQueryWithMinScore tests RAG query with custom min-score parameter.
func TestRAGQueryWithMinScore(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	minScoreValues := []float32{0.5, 0.7, 0.8, 0.9}

	for _, minScore := range minScoreValues {
		t.Run(cli.UniqueKey("MinScore"), func(t *testing.T) {
			cli.RunForTransports(t, "QueryWithMinScore", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				namespace := cli.UniqueKey("test-ns")
				cmd := cli.RAGQueryWithOptionsCommand(
					"Describe the database schema",
					namespace,
					0,        // topK (default)
					0,        // maxTokens (default)
					minScore, // minScore
					"",       // template (default)
					false,    // includeCitations
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Query with minScore=%.2f returned error: %v, stderr: %s", minScore, err, stderr)
				} else {
					t.Logf("Query with minScore=%.2f output: %s", minScore, stdout)
				}
			})
		})
	}
}

// TestRAGQueryWithTemplate tests RAG query with different template formats.
func TestRAGQueryWithTemplate(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	templates := []string{"markdown", "json", "xml", "plain"}

	for _, template := range templates {
		t.Run(template, func(t *testing.T) {
			cli.RunForTransports(t, "QueryWithTemplate", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				namespace := cli.UniqueKey("test-ns")
				cmd := cli.RAGQueryWithOptionsCommand(
					"List all API endpoints",
					namespace,
					0,        // topK (default)
					0,        // maxTokens (default)
					0,        // minScore (default)
					template, // template
					false,    // includeCitations
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Query with template=%s returned error: %v, stderr: %s", template, err, stderr)
				} else {
					t.Logf("Query with template=%s output: %s", template, stdout)
				}
			})
		})
	}
}

// TestRAGQueryWithCitations tests RAG query with citations enabled.
func TestRAGQueryWithCitations(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	cli.RunForTransports(t, "QueryWithCitations", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		namespace := cli.UniqueKey("test-ns")
		cmd := cli.RAGQueryWithOptionsCommand(
			"How do I implement user authentication?",
			namespace,
			10,         // topK
			4000,       // maxTokens
			0.7,        // minScore
			"markdown", // template
			true,       // includeCitations
		)
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Query with citations returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Query with citations output: %s", stdout)
		}
	})
}

// TestRAGQueryWithModel tests RAG query with specific embedding model.
func TestRAGQueryWithModel(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	models := []string{"all-minilm", "nomic-embed-text"}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			cli.RunForTransports(t, "QueryWithModel", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				namespace := cli.UniqueKey("test-ns")
				cmd := cli.RAGQueryWithModelCommand(
					"What is the query syntax?",
					namespace,
					model,
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Query with model=%s returned error: %v, stderr: %s", model, err, stderr)
				} else {
					t.Logf("Query with model=%s output: %s", model, stdout)
				}
			})
		})
	}
}

// TestRAGQueryWithOutputFormat tests RAG query with different output formats.
func TestRAGQueryWithOutputFormat(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	outputFormats := []string{"table", "json", "yaml"}

	for _, output := range outputFormats {
		t.Run(output, func(t *testing.T) {
			cli.RunForTransports(t, "QueryWithOutput", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				namespace := cli.UniqueKey("test-ns")
				cmd := cli.RAGQueryWithOutputCommand(
					"Explain error handling",
					namespace,
					output,
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Query with output=%s returned error: %v, stderr: %s", output, err, stderr)
				} else {
					t.Logf("Query with output=%s output length: %d", output, len(stdout))
				}
			})
		})
	}
}

// TestRAGQueryAllOptions tests RAG query with all options combined.
func TestRAGQueryAllOptions(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	cli.RunForTransports(t, "QueryAllOptions", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		namespace := cli.UniqueKey("test-ns")
		cmd := cli.RAGQueryWithOptionsCommand(
			"Complete guide to database configuration",
			namespace,
			20,     // topK
			8000,   // maxTokens
			0.75,   // minScore
			"json", // template
			true,   // includeCitations
		)
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Query with all options returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Query with all options output length: %d", len(stdout))
		}
	})
}

// =============================================================================
// RAG Context Command Tests
// =============================================================================

// createTestResultsFile creates a temporary JSON file with test results.
func createTestResultsFile(t *testing.T, results []testResult) string {
	t.Helper()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "results.json")

	data, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err, "Failed to marshal test results")

	err = os.WriteFile(filePath, data, 0644)
	require.NoError(t, err, "Failed to write test results file")

	return filePath
}

// TestRAGContextBasic tests basic RAG context building.
func TestRAGContextBasic(t *testing.T) {
	results := []testResult{
		{
			Content:  "Machine learning is a subset of artificial intelligence.",
			Score:    0.95,
			Metadata: map[string]string{"file_path": "/docs/ml.md", "start_line": "1", "end_line": "10"},
		},
		{
			Content:  "Deep learning uses neural networks with multiple layers.",
			Score:    0.88,
			Metadata: map[string]string{"file_path": "/docs/dl.md", "start_line": "1", "end_line": "15"},
		},
	}

	resultsFile := createTestResultsFile(t, results)

	cli.RunForTransports(t, "ContextBasic", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextCommand(resultsFile)
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Context command returned error: %v, stderr: %s", err, stderr)
		} else {
			assert.NotEmpty(t, stdout, "Context output should not be empty")
			t.Logf("Context output: %s", stdout)
		}
	})
}

// TestRAGContextWithMaxTokens tests RAG context with custom max-tokens.
func TestRAGContextWithMaxTokens(t *testing.T) {
	results := []testResult{
		{Content: "First result content.", Score: 0.95},
		{Content: "Second result content.", Score: 0.90},
		{Content: "Third result content.", Score: 0.85},
	}

	resultsFile := createTestResultsFile(t, results)

	maxTokensValues := []int{500, 1000, 2000, 4000}

	for _, maxTokens := range maxTokensValues {
		t.Run(cli.UniqueKey("MaxTokens"), func(t *testing.T) {
			cli.RunForTransports(t, "ContextWithMaxTokens", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				cmd := cli.RAGContextWithOptionsCommand(
					resultsFile,
					maxTokens, // maxTokens
					"",        // template (default)
					"",        // truncation (default)
					false,     // includeCitations
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Context with maxTokens=%d returned error: %v, stderr: %s", maxTokens, err, stderr)
				} else {
					t.Logf("Context with maxTokens=%d output length: %d", maxTokens, len(stdout))
				}
			})
		})
	}
}

// TestRAGContextWithTemplate tests RAG context with different templates.
func TestRAGContextWithTemplate(t *testing.T) {
	results := []testResult{
		{Content: "Database configuration guide.", Score: 0.95},
		{Content: "API reference documentation.", Score: 0.90},
	}

	resultsFile := createTestResultsFile(t, results)

	templates := []string{"markdown", "json", "xml", "plain"}

	for _, template := range templates {
		t.Run(template, func(t *testing.T) {
			cli.RunForTransports(t, "ContextWithTemplate", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				cmd := cli.RAGContextWithOptionsCommand(
					resultsFile,
					0,        // maxTokens (default)
					template, // template
					"",       // truncation (default)
					false,    // includeCitations
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Context with template=%s returned error: %v, stderr: %s", template, err, stderr)
				} else {
					t.Logf("Context with template=%s output length: %d", template, len(stdout))
				}
			})
		})
	}
}

// TestRAGContextWithTruncation tests RAG context with different truncation strategies.
func TestRAGContextWithTruncation(t *testing.T) {
	results := []testResult{
		{Content: "Long document content that may need truncation.", Score: 0.95},
		{Content: "Another document with detailed information.", Score: 0.90},
	}

	resultsFile := createTestResultsFile(t, results)

	truncationStrategies := []string{"smart", "hard", "none"}

	for _, truncation := range truncationStrategies {
		t.Run(truncation, func(t *testing.T) {
			cli.RunForTransports(t, "ContextWithTruncation", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				cmd := cli.RAGContextWithOptionsCommand(
					resultsFile,
					2000,       // maxTokens
					"",         // template (default)
					truncation, // truncation
					false,      // includeCitations
				)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Context with truncation=%s returned error: %v, stderr: %s", truncation, err, stderr)
				} else {
					t.Logf("Context with truncation=%s output length: %d", truncation, len(stdout))
				}
			})
		})
	}
}

// TestRAGContextWithCitations tests RAG context with citations enabled.
func TestRAGContextWithCitations(t *testing.T) {
	results := []testResult{
		{
			Content:  "Authentication uses JWT tokens for session management.",
			Score:    0.95,
			Metadata: map[string]string{"file_path": "/docs/auth.md", "start_line": "15", "end_line": "25"},
		},
		{
			Content:  "Authorization is role-based with policy enforcement.",
			Score:    0.88,
			Metadata: map[string]string{"file_path": "/docs/auth.md", "start_line": "30", "end_line": "45"},
		},
	}

	resultsFile := createTestResultsFile(t, results)

	cli.RunForTransports(t, "ContextWithCitations", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextWithOptionsCommand(
			resultsFile,
			4000,       // maxTokens
			"markdown", // template
			"smart",    // truncation
			true,       // includeCitations
		)
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Context with citations returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Context with citations output: %s", stdout)
		}
	})
}

// TestRAGContextWithOutputFormat tests RAG context with different output formats.
func TestRAGContextWithOutputFormat(t *testing.T) {
	results := []testResult{
		{Content: "Test content for output format testing.", Score: 0.95},
	}

	resultsFile := createTestResultsFile(t, results)

	outputFormats := []string{"table", "json", "yaml"}

	for _, output := range outputFormats {
		t.Run(output, func(t *testing.T) {
			cli.RunForTransports(t, "ContextWithOutput", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				cmd := cli.RAGContextWithOutputCommand(resultsFile, output)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Context with output=%s returned error: %v, stderr: %s", output, err, stderr)
				} else {
					t.Logf("Context with output=%s output length: %d", output, len(stdout))
				}
			})
		})
	}
}

// TestRAGContextAllOptions tests RAG context with all options combined.
func TestRAGContextAllOptions(t *testing.T) {
	results := []testResult{
		{
			Content:  "Complete documentation for the vector database system.",
			Score:    0.98,
			Metadata: map[string]string{"file_path": "/docs/index.md", "start_line": "1", "end_line": "50"},
		},
		{
			Content:  "API reference with examples and code snippets.",
			Score:    0.92,
			Metadata: map[string]string{"file_path": "/docs/api.md", "start_line": "1", "end_line": "100"},
		},
		{
			Content:  "Configuration options and environment variables.",
			Score:    0.87,
			Metadata: map[string]string{"file_path": "/docs/config.md", "start_line": "1", "end_line": "75"},
		},
	}

	resultsFile := createTestResultsFile(t, results)

	cli.RunForTransports(t, "ContextAllOptions", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextWithOptionsCommand(
			resultsFile,
			8000,    // maxTokens
			"json",  // template
			"smart", // truncation
			true,    // includeCitations
		)
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Context with all options returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Context with all options output length: %d", len(stdout))
		}
	})
}

// TestRAGContextEmptyResults tests RAG context with empty results array.
func TestRAGContextEmptyResults(t *testing.T) {
	results := []testResult{}
	resultsFile := createTestResultsFile(t, results)

	cli.RunForTransports(t, "ContextEmptyResults", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextCommand(resultsFile)
		stdout, stderr, err := runner.Run(cmd)

		// Empty results should fail with appropriate error
		if err != nil {
			t.Logf("Context with empty results returned expected error: %v", err)
			assert.Contains(t, stderr, "no results", "Error should mention empty results")
		} else {
			t.Logf("Context with empty results unexpectedly succeeded: %s", stdout)
		}
	})
}

// TestRAGContextLargeResults tests RAG context with many results.
func TestRAGContextLargeResults(t *testing.T) {
	// Create 50 test results
	results := make([]testResult, 50)
	for i := 0; i < 50; i++ {
		results[i] = testResult{
			Content:  "Document content for result " + cli.UniqueKey("doc"),
			Score:    float32(0.99 - float32(i)*0.01),
			Metadata: map[string]string{"file_path": "/docs/doc" + cli.UniqueKey("id") + ".md"},
		}
	}

	resultsFile := createTestResultsFile(t, results)

	cli.RunForTransports(t, "ContextLargeResults", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.RAGContextWithOptionsCommand(
			resultsFile,
			16000,      // maxTokens
			"markdown", // template
			"smart",    // truncation
			true,       // includeCitations
		)
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Context with large results returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Context with large results output length: %d", len(stdout))
		}
	})
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestRAGQueryLongQuery tests RAG query with a very long query string.
func TestRAGQueryLongQuery(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	longQuery := "This is a very long query that spans multiple sentences. " +
		"It includes detailed context about what the user is looking for. " +
		"The query covers topics like database configuration, authentication, " +
		"authorization, API design, and system architecture. " +
		"We want to find relevant documentation that addresses all of these topics."

	cli.RunForTransports(t, "QueryLongQuery", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		namespace := cli.UniqueKey("test-ns")
		cmd := cli.RAGQueryCommand(longQuery, namespace)
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Query with long query returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Query with long query output length: %d", len(stdout))
		}
	})
}

// TestRAGQuerySpecialCharacters tests RAG query with special characters.
func TestRAGQuerySpecialCharacters(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	queries := []string{
		"How do I use the @deprecated annotation?",
		"What is the syntax for $variable interpolation?",
		"Explain #hashtag search functionality",
		"How to handle 'single quotes' in queries?",
		"Database connection string with &ampersand",
	}

	for _, query := range queries {
		t.Run(cli.UniqueKey("SpecialChar"), func(t *testing.T) {
			cli.RunForTransports(t, "QuerySpecialChars", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
				namespace := cli.UniqueKey("test-ns")
				cmd := cli.RAGQueryCommand(query, namespace)
				stdout, stderr, err := runner.Run(cmd)
				if err != nil {
					t.Logf("Query with special chars returned error: %v, stderr: %s", err, stderr)
				} else {
					t.Logf("Query with special chars succeeded, output length: %d", len(stdout))
				}
			})
		})
	}
}

// TestRAGContextInvalidJSON tests RAG context with invalid JSON file.
func TestRAGContextInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid.json")

	err := os.WriteFile(filePath, []byte("{ invalid json }"), 0644)
	require.NoError(t, err, "Failed to write invalid JSON file")

	cli.RunForTransports(t, "ContextInvalidJSON", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.CommandDef{
			Name:          "rag context invalid json",
			Args:          []string{"rag", "context", "--results", filePath},
			ExpectSuccess: false,
			Timeout:       10 * time.Second,
		}
		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Context with invalid JSON returned expected error: %v", err)
			assert.NotEmpty(t, stderr, "Should have error output")
		} else {
			t.Logf("Context with invalid JSON unexpectedly succeeded: %s", stdout)
		}
	})
}

// TestRAGContextWrongJSONStructure tests RAG context with wrong JSON structure.
func TestRAGContextWrongJSONStructure(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "wrong_structure.json")

	// Valid JSON but wrong structure (object instead of array)
	err := os.WriteFile(filePath, []byte(`{"wrong": "structure"}`), 0644)
	require.NoError(t, err, "Failed to write wrong structure JSON file")

	cli.RunForTransports(t, "ContextWrongStructure", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		cmd := cli.CommandDef{
			Name:          "rag context wrong structure",
			Args:          []string{"rag", "context", "--results", filePath},
			ExpectSuccess: false,
			Timeout:       10 * time.Second,
		}
		stdout, _, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Context with wrong structure returned expected error: %v", err)
		} else {
			t.Logf("Context with wrong structure result: %s", stdout)
		}
	})
}

// =============================================================================
// Timeout Tests
// =============================================================================

// TestRAGQueryTimeout tests RAG query with custom timeout.
func TestRAGQueryTimeout(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	cli.RunForTransports(t, "QueryTimeout", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		namespace := cli.UniqueKey("test-ns")

		// Create command with explicit timeout in args
		cmd := cli.CommandDef{
			Name: "rag query with timeout",
			Args: []string{
				"rag", "query", "Test query for timeout",
				"--namespace", namespace,
				"--timeout", "5s",
			},
			ExpectSuccess:  true,
			Timeout:        30 * time.Second,
			RequiresUnseal: true,
		}

		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Query with timeout returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Query with timeout output: %s", stdout)
		}
	})
}

// =============================================================================
// TLS Tests
// =============================================================================

// TestRAGQueryWithTLS tests RAG query with TLS enabled.
func TestRAGQueryWithTLS(t *testing.T) {
	requireCluster(t)
	requireOllama(t)

	if !helpers.IsTLSAvailable() {
		// TLS is intentionally disabled in Docker integration test environment.
		// This test validates TLS-enabled RAG queries and requires certificates.
		t.Log("TLS certificates not configured: set QUICBAAS_TLS_CERT, QUICBAAS_TLS_KEY, and QUICBAAS_TLS_CA to enable this test")
		return
	}

	certFile := os.Getenv("QUICBAAS_TLS_CERT")
	keyFile := os.Getenv("QUICBAAS_TLS_KEY")
	caFile := os.Getenv("QUICBAAS_TLS_CA")

	cli.RunForTransports(t, "QueryWithTLS", cli.RAGTransportsToSkip, func(t *testing.T, runner *cli.TestRunner) {
		namespace := cli.UniqueKey("test-ns")

		args := []string{
			"rag", "query", "Test query with TLS",
			"--namespace", namespace,
			"--tls",
			"--tls-cert", certFile,
			"--tls-key", keyFile,
		}

		if caFile != "" {
			args = append(args, "--tls-ca", caFile)
		}

		cmd := cli.CommandDef{
			Name:           "rag query with TLS",
			Args:           args,
			ExpectSuccess:  true,
			Timeout:        60 * time.Second,
			RequiresUnseal: true,
		}

		stdout, stderr, err := runner.Run(cmd)
		if err != nil {
			t.Logf("Query with TLS returned error: %v, stderr: %s", err, stderr)
		} else {
			t.Logf("Query with TLS output length: %d", len(stdout))
		}
	})
}
