//go:build rest

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

package rest

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Barrier REST handlers — /api/v1/barrier surface.
//
// Mirrors the go-qrdb /v1/barrier surface (handlers_barrier.go in
// pkg/api/rest there) so QuicBaaS exposes an identical contract. Each
// handler short-circuits to 503 Service Unavailable when the barrier
// facade is not yet wired (early-boot or test setups).
// =============================================================================

// ---------------------------------------------------------------------------
// REST request types.
//
// Every request shape whose fields the go-qrdb SDK already names is an ALIAS
// of that SDK type, never a copy. The stack is pre-v1 with exactly ONE wire
// format (CLAUDE.md, "SDK Boundary"), and go-qrdb's
// sdk/go/transport/barrier_types.go is where that format is written down: the
// qrdb REST transport marshals those structs straight into the request body
// this package decodes. A local mirror of them is a second copy of one
// contract, and the two drift silently — go-qrdb v0.19.0-alpha retyped every
// barrier credential from string to []byte (so it can be wiped), which made
// `secret` a base64 string on the wire, and the mirror here kept decoding it
// as plain text. `barrier unseal` then handed the base64 TEXT to the seal
// strategy and every unseal over REST failed with "decryption failed".
// Aliasing makes that class of drift a compile error instead.
// ---------------------------------------------------------------------------

type (
	// barrierInitializeRequest is the JSON payload for
	// POST /api/v1/barrier/initialize. Secret is base64 on the wire.
	barrierInitializeRequest = qrdbtransport.BarrierInitializeRequest

	// barrierUnsealRequest is the JSON payload for
	// POST /api/v1/barrier/unseal. Secret is base64 on the wire.
	barrierUnsealRequest = qrdbtransport.BarrierUnsealRequest

	// barrierShamirInitializeRequest is the JSON payload for
	// POST /api/v1/barrier/shamir/initialize. It carries no secret: the
	// shares are sealed to registered officers, never returned.
	barrierShamirInitializeRequest = qrdbtransport.BarrierInitializeShamirRequest

	// barrierRecoveryGenerateRequest is the JSON payload for
	// POST /api/v1/barrier/recovery/generate.
	barrierRecoveryGenerateRequest = qrdbtransport.BarrierGenerateRecoveryKeysRequest
)

// barrierEnableEncryptionRequest is the JSON payload for
// POST /api/v1/barrier/enable-encryption.
//
// This one shape is deliberately NOT an alias of
// qrdbtransport.BarrierEnableEncryptionRequest. The encryption migration is a
// QuicBaaS-native verb — it is served on four QuicBaaS surfaces (REST system,
// REST per-tenant, gRPC api/proto/barrier.proto, MCP qrdb_barrier_enable_
// encryption), all of which name the target `target_strategy`, and go-qrdb's
// transport has no per-tenant counterpart at all. Renaming only this copy to
// upstream's `strategy` would de-align it from its three siblings. Passphrase
// nonetheless follows the same wire rule as every other barrier credential
// here — base64-encoded bytes, matching
// qrdbtransport.BarrierEnableEncryptionRequest.Passphrase ([]byte) — so one
// REST surface carries credentials one way.
type barrierEnableEncryptionRequest struct {
	// TargetStrategy is the keyed strategy to migrate to ("passphrase" or
	// "shamir"). Required.
	TargetStrategy string `json:"target_strategy"`

	// Mechanism selects the physical re-encryption approach ("swap" or
	// "inplace"). Defaults to "swap" when empty.
	Mechanism string `json:"mechanism,omitempty"`

	// Availability controls write availability during migration ("online"
	// or "quiesced"). Defaults to "online" when empty.
	Availability string `json:"availability,omitempty"`

	// Passphrase is the new passphrase for a passphrase-target migration,
	// base64-encoded on the wire. Ignored for Shamir targets.
	Passphrase []byte `json:"passphrase,omitempty"`

	// ShamirThreshold / ShamirTotal configure the M-of-N scheme for a
	// Shamir-target migration. Ignored for passphrase targets.
	ShamirThreshold int `json:"shamir_threshold,omitempty"`
	ShamirTotal     int `json:"shamir_total,omitempty"`

	// TenantID is decoded only so it can be REFUSED. This route's scope is
	// its path — the system barrier; a tenant migration has its own route —
	// and a body tenant_id silently dropped here migrated the system barrier
	// for a caller who asked for a tenant's (go-quicbaas#990).
	TenantID string `json:"tenant_id,omitempty"`
}

// barrierSealRequest is the optional seal body. SOPin is a plain JSON
// string, matching qrdbtransport.BarrierSealRequest.SOPin, so the qrdb SDK's
// REST transport and this route share one wire shape. An empty body is
// valid: it is refused only when a PIN backend with an SO PIN is configured.
type barrierSealRequest struct {
	SOPin string `json:"so_pin,omitempty"`
}

// barrierWireEnableEncryptionRequest is the JSON payload for the go-qrdb
// wire mount POST /v1/barrier/enable-encryption. Unlike the /api/v1 shape
// above, it IS an alias of qrdbtransport.BarrierEnableEncryptionRequest: the
// path is go-qrdb's, re-served here, and its client is go-qrdb's REST
// transport, so the target is `strategy` and a body `tenant_id` selects the
// scope (empty means the system barrier) — go-quicbaas#990.
type barrierWireEnableEncryptionRequest = qrdbtransport.BarrierEnableEncryptionRequest

// Values of qrdbtransport.BarrierEnableEncryptionResponse on the wire mount.
// They are go-qrdb's own: its server answers the same path with these words.
const (
	// barrierMigrationStarted acknowledges an accepted migration. Callers
	// observe completion through the migrated barrier's status — the system
	// barrier's or the tenant's — whose strategy changes.
	barrierMigrationStarted = "migration_started"
	// barrierScopeSystem / barrierScopeTenant name the barrier migrated.
	barrierScopeSystem = string(qrdbsdk.ScopeSystem)
	barrierScopeTenant = string(qrdbsdk.ScopeTenant)
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// wipeCredential returns a cleanup that zeroes the credential slice the
// request field finally holds. It takes a POINTER because of when it has to be
// registered: before the decode, not after.
//
// encoding/json populates earlier fields before it reports a later field's
// type error, so a body whose `secret` is well-formed and whose `threshold` is
// not leaves the plaintext credential on the heap even though the request was
// refused. So does every validation branch that returns 400 between the decode
// and the service call. `defer secure.Wipe(req.Secret)` cannot cover either:
// defer evaluates its arguments immediately, capturing the nil slice that
// exists before the decode runs. Reading the field through a pointer inside
// the deferred closure captures the slice the decode actually produced.
//
// WHAT THIS DOES AND DOES NOT DESTROY, stated because the difference is easy
// to read past. It destroys the slice the field holds when the handler
// returns, and every alias of it — which is the copy the barrier service was
// handed, and the one an attacker reading this process's heap would find under
// a stable name. It does not reach an allocation the decoder made and then
// dropped, and nothing in this package can:
//
//   - A duplicate JSON key re-assigns the field. `{"secret":"aHVudGVyMg==",
//     "secret":null}` decodes the first value, allocates it, then overwrites
//     the field with nil; the first allocation is unreferenced and only the
//     collector reclaims it.
//   - A malformed base64 value leaves whatever the decoder had written to its
//     scratch buffer before it gave up.
//
// This is the decode-side half of a limit go-qrdb states for the encode side
// on BarrierInitializeRequest: "serializing a value means copying it, and the
// wire format owns that copy." Deserializing is the same trade in reverse. The
// claim worth making is the narrow one — the credential does not outlive the
// request under a name anything still holds — and that is the claim
// docs/barrier/api.md makes.
func wipeCredential(secret *[]byte) func() {
	return func() { secure.Wipe(*secret) }
}

// destroyCredentials destroys the protected-memory secret a barrier
// migration was handed. Credentials without a secret are a no-op. The
// destroy error is returned, not dropped: a secret that could not be
// destroyed is still resident and the caller must hear about it.
func destroyCredentials(creds qrdbsdk.Credentials) error {
	if creds.Secret == nil {
		return nil
	}
	return creds.Secret.Destroy()
}

// requireBarrierService writes a 503 Service Unavailable response when the
// barrier facade is unwired. Returns true when the service is present.
func (s *Server) requireBarrierService(w http.ResponseWriter, r *http.Request) bool {
	if s.barrierService() == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrBarrierServiceUnavailable)
		return false
	}
	return true
}

// readBarrierJSON decodes a JSON request body into the supplied destination.
// It returns false (after writing a 400 response) if the body cannot be
// decoded or read. EOF is treated as a valid empty body for endpoints with
// optional payloads — callers can supply a nil dst when no body is expected.
func readBarrierJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if dst == nil {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err != nil && !errors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return false
	}
	defer r.Body.Close()
	// The raw body holds the base64 text of any credential it carries.
	defer secure.Wipe(body)

	if len(body) == 0 {
		return true
	}
	if err := json.Unmarshal(body, dst); err != nil {
		var corrupt base64.CorruptInputError
		if errors.As(err, &corrupt) {
			RespondError(w, r, http.StatusBadRequest, &BarrierCredentialEncodingError{})
			return false
		}
		RespondBadRequest(w, r, "invalid JSON body")
		return false
	}
	return true
}

// barrierTimeFormat is the ONE rendering every barrier timestamp uses on
// every transport. It is RFC3339Nano, not RFC3339, because go-qrdb's own
// qrdb.v1 gRPC barrier surface — which quicbaas's unix and gRPC transports
// reach through the platform-shard servicer adapter — already renders
// InitializedAt at nanosecond precision. Rendering the same instant at
// second precision here made `barrier status` answer
// "2026-08-25T04:40:05Z" over REST/MCP and "2026-08-25T04:40:05.727835519Z"
// over unix/gRPC for one barrier: not a contradiction, but not
// byte-comparable either, and go-quicbaas#405 requires an operator
// diffing two transports to get the same answer.
const barrierTimeFormat = time.RFC3339Nano

// barrierStatusToResponse maps a BarrierStatusInfo to its JSON wire shape.
func barrierStatusToResponse(info *service.BarrierStatusInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"sealed":          info.Sealed,
		"initialized":     info.Initialized,
		"strategy":        info.Strategy,
		"hardware_backed": info.HardwareBacked,
	}
	if !info.InitializedAt.IsZero() {
		resp["initialized_at"] = info.InitializedAt.UTC().Format(barrierTimeFormat)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// HandleBarrierStatus handles GET /api/v1/barrier/status.
func (s *Server) HandleBarrierStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	info, err := s.barrierSvc.BarrierStatus(r.Context())
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, barrierStatusToResponse(info))
}

// HandleBarrierInitialize handles POST /api/v1/barrier/initialize.
func (s *Server) HandleBarrierInitialize(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierInitializeRequest
	defer wipeCredential(&req.Secret)()
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if err := s.barrierSvc.BarrierInitialize(r.Context(), req.Secret); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierUnseal handles POST /api/v1/barrier/unseal.
func (s *Server) HandleBarrierUnseal(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierUnsealRequest
	defer wipeCredential(&req.Secret)()
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if err := s.barrierSvc.BarrierUnseal(r.Context(), req.Secret); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierSeal handles POST /api/v1/barrier/seal.
func (s *Server) HandleBarrierSeal(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierSealRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	soPIN := []byte(req.SOPin)
	defer wipeCredential(&soPIN)()

	if err := s.barrierSvc.BarrierSeal(holderContext(r), &service.BarrierSealRequest{SOPIN: soPIN}); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierShamirInitialize handles POST /api/v1/barrier/shamir/initialize.
func (s *Server) HandleBarrierShamirInitialize(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierShamirInitializeRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Threshold <= 0 {
		RespondBadRequest(w, r, "threshold must be greater than 0")
		return
	}
	if req.TotalShares <= 0 {
		RespondBadRequest(w, r, "total_shares must be greater than 0")
		return
	}
	if req.Threshold > req.TotalShares {
		RespondBadRequest(w, r, "threshold cannot exceed total_shares")
		return
	}

	resp, err := s.barrierSvc.BarrierInitializeShamir(r.Context(), &req)
	var refused *sdkerrors.SystemShamirInitRefusedError
	if errors.As(err, &refused) {
		// The refusal names the ceremony; it is safe to show the caller.
		RespondError(w, r, http.StatusPreconditionFailed, err)
		return
	}
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBarrierRecoveryGenerate handles POST /api/v1/barrier/recovery/generate.
func (s *Server) HandleBarrierRecoveryGenerate(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierRecoveryGenerateRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Threshold <= 0 {
		RespondBadRequest(w, r, "threshold must be greater than 0")
		return
	}

	resp, err := s.barrierSvc.BarrierGenerateRecoveryKeys(r.Context(), &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBarrierRecoveryDelete handles DELETE /api/v1/barrier/recovery.
func (s *Server) HandleBarrierRecoveryDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	if err := s.barrierSvc.BarrierDeleteRecoveryKeys(r.Context()); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierRecoveryStatus handles GET /api/v1/barrier/recovery/status.
func (s *Server) HandleBarrierRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	hasKeys, err := s.barrierSvc.BarrierHasRecoveryKeys(r.Context())
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// The field is "has_keys", not "has_recovery_keys". The canonical client of
	// this route is go-qrdb's REST transport, which decodes the body into
	// transport.BarrierHasRecoveryKeysResponse (`json:"has_keys"`). The barrier
	// is qrdb's, so every transport here answers in the wire format qrdb's
	// client for THAT transport reads: REST "has_keys", MCP and gRPC
	// "has_recovery_keys" (go-quicbaas#991).
	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"has_keys": hasKeys,
	})
}

// HandleBarrierEnableEncryption handles
// POST /api/v1/barrier/enable-encryption.
//
// It migrates the system barrier from the none (unencrypted) strategy to a
// keyed strategy in place. The operation is driven through the embedded qrdb
// node's EncryptionMigrator (not the transport BarrierClient, which stubs
// this operation). Callers must supply the target strategy, mechanism, and
// availability; a passphrase or Shamir parameters are required for the
// respective target strategy.
func (s *Server) HandleBarrierEnableEncryption(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierEnableEncryptionRequest
	defer wipeCredential(&req.Passphrase)()
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.TenantID != "" {
		RespondBadRequest(w, r, "tenant_id is not accepted on this route: it migrates the system barrier; "+
			"use POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption for a tenant")
		return
	}
	if req.TargetStrategy == "" {
		RespondBadRequest(w, r, "target_strategy is required")
		return
	}

	mech := req.Mechanism
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := req.Availability
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}

	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(req.TargetStrategy),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: req.ShamirThreshold,
		ShamirTotal:     req.ShamirTotal,
	}
	if len(req.Passphrase) > 0 {
		creds, credErr := qrdbsdk.CredentialsFor(r.Context(), req.Passphrase)
		if credErr != nil {
			s.handleServiceError(w, r, credErr)
			return
		}
		opts.Credentials = creds
	}

	err := s.barrierSvc.EnableEncryption(r.Context(), "", opts)
	if destroyErr := destroyCredentials(opts.Credentials); destroyErr != nil && err == nil {
		err = destroyErr
	}
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierWireEnableEncryption handles the go-qrdb wire mount
// POST /v1/barrier/enable-encryption.
//
// It speaks go-qrdb's wire verbatim (go-quicbaas#990): the request is
// qrdbtransport.BarrierEnableEncryptionRequest — `strategy`, with a body
// `tenant_id` selecting a tenant's dedicated barrier and an empty one the
// system barrier — and the answer is 200 with a
// qrdbtransport.BarrierEnableEncryptionResponse, which is what go-qrdb's REST
// transport decodes. The quicbaas-owned /api/v1 route keeps `target_strategy`
// and 204, as do the gRPC and MCP surfaces.
//
// Because the body selects the scope, the route is registered identity-gated
// and this handler authorizes the scope the request selected
// (authorizeBarrierScope): seal:enable_encryption against the system tenant
// for the system barrier, tenant_barrier:enable_encryption against the named
// tenant for a tenant's. BarrierService.EnableEncryption then binds a
// tenant-scoped migration to the caller's own tenant, as on every transport.
func (s *Server) HandleBarrierWireEnableEncryption(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierWireEnableEncryptionRequest
	defer wipeCredential(&req.Passphrase)()
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Strategy == "" {
		RespondBadRequest(w, r, "strategy is required")
		return
	}
	if !s.authorizeBarrierScope(w, r, req.TenantID) {
		return
	}

	mech := req.Mechanism
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := req.Availability
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}

	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(req.Strategy),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: req.ShamirThreshold,
		ShamirTotal:     req.ShamirTotal,
	}
	if len(req.Passphrase) > 0 {
		creds, credErr := qrdbsdk.CredentialsFor(r.Context(), req.Passphrase)
		if credErr != nil {
			s.handleServiceError(w, r, credErr)
			return
		}
		opts.Credentials = creds
	}

	scope := barrierScopeSystem
	if req.TenantID != "" {
		scope = barrierScopeTenant
	}
	err := s.barrierSvc.EnableEncryption(r.Context(), req.TenantID, opts)
	if destroyErr := destroyCredentials(opts.Credentials); destroyErr != nil && err == nil {
		err = destroyErr
	}
	if err != nil {
		if req.TenantID != "" {
			s.handleTenantBarrierError(w, r, err)
			return
		}
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, qrdbtransport.BarrierEnableEncryptionResponse{
		Status: barrierMigrationStarted,
		Scope:  scope,
	})
}

// Permissions HandleBarrierWireEnableEncryption requires, per selected scope.
// They are the permissions the path-scoped siblings carry at registration:
// POST /api/v1/barrier/enable-encryption (system) and
// POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption (tenant).
const (
	barrierSystemEnableResource = "seal"
	barrierTenantEnableResource = "tenant_barrier"
	barrierEnableAction         = "enable_encryption"
)

// BarrierScopeAuthorizerUnavailableError reports that the scope authorizer
// POST /v1/barrier/enable-encryption depends on is not wired or not yet able
// to decide. The route fails closed on it (503) rather than admit a caller
// no decision was rendered for.
// BarrierCredentialEncodingError reports that a barrier credential field in a
// REST request body is not well-formed base64. Every barrier credential on
// this surface is bytes, so JSON carries it base64-encoded. The error names
// no field and echoes no input: the rejected text may be a raw credential.
type BarrierCredentialEncodingError struct{}

// Error implements the error interface.
func (e *BarrierCredentialEncodingError) Error() string {
	return "barrier credential must be base64-encoded bytes"
}

type BarrierScopeAuthorizerUnavailableError struct {
	// Reason says why no decision could be made.
	Reason string
}

// Error returns a human-readable description.
func (e *BarrierScopeAuthorizerUnavailableError) Error() string {
	if e.Reason == "" {
		return "barrier scope authorizer is not available"
	}
	return "barrier scope authorizer is not available: " + e.Reason
}

// authorizeBarrierScope authorizes the go-qrdb wire enable-encryption request
// for the scope its body selected, writing the refusal and returning false
// when the caller may not migrate that barrier.
//
// The route is registered identity-gated (routes_permissions_generated.go)
// because one registration-site permission cannot describe a route whose
// scope is in its body: seal:enable_encryption would refuse a tenant
// principal holding tenant_barrier:enable_encryption, and
// tenant_barrier:enable_encryption would admit a tenant principal to the
// SYSTEM barrier. The decision is go-qrdb's RBAC authorizer's, with the
// target tenant bound explicitly — never inherited from the request context —
// so a tenant principal cannot reach the system barrier or another tenant's.
func (s *Server) authorizeBarrierScope(w http.ResponseWriter, r *http.Request, tenantID string) bool {
	authz := s.barrierScopeAuthorizer()
	if authz == nil {
		RespondError(w, r, http.StatusServiceUnavailable,
			&BarrierScopeAuthorizerUnavailableError{Reason: "not configured"})
		return false
	}
	identity := qrdbsdk.IdentityFromContext(r.Context())
	if identity == nil {
		RespondError(w, r, http.StatusUnauthorized, &UnauthorizedError{Message: "authentication required"})
		return false
	}

	resource := barrierSystemEnableResource
	if tenantID != "" {
		resource = barrierTenantEnableResource
	}
	// An empty tenantID binds the system tenant (qrdbsdk.ContextWithTenant).
	ctx := qrdbsdk.ContextWithTenant(r.Context(), tenantID)
	if err := authz.Authorize(ctx, identity, resource, barrierEnableAction); err != nil {
		var unavailable *BarrierScopeAuthorizerUnavailableError
		if errors.As(err, &unavailable) {
			RespondError(w, r, http.StatusServiceUnavailable, unavailable)
			return false
		}
		RespondError(w, r, http.StatusForbidden, &ForbiddenError{
			Reason: resource + ":" + barrierEnableAction + " is required for the requested scope",
		})
		return false
	}
	return true
}
