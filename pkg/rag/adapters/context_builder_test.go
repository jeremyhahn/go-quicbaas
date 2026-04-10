//go:build rag

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

package adapters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	ragcontext "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

func TestNewContextBuilderAdapter(t *testing.T) {
	t.Run("success with valid builder", func(t *testing.T) {
		builder := ragcontext.NewBuilder()

		adapter, err := NewContextBuilderAdapter(builder)

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.NotNil(t, adapter.config)
		assert.Equal(t, 8000, adapter.config.DefaultMaxTokens)
	})

	t.Run("error with nil builder", func(t *testing.T) {
		adapter, err := NewContextBuilderAdapter(nil)

		assert.Error(t, err)
		assert.Nil(t, adapter)
		assert.ErrorIs(t, err, ErrNilContextBuilder)
	})

	t.Run("success with options", func(t *testing.T) {
		builder := ragcontext.NewBuilder()
		log := createContextTestLogger(t)
		config := &ContextBuilderConfig{
			DefaultMaxTokens: 4000,
			DefaultTemplate:  ragcontext.TemplateJSON,
			IncludeCitations: false,
			IncludeMetadata:  false,
		}

		adapter, err := NewContextBuilderAdapter(
			builder,
			WithContextBuilderConfig(config),
			WithContextBuilderLogger(log),
		)

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.Equal(t, 4000, adapter.config.DefaultMaxTokens)
		assert.Equal(t, ragcontext.TemplateJSON, adapter.config.DefaultTemplate)
		assert.NotNil(t, adapter.logger)
	})

	t.Run("error with invalid config", func(t *testing.T) {
		builder := ragcontext.NewBuilder()
		config := &ContextBuilderConfig{
			DefaultMaxTokens: -100, // Invalid
		}

		adapter, err := NewContextBuilderAdapter(builder, WithContextBuilderConfig(config))

		assert.Error(t, err)
		assert.Nil(t, adapter)
		assert.ErrorIs(t, err, ErrInvalidMaxTokens)
	})

	t.Run("nil config option is ignored", func(t *testing.T) {
		builder := ragcontext.NewBuilder()

		adapter, err := NewContextBuilderAdapter(builder, WithContextBuilderConfig(nil))

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.NotNil(t, adapter.config)
	})

	t.Run("nil logger option is ignored", func(t *testing.T) {
		builder := ragcontext.NewBuilder()

		adapter, err := NewContextBuilderAdapter(builder, WithContextBuilderLogger(nil))

		require.NoError(t, err)
		assert.NotNil(t, adapter)
		assert.Nil(t, adapter.logger)
	})

	t.Run("with max tokens option", func(t *testing.T) {
		builder := ragcontext.NewBuilder()

		adapter, err := NewContextBuilderAdapter(builder, WithDefaultMaxTokens(2000))

		require.NoError(t, err)
		assert.Equal(t, 2000, adapter.config.DefaultMaxTokens)
	})

	t.Run("with include citations option", func(t *testing.T) {
		builder := ragcontext.NewBuilder()

		adapter, err := NewContextBuilderAdapter(builder, WithIncludeCitations(false))

		require.NoError(t, err)
		assert.False(t, adapter.config.IncludeCitations)
	})

	t.Run("zero max tokens option is ignored", func(t *testing.T) {
		builder := ragcontext.NewBuilder()

		adapter, err := NewContextBuilderAdapter(builder, WithDefaultMaxTokens(0))

		require.NoError(t, err)
		assert.Equal(t, 8000, adapter.config.DefaultMaxTokens) // Default value
	})
}

func TestContextBuilderAdapter_Build(t *testing.T) {
	t.Run("success with default options", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(3)
		opts := pipeline.ContextOptions{}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
		assert.NotEmpty(t, builtContext.Content)
		assert.Greater(t, builtContext.Tokens, 0)
	})

	t.Run("success with custom max tokens", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(3)
		opts := pipeline.ContextOptions{
			MaxTokens: 1000,
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
	})

	t.Run("success with markdown template", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(2)
		opts := pipeline.ContextOptions{
			Template: "markdown",
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
		assert.NotEmpty(t, builtContext.Content)
	})

	t.Run("success with json template", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(2)
		opts := pipeline.ContextOptions{
			Template: "json",
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
	})

	t.Run("success with xml template", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(2)
		opts := pipeline.ContextOptions{
			Template: "xml",
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
	})

	t.Run("success with plain template", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(2)
		opts := pipeline.ContextOptions{
			Template: "plain",
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
	})

	t.Run("success with logger enabled", func(t *testing.T) {
		builder := ragcontext.NewBuilder()
		log := createContextTestLogger(t)
		adapter, err := NewContextBuilderAdapter(builder, WithContextBuilderLogger(log))
		require.NoError(t, err)

		results := createTestContextPipelineResults(2)
		opts := pipeline.ContextOptions{}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
		assert.NotEmpty(t, builtContext.Content)
	})

	t.Run("error with empty results", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		opts := pipeline.ContextOptions{}

		builtContext, err := adapter.Build(context.Background(), nil, opts)

		assert.Error(t, err)
		assert.Nil(t, builtContext)
		assert.ErrorIs(t, err, ErrNoResultsProvided)
	})

	t.Run("error with empty result slice", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		opts := pipeline.ContextOptions{}

		builtContext, err := adapter.Build(context.Background(), []pipeline.Result{}, opts)

		assert.Error(t, err)
		assert.Nil(t, builtContext)
		assert.ErrorIs(t, err, ErrNoResultsProvided)
	})

	t.Run("context cancellation", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(3)
		opts := pipeline.ContextOptions{}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		builtContext, err := adapter.Build(ctx, results, opts)

		assert.Error(t, err)
		assert.Nil(t, builtContext)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("uses default max tokens when opts max tokens is zero", func(t *testing.T) {
		builder := ragcontext.NewBuilder()
		adapter, err := NewContextBuilderAdapter(builder, WithDefaultMaxTokens(500))
		require.NoError(t, err)

		results := createTestContextPipelineResults(1)
		opts := pipeline.ContextOptions{
			MaxTokens: 0, // Should use default
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
	})

	t.Run("unknown template uses default", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(1)
		opts := pipeline.ContextOptions{
			Template: "unknown_template",
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
	})

	t.Run("uses negative max tokens from opts triggers default", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)
		results := createTestContextPipelineResults(1)
		opts := pipeline.ContextOptions{
			MaxTokens: -1, // Should use default
		}

		builtContext, err := adapter.Build(context.Background(), results, opts)

		require.NoError(t, err)
		assert.NotNil(t, builtContext)
	})
}

func TestContextBuilderAdapter_Close(t *testing.T) {
	t.Run("close returns nil", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)

		err := adapter.Close()

		assert.NoError(t, err)
	})

	t.Run("close can be called multiple times", func(t *testing.T) {
		adapter := createTestContextBuilderAdapter(t)

		err1 := adapter.Close()
		err2 := adapter.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})
}

func TestContextBuilderConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultContextBuilderConfig()

		err := config.Validate()

		assert.NoError(t, err)
	})

	t.Run("invalid zero max tokens", func(t *testing.T) {
		config := &ContextBuilderConfig{
			DefaultMaxTokens: 0,
		}

		err := config.Validate()

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidMaxTokens)
	})

	t.Run("invalid negative max tokens", func(t *testing.T) {
		config := &ContextBuilderConfig{
			DefaultMaxTokens: -100,
		}

		err := config.Validate()

		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidMaxTokens)
	})
}

func TestDefaultContextBuilderConfig(t *testing.T) {
	config := DefaultContextBuilderConfig()

	assert.Equal(t, 8000, config.DefaultMaxTokens)
	assert.Equal(t, ragcontext.TemplateMarkdown, config.DefaultTemplate)
	assert.True(t, config.IncludeCitations)
	assert.True(t, config.IncludeMetadata)
}

func TestContextBuilderAdapter_getTemplate(t *testing.T) {
	adapter := createTestContextBuilderAdapter(t)

	tests := []struct {
		name     string
		input    string
		expected ragcontext.Template
	}{
		{"markdown", "markdown", ragcontext.TemplateMarkdown},
		{"json", "json", ragcontext.TemplateJSON},
		{"xml", "xml", ragcontext.TemplateXML},
		{"plain", "plain", ragcontext.TemplatePlain},
		{"empty uses default", "", ragcontext.TemplateMarkdown},
		{"unknown uses default", "unknown", ragcontext.TemplateMarkdown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.getTemplate(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPipelineToContextResults(t *testing.T) {
	t.Run("converts non-empty results", func(t *testing.T) {
		pipelineResults := []pipeline.Result{
			{
				ID:        "doc1",
				Content:   "content1",
				FilePath:  "/path1",
				StartLine: 1,
				EndLine:   10,
				Score:     0.9,
			},
			{
				ID:        "doc2",
				Content:   "content2",
				FilePath:  "/path2",
				StartLine: 5,
				EndLine:   15,
				Score:     0.8,
			},
		}

		contextResults := pipelineToContextResults(pipelineResults)

		require.Len(t, contextResults, 2)
		assert.Equal(t, "doc1", contextResults[0].ID)
		assert.Equal(t, "content1", contextResults[0].Content)
		assert.Equal(t, "/path1", contextResults[0].Metadata.FilePath)
		assert.Equal(t, 1, contextResults[0].Metadata.StartLine)
		assert.Equal(t, 10, contextResults[0].Metadata.EndLine)
		assert.Equal(t, float32(0.9), contextResults[0].FusionScore)
		assert.Equal(t, "doc2", contextResults[1].ID)
	})

	t.Run("returns nil for empty results", func(t *testing.T) {
		contextResults := pipelineToContextResults([]pipeline.Result{})

		assert.Nil(t, contextResults)
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		contextResults := pipelineToContextResults(nil)

		assert.Nil(t, contextResults)
	})
}

// Helper functions

func createTestContextBuilderAdapter(t *testing.T) *ContextBuilderAdapter {
	t.Helper()
	builder := ragcontext.NewBuilder()
	adapter, err := NewContextBuilderAdapter(builder)
	require.NoError(t, err)
	return adapter
}

func createTestContextPipelineResults(count int) []pipeline.Result {
	results := make([]pipeline.Result, count)
	for i := 0; i < count; i++ {
		results[i] = pipeline.Result{
			ID:        "doc" + string(rune('0'+i)),
			Content:   "Test content for document " + string(rune('0'+i)) + ". This is some sample text that will be included in the context.",
			FilePath:  "/test/file" + string(rune('0'+i)) + ".go",
			StartLine: i * 10,
			EndLine:   (i + 1) * 10,
			Score:     float32(0.9 - float64(i)*0.1),
		}
	}
	return results
}

func createContextTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}
