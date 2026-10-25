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

package iam

import (
	"bytes"
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// TestWebAuthnServiceFactory_UnauthenticatedCeremonyResolvesAccount runs an
// invite registration and a passkey login against a node that enforces
// authorization, with no caller identity on any ceremony context — the state
// a browser is in before it has authenticated. Both ceremonies resolve their
// account and rewrite its display name; each must succeed without the caller
// holding users:read or users:update.
func TestWebAuthnServiceFactory_UnauthenticatedCeremonyResolvesAccount(t *testing.T) {
	const (
		email  = "invitee@example.com"
		rpID   = "example.com"
		origin = "https://example.com"
	)
	n := qrdbnode.TenantHost(t)
	admin := qrdbsdk.ContextWithTenant(context.Background(), qrdbsdk.SystemTenantID)
	invitee, err := n.Node.UserService().CreateUser(admin, &qrdbsdk.CreateUserRequest{
		Username: email,
		Email:    email,
		Name:     "Invitee",
		TenantID: qrdbsdk.SystemTenantID,
		Pending:  true,
	})
	if err != nil {
		t.Fatalf("create pending invitee: %v", err)
	}
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config:              WebAuthnFactoryConfig{RPID: rpID, RPDisplayName: "Example", RPOrigins: []string{origin}},
		QRDB:                n.Node.Client(),
		Service:             n.Node,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
		Assurance:           WebAuthnAssuranceAny,
		Admins:              &stubAdminResolver{},
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}
	svc, err := f.ServiceFor(context.Background(), qrdbsdk.SystemTenantID)
	if err != nil {
		t.Fatalf("ServiceFor: %v", err)
	}
	authenticator, err := xkmswebauthn.NewMockAuthenticator(rpID)
	if err != nil {
		t.Fatalf("NewMockAuthenticator: %v", err)
	}
	handle := xkmswebauthn.HandleFromUserID(invitee.ID)
	unauthenticated := context.Background()

	creation, sessionID, err := svc.BeginInviteRegistration(unauthenticated, email)
	if err != nil {
		t.Fatalf("BeginInviteRegistration: %v", err)
	}
	attestation, err := authenticator.CreateAttestationObject(creation.Response.Challenge, handle, origin)
	if err != nil {
		t.Fatalf("CreateAttestationObject: %v", err)
	}
	if _, err := svc.FinishInviteRegistration(unauthenticated, sessionID, attestation); err != nil {
		t.Fatalf("FinishInviteRegistration: %v", err)
	}

	// Activation is the invite flow's next step and an administrator's write,
	// not part of either ceremony.
	if _, err := n.Node.UserService().UpdateUser(admin, &qrdbsdk.UpdateUserRequest{
		ID:        invitee.ID,
		Lifecycle: qrdbsdk.LifecycleActive,
	}); err != nil {
		t.Fatalf("activate invitee: %v", err)
	}

	assertion, sessionID, err := svc.BeginLogin(unauthenticated, handle)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	response, err := authenticator.CreateAssertionResponse(assertion.Response.Challenge, handle, origin)
	if err != nil {
		t.Fatalf("CreateAssertionResponse: %v", err)
	}
	_, user, err := svc.FinishLogin(unauthenticated, sessionID, handle, response)
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if !bytes.Equal(user.WebAuthnID(), handle) {
		t.Fatalf("FinishLogin user handle = %x, want %x", user.WebAuthnID(), handle)
	}
}
