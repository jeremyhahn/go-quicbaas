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
	"testing"
)

// TestErrorsAreDistinct verifies every sentinel is set, carries a message and
// does not match any other sentinel with errors.Is.
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
		if err1 == nil || err1.Error() == "" {
			t.Fatalf("sentinel %d is nil or has an empty message", i)
		}
		for j, err2 := range allErrors {
			if i != j && errors.Is(err1, err2) {
				t.Errorf("expected errors to be distinct: %v and %v", err1, err2)
			}
		}
	}
}

// TestTypedErrors verifies the Error() text of each typed error and that it
// unwraps to its sentinel or cause.
func TestTypedErrors(t *testing.T) {
	cause := errors.New("connection refused")

	tests := []struct {
		name    string
		err     error
		want    string
		unwraps []error
	}{
		{
			name:    "OllamaSentinelCauseError",
			err:     &OllamaSentinelCauseError{Sentinel: ErrOllamaUnavailable, Cause: cause},
			want:    ErrOllamaUnavailable.Error() + ": connection refused",
			unwraps: []error{ErrOllamaUnavailable, cause},
		},
		{
			name:    "OllamaHTTPError",
			err:     &OllamaHTTPError{Sentinel: ErrRequestFailed, StatusCode: 429, Message: "rate limit exceeded"},
			want:    ErrRequestFailed.Error() + ": HTTP 429 - rate limit exceeded",
			unwraps: []error{ErrRequestFailed},
		},
		{
			name:    "OllamaUnavailableAtError",
			err:     &OllamaUnavailableAtError{BaseURL: "http://localhost:11434"},
			want:    ErrOllamaUnavailable.Error() + ": server not running at http://localhost:11434",
			unwraps: []error{ErrOllamaUnavailable},
		},
		{
			name:    "OllamaRetryExhaustedError",
			err:     &OllamaRetryExhaustedError{MaxRetries: 3, Cause: cause},
			want:    "failed after 3 retries: connection refused",
			unwraps: []error{cause},
		},
		{
			name:    "OllamaEmbedIndexError",
			err:     &OllamaEmbedIndexError{Index: 7, Cause: cause},
			want:    "failed to embed text at index 7: connection refused",
			unwraps: []error{cause},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
			for _, target := range tt.unwraps {
				if !errors.Is(tt.err, target) {
					t.Errorf("expected errors.Is to match %v", target)
				}
			}
		})
	}
}
