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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// newTestWebAuthnFactory builds a relying-party factory over the zero-value
// qrdb client. Construction is all these tests need — the factory is the token
// the binding publishes, and its store wiring is covered in pkg/iam.
func newTestWebAuthnFactory(t *testing.T) *iam.WebAuthnServiceFactory {
	t.Helper()
	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          "localhost",
			RPDisplayName: "QuicBaaS",
			RPOrigins:     []string{"https://localhost:18443"},
		},
		QRDB:                &qrdbsdk.Client{},
		AttestationRecorder: func() qrdbsdk.AttestationRecorder { return testutil.NewMemoryUserAuthFactors() },
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}
	return factory
}

// TestXKMSModuleWebAuthnTenantIsTheSystemTenant pins the tenant the composed
// go-xkms module's single relying party is scoped to. go-xkms builds it with
// rest.DefaultWebAuthnTenant (the qrdb system tenant) because pkg/apimodule
// never sets HostConfig.WebAuthnTenantID; a decorator scoped anywhere else
// would resolve the registrant in the wrong tenant.
func TestXKMSModuleWebAuthnTenantIsTheSystemTenant(t *testing.T) {
	if xkmsModuleWebAuthnTenantID != qrdbsdk.SystemTenantID {
		t.Fatalf("xkms module webauthn tenant = %q, want the qrdb system tenant %q",
			xkmsModuleWebAuthnTenantID, qrdbsdk.SystemTenantID)
	}
}

// TestXKMSModuleWebAuthnDecorator_RefusesUntilBound is the composer-side
// fail-closed guard: the decorator is handed to go-xkms while the qrdb node is
// still being built, so before IAM initialisation it must refuse registrations
// rather than pass the undecorated store through — which is exactly the split
// brain go-quicbaas#484 reported.
func TestXKMSModuleWebAuthnDecorator_RefusesUntilBound(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	decorate, err := s.xkmsModuleWebAuthnCredentialStoreDecorator()
	if err != nil {
		t.Fatalf("xkmsModuleWebAuthnCredentialStoreDecorator: %v", err)
	}

	inner := xkmswebauthn.NewMemoryCredentialStore()
	decorated := decorate(inner)
	if decorated == nil {
		t.Fatal("decorator returned nil for a valid store")
	}
	if decorated == xkmswebauthn.CredentialStore(inner) {
		t.Fatal("decorator returned the store unwrapped; registrations would enroll no login factor")
	}

	cred := &xkmswebauthn.Credential{ID: []byte{1, 2, 3}, UserID: make([]byte, 8)}
	saveErr := decorated.Save(context.Background(), cred)
	var unbound *iam.WebAuthnFactorStoreUnboundError
	if !errors.As(saveErr, &unbound) {
		t.Fatalf("Save error = %v, want *iam.WebAuthnFactorStoreUnboundError", saveErr)
	}
	if _, getErr := inner.GetByCredentialID(context.Background(), cred.ID); !errors.Is(getErr, xkmswebauthn.ErrCredentialNotFound) {
		t.Fatalf("credential was persisted despite the refusal (lookup err = %v)", getErr)
	}
}

// TestBindXKMSModuleWebAuthnFactory_PublishesFactory confirms the late binding
// closes the loop: once IAM initialisation produces a WebAuthn manager, the
// decorator's resolver returns that manager's factory.
func TestBindXKMSModuleWebAuthnFactory_PublishesFactory(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if got := s.webAuthnServiceFactory(); got != nil {
		t.Fatalf("factory = %v before binding, want nil", got)
	}

	factory := newTestWebAuthnFactory(t)
	manager, err := iam.NewXKMSWebAuthnManager(factory)
	if err != nil {
		t.Fatalf("NewXKMSWebAuthnManager: %v", err)
	}
	s.bindXKMSModuleWebAuthnFactory(&iamComponents{webauthnManager: manager})

	if got := s.webAuthnServiceFactory(); got != factory {
		t.Fatalf("resolver returned %v, want the bound factory %v", got, factory)
	}
}

// TestBindXKMSModuleWebAuthnFactory_LeavesUnboundWithoutAManager covers the
// node that never initialised a WebAuthn surface: the binding must leave the
// resolver empty (so the decorator keeps failing closed) instead of panicking
// or publishing a nil factory as if it were real.
func TestBindXKMSModuleWebAuthnFactory_LeavesUnboundWithoutAManager(t *testing.T) {
	tests := []struct {
		name       string
		components *iamComponents
	}{
		{"nil components", nil},
		{"no webauthn manager", &iamComponents{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{logger: buildTestLogger(t)}
			s.bindXKMSModuleWebAuthnFactory(tc.components)
			if got := s.webAuthnServiceFactory(); got != nil {
				t.Fatalf("factory = %v, want nil so the decorator keeps failing closed", got)
			}
		})
	}
}
