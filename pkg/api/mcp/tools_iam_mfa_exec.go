//go:build mcp

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

package mcp

import (
	"context"
	"fmt"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// MFA Manager Access
// =============================================================================

// mfaManagerAccessor provides access to the MFA manager from the QuicBaaS service.
// The GetMFAManager method is available when built with the "iam" build tag.
type mfaManagerAccessor interface {
	GetMFAManager(ctx context.Context) (service.MFAManager, error)
}

// getMFAManager retrieves the MFA manager from the QuicBaaS service.
func (r *ToolRegistry) getMFAManager(ctx context.Context) (service.MFAManager, error) {
	accessor, ok := r.service.(mfaManagerAccessor)
	if !ok {
		return nil, &MFAServiceUnavailableError{Reason: "service does not support MFA"}
	}
	mgr, err := accessor.GetMFAManager(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get MFA manager", err)
	}
	if mgr == nil {
		return nil, &MFAServiceUnavailableError{Reason: "MFA service not configured"}
	}
	return mgr, nil
}

// =============================================================================
// Error Types
// =============================================================================

// MFAServiceUnavailableError indicates the MFA service is not available.
type MFAServiceUnavailableError struct {
	Reason string
}

// Error returns the error message.
func (e *MFAServiceUnavailableError) Error() string {
	return fmt.Sprintf("MFA service unavailable: %s", e.Reason)
}

// MFAInvalidInputError indicates invalid input for an MFA operation.
type MFAInvalidInputError struct {
	Field   string
	Message string
}

// Error returns the error message.
func (e *MFAInvalidInputError) Error() string {
	return fmt.Sprintf("MFA invalid input: %s: %s", e.Field, e.Message)
}

// PEPServiceUnavailableError indicates the PEP service is not available.
type PEPServiceUnavailableError struct {
	Reason string
}

// Error returns the error message.
func (e *PEPServiceUnavailableError) Error() string {
	return fmt.Sprintf("PEP service unavailable: %s", e.Reason)
}

// PEPInvalidInputError indicates invalid input for a PEP operation.
type PEPInvalidInputError struct {
	Field   string
	Message string
}

// Error returns the error message.
func (e *PEPInvalidInputError) Error() string {
	return fmt.Sprintf("PEP invalid input: %s: %s", e.Field, e.Message)
}

// =============================================================================
// MFA Enrollment Execution Handlers
// =============================================================================

// executeMFABeginEnroll begins MFA enrollment for a user.
func (r *ToolRegistry) executeMFABeginEnroll(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	userID := getString(args, "user_id")
	if userID == "" {
		return nil, &MFAInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &MFAInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	method := getString(args, "method")
	if method == "" {
		method = "totp" // Default to TOTP
	}

	resp, err := mgr.BeginEnrollment(ctx, userID, tenantID, iam.MFAMethod(method))
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// executeMFACompleteEnroll completes MFA enrollment by verifying the initial code.
func (r *ToolRegistry) executeMFACompleteEnroll(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	enrollmentID := getString(args, "enrollment_id")
	if enrollmentID == "" {
		return nil, &MFAInvalidInputError{Field: "enrollment_id", Message: "enrollment_id is required"}
	}

	code := getString(args, "code")
	if code == "" {
		return nil, &MFAInvalidInputError{Field: "code", Message: "code is required"}
	}

	err = mgr.CompleteEnrollment(ctx, enrollmentID, code)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"success": true,
		"message": "MFA enrollment completed successfully",
	})
}

// executeMFACancelEnroll cancels an in-progress MFA enrollment.
func (r *ToolRegistry) executeMFACancelEnroll(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	enrollmentID := getString(args, "enrollment_id")
	if enrollmentID == "" {
		return nil, &MFAInvalidInputError{Field: "enrollment_id", Message: "enrollment_id is required"}
	}

	err = mgr.CancelEnrollment(ctx, enrollmentID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"success": true,
		"message": "MFA enrollment cancelled",
	})
}

// =============================================================================
// MFA Status Execution Handlers
// =============================================================================

// executeMFAGetStatus gets MFA enrollment status for a user.
func (r *ToolRegistry) executeMFAGetStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	userID := getString(args, "user_id")
	if userID == "" {
		return nil, &MFAInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &MFAInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	status, err := mgr.GetMFAStatus(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	return marshalResult(status)
}

// executeMFADisable disables MFA for a user.
func (r *ToolRegistry) executeMFADisable(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	userID := getString(args, "user_id")
	if userID == "" {
		return nil, &MFAInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &MFAInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	err = mgr.DisableMFA(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"success": true,
		"message": "MFA disabled successfully",
	})
}

// =============================================================================
// MFA Challenge Execution Handlers
// =============================================================================

// executeMFABeginChallenge begins an MFA challenge during authentication.
func (r *ToolRegistry) executeMFABeginChallenge(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	userID := getString(args, "user_id")
	if userID == "" {
		return nil, &MFAInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &MFAInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	sessionToken := getString(args, "session_token")
	if sessionToken == "" {
		return nil, &MFAInvalidInputError{Field: "session_token", Message: "session_token is required"}
	}

	challenge, err := mgr.BeginChallenge(ctx, userID, tenantID, sessionToken)
	if err != nil {
		return nil, err
	}

	return marshalResult(challenge)
}

// executeMFAVerifyChallenge verifies an MFA challenge with an OTP code.
func (r *ToolRegistry) executeMFAVerifyChallenge(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	challengeID := getString(args, "challenge_id")
	if challengeID == "" {
		return nil, &MFAInvalidInputError{Field: "challenge_id", Message: "challenge_id is required"}
	}

	code := getString(args, "code")
	if code == "" {
		return nil, &MFAInvalidInputError{Field: "code", Message: "code is required"}
	}

	resp, err := mgr.VerifyChallenge(ctx, challengeID, code)
	if err != nil {
		return nil, err
	}

	return marshalResult(resp)
}

// =============================================================================
// MFA Backup Code Execution Handlers
// =============================================================================

// executeMFAGenerateBackupCodes generates backup codes for a user.
func (r *ToolRegistry) executeMFAGenerateBackupCodes(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	userID := getString(args, "user_id")
	if userID == "" {
		return nil, &MFAInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &MFAInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	codes, err := mgr.GenerateBackupCodes(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"codes": codes,
		"count": len(codes),
	})
}

// executeMFAVerifyBackupCode verifies a backup code for a user.
func (r *ToolRegistry) executeMFAVerifyBackupCode(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	userID := getString(args, "user_id")
	if userID == "" {
		return nil, &MFAInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &MFAInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	code := getString(args, "code")
	if code == "" {
		return nil, &MFAInvalidInputError{Field: "code", Message: "code is required"}
	}

	err = mgr.VerifyBackupCode(ctx, userID, tenantID, code)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"success": true,
		"message": "Backup code verified successfully",
	})
}

// executeMFAGetBackupCodeStatus gets backup code status for a user.
func (r *ToolRegistry) executeMFAGetBackupCodeStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mgr, err := r.getMFAManager(ctx)
	if err != nil {
		return nil, err
	}

	userID := getString(args, "user_id")
	if userID == "" {
		return nil, &MFAInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &MFAInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	status, err := mgr.GetBackupCodeStatus(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	return marshalResult(status)
}

// =============================================================================
// PEP Execution Handlers
// =============================================================================

// pepServiceAccessor provides access to the PEP (Policy Enforcement Point) service.
type pepServiceAccessor interface {
	GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
}

// executePEPCheck checks authorization for a single request.
func (r *ToolRegistry) executePEPCheck(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accessor, ok := r.service.(pepServiceAccessor)
	if !ok {
		return nil, &PEPServiceUnavailableError{Reason: "service does not support PEP"}
	}

	engine, err := accessor.GetPolicyEngine(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get policy engine", err)
	}
	if engine == nil {
		return nil, &PEPServiceUnavailableError{Reason: "policy engine not configured"}
	}

	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &PEPInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}

	principal := getString(args, "principal")
	if principal == "" {
		return nil, &PEPInvalidInputError{Field: "principal", Message: "principal is required"}
	}

	action := getString(args, "action")
	if action == "" {
		return nil, &PEPInvalidInputError{Field: "action", Message: "action is required"}
	}

	resource := getString(args, "resource")
	if resource == "" {
		return nil, &PEPInvalidInputError{Field: "resource", Message: "resource is required"}
	}

	checkReq := &iam.CheckRequest{
		Subject: iam.Subject{
			ID:       principal,
			TenantID: tenantID,
		},
		Action:   action,
		Resource: resource,
	}

	// Parse optional context
	if ctxRaw, ok := args["context"].(map[string]interface{}); ok {
		checkReq.Context = ctxRaw
	}

	checkResp, err := engine.Evaluate(ctx, checkReq)
	if err != nil {
		return nil, err
	}

	return marshalResult(checkResp)
}

// executePEPCheckBatch checks authorization for multiple requests.
func (r *ToolRegistry) executePEPCheckBatch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	accessor, ok := r.service.(pepServiceAccessor)
	if !ok {
		return nil, &PEPServiceUnavailableError{Reason: "service does not support PEP"}
	}

	engine, err := accessor.GetPolicyEngine(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get policy engine", err)
	}
	if engine == nil {
		return nil, &PEPServiceUnavailableError{Reason: "policy engine not configured"}
	}

	checksRaw, ok := args["checks"].([]interface{})
	if !ok {
		return nil, &PEPInvalidInputError{Field: "checks", Message: "checks array is required"}
	}

	results := make([]interface{}, 0, len(checksRaw))
	for _, checkRaw := range checksRaw {
		check, ok := checkRaw.(map[string]interface{})
		if !ok {
			continue
		}

		tenantID := getString(check, "tenant_id")
		principal := getString(check, "principal")
		action := getString(check, "action")
		resource := getString(check, "resource")

		checkReq := &iam.CheckRequest{
			Subject: iam.Subject{
				ID:       principal,
				TenantID: tenantID,
			},
			Action:   action,
			Resource: resource,
		}

		if ctxRaw, ok := check["context"].(map[string]interface{}); ok {
			checkReq.Context = ctxRaw
		}

		checkResp, evalErr := engine.Evaluate(ctx, checkReq)
		if evalErr != nil {
			results = append(results, map[string]interface{}{
				"principal": principal,
				"resource":  resource,
				"action":    action,
				"error":     evalErr.Error(),
			})
			continue
		}
		results = append(results, checkResp)
	}

	return marshalResult(map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}
