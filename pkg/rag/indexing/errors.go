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

// Package indexing provides an auto-indexing service that wires the watcher,
// chunker, embedder, and vector store pipeline for incremental file indexing.
package indexing

// IndexingDisabledError indicates that indexing is not enabled in the configuration.
type IndexingDisabledError struct{}

// Error implements the error interface.
func (e *IndexingDisabledError) Error() string {
	return "indexing is not enabled"
}

// IndexingAlreadyRunningError indicates that the indexing service has already been started.
type IndexingAlreadyRunningError struct{}

// Error implements the error interface.
func (e *IndexingAlreadyRunningError) Error() string {
	return "indexing service is already running"
}

// IndexingNotRunningError indicates that the indexing service has not been started.
type IndexingNotRunningError struct{}

// Error implements the error interface.
func (e *IndexingNotRunningError) Error() string {
	return "indexing service is not running"
}

// FileReadError indicates a failure to read a file for indexing.
type FileReadError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *FileReadError) Error() string {
	return "failed to read file for indexing: " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *FileReadError) Unwrap() error {
	return e.Cause
}

// ChunkingError indicates a failure to chunk file content.
type ChunkingError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *ChunkingError) Error() string {
	return "failed to chunk file: " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *ChunkingError) Unwrap() error {
	return e.Cause
}

// EmbeddingError indicates a failure to generate embeddings for chunks.
type EmbeddingError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *EmbeddingError) Error() string {
	return "failed to embed chunks for file: " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *EmbeddingError) Unwrap() error {
	return e.Cause
}

// IndexStoreError indicates a failure to store vectors in the index.
type IndexStoreError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *IndexStoreError) Error() string {
	return "failed to store vectors for file: " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *IndexStoreError) Unwrap() error {
	return e.Cause
}

// MetadataStoreError indicates a failure to update the metadata store.
type MetadataStoreError struct {
	Operation string
	Key       string
	Cause     error
}

// Error implements the error interface.
func (e *MetadataStoreError) Error() string {
	return "metadata store " + e.Operation + " failed for key: " + e.Key + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MetadataStoreError) Unwrap() error {
	return e.Cause
}

// UnsupportedIndexTypeError indicates an unknown or unsupported index type was requested.
type UnsupportedIndexTypeError struct {
	IndexType string
}

// Error implements the error interface.
func (e *UnsupportedIndexTypeError) Error() string {
	return "unsupported index type: " + e.IndexType
}

// GPUUnavailableError indicates that GPU acceleration was requested but is not available.
type GPUUnavailableError struct{}

// Error implements the error interface.
func (e *GPUUnavailableError) Error() string {
	return "GPU acceleration requested but not available"
}

// ConfigValidationError indicates an invalid indexing configuration field.
type ConfigValidationError struct {
	Field  string
	Reason string
}

// Error implements the error interface.
func (e *ConfigValidationError) Error() string {
	return "invalid indexing config: " + e.Field + " " + e.Reason
}

// QRDBVectorClientNilError indicates that no VectorClient was provided.
type QRDBVectorClientNilError struct{}

// Error implements the error interface.
func (e *QRDBVectorClientNilError) Error() string {
	return "qrdb vector index: VectorClient must not be nil"
}

// QRDBVectorNamespaceEmptyError indicates that no namespace was provided.
type QRDBVectorNamespaceEmptyError struct{}

// Error implements the error interface.
func (e *QRDBVectorNamespaceEmptyError) Error() string {
	return "qrdb vector index: namespace must not be empty"
}

// QRDBVectorCreateNamespaceError wraps a failure to create a qrdb namespace.
type QRDBVectorCreateNamespaceError struct {
	Namespace string
	Cause     error
}

// Error implements the error interface.
func (e *QRDBVectorCreateNamespaceError) Error() string {
	return "qrdb vector index: failed to create namespace " + e.Namespace + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *QRDBVectorCreateNamespaceError) Unwrap() error {
	return e.Cause
}

// QRDBVectorUpsertError wraps a failure to upsert a vector.
type QRDBVectorUpsertError struct {
	ID    string
	Cause error
}

// Error implements the error interface.
func (e *QRDBVectorUpsertError) Error() string {
	return "qrdb vector index: upsert failed for id " + e.ID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *QRDBVectorUpsertError) Unwrap() error {
	return e.Cause
}

// QRDBVectorSearchError wraps a failure to perform a similarity search.
type QRDBVectorSearchError struct {
	Cause error
}

// Error implements the error interface.
func (e *QRDBVectorSearchError) Error() string {
	return "qrdb vector index: search failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *QRDBVectorSearchError) Unwrap() error {
	return e.Cause
}

// QRDBVectorDeleteError wraps a failure to delete a vector.
type QRDBVectorDeleteError struct {
	ID    string
	Cause error
}

// Error implements the error interface.
func (e *QRDBVectorDeleteError) Error() string {
	return "qrdb vector index: delete failed for id " + e.ID + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *QRDBVectorDeleteError) Unwrap() error {
	return e.Cause
}
