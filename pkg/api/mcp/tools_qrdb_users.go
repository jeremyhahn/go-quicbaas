//go:build mcp

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

// Package mcp — qrdb user lookup + user-login factor tools exposed over MCP.
//
// The qrdb MCP transport (go-qrdb/sdk/go/transport/mcp) calls tools named
// "qrdb_user_get*" and "qrdb_user_authfactor_*" with JSON-text response
// shapes. To keep that surface reachable through quicbaasd's MCP listener,
// these tools mirror the qrdb tool names, argument schemas, and response
// payloads exactly (go-qrdb pkg/api/mcp tools_users.go +
// user_auth_factor_tool.go): user entities marshal with camelCase keys,
// factor records marshal with snake_case keys and base64-encoded credential
// bytes. Each tool delegates to the lazy user / user-auth-factor servicer
// proxies wired by lifecycle via SetUserService / SetUserAuthFactorService —
// the SAME per-call embedded-node resolution every other transport uses, so
// factor enrollment over an MCP-selected client operates on the store the
// unified-login ceremony reads. While the embedded node is absent or sealed
// the proxy fails closed with a typed unavailable error.
//
// Scope mirrors the QUIC bridge (handler_qrdb_users.go): user LOOKUP only
// (read-only resolution) plus the full user-login factor family. User
// mutation tools (qrdb_user_create/delete/list) are owned by the IAM tool
// surface in tools.go.

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// UserServiceUnavailableError is returned by every qrdb_user_get* tool when
// the user lookup service has not been wired by the lifecycle layer. Mirrors
// the CredentialServiceUnavailableError convention.
type UserServiceUnavailableError struct{}

// Error implements the error interface.
func (*UserServiceUnavailableError) Error() string {
	return "qrdb user service not configured"
}

// UserAuthFactorServiceUnavailableError is returned by every
// qrdb_user_authfactor_* tool when the user auth-factor service has not been
// wired by the lifecycle layer.
type UserAuthFactorServiceUnavailableError struct{}

// Error implements the error interface.
func (*UserAuthFactorServiceUnavailableError) Error() string {
	return "qrdb user auth-factor service not configured"
}

// UserToolMissingArgumentError is returned by qrdb user/factor tools whose
// schema lists an argument as required but the caller omitted it (or supplied
// an unparsable value).
type UserToolMissingArgumentError struct {
	Arg string
}

// Error implements the error interface.
func (e *UserToolMissingArgumentError) Error() string {
	return e.Arg + " is required"
}

// UserToolInvalidArgumentError wraps a decode failure on a qrdb user/factor
// tool argument (bad base64 or bad JSON).
type UserToolInvalidArgumentError struct {
	Arg string
	Err error
}

// Error implements the error interface.
func (e *UserToolInvalidArgumentError) Error() string {
	if e.Err == nil {
		return "invalid argument: " + e.Arg
	}
	return "invalid argument: " + e.Arg + ": " + e.Err.Error()
}

// Unwrap exposes the underlying decode error for errors.Is/As traversal.
func (e *UserToolInvalidArgumentError) Unwrap() error { return e.Err }

// =============================================================================
// Tool registration
// =============================================================================

// registerQRDBUserTools adds every qrdb_user_get* and qrdb_user_authfactor_*
// tool definition to the registry. The tools are dispatched via
// isQRDBUserTool / executeQRDBUserTool, mirroring the credential tool wiring.
func (r *ToolRegistry) registerQRDBUserTools() {
	r.addTool(ToolDefinition{
		Name:        "qrdb_user_get",
		Description: "Retrieve a qrdb user by its globally unique numeric ID.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Numeric user ID (uint64, decimal string)."},
			},
			"required": []string{"id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_get_by_username",
		Description: "Resolve a qrdb user by tenant-scoped username (the factor-enrollment subject lookup).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant ID (empty selects the default tenant)."},
				"username":  map[string]interface{}{"type": "string", "description": "Username unique within the tenant."},
			},
			"required": []string{"username"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_get_by_cert_thumbprint",
		Description: "Resolve a qrdb user by mTLS SPKI SHA-256 thumbprint (the mTLS-auth hot-path lookup).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"thumbprint": map[string]interface{}{"type": "string", "description": "Hex-encoded SPKI SHA-256 thumbprint."},
			},
			"required": []string{"thumbprint"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_authfactor_add",
		Description: "Register a user authentication factor (mtls, fido2, or piv).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id":             map[string]interface{}{"type": "string", "description": "Numeric user ID (uint64, decimal string)."},
				"type":                map[string]interface{}{"type": "string", "description": "Factor type: mtls, fido2, or piv."},
				"label":               map[string]interface{}{"type": "string", "description": "Human-readable display label."},
				"credential_b64":      map[string]interface{}{"type": "string", "description": "Base64-encoded canonical lookup-key bytes."},
				"spki_credential_b64": map[string]interface{}{"type": "string", "description": "Base64-encoded SHA-256 of the certificate SubjectPublicKeyInfo (renewal-stable secondary lookup key, mtls/piv only)."},
				"metadata_json":       map[string]interface{}{"type": "string", "description": "JSON-encoded type-specific metadata."},
				"created_by":          map[string]interface{}{"type": "string", "description": "Provenance: self, admin, or ceremony."},
			},
			"required": []string{"user_id", "type", "credential_b64"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_authfactor_list",
		Description: "List all factors (active and revoked) belonging to a user.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{"type": "string", "description": "Numeric user ID (uint64, decimal string)."},
			},
			"required": []string{"user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_authfactor_get",
		Description: "Retrieve a single user auth factor by ID.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"factor_id": map[string]interface{}{"type": "string", "description": "Stable factor identifier."},
			},
			"required": []string{"factor_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_authfactor_remove",
		Description: "Soft-delete a factor by setting its RevokedAt. Use force=true to remove the user's last factor (admin override).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"factor_id": map[string]interface{}{"type": "string", "description": "Factor ID."},
				"force":     map[string]interface{}{"type": "boolean", "description": "Bypass the last-active-factor guard."},
			},
			"required": []string{"factor_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_authfactor_rotate",
		Description: "Atomically replace an old factor with a new one in a single transaction.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"old_factor_id":       map[string]interface{}{"type": "string", "description": "Existing factor to revoke."},
				"type":                map[string]interface{}{"type": "string", "description": "New factor type."},
				"label":               map[string]interface{}{"type": "string", "description": "New factor label."},
				"credential_b64":      map[string]interface{}{"type": "string", "description": "Base64-encoded new credential bytes."},
				"spki_credential_b64": map[string]interface{}{"type": "string", "description": "Base64-encoded SHA-256 of the certificate SubjectPublicKeyInfo (renewal-stable secondary lookup key, mtls/piv only)."},
				"metadata_json":       map[string]interface{}{"type": "string", "description": "JSON-encoded new metadata."},
			},
			"required": []string{"old_factor_id", "type", "credential_b64"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_authfactor_relabel",
		Description: "Update the human-readable label on a user auth factor.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"factor_id": map[string]interface{}{"type": "string", "description": "Factor ID."},
				"label":     map[string]interface{}{"type": "string", "description": "New label."},
			},
			"required": []string{"factor_id", "label"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_user_authfactor_login",
		Description: "Resolve an inbound (type, lookup-key) tuple to a user and factor. Lookup resolution only — no bearer is minted and hardware verification must be performed by the caller.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"type":           map[string]interface{}{"type": "string", "description": "Factor type: mtls, fido2, or piv."},
				"lookup_key_b64": map[string]interface{}{"type": "string", "description": "Base64-encoded canonical lookup-key bytes."},
			},
			"required": []string{"type", "lookup_key_b64"},
		},
	})
}

// =============================================================================
// Membership + dispatch
// =============================================================================

// qrdbUserToolNames carries every qrdb user lookup and user auth-factor tool
// name handled by executeQRDBUserTool. Membership is used by isQRDBUserTool
// to route dispatch from the registry's default branch. The IAM-owned
// qrdb_user_create / qrdb_user_delete / qrdb_user_list tools are NOT members;
// they dispatch through the RBAC switch in tools.go.
var qrdbUserToolNames = map[string]struct{}{
	"qrdb_user_get":                    {},
	"qrdb_user_get_by_username":        {},
	"qrdb_user_get_by_cert_thumbprint": {},
	"qrdb_user_authfactor_add":         {},
	"qrdb_user_authfactor_list":        {},
	"qrdb_user_authfactor_get":         {},
	"qrdb_user_authfactor_remove":      {},
	"qrdb_user_authfactor_rotate":      {},
	"qrdb_user_authfactor_relabel":     {},
	"qrdb_user_authfactor_login":       {},
}

// isQRDBUserTool reports whether the given tool name belongs to the qrdb
// user lookup / user auth-factor surface owned by executeQRDBUserTool.
func isQRDBUserTool(name string) bool {
	_, ok := qrdbUserToolNames[name]
	return ok
}

// qrdbUserToolFunc is the signature of a per-tool dispatcher bound to the
// registry. The dispatch table below keeps executeQRDBUserTool branch-free.
type qrdbUserToolFunc func(r *ToolRegistry, ctx context.Context, args map[string]interface{}) (interface{}, error)

// qrdbUserToolDispatch maps tool name -> handler. Lookup is O(1).
var qrdbUserToolDispatch = map[string]qrdbUserToolFunc{
	"qrdb_user_get":                    (*ToolRegistry).executeQRDBUserGet,
	"qrdb_user_get_by_username":        (*ToolRegistry).executeQRDBUserGetByUsername,
	"qrdb_user_get_by_cert_thumbprint": (*ToolRegistry).executeQRDBUserGetByCertThumbprint,
	"qrdb_user_authfactor_add":         (*ToolRegistry).executeUserAuthFactorAdd,
	"qrdb_user_authfactor_list":        (*ToolRegistry).executeUserAuthFactorList,
	"qrdb_user_authfactor_get":         (*ToolRegistry).executeUserAuthFactorGet,
	"qrdb_user_authfactor_remove":      (*ToolRegistry).executeUserAuthFactorRemove,
	"qrdb_user_authfactor_rotate":      (*ToolRegistry).executeUserAuthFactorRotate,
	"qrdb_user_authfactor_relabel":     (*ToolRegistry).executeUserAuthFactorRelabel,
	"qrdb_user_authfactor_login":       (*ToolRegistry).executeUserAuthFactorLogin,
}

// executeQRDBUserTool dispatches a qrdb user/factor tool by name. Mirrors the
// credential tool wiring: the services are read from registry fields without
// locking — lifecycle wiring is single-shot before the listener starts
// accepting traffic, so no race exists in practice.
func (r *ToolRegistry) executeQRDBUserTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handler, ok := qrdbUserToolDispatch[name]
	if !ok {
		// Should be unreachable when isQRDBUserTool gated the call.
		return nil, NewUnknownQRDBUserToolError(name)
	}
	return handler(r, ctx, args)
}

// UnknownQRDBUserToolError indicates dispatch reached executeQRDBUserTool
// with a name absent from the dispatch table.
type UnknownQRDBUserToolError struct {
	Name string
}

// NewUnknownQRDBUserToolError builds an UnknownQRDBUserToolError.
func NewUnknownQRDBUserToolError(name string) *UnknownQRDBUserToolError {
	return &UnknownQRDBUserToolError{Name: name}
}

// Error implements the error interface.
func (e *UnknownQRDBUserToolError) Error() string {
	return "unknown qrdb user tool: " + e.Name
}

// =============================================================================
// User lookup tool handlers
// =============================================================================

// executeQRDBUserGet serves qrdb_user_get.
func (r *ToolRegistry) executeQRDBUserGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userSvc
	if svc == nil {
		return nil, &UserServiceUnavailableError{}
	}
	id := getUint64(args, "id")
	if id == 0 {
		return nil, &UserToolMissingArgumentError{Arg: "id"}
	}
	u, err := svc.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return qrdbUserToMCPMap(u), nil
}

// executeQRDBUserGetByUsername serves qrdb_user_get_by_username.
func (r *ToolRegistry) executeQRDBUserGetByUsername(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userSvc
	if svc == nil {
		return nil, &UserServiceUnavailableError{}
	}
	username := getString(args, "username")
	if username == "" {
		return nil, &UserToolMissingArgumentError{Arg: "username"}
	}
	u, err := svc.GetUserByUsername(ctx, getString(args, "tenant_id"), username)
	if err != nil {
		return nil, err
	}
	return qrdbUserToMCPMap(u), nil
}

// executeQRDBUserGetByCertThumbprint serves qrdb_user_get_by_cert_thumbprint.
func (r *ToolRegistry) executeQRDBUserGetByCertThumbprint(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userSvc
	if svc == nil {
		return nil, &UserServiceUnavailableError{}
	}
	thumbprint := getString(args, "thumbprint")
	if thumbprint == "" {
		return nil, &UserToolMissingArgumentError{Arg: "thumbprint"}
	}
	u, err := svc.UserByCertThumbprint(ctx, thumbprint)
	if err != nil {
		return nil, err
	}
	return qrdbUserToMCPMap(u), nil
}

// =============================================================================
// User auth-factor tool handlers
// =============================================================================

// executeUserAuthFactorAdd serves qrdb_user_authfactor_add. The service
// assigns the factor's ID and RegisteredAt in place, so the response echoes
// the fully populated record.
func (r *ToolRegistry) executeUserAuthFactorAdd(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userAuthFactorSvc
	if svc == nil {
		return nil, &UserAuthFactorServiceUnavailableError{}
	}
	userID := getUint64(args, "user_id")
	if userID == 0 {
		return nil, &UserToolMissingArgumentError{Arg: "user_id"}
	}
	factor, err := userAuthFactorFromMCPArgs(args)
	if err != nil {
		return nil, err
	}
	factor.UserID = userID
	factor.CreatedBy = getString(args, "created_by")
	if err := svc.Add(ctx, userID, factor); err != nil {
		return nil, err
	}
	return map[string]interface{}{"factor": userAuthFactorToMCPMap(factor)}, nil
}

// executeUserAuthFactorList serves qrdb_user_authfactor_list.
func (r *ToolRegistry) executeUserAuthFactorList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userAuthFactorSvc
	if svc == nil {
		return nil, &UserAuthFactorServiceUnavailableError{}
	}
	userID := getUint64(args, "user_id")
	if userID == 0 {
		return nil, &UserToolMissingArgumentError{Arg: "user_id"}
	}
	factors, err := svc.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, len(factors))
	for i, f := range factors {
		out[i] = userAuthFactorToMCPMap(f)
	}
	return map[string]interface{}{"factors": out, "total": len(out)}, nil
}

// executeUserAuthFactorGet serves qrdb_user_authfactor_get.
func (r *ToolRegistry) executeUserAuthFactorGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userAuthFactorSvc
	if svc == nil {
		return nil, &UserAuthFactorServiceUnavailableError{}
	}
	factorID := getString(args, "factor_id")
	if factorID == "" {
		return nil, &UserToolMissingArgumentError{Arg: "factor_id"}
	}
	factor, err := svc.Get(ctx, factorID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"factor": userAuthFactorToMCPMap(factor)}, nil
}

// executeUserAuthFactorRemove serves qrdb_user_authfactor_remove.
func (r *ToolRegistry) executeUserAuthFactorRemove(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userAuthFactorSvc
	if svc == nil {
		return nil, &UserAuthFactorServiceUnavailableError{}
	}
	factorID := getString(args, "factor_id")
	if factorID == "" {
		return nil, &UserToolMissingArgumentError{Arg: "factor_id"}
	}
	if err := svc.Remove(ctx, factorID, getBool(args, "force")); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "revoked", "factor_id": factorID}, nil
}

// executeUserAuthFactorRotate serves qrdb_user_authfactor_rotate. The
// response key is "factor", matching go-qrdb's MCP server (the QUIC wire uses
// "new_factor" — the two wires intentionally differ).
func (r *ToolRegistry) executeUserAuthFactorRotate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userAuthFactorSvc
	if svc == nil {
		return nil, &UserAuthFactorServiceUnavailableError{}
	}
	oldID := getString(args, "old_factor_id")
	if oldID == "" {
		return nil, &UserToolMissingArgumentError{Arg: "old_factor_id"}
	}
	newFactor, err := userAuthFactorFromMCPArgs(args)
	if err != nil {
		return nil, err
	}
	if err := svc.Rotate(ctx, oldID, newFactor); err != nil {
		return nil, err
	}
	return map[string]interface{}{"factor": userAuthFactorToMCPMap(newFactor)}, nil
}

// executeUserAuthFactorRelabel serves qrdb_user_authfactor_relabel.
// Mirroring go-qrdb's server, the updated record is re-read and returned.
func (r *ToolRegistry) executeUserAuthFactorRelabel(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userAuthFactorSvc
	if svc == nil {
		return nil, &UserAuthFactorServiceUnavailableError{}
	}
	factorID := getString(args, "factor_id")
	if factorID == "" {
		return nil, &UserToolMissingArgumentError{Arg: "factor_id"}
	}
	label := getString(args, "label")
	if label == "" {
		return nil, &UserToolMissingArgumentError{Arg: "label"}
	}
	if err := svc.Relabel(ctx, factorID, label); err != nil {
		return nil, err
	}
	factor, err := svc.Get(ctx, factorID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"factor": userAuthFactorToMCPMap(factor)}, nil
}

// executeUserAuthFactorLogin serves qrdb_user_authfactor_login. Lookup
// resolution only: no bearer is minted and no possession is proven here.
// Credential-minting login flows on quicbaasd go exclusively through the
// unified login ceremony.
func (r *ToolRegistry) executeUserAuthFactorLogin(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc := r.userAuthFactorSvc
	if svc == nil {
		return nil, &UserAuthFactorServiceUnavailableError{}
	}
	typ := getString(args, "type")
	if typ == "" {
		return nil, &UserToolMissingArgumentError{Arg: "type"}
	}
	keyB64 := getString(args, "lookup_key_b64")
	if keyB64 == "" {
		return nil, &UserToolMissingArgumentError{Arg: "lookup_key_b64"}
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, &UserToolInvalidArgumentError{Arg: "lookup_key_b64", Err: err}
	}
	factor, err := svc.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorType(typ), key)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"user_id": factor.UserID,
		"active":  true,
		"factor":  userAuthFactorToMCPMap(factor),
	}, nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

// qrdbUserToMCPMap projects a qrdbsdk.User into the JSON shape the qrdb MCP
// transport decodes (camelCase keys, RFC 3339 timestamps) — identical to
// go-qrdb's json.Marshal(user.User) output.
func qrdbUserToMCPMap(u *qrdbsdk.User) map[string]interface{} {
	if u == nil {
		return nil
	}
	roleIDs := u.RoleIDs
	if roleIDs == nil {
		roleIDs = []string{}
	}
	return map[string]interface{}{
		"id":        u.ID,
		"username":  u.Username,
		"tenantID":  u.TenantID,
		"roleIDs":   roleIDs,
		"enabled":   u.Enabled,
		"system":    u.System,
		"createdAt": u.CreatedAt.Format(time.RFC3339),
		"updatedAt": u.UpdatedAt.Format(time.RFC3339),
	}
}

// userAuthFactorToMCPMap projects a qrdbsdk.AuthFactor into the JSON shape
// the qrdb MCP transport decodes, mirroring go-qrdb's factorToMCPMap:
// snake_case keys, base64-encoded credential bytes, RFC 3339 timestamps.
func userAuthFactorToMCPMap(f *qrdbsdk.AuthFactor) map[string]interface{} {
	if f == nil {
		return nil
	}
	out := map[string]interface{}{
		"id":            f.ID,
		"user_id":       f.UserID,
		"type":          string(f.Type),
		"label":         f.Label,
		"credential":    base64.StdEncoding.EncodeToString(f.Credential),
		"metadata":      f.Metadata,
		"registered_at": f.RegisteredAt.Format(time.RFC3339),
		"created_by":    f.CreatedBy,
	}
	if len(f.SPKICredential) > 0 {
		out["spki_credential"] = base64.StdEncoding.EncodeToString(f.SPKICredential)
	}
	if f.LastUsedAt != nil {
		out["last_used_at"] = f.LastUsedAt.Format(time.RFC3339)
	}
	if f.RevokedAt != nil {
		out["revoked_at"] = f.RevokedAt.Format(time.RFC3339)
	}
	return out
}

// userAuthFactorFromMCPArgs builds a qrdbsdk.AuthFactor from the shared
// factor argument set (type, label, credential_b64, spki_credential_b64,
// metadata_json) used by the add and rotate tools.
func userAuthFactorFromMCPArgs(args map[string]interface{}) (*qrdbsdk.AuthFactor, error) {
	typ := getString(args, "type")
	if typ == "" {
		return nil, &UserToolMissingArgumentError{Arg: "type"}
	}
	credB64 := getString(args, "credential_b64")
	if credB64 == "" {
		return nil, &UserToolMissingArgumentError{Arg: "credential_b64"}
	}
	cred, err := base64.StdEncoding.DecodeString(credB64)
	if err != nil {
		return nil, &UserToolInvalidArgumentError{Arg: "credential_b64", Err: err}
	}
	var spkiCred []byte
	if spkiB64 := getString(args, "spki_credential_b64"); spkiB64 != "" {
		spkiCred, err = base64.StdEncoding.DecodeString(spkiB64)
		if err != nil {
			return nil, &UserToolInvalidArgumentError{Arg: "spki_credential_b64", Err: err}
		}
	}
	var metadata map[string]string
	if mj := getString(args, "metadata_json"); mj != "" {
		if err := json.Unmarshal([]byte(mj), &metadata); err != nil {
			return nil, &UserToolInvalidArgumentError{Arg: "metadata_json", Err: err}
		}
	}
	return &qrdbsdk.AuthFactor{
		Type:           qrdbsdk.AuthFactorType(typ),
		Label:          getString(args, "label"),
		Credential:     cred,
		SPKICredential: spkiCred,
		Metadata:       metadata,
	}, nil
}

// =============================================================================
// Wiring
// =============================================================================

// SetUserService installs the qrdb user lookup facade used by the
// qrdb_user_get* tools. Lifecycle wiring passes the lazy per-call proxy over
// the embedded node's user service (user services are nil until unseal on
// seal-enabled boots, so eager capture is forbidden). Passing nil disables
// the surface (every tool returns UserServiceUnavailableError).
func (r *ToolRegistry) SetUserService(svc qrdbsdk.UserServicer) {
	r.userSvc = svc
}

// SetUserAuthFactorService installs the qrdb user auth-factor facade used by
// the qrdb_user_authfactor_* tools. Lifecycle wiring passes the lazy per-call
// proxy over the SAME UserAuthFactor store instance the unified-login
// ceremony resolves against. Passing nil disables the surface.
func (r *ToolRegistry) SetUserAuthFactorService(svc qrdbsdk.UserAuthFactorServicer) {
	r.userAuthFactorSvc = svc
}

// SetUserService forwards the qrdb user lookup facade to the tool registry.
func (s *Server) SetUserService(svc qrdbsdk.UserServicer) {
	if s.tools != nil {
		s.tools.SetUserService(svc)
	}
}

// SetUserAuthFactorService forwards the qrdb user auth-factor facade to the
// tool registry.
func (s *Server) SetUserAuthFactorService(svc qrdbsdk.UserAuthFactorServicer) {
	if s.tools != nil {
		s.tools.SetUserAuthFactorService(svc)
	}
}
