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

package parameterstore

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.ParameterStore"
)

// Client provides parameter store operations.
type Client interface {
	// GetParameter retrieves a single parameter by path.
	GetParameter(ctx context.Context, req *GetParameterRequest) (*GetParameterResponse, error)

	// SetParameter creates or updates a parameter.
	SetParameter(ctx context.Context, req *SetParameterRequest) (*SetParameterResponse, error)

	// DeleteParameter removes a parameter by path.
	DeleteParameter(ctx context.Context, req *DeleteParameterRequest) (*DeleteParameterResponse, error)

	// GetParameters retrieves multiple parameters by paths (batch get).
	GetParameters(ctx context.Context, req *GetParametersRequest) (*GetParametersResponse, error)

	// SetParameters creates or updates multiple parameters (batch set).
	SetParameters(ctx context.Context, req *SetParametersRequest) (*SetParametersResponse, error)

	// DeleteParameters removes multiple parameters (batch delete).
	DeleteParameters(ctx context.Context, req *DeleteParametersRequest) (*DeleteParametersResponse, error)

	// ListParameters returns parameters matching a path prefix.
	ListParameters(ctx context.Context, req *ListParametersRequest) (*ListParametersResponse, error)

	// SearchByTags finds parameters matching tag key-value pairs.
	SearchByTags(ctx context.Context, req *SearchByTagsRequest) (*SearchByTagsResponse, error)
}

// GetParameterRequest is a request to retrieve a parameter by path.
type GetParameterRequest struct {
	// Path is the parameter path (e.g., "/app/database/host").
	Path string

	// WithDecryption decrypts the value if encrypted (default: true).
	WithDecryption bool
}

// GetParameterResponse is the response from a GetParameter operation.
type GetParameterResponse struct {
	// Parameter is the retrieved parameter.
	Parameter *ParameterValue

	// Timestamp is when the response was generated.
	Timestamp time.Time
}

// SetParameterRequest is a request to create or update a parameter.
type SetParameterRequest struct {
	// Path is the parameter path (required).
	Path string

	// Value is the parameter value (required).
	Value []byte

	// Type specifies the value type (default: string).
	Type string

	// Description is optional documentation.
	Description string

	// KMSKeyID enables encryption with the specified key.
	KMSKeyID string

	// Tags are key-value labels.
	Tags map[string]string

	// TTLSeconds sets the time-to-live in seconds (0 = no expiration).
	TTLSeconds int64

	// AuditEnabled enables audit logging for this parameter.
	AuditEnabled bool

	// Overwrite allows updating existing parameters (default: true).
	Overwrite bool
}

// SetParameterResponse is the response from a SetParameter operation.
type SetParameterResponse struct {
	// Path is the parameter path.
	Path string

	// Created indicates if a new parameter was created.
	Created bool

	// Timestamp is when the operation completed.
	Timestamp time.Time
}

// DeleteParameterRequest is a request to delete a parameter by path.
type DeleteParameterRequest struct {
	// Path is the parameter path to delete.
	Path string
}

// DeleteParameterResponse is the response from a DeleteParameter operation.
type DeleteParameterResponse struct {
	// Path is the deleted parameter path.
	Path string

	// Deleted indicates if deletion succeeded.
	Deleted bool

	// Timestamp is when deletion occurred.
	Timestamp time.Time
}

// GetParametersRequest is a request for batch parameter retrieval.
type GetParametersRequest struct {
	// Paths are the parameter paths to retrieve.
	Paths []string

	// WithDecryption decrypts values if encrypted (default: true).
	WithDecryption bool
}

// GetParametersResponse is the response from a GetParameters operation.
type GetParametersResponse struct {
	// Parameters contains successfully retrieved parameters.
	Parameters []*ParameterValue

	// Errors contains per-path errors for failed retrievals.
	Errors map[string]string

	// Timestamp is when the response was generated.
	Timestamp time.Time
}

// SetParametersRequest is a request for batch parameter creation/update.
type SetParametersRequest struct {
	// Parameters are the parameters to set.
	Parameters []*SetParameterRequest
}

// SetParametersResponse is the response from a SetParameters operation.
type SetParametersResponse struct {
	// Results contains per-path results.
	Results []*SetParameterResult

	// Timestamp is when the operation completed.
	Timestamp time.Time
}

// SetParameterResult contains the result of setting a single parameter.
type SetParameterResult struct {
	// Path is the parameter path.
	Path string

	// Created indicates if a new parameter was created.
	Created bool

	// Success indicates if the operation succeeded.
	Success bool

	// Error contains the error message if failed.
	Error string
}

// DeleteParametersRequest is a request for batch parameter deletion.
type DeleteParametersRequest struct {
	// Paths are the parameter paths to delete.
	Paths []string
}

// DeleteParametersResponse is the response from a DeleteParameters operation.
type DeleteParametersResponse struct {
	// Results contains per-path deletion results.
	Results []*DeleteParameterResult

	// DeletedCount is the number of parameters successfully deleted.
	DeletedCount int

	// Timestamp is when the operation completed.
	Timestamp time.Time
}

// DeleteParameterResult contains the result of deleting a single parameter.
type DeleteParameterResult struct {
	// Path is the parameter path.
	Path string

	// Deleted indicates if deletion succeeded.
	Deleted bool

	// Error contains the error message if failed.
	Error string
}

// ListParametersRequest is a request to list parameters by prefix.
type ListParametersRequest struct {
	// Prefix filters by path prefix (e.g., "/app/" returns all under /app).
	Prefix string

	// Recursive includes nested paths (default: true).
	Recursive bool

	// WithDecryption decrypts values if encrypted (default: false for listing).
	WithDecryption bool

	// WithValues includes parameter values (default: false for metadata only).
	WithValues bool

	// Limit is the maximum number of results (default: 100).
	Limit int

	// Cursor is the pagination cursor.
	Cursor string
}

// ListParametersResponse is the response from a ListParameters operation.
type ListParametersResponse struct {
	// Parameters contains the listed parameters.
	Parameters []*ParameterValue

	// NextCursor is the cursor for the next page.
	NextCursor string

	// HasMore indicates if there are more results.
	HasMore bool

	// TotalCount is the total number of matching parameters (if available).
	TotalCount int64

	// Timestamp is when the response was generated.
	Timestamp time.Time
}

// SearchByTagsRequest is a request to search parameters by tags.
type SearchByTagsRequest struct {
	// Tags are required tag key-value pairs (ANDed together).
	Tags map[string]string

	// Prefix optionally filters by path prefix as well.
	Prefix string

	// WithValues includes parameter values (default: false).
	WithValues bool

	// WithDecryption decrypts values if encrypted (default: false).
	WithDecryption bool

	// Limit is the maximum number of results (default: 100).
	Limit int

	// Cursor is the pagination cursor.
	Cursor string
}

// SearchByTagsResponse is the response from a SearchByTags operation.
type SearchByTagsResponse struct {
	// Parameters contains matching parameters.
	Parameters []*ParameterValue

	// NextCursor is the cursor for the next page.
	NextCursor string

	// HasMore indicates if there are more results.
	HasMore bool

	// Timestamp is when the response was generated.
	Timestamp time.Time
}

// ParameterValue represents a parameter with its value and metadata.
type ParameterValue struct {
	// Path is the parameter path.
	Path string

	// Value is the parameter value (decrypted if requested).
	Value []byte

	// Type is the value type (string, number, json, binary).
	Type string

	// Description is the parameter description.
	Description string

	// Tags are the parameter tags.
	Tags map[string]string

	// IsEncrypted indicates if the value is encrypted.
	IsEncrypted bool

	// KMSKeyID is the encryption key (if encrypted).
	KMSKeyID string

	// ExpiresAt is when the parameter expires (nil = no expiration).
	ExpiresAt *time.Time

	// AuditEnabled indicates if audit logging is enabled for this parameter.
	AuditEnabled bool

	// CreatedAt is when the parameter was created.
	CreatedAt time.Time

	// UpdatedAt is when the parameter was last modified.
	UpdatedAt time.Time

	// CreatedBy is the user who created the parameter.
	CreatedBy string

	// UpdatedBy is the user who last modified the parameter.
	UpdatedBy string
}

// client is the concrete implementation of Client.
type client struct {
	transport transport.Transport
}

// NewClient creates a new ParameterStore client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// GetParameter retrieves a single parameter by path.
func (c *client) GetParameter(ctx context.Context, req *GetParameterRequest) (*GetParameterResponse, error) {
	if err := validateGetParameterRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetParameter", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("parameterstore.GetParameter", "nil response from server")
	}

	getResp, ok := resp.(*GetParameterResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.GetParameter", "invalid response type")
	}

	return getResp, nil
}

// SetParameter creates or updates a parameter.
func (c *client) SetParameter(ctx context.Context, req *SetParameterRequest) (*SetParameterResponse, error) {
	if err := validateSetParameterRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetParameter", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("parameterstore.SetParameter", "nil response from server")
	}

	setResp, ok := resp.(*SetParameterResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.SetParameter", "invalid response type")
	}

	return setResp, nil
}

// DeleteParameter removes a parameter by path.
func (c *client) DeleteParameter(ctx context.Context, req *DeleteParameterRequest) (*DeleteParameterResponse, error) {
	if err := validateDeleteParameterRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteParameter", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("parameterstore.DeleteParameter", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteParameterResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.DeleteParameter", "invalid response type")
	}

	return deleteResp, nil
}

// GetParameters retrieves multiple parameters by paths (batch get).
func (c *client) GetParameters(ctx context.Context, req *GetParametersRequest) (*GetParametersResponse, error) {
	if err := validateGetParametersRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetParameters", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &GetParametersResponse{
			Parameters: []*ParameterValue{},
			Errors:     make(map[string]string),
			Timestamp:  time.Now(),
		}, nil
	}

	getResp, ok := resp.(*GetParametersResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.GetParameters", "invalid response type")
	}

	return getResp, nil
}

// SetParameters creates or updates multiple parameters (batch set).
func (c *client) SetParameters(ctx context.Context, req *SetParametersRequest) (*SetParametersResponse, error) {
	if err := validateSetParametersRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetParameters", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("parameterstore.SetParameters", "nil response from server")
	}

	setResp, ok := resp.(*SetParametersResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.SetParameters", "invalid response type")
	}

	return setResp, nil
}

// DeleteParameters removes multiple parameters (batch delete).
func (c *client) DeleteParameters(ctx context.Context, req *DeleteParametersRequest) (*DeleteParametersResponse, error) {
	if err := validateDeleteParametersRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteParameters", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("parameterstore.DeleteParameters", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteParametersResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.DeleteParameters", "invalid response type")
	}

	return deleteResp, nil
}

// ListParameters returns parameters matching a path prefix.
func (c *client) ListParameters(ctx context.Context, req *ListParametersRequest) (*ListParametersResponse, error) {
	if err := validateListParametersRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListParameters", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListParametersResponse{
			Parameters: []*ParameterValue{},
			Timestamp:  time.Now(),
		}, nil
	}

	listResp, ok := resp.(*ListParametersResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.ListParameters", "invalid response type")
	}

	return listResp, nil
}

// SearchByTags finds parameters matching tag key-value pairs.
func (c *client) SearchByTags(ctx context.Context, req *SearchByTagsRequest) (*SearchByTagsResponse, error) {
	if err := validateSearchByTagsRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "SearchByTags", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &SearchByTagsResponse{
			Parameters: []*ParameterValue{},
			Timestamp:  time.Now(),
		}, nil
	}

	searchResp, ok := resp.(*SearchByTagsResponse)
	if !ok {
		return nil, errors.NewInternal("parameterstore.SearchByTags", "invalid response type")
	}

	return searchResp, nil
}

// Validation functions

func validateGetParameterRequest(req *GetParameterRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.GetParameter", "request is required")
	}
	if req.Path == "" {
		return errors.NewInvalidInput("parameterstore.GetParameter", "path is required")
	}
	return nil
}

func validateSetParameterRequest(req *SetParameterRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.SetParameter", "request is required")
	}
	if req.Path == "" {
		return errors.NewInvalidInput("parameterstore.SetParameter", "path is required")
	}
	if len(req.Value) == 0 {
		return errors.NewInvalidInput("parameterstore.SetParameter", "value is required")
	}
	return nil
}

func validateDeleteParameterRequest(req *DeleteParameterRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.DeleteParameter", "request is required")
	}
	if req.Path == "" {
		return errors.NewInvalidInput("parameterstore.DeleteParameter", "path is required")
	}
	return nil
}

func validateGetParametersRequest(req *GetParametersRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.GetParameters", "request is required")
	}
	if len(req.Paths) == 0 {
		return errors.NewInvalidInput("parameterstore.GetParameters", "paths is required")
	}
	return nil
}

func validateSetParametersRequest(req *SetParametersRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.SetParameters", "request is required")
	}
	if len(req.Parameters) == 0 {
		return errors.NewInvalidInput("parameterstore.SetParameters", "parameters is required")
	}
	for i, param := range req.Parameters {
		if param == nil {
			return errors.NewInvalidInput("parameterstore.SetParameters", "parameter at index is nil").
				WithContext("index", i)
		}
		if param.Path == "" {
			return errors.NewInvalidInput("parameterstore.SetParameters", "path is required for parameter").
				WithContext("index", i)
		}
		if len(param.Value) == 0 {
			return errors.NewInvalidInput("parameterstore.SetParameters", "value is required for parameter").
				WithContext("index", i).WithContext("path", param.Path)
		}
	}
	return nil
}

func validateDeleteParametersRequest(req *DeleteParametersRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.DeleteParameters", "request is required")
	}
	if len(req.Paths) == 0 {
		return errors.NewInvalidInput("parameterstore.DeleteParameters", "paths is required")
	}
	return nil
}

func validateListParametersRequest(req *ListParametersRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.ListParameters", "request is required")
	}
	// Prefix is optional - empty prefix lists all parameters
	return nil
}

func validateSearchByTagsRequest(req *SearchByTagsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("parameterstore.SearchByTags", "request is required")
	}
	if len(req.Tags) == 0 {
		return errors.NewInvalidInput("parameterstore.SearchByTags", "tags is required")
	}
	return nil
}
