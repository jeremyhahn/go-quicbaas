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

// Package ollama provides an HTTP client for interacting with the Ollama API.
// It supports embedding generation, model management, and health checks.
package ollama

import (
	"errors"
	"fmt"
)

// Common errors for Ollama operations.
var (
	// ErrOllamaUnavailable indicates the Ollama server is not reachable.
	ErrOllamaUnavailable = errors.New("ollama server unavailable")

	// ErrModelNotFound indicates the requested model does not exist.
	ErrModelNotFound = errors.New("model not found")

	// ErrModelNotLoaded indicates the model is not currently loaded.
	ErrModelNotLoaded = errors.New("model not loaded")

	// ErrInvalidInput indicates invalid input was provided.
	ErrInvalidInput = errors.New("invalid input")

	// ErrEmptyResponse indicates an empty response was received.
	ErrEmptyResponse = errors.New("empty response from ollama")

	// ErrRequestFailed indicates the HTTP request failed.
	ErrRequestFailed = errors.New("request failed")

	// ErrInvalidResponse indicates the response could not be parsed.
	ErrInvalidResponse = errors.New("invalid response format")

	// ErrTimeout indicates the request timed out.
	ErrTimeout = errors.New("request timeout")

	// ErrContextCanceled indicates the context was canceled.
	ErrContextCanceled = errors.New("context canceled")
)

// OllamaSentinelCauseError wraps a sentinel error with an underlying cause.
// It supports multi-unwrap (Go 1.20+) for errors.Is/As on both sentinel and cause.
type OllamaSentinelCauseError struct {
	Sentinel error
	Cause    error
}

// Error implements the error interface.
func (e *OllamaSentinelCauseError) Error() string {
	return e.Sentinel.Error() + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *OllamaSentinelCauseError) Unwrap() []error {
	return []error{e.Sentinel, e.Cause}
}

// OllamaSentinelMessageError wraps a sentinel error with a descriptive message.
type OllamaSentinelMessageError struct {
	Sentinel error
	Message  string
}

// Error implements the error interface.
func (e *OllamaSentinelMessageError) Error() string {
	return e.Sentinel.Error() + ": " + e.Message
}

// Unwrap returns the sentinel for errors.Is/As support.
func (e *OllamaSentinelMessageError) Unwrap() error {
	return e.Sentinel
}

// OllamaHTTPError wraps a sentinel error with HTTP status code and message.
type OllamaHTTPError struct {
	Sentinel   error
	StatusCode int
	Message    string
}

// Error implements the error interface.
func (e *OllamaHTTPError) Error() string {
	return fmt.Sprintf("%s: HTTP %d - %s", e.Sentinel.Error(), e.StatusCode, e.Message)
}

// Unwrap returns the sentinel for errors.Is/As support.
func (e *OllamaHTTPError) Unwrap() error {
	return e.Sentinel
}

// OllamaUnavailableAtError indicates the Ollama server is not running at a specific URL.
type OllamaUnavailableAtError struct {
	BaseURL string
}

// Error implements the error interface.
func (e *OllamaUnavailableAtError) Error() string {
	return ErrOllamaUnavailable.Error() + ": server not running at " + e.BaseURL
}

// Unwrap returns the sentinel for errors.Is/As support.
func (e *OllamaUnavailableAtError) Unwrap() error {
	return ErrOllamaUnavailable
}

// OllamaRetryExhaustedError indicates all retries have been exhausted.
type OllamaRetryExhaustedError struct {
	MaxRetries int
	Cause      error
}

// Error implements the error interface.
func (e *OllamaRetryExhaustedError) Error() string {
	return fmt.Sprintf("failed after %d retries: %v", e.MaxRetries, e.Cause)
}

// Unwrap returns the cause for errors.Is/As support.
func (e *OllamaRetryExhaustedError) Unwrap() error {
	return e.Cause
}

// OllamaEmbedIndexError indicates a failure to embed text at a specific index.
type OllamaEmbedIndexError struct {
	Index int
	Cause error
}

// Error implements the error interface.
func (e *OllamaEmbedIndexError) Error() string {
	return fmt.Sprintf("failed to embed text at index %d: %v", e.Index, e.Cause)
}

// Unwrap returns the cause for errors.Is/As support.
func (e *OllamaEmbedIndexError) Unwrap() error {
	return e.Cause
}
