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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	tenantServiceName = "quicbaas.IAM.Tenant"
)

// Tenant provisioning types. A tenant's isolation mode and its engine-level
// limits are decided at creation time and cannot be inferred afterwards, so
// the create request has to be able to name them. They are aliases of the
// engine's own types rather than parallel definitions: a divergent copy here
// would drift from what the engine actually enforces.

// TenantIsolationType identifies a tenant's engine-side isolation mode.
type TenantIsolationType = qrdbsdk.TenantIsolationType

// Engine isolation modes, ordered from weakest to strongest separation. The
// zero value defers to the engine default (prefix), which is why every
// existing caller that omits IsolationType keeps its current behavior.
const (
	// TenantIsolationPrefix shares the system barrier and separates tenant
	// data by logical key prefixing.
	TenantIsolationPrefix = qrdbsdk.TenantIsolationPrefix

	// TenantIsolationBarrier gives the tenant its own DEK and a dedicated
	// namespace under the system data root.
	TenantIsolationBarrier = qrdbsdk.TenantIsolationBarrier

	// TenantIsolationVolume gives the tenant a dedicated filesystem DataRoot.
	TenantIsolationVolume = qrdbsdk.TenantIsolationVolume

	// TenantIsolationRaft gives the tenant a dedicated Raft group per database.
	TenantIsolationRaft = qrdbsdk.TenantIsolationRaft
)

// EngineQuotas are the engine-level numeric resource limits applied to a
// tenant at creation. They are distinct from the BaaS-layer IAM quotas carried
// in Settings.
//
// This is a limits-only view of the engine's Quotas rather than an alias of
// it. The engine's struct also carries the Current* usage counters it
// maintains as consumption changes; those are outputs, and the gRPC
// CreateTenantRequest models limits only. Aliasing the full struct would make
// a counter part of the accepted create-time shape on the JSON transports
// while gRPC silently dropped it — the same request meaning different things
// per transport. Naming only the limits makes the accepted field set identical
// everywhere by construction: a Go caller cannot set a counter, and strict
// decoding rejects one on the wire. Use UpdateQuotas to correct a counter.
//
// A value of -1 means unlimited; 0 means use the engine's system default.
type EngineQuotas struct {
	// MaxDatabases caps the number of databases the tenant may create.
	MaxDatabases int64 `json:"max_databases"`

	// MaxRaftGroups caps the number of dedicated Raft consensus groups.
	MaxRaftGroups int64 `json:"max_raft_groups"`

	// MaxReplicasPerGroup caps the replication factor of any single group.
	MaxReplicasPerGroup int64 `json:"max_replicas_per_group"`

	// MaxTotalReplicas caps total replicas across all dedicated-raft groups.
	MaxTotalReplicas int64 `json:"max_total_replicas"`

	// MaxCollections caps the number of vector/time-series collections.
	MaxCollections int64 `json:"max_collections"`

	// MaxKeys caps the total number of keys across all databases.
	MaxKeys int64 `json:"max_keys"`

	// MaxStorageBytes caps total on-disk storage consumption in bytes.
	MaxStorageBytes int64 `json:"max_storage_bytes"`

	// MaxDatabaseSizeBytes caps the on-disk size of any single database.
	MaxDatabaseSizeBytes int64 `json:"max_database_size_bytes"`

	// MaxUsers caps the number of IAM users the tenant may create.
	MaxUsers int64 `json:"max_users"`

	// MaxRoles caps the number of IAM roles the tenant may create.
	MaxRoles int64 `json:"max_roles"`

	// MaxCertificates caps the number of TLS certificates the tenant may hold.
	MaxCertificates int64 `json:"max_certificates"`

	// MaxBackups caps the number of database backups the tenant may retain.
	MaxBackups int64 `json:"max_backups"`

	// MaxVectorDims caps the maximum vector dimensionality the tenant may use.
	MaxVectorDims int64 `json:"max_vector_dims"`

	// MaxTSMetrics caps the number of time-series metrics the tenant may track.
	MaxTSMetrics int64 `json:"max_ts_metrics"`

	// MaxDedicatedWALGroups caps dedicated-raft groups with a per-tenant WAL.
	MaxDedicatedWALGroups int64 `json:"max_dedicated_wal_groups"`

	// SystemBarrierQuota governs the tenant's use of the shared system barrier
	// (bytes): 0 = unavailable, -1 = unlimited, >0 = capped at that many bytes.
	SystemBarrierQuota int64 `json:"system_barrier_quota"`
}

// Quotas converts the create-time limits into the engine's own Quotas struct,
// leaving every usage counter at zero — the engine owns those and fills them
// in as consumption changes.
func (q *EngineQuotas) Quotas() *qrdbsdk.Quotas {
	if q == nil {
		return nil
	}
	return &qrdbsdk.Quotas{
		MaxDatabases:          q.MaxDatabases,
		MaxRaftGroups:         q.MaxRaftGroups,
		MaxReplicasPerGroup:   q.MaxReplicasPerGroup,
		MaxTotalReplicas:      q.MaxTotalReplicas,
		MaxCollections:        q.MaxCollections,
		MaxKeys:               q.MaxKeys,
		MaxStorageBytes:       q.MaxStorageBytes,
		MaxDatabaseSizeBytes:  q.MaxDatabaseSizeBytes,
		MaxUsers:              q.MaxUsers,
		MaxRoles:              q.MaxRoles,
		MaxCertificates:       q.MaxCertificates,
		MaxBackups:            q.MaxBackups,
		MaxVectorDims:         q.MaxVectorDims,
		MaxTSMetrics:          q.MaxTSMetrics,
		MaxDedicatedWALGroups: q.MaxDedicatedWALGroups,
		SystemBarrierQuota:    q.SystemBarrierQuota,
	}
}

// EngineEntitlements are the engine-level feature entitlements (isolation
// tiers, dedicated-storage, WAL features) granted to a tenant.
type EngineEntitlements = qrdbsdk.Entitlements

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

	// Provision creates a tenant together with its built-in roles and its
	// first admin. It is the onboarding path when public registration is off,
	// and needs the tenants:create grant.
	Provision(ctx context.Context, req *ProvisionTenantRequest) (*ProvisionTenantResponse, error)
}

// TenantAdminEnrollment selects how a provisioned tenant's first admin gets
// its credential.
type TenantAdminEnrollment string

const (
	// TenantAdminEnrollCert signs the admin's own CSR and binds the issued
	// certificate. The private key never leaves the admin. This is the
	// default.
	TenantAdminEnrollCert TenantAdminEnrollment = "cert"

	// TenantAdminEnrollInvite creates the admin pending and returns a
	// one-time invite token the admin redeems with a PIV certificate.
	TenantAdminEnrollInvite TenantAdminEnrollment = "invite"
)

// ProvisionTenantRequest is a request to provision a tenant and its admin.
type ProvisionTenantRequest struct {
	// TenantID is the new tenant's id. Required.
	TenantID string `json:"tenant_id"`

	// Name is the human-readable tenant name. Empty defaults to TenantID.
	Name string `json:"name,omitempty"`

	// IsolationType selects the engine isolation strategy. Empty defaults to
	// a per-tenant barrier, unlike Create whose default is prefix.
	IsolationType TenantIsolationType `json:"isolation_type,omitempty"`

	// AdminUsername is the tenant admin's login name. Empty defaults to
	// "admin".
	AdminUsername string `json:"admin_username,omitempty"`

	// AdminEnrollment selects the admin credential path. Empty defaults to
	// TenantAdminEnrollCert.
	AdminEnrollment TenantAdminEnrollment `json:"admin_enrollment,omitempty"`

	// AdminCSRPEM is the admin's PEM-encoded CSR. Required in cert mode and
	// refused in invite mode.
	AdminCSRPEM string `json:"admin_csr_pem,omitempty"`

	// InviteExpiryHours bounds the invite's lifetime in invite mode. Zero
	// selects the server's default.
	InviteExpiryHours int `json:"invite_expiry_hours,omitempty"`
}

// ProvisionTenantResponse describes a provisioned tenant and its admin.
type ProvisionTenantResponse struct {
	// TenantID is the provisioned tenant.
	TenantID string `json:"tenant_id"`

	// AdminUserID is the tenant admin's user id (decimal).
	AdminUserID string `json:"admin_user_id"`

	// AdminUsername is the tenant admin's login name.
	AdminUsername string `json:"admin_username"`

	// AdminRoleID is the tenant-scoped Admin role the admin is bound to.
	AdminRoleID string `json:"admin_role_id"`

	// AdminPending reports that the admin awaits invite redemption.
	AdminPending bool `json:"admin_pending"`

	// AdminCertPEM is the issued admin certificate (cert mode only).
	AdminCertPEM string `json:"admin_cert_pem,omitempty"`

	// AdminCertThumbprint is the bound SPKI SHA-256 thumbprint (cert mode
	// only).
	AdminCertThumbprint string `json:"admin_cert_thumbprint,omitempty"`

	// InviteToken is the one-time activation token (invite mode only).
	InviteToken string `json:"invite_token,omitempty"`

	// InviteExpiresAt is when InviteToken stops being redeemable (invite
	// mode only).
	InviteExpiresAt *time.Time `json:"invite_expires_at,omitempty"`

	// Timestamp is when provisioning completed.
	Timestamp time.Time `json:"timestamp"`
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

	// IsolationType selects the engine isolation strategy for the tenant's
	// data. It is only settable here: the engine binds a tenant's isolation
	// at creation and a later update cannot move data between strategies.
	// Empty defers to the engine default (prefix), so callers that never set
	// it are unaffected. The reserved system tenant must remain prefix.
	IsolationType TenantIsolationType `json:"isolation_type,omitempty"`

	// EngineQuotas overrides the engine-level resource limits applied to the
	// tenant at creation. Nil leaves the engine's configured defaults in
	// place; a non-nil value is applied immediately after the tenant exists,
	// so a provisioning caller does not race a newly created tenant against
	// a follow-up quota update.
	EngineQuotas *EngineQuotas `json:"engine_quotas,omitempty"`

	// EngineEntitlements overrides the engine-level feature entitlements for
	// the tenant at creation. Nil leaves the engine's configured defaults in
	// place.
	EngineEntitlements *EngineEntitlements `json:"engine_entitlements,omitempty"`
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

// Provision creates a tenant together with its built-in roles and first admin.
func (c *tenantClient) Provision(ctx context.Context, req *ProvisionTenantRequest) (*ProvisionTenantResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("iam.tenant.Provision", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("iam.tenant.Provision", "tenant ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.tenant.Provision", "context is required")
	}

	resp, err := c.transport.Request(ctx, tenantServiceName, "Provision", req)
	if err != nil {
		return nil, err
	}
	if typed, ok := resp.(*ProvisionTenantResponse); ok {
		return typed, nil
	}
	return convertMapResponse[ProvisionTenantResponse](resp, "iam.tenant.Provision")
}

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
