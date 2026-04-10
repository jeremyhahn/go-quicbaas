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
	gocontext "context"

	ragcontext "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ContextBuilderConfig holds configuration for the context builder adapter.
type ContextBuilderConfig struct {
	// DefaultMaxTokens is the default maximum token limit.
	DefaultMaxTokens int

	// DefaultTemplate is the default output template.
	DefaultTemplate ragcontext.Template

	// IncludeCitations controls whether to include source citations.
	IncludeCitations bool

	// IncludeMetadata controls whether to include metadata in output.
	IncludeMetadata bool
}

// DefaultContextBuilderConfig returns sensible defaults for the context builder adapter.
func DefaultContextBuilderConfig() *ContextBuilderConfig {
	return &ContextBuilderConfig{
		DefaultMaxTokens: 8000,
		DefaultTemplate:  ragcontext.TemplateMarkdown,
		IncludeCitations: true,
		IncludeMetadata:  true,
	}
}

// Validate validates the context builder configuration.
func (c *ContextBuilderConfig) Validate() error {
	if c.DefaultMaxTokens <= 0 {
		return ErrInvalidMaxTokens
	}
	return nil
}

// ContextBuilderOption is a functional option for configuring the ContextBuilderAdapter.
type ContextBuilderOption func(*ContextBuilderAdapter)

// WithContextBuilderConfig sets the context builder configuration.
func WithContextBuilderConfig(config *ContextBuilderConfig) ContextBuilderOption {
	return func(a *ContextBuilderAdapter) {
		if config != nil {
			a.config = config
		}
	}
}

// WithContextBuilderLogger sets the logger for the context builder adapter.
func WithContextBuilderLogger(log *logger.Logger) ContextBuilderOption {
	return func(a *ContextBuilderAdapter) {
		if log != nil {
			a.logger = log
		}
	}
}

// WithDefaultMaxTokens sets the default max tokens.
func WithDefaultMaxTokens(maxTokens int) ContextBuilderOption {
	return func(a *ContextBuilderAdapter) {
		if maxTokens > 0 {
			a.config.DefaultMaxTokens = maxTokens
		}
	}
}

// WithIncludeCitations enables or disables citations.
func WithIncludeCitations(include bool) ContextBuilderOption {
	return func(a *ContextBuilderAdapter) {
		a.config.IncludeCitations = include
	}
}

// ContextBuilderAdapter adapts ragcontext.Builder to the pipeline.ContextBuilder interface.
// It bridges the context building implementation with the pipeline's expected interface,
// handling type conversions between context.Result and pipeline.Result types.
type ContextBuilderAdapter struct {
	builder ragcontext.Builder
	config  *ContextBuilderConfig
	logger  *logger.Logger
}

// NewContextBuilderAdapter creates a new context builder adapter.
func NewContextBuilderAdapter(builder ragcontext.Builder, opts ...ContextBuilderOption) (*ContextBuilderAdapter, error) {
	if builder == nil {
		return nil, ErrNilContextBuilder
	}

	adapter := &ContextBuilderAdapter{
		builder: builder,
		config:  DefaultContextBuilderConfig(),
		logger:  nil,
	}

	// Apply options
	for _, opt := range opts {
		opt(adapter)
	}

	// Validate configuration
	if err := adapter.config.Validate(); err != nil {
		return nil, err
	}

	return adapter, nil
}

// Build implements pipeline.ContextBuilder interface.
// It converts pipeline results to context results, builds the context,
// and returns a pipeline-compatible built context.
func (a *ContextBuilderAdapter) Build(ctx gocontext.Context, results []pipeline.Result, opts pipeline.ContextOptions) (*pipeline.BuiltContext, error) {
	// Validate inputs
	if len(results) == 0 {
		return nil, ErrNoResultsProvided
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Apply defaults
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = a.config.DefaultMaxTokens
	}

	// Convert pipeline results to context results
	contextResults := pipelineToContextResults(results)

	// Determine template
	template := a.getTemplate(opts.Template)

	// Build context using the underlying builder
	var builtContext *ragcontext.BuiltContext
	var err error

	if opts.Template != "" {
		builtContext, err = a.builder.BuildWithTemplate(contextResults, template, maxTokens)
	} else {
		builtContext, err = a.builder.Build(contextResults, maxTokens)
	}

	if err != nil {
		if a.logger != nil {
			a.logger.WithError(err).Error("context build operation failed")
		}
		return nil, ErrContextBuildFailed
	}

	// Check context cancellation before returning
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Convert to pipeline built context
	pipelineContext := &pipeline.BuiltContext{
		Content:   builtContext.Content,
		Tokens:    builtContext.Tokens,
		Truncated: builtContext.Truncated,
	}

	if a.logger != nil {
		a.logger.Debugf("built context with %d tokens, truncated=%v", pipelineContext.Tokens, pipelineContext.Truncated)
	}

	return pipelineContext, nil
}

// Close implements pipeline.ContextBuilder interface.
func (a *ContextBuilderAdapter) Close() error {
	// Builder doesn't have resources to close
	return nil
}

// getTemplate converts a string template name to a ragcontext.Template.
func (a *ContextBuilderAdapter) getTemplate(templateName string) ragcontext.Template {
	switch templateName {
	case "markdown":
		return ragcontext.TemplateMarkdown
	case "json":
		return ragcontext.TemplateJSON
	case "xml":
		return ragcontext.TemplateXML
	case "plain":
		return ragcontext.TemplatePlain
	default:
		return a.config.DefaultTemplate
	}
}

// pipelineToContextResults converts pipeline.Result slice to ragcontext.Result slice.
func pipelineToContextResults(results []pipeline.Result) []ragcontext.Result {
	if len(results) == 0 {
		return nil
	}

	contextResults := make([]ragcontext.Result, len(results))
	for i, r := range results {
		contextResults[i] = ragcontext.Result{
			ID:        r.ID,
			Content:   r.Content,
			Namespace: "", // Pipeline result doesn't have namespace
			Metadata: ragcontext.Metadata{
				FilePath:  r.FilePath,
				StartLine: r.StartLine,
				EndLine:   r.EndLine,
			},
			FusionScore: r.Score,
		}
	}
	return contextResults
}
