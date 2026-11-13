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

package embedded

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const gdprServiceName = "quicbaas.IAM.GDPR"

// iamGDPRServiceName names the GDPR verbs in a not-configured error.
const iamGDPRServiceName = "IAM.GDPR"

// iamGDPRUnaryHandlers routes the SDK IAM client's GDPR verbs onto the
// iam.GDPRManager the REST and gRPC servers call. The block is kept apart
// from iamUnaryHandlers so it merges independently.
var iamGDPRUnaryHandlers = map[string]handlerFunc{
	gdprServiceName + "/GrantConsent":      handleIAMGDPRGrantConsent,
	gdprServiceName + "/WithdrawConsent":   handleIAMGDPRWithdrawConsent,
	gdprServiceName + "/GetConsent":        handleIAMGDPRGetConsent,
	gdprServiceName + "/ListConsents":      handleIAMGDPRListConsents,
	gdprServiceName + "/RequestExport":     handleIAMGDPRRequestExport,
	gdprServiceName + "/GetExportStatus":   handleIAMGDPRGetExportStatus,
	gdprServiceName + "/GenerateExport":    handleIAMGDPRGenerateExport,
	gdprServiceName + "/RequestDeletion":   handleIAMGDPRRequestDeletion,
	gdprServiceName + "/CancelDeletion":    handleIAMGDPRCancelDeletion,
	gdprServiceName + "/GetDeletionStatus": handleIAMGDPRGetDeletionStatus,
}

func init() {
	for k, v := range iamGDPRUnaryHandlers {
		unaryHandlers[k] = v
	}
}

// gdprManagerProvider is the accessor the REST server reads the GDPR manager
// through.
type gdprManagerProvider interface {
	GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
}

// gdprCall is the tenant, data subject and manager a GDPR verb acts on.
type gdprCall struct {
	tenantID string
	userID   uint64
	mgr      *iam.GDPRManager
}

// gdprResolve reads the tenant and user every GDPR request names, applies the
// tenant check the gRPC server makes (the request may name only the tenant
// the call is bound to) and returns the manager.
func gdprResolve(ctx context.Context, svc service.QuicBaaS, method string, req interface{}) (*gdprCall, error) {
	requested, err := gdprRequestField(req, method, "TenantID")
	if err != nil {
		return nil, err
	}
	rawUserID, err := gdprRequestField(req, method, "UserID")
	if err != nil {
		return nil, err
	}
	tenantID, err := tenantctx.Target(ctx, gdprServiceName+"/"+method, requested)
	if err != nil {
		return nil, err
	}
	userID, err := parseIAMUserID(iamGDPRServiceName, method, rawUserID)
	if err != nil {
		return nil, err
	}
	p, ok := svc.(gdprManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError(iamGDPRServiceName)
	}
	mgr, err := p.GetGDPRManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError(iamGDPRServiceName)
	}
	return &gdprCall{tenantID: tenantID, userID: userID, mgr: mgr}, nil
}

// gdprRequestField reads a string field from a request the SDK client sends
// as an unexported struct. A request without the field is the wrong type.
func gdprRequestField(req interface{}, method, name string) (string, error) {
	value, ok := iamRequestField(req, name)
	if !ok {
		return "", NewInvalidRequestTypeError(gdprServiceName, method,
			"struct with "+name, fmt.Sprintf("%T", req))
	}
	return value, nil
}

// owns refuses a record of another tenant or data subject as not found, as
// the REST server does for every verb that names a record by its id.
func (c *gdprCall) owns(method, kind, tenantID string, userID uint64) error {
	if tenantID != c.tenantID || userID != c.userID {
		return errors.NewNotFound("embedded."+iamGDPRServiceName+"."+method, kind+" not found")
	}
	return nil
}

func handleIAMGDPRGrantConsent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, err := gdprResolve(ctx, svc, "GrantConsent", req)
	if err != nil {
		return nil, err
	}
	consent, ok := iamRequestStructPtr(req, "Consent").(*iamsdk.GrantConsentRequest)
	if !ok || consent == nil {
		return nil, NewInvalidRequestTypeError(gdprServiceName, "GrantConsent",
			"struct with *iam.GrantConsentRequest Consent", fmt.Sprintf("%T", req))
	}
	var expiresAt *time.Time
	if !consent.ExpiresAt.IsZero() {
		t := consent.ExpiresAt
		expiresAt = &t
	}
	if _, err := call.mgr.GrantConsent(ctx, call.tenantID, call.userID,
		iam.ConsentPurpose(consent.Purpose), consent.Version, expiresAt, "", ""); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMGDPRWithdrawConsent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, purpose, err := gdprConsentCall(ctx, svc, "WithdrawConsent", req)
	if err != nil {
		return nil, err
	}
	if err := call.mgr.WithdrawConsent(ctx, call.tenantID, call.userID, purpose, "", ""); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMGDPRGetConsent(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, purpose, err := gdprConsentCall(ctx, svc, "GetConsent", req)
	if err != nil {
		return nil, err
	}
	consent, err := call.mgr.GetConsent(ctx, call.tenantID, call.userID, purpose)
	if err != nil {
		return nil, err
	}
	return toSDKConsent(consent)
}

func handleIAMGDPRListConsents(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, err := gdprResolve(ctx, svc, "ListConsents", req)
	if err != nil {
		return nil, err
	}
	consents, err := call.mgr.ListUserConsents(ctx, call.tenantID, call.userID)
	if err != nil {
		return nil, err
	}
	out := &iamsdk.ListConsentsResponse{Consents: make([]iamsdk.Consent, 0, len(consents)), TotalCount: len(consents)}
	for _, c := range consents {
		sdkConsent, err := toSDKConsent(c)
		if err != nil {
			return nil, err
		}
		out.Consents = append(out.Consents, *sdkConsent)
	}
	return out, nil
}

// gdprConsentCall resolves a verb whose request names one consent purpose.
func gdprConsentCall(ctx context.Context, svc service.QuicBaaS, method string, req interface{}) (*gdprCall, iam.ConsentPurpose, error) {
	call, err := gdprResolve(ctx, svc, method, req)
	if err != nil {
		return nil, "", err
	}
	purpose, err := gdprRequestField(req, method, "Purpose")
	if err != nil {
		return nil, "", err
	}
	return call, iam.ConsentPurpose(purpose), nil
}

// handleIAMGDPRRequestExport records the export as requested by the data
// subject, the REST server's default when no requester is named.
func handleIAMGDPRRequestExport(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, err := gdprResolve(ctx, svc, "RequestExport", req)
	if err != nil {
		return nil, err
	}
	export, ok := iamRequestStructPtr(req, "Export").(*iamsdk.ExportRequest)
	if !ok || export == nil {
		export = &iamsdk.ExportRequest{}
	}
	exportReq, err := call.mgr.RequestExport(ctx, call.tenantID, call.userID, call.userID,
		iam.DataExportFormat(export.Format.String()), export.IncludeCategories, export.ExcludeCategories)
	if err != nil {
		return nil, err
	}
	return toSDKExportRequest(exportReq)
}

func handleIAMGDPRGetExportStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	_, exportReq, err := gdprOwnedExport(ctx, svc, "GetExportStatus", req)
	if err != nil {
		return nil, err
	}
	return toSDKExportRequest(exportReq)
}

// handleIAMGDPRGenerateExport follows the REST download: only a completed
// export of the caller's data subject is generated.
func handleIAMGDPRGenerateExport(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, exportReq, err := gdprOwnedExport(ctx, svc, "GenerateExport", req)
	if err != nil {
		return nil, err
	}
	if exportReq.Status != iam.DataExportStatusCompleted {
		return nil, errors.NewConflict("embedded."+iamGDPRServiceName+".GenerateExport",
			"export is not ready for download")
	}
	export, err := call.mgr.GenerateExport(ctx, exportReq.ID)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(export)
	if err != nil {
		return nil, err
	}
	out := &iamsdk.UserDataExport{
		RequestID:   exportReq.ID,
		UserID:      strconv.FormatUint(exportReq.UserID, 10),
		Data:        data,
		GeneratedAt: export.ExportMetadata.ExportedAt,
		ExpiresAt:   timeOrZero(exportReq.ExpiresAt),
		Categories:  export.ExportMetadata.Categories,
	}
	if err := decodeSDKEnum(&out.Format, string(exportReq.Format)); err != nil {
		return nil, err
	}
	return out, nil
}

// gdprOwnedExport reads the export a request names and refuses one that
// belongs to another tenant or data subject.
func gdprOwnedExport(ctx context.Context, svc service.QuicBaaS, method string, req interface{}) (*gdprCall, *iam.DataExportRequest, error) {
	call, err := gdprResolve(ctx, svc, method, req)
	if err != nil {
		return nil, nil, err
	}
	exportID, err := gdprRequestField(req, method, "ExportID")
	if err != nil {
		return nil, nil, err
	}
	exportReq, err := call.mgr.GetExportStatus(ctx, exportID)
	if err != nil {
		return nil, nil, err
	}
	if err := call.owns(method, "export", exportReq.TenantID, exportReq.UserID); err != nil {
		return nil, nil, err
	}
	return call, exportReq, nil
}

// handleIAMGDPRRequestDeletion records the erasure as requested by the data
// subject, the REST server's default when no requester is named.
func handleIAMGDPRRequestDeletion(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, err := gdprResolve(ctx, svc, "RequestDeletion", req)
	if err != nil {
		return nil, err
	}
	deletion, ok := iamRequestStructPtr(req, "Deletion").(*iamsdk.DeletionRequest)
	if !ok || deletion == nil {
		deletion = &iamsdk.DeletionRequest{}
	}
	graceDays, err := gdprGracePeriodDays(deletion.ScheduledFor, time.Now())
	if err != nil {
		return nil, err
	}
	deletionReq, err := call.mgr.RequestDeletion(ctx, call.tenantID, call.userID, call.userID,
		deletion.Reason, nil, deletion.RetainCategories, graceDays)
	if err != nil {
		return nil, err
	}
	return toSDKDeletionRequest(deletionReq)
}

// gdprGracePeriodDays turns the date the client schedules an erasure for into
// the grace period the manager takes, rounded up to whole days. No date keeps
// the manager's default grace period. The manager reads zero days as "erase
// now", an irreversible step no client date asks for, so a date that is not in
// the future is refused.
func gdprGracePeriodDays(scheduledFor, now time.Time) (int, error) {
	if scheduledFor.IsZero() {
		return iam.DefaultGracePeriodDays, nil
	}
	if !scheduledFor.After(now) {
		return 0, errors.NewInvalidInput("embedded."+iamGDPRServiceName+".RequestDeletion",
			"scheduled_for must be in the future")
	}
	return int(math.Ceil(scheduledFor.Sub(now).Hours() / 24)), nil
}

// handleIAMGDPRCancelDeletion follows the REST server: the erasure must
// belong to the caller's data subject before it is cancelled.
func handleIAMGDPRCancelDeletion(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	call, deletionReq, err := gdprOwnedDeletion(ctx, svc, "CancelDeletion", req)
	if err != nil {
		return nil, err
	}
	cancelledBy, err := gdprRequestField(req, "CancelDeletion", "CancelledBy")
	if err != nil {
		return nil, err
	}
	if err := call.mgr.CancelDeletion(ctx, deletionReq.ID, cancelledBy); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMGDPRGetDeletionStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	_, deletionReq, err := gdprOwnedDeletion(ctx, svc, "GetDeletionStatus", req)
	if err != nil {
		return nil, err
	}
	return toSDKDeletionRequest(deletionReq)
}

// gdprOwnedDeletion reads the erasure a request names and refuses one that
// belongs to another tenant or data subject.
func gdprOwnedDeletion(ctx context.Context, svc service.QuicBaaS, method string, req interface{}) (*gdprCall, *iam.DataDeletionRequest, error) {
	call, err := gdprResolve(ctx, svc, method, req)
	if err != nil {
		return nil, nil, err
	}
	deletionID, err := gdprRequestField(req, method, "DeletionID")
	if err != nil {
		return nil, nil, err
	}
	deletionReq, err := call.mgr.GetDeletionStatus(ctx, deletionID)
	if err != nil {
		return nil, nil, err
	}
	if err := call.owns(method, "deletion request", deletionReq.TenantID, deletionReq.UserID); err != nil {
		return nil, nil, err
	}
	return call, deletionReq, nil
}

// decodeSDKEnum sets an SDK value from the string the manager stores through
// the value's own JSON decoding, so the embedded path accepts and refuses the
// same values a REST response does.
func decodeSDKEnum(target json.Unmarshaler, value string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return target.UnmarshalJSON(data)
}

// timeOrZero returns the time a pointer holds, or the zero time for nil.
func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// toSDKConsent converts a stored consent into the shape the SDK client
// decodes.
func toSDKConsent(c *iam.Consent) (*iamsdk.Consent, error) {
	out := &iamsdk.Consent{
		ID:          c.ID,
		TenantID:    c.TenantID,
		UserID:      strconv.FormatUint(c.UserID, 10),
		Purpose:     string(c.Purpose),
		GrantedAt:   timeOrZero(c.GrantedAt),
		WithdrawnAt: timeOrZero(c.WithdrawnAt),
		ExpiresAt:   timeOrZero(c.ExpiresAt),
		IPAddress:   c.IPAddress,
		UserAgent:   c.UserAgent,
		Metadata:    c.Metadata,
	}
	if err := decodeSDKEnum(&out.Status, string(c.Status)); err != nil {
		return nil, err
	}
	if c.Version != "" {
		if err := decodeSDKEnum(&out.Version, c.Version); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// toSDKExportRequest converts a stored export request into the shape the SDK
// client decodes.
func toSDKExportRequest(e *iam.DataExportRequest) (*iamsdk.DataExportRequest, error) {
	out := &iamsdk.DataExportRequest{
		ID:                e.ID,
		TenantID:          e.TenantID,
		UserID:            strconv.FormatUint(e.UserID, 10),
		IncludeCategories: e.IncludeCategories,
		ExcludeCategories: e.ExcludeCategories,
		RequestedAt:       e.RequestedAt,
		CompletedAt:       timeOrZero(e.CompletedAt),
		ExpiresAt:         timeOrZero(e.ExpiresAt),
		DownloadURL:       e.DownloadURL,
		SizeBytes:         e.FileSizeBytes,
		ErrorMessage:      e.ErrorMessage,
		Metadata:          e.Metadata,
	}
	if err := decodeSDKEnum(&out.Status, string(e.Status)); err != nil {
		return nil, err
	}
	if err := decodeSDKEnum(&out.Format, string(e.Format)); err != nil {
		return nil, err
	}
	return out, nil
}

// toSDKDeletionRequest converts a stored erasure request into the shape the
// SDK client decodes.
func toSDKDeletionRequest(d *iam.DataDeletionRequest) (*iamsdk.DataDeletionRequest, error) {
	out := &iamsdk.DataDeletionRequest{
		ID:                  d.ID,
		TenantID:            d.TenantID,
		UserID:              strconv.FormatUint(d.UserID, 10),
		Reason:              d.Reason,
		RetainCategories:    d.PreserveCategories,
		RequestedAt:         d.RequestedAt,
		ScheduledFor:        timeOrZero(d.GracePeriodEndsAt),
		ProcessingStartedAt: timeOrZero(d.StartedAt),
		CompletedAt:         timeOrZero(d.CompletedAt),
		CancelledAt:         timeOrZero(d.CancelledAt),
		ErrorMessage:        d.ErrorMessage,
		Metadata:            d.Metadata,
	}
	if err := decodeSDKEnum(&out.Status, string(d.Status)); err != nil {
		return nil, err
	}
	return out, nil
}
