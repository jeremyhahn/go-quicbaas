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

package pipeline

import (
	"errors"
	"fmt"
)

var (
	// Pipeline errors.
	ErrPipelineNotInitialized = errors.New("pipeline not initialized")
	ErrPipelineClosed         = errors.New("pipeline already closed")
	ErrQueryTimeout           = errors.New("query timeout exceeded")
	ErrInvalidQuery           = errors.New("invalid query")
	ErrNoResults              = errors.New("no results found")

	// Component errors.
	ErrQueryHandlerFailed  = errors.New("query handler failed")
	ErrVectorSearchFailed  = errors.New("vector search failed")
	ErrKeywordSearchFailed = errors.New("keyword search failed")
	ErrRankingFailed       = errors.New("ranking failed")
	ErrRerankingFailed     = errors.New("reranking failed")
	ErrContextBuildFailed  = errors.New("context building failed")

	// Resource errors.
	ErrConcurrencyLimitReached = errors.New("concurrency limit reached")
	ErrCircuitBreakerOpen      = errors.New("circuit breaker open")
	ErrResourceExhausted       = errors.New("resource exhausted")

	// Configuration errors.
	ErrInvalidConfiguration = errors.New("invalid configuration")
	ErrMissingDependency    = errors.New("missing required dependency")

	// Retry errors.
	ErrInvalidMaxAttempts = errors.New("max attempts must be positive")
)

// PipelineError wraps errors with context.
type PipelineError struct {
	Err       error
	Component string
	Operation string
	Details   map[string]interface{}
}

func (e *PipelineError) Error() string {
	return fmt.Sprintf("%s failed during %s: %v", e.Component, e.Operation, e.Err)
}

func (e *PipelineError) Unwrap() error {
	return e.Err
}

// NewPipelineError creates a new pipeline error.
func NewPipelineError(err error, component, operation string, details map[string]interface{}) error {
	return &PipelineError{
		Err:       err,
		Component: component,
		Operation: operation,
		Details:   details,
	}
}
