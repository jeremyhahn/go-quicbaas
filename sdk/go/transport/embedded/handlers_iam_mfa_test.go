// Copyright (c) 2025 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package embedded

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// mfaTenantCtx binds the caller to tenant acme, the tenant the MFA tests name.
func mfaTenantCtx() context.Context {
	return tenantctx.WithTenant(context.Background(), "acme")
}

// errMFADouble is the service error the MFA double returns on demand.
var errMFADouble = errors.New("mfa double failure")

// mfaDouble is a service.MFAManager that records the call it is handed.
type mfaDouble struct {
	service.MFAManager
	fail       bool
	userID     uint64
	tenantID   string
	enrollment string
	code       string
	method     iam.MFAMethod
	disabled   bool
}

func (d *mfaDouble) record(userID uint64, tenantID string) error {
	d.userID, d.tenantID = userID, tenantID
	if d.fail {
		return errMFADouble
	}
	return nil
}

func (d *mfaDouble) GetMFAStatus(_ context.Context, userID uint64, tenantID string) (*iam.MFAStatus, error) {
	if err := d.record(userID, tenantID); err != nil {
		return nil, err
	}
	return &iam.MFAStatus{Enabled: true, Method: iam.MFAMethodTOTP, BackupCodesRemaining: 7}, nil
}

func (d *mfaDouble) BeginEnrollment(_ context.Context, userID uint64, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	d.method = method
	if err := d.record(userID, tenantID); err != nil {
		return nil, err
	}
	return &iam.MFAEnrollResponse{EnrollmentID: "enr-1", Secret: "S3CR", QRCodeURI: "otpauth://totp/x"}, nil
}

func (d *mfaDouble) CompleteEnrollment(_ context.Context, userID uint64, tenantID, enrollmentID, code string) error {
	d.enrollment, d.code = enrollmentID, code
	return d.record(userID, tenantID)
}

func (d *mfaDouble) DisableMFA(_ context.Context, userID uint64, tenantID string) error {
	if err := d.record(userID, tenantID); err != nil {
		return err
	}
	d.disabled = true
	return nil
}

func (d *mfaDouble) GetBackupCodeStatus(_ context.Context, userID uint64, tenantID string) (*iam.BackupCodeStatus, error) {
	if err := d.record(userID, tenantID); err != nil {
		return nil, err
	}
	return &iam.BackupCodeStatus{Total: 10, Remaining: 4, GeneratedAt: time.Unix(1700000000, 0).UTC()}, nil
}

func (d *mfaDouble) GenerateBackupCodes(_ context.Context, userID uint64, tenantID string) ([]string, error) {
	if err := d.record(userID, tenantID); err != nil {
		return nil, err
	}
	return []string{"aaaa-1111", "bbbb-2222"}, nil
}

// mockQuicBaaSWithMFA exposes an MFA manager through the mfaManagerProvider seam.
type mockQuicBaaSWithMFA struct {
	service.QuicBaaS
	mfa service.MFAManager
}

func (m *mockQuicBaaSWithMFA) GetMFAManager(_ context.Context) (service.MFAManager, error) {
	return m.mfa, nil
}

// newMFAClient drives the real SDK IAM client over the embedded transport.
func newMFAClient(t *testing.T, svc service.QuicBaaS) iamsdk.Client {
	t.Helper()
	tr, err := New(svc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return iamsdk.NewClient(tr)
}

func TestIAMMFAVerbsRouteThroughSDKClient(t *testing.T) {
	ctx := mfaTenantCtx()
	double := &mfaDouble{}
	client := newMFAClient(t, &mockQuicBaaSWithMFA{mfa: double})

	status, err := client.GetMFAStatus(ctx, "acme", "42")
	if err != nil || !status.Enabled || status.Method != "totp" || status.BackupCodesRemaining != 7 {
		t.Fatalf("GetMFAStatus = %+v, %v", status, err)
	}
	if double.userID != 42 || double.tenantID != "acme" {
		t.Fatalf("GetMFAStatus reached the manager with user %d tenant %q", double.userID, double.tenantID)
	}

	enrollment, err := client.BeginMFAEnrollment(ctx, "acme", "42", "totp")
	if err != nil || enrollment.EnrollmentID != "enr-1" || enrollment.ProvisioningURI != "otpauth://totp/x" || double.method != iam.MFAMethodTOTP {
		t.Fatalf("BeginMFAEnrollment = %+v, %v (method %q)", enrollment, err, double.method)
	}

	if err := client.CompleteMFAEnrollment(ctx, "enr-1", "123456"); err != nil {
		t.Fatalf("CompleteMFAEnrollment: %v", err)
	}
	if double.enrollment != "enr-1" || double.code != "123456" || double.userID != 0 {
		t.Fatalf("CompleteMFAEnrollment reached the manager with %q %q user %d", double.enrollment, double.code, double.userID)
	}

	if err := client.DisableMFA(ctx, "acme", "42"); err != nil || !double.disabled {
		t.Fatalf("DisableMFA: %v (disabled %v)", err, double.disabled)
	}

	backup, err := client.GetBackupCodeStatus(ctx, "acme", "42")
	if err != nil || backup.TotalCodes != 10 || backup.RemainingCodes != 4 || backup.GeneratedAt == nil {
		t.Fatalf("GetBackupCodeStatus = %+v, %v", backup, err)
	}

	codes, err := client.GenerateBackupCodes(ctx, "acme", "42")
	if err != nil || len(codes) != 2 || codes[0] != "aaaa-1111" {
		t.Fatalf("GenerateBackupCodes = %v, %v", codes, err)
	}
}

func TestIAMMFAVerbsReturnServiceError(t *testing.T) {
	ctx := mfaTenantCtx()
	client := newMFAClient(t, &mockQuicBaaSWithMFA{mfa: &mfaDouble{fail: true}})

	calls := map[string]func() error{
		"GetMFAStatus": func() error { _, err := client.GetMFAStatus(ctx, "acme", "42"); return err },
		"BeginMFAEnrollment": func() error {
			_, err := client.BeginMFAEnrollment(ctx, "acme", "42", "totp")
			return err
		},
		"CompleteMFAEnrollment": func() error { return client.CompleteMFAEnrollment(ctx, "enr-1", "123456") },
		"DisableMFA":            func() error { return client.DisableMFA(ctx, "acme", "42") },
		"GetBackupCodeStatus":   func() error { _, err := client.GetBackupCodeStatus(ctx, "acme", "42"); return err },
		"GenerateBackupCodes":   func() error { _, err := client.GenerateBackupCodes(ctx, "acme", "42"); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), errMFADouble.Error()) {
			t.Errorf("%s: err = %v, want the service error", name, err)
		}
	}
}

func TestIAMMFAHandlers_NotConfigured(t *testing.T) {
	ctx := context.Background()
	for key, handler := range iamMFAHandlers {
		_, err := handler(ctx, &mockQuicBaaSWithMFA{}, &struct{ TenantID, UserID, EnrollmentID, Code string }{})
		var notConfigured *ServiceNotConfiguredError
		if !errors.As(err, &notConfigured) {
			t.Errorf("%s: err = %v, want ServiceNotConfiguredError", key, err)
		}
	}
}

func TestIAMMFAHandlers_RejectBadRequests(t *testing.T) {
	ctx := context.Background()
	svc := &mockQuicBaaSWithMFA{mfa: &mfaDouble{}}
	type tenantUser struct{ TenantID, UserID string }
	var typedNil *tenantUser
	for key, handler := range iamMFAHandlers {
		for name, req := range map[string]interface{}{"wrong type": "not a request", "typed nil": typedNil} {
			_, err := handler(ctx, svc, req)
			var invalid *InvalidRequestTypeError
			if !errors.As(err, &invalid) {
				t.Errorf("%s %s: err = %v, want InvalidRequestTypeError", key, name, err)
			}
		}
	}
}

func TestIAMMFAHandlers_RejectBadUserID(t *testing.T) {
	_, err := handleIAMMFAGetStatus(mfaTenantCtx(), &mockQuicBaaSWithMFA{mfa: &mfaDouble{}},
		&struct{ TenantID, UserID string }{TenantID: "acme", UserID: "not-a-number"})
	var invalid *InvalidUserIDError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidUserIDError", err)
	}
}

// TestIAMMFAVerbsRefuseAnotherTenant proves a caller bound to acme cannot act
// on a user of another tenant: the refusal comes before the manager is called,
// as on the gRPC MFA server.
func TestIAMMFAVerbsRefuseAnotherTenant(t *testing.T) {
	ctx := mfaTenantCtx()
	double := &mfaDouble{}
	client := newMFAClient(t, &mockQuicBaaSWithMFA{mfa: double})

	calls := map[string]func() error{
		"GetMFAStatus": func() error { _, err := client.GetMFAStatus(ctx, "globex", "42"); return err },
		"BeginMFAEnrollment": func() error {
			_, err := client.BeginMFAEnrollment(ctx, "globex", "42", "totp")
			return err
		},
		"DisableMFA":          func() error { return client.DisableMFA(ctx, "globex", "42") },
		"GetBackupCodeStatus": func() error { _, err := client.GetBackupCodeStatus(ctx, "globex", "42"); return err },
		"GenerateBackupCodes": func() error { _, err := client.GenerateBackupCodes(ctx, "globex", "42"); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), "may not act for tenant globex") {
			t.Errorf("%s: err = %v, want a cross-tenant refusal", name, err)
		}
	}
	if double.tenantID != "" || double.disabled {
		t.Fatalf("a cross-tenant call reached the manager (tenant %q)", double.tenantID)
	}
}

// TestIAMMFADispatchRegistered guards the keys the SDK IAM client sends: a
// missing one ends at UnknownMethodError on the embedded transport (#1554).
func TestIAMMFADispatchRegistered(t *testing.T) {
	for _, method := range []string{
		"GetMFAStatus", "BeginMFAEnrollment", "CompleteMFAEnrollment",
		"DisableMFA", "GetBackupCodeStatus", "GenerateBackupCodes",
	} {
		if _, ok := unaryHandlers["quicbaas.IAM.MFA/"+method]; !ok {
			t.Errorf("quicbaas.IAM.MFA/%s is not registered", method)
		}
	}
}
