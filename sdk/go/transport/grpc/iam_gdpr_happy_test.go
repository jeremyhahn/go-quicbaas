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

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

func sampleConsent() *iampb.Consent {
	return &iampb.Consent{
		Id: "c1", UserId: "u1", TenantId: "t1",
		Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING,
		Status:  iampb.ConsentStatus_CONSENT_STATUS_GRANTED,
		Version: "1", IpAddress: "1.2.3.4", UserAgent: "agent",
		Metadata: map[string]string{"k": "v"}, GrantedAt: iamTime(),
	}
}

func sampleExportReq() *iampb.DataExportRequest {
	return &iampb.DataExportRequest{
		Id: "ex1", UserId: "u1", TenantId: "t1", RequestedBy: "u1",
		IncludeCategories: []string{"profile"}, DownloadUrl: "https://x",
		RequestedAt: iamTime(),
	}
}

func sampleDeletionReq() *iampb.DataDeletionRequest {
	return &iampb.DataDeletionRequest{
		Id: "d1", UserId: "u1", TenantId: "t1", Reason: "gdpr",
		PreserveCategories: []string{"legal"}, RequestedAt: iamTime(),
	}
}

// GDPR methods on the shared fake IAM server.
func (f *fakeIAMServer) GrantConsent(context.Context, *iampb.GrantConsentRequest) (*iampb.Consent, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleConsent(), nil
}
func (f *fakeIAMServer) WithdrawConsent(context.Context, *iampb.WithdrawConsentRequest) (*iampb.WithdrawConsentResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.WithdrawConsentResponse{}, nil
}
func (f *fakeIAMServer) GetConsent(context.Context, *iampb.GetConsentRequest) (*iampb.Consent, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleConsent(), nil
}
func (f *fakeIAMServer) ListUserConsents(context.Context, *iampb.ListUserConsentsRequest) (*iampb.ConsentList, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ConsentList{Consents: []*iampb.Consent{sampleConsent()}, Total: 1}, nil
}
func (f *fakeIAMServer) RequestExport(context.Context, *iampb.RequestExportRequest) (*iampb.DataExportRequest, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleExportReq(), nil
}
func (f *fakeIAMServer) GetExportStatus(context.Context, *iampb.GetExportStatusRequest) (*iampb.DataExportRequest, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleExportReq(), nil
}
func (f *fakeIAMServer) GenerateExport(context.Context, *iampb.GenerateExportRequest) (*iampb.UserDataExport, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.UserDataExport{ExportMetadata: &iampb.ExportMetadata{ExportId: "ex1", UserId: "u1", Categories: []string{"profile"}}}, nil
}
func (f *fakeIAMServer) RequestDeletion(context.Context, *iampb.RequestDeletionRequest) (*iampb.DataDeletionRequest, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleDeletionReq(), nil
}
func (f *fakeIAMServer) CancelDeletion(context.Context, *iampb.CancelDeletionRequest) (*iampb.CancelDeletionResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.CancelDeletionResponse{}, nil
}
func (f *fakeIAMServer) ProcessDeletion(context.Context, *iampb.ProcessDeletionRequest) (*iampb.DeletionSummary, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeletionSummary{SessionsDeleted: 1, ConsentsDeleted: 2, TokensRevoked: 3}, nil
}
func (f *fakeIAMServer) GetDeletionStatus(context.Context, *iampb.GetDeletionStatusRequest) (*iampb.DataDeletionRequest, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleDeletionReq(), nil
}

var iamGDPRCases = []iamCase{
	{"GrantConsent", "quicbaas.IAM.GDPR", "GrantConsent", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "consent": map[string]interface{}{"purpose": "marketing", "version": "1"}}},
	{"WithdrawConsent", "quicbaas.IAM.GDPR", "WithdrawConsent", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing"}},
	{"GetConsent", "quicbaas.IAM.GDPR", "GetConsent", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "purpose": "marketing"}},
	{"ListConsents", "quicbaas.IAM.GDPR", "ListConsents", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	{"RequestExport", "quicbaas.IAM.GDPR", "RequestExport", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "include_categories": []string{"profile"}}},
	{"GetExportStatus", "quicbaas.IAM.GDPR", "GetExportStatus", map[string]interface{}{"export_id": "ex1"}},
	{"GenerateExport", "quicbaas.IAM.GDPR", "GenerateExport", map[string]interface{}{"export_id": "ex1"}},
	{"RequestDeletion", "quicbaas.IAM.GDPR", "RequestDeletion", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "deletion": map[string]interface{}{"reason": "gdpr", "delete_categories": []string{"a"}, "retain_categories": []string{"legal"}}}},
	{"CancelDeletion", "quicbaas.IAM.GDPR", "CancelDeletion", map[string]interface{}{"deletion_id": "d1", "cancelled_by": "admin"}},
	{"ProcessDeletion", "quicbaas.IAM.GDPR", "ProcessDeletion", map[string]interface{}{"deletion_id": "d1"}},
	{"GetDeletionStatus", "quicbaas.IAM.GDPR", "GetDeletionStatus", map[string]interface{}{"deletion_id": "d1"}},
}

func init() {
	iamServiceCases = append(iamServiceCases, iamGDPRCases...)
}
