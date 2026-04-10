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

package interfaces

import (
	"context"
	"time"
)

// ParameterStoreService defines operations for hierarchical parameter management.
// Parameters are tenant-scoped and support encryption, TTL, and tagging.
type ParameterStoreService interface {
	// GetParameter retrieves a single parameter by path.
	// Returns ErrParameterNotFound if the parameter doesn't exist.
	// Returns ErrParameterExpired if the parameter has expired.
	GetParameter(ctx context.Context, req *GetParameterRequest) (*GetParameterResponse, error)

	// SetParameter creates or updates a parameter.
	// Creates a new parameter if the path doesn't exist.
	// Updates the existing parameter if the path exists (and Overwrite is true).
	SetParameter(ctx context.Context, req *SetParameterRequest) (*SetParameterResponse, error)

	// DeleteParameter removes a parameter by path.
	// Returns ErrParameterNotFound if the parameter doesn't exist.
	DeleteParameter(ctx context.Context, req *DeleteParameterRequest) (*DeleteParameterResponse, error)

	// GetParameters retrieves multiple parameters by paths (batch get).
	// Missing or expired parameters are omitted from results with error details.
	GetParameters(ctx context.Context, req *GetParametersRequest) (*GetParametersResponse, error)

	// SetParameters creates or updates multiple parameters atomically (batch set).
	// All parameters succeed or all fail (atomic operation).
	SetParameters(ctx context.Context, req *SetParametersRequest) (*SetParametersResponse, error)

	// DeleteParameters removes multiple parameters by paths (batch delete).
	// Missing parameters are reported in the response.
	DeleteParameters(ctx context.Context, req *DeleteParametersRequest) (*DeleteParametersResponse, error)

	// ListParameters returns parameters matching a path prefix.
	// Results are sorted by path and support pagination.
	ListParameters(ctx context.Context, req *ListParametersRequest) (*ListParametersResponse, error)

	// SearchByTags finds parameters matching tag key-value pairs.
	// Multiple tags are ANDed together.
	SearchByTags(ctx context.Context, req *SearchByTagsRequest) (*SearchByTagsResponse, error)
}

// GetParameterRequest contains parameters for GetParameter.
type GetParameterRequest struct {
	// Path is the parameter path (e.g., "/app/database/host").
	Path string `json:"path"`

	// WithDecryption decrypts the value if encrypted (default: true).
	WithDecryption bool `json:"with_decryption"`
}

// GetParameterResponse contains the retrieved parameter.
type GetParameterResponse struct {
	// Parameter is the retrieved parameter.
	Parameter *ParameterValue `json:"parameter"`

	// Timestamp is when the response was generated.
	Timestamp time.Time `json:"timestamp"`
}

// SetParameterRequest contains parameters for SetParameter.
type SetParameterRequest struct {
	// Path is the parameter path (required).
	Path string `json:"path"`

	// Value is the parameter value (required).
	Value []byte `json:"value"`

	// Type specifies the value type (default: string).
	Type string `json:"type,omitempty"`

	// Description is optional documentation.
	Description string `json:"description,omitempty"`

	// KMSKeyID enables encryption with the specified key.
	KMSKeyID string `json:"kms_key_id,omitempty"`

	// Tags are key-value labels.
	Tags map[string]string `json:"tags,omitempty"`

	// TTLSeconds sets the time-to-live in seconds (0 = no expiration).
	TTLSeconds int64 `json:"ttl_seconds,omitempty"`

	// AuditEnabled enables audit logging for this parameter.
	AuditEnabled bool `json:"audit_enabled"`

	// Overwrite allows updating existing parameters (default: true).
	Overwrite bool `json:"overwrite"`
}

// SetParameterResponse confirms parameter creation/update.
type SetParameterResponse struct {
	// Path is the parameter path.
	Path string `json:"path"`

	// Created indicates if a new parameter was created.
	Created bool `json:"created"`

	// Timestamp is when the operation completed.
	Timestamp time.Time `json:"timestamp"`
}

// DeleteParameterRequest contains parameters for DeleteParameter.
type DeleteParameterRequest struct {
	// Path is the parameter path to delete.
	Path string `json:"path"`
}

// DeleteParameterResponse confirms parameter deletion.
type DeleteParameterResponse struct {
	// Path is the deleted parameter path.
	Path string `json:"path"`

	// Deleted indicates if deletion succeeded.
	Deleted bool `json:"deleted"`

	// Timestamp is when deletion occurred.
	Timestamp time.Time `json:"timestamp"`
}

// GetParametersRequest contains parameters for batch get.
type GetParametersRequest struct {
	// Paths are the parameter paths to retrieve.
	Paths []string `json:"paths"`

	// WithDecryption decrypts values if encrypted (default: true).
	WithDecryption bool `json:"with_decryption"`
}

// GetParametersResponse contains batch get results.
type GetParametersResponse struct {
	// Parameters contains successfully retrieved parameters.
	Parameters []*ParameterValue `json:"parameters"`

	// Errors contains per-path errors for failed retrievals.
	Errors map[string]string `json:"errors,omitempty"`

	// Timestamp is when the response was generated.
	Timestamp time.Time `json:"timestamp"`
}

// SetParametersRequest contains parameters for batch set.
type SetParametersRequest struct {
	// Parameters are the parameters to set.
	Parameters []*SetParameterRequest `json:"parameters"`
}

// SetParametersResponse confirms batch set.
type SetParametersResponse struct {
	// Results contains per-path results.
	Results []*SetParameterResult `json:"results"`

	// Timestamp is when the operation completed.
	Timestamp time.Time `json:"timestamp"`
}

// SetParameterResult contains the result of setting a single parameter.
type SetParameterResult struct {
	// Path is the parameter path.
	Path string `json:"path"`

	// Created indicates if a new parameter was created.
	Created bool `json:"created"`

	// Success indicates if the operation succeeded.
	Success bool `json:"success"`

	// Error contains the error message if failed.
	Error string `json:"error,omitempty"`
}

// DeleteParametersRequest contains parameters for batch delete.
type DeleteParametersRequest struct {
	// Paths are the parameter paths to delete.
	Paths []string `json:"paths"`
}

// DeleteParametersResponse confirms batch delete.
type DeleteParametersResponse struct {
	// Results contains per-path deletion results.
	Results []*DeleteParameterResult `json:"results"`

	// DeletedCount is the number of parameters successfully deleted.
	DeletedCount int `json:"deleted_count"`

	// Timestamp is when the operation completed.
	Timestamp time.Time `json:"timestamp"`
}

// DeleteParameterResult contains the result of deleting a single parameter.
type DeleteParameterResult struct {
	// Path is the parameter path.
	Path string `json:"path"`

	// Deleted indicates if deletion succeeded.
	Deleted bool `json:"deleted"`

	// Error contains the error message if failed.
	Error string `json:"error,omitempty"`
}

// ListParametersRequest contains parameters for ListParameters.
type ListParametersRequest struct {
	// Prefix filters by path prefix (e.g., "/app/" returns all under /app).
	Prefix string `json:"prefix"`

	// Recursive includes nested paths (default: true).
	Recursive bool `json:"recursive"`

	// WithDecryption decrypts values if encrypted (default: false for listing).
	WithDecryption bool `json:"with_decryption"`

	// WithValues includes parameter values (default: false for metadata only).
	WithValues bool `json:"with_values"`

	// Limit is the maximum number of results (default: 100).
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor.
	Cursor string `json:"cursor,omitempty"`
}

// ListParametersResponse contains list results.
type ListParametersResponse struct {
	// Parameters contains the listed parameters.
	Parameters []*ParameterValue `json:"parameters"`

	// NextCursor is the cursor for the next page.
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results.
	HasMore bool `json:"has_more"`

	// TotalCount is the total number of matching parameters (if available).
	TotalCount int64 `json:"total_count,omitempty"`

	// Timestamp is when the response was generated.
	Timestamp time.Time `json:"timestamp"`
}

// SearchByTagsRequest contains parameters for SearchByTags.
type SearchByTagsRequest struct {
	// Tags are required tag key-value pairs (ANDed together).
	Tags map[string]string `json:"tags"`

	// Prefix optionally filters by path prefix as well.
	Prefix string `json:"prefix,omitempty"`

	// WithValues includes parameter values (default: false).
	WithValues bool `json:"with_values"`

	// WithDecryption decrypts values if encrypted (default: false).
	WithDecryption bool `json:"with_decryption"`

	// Limit is the maximum number of results (default: 100).
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor.
	Cursor string `json:"cursor,omitempty"`
}

// SearchByTagsResponse contains search results.
type SearchByTagsResponse struct {
	// Parameters contains matching parameters.
	Parameters []*ParameterValue `json:"parameters"`

	// NextCursor is the cursor for the next page.
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results.
	HasMore bool `json:"has_more"`

	// Timestamp is when the response was generated.
	Timestamp time.Time `json:"timestamp"`
}

// ParameterValue represents a parameter with its value and metadata.
type ParameterValue struct {
	// Path is the parameter path.
	Path string `json:"path"`

	// Value is the parameter value (decrypted if requested).
	Value []byte `json:"value,omitempty"`

	// Type is the value type (string, number, json, binary).
	Type string `json:"type"`

	// Description is the parameter description.
	Description string `json:"description,omitempty"`

	// Tags are the parameter tags.
	Tags map[string]string `json:"tags,omitempty"`

	// IsEncrypted indicates if the value is encrypted.
	IsEncrypted bool `json:"is_encrypted"`

	// KMSKeyID is the encryption key (if encrypted).
	KMSKeyID string `json:"kms_key_id,omitempty"`

	// ExpiresAt is when the parameter expires (nil = no expiration).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// AuditEnabled indicates if audit logging is enabled for this parameter.
	AuditEnabled bool `json:"audit_enabled"`

	// CreatedAt is when the parameter was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the parameter was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// CreatedBy is the user who created the parameter.
	CreatedBy string `json:"created_by,omitempty"`

	// UpdatedBy is the user who last modified the parameter.
	UpdatedBy string `json:"updated_by,omitempty"`
}
