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
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// MFAManager interface defines MFA operations.
type MFAManager = service.MFAManager

// MFAEnrollRequest represents a request to begin MFA enrollment.
type MFAEnrollRequest struct {
	Method string `json:"method,omitempty"` // Optional, defaults to "totp"
}

// MFAEnrollResponse represents the response to begin MFA enrollment.
type MFAEnrollResponse struct {
	EnrollmentID    string `json:"enrollment_id"`
	ProvisioningURI string `json:"provisioning_uri"`
	Secret          string `json:"secret"`
	ExpiresAt       string `json:"expires_at"`
}

// MFACompleteRequest represents a request to complete MFA enrollment.
type MFACompleteRequest struct {
	EnrollmentID string `json:"enrollment_id"`
	Code         string `json:"code"`
}

// MFAChallengeRequest represents a request to begin an MFA challenge.
type MFAChallengeRequest struct {
	SessionToken string `json:"session_token"`
}

// MFAChallengeResponse represents the response to begin an MFA challenge.
type MFAChallengeResponse struct {
	ChallengeID string `json:"challenge_id"`
	Method      string `json:"method"`
	ExpiresAt   string `json:"expires_at"`
}

// MFAVerifyRequest represents a request to verify an MFA challenge.
type MFAVerifyRequest struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
}

// MFAVerifyResponse represents the response to verify an MFA challenge.
type MFAVerifyResponse struct {
	Success              bool   `json:"success"`
	Token                string `json:"token,omitempty"`
	BackupCodesRemaining int    `json:"backup_codes_remaining,omitempty"`
}

// MFAStatusResponse represents the MFA status for a user.
type MFAStatusResponse struct {
	Enabled              bool   `json:"enabled"`
	Method               string `json:"method,omitempty"`
	EnrolledAt           string `json:"enrolled_at,omitempty"`
	LastVerified         string `json:"last_verified,omitempty"`
	BackupCodesTotal     int    `json:"backup_codes_total,omitempty"`
	BackupCodesRemaining int    `json:"backup_codes_remaining,omitempty"`
}

// BackupCodesResponse represents backup codes for a user.
type BackupCodesResponse struct {
	Codes []string `json:"codes"`
}

// BackupCodeStatusResponse represents the status of backup codes.
type BackupCodeStatusResponse struct {
	TotalCodes     int     `json:"total_codes"`
	RemainingCodes int     `json:"remaining_codes"`
	GeneratedAt    *string `json:"generated_at,omitempty"`
}

// ErrMFANotAvailable is returned when the MFA service is not available.
var ErrMFANotAvailable = errors.New("MFA service not available")

// getMFAManager retrieves the MFA manager from the service.
func (h *IAMHandler) getMFAManager(ctx context.Context) (MFAManager, error) {
	mfaProvider, ok := h.server.service.(interface {
		GetMFAManager(ctx context.Context) (MFAManager, error)
	})
	if !ok {
		return nil, ErrMFANotAvailable
	}
	mgr, err := mfaProvider.GetMFAManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, ErrMFANotAvailable
	}
	return mgr, nil
}

// BeginMFAEnrollment handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll
// Begins the MFA enrollment process for a user.
func (h *IAMHandler) BeginMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}
	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	// Parse optional request body
	var req MFAEnrollRequest
	if r.Body != nil && r.ContentLength > 0 {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "failed to read request body"))
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				RespondIAMError(w, r, http.StatusBadRequest,
					NewIAMAPIError(ErrBadRequest, "invalid JSON"))
				return
			}
		}
	}

	// Default method to TOTP
	method := iam.MFAMethodTOTP
	if req.Method != "" {
		method = iam.MFAMethod(req.Method)
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	resp, err := mfaMgr.BeginEnrollment(ctx, userID, tenantID, method)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, MFAEnrollResponse{
		EnrollmentID:    resp.EnrollmentID,
		ProvisioningURI: resp.QRCodeURI,
		Secret:          resp.Secret,
		ExpiresAt:       resp.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// CompleteMFAEnrollment handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify
// Completes the MFA enrollment by verifying the code.
func (h *IAMHandler) CompleteMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")

	if tenantID == "" || userIDParam == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID and user ID are required"))
		return
	}
	if _, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id"); !ok {
		return
	}
	if _, ok := requireUserIDParam(w, r); !ok {
		return
	}

	var req MFACompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON"))
		return
	}

	if req.EnrollmentID == "" || req.Code == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "enrollment_id and code are required"))
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	if err := mfaMgr.CompleteEnrollment(ctx, req.EnrollmentID, req.Code); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]bool{"success": true})
}

// CancelMFAEnrollment handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll/{enrollment_id}
// Cancels a pending MFA enrollment.
func (h *IAMHandler) CancelMFAEnrollment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")
	enrollmentID := chi.URLParam(r, "enrollment_id")

	if tenantID == "" || userIDParam == "" || enrollmentID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID, user ID, and enrollment ID are required"))
		return
	}
	if _, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id"); !ok {
		return
	}
	if _, ok := requireUserIDParam(w, r); !ok {
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	if err := mfaMgr.CancelEnrollment(ctx, enrollmentID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetMFAStatus handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/mfa
// Returns the MFA status for a user.
func (h *IAMHandler) GetMFAStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")

	if tenantID == "" || userIDParam == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID and user ID are required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}
	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	status, err := mfaMgr.GetMFAStatus(ctx, userID, tenantID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	resp := MFAStatusResponse{
		Enabled:              status.Enabled,
		Method:               string(status.Method),
		BackupCodesTotal:     status.BackupCodesTotal,
		BackupCodesRemaining: status.BackupCodesRemaining,
	}
	if status.EnrolledAt != nil {
		resp.EnrolledAt = status.EnrolledAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if status.LastVerified != nil {
		resp.LastVerified = status.LastVerified.Format("2006-01-02T15:04:05Z07:00")
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// DisableMFA handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/mfa
// Disables MFA for a user.
func (h *IAMHandler) DisableMFA(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")

	if tenantID == "" || userIDParam == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID and user ID are required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}
	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	if err := mfaMgr.DisableMFA(ctx, userID, tenantID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// BeginMFAChallenge handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/challenge
// Begins an MFA challenge for authentication.
func (h *IAMHandler) BeginMFAChallenge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")

	if tenantID == "" || userIDParam == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID and user ID are required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}
	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	var req MFAChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON"))
		return
	}

	if req.SessionToken == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "session_token is required"))
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	challenge, err := mfaMgr.BeginChallenge(ctx, userID, tenantID, req.SessionToken)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, MFAChallengeResponse{
		ChallengeID: challenge.ID,
		Method:      string(challenge.Method),
		ExpiresAt:   challenge.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// VerifyMFAChallenge handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify-challenge
// Verifies an MFA challenge.
func (h *IAMHandler) VerifyMFAChallenge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")

	if tenantID == "" || userIDParam == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID and user ID are required"))
		return
	}
	if _, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id"); !ok {
		return
	}
	if _, ok := requireUserIDParam(w, r); !ok {
		return
	}

	var req MFAVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON"))
		return
	}

	if req.ChallengeID == "" || req.Code == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "challenge_id and code are required"))
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	resp, err := mfaMgr.VerifyChallenge(ctx, req.ChallengeID, req.Code)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, MFAVerifyResponse{
		Success:              resp.Success,
		Token:                resp.Token,
		BackupCodesRemaining: resp.BackupCodesRemaining,
	})
}

// GenerateBackupCodes handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes
// Generates new backup codes for a user (regenerates if already exist).
func (h *IAMHandler) GenerateBackupCodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")

	if tenantID == "" || userIDParam == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID and user ID are required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}
	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	codes, err := mfaMgr.GenerateBackupCodes(ctx, userID, tenantID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, BackupCodesResponse{
		Codes: codes,
	})
}

// GetBackupCodeStatus handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes
// Returns the status of backup codes for a user.
func (h *IAMHandler) GetBackupCodeStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := chi.URLParam(r, "tenant_id")
	userIDParam := chi.URLParam(r, "user_id")

	if tenantID == "" || userIDParam == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID and user ID are required"))
		return
	}
	tenantID, ok := h.server.urlTenant(w, r, "rest.IAM", "tenant_id")
	if !ok {
		return
	}
	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}

	mfaMgr, err := h.getMFAManager(ctx)
	if err != nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}

	status, err := mfaMgr.GetBackupCodeStatus(ctx, userID, tenantID)
	if err != nil {
		// If user has no backup codes yet, return zeros instead of an error.
		//
		// IsMFANotEnrolled, not errors.Is: errors.Is compares the QBError CODE,
		// so ANY not-found the MFA store could raise -- an absent user, a
		// backend that answered 404 -- was answered with a 200 reporting zero
		// backup codes. A failure to READ the enrollment reported as its
		// absence tells the operator the account has no recovery codes when
		// nothing established that (go-quicbaas#434).
		if iam.IsMFANotEnrolled(err) {
			RespondJSON(w, r, http.StatusOK, BackupCodeStatusResponse{
				TotalCodes:     0,
				RemainingCodes: 0,
			})
			return
		}
		h.server.handleServiceError(w, r, err)
		return
	}

	// Format generated_at if available
	var generatedAt *string
	if !status.GeneratedAt.IsZero() {
		formatted := status.GeneratedAt.Format("2006-01-02T15:04:05Z07:00")
		generatedAt = &formatted
	}

	RespondJSON(w, r, http.StatusOK, BackupCodeStatusResponse{
		TotalCodes:     status.Total,
		RemainingCodes: status.Remaining,
		GeneratedAt:    generatedAt,
	})
}
