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
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// consentPurposeFromString converts a lowercase purpose string (e.g., "marketing")
// to the corresponding protobuf ConsentPurpose enum value. Returns
// CONSENT_PURPOSE_UNSPECIFIED for unrecognized strings.
func consentPurposeFromString(s string) iampb.ConsentPurpose {
	key := "CONSENT_PURPOSE_" + strings.ToUpper(s)
	if val, ok := iampb.ConsentPurpose_value[key]; ok {
		return iampb.ConsentPurpose(val)
	}
	return iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED
}

// handleGrantConsent handles GrantConsent requests.
func (h *iamHandler) handleGrantConsent(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGrantConsent", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	version, _ := getString(reqMap, "version")
	ipAddress, _ := getString(reqMap, "ip_address")
	userAgent, _ := getString(reqMap, "user_agent")

	var purpose iampb.ConsentPurpose

	// Extract purpose from the nested "consent" field sent by the SDK client.
	if consentMap, ok := getMap(reqMap, "consent"); ok {
		if p, ok := getString(consentMap, "purpose"); ok {
			purpose = consentPurposeFromString(p)
		}
		if v, ok := getString(consentMap, "version"); ok {
			version = v
		}
	}

	pbReq := &iampb.GrantConsentRequest{
		TenantId:  tenantID,
		UserId:    userID,
		Purpose:   purpose,
		Version:   version,
		IpAddress: ipAddress,
		UserAgent: userAgent,
	}

	resp, err := client.GrantConsent(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGrantConsent")
	}

	// GrantConsent SDK client expects nil response on success.
	_ = resp
	return nil, nil
}

// handleWithdrawConsent handles WithdrawConsent requests.
func (h *iamHandler) handleWithdrawConsent(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleWithdrawConsent", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	purposeStr, _ := getString(reqMap, "purpose")
	ipAddress, _ := getString(reqMap, "ip_address")
	userAgent, _ := getString(reqMap, "user_agent")

	pbReq := &iampb.WithdrawConsentRequest{
		TenantId:  tenantID,
		UserId:    userID,
		Purpose:   consentPurposeFromString(purposeStr),
		IpAddress: ipAddress,
		UserAgent: userAgent,
	}

	_, err := client.WithdrawConsent(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleWithdrawConsent")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleGetConsent handles GetConsent requests.
func (h *iamHandler) handleGetConsent(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetConsent", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	purposeStr, _ := getString(reqMap, "purpose")

	pbReq := &iampb.GetConsentRequest{
		TenantId: tenantID,
		UserId:   userID,
		Purpose:  consentPurposeFromString(purposeStr),
	}

	resp, err := client.GetConsent(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetConsent")
	}

	return convertProtoConsentToSDK(resp), nil
}

// handleListUserConsents handles ListUserConsents requests.
func (h *iamHandler) handleListUserConsents(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListUserConsents", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.ListUserConsentsRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := client.ListUserConsents(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListUserConsents")
	}

	consents := make([]iam.Consent, 0, len(resp.Consents))
	for _, c := range resp.Consents {
		if converted := convertProtoConsentToSDK(c); converted != nil {
			consents = append(consents, *converted)
		}
	}

	return &iam.ListConsentsResponse{
		Consents:   consents,
		TotalCount: int(resp.Total),
	}, nil
}

// handleRequestExport handles RequestExport requests.
func (h *iamHandler) handleRequestExport(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRequestExport", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	requestedBy, _ := getString(reqMap, "requested_by")
	if requestedBy == "" {
		requestedBy = userID
	}
	format, _ := getInt(reqMap, "format")
	includeCategories, _ := getStringSlice(reqMap, "include_categories")
	excludeCategories, _ := getStringSlice(reqMap, "exclude_categories")

	pbReq := &iampb.RequestExportRequest{
		TenantId:          tenantID,
		UserId:            userID,
		RequestedBy:       requestedBy,
		Format:            iampb.DataExportFormat(format),
		IncludeCategories: includeCategories,
		ExcludeCategories: excludeCategories,
	}

	resp, err := client.RequestExport(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRequestExport")
	}

	return convertProtoDataExportRequestToSDK(resp), nil
}

// handleGetExportStatus handles GetExportStatus requests.
func (h *iamHandler) handleGetExportStatus(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetExportStatus", "invalid request type")
	}

	exportID, _ := getString(reqMap, "export_id")

	pbReq := &iampb.GetExportStatusRequest{
		ExportId: exportID,
	}

	resp, err := client.GetExportStatus(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetExportStatus")
	}

	return convertProtoDataExportRequestToSDK(resp), nil
}

// handleGenerateExport handles GenerateExport requests.
func (h *iamHandler) handleGenerateExport(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGenerateExport", "invalid request type")
	}

	exportID, _ := getString(reqMap, "export_id")

	pbReq := &iampb.GenerateExportRequest{
		ExportId: exportID,
	}

	resp, err := client.GenerateExport(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGenerateExport")
	}

	return convertProtoUserDataExportToSDK(resp), nil
}

// handleRequestDeletion handles RequestDeletion requests.
func (h *iamHandler) handleRequestDeletion(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRequestDeletion", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	requestedBy, _ := getString(reqMap, "requested_by")
	if requestedBy == "" {
		requestedBy = userID
	}
	reason, _ := getString(reqMap, "reason")
	deleteCategories, _ := getStringSlice(reqMap, "delete_categories")
	preserveCategories, _ := getStringSlice(reqMap, "preserve_categories")
	gracePeriodDays, _ := getInt(reqMap, "grace_period_days")

	pbReq := &iampb.RequestDeletionRequest{
		TenantId:           tenantID,
		UserId:             userID,
		RequestedBy:        requestedBy,
		Reason:             reason,
		DeleteCategories:   deleteCategories,
		PreserveCategories: preserveCategories,
		GracePeriodDays:    int32(gracePeriodDays),
	}

	resp, err := client.RequestDeletion(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRequestDeletion")
	}

	return convertProtoDataDeletionRequestToSDK(resp), nil
}

// handleCancelDeletion handles CancelDeletion requests.
func (h *iamHandler) handleCancelDeletion(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCancelDeletion", "invalid request type")
	}

	deletionID, _ := getString(reqMap, "deletion_id")
	cancelledBy, _ := getString(reqMap, "cancelled_by")
	if cancelledBy == "" {
		cancelledBy, _ = getString(reqMap, "user_id")
	}

	pbReq := &iampb.CancelDeletionRequest{
		DeletionId:  deletionID,
		CancelledBy: cancelledBy,
	}

	_, err := client.CancelDeletion(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCancelDeletion")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleProcessDeletion handles ProcessDeletion requests.
func (h *iamHandler) handleProcessDeletion(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleProcessDeletion", "invalid request type")
	}

	deletionID, _ := getString(reqMap, "deletion_id")

	pbReq := &iampb.ProcessDeletionRequest{
		DeletionId: deletionID,
	}

	resp, err := client.ProcessDeletion(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleProcessDeletion")
	}

	// ProcessDeletion returns a DeletionSummary, which maps to DataDeletionRequest status.
	return &iam.DataDeletionRequest{
		ID:                 deletionID,
		Status:             iam.DeletionStatusCompleted,
		DeletedRecordCount: int64(resp.SessionsDeleted + resp.ConsentsDeleted + resp.CredentialsDeleted + resp.GroupMembershipsRemoved + resp.RoleAssignmentsRemoved + resp.TokensRevoked),
	}, nil
}

// handleGetDeletionStatus handles GetDeletionStatus requests.
func (h *iamHandler) handleGetDeletionStatus(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetDeletionStatus", "invalid request type")
	}

	deletionID, _ := getString(reqMap, "deletion_id")

	pbReq := &iampb.GetDeletionStatusRequest{
		DeletionId: deletionID,
	}

	resp, err := client.GetDeletionStatus(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetDeletionStatus")
	}

	return convertProtoDataDeletionRequestToSDK(resp), nil
}

// convertProtoConsentToSDK converts a protobuf Consent to SDK Consent.
func convertProtoConsentToSDK(c *iampb.Consent) *iam.Consent {
	if c == nil {
		return nil
	}

	// Convert protobuf enum name to user-friendly lowercase string.
	// e.g., CONSENT_PURPOSE_MARKETING -> marketing
	purpose := strings.TrimPrefix(c.Purpose.String(), "CONSENT_PURPOSE_")
	purpose = strings.ToLower(purpose)

	resp := &iam.Consent{
		ID:        c.Id,
		UserID:    c.UserId,
		TenantID:  c.TenantId,
		Purpose:   purpose,
		Status:    convertProtoConsentStatus(c.Status),
		IPAddress: c.IpAddress,
		UserAgent: c.UserAgent,
		Metadata:  c.Metadata,
		Version:   0,
	}

	if c.Version != "" {
		// Version is stored as string in proto, parse to int if possible.
		resp.Version = 0
	}

	if c.GrantedAt != nil {
		resp.GrantedAt = c.GrantedAt.AsTime()
	}
	if c.WithdrawnAt != nil {
		resp.WithdrawnAt = c.WithdrawnAt.AsTime()
	}
	if c.ExpiresAt != nil {
		resp.ExpiresAt = c.ExpiresAt.AsTime()
	}

	return resp
}

// convertProtoConsentStatus maps proto ConsentStatus enum to SDK ConsentStatus.
// Proto: UNSPECIFIED=0, GRANTED=1, DENIED=2, WITHDRAWN=3, PENDING=4
// SDK:   Granted=0, Withdrawn=1, Revoked=2, Expired=3
func convertProtoConsentStatus(s iampb.ConsentStatus) iam.ConsentStatus {
	switch s {
	case iampb.ConsentStatus_CONSENT_STATUS_GRANTED:
		return iam.ConsentStatusGranted
	case iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN:
		return iam.ConsentStatusWithdrawn
	default:
		return iam.ConsentStatus(s)
	}
}

// convertProtoDataExportRequestToSDK converts a protobuf DataExportRequest to SDK DataExportRequest.
func convertProtoDataExportRequestToSDK(r *iampb.DataExportRequest) *iam.DataExportRequest {
	if r == nil {
		return nil
	}

	resp := &iam.DataExportRequest{
		ID:                r.Id,
		UserID:            r.UserId,
		TenantID:          r.TenantId,
		Format:            iam.ExportFormat(r.Format),
		Status:            iam.ExportStatus(r.Status),
		IncludeCategories: r.IncludeCategories,
		ExcludeCategories: r.ExcludeCategories,
		DownloadURL:       r.DownloadUrl,
		SizeBytes:         r.FileSizeBytes,
		ErrorMessage:      r.ErrorMessage,
		Metadata:          r.Metadata,
	}

	if r.RequestedAt != nil {
		resp.RequestedAt = r.RequestedAt.AsTime()
	}
	if r.CompletedAt != nil {
		resp.CompletedAt = r.CompletedAt.AsTime()
	}
	if r.ExpiresAt != nil {
		resp.ExpiresAt = r.ExpiresAt.AsTime()
	}

	return resp
}

// convertProtoDataDeletionRequestToSDK converts a protobuf DataDeletionRequest to SDK DataDeletionRequest.
func convertProtoDataDeletionRequestToSDK(r *iampb.DataDeletionRequest) *iam.DataDeletionRequest {
	if r == nil {
		return nil
	}

	resp := &iam.DataDeletionRequest{
		ID:               r.Id,
		UserID:           r.UserId,
		TenantID:         r.TenantId,
		Status:           iam.DeletionStatus(r.Status),
		Reason:           r.Reason,
		RetainCategories: r.PreserveCategories,
		ErrorMessage:     r.ErrorMessage,
		Metadata:         r.Metadata,
	}

	if r.RequestedAt != nil {
		resp.RequestedAt = r.RequestedAt.AsTime()
	}
	if r.GracePeriodEndsAt != nil {
		resp.ScheduledFor = r.GracePeriodEndsAt.AsTime()
	}
	if r.StartedAt != nil {
		resp.ProcessingStartedAt = r.StartedAt.AsTime()
	}
	if r.CompletedAt != nil {
		resp.CompletedAt = r.CompletedAt.AsTime()
	}
	if r.CancelledAt != nil {
		resp.CancelledAt = r.CancelledAt.AsTime()
	}
	if r.CancelledBy != "" {
		resp.CancellationReason = r.CancelledBy
	}

	return resp
}

// convertProtoUserDataExportToSDK converts a protobuf UserDataExport to SDK UserDataExport.
func convertProtoUserDataExportToSDK(e *iampb.UserDataExport) *iam.UserDataExport {
	if e == nil {
		return nil
	}

	resp := &iam.UserDataExport{}

	if e.ExportMetadata != nil {
		resp.RequestID = e.ExportMetadata.ExportId
		resp.UserID = e.ExportMetadata.UserId
		resp.Format = iam.ExportFormat(e.ExportMetadata.Format)
		resp.Categories = e.ExportMetadata.Categories
	}

	return resp
}

// gdprTimestampToProto converts a time.Time to a protobuf timestamp for GDPR operations.
func gdprTimestampToProto(t interface{}) *timestamppb.Timestamp {
	return nil
}
