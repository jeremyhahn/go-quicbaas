//go:build quic

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

package quic

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// barrierTenantGovernancer applies engine-side quota and entitlement limits to
// tenants via the iam.DefaultTenantManager delegate. Defined locally to avoid
// an import cycle with pkg/iam from the quic handler package.
type barrierTenantGovernancer interface {
	UpdateEngineQuotas(ctx context.Context, id string, quotas qrdbsdk.Quotas) error
	UpdateEngineEntitlements(ctx context.Context, id string, entitlements qrdbsdk.Entitlements) error
}

// =============================================================================
// Barrier QUIC handlers — OpBarrier (0xC0) sub-op dispatcher.
//
// Mirrors the /api/v1/barrier REST surface exposed by
// pkg/api/rest/handlers_barrier.go (17 methods) and the gRPC barrier service.
// Every handler delegates to the BarrierServicer facade wired into the QUIC
// server at lifecycle time. Until the facade is installed, every sub-handler
// short-circuits with a typed ServiceUnavailableError ("barrier") which maps
// to wire status StatusUnavailable on the protocol.
//
// Wire format for OpBarrier requests:
//
//	[opcode=OpBarrier][tokenLen][token][payloadLen][subOp:1][body...]
//
// The first byte of the QUIC payload identifies the barrier sub-operation
// (see SubOpBarrier* constants in protocol.go); the remaining bytes carry the
// JSON-encoded request body matching the wire request types declared in this
// file. Responses use the same JSON encoding as REST, less the HTTP envelope.
// =============================================================================

// ---------------------------------------------------------------------------
// Wire request types (local to this file; JSON shape only).
//
// These intentionally mirror the unexported REST request types
// (pkg/api/rest/handlers_barrier.go) so REST and QUIC clients share an
// identical body schema and round-trip the same JSON bytes.
// ---------------------------------------------------------------------------

// barrierInitializeWireRequest is the JSON payload for SubOpBarrierInitialize.
type barrierInitializeWireRequest struct {
	Secret string `json:"secret"`
}

// barrierUnsealWireRequest is the JSON payload for SubOpBarrierUnseal.
type barrierUnsealWireRequest struct {
	Secret string `json:"secret"`
}

// barrierShamirInitializeWireRequest is the JSON payload for
// SubOpBarrierShamirInitialize.
type barrierShamirInitializeWireRequest struct {
	Secret      string `json:"secret"`
	Threshold   int    `json:"threshold"`
	TotalShares int    `json:"total_shares"`
}

// barrierShamirUnsealShareWireRequest is the JSON payload for
// SubOpBarrierShamirUnsealShare.
type barrierShamirUnsealShareWireRequest struct {
	Share string `json:"share"`
}

// barrierShamirUnsealSharesWireRequest is the JSON payload for
// SubOpBarrierShamirUnsealShares.
type barrierShamirUnsealSharesWireRequest struct {
	Shares []string `json:"shares"`
}

// barrierRekeyWireRequest is the JSON payload for SubOpBarrierRekey.
type barrierRekeyWireRequest struct {
	Threshold int `json:"threshold"`
	Total     int `json:"total"`
}

// barrierRecoveryGenerateWireRequest is the JSON payload for
// SubOpBarrierRecoveryGenerate.
type barrierRecoveryGenerateWireRequest struct {
	Threshold int `json:"threshold"`
	Total     int `json:"total"`
}

// barrierRecoveryRecoverWireRequest is the JSON payload for
// SubOpBarrierRecoveryRecover.
type barrierRecoveryRecoverWireRequest struct {
	Keys []string `json:"keys"`
}

// barrierRootTokenWireRequest is the JSON payload for SubOpBarrierRootToken.
type barrierRootTokenWireRequest struct {
	Shares []string `json:"shares"`
}

// barrierEnableEncryptionWireRequest is the JSON payload for
// SubOpBarrierEnableEncryption. It mirrors the REST
// barrierEnableEncryptionRequest body exactly.
type barrierEnableEncryptionWireRequest struct {
	TargetStrategy  string `json:"target_strategy"`
	Mechanism       string `json:"mechanism"`
	Availability    string `json:"availability"`
	Passphrase      string `json:"passphrase,omitempty"`
	ShamirThreshold int    `json:"shamir_threshold,omitempty"`
	ShamirTotal     int    `json:"shamir_total,omitempty"`
}

// tenantUpdateQuotasWireRequest is the JSON payload for
// SubOpBarrierUpdateTenantQuotas. It carries the tenant ID and the full
// quota struct; zero-value fields mean "use system default" on the engine side.
type tenantUpdateQuotasWireRequest struct {
	TenantID string         `json:"tenant_id"`
	Quotas   qrdbsdk.Quotas `json:"quotas"`
}

// tenantUpdateEntitlementsWireRequest is the JSON payload for
// SubOpBarrierUpdateTenantEntitlements. It carries the tenant ID and the full
// entitlements struct.
type tenantUpdateEntitlementsWireRequest struct {
	TenantID     string               `json:"tenant_id"`
	Entitlements qrdbsdk.Entitlements `json:"entitlements"`
}

// ---------------------------------------------------------------------------
// Wire response types
//
// Response bodies mirror the REST envelopes byte-for-byte.
// ---------------------------------------------------------------------------

// barrierStatusWireResponse mirrors barrierStatusToResponse from the REST
// handler. InitializedAt is RFC3339 in UTC; empty when zero.
type barrierStatusWireResponse struct {
	Sealed         bool   `json:"sealed"`
	Strategy       string `json:"strategy"`
	HardwareBacked bool   `json:"hardware_backed"`
	InitializedAt  string `json:"initialized_at,omitempty"`
}

// shamirInitWireResponse mirrors shamirInitToResponse. Share material is
// never returned (qrdb share-free contract); ShareCount reports how many
// shares were sealed to enrolled Security Officer enclaves.
type shamirInitWireResponse struct {
	Threshold   int `json:"threshold"`
	TotalShares int `json:"total_shares"`
	ShareCount  int `json:"share_count"`
}

// unsealShareProgressWireResponse mirrors unsealShareProgressToResponse.
type unsealShareProgressWireResponse struct {
	Required  int  `json:"required"`
	Submitted int  `json:"submitted"`
	Complete  bool `json:"complete"`
}

// recoveryKeysWireResponse mirrors recoveryKeysToResponse. Recovery key
// material is never returned (qrdb share-free contract); ShareCount reports
// how many recovery shares were sealed to enrolled recovery officers.
type recoveryKeysWireResponse struct {
	Threshold  int `json:"threshold"`
	Total      int `json:"total"`
	ShareCount int `json:"share_count"`
}

// recoveryStatusWireResponse mirrors the JSON shape returned by
// HandleBarrierRecoveryStatus.
type recoveryStatusWireResponse struct {
	HasRecoveryKeys bool `json:"has_recovery_keys"`
}

// rootTokenWireResponse mirrors rootTokenToResponse. CreatedAt is RFC3339
// UTC; empty when zero.
type rootTokenWireResponse struct {
	Token     string `json:"token"`
	CreatedAt string `json:"created_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

// handleBarrierDispatch routes barrier operations based on the sub-opcode in
// the first byte of the QUIC request payload. This mirrors the scheduler and
// orchestrator dispatch patterns: the wire opcode is OpBarrier (0xC0) and
// payload[0] selects one of the SubOpBarrier* operations declared in
// protocol.go.
//
// On dispatch failure the request is rejected with StatusInvalidArgument
// (missing or unknown sub-opcode).
func (h *Handler) handleBarrierDispatch(ctx context.Context, req *Request) *Response {
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleBarrierDispatch", "barrier"))
	}

	subOpcode := req.Payload[0]
	subReq := &Request{
		Opcode:  req.Opcode,
		Token:   req.Token,
		Payload: req.Payload[1:],
	}

	handler, ok := barrierSubOpHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleBarrierDispatch", "barrier", subOpcode))
	}
	return handler(h, ctx, subReq)
}

// barrierSubOpHandler is the dispatch function signature for barrier
// sub-operations. Using a typed function avoids any reflection overhead.
type barrierSubOpHandler func(h *Handler, ctx context.Context, req *Request) *Response

// barrierSubOpHandlers maps barrier sub-opcodes to their dispatch handlers.
// This provides O(1) constant-time routing identical to the scheduler and
// orchestrator dispatch tables.
var barrierSubOpHandlers = map[byte]barrierSubOpHandler{
	SubOpBarrierStatus:                   (*Handler).handleBarrierStatus,
	SubOpBarrierInitialize:               (*Handler).handleBarrierInitialize,
	SubOpBarrierUnseal:                   (*Handler).handleBarrierUnseal,
	SubOpBarrierSeal:                     (*Handler).handleBarrierSeal,
	SubOpBarrierShamirInitialize:         (*Handler).handleBarrierShamirInitialize,
	SubOpBarrierShamirUnsealShare:        (*Handler).handleBarrierShamirUnsealShare,
	SubOpBarrierShamirUnsealShares:       (*Handler).handleBarrierShamirUnsealShares,
	SubOpBarrierRekey:                    (*Handler).handleBarrierRekey,
	SubOpBarrierRecoveryGenerate:         (*Handler).handleBarrierRecoveryGenerate,
	SubOpBarrierRecoveryRecover:          (*Handler).handleBarrierRecoveryRecover,
	SubOpBarrierRecoveryDelete:           (*Handler).handleBarrierRecoveryDelete,
	SubOpBarrierRecoveryStatus:           (*Handler).handleBarrierRecoveryStatus,
	SubOpBarrierRootToken:                (*Handler).handleBarrierRootToken,
	SubOpBarrierEnableEncryption:         (*Handler).handleBarrierEnableEncryption,
	SubOpBarrierUpdateTenantQuotas:       (*Handler).handleBarrierUpdateTenantQuotas,
	SubOpBarrierUpdateTenantEntitlements: (*Handler).handleBarrierUpdateTenantEntitlements,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireBarrierService returns the wired BarrierServicer or an error
// response carrying StatusUnavailable when the facade has not been installed.
func (h *Handler) requireBarrierService() (service.BarrierServicer, *Response) {
	if h.barrierService == nil {
		return nil, h.errorResponse(api.NewServiceUnavailableError("barrier"))
	}
	return h.barrierService, nil
}

// formatRFC3339 returns a UTC RFC3339 formatted timestamp, or "" if zero.
func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// Handlers (17 — REST parity)
// ---------------------------------------------------------------------------

// handleBarrierStatus returns the current barrier state.
// REST equivalent: GET /api/v1/barrier/status.
func (h *Handler) handleBarrierStatus(ctx context.Context, _ *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	info, err := svc.BarrierStatus(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&barrierStatusWireResponse{
		Sealed:         info.Sealed,
		Strategy:       info.Strategy,
		HardwareBacked: info.HardwareBacked,
		InitializedAt:  formatRFC3339(info.InitializedAt),
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleBarrierInitialize initialises the barrier with a single passphrase.
// REST equivalent: POST /api/v1/barrier/initialize.
func (h *Handler) handleBarrierInitialize(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierInitializeWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}

	if err := svc.BarrierInitialize(ctx, body.Secret); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierUnseal unseals the barrier with a single passphrase.
// REST equivalent: POST /api/v1/barrier/unseal.
func (h *Handler) handleBarrierUnseal(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierUnsealWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}

	if err := svc.BarrierUnseal(ctx, body.Secret); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierSeal transitions the barrier into the sealed state.
// REST equivalent: POST /api/v1/barrier/seal.
func (h *Handler) handleBarrierSeal(ctx context.Context, _ *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	if err := svc.BarrierSeal(ctx); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierShamirInitialize initialises the barrier with Shamir secret
// sharing and returns the generated shares.
// REST equivalent: POST /api/v1/barrier/shamir/initialize.
func (h *Handler) handleBarrierShamirInitialize(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierShamirInitializeWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Threshold <= 0 {
		return h.errorResponse(api.NewInvalidFormatMessageError("threshold", "must be greater than 0"))
	}
	if body.TotalShares <= 0 {
		return h.errorResponse(api.NewInvalidFormatMessageError("total_shares", "must be greater than 0"))
	}
	if body.Threshold > body.TotalShares {
		return h.errorResponse(api.NewInvalidFormatMessageError("threshold", "cannot exceed total_shares"))
	}

	info, err := svc.BarrierInitializeShamir(ctx, body.Secret, body.Threshold, body.TotalShares)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&shamirInitWireResponse{
		Threshold:   info.Threshold,
		TotalShares: info.TotalShares,
		ShareCount:  info.ShareCount,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleBarrierShamirUnsealShare submits a single Shamir share toward the
// unseal quorum and returns the current progress.
// REST equivalent: POST /api/v1/barrier/shamir/unseal-share.
func (h *Handler) handleBarrierShamirUnsealShare(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierShamirUnsealShareWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Share == "" {
		return h.errorResponse(api.NewRequiredFieldError("share"))
	}

	progress, err := svc.BarrierUnsealWithShare(ctx, body.Share)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&unsealShareProgressWireResponse{
		Required:  progress.Required,
		Submitted: progress.Submitted,
		Complete:  progress.Complete,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleBarrierShamirUnsealShares unseals the barrier with a complete set
// of Shamir shares in a single call.
// REST equivalent: POST /api/v1/barrier/shamir/unseal-shares.
func (h *Handler) handleBarrierShamirUnsealShares(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierShamirUnsealSharesWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if len(body.Shares) == 0 {
		return h.errorResponse(api.NewRequiredFieldError("shares"))
	}

	if err := svc.BarrierUnsealWithShares(ctx, body.Shares); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierRekey generates new Shamir shares for the existing root key.
// REST equivalent: POST /api/v1/barrier/rekey.
func (h *Handler) handleBarrierRekey(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierRekeyWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Threshold <= 0 {
		return h.errorResponse(api.NewInvalidFormatMessageError("threshold", "must be greater than 0"))
	}
	if body.Total <= 0 {
		return h.errorResponse(api.NewInvalidFormatMessageError("total", "must be greater than 0"))
	}
	if body.Threshold > body.Total {
		return h.errorResponse(api.NewInvalidFormatMessageError("threshold", "cannot exceed total"))
	}

	info, err := svc.BarrierRekey(ctx, body.Threshold, body.Total)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&shamirInitWireResponse{
		Threshold:   info.Threshold,
		TotalShares: info.TotalShares,
		ShareCount:  info.ShareCount,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleBarrierRecoveryGenerate generates recovery shares from the root key
// for disaster recovery.
// REST equivalent: POST /api/v1/barrier/recovery/generate.
func (h *Handler) handleBarrierRecoveryGenerate(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierRecoveryGenerateWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.Threshold <= 0 {
		return h.errorResponse(api.NewInvalidFormatMessageError("threshold", "must be greater than 0"))
	}
	if body.Total <= 0 {
		return h.errorResponse(api.NewInvalidFormatMessageError("total", "must be greater than 0"))
	}
	if body.Threshold > body.Total {
		return h.errorResponse(api.NewInvalidFormatMessageError("threshold", "cannot exceed total"))
	}

	info, err := svc.BarrierGenerateRecoveryKeys(ctx, body.Threshold, body.Total)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&recoveryKeysWireResponse{
		Threshold:  info.Threshold,
		Total:      info.Total,
		ShareCount: info.ShareCount,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleBarrierRecoveryRecover unseals the barrier using recovery shares.
// REST equivalent: POST /api/v1/barrier/recovery/recover.
func (h *Handler) handleBarrierRecoveryRecover(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierRecoveryRecoverWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if len(body.Keys) == 0 {
		return h.errorResponse(api.NewRequiredFieldError("keys"))
	}

	if err := svc.BarrierRecoverWithKeys(ctx, body.Keys); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierRecoveryDelete removes recovery key metadata.
// REST equivalent: DELETE /api/v1/barrier/recovery.
func (h *Handler) handleBarrierRecoveryDelete(ctx context.Context, _ *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	if err := svc.BarrierDeleteRecoveryKeys(ctx); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierRecoveryStatus reports whether recovery key metadata exists.
// REST equivalent: GET /api/v1/barrier/recovery/status.
func (h *Handler) handleBarrierRecoveryStatus(ctx context.Context, _ *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	hasKeys, err := svc.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&recoveryStatusWireResponse{HasRecoveryKeys: hasKeys})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// handleBarrierRootToken creates a new root token by proving knowledge of
// the master key through Shamir shares.
// REST equivalent: POST /api/v1/barrier/root-token.
func (h *Handler) handleBarrierRootToken(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierRootTokenWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if len(body.Shares) == 0 {
		return h.errorResponse(api.NewRequiredFieldError("shares"))
	}

	info, err := svc.BarrierGenerateRootToken(ctx, body.Shares)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&rootTokenWireResponse{
		Token:     info.Token,
		CreatedAt: formatRFC3339(info.CreatedAt),
	})
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}

// requireGovernance returns the wired barrierTenantGovernancer or an error
// response carrying StatusUnavailable when the facade has not been installed.
func (h *Handler) requireGovernance() (barrierTenantGovernancer, *Response) {
	if h.governance == nil {
		return nil, h.errorResponse(api.NewServiceUnavailableError("governance"))
	}
	return h.governance, nil
}

// handleBarrierEnableEncryption migrates the system barrier from the none
// strategy to a keyed at-rest encryption strategy. This is a one-way,
// in-place migration delegated to the embedded qrdb node.
// REST equivalent: POST /api/v1/barrier/enable-encryption.
func (h *Handler) handleBarrierEnableEncryption(ctx context.Context, req *Request) *Response {
	svc, errResp := h.requireBarrierService()
	if errResp != nil {
		return errResp
	}

	var body barrierEnableEncryptionWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.TargetStrategy == "" {
		return h.errorResponse(api.NewRequiredFieldError("target_strategy"))
	}

	mech := body.Mechanism
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := body.Availability
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}

	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(body.TargetStrategy),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: body.ShamirThreshold,
		ShamirTotal:     body.ShamirTotal,
	}
	if body.Passphrase != "" {
		opts.Credentials = qrdbsdk.Credentials{Secret: []byte(body.Passphrase)}
	}

	// tenantID="" routes to the system barrier scope inside EnableEncryption.
	if err := svc.EnableEncryption(ctx, "", opts); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierUpdateTenantQuotas applies engine-level numeric resource limits
// to the identified tenant. Delegates to the barrierTenantGovernancer facade
// (iam.DefaultTenantManager). Returns StatusOK (empty body) on success.
// REST equivalent: PATCH /api/v1/tenants/{id}/quotas.
func (h *Handler) handleBarrierUpdateTenantQuotas(ctx context.Context, req *Request) *Response {
	gov, errResp := h.requireGovernance()
	if errResp != nil {
		return errResp
	}

	var body tenantUpdateQuotasWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.TenantID == "" {
		return h.errorResponse(api.NewRequiredFieldError("tenant_id"))
	}

	if err := gov.UpdateEngineQuotas(ctx, body.TenantID, body.Quotas); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}

// handleBarrierUpdateTenantEntitlements applies engine-level feature
// entitlements to the identified tenant. Delegates to the
// barrierTenantGovernancer facade (iam.DefaultTenantManager). Returns StatusOK
// (empty body) on success.
// REST equivalent: PATCH /api/v1/tenants/{id}/entitlements.
func (h *Handler) handleBarrierUpdateTenantEntitlements(ctx context.Context, req *Request) *Response {
	gov, errResp := h.requireGovernance()
	if errResp != nil {
		return errResp
	}

	var body tenantUpdateEntitlementsWireRequest
	if err := DecodePayload(req.Payload, &body); err != nil {
		return h.errorResponse(err)
	}
	if body.TenantID == "" {
		return h.errorResponse(api.NewRequiredFieldError("tenant_id"))
	}

	if err := gov.UpdateEngineEntitlements(ctx, body.TenantID, body.Entitlements); err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK}
}
