//go:build !integration

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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// fakeWebAuthnManagerGetter satisfies webAuthnManagerGetter for tests.
type fakeWebAuthnManagerGetter struct {
	mgr WebAuthnManager
	err error
}

func (f *fakeWebAuthnManagerGetter) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	return f.mgr, f.err
}

// fakeLegacyWebAuthnManager is a non-xkms WebAuthnManager: the FIDO2 service
// must report not-configured for it.
type fakeLegacyWebAuthnManager struct{}

func (f *fakeLegacyWebAuthnManager) BeginRegistration(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeLegacyWebAuthnManager) BeginInviteRegistration(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeLegacyWebAuthnManager) FinishRegistration(_ context.Context, _, _ string, _ map[string]interface{}) (*iam.AttestationResult, error) {
	return nil, nil
}

func (f *fakeLegacyWebAuthnManager) BeginLogin(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeLegacyWebAuthnManager) FinishLogin(_ context.Context, _ string, _ map[string]interface{}) (*iam.User, error) {
	return nil, nil
}

// TestNewFIDO2Service verifies the constructor returns a non-nil service.
func TestNewFIDO2Service(t *testing.T) {
	svc := NewFIDO2Service(&fakeWebAuthnManagerGetter{})
	if svc == nil {
		t.Fatal("NewFIDO2Service returned nil")
	}
}

// TestFIDO2Service_NilRequests verifies every ceremony verb fails closed
// with the typed nil-request error.
func TestFIDO2Service_NilRequests(t *testing.T) {
	svc := NewFIDO2Service(&fakeWebAuthnManagerGetter{})
	ctx := context.Background()

	if _, err := svc.BeginRegistration(ctx, "tenant-a", nil); !stderrors.Is(err, ErrFIDO2NilRequest) {
		t.Errorf("BeginRegistration(nil) = %v, want ErrFIDO2NilRequest", err)
	}
	if _, err := svc.FinishRegistration(ctx, "tenant-a", nil); !stderrors.Is(err, ErrFIDO2NilRequest) {
		t.Errorf("FinishRegistration(nil) = %v, want ErrFIDO2NilRequest", err)
	}
	if _, err := svc.BeginAuthentication(ctx, "tenant-a", nil); !stderrors.Is(err, ErrFIDO2NilRequest) {
		t.Errorf("BeginAuthentication(nil) = %v, want ErrFIDO2NilRequest", err)
	}
	if _, err := svc.FinishAuthentication(ctx, "tenant-a", nil); !stderrors.Is(err, ErrFIDO2NilRequest) {
		t.Errorf("FinishAuthentication(nil) = %v, want ErrFIDO2NilRequest", err)
	}
}

// TestFIDO2Service_TenantRequired verifies every ceremony verb rejects an
// empty tenant with the typed tenant-required error.
func TestFIDO2Service_TenantRequired(t *testing.T) {
	svc := NewFIDO2Service(&fakeWebAuthnManagerGetter{})
	ctx := context.Background()

	if _, err := svc.BeginRegistration(ctx, "", &xkmstransport.BeginRegistrationRequest{Email: "u"}); !stderrors.Is(err, ErrFIDO2TenantRequired) {
		t.Errorf("BeginRegistration empty tenant = %v, want ErrFIDO2TenantRequired", err)
	}
	if _, err := svc.FinishRegistration(ctx, "", &xkmstransport.FinishRegistrationRequest{SessionID: "s"}); !stderrors.Is(err, ErrFIDO2TenantRequired) {
		t.Errorf("FinishRegistration empty tenant = %v, want ErrFIDO2TenantRequired", err)
	}
	if _, err := svc.BeginAuthentication(ctx, "", &xkmstransport.BeginAuthenticationRequest{Email: "u"}); !stderrors.Is(err, ErrFIDO2TenantRequired) {
		t.Errorf("BeginAuthentication empty tenant = %v, want ErrFIDO2TenantRequired", err)
	}
	if _, err := svc.FinishAuthentication(ctx, "", &xkmstransport.FinishAuthenticationRequest{SessionID: "s"}); !stderrors.Is(err, ErrFIDO2TenantRequired) {
		t.Errorf("FinishAuthentication empty tenant = %v, want ErrFIDO2TenantRequired", err)
	}
}

// TestFIDO2Service_NotConfigured verifies the typed not-configured error for
// every resolution failure mode: nil getter, nil manager, and a manager that
// is not the xkms-backed multi-tenant implementation.
func TestFIDO2Service_NotConfigured(t *testing.T) {
	ctx := context.Background()
	req := &xkmstransport.BeginRegistrationRequest{Email: "u"}

	cases := []struct {
		name string
		svc  FIDO2Service
	}{
		{"nil getter", NewFIDO2Service(nil)},
		{"nil manager", NewFIDO2Service(&fakeWebAuthnManagerGetter{mgr: nil})},
		{"legacy manager", NewFIDO2Service(&fakeWebAuthnManagerGetter{mgr: &fakeLegacyWebAuthnManager{}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.svc.BeginRegistration(ctx, "tenant-a", req); !stderrors.Is(err, ErrFIDO2NotConfigured) {
				t.Errorf("BeginRegistration = %v, want ErrFIDO2NotConfigured", err)
			}
		})
	}
}

// TestFIDO2Service_ManagerErrorPropagates verifies a manager-resolution
// error surfaces unchanged to the caller on every verb.
func TestFIDO2Service_ManagerErrorPropagates(t *testing.T) {
	boom := stderrors.New("manager resolution failed")
	svc := NewFIDO2Service(&fakeWebAuthnManagerGetter{err: boom})
	ctx := context.Background()

	if _, err := svc.BeginRegistration(ctx, "tenant-a", &xkmstransport.BeginRegistrationRequest{Email: "u"}); !stderrors.Is(err, boom) {
		t.Errorf("BeginRegistration = %v, want propagated manager error", err)
	}
	if _, err := svc.FinishRegistration(ctx, "tenant-a", &xkmstransport.FinishRegistrationRequest{SessionID: "s"}); !stderrors.Is(err, boom) {
		t.Errorf("FinishRegistration = %v, want propagated manager error", err)
	}
	if _, err := svc.BeginAuthentication(ctx, "tenant-a", &xkmstransport.BeginAuthenticationRequest{Email: "u"}); !stderrors.Is(err, boom) {
		t.Errorf("BeginAuthentication = %v, want propagated manager error", err)
	}
	if _, err := svc.FinishAuthentication(ctx, "tenant-a", &xkmstransport.FinishAuthenticationRequest{SessionID: "s"}); !stderrors.Is(err, boom) {
		t.Errorf("FinishAuthentication = %v, want propagated manager error", err)
	}
}
