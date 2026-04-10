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

package iam

import (
	"context"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	tenantServiceName = "quicbaas.IAM.Tenant"
)

// TenantClient provides tenant management operations.
type TenantClient interface {
	// Create creates a new tenant.
	Create(ctx context.Context, req *CreateTenantRequest) (*Tenant, error)

	// Get retrieves a tenant by ID.
	Get(ctx context.Context, tenantID string) (*Tenant, error)

	// Update updates a tenant.
	Update(ctx context.Context, tenantID string, req *UpdateTenantRequest) (*Tenant, error)

	// Delete deletes a tenant.
	Delete(ctx context.Context, tenantID string) error

	// List lists tenants with optional filtering.
	List(ctx context.Context, req *ListTenantsRequest) (*ListTenantsResponse, error)
}

// CreateTenantRequest is a request to create a tenant.
type CreateTenantRequest struct {
	// ID is the unique identifier for the tenant.
	ID string `json:"id,omitempty"`

	// Name is the unique name for the tenant.
	Name string `json:"name"`

	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Settings contains tenant-specific settings.
	Settings map[string]string `json:"settings,omitempty"`

	// Metadata contains additional metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdateTenantRequest is a request to update a tenant.
type UpdateTenantRequest struct {
	// DisplayName is the human-readable display name.
	DisplayName string `json:"display_name,omitempty"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Settings contains tenant-specific settings.
	Settings map[string]string `json:"settings,omitempty"`

	// Metadata contains additional metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Status is the tenant status (for suspend/unsuspend).
	Status *TenantStatus `json:"status,omitempty"`
}

// ListTenantsRequest is a request to list tenants.
type ListTenantsRequest struct {
	// PageSize is the maximum number of tenants to return.
	PageSize int `json:"page_size,omitempty"`

	// PageToken is the token for the next page.
	PageToken string `json:"page_token,omitempty"`

	// Filter is an optional filter expression.
	Filter string `json:"filter,omitempty"`
}

// ListTenantsResponse is the response from listing tenants.
type ListTenantsResponse struct {
	// Tenants is the list of tenants.
	Tenants []Tenant `json:"tenants"`

	// Pagination contains pagination information.
	Pagination PaginationInfo `json:"pagination"`
}

// Internal request/response types for transport.

type getTenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type deleteTenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type deleteTenantResponse struct{}

type updateTenantRequestInternal struct {
	TenantID string               `json:"tenant_id"`
	Request  *UpdateTenantRequest `json:"request"`
}

// tenantClient implements the TenantClient interface.
type tenantClient struct {
	transport transport.Transport
}

// NewTenantClient creates a new tenant client.
func NewTenantClient(t transport.Transport) TenantClient {
	if t == nil {
		return nil
	}
	return &tenantClient{
		transport: t,
	}
}

// Create creates a new tenant.
func (c *tenantClient) Create(ctx context.Context, req *CreateTenantRequest) (*Tenant, error) {
	if err := validateCreateTenantRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.tenant.Create", "context is required")
	}

	resp, err := c.transport.Request(ctx, tenantServiceName, "Create", req)
	if err != nil {
		return nil, err
	}

	return parseTenantResponse(resp)
}

// Get retrieves a tenant by ID.
func (c *tenantClient) Get(ctx context.Context, tenantID string) (*Tenant, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.tenant.Get", "tenant ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.tenant.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, tenantServiceName, "Get", &getTenantRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}

	return parseTenantResponse(resp)
}

// Update updates a tenant.
func (c *tenantClient) Update(ctx context.Context, tenantID string, req *UpdateTenantRequest) (*Tenant, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.tenant.Update", "tenant ID is required")
	}

	if req == nil {
		return nil, errors.NewInvalidInput("iam.tenant.Update", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.tenant.Update", "context is required")
	}

	resp, err := c.transport.Request(ctx, tenantServiceName, "Update", &updateTenantRequestInternal{
		TenantID: tenantID,
		Request:  req,
	})
	if err != nil {
		return nil, err
	}

	return parseTenantResponse(resp)
}

// Delete deletes a tenant.
func (c *tenantClient) Delete(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.tenant.Delete", "tenant ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.tenant.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, tenantServiceName, "Delete", &deleteTenantRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return err
	}

	// Accept any response - deletion succeeded if no error was returned
	_ = resp
	return nil
}

// List lists tenants with optional filtering.
func (c *tenantClient) List(ctx context.Context, req *ListTenantsRequest) (*ListTenantsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.tenant.List", "context is required")
	}

	if req == nil {
		req = &ListTenantsRequest{}
	}

	resp, err := c.transport.Request(ctx, tenantServiceName, "List", req)
	if err != nil {
		return nil, err
	}

	return parseListTenantsResponse(resp)
}

// Validation functions.

func validateCreateTenantRequest(req *CreateTenantRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.tenant.Create", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("iam.tenant.Create", "name is required")
	}
	return nil
}

// parseListTenantsResponse parses a list tenants response from the transport.
func parseListTenantsResponse(resp interface{}) (*ListTenantsResponse, error) {
	if resp == nil {
		return &ListTenantsResponse{Tenants: []Tenant{}}, nil
	}

	// If already a *ListTenantsResponse, return it directly
	if listResp, ok := resp.(*ListTenantsResponse); ok {
		return listResp, nil
	}

	// Handle map response (from gRPC or REST)
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.tenant.List", "invalid response type")
	}

	listResp := &ListTenantsResponse{
		Tenants: []Tenant{},
	}

	// Parse tenants array
	if tenantsRaw, ok := respMap["tenants"]; ok {
		switch tenants := tenantsRaw.(type) {
		case []interface{}:
			for _, t := range tenants {
				if tenantMap, ok := t.(map[string]interface{}); ok {
					tenant, err := parseTenantResponse(tenantMap)
					if err == nil && tenant != nil {
						listResp.Tenants = append(listResp.Tenants, *tenant)
					}
				}
			}
		case []*Tenant:
			for _, t := range tenants {
				if t != nil {
					listResp.Tenants = append(listResp.Tenants, *t)
				}
			}
		}
	}

	// Parse pagination
	if pagination, ok := respMap["pagination"].(map[string]interface{}); ok {
		if v, ok := pagination["total_count"].(float64); ok {
			listResp.Pagination.TotalCount = int(v)
		}
		if v, ok := pagination["page_size"].(float64); ok {
			listResp.Pagination.PageSize = int(v)
		}
		if v, ok := pagination["next_page_token"].(string); ok {
			listResp.Pagination.NextPageToken = v
		}
	}

	return listResp, nil
}

// parseTenantResponse parses a tenant response from the transport.
func parseTenantResponse(resp interface{}) (*Tenant, error) {
	if resp == nil {
		return nil, errors.NewInternal("iam.tenant", "nil response from server")
	}

	// If already a *Tenant, return it directly
	if tenant, ok := resp.(*Tenant); ok {
		return tenant, nil
	}

	// Handle map response (from gRPC or REST)
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.tenant", "invalid response type")
	}

	// Check for nested tenant field (server response format: {tenant: {...}, timestamp: "..."})
	if tenantData, ok := respMap["tenant"].(map[string]interface{}); ok {
		respMap = tenantData
	}

	tenant := &Tenant{}

	if v, ok := respMap["id"].(string); ok {
		tenant.ID = v
	}
	if v, ok := respMap["name"].(string); ok {
		tenant.Name = v
	}
	if v, ok := respMap["display_name"].(string); ok {
		tenant.DisplayName = v
	} else if v, ok := respMap["Name"].(string); ok {
		// Fall back to capitalized Name for REST compatibility
		tenant.DisplayName = v
	}
	// If DisplayName is still empty, use Name as fallback.
	if tenant.DisplayName == "" && tenant.Name != "" {
		tenant.DisplayName = tenant.Name
	}
	if v, ok := respMap["description"].(string); ok {
		tenant.Description = v
	}
	if v, ok := respMap["status"].(string); ok {
		tenant.Status = parseTenantStatus(v)
	}
	if v, ok := respMap["settings"].(map[string]string); ok {
		tenant.Settings = v
	} else if v, ok := respMap["settings"].(map[string]interface{}); ok {
		settings := make(map[string]string)
		for k, val := range v {
			switch tv := val.(type) {
			case string:
				settings[k] = tv
			case float64:
				settings[k] = fmt.Sprintf("%g", tv)
			case bool:
				settings[k] = fmt.Sprintf("%t", tv)
			default:
				settings[k] = fmt.Sprintf("%v", tv)
			}
		}
		tenant.Settings = settings
		tenant.Settings = settings
	}
	if v, ok := respMap["metadata"].(map[string]string); ok {
		tenant.Metadata = v
	}

	// Parse timestamps
	if v, ok := respMap["created_at"].(string); ok {
		if t, err := parseTime(v); err == nil {
			tenant.CreatedAt = t
		}
	} else if v, ok := respMap["created_at"].(time.Time); ok {
		tenant.CreatedAt = v
	}
	if v, ok := respMap["updated_at"].(string); ok {
		if t, err := parseTime(v); err == nil {
			tenant.UpdatedAt = t
		}
	} else if v, ok := respMap["updated_at"].(time.Time); ok {
		tenant.UpdatedAt = v
	}

	// Parse quotas
	if quotas, ok := respMap["quotas"].(map[string]interface{}); ok {
		tenant.Quotas = &TenantQuotas{}
		if v, ok := quotas["max_users"].(float64); ok {
			tenant.Quotas.MaxUsers = int(v)
		}
		if v, ok := quotas["max_groups"].(float64); ok {
			tenant.Quotas.MaxGroups = int(v)
		}
		if v, ok := quotas["max_roles"].(float64); ok {
			tenant.Quotas.MaxRoles = int(v)
		}
		if v, ok := quotas["max_policies"].(float64); ok {
			tenant.Quotas.MaxPolicies = int(v)
		}
		if v, ok := quotas["max_applications"].(float64); ok {
			tenant.Quotas.MaxApplications = int(v)
		}
	}

	return tenant, nil
}

// parseTenantStatus parses a tenant status string.
func parseTenantStatus(s string) TenantStatus {
	switch s {
	case "active":
		return TenantStatusActive
	case "suspended":
		return TenantStatusSuspended
	case "pending":
		return TenantStatusUnspecified
	case "deleted":
		return TenantStatusDeleted
	default:
		return TenantStatusActive
	}
}

// parseTime parses a time string.
func parseTime(s string) (time.Time, error) {
	// Try RFC3339 first
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	// Try RFC3339Nano
	t, err = time.Parse(time.RFC3339Nano, s)
	if err == nil {
		return t, nil
	}
	return time.Time{}, err
}
