//go:build quic

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

package quic

import (
	"context"
	"encoding/json"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// qrdb user + user auth-factor handlers (qrdb wire format, qrdb-quic ALPN)
//
// The qrdb SDK QUIC client (go-qrdb/sdk/go/transport/quic) issues the user.*
// and userauthfactor.* method families with JSON payloads — unlike the
// protobuf credential/barrier families — mirroring go-qrdb's own QUIC server
// (pkg/api/quic/handlers_users.go + user_auth_factor_frame.go). The wire
// request/response shapes are pinned inline below: camelCase keys for the
// user entity, snake_case keys for the auth-factor record, byte slices
// base64-encoded by encoding/json.
//
// Each handler follows the same pattern as the credential and tenant-barrier
// bridges: unmarshal the canonical qrdb wire request, delegate to the lazy
// user / user-auth-factor servicer proxies wired by lifecycle (the SAME
// per-call GetQRDBUserServicer / GetQRDBUserAuthFactorServicer resolution the
// gRPC, Unix, REST, and embedded transports use), and marshal the canonical
// qrdb wire response. While the embedded node is absent or sealed the proxy
// fails closed with a typed unavailable error, which travels back to the
// client as the wire error text.
//
// Scope: this bridge serves user LOOKUP (read-only resolution — the surface
// the SDK factor-enrollment flow depends on) plus the full user-login factor
// family. User mutation verbs (create/update/delete/enable/disable) remain
// unimplemented on this ALPN; they are owned by the IAM surfaces.
// =============================================================================

// QRDBUserNotConfiguredError indicates a user.* lookup RPC arrived on the
// qrdb-quic ALPN but no UserServicer was wired into the QUIC server
// (SetQRDBUserService not called or called with nil).
type QRDBUserNotConfiguredError struct{}

// Error implements the error interface.
func (*QRDBUserNotConfiguredError) Error() string {
	return "qrdb user service not configured"
}

// QRDBUserAuthFactorNotConfiguredError indicates a userauthfactor.* RPC
// arrived on the qrdb-quic ALPN but no UserAuthFactorServicer was wired into
// the QUIC server (SetQRDBUserAuthFactorService not called or called with
// nil).
type QRDBUserAuthFactorNotConfiguredError struct{}

// Error implements the error interface.
func (*QRDBUserAuthFactorNotConfiguredError) Error() string {
	return "qrdb user auth-factor service not configured"
}

// =============================================================================
// Wire types (pinned to the qrdb QUIC wire contract)
// =============================================================================

// qrdbGetUserWireRequest is the JSON wire payload for user.Get.
type qrdbGetUserWireRequest struct {
	ID uint64 `json:"id"`
}

// qrdbGetUserByUsernameWireRequest is the JSON wire payload for
// user.GetByUsername.
type qrdbGetUserByUsernameWireRequest struct {
	TenantID string `json:"tenantID"`
	Username string `json:"username"`
}

// qrdbGetUserByCertThumbprintWireRequest is the JSON wire payload for
// user.GetByCertThumbprint.
type qrdbGetUserByCertThumbprintWireRequest struct {
	Thumbprint string `json:"thumbprint"`
}

// qrdbUserWireResponse is the JSON wire representation of a user entity.
// Keys are camelCase, matching go-qrdb's userWireResponse byte-for-byte.
type qrdbUserWireResponse struct {
	ID        uint64   `json:"id"`
	Username  string   `json:"username"`
	TenantID  string   `json:"tenantID"`
	RoleIDs   []string `json:"roleIDs"`
	Enabled   bool     `json:"enabled"`
	System    bool     `json:"system"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// qrdbUserFactorWireRecord is the JSON wire shape of a qrdbsdk.AuthFactor.
// Keys are snake_case, matching go-qrdb's quicUserFactorRecord byte-for-byte;
// []byte fields are base64-encoded by encoding/json.
type qrdbUserFactorWireRecord struct {
	ID             string            `json:"id"`
	UserID         uint64            `json:"user_id"`
	Type           string            `json:"type"`
	Label          string            `json:"label,omitempty"`
	Credential     []byte            `json:"credential"`
	SPKICredential []byte            `json:"spki_credential,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	RegisteredAt   string            `json:"registered_at"`
	LastUsedAt     string            `json:"last_used_at,omitempty"`
	RevokedAt      string            `json:"revoked_at,omitempty"`
	CreatedBy      string            `json:"created_by,omitempty"`
}

// qrdbUserFactorAddWireRequest is the JSON wire payload for
// userauthfactor.Add (and the nested new_factor of userauthfactor.Rotate).
type qrdbUserFactorAddWireRequest struct {
	UserID         uint64            `json:"user_id"`
	Type           string            `json:"type"`
	Label          string            `json:"label,omitempty"`
	Credential     []byte            `json:"credential"`
	SPKICredential []byte            `json:"spki_credential,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedBy      string            `json:"created_by,omitempty"`
}

// qrdbUserFactorListWireRequest is the JSON wire payload for
// userauthfactor.List.
type qrdbUserFactorListWireRequest struct {
	UserID uint64 `json:"user_id"`
}

// qrdbUserFactorGetWireRequest is the JSON wire payload for
// userauthfactor.Get.
type qrdbUserFactorGetWireRequest struct {
	FactorID string `json:"factor_id"`
}

// qrdbUserFactorRemoveWireRequest is the JSON wire payload for
// userauthfactor.Remove.
type qrdbUserFactorRemoveWireRequest struct {
	FactorID string `json:"factor_id"`
	Force    bool   `json:"force,omitempty"`
}

// qrdbUserFactorRotateWireRequest is the JSON wire payload for
// userauthfactor.Rotate.
type qrdbUserFactorRotateWireRequest struct {
	OldFactorID string                       `json:"old_factor_id"`
	NewFactor   qrdbUserFactorAddWireRequest `json:"new_factor"`
}

// qrdbUserFactorRelabelWireRequest is the JSON wire payload for
// userauthfactor.Relabel.
type qrdbUserFactorRelabelWireRequest struct {
	FactorID string `json:"factor_id"`
	Label    string `json:"label"`
}

// qrdbUserFactorLoginWireRequest is the JSON wire payload for
// userauthfactor.Login.
type qrdbUserFactorLoginWireRequest struct {
	Type      string `json:"type"`
	LookupKey []byte `json:"lookup_key"`
}

// =============================================================================
// Wire conversion helpers
// =============================================================================

// qrdbUserToWireQUIC projects a qrdbsdk.User into the qrdb QUIC wire shape.
func qrdbUserToWireQUIC(u *qrdbsdk.User) *qrdbUserWireResponse {
	if u == nil {
		return nil
	}
	roleIDs := u.RoleIDs
	if roleIDs == nil {
		roleIDs = []string{}
	}
	return &qrdbUserWireResponse{
		ID:        u.ID,
		Username:  u.Username,
		TenantID:  u.TenantID,
		RoleIDs:   roleIDs,
		Enabled:   u.Enabled,
		System:    u.System,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
		UpdatedAt: u.UpdatedAt.Format(time.RFC3339),
	}
}

// qrdbUserFactorToWireQUIC projects a qrdbsdk.AuthFactor into the qrdb QUIC
// wire record, mirroring go-qrdb's userFactorToQUIC.
func qrdbUserFactorToWireQUIC(f *qrdbsdk.AuthFactor) qrdbUserFactorWireRecord {
	if f == nil {
		return qrdbUserFactorWireRecord{}
	}
	r := qrdbUserFactorWireRecord{
		ID:             f.ID,
		UserID:         f.UserID,
		Type:           string(f.Type),
		Label:          f.Label,
		Credential:     f.Credential,
		SPKICredential: f.SPKICredential,
		Metadata:       f.Metadata,
		RegisteredAt:   f.RegisteredAt.Format(time.RFC3339),
		CreatedBy:      f.CreatedBy,
	}
	if f.LastUsedAt != nil {
		r.LastUsedAt = f.LastUsedAt.Format(time.RFC3339)
	}
	if f.RevokedAt != nil {
		r.RevokedAt = f.RevokedAt.Format(time.RFC3339)
	}
	return r
}

// qrdbUserFactorFromWireQUIC builds a qrdbsdk.AuthFactor from the wire add
// request, mirroring go-qrdb's userFactorFromQUIC.
func qrdbUserFactorFromWireQUIC(req *qrdbUserFactorAddWireRequest) *qrdbsdk.AuthFactor {
	return &qrdbsdk.AuthFactor{
		UserID:         req.UserID,
		Type:           qrdbsdk.AuthFactorType(req.Type),
		Label:          req.Label,
		Credential:     req.Credential,
		SPKICredential: req.SPKICredential,
		Metadata:       req.Metadata,
		CreatedBy:      req.CreatedBy,
	}
}

// =============================================================================
// Facade accessors (snapshot under svcMu)
// =============================================================================

// qrdbUserService returns the currently wired qrdb user lookup facade, or nil
// if SetQRDBUserService has not been called with a non-nil value.
func (s *Server) qrdbUserService() qrdbsdk.UserServicer {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.qrdbUserSvc
}

// qrdbUserAuthFactorService returns the currently wired qrdb user auth-factor
// facade, or nil if SetQRDBUserAuthFactorService has not been called with a
// non-nil value.
func (s *Server) qrdbUserAuthFactorService() qrdbsdk.UserAuthFactorServicer {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.qrdbUserAuthFactorSvc
}

// =============================================================================
// User lookup handlers (qrdb wire format)
// =============================================================================

// qrdbUserGet handles the "user.Get" qrdb-quic method.
func (s *Server) qrdbUserGet(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserService()
	if svc == nil {
		return nil, &QRDBUserNotConfiguredError{}
	}
	var req qrdbGetUserWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	u, err := svc.GetUser(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(qrdbUserToWireQUIC(u))
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserGetByUsername handles the "user.GetByUsername" qrdb-quic method.
// This is the lookup the qrdb SDK factor surface uses to resolve a subject to
// its uint64 user id before enrolling a factor.
func (s *Server) qrdbUserGetByUsername(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserService()
	if svc == nil {
		return nil, &QRDBUserNotConfiguredError{}
	}
	var req qrdbGetUserByUsernameWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	u, err := svc.GetUserByUsername(ctx, req.TenantID, req.Username)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(qrdbUserToWireQUIC(u))
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserGetByCertThumbprint handles the "user.GetByCertThumbprint"
// qrdb-quic method (the mTLS-auth hot-path lookup).
func (s *Server) qrdbUserGetByCertThumbprint(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserService()
	if svc == nil {
		return nil, &QRDBUserNotConfiguredError{}
	}
	var req qrdbGetUserByCertThumbprintWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	u, err := svc.UserByCertThumbprint(ctx, req.Thumbprint)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(qrdbUserToWireQUIC(u))
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// =============================================================================
// User auth-factor handlers (qrdb wire format)
// =============================================================================

// qrdbUserFactorAdd handles the "userauthfactor.Add" qrdb-quic method. The
// service assigns the factor's ID and RegisteredAt in place, so the response
// echoes the fully populated record.
func (s *Server) qrdbUserFactorAdd(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserAuthFactorService()
	if svc == nil {
		return nil, &QRDBUserAuthFactorNotConfiguredError{}
	}
	var req qrdbUserFactorAddWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	factor := qrdbUserFactorFromWireQUIC(&req)
	if err := svc.Add(ctx, req.UserID, factor); err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(map[string]any{"factor": qrdbUserFactorToWireQUIC(factor)})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserFactorList handles the "userauthfactor.List" qrdb-quic method.
func (s *Server) qrdbUserFactorList(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserAuthFactorService()
	if svc == nil {
		return nil, &QRDBUserAuthFactorNotConfiguredError{}
	}
	var req qrdbUserFactorListWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	factors, err := svc.List(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	wire := make([]qrdbUserFactorWireRecord, len(factors))
	for i, f := range factors {
		wire[i] = qrdbUserFactorToWireQUIC(f)
	}
	resp, marshalErr := json.Marshal(map[string]any{
		"factors": wire,
		"total":   len(wire),
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserFactorGet handles the "userauthfactor.Get" qrdb-quic method.
func (s *Server) qrdbUserFactorGet(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserAuthFactorService()
	if svc == nil {
		return nil, &QRDBUserAuthFactorNotConfiguredError{}
	}
	var req qrdbUserFactorGetWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	factor, err := svc.Get(ctx, req.FactorID)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(map[string]any{"factor": qrdbUserFactorToWireQUIC(factor)})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserFactorRemove handles the "userauthfactor.Remove" qrdb-quic method
// (soft delete; removing the last active factor requires force=true).
func (s *Server) qrdbUserFactorRemove(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserAuthFactorService()
	if svc == nil {
		return nil, &QRDBUserAuthFactorNotConfiguredError{}
	}
	var req qrdbUserFactorRemoveWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.Remove(ctx, req.FactorID, req.Force); err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(map[string]string{"status": "revoked"})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserFactorRotate handles the "userauthfactor.Rotate" qrdb-quic method.
// The response key is "new_factor", matching go-qrdb's QUIC server (the MCP
// mirror uses "factor" — the two wires intentionally differ).
func (s *Server) qrdbUserFactorRotate(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserAuthFactorService()
	if svc == nil {
		return nil, &QRDBUserAuthFactorNotConfiguredError{}
	}
	var req qrdbUserFactorRotateWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	newFactor := qrdbUserFactorFromWireQUIC(&req.NewFactor)
	if err := svc.Rotate(ctx, req.OldFactorID, newFactor); err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(map[string]any{"new_factor": qrdbUserFactorToWireQUIC(newFactor)})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserFactorRelabel handles the "userauthfactor.Relabel" qrdb-quic
// method. Mirroring go-qrdb's server, the updated record is re-read and
// returned.
func (s *Server) qrdbUserFactorRelabel(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserAuthFactorService()
	if svc == nil {
		return nil, &QRDBUserAuthFactorNotConfiguredError{}
	}
	var req qrdbUserFactorRelabelWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.Relabel(ctx, req.FactorID, req.Label); err != nil {
		return nil, err
	}
	factor, err := svc.Get(ctx, req.FactorID)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(map[string]any{"factor": qrdbUserFactorToWireQUIC(factor)})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbUserFactorLogin handles the "userauthfactor.Login" qrdb-quic method.
// This is lookup RESOLUTION only, mirroring the qrdb wire contract: no bearer
// is minted and no possession is proven here. Credential-minting login flows
// on quicbaasd go exclusively through the unified login ceremony, which
// derives the lookup key from the VERIFIED mTLS peer certificate.
func (s *Server) qrdbUserFactorLogin(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbUserAuthFactorService()
	if svc == nil {
		return nil, &QRDBUserAuthFactorNotConfiguredError{}
	}
	var req qrdbUserFactorLoginWireRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	factor, err := svc.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorType(req.Type), req.LookupKey)
	if err != nil {
		return nil, err
	}
	resp, marshalErr := json.Marshal(map[string]any{
		"user_id": factor.UserID,
		"active":  true,
		"factor":  qrdbUserFactorToWireQUIC(factor),
	})
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// =============================================================================
// Wiring
// =============================================================================

// SetQRDBUserService installs the qrdb user lookup facade used by the user.*
// method handlers on the qrdb-quic ALPN. Lifecycle wiring passes the lazy
// per-call proxy over the embedded node's user service (user services are nil
// until unseal on seal-enabled boots, so eager capture is forbidden). Safe to
// call concurrently with request handling. Passing nil disables the surface
// (handlers respond with the not-configured error).
func (s *Server) SetQRDBUserService(svc qrdbsdk.UserServicer) {
	s.svcMu.Lock()
	s.qrdbUserSvc = svc
	s.svcMu.Unlock()
}

// SetQRDBUserAuthFactorService installs the qrdb user auth-factor facade used
// by the userauthfactor.* method handlers on the qrdb-quic ALPN. Lifecycle
// wiring passes the lazy per-call proxy over the SAME UserAuthFactor store
// instance the unified-login ceremony resolves against. Safe to call
// concurrently with request handling. Passing nil disables the surface.
func (s *Server) SetQRDBUserAuthFactorService(svc qrdbsdk.UserAuthFactorServicer) {
	s.svcMu.Lock()
	s.qrdbUserAuthFactorSvc = svc
	s.svcMu.Unlock()
}
