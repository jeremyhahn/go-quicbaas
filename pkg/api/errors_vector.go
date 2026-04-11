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

package api

import (
	"fmt"
	"strconv"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Generalized Vector Index Error Types
// =============================================================================

// IndexNotTrainedError indicates an operation was attempted on an untrained index.
type IndexNotTrainedError struct {
	IndexType string
}

// Error implements the error interface.
func (e *IndexNotTrainedError) Error() string {
	return "index type " + e.IndexType + " is not trained"
}

// NewIndexNotTrainedError creates a QBError for an untrained index operation.
func NewIndexNotTrainedError(op, indexType string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   op,
		Err:  &IndexNotTrainedError{IndexType: indexType},
	}
}

// UnsupportedIndexTypeError indicates an unsupported index type was requested.
type UnsupportedIndexTypeError struct {
	IndexType string
}

// Error implements the error interface.
func (e *UnsupportedIndexTypeError) Error() string {
	return "unsupported index type: " + e.IndexType
}

// NewUnsupportedIndexTypeError creates a QBError for an unsupported index type.
func NewUnsupportedIndexTypeError(op, indexType string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &UnsupportedIndexTypeError{IndexType: indexType},
	}
}

// IndexCapacityExceededError indicates the index has reached its maximum capacity.
type IndexCapacityExceededError struct {
	Current int64
	Max     int64
}

// Error implements the error interface.
func (e *IndexCapacityExceededError) Error() string {
	return "index capacity exceeded: current " + strconv.FormatInt(e.Current, 10) +
		", max " + strconv.FormatInt(e.Max, 10)
}

// NewIndexCapacityExceededError creates a QBError for index capacity exceeded.
func NewIndexCapacityExceededError(op string, current, max int64) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrResourceExhausted,
		Op:   op,
		Err:  &IndexCapacityExceededError{Current: current, Max: max},
	}
}

// =============================================================================
// GPU Vector Index Error Types
// =============================================================================

// GPUNotCompiledError indicates GPU support was not compiled into the binary.
type GPUNotCompiledError struct{}

// Error implements the error interface.
func (e *GPUNotCompiledError) Error() string {
	return "GPU support not compiled (build with -tags gpu)"
}

// NewGPUNotCompiledError creates a QBError for missing GPU support.
func NewGPUNotCompiledError(op string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   op,
		Err:  &GPUNotCompiledError{},
	}
}

// GPUDeviceNotFoundError indicates the requested GPU device does not exist.
type GPUDeviceNotFoundError struct {
	DeviceID int
}

// Error implements the error interface.
func (e *GPUDeviceNotFoundError) Error() string {
	return "GPU device " + strconv.Itoa(e.DeviceID) + " not found"
}

// NewGPUDeviceNotFoundError creates a QBError for a missing GPU device.
func NewGPUDeviceNotFoundError(op string, deviceID int) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err:  &GPUDeviceNotFoundError{DeviceID: deviceID},
		Context: map[string]interface{}{
			"device_id": deviceID,
		},
	}
}

// GPUOutOfMemoryError indicates GPU memory has been exhausted.
type GPUOutOfMemoryError struct {
	RequestedBytes int64
	AvailableBytes int64
}

// Error implements the error interface.
func (e *GPUOutOfMemoryError) Error() string {
	return "GPU out of memory: requested " +
		strconv.FormatInt(e.RequestedBytes, 10) +
		" bytes, available " +
		strconv.FormatInt(e.AvailableBytes, 10) + " bytes"
}

// NewGPUOutOfMemoryError creates a QBError for GPU memory exhaustion.
func NewGPUOutOfMemoryError(op string, requested, available int64) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrResourceExhausted,
		Op:   op,
		Err: &GPUOutOfMemoryError{
			RequestedBytes: requested,
			AvailableBytes: available,
		},
		Context: map[string]interface{}{
			"requested_bytes": requested,
			"available_bytes": available,
		},
	}
}

// GPUKernelFailedError indicates a GPU kernel execution failure.
type GPUKernelFailedError struct {
	Kernel string
	Cause  error
}

// Error implements the error interface.
func (e *GPUKernelFailedError) Error() string {
	return "GPU kernel " + e.Kernel + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *GPUKernelFailedError) Unwrap() error {
	return e.Cause
}

// NewGPUKernelFailedError creates a QBError for a GPU kernel failure.
func NewGPUKernelFailedError(op, kernel string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err: &GPUKernelFailedError{
			Kernel: kernel,
			Cause:  cause,
		},
		Context: map[string]interface{}{
			"kernel": kernel,
		},
	}
}

// =============================================================================
// IVF Index Error Types
// =============================================================================

// IVFConfigValidationError indicates an IVF configuration validation failure.
type IVFConfigValidationError struct {
	Message string
}

// Error implements the error interface.
func (e *IVFConfigValidationError) Error() string {
	return e.Message
}

// NewIVFConfigValidationError creates a QBError for an IVF config validation failure.
func NewIVFConfigValidationError(op, message string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &IVFConfigValidationError{Message: message},
	}
}

// IVFInvalidConfigError indicates an invalid IVF configuration.
type IVFInvalidConfigError struct {
	Cause error
}

// Error implements the error interface.
func (e *IVFInvalidConfigError) Error() string {
	return "invalid config: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *IVFInvalidConfigError) Unwrap() error {
	return e.Cause
}

// NewIVFInvalidConfigError creates a QBError for an invalid IVF configuration.
func NewIVFInvalidConfigError(op string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &IVFInvalidConfigError{Cause: cause},
	}
}

// IVFEmptyTrainingSetError indicates an attempt to train on an empty vector set.
type IVFEmptyTrainingSetError struct{}

// Error implements the error interface.
func (e *IVFEmptyTrainingSetError) Error() string {
	return "cannot train on empty vector set"
}

// NewIVFEmptyTrainingSetError creates a QBError for training on an empty set.
func NewIVFEmptyTrainingSetError(op string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &IVFEmptyTrainingSetError{},
	}
}

// IVFDimensionMismatchError indicates a vector has the wrong number of dimensions.
type IVFDimensionMismatchError struct {
	Index    int
	Got      int
	Expected int
}

// Error implements the error interface.
func (e *IVFDimensionMismatchError) Error() string {
	if e.Index >= 0 {
		return fmt.Sprintf("vector %d has %d dimensions, expected %d", e.Index, e.Got, e.Expected)
	}
	return fmt.Sprintf("vector has %d dimensions, expected %d", e.Got, e.Expected)
}

// NewIVFDimensionMismatchError creates a QBError for a vector dimension mismatch.
func NewIVFDimensionMismatchError(op string, index, got, expected int) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &IVFDimensionMismatchError{Index: index, Got: got, Expected: expected},
	}
}

// IVFClusteringFailedError indicates k-means clustering failed.
type IVFClusteringFailedError struct {
	Cause error
}

// Error implements the error interface.
func (e *IVFClusteringFailedError) Error() string {
	return "k-means clustering failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *IVFClusteringFailedError) Unwrap() error {
	return e.Cause
}

// NewIVFClusteringFailedError creates a QBError for a clustering failure.
func NewIVFClusteringFailedError(op string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &IVFClusteringFailedError{Cause: cause},
	}
}

// IVFNotTrainedError indicates an operation was attempted on an untrained index.
type IVFNotTrainedError struct {
	Operation string
}

// Error implements the error interface.
func (e *IVFNotTrainedError) Error() string {
	return "index must be trained" + e.suffix()
}

func (e *IVFNotTrainedError) suffix() string {
	if e.Operation != "" {
		return " before " + e.Operation
	}
	return ""
}

// NewIVFNotTrainedError creates a QBError for an untrained index operation.
func NewIVFNotTrainedError(op, operation string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   op,
		Err:  &IVFNotTrainedError{Operation: operation},
	}
}

// IVFDuplicateIDError indicates a vector with the given ID already exists.
type IVFDuplicateIDError struct {
	ID string
}

// Error implements the error interface.
func (e *IVFDuplicateIDError) Error() string {
	return "vector with ID " + e.ID + " already exists"
}

// NewIVFDuplicateIDError creates a QBError for a duplicate vector ID.
func NewIVFDuplicateIDError(op, id string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   op,
		Err:  &IVFDuplicateIDError{ID: id},
	}
}

// IVFVectorNotFoundError indicates a vector with the given ID was not found.
type IVFVectorNotFoundError struct {
	ID string
}

// Error implements the error interface.
func (e *IVFVectorNotFoundError) Error() string {
	return "vector with ID " + e.ID + " not found"
}

// NewIVFVectorNotFoundError creates a QBError for a missing vector.
func NewIVFVectorNotFoundError(op, id string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   op,
		Err:  &IVFVectorNotFoundError{ID: id},
	}
}

// IVFSaveError indicates a failure saving or loading an IVF index.
type IVFSaveError struct {
	Action string
	Cause  error
}

// Error implements the error interface.
func (e *IVFSaveError) Error() string {
	return "failed to " + e.Action + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *IVFSaveError) Unwrap() error {
	return e.Cause
}

// NewIVFSaveError creates a QBError for an IVF index save/load failure.
func NewIVFSaveError(op, action string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrStorage,
		Op:   op,
		Err:  &IVFSaveError{Action: action, Cause: cause},
	}
}

// IVFUntrainedSaveError indicates an attempt to save an untrained index.
type IVFUntrainedSaveError struct{}

// Error implements the error interface.
func (e *IVFUntrainedSaveError) Error() string {
	return "cannot save untrained index"
}

// NewIVFUntrainedSaveError creates a QBError for saving an untrained index.
func NewIVFUntrainedSaveError(op string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   op,
		Err:  &IVFUntrainedSaveError{},
	}
}

// IVFQueryDimensionError indicates a query vector has the wrong dimensions.
type IVFQueryDimensionError struct {
	Got      int
	Expected int
}

// Error implements the error interface.
func (e *IVFQueryDimensionError) Error() string {
	return fmt.Sprintf("query has %d dimensions, expected %d", e.Got, e.Expected)
}

// NewIVFQueryDimensionError creates a QBError for a query dimension mismatch.
func NewIVFQueryDimensionError(op string, got, expected int) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &IVFQueryDimensionError{Got: got, Expected: expected},
	}
}

// =============================================================================
// K-Means Error Types
// =============================================================================

// KMeansEmptyVectorSetError indicates an attempt to cluster an empty vector set.
type KMeansEmptyVectorSetError struct{}

// Error implements the error interface.
func (e *KMeansEmptyVectorSetError) Error() string {
	return "cannot cluster empty vector set"
}

// NewKMeansEmptyVectorSetError creates a QBError for clustering an empty set.
func NewKMeansEmptyVectorSetError(op string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &KMeansEmptyVectorSetError{},
	}
}

// KMeansInsufficientVectorsError indicates too few vectors for the requested clusters.
type KMeansInsufficientVectorsError struct {
	Clusters int
	Vectors  int
}

// Error implements the error interface.
func (e *KMeansInsufficientVectorsError) Error() string {
	return fmt.Sprintf("cannot create %d clusters from %d vectors", e.Clusters, e.Vectors)
}

// NewKMeansInsufficientVectorsError creates a QBError for insufficient vectors.
func NewKMeansInsufficientVectorsError(op string, clusters, vectors int) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &KMeansInsufficientVectorsError{Clusters: clusters, Vectors: vectors},
	}
}

// KMeansInitError indicates failure to initialize centroids.
type KMeansInitError struct {
	Cause error
}

// Error implements the error interface.
func (e *KMeansInitError) Error() string {
	return "failed to initialize centroids: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *KMeansInitError) Unwrap() error {
	return e.Cause
}

// NewKMeansInitError creates a QBError for a centroid initialization failure.
func NewKMeansInitError(op string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &KMeansInitError{Cause: cause},
	}
}

// KMeansRecomputeError indicates failure to recompute centroids during iteration.
type KMeansRecomputeError struct {
	Iteration int
	Cause     error
}

// Error implements the error interface.
func (e *KMeansRecomputeError) Error() string {
	return "failed to recompute centroids at iteration " + strconv.Itoa(e.Iteration) + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *KMeansRecomputeError) Unwrap() error {
	return e.Cause
}

// NewKMeansRecomputeError creates a QBError for a centroid recompute failure.
func NewKMeansRecomputeError(op string, iteration int, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &KMeansRecomputeError{Iteration: iteration, Cause: cause},
	}
}

// =============================================================================
// HNSW Persistence Error Types
// =============================================================================

// HNSWPersistenceError indicates a failure during HNSW index persistence.
type HNSWPersistenceError struct {
	Action string
	Cause  error
}

// Error implements the error interface.
func (e *HNSWPersistenceError) Error() string {
	return "failed to " + e.Action + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *HNSWPersistenceError) Unwrap() error {
	return e.Cause
}

// NewHNSWPersistenceError creates a QBError for an HNSW persistence failure.
func NewHNSWPersistenceError(op, action string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrStorage,
		Op:   op,
		Err:  &HNSWPersistenceError{Action: action, Cause: cause},
	}
}

// =============================================================================
// RAG Adapter Error Types
// =============================================================================

// RAGSearchError indicates a failure during RAG vector search.
type RAGSearchError struct {
	Action string
	Cause  error
}

// Error implements the error interface.
func (e *RAGSearchError) Error() string {
	return e.Action + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *RAGSearchError) Unwrap() error {
	return e.Cause
}

// NewRAGSearchError creates a QBError for a RAG search failure.
func NewRAGSearchError(op, action string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &RAGSearchError{Action: action, Cause: cause},
	}
}

// =============================================================================
// Scalar Index Error Types
// =============================================================================

// ScalarIndexNotFoundError indicates that a scalar index for the specified field
// does not exist.
type ScalarIndexNotFoundError struct {
	Field string
}

// Error implements the error interface.
func (e *ScalarIndexNotFoundError) Error() string {
	return "scalar index not found for field: " + e.Field
}

// NewScalarIndexNotFoundError creates a QBError for a missing scalar index.
func NewScalarIndexNotFoundError(op, field string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   op,
		Err:  &ScalarIndexNotFoundError{Field: field},
		Context: map[string]interface{}{
			"field": field,
		},
	}
}

// UnsupportedScalarIndexTypeError indicates that the specified scalar index type
// is not recognized or supported.
type UnsupportedScalarIndexTypeError struct {
	IndexType string
}

// Error implements the error interface.
func (e *UnsupportedScalarIndexTypeError) Error() string {
	return "unsupported scalar index type: " + e.IndexType
}

// NewUnsupportedScalarIndexTypeError creates a QBError for an unsupported scalar index type.
func NewUnsupportedScalarIndexTypeError(op, indexType string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &UnsupportedScalarIndexTypeError{IndexType: indexType},
		Context: map[string]interface{}{
			"index_type": indexType,
		},
	}
}

// =============================================================================
// Distributed Search Error Types
// =============================================================================

// DistributedSearchTimeoutError indicates a distributed search timed out.
type DistributedSearchTimeoutError struct {
	Namespace string
	Timeout   time.Duration
}

// Error implements the error interface.
func (e *DistributedSearchTimeoutError) Error() string {
	return "distributed search timed out for namespace " + e.Namespace +
		" after " + e.Timeout.String()
}

// NewDistributedSearchTimeoutError creates a QBError for a distributed search timeout.
func NewDistributedSearchTimeoutError(op, namespace string, timeout time.Duration) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrTimeout,
		Op:   op,
		Err:  &DistributedSearchTimeoutError{Namespace: namespace, Timeout: timeout},
	}
}

// DistributedSearchNoShardsError indicates no shards were provided for distributed search.
type DistributedSearchNoShardsError struct {
	Namespace string
}

// Error implements the error interface.
func (e *DistributedSearchNoShardsError) Error() string {
	return "no shards provided for distributed search in namespace: " + e.Namespace
}

// NewDistributedSearchNoShardsError creates a QBError when no shards are provided.
func NewDistributedSearchNoShardsError(op, namespace string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &DistributedSearchNoShardsError{Namespace: namespace},
	}
}

// ShardUnavailableError indicates a shard could not be reached during distributed search.
type ShardUnavailableError struct {
	ShardID uint64
	NodeID  string
	Cause   error
}

// Error implements the error interface.
func (e *ShardUnavailableError) Error() string {
	return "shard " + strconv.FormatUint(e.ShardID, 10) +
		" on node " + e.NodeID + " unavailable: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *ShardUnavailableError) Unwrap() error {
	return e.Cause
}

// NewShardUnavailableError creates a QBError for an unavailable shard.
func NewShardUnavailableError(op string, shardID uint64, nodeID string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err: &ShardUnavailableError{
			ShardID: shardID,
			NodeID:  nodeID,
			Cause:   cause,
		},
		Retry: true,
	}
}

// MergeOverflowError indicates the total number of results exceeded the maximum.
type MergeOverflowError struct {
	TotalResults int
	MaxResults   int
}

// Error implements the error interface.
func (e *MergeOverflowError) Error() string {
	return "merge overflow: " + strconv.Itoa(e.TotalResults) +
		" results exceed max " + strconv.Itoa(e.MaxResults)
}

// NewMergeOverflowError creates a QBError for a merge result overflow.
func NewMergeOverflowError(op string, total, max int) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrResourceExhausted,
		Op:   op,
		Err:  &MergeOverflowError{TotalResults: total, MaxResults: max},
	}
}

// =============================================================================
// Quantization Error Types
// =============================================================================

// QuantizationFailedError indicates a vector quantization operation failed.
type QuantizationFailedError struct {
	Op      string
	Message string
}

// Error implements the error interface.
func (e *QuantizationFailedError) Error() string {
	return "quantization failed: " + e.Message
}

// NewQuantizationFailedError creates a QBError for a quantization failure.
func NewQuantizationFailedError(op, message string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &QuantizationFailedError{Op: op, Message: message},
	}
}

// QuantizationPrecisionMismatchError indicates a vector precision mismatch.
type QuantizationPrecisionMismatchError struct {
	Expected string
	Got      string
}

// Error implements the error interface.
func (e *QuantizationPrecisionMismatchError) Error() string {
	return "precision mismatch: expected " + e.Expected + ", got " + e.Got
}

// NewPrecisionMismatchError creates a QBError for a precision mismatch.
func NewPrecisionMismatchError(op, expected, got string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &QuantizationPrecisionMismatchError{Expected: expected, Got: got},
	}
}

// QuantizationCodebookNotTrainedError indicates PQ codebooks have not been trained.
type QuantizationCodebookNotTrainedError struct{}

// Error implements the error interface.
func (e *QuantizationCodebookNotTrainedError) Error() string {
	return "PQ codebooks not trained"
}

// NewCodebookNotTrainedError creates a QBError for untrained PQ codebooks.
func NewCodebookNotTrainedError(op string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrPreconditionFailed,
		Op:   op,
		Err:  &QuantizationCodebookNotTrainedError{},
	}
}
