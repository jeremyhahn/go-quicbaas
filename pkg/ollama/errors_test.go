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

package ollama

import (
	"errors"
	"strings"
	"testing"
)

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrOllamaUnavailable", ErrOllamaUnavailable},
		{"ErrModelNotFound", ErrModelNotFound},
		{"ErrModelNotLoaded", ErrModelNotLoaded},
		{"ErrInvalidInput", ErrInvalidInput},
		{"ErrEmptyResponse", ErrEmptyResponse},
		{"ErrRequestFailed", ErrRequestFailed},
		{"ErrInvalidResponse", ErrInvalidResponse},
		{"ErrTimeout", ErrTimeout},
		{"ErrContextCanceled", ErrContextCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Errorf("expected %s to be non-nil", tt.name)
			}
			if tt.err.Error() == "" {
				t.Errorf("expected %s to have non-empty message", tt.name)
			}
		})
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	allErrors := []error{
		ErrOllamaUnavailable,
		ErrModelNotFound,
		ErrModelNotLoaded,
		ErrInvalidInput,
		ErrEmptyResponse,
		ErrRequestFailed,
		ErrInvalidResponse,
		ErrTimeout,
		ErrContextCanceled,
	}

	for i, err1 := range allErrors {
		for j, err2 := range allErrors {
			if i != j && errors.Is(err1, err2) {
				t.Errorf("expected errors to be distinct: %v and %v", err1, err2)
			}
		}
	}
}

func TestErrorWrapping(t *testing.T) {
	wrapped := errors.Join(ErrModelNotFound, errors.New("additional context"))

	if !errors.Is(wrapped, ErrModelNotFound) {
		t.Error("wrapped error should match ErrModelNotFound with errors.Is")
	}
}

// =============================================================================
// Consolidated from error_types_coverage_test.go
// =============================================================================

// TestOllamaSentinelCauseError_ErrorMessage verifies the Error() output format
// for OllamaSentinelCauseError, which combines sentinel and cause messages.
func TestOllamaSentinelCauseError_ErrorMessage(t *testing.T) {
	cause := errors.New("connection refused")
	err := &OllamaSentinelCauseError{
		Sentinel: ErrOllamaUnavailable,
		Cause:    cause,
	}

	got := err.Error()
	if !strings.Contains(got, ErrOllamaUnavailable.Error()) {
		t.Errorf("expected error to contain sentinel message %q, got %q",
			ErrOllamaUnavailable.Error(), got)
	}
	if !strings.Contains(got, cause.Error()) {
		t.Errorf("expected error to contain cause message %q, got %q",
			cause.Error(), got)
	}
}

// TestOllamaHTTPError_ErrorMessage verifies the Error() output format
// for OllamaHTTPError, which includes HTTP status code and message.
func TestOllamaHTTPError_ErrorMessage(t *testing.T) {
	err := &OllamaHTTPError{
		Sentinel:   ErrRequestFailed,
		StatusCode: 429,
		Message:    "rate limit exceeded",
	}

	got := err.Error()
	if !strings.Contains(got, "429") {
		t.Errorf("expected error to contain status code 429, got %q", got)
	}
	if !strings.Contains(got, "rate limit exceeded") {
		t.Errorf("expected error to contain message, got %q", got)
	}
	if !strings.Contains(got, ErrRequestFailed.Error()) {
		t.Errorf("expected error to contain sentinel message, got %q", got)
	}
}

// TestOllamaUnavailableAtError_ErrorAndUnwrap verifies Error() output and
// Unwrap() behavior for OllamaUnavailableAtError.
func TestOllamaUnavailableAtError_ErrorAndUnwrap(t *testing.T) {
	err := &OllamaUnavailableAtError{
		BaseURL: "http://localhost:11434",
	}

	got := err.Error()
	if !strings.Contains(got, "http://localhost:11434") {
		t.Errorf("expected error to contain base URL, got %q", got)
	}
	if !strings.Contains(got, ErrOllamaUnavailable.Error()) {
		t.Errorf("expected error to contain sentinel message, got %q", got)
	}

	// Verify Unwrap returns sentinel
	if !errors.Is(err, ErrOllamaUnavailable) {
		t.Error("expected errors.Is to match ErrOllamaUnavailable")
	}
}

// TestOllamaRetryExhaustedError_ErrorAndUnwrap verifies Error() and Unwrap()
// for OllamaRetryExhaustedError.
func TestOllamaRetryExhaustedError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("server down")
	err := &OllamaRetryExhaustedError{
		MaxRetries: 3,
		Cause:      cause,
	}

	got := err.Error()
	if !strings.Contains(got, "3") {
		t.Errorf("expected error to contain retry count, got %q", got)
	}
	if !strings.Contains(got, "server down") {
		t.Errorf("expected error to contain cause message, got %q", got)
	}

	// Verify Unwrap returns cause
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to match cause error")
	}
}

// TestOllamaEmbedIndexError_ErrorAndUnwrap verifies Error() and Unwrap()
// for OllamaEmbedIndexError.
func TestOllamaEmbedIndexError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("model not loaded")
	err := &OllamaEmbedIndexError{
		Index: 7,
		Cause: cause,
	}

	got := err.Error()
	if !strings.Contains(got, "7") {
		t.Errorf("expected error to contain index, got %q", got)
	}
	if !strings.Contains(got, "model not loaded") {
		t.Errorf("expected error to contain cause message, got %q", got)
	}

	// Verify Unwrap returns cause
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to match cause error")
	}
}
