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

package indexing

import (
	"errors"
	"strings"
	"testing"
)

func TestIndexingDisabledError_Error(t *testing.T) {
	err := &IndexingDisabledError{}
	if err.Error() != "indexing is not enabled" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

func TestIndexingAlreadyRunningError_Error(t *testing.T) {
	err := &IndexingAlreadyRunningError{}
	if err.Error() != "indexing service is already running" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

func TestIndexingNotRunningError_Error(t *testing.T) {
	err := &IndexingNotRunningError{}
	if err.Error() != "indexing service is not running" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

func TestFileReadError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("permission denied")
	err := &FileReadError{Path: "/test/file.go", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "/test/file.go") {
		t.Fatalf("error message should contain file path, got: %s", msg)
	}
	if !strings.Contains(msg, "permission denied") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap should return the cause")
	}
}

func TestChunkingError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("parse error")
	err := &ChunkingError{Path: "/test/file.go", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "/test/file.go") {
		t.Fatalf("error message should contain file path, got: %s", msg)
	}
	if !strings.Contains(msg, "parse error") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap should return the cause")
	}
}

func TestEmbeddingError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("service unavailable")
	err := &EmbeddingError{Path: "/test/file.go", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "/test/file.go") {
		t.Fatalf("error message should contain file path, got: %s", msg)
	}
	if !strings.Contains(msg, "service unavailable") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap should return the cause")
	}
}

func TestIndexStoreError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("dimension mismatch")
	err := &IndexStoreError{Path: "/test/file.go", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "/test/file.go") {
		t.Fatalf("error message should contain file path, got: %s", msg)
	}
	if !strings.Contains(msg, "dimension mismatch") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap should return the cause")
	}
}

func TestMetadataStoreError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("io error")
	err := &MetadataStoreError{Operation: "put", Key: "idx:ns:abc", Cause: cause}

	msg := err.Error()
	if !strings.Contains(msg, "put") {
		t.Fatalf("error message should contain operation, got: %s", msg)
	}
	if !strings.Contains(msg, "idx:ns:abc") {
		t.Fatalf("error message should contain key, got: %s", msg)
	}
	if !strings.Contains(msg, "io error") {
		t.Fatalf("error message should contain cause, got: %s", msg)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap should return the cause")
	}
}

func TestUnsupportedIndexTypeError_Error(t *testing.T) {
	err := &UnsupportedIndexTypeError{IndexType: "gpu_cagra"}
	msg := err.Error()
	if !strings.Contains(msg, "gpu_cagra") {
		t.Fatalf("error message should contain index type, got: %s", msg)
	}
}

func TestGPUUnavailableError_Error(t *testing.T) {
	err := &GPUUnavailableError{}
	if !strings.Contains(err.Error(), "GPU") {
		t.Fatalf("error message should mention GPU, got: %s", err.Error())
	}
}

func TestConfigValidationError_Error(t *testing.T) {
	err := &ConfigValidationError{Field: "BatchSize", Reason: "must be positive"}
	msg := err.Error()
	if !strings.Contains(msg, "BatchSize") {
		t.Fatalf("error message should contain field, got: %s", msg)
	}
	if !strings.Contains(msg, "must be positive") {
		t.Fatalf("error message should contain reason, got: %s", msg)
	}
}

func TestErrorTypes_ImplementError(t *testing.T) {
	// Verify all error types implement the error interface.
	var _ error = (*IndexingDisabledError)(nil)
	var _ error = (*IndexingAlreadyRunningError)(nil)
	var _ error = (*IndexingNotRunningError)(nil)
	var _ error = (*FileReadError)(nil)
	var _ error = (*ChunkingError)(nil)
	var _ error = (*EmbeddingError)(nil)
	var _ error = (*IndexStoreError)(nil)
	var _ error = (*MetadataStoreError)(nil)
	var _ error = (*UnsupportedIndexTypeError)(nil)
	var _ error = (*GPUUnavailableError)(nil)
	var _ error = (*ConfigValidationError)(nil)
}
