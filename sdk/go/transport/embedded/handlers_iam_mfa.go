// Copyright (c) 2025 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// iamMFAService is the service name the SDK IAM client dispatches MFA verbs on.
const iamMFAService = "quicbaas.IAM.MFA"

// iamMFAHandlers routes the MFA verbs the SDK IAM client sends to the same
// service.MFAManager the REST and gRPC transports call (go-quicbaas#1554).
var iamMFAHandlers = map[string]handlerFunc{
	iamMFAService + "/GetMFAStatus":          handleIAMMFAGetStatus,
	iamMFAService + "/BeginMFAEnrollment":    handleIAMMFABeginEnrollment,
	iamMFAService + "/CompleteMFAEnrollment": handleIAMMFACompleteEnrollment,
	iamMFAService + "/DisableMFA":            handleIAMMFADisable,
	iamMFAService + "/GetBackupCodeStatus":   handleIAMMFAGetBackupCodeStatus,
	iamMFAService + "/GenerateBackupCodes":   handleIAMMFAGenerateBackupCodes,
}

func init() {
	for k, v := range iamMFAHandlers {
		unaryHandlers[k] = v
	}
}

// mfaManagerProvider is the structural seam a service.QuicBaaS exposes its
// MFA manager through.
type mfaManagerProvider interface {
	GetMFAManager(ctx context.Context) (service.MFAManager, error)
}

func mfaManagerFor(ctx context.Context, svc service.QuicBaaS) (service.MFAManager, error) {
	p, ok := svc.(mfaManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.MFA")
	}
	mgr, err := p.GetMFAManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError("IAM.MFA")
	}
	return mgr, nil
}

// mfaUserTarget resolves the manager, the caller-bound tenant and the user a
// tenant+user MFA verb acts on. The tenant check is the one the gRPC MFA
// server makes (requireCallerTenant): a request naming another tenant is
// refused before the service is called.
func mfaUserTarget(ctx context.Context, svc service.QuicBaaS, method string, req interface{}) (service.MFAManager, string, uint64, error) {
	mgr, err := mfaManagerFor(ctx, svc)
	if err != nil {
		return nil, "", 0, err
	}
	tenantID, hasTenant := iamRequestField(req, "TenantID")
	userID, hasUser := iamRequestField(req, "UserID")
	if !hasTenant || !hasUser {
		return nil, "", 0, NewInvalidRequestTypeError(iamMFAService, method,
			"struct with TenantID and UserID fields", typeNameOf(req))
	}
	tenantID, err = tenantctx.Target(ctx, iamMFAService+"/"+method, tenantID)
	if err != nil {
		return nil, "", 0, err
	}
	uid, err := parseIAMUserID(iamMFAService, method, userID)
	if err != nil {
		return nil, "", 0, err
	}
	return mgr, tenantID, uid, nil
}

func handleIAMMFAGetStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, tenantID, uid, err := mfaUserTarget(ctx, svc, "GetMFAStatus", req)
	if err != nil {
		return nil, err
	}
	status, err := mgr.GetMFAStatus(ctx, uid, tenantID)
	if err != nil {
		return nil, err
	}
	return &cli.MFAStatus{
		Enabled:              status.Enabled,
		Method:               string(status.Method),
		EnrolledAt:           status.EnrolledAt,
		LastVerified:         status.LastVerified,
		BackupCodesRemaining: status.BackupCodesRemaining,
	}, nil
}

func handleIAMMFABeginEnrollment(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, tenantID, uid, err := mfaUserTarget(ctx, svc, "BeginMFAEnrollment", req)
	if err != nil {
		return nil, err
	}
	method, _ := iamRequestField(req, "Method")
	resp, err := mgr.BeginEnrollment(ctx, uid, tenantID, iam.MFAMethod(method))
	if err != nil {
		return nil, err
	}
	return &cli.MFAEnrollment{
		EnrollmentID:    resp.EnrollmentID,
		Secret:          resp.Secret,
		ProvisioningURI: resp.QRCodeURI,
		ExpiresAt:       resp.ExpiresAt,
	}, nil
}

// handleIAMMFACompleteEnrollment binds the enrollment to the caller's tenant
// only: the request names no user, exactly as on the gRPC transport.
func handleIAMMFACompleteEnrollment(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := mfaManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	enrollmentID, hasEnrollment := iamRequestField(req, "EnrollmentID")
	code, hasCode := iamRequestField(req, "Code")
	if !hasEnrollment || !hasCode {
		return nil, NewInvalidRequestTypeError(iamMFAService, "CompleteMFAEnrollment",
			"struct with EnrollmentID and Code fields", typeNameOf(req))
	}
	tenantID, err := tenantctx.Target(ctx, iamMFAService+"/CompleteMFAEnrollment", "")
	if err != nil {
		return nil, err
	}
	if err := mgr.CompleteEnrollment(ctx, 0, tenantID, enrollmentID, code); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMMFADisable(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, tenantID, uid, err := mfaUserTarget(ctx, svc, "DisableMFA", req)
	if err != nil {
		return nil, err
	}
	if err := mgr.DisableMFA(ctx, uid, tenantID); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMMFAGetBackupCodeStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, tenantID, uid, err := mfaUserTarget(ctx, svc, "GetBackupCodeStatus", req)
	if err != nil {
		return nil, err
	}
	status, err := mgr.GetBackupCodeStatus(ctx, uid, tenantID)
	if err != nil {
		return nil, err
	}
	out := &cli.BackupCodeStatus{
		RemainingCodes: status.Remaining,
		TotalCodes:     status.Total,
	}
	if !status.GeneratedAt.IsZero() {
		generatedAt := status.GeneratedAt
		out.GeneratedAt = &generatedAt
	}
	return out, nil
}

func handleIAMMFAGenerateBackupCodes(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, tenantID, uid, err := mfaUserTarget(ctx, svc, "GenerateBackupCodes", req)
	if err != nil {
		return nil, err
	}
	return mgr.GenerateBackupCodes(ctx, uid, tenantID)
}
