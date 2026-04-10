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

package grpc

import (
	"fmt"
	"math"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

const (
	maxBatchSize  = 10000
	maxK          = 10000
	maxPageSize   = 10000
	maxDimensions = 65536
)

// VectorValidator validates vector service requests.
type VectorValidator struct{}

// NewVectorValidator creates a new VectorValidator.
func NewVectorValidator() *VectorValidator {
	return &VectorValidator{}
}

// ValidateUpsertRequest validates an upsert vector request.
func (v *VectorValidator) ValidateUpsertRequest(req *pb.UpsertVectorRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Vector == nil {
		return api.NewRequiredFieldError("vector")
	}
	if req.Vector.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if req.Vector.Id == "" {
		return api.NewRequiredFieldError("vector ID")
	}
	if len(req.Vector.Vector) == 0 {
		return api.NewRequiredFieldError("vector data")
	}

	// Validate vector values for NaN and Inf
	for i, val := range req.Vector.Vector {
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return api.NewInvalidInputError(
				fmt.Sprintf("vector contains invalid value at index %d: NaN or Inf not allowed", i))
		}
	}

	return nil
}

// ValidateBatchUpsertRequest validates a batch upsert request.
func (v *VectorValidator) ValidateBatchUpsertRequest(req *pb.BatchUpsertVectorsRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if len(req.Vectors) == 0 {
		return api.NewEmptyCollectionError("vectors")
	}

	// Check maximum batch size
	if len(req.Vectors) > maxBatchSize {
		return api.NewInvalidInputError(
			fmt.Sprintf("batch size %d exceeds maximum allowed %d", len(req.Vectors), maxBatchSize))
	}

	namespace := ""
	for i, vec := range req.Vectors {
		if vec.Namespace == "" {
			return api.NewBatchItemValidationError(i, "namespace is required")
		}
		if namespace == "" {
			namespace = vec.Namespace
		} else if vec.Namespace != namespace {
			return api.NewInvalidInputError("all vectors must be in the same namespace")
		}
		if vec.Id == "" {
			return api.NewBatchItemValidationError(i, "ID is required")
		}
		if len(vec.Vector) == 0 {
			return api.NewBatchItemValidationError(i, "vector data is required")
		}

		// Validate vector values for NaN and Inf
		for j, val := range vec.Vector {
			if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
				return api.NewBatchItemValidationError(i,
					fmt.Sprintf("contains invalid value at index %d: NaN or Inf not allowed", j))
			}
		}
	}
	return nil
}

// ValidateDeleteRequest validates a delete vector request.
func (v *VectorValidator) ValidateDeleteRequest(req *pb.DeleteVectorRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if req.Id == "" {
		return api.NewRequiredFieldError("vector ID")
	}
	return nil
}

// ValidateBatchDeleteRequest validates a batch delete request.
func (v *VectorValidator) ValidateBatchDeleteRequest(req *pb.BatchDeleteVectorsRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if len(req.Ids) == 0 {
		return api.NewRequiredFieldError("at least one vector ID")
	}
	return nil
}

// ValidateGetRequest validates a get vector request.
func (v *VectorValidator) ValidateGetRequest(req *pb.GetVectorRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if req.Id == "" {
		return api.NewRequiredFieldError("vector ID")
	}
	return nil
}

// ValidateSearchRequest validates a search request.
func (v *VectorValidator) ValidateSearchRequest(req *pb.SearchSimilarRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if len(req.QueryVector) == 0 {
		return api.NewRequiredFieldError("query vector")
	}
	if req.K <= 0 {
		return api.NewInvalidInputError("k must be positive")
	}
	if req.K > maxK {
		return api.NewInvalidInputError(
			fmt.Sprintf("k %d exceeds maximum allowed %d", req.K, maxK))
	}

	// Validate query vector values for NaN and Inf
	for i, val := range req.QueryVector {
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return api.NewInvalidInputError(
				fmt.Sprintf("query vector contains invalid value at index %d: NaN or Inf not allowed", i))
		}
	}

	return nil
}

// ValidateSearchByIDRequest validates a search by ID request.
func (v *VectorValidator) ValidateSearchByIDRequest(req *pb.SearchByIDRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if req.Id == "" {
		return api.NewRequiredFieldError("vector ID")
	}
	if req.K <= 0 {
		return api.NewInvalidInputError("k must be positive")
	}
	if req.K > maxK {
		return api.NewInvalidInputError(
			fmt.Sprintf("k %d exceeds maximum allowed %d", req.K, maxK))
	}
	return nil
}

// ValidateListRequest validates a list vectors request.
func (v *VectorValidator) ValidateListRequest(req *pb.ListVectorsRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if req.PageSize < 0 {
		return api.NewInvalidInputError("page size cannot be negative")
	}
	if req.PageSize > maxPageSize {
		return api.NewInvalidInputError(
			fmt.Sprintf("page size cannot exceed %d", maxPageSize))
	}
	return nil
}

// ValidateCreateNamespaceRequest validates a create namespace request.
func (v *VectorValidator) ValidateCreateNamespaceRequest(req *pb.CreateNamespaceRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Config == nil {
		return api.NewRequiredFieldError("config")
	}
	if req.Config.Namespace == "" {
		return api.NewRequiredFieldError("namespace name")
	}
	if req.Config.Dimensions <= 0 {
		return api.NewInvalidInputError("dimensions must be positive")
	}
	if req.Config.Dimensions > maxDimensions {
		return api.NewInvalidInputError(
			fmt.Sprintf("dimensions cannot exceed %d", maxDimensions))
	}
	if req.Config.DistanceMetric == pb.DistanceMetric_DISTANCE_METRIC_UNSPECIFIED {
		return api.NewRequiredFieldError("distance_metric")
	}
	return nil
}

// ValidateUpdateNamespaceRequest validates an update namespace request.
func (v *VectorValidator) ValidateUpdateNamespaceRequest(req *pb.UpdateNamespaceRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	return nil
}

// ValidateDeleteNamespaceRequest validates a delete namespace request.
func (v *VectorValidator) ValidateDeleteNamespaceRequest(req *pb.DeleteNamespaceRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	return nil
}

// ValidateGetNamespaceInfoRequest validates a get namespace info request.
func (v *VectorValidator) ValidateGetNamespaceInfoRequest(req *pb.GetNamespaceInfoRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	return nil
}

// ValidateRebuildIndexRequest validates a rebuild index request.
func (v *VectorValidator) ValidateRebuildIndexRequest(req *pb.RebuildIndexRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	return nil
}

// ValidateUpdateMetadataRequest validates an update metadata request.
func (v *VectorValidator) ValidateUpdateMetadataRequest(req *pb.UpdateMetadataRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	if req.Id == "" {
		return api.NewRequiredFieldError("vector ID")
	}
	if len(req.Metadata) == 0 {
		return api.NewEmptyCollectionError("metadata")
	}
	return nil
}

// ValidateGetVectorStatsRequest validates a get vector stats request.
func (v *VectorValidator) ValidateGetVectorStatsRequest(req *pb.GetVectorStatsRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	// Namespace is optional - empty returns cluster-wide stats
	return nil
}

// ValidateGetIndexProgressRequest validates a get index progress request.
func (v *VectorValidator) ValidateGetIndexProgressRequest(req *pb.GetIndexProgressRequest) error {
	if req == nil {
		return api.NewInvalidInputError("request cannot be nil")
	}
	if req.Namespace == "" {
		return api.NewRequiredFieldError("namespace")
	}
	// operation_id is optional - empty returns current/latest operation
	return nil
}
