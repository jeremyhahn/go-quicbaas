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

package grpc

import (
	"context"
	"time"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// PEP Service handlers

// handlePEPCheck handles PEP Check requests.
func (h *iamHandler) handlePEPCheck(ctx context.Context, client iampb.PEPServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handlePEPCheck", "invalid request type")
	}

	// Extract from nested "request" field if present (SDK wraps in checkRequestInternal).
	if nested, ok := reqMap["request"].(map[string]interface{}); ok {
		reqMap = nested
	}

	subjectMap, _ := getMap(reqMap, "subject")
	action, _ := getString(reqMap, "action")
	resource, _ := getString(reqMap, "resource")
	contextMap, _ := getStringMap(reqMap, "context")

	var subject *iampb.Subject
	if subjectMap != nil {
		subject = extractSubjectFromMap(subjectMap)
	}

	pbReq := &iampb.CheckRequest{
		Subject:  subject,
		Action:   action,
		Resource: resource,
		Context:  contextMap,
	}

	resp, err := client.Check(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handlePEPCheck")
	}

	return &iam.CheckResponse{
		Allowed:          resp.Allowed,
		MatchedPolicy:    resp.MatchedPolicy,
		MatchedRule:      resp.MatchedRule,
		EvaluationTimeNs: resp.EvaluationTimeNs,
		Timestamp:        time.Now(),
	}, nil
}

// handlePEPCheckBatch handles PEP CheckBatch requests.
func (h *iamHandler) handlePEPCheckBatch(ctx context.Context, client iampb.PEPServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handlePEPCheckBatch", "invalid request type")
	}

	// Extract from nested "request" field if present (SDK wraps in checkBatchRequestInternal).
	if nested, ok := reqMap["request"].(map[string]interface{}); ok {
		reqMap = nested
	}

	var subject *iampb.Subject
	var action string
	var resources []string
	var contextMap map[string]string

	// Check for SDK's CheckBatchRequest format with requests array.
	if requestsVal, ok := reqMap["requests"]; ok {
		// Handle as array of requests.
		var requests []interface{}
		switch r := requestsVal.(type) {
		case []interface{}:
			requests = r
		case []map[string]interface{}:
			for _, m := range r {
				requests = append(requests, m)
			}
		}

		if len(requests) > 0 {
			// Extract subject and action from first request.
			if firstReq, ok := requests[0].(map[string]interface{}); ok {
				if subjectMap, ok := getMap(firstReq, "subject"); ok && subjectMap != nil {
					subject = extractSubjectFromMap(subjectMap)
				}
				action, _ = getString(firstReq, "action")
				contextMap, _ = getStringMap(firstReq, "context")
			}

			// Collect all resources from all requests.
			for _, r := range requests {
				if reqItem, ok := r.(map[string]interface{}); ok {
					if resource, ok := getString(reqItem, "resource"); ok {
						resources = append(resources, resource)
					}
				}
			}
		}
	} else {
		// Fall back to proto format (subject/action/resources at top level).
		subjectMap, _ := getMap(reqMap, "subject")
		if subjectMap != nil {
			subject = extractSubjectFromMap(subjectMap)
		}
		action, _ = getString(reqMap, "action")
		resources, _ = getStringSlice(reqMap, "resources")
		contextMap, _ = getStringMap(reqMap, "context")
	}

	stopOnDeny, _ := getBool(reqMap, "stop_on_deny")

	pbReq := &iampb.CheckBatchRequest{
		Subject:    subject,
		Action:     action,
		Resources:  resources,
		Context:    contextMap,
		StopOnDeny: stopOnDeny,
	}

	resp, err := client.CheckBatch(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handlePEPCheckBatch")
	}

	// Convert proto results ([]bool) to SDK CheckResponse slice.
	results := make([]iam.CheckResponse, 0, len(resp.Results))
	allAllowed := true
	allowedCount := 0
	deniedCount := 0
	for _, allowed := range resp.Results {
		cr := iam.CheckResponse{
			Allowed: allowed,
		}
		results = append(results, cr)
		if allowed {
			allowedCount++
		} else {
			allAllowed = false
			deniedCount++
		}
	}

	return &iam.CheckBatchResponse{
		Results:               results,
		AllAllowed:            allAllowed,
		AllowedCount:          allowedCount,
		DeniedCount:           deniedCount,
		TotalEvaluationTimeNs: resp.EvaluationTimeNs,
	}, nil
}

// extractSubjectFromMap extracts a Subject proto from a map representation.
func extractSubjectFromMap(subjectMap map[string]interface{}) *iampb.Subject {
	subjectID, _ := getString(subjectMap, "id")
	subjectTenantID, _ := getString(subjectMap, "tenant_id")
	roles, _ := getStringSlice(subjectMap, "roles")
	groups, _ := getStringSlice(subjectMap, "groups")
	attributes, _ := getStringMap(subjectMap, "attributes")
	mfaVerified, _ := getBool(subjectMap, "mfa_verified")

	return &iampb.Subject{
		Id:          subjectID,
		TenantId:    subjectTenantID,
		Roles:       roles,
		Groups:      groups,
		Attributes:  attributes,
		MfaVerified: mfaVerified,
	}
}

// MFA Service handlers

// handleBeginMFAEnrollment handles BeginMFAEnrollment requests.
func (h *iamHandler) handleBeginMFAEnrollment(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleBeginMFAEnrollment", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	method, _ := getInt(reqMap, "method")

	pbReq := &iampb.BeginMFAEnrollmentRequest{
		TenantId: tenantID,
		UserId:   userID,
		Method:   iampb.MFAMethod(method),
	}

	resp, err := client.BeginEnrollment(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleBeginMFAEnrollment")
	}

	enrollment := &cli.MFAEnrollment{
		EnrollmentID:    resp.EnrollmentId,
		Secret:          resp.Secret,
		ProvisioningURI: resp.ProvisioningUri,
	}

	if resp.ExpiresAt != nil {
		enrollment.ExpiresAt = resp.ExpiresAt.AsTime()
	}

	return enrollment, nil
}

// handleCompleteMFAEnrollment handles CompleteMFAEnrollment requests.
func (h *iamHandler) handleCompleteMFAEnrollment(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCompleteMFAEnrollment", "invalid request type")
	}

	enrollmentID, _ := getString(reqMap, "enrollment_id")
	code, _ := getString(reqMap, "code")

	pbReq := &iampb.CompleteMFAEnrollmentRequest{
		EnrollmentId: enrollmentID,
		Code:         code,
	}

	resp, err := client.CompleteEnrollment(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCompleteMFAEnrollment")
	}

	return map[string]interface{}{
		"success":      resp.Success,
		"backup_codes": resp.BackupCodes,
	}, nil
}

// handleCancelMFAEnrollment handles CancelMFAEnrollment requests.
func (h *iamHandler) handleCancelMFAEnrollment(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCancelMFAEnrollment", "invalid request type")
	}

	enrollmentID, _ := getString(reqMap, "enrollment_id")

	pbReq := &iampb.CancelMFAEnrollmentRequest{
		EnrollmentId: enrollmentID,
	}

	resp, err := client.CancelEnrollment(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCancelMFAEnrollment")
	}

	return map[string]interface{}{"success": resp.Success}, nil
}

// handleBeginMFAChallenge handles BeginMFAChallenge requests.
func (h *iamHandler) handleBeginMFAChallenge(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleBeginMFAChallenge", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	sessionToken, _ := getString(reqMap, "session_token")

	pbReq := &iampb.BeginMFAChallengeRequest{
		TenantId:     tenantID,
		UserId:       userID,
		SessionToken: sessionToken,
	}

	resp, err := client.BeginChallenge(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleBeginMFAChallenge")
	}

	result := map[string]interface{}{
		"challenge_id": resp.ChallengeId,
		"method":       int(resp.Method),
	}

	if resp.ExpiresAt != nil {
		result["expires_at"] = resp.ExpiresAt.AsTime()
	}

	return result, nil
}

// handleVerifyMFAChallenge handles VerifyMFAChallenge requests.
func (h *iamHandler) handleVerifyMFAChallenge(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleVerifyMFAChallenge", "invalid request type")
	}

	challengeID, _ := getString(reqMap, "challenge_id")
	code, _ := getString(reqMap, "code")

	pbReq := &iampb.VerifyMFAChallengeRequest{
		ChallengeId: challengeID,
		Code:        code,
	}

	resp, err := client.VerifyChallenge(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleVerifyMFAChallenge")
	}

	result := map[string]interface{}{
		"success":       resp.Success,
		"session_token": resp.SessionToken,
	}

	if resp.VerifiedAt != nil {
		result["verified_at"] = resp.VerifiedAt.AsTime()
	}

	return result, nil
}

// handleVerifyMFACode handles VerifyMFACode requests.
func (h *iamHandler) handleVerifyMFACode(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleVerifyMFACode", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	code, _ := getString(reqMap, "code")

	pbReq := &iampb.VerifyMFACodeRequest{
		TenantId: tenantID,
		UserId:   userID,
		Code:     code,
	}

	resp, err := client.VerifyCode(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleVerifyMFACode")
	}

	return map[string]interface{}{"success": resp.Success}, nil
}

// handleGetMFAStatus handles GetMFAStatus requests.
func (h *iamHandler) handleGetMFAStatus(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetMFAStatus", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.GetMFAStatusRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := client.GetStatus(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetMFAStatus")
	}

	status := &cli.MFAStatus{
		Enabled:              resp.Enabled,
		Method:               iampb.MFAMethod(resp.Method).String(),
		BackupCodesRemaining: int(resp.BackupCodesRemaining),
	}

	if resp.EnrolledAt != nil {
		t := resp.EnrolledAt.AsTime()
		status.EnrolledAt = &t
	}
	if resp.LastVerified != nil {
		t := resp.LastVerified.AsTime()
		status.LastVerified = &t
	}

	return status, nil
}

// handleDisableMFA handles DisableMFA requests.
func (h *iamHandler) handleDisableMFA(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDisableMFA", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.DisableMFARequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := client.Disable(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDisableMFA")
	}

	return map[string]interface{}{"success": resp.Success}, nil
}

// handleGenerateBackupCodes handles GenerateBackupCodes requests.
func (h *iamHandler) handleGenerateBackupCodes(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGenerateBackupCodes", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.GenerateBackupCodesRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := client.GenerateBackupCodes(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGenerateBackupCodes")
	}

	now := time.Now()
	return &cli.BackupCodeStatus{
		RemainingCodes: len(resp.Codes),
		TotalCodes:     len(resp.Codes),
		GeneratedAt:    &now,
	}, nil
}

// handleVerifyBackupCode handles VerifyBackupCode requests.
func (h *iamHandler) handleVerifyBackupCode(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleVerifyBackupCode", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	code, _ := getString(reqMap, "code")

	pbReq := &iampb.VerifyBackupCodeRequest{
		TenantId: tenantID,
		UserId:   userID,
		Code:     code,
	}

	resp, err := client.VerifyBackupCode(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleVerifyBackupCode")
	}

	return map[string]interface{}{
		"success":         resp.Success,
		"remaining_codes": resp.RemainingCodes,
	}, nil
}

// handleGetBackupCodeStatus handles GetBackupCodeStatus requests.
func (h *iamHandler) handleGetBackupCodeStatus(ctx context.Context, client iampb.MFAServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetBackupCodeStatus", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.GetBackupCodeStatusRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := client.GetBackupCodeStatus(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetBackupCodeStatus")
	}

	status := &cli.BackupCodeStatus{
		TotalCodes:     int(resp.TotalCodes),
		RemainingCodes: int(resp.RemainingCodes),
	}

	if resp.GeneratedAt != nil {
		t := resp.GeneratedAt.AsTime()
		status.GeneratedAt = &t
	}

	return status, nil
}
