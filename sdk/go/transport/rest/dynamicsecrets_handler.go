// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// dynamicsecrets_handler.go provides typed REST routing for the
// quicbaas.DynamicSecrets service (Security().DynamicSecrets()).
//
// Without this handler the generic dispatch path mapped every method to
// /api/v1/dynamicsecrets/<method>, which the server does not mount — the real
// routes live under /api/v1/secrets/dynamic/*. This handler substitutes the
// role name / lease ID path parameters and decodes the server's REST wire
// shapes into the leaf sdk/go/client/dynamicsecrets response types the SDK
// client type-asserts on.

package rest

import (
	"context"
	"net/http"
	"net/url"
	"time"

	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// dynamicSecretsServicePrefix is the gRPC-style service identifier the SDK
// uses to route DynamicSecrets requests to this handler.
const dynamicSecretsServicePrefix = "quicbaas.DynamicSecrets"

// dynamicSecretsBasePath is the server mount point for the dynamic secrets
// engine.
const dynamicSecretsBasePath = "/api/v1/secrets/dynamic"

// isDynamicSecretsService reports whether the supplied service name resolves
// to the DynamicSecrets service.
func isDynamicSecretsService(service string) bool {
	return trimRESTServicePrefix(service) == "DynamicSecrets"
}

// =============================================================================
// Server wire shapes
// =============================================================================
//
// These mirror the JSON the quicbaasd REST handlers emit
// (pkg/api/rest/handlers_dynamic_secrets.go). They are declared here rather
// than imported so the SDK transport keeps no dependency on the server
// package.

// dsRoleWire is the server's role representation.
type dsRoleWire struct {
	Name          string            `json:"name"`
	GeneratorType string            `json:"generator_type"`
	Config        map[string]string `json:"config,omitempty"`
	DefaultTTL    string            `json:"default_ttl"`
	MaxTTL        string            `json:"max_ttl"`
	CreatedAt     int64             `json:"created_at,omitempty"`
	Timestamp     int64             `json:"timestamp"`
}

// dsCreateRoleWire is the server's create-role request body.
type dsCreateRoleWire struct {
	Name          string            `json:"name"`
	GeneratorType string            `json:"generator_type"`
	Config        map[string]string `json:"config,omitempty"`
	DefaultTTL    string            `json:"default_ttl"`
	MaxTTL        string            `json:"max_ttl"`
}

// dsListRolesWire is the server's list-roles response.
type dsListRolesWire struct {
	Roles []dsRoleWire `json:"roles"`
	Count int          `json:"count"`
}

// dsDeleteRoleWire is the server's delete-role response.
type dsDeleteRoleWire struct {
	Success   bool  `json:"success"`
	DeletedAt int64 `json:"deleted_at"`
}

// dsGenerateCredentialWire is the server's generate-credential request body.
type dsGenerateCredentialWire struct {
	TTL string `json:"ttl,omitempty"`
}

// dsCredentialWire is the server's generate-credential response.
type dsCredentialWire struct {
	CredentialID string            `json:"credential_id"`
	Username     string            `json:"username,omitempty"`
	Password     string            `json:"password,omitempty"`
	Secret       map[string]string `json:"secret,omitempty"`
	LeaseID      string            `json:"lease_id"`
	LeaseTTL     string            `json:"lease_ttl"`
	LeaseExpiry  int64             `json:"lease_expiry"`
	Renewable    bool              `json:"renewable"`
}

// dsRenewLeaseWire is the server's renew-lease request body.
type dsRenewLeaseWire struct {
	Increment string `json:"increment,omitempty"`
}

// dsRenewedLeaseWire is the server's renew-lease response.
type dsRenewedLeaseWire struct {
	LeaseID     string `json:"lease_id"`
	LeaseTTL    string `json:"lease_ttl"`
	LeaseExpiry int64  `json:"lease_expiry"`
	RenewCount  int    `json:"renew_count"`
	RenewedAt   int64  `json:"renewed_at"`
}

// dsRevokeLeaseWire is the server's revoke-lease response.
type dsRevokeLeaseWire struct {
	Success   bool  `json:"success"`
	RevokedAt int64 `json:"revoked_at"`
}

// dsLeaseWire is the server's lease representation in list responses.
type dsLeaseWire struct {
	LeaseID    string            `json:"lease_id"`
	Path       string            `json:"path"`
	TTL        string            `json:"ttl"`
	Expiry     int64             `json:"expiry"`
	Renewable  bool              `json:"renewable"`
	RenewCount int               `json:"renew_count"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// dsListLeasesWire is the server's list-leases response.
type dsListLeasesWire struct {
	Leases []dsLeaseWire `json:"leases"`
	Count  int           `json:"count"`
}

// =============================================================================
// Handler
// =============================================================================

// dynamicSecretsRESTHandler delegates DynamicSecrets service calls to the
// quicbaasd REST endpoints.
type dynamicSecretsRESTHandler struct {
	transport *Transport
}

// newDynamicSecretsRESTHandler constructs a handler bound to t.
func newDynamicSecretsRESTHandler(t *Transport) *dynamicSecretsRESTHandler {
	return &dynamicSecretsRESTHandler{transport: t}
}

// dynamicSecretsMethods maps SDK method names to their handler funcs for O(1)
// dispatch.
var dynamicSecretsMethods = map[string]func(*dynamicSecretsRESTHandler, context.Context, interface{}) (interface{}, error){
	"CreateRole":         (*dynamicSecretsRESTHandler).createRole,
	"GetRole":            (*dynamicSecretsRESTHandler).getRole,
	"ListRoles":          (*dynamicSecretsRESTHandler).listRoles,
	"DeleteRole":         (*dynamicSecretsRESTHandler).deleteRole,
	"GenerateCredential": (*dynamicSecretsRESTHandler).generateCredential,
	"RenewLease":         (*dynamicSecretsRESTHandler).renewLease,
	"RevokeLease":        (*dynamicSecretsRESTHandler).revokeLease,
	"ListLeases":         (*dynamicSecretsRESTHandler).listLeases,
}

// handleRequest dispatches a DynamicSecrets method to its REST handler.
func (h *dynamicSecretsRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := dynamicSecretsMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.dynamicsecrets."+method,
			"unsupported DynamicSecrets method")
	}
	return fn(h, ctx, req)
}

func (h *dynamicSecretsRESTHandler) createRole(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.CreateRoleRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.dynamicsecrets.CreateRole", "request is required")
	}
	body := &dsCreateRoleWire{
		Name:          r.Name,
		GeneratorType: r.GeneratorType,
		Config:        r.Config,
		DefaultTTL:    r.DefaultTTL.String(),
		MaxTTL:        r.MaxTTL.String(),
	}

	var wire dsRoleWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		dynamicSecretsBasePath+"/roles", body, &wire); err != nil {
		return nil, err
	}
	return &dsclient.CreateRoleResponse{
		Role:      dsRoleFromWire(&wire),
		CreatedAt: unixToTime(wire.CreatedAt),
	}, nil
}

func (h *dynamicSecretsRESTHandler) getRole(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.GetRoleRequest)
	if !ok || r == nil || r.Name == "" {
		return nil, errors.NewInvalidInput("rest.dynamicsecrets.GetRole", "role name is required")
	}

	var wire dsRoleWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		dynamicSecretsBasePath+"/roles/"+url.PathEscape(r.Name), nil, &wire); err != nil {
		return nil, err
	}
	return &dsclient.GetRoleResponse{Role: dsRoleFromWire(&wire)}, nil
}

func (h *dynamicSecretsRESTHandler) listRoles(ctx context.Context, req interface{}) (interface{}, error) {
	endpoint := dynamicSecretsBasePath + "/roles"
	if r, ok := req.(*dsclient.ListRolesRequest); ok && r != nil && r.Prefix != "" {
		endpoint += "?" + url.Values{"prefix": {r.Prefix}}.Encode()
	}

	var wire dsListRolesWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	roles := make([]*dsclient.Role, 0, len(wire.Roles))
	for i := range wire.Roles {
		roles = append(roles, dsRoleFromWire(&wire.Roles[i]))
	}
	return &dsclient.ListRolesResponse{Roles: roles, Count: wire.Count}, nil
}

func (h *dynamicSecretsRESTHandler) deleteRole(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.DeleteRoleRequest)
	if !ok || r == nil || r.Name == "" {
		return nil, errors.NewInvalidInput("rest.dynamicsecrets.DeleteRole", "role name is required")
	}

	var wire dsDeleteRoleWire
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete,
		dynamicSecretsBasePath+"/roles/"+url.PathEscape(r.Name), nil, &wire); err != nil {
		return nil, err
	}
	return &dsclient.DeleteRoleResponse{DeletedAt: unixToTime(wire.DeletedAt)}, nil
}

func (h *dynamicSecretsRESTHandler) generateCredential(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.GenerateCredentialRequest)
	if !ok || r == nil || r.RoleName == "" {
		return nil, errors.NewInvalidInput("rest.dynamicsecrets.GenerateCredential", "role name is required")
	}
	body := &dsGenerateCredentialWire{}
	if r.TTL > 0 {
		body.TTL = r.TTL.String()
	}

	var wire dsCredentialWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		dynamicSecretsBasePath+"/credentials/"+url.PathEscape(r.RoleName), body, &wire); err != nil {
		return nil, err
	}

	data := make(map[string]string, len(wire.Secret)+2)
	for k, v := range wire.Secret {
		data[k] = v
	}
	if wire.Username != "" {
		data["username"] = wire.Username
	}
	if wire.Password != "" {
		data["password"] = wire.Password
	}

	ttl := parseDurationOrZero(wire.LeaseTTL)
	expiry := unixToTime(wire.LeaseExpiry)

	return &dsclient.GenerateCredentialResponse{
		Credential: &dsclient.Credential{
			CredentialID: wire.CredentialID,
			Data:         data,
			LeaseID:      wire.LeaseID,
			ExpiresAt:    expiry,
		},
		Lease: &dsclient.Lease{
			LeaseID:   wire.LeaseID,
			ExpiresAt: expiry,
			TTL:       ttl,
			Renewable: wire.Renewable,
		},
		LeaseID: wire.LeaseID,
	}, nil
}

func (h *dynamicSecretsRESTHandler) renewLease(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.RenewLeaseRequest)
	if !ok || r == nil || r.LeaseID == "" {
		return nil, errors.NewInvalidInput("rest.dynamicsecrets.RenewLease", "lease id is required")
	}
	body := &dsRenewLeaseWire{}
	if r.Increment > 0 {
		body.Increment = r.Increment.String()
	}

	var wire dsRenewedLeaseWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		dynamicSecretsBasePath+"/leases/"+url.PathEscape(r.LeaseID)+"/renew", body, &wire); err != nil {
		return nil, err
	}
	return &dsclient.RenewLeaseResponse{
		Lease: &dsclient.Lease{
			LeaseID:    wire.LeaseID,
			ExpiresAt:  unixToTime(wire.LeaseExpiry),
			TTL:        parseDurationOrZero(wire.LeaseTTL),
			RenewCount: wire.RenewCount,
			Renewable:  true,
		},
		RenewedAt: unixToTime(wire.RenewedAt),
	}, nil
}

func (h *dynamicSecretsRESTHandler) revokeLease(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dsclient.RevokeLeaseRequest)
	if !ok || r == nil || r.LeaseID == "" {
		return nil, errors.NewInvalidInput("rest.dynamicsecrets.RevokeLease", "lease id is required")
	}

	var wire dsRevokeLeaseWire
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete,
		dynamicSecretsBasePath+"/leases/"+url.PathEscape(r.LeaseID), nil, &wire); err != nil {
		return nil, err
	}
	return &dsclient.RevokeLeaseResponse{RevokedAt: unixToTime(wire.RevokedAt)}, nil
}

func (h *dynamicSecretsRESTHandler) listLeases(ctx context.Context, req interface{}) (interface{}, error) {
	endpoint := dynamicSecretsBasePath + "/leases"
	if r, ok := req.(*dsclient.ListLeasesRequest); ok && r != nil && r.Prefix != "" {
		endpoint += "?" + url.Values{"prefix": {r.Prefix}}.Encode()
	}

	var wire dsListLeasesWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	leases := make([]*dsclient.Lease, 0, len(wire.Leases))
	for i := range wire.Leases {
		l := &wire.Leases[i]
		leases = append(leases, &dsclient.Lease{
			LeaseID:    l.LeaseID,
			SecretPath: l.Path,
			ExpiresAt:  unixToTime(l.Expiry),
			TTL:        parseDurationOrZero(l.TTL),
			RenewCount: l.RenewCount,
			Renewable:  l.Renewable,
			Metadata:   l.Metadata,
		})
	}
	return &dsclient.ListLeasesResponse{Leases: leases, Count: wire.Count}, nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

// dsRoleFromWire converts a server role payload into the SDK role type.
func dsRoleFromWire(w *dsRoleWire) *dsclient.Role {
	if w == nil {
		return nil
	}
	return &dsclient.Role{
		Name:          w.Name,
		GeneratorType: w.GeneratorType,
		Config:        w.Config,
		DefaultTTL:    parseDurationOrZero(w.DefaultTTL),
		MaxTTL:        parseDurationOrZero(w.MaxTTL),
	}
}

// parseDurationOrZero parses a Go duration string, yielding zero when the
// value is empty or malformed. The server always emits time.Duration.String()
// output, so a parse failure means the field was simply absent.
func parseDurationOrZero(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

// unixToTime converts a Unix-seconds timestamp to a time.Time, mapping the
// zero value to the zero time rather than 1970-01-01.
func unixToTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}
