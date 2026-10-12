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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// errFIDO2 is a sentinel returned by the fake FIDO2 service to drive
// error-propagation paths through the embedded handlers.
var errFIDO2 = errors.New("fido2: boom")

// fakeFIDO2Service is a configurable FIDO2Service. When failNext is true
// every method returns errFIDO2; otherwise it returns a populated response
// and records the tenant and request it received.
type fakeFIDO2Service struct {
	failNext   bool
	lastTenant string
}

func (f *fakeFIDO2Service) BeginRegistration(ctx context.Context, tenantID string, req *xkmstransport.BeginRegistrationRequest) (*xkmstransport.BeginRegistrationResponse, error) {
	f.lastTenant = tenantID
	if f.failNext {
		return nil, errFIDO2
	}
	return &xkmstransport.BeginRegistrationResponse{
		SessionID:   "sess-1",
		OptionsJSON: `{"publicKey":{}}`,
	}, nil
}

func (f *fakeFIDO2Service) BeginAddCredential(ctx context.Context, tenantID string) (*xkmstransport.BeginRegistrationResponse, error) {
	f.lastTenant = tenantID
	if f.failNext {
		return nil, errFIDO2
	}
	return &xkmstransport.BeginRegistrationResponse{SessionID: "add-sess", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fakeFIDO2Service) FinishRegistration(ctx context.Context, tenantID string, req *xkmstransport.FinishRegistrationRequest) (*xkmstransport.FinishRegistrationResponse, error) {
	f.lastTenant = tenantID
	if f.failNext {
		return nil, errFIDO2
	}
	return &xkmstransport.FinishRegistrationResponse{
		Success:      true,
		CredentialID: "cred-1",
		Token:        "tok-1",
	}, nil
}

func (f *fakeFIDO2Service) BeginAuthentication(ctx context.Context, tenantID string, req *xkmstransport.BeginAuthenticationRequest) (*xkmstransport.BeginAuthenticationResponse, error) {
	f.lastTenant = tenantID
	if f.failNext {
		return nil, errFIDO2
	}
	return &xkmstransport.BeginAuthenticationResponse{
		SessionID:   "sess-2",
		OptionsJSON: `{"publicKey":{}}`,
	}, nil
}

func (f *fakeFIDO2Service) FinishAuthentication(ctx context.Context, tenantID string, req *xkmstransport.FinishAuthenticationRequest) (*xkmstransport.FinishAuthenticationResponse, error) {
	f.lastTenant = tenantID
	if f.failNext {
		return nil, errFIDO2
	}
	return &xkmstransport.FinishAuthenticationResponse{
		Success:  true,
		Token:    "tok-2",
		Username: req.Email,
	}, nil
}

// fido2Mock embeds the base mock and overrides FIDO2().
type fido2Mock struct {
	*mockQuicBaaS
	fido2 service.FIDO2Service
}

func (m *fido2Mock) FIDO2() service.FIDO2Service { return m.fido2 }

func newFIDO2Transport(t *testing.T, fido2 service.FIDO2Service) *Transport {
	t.Helper()
	tr, err := New(&fido2Mock{mockQuicBaaS: &mockQuicBaaS{}, fido2: fido2})
	require.NoError(t, err)
	return tr
}

func TestTransport_FIDO2BeginRegistration(t *testing.T) {
	fake := &fakeFIDO2Service{}
	tr := newFIDO2Transport(t, fake)
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := tr.Request(ctx, "quicbaas.FIDO2", "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Email: "alice@example.com", DisplayName: "Alice"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.BeginRegistrationResponse)
	require.True(t, ok)
	assert.Equal(t, "sess-1", typed.SessionID)
	assert.Equal(t, "tenant-a", fake.lastTenant, "tenant must ride the context into the service layer")
}

func TestTransport_FIDO2FinishRegistration(t *testing.T) {
	fake := &fakeFIDO2Service{}
	tr := newFIDO2Transport(t, fake)
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := tr.Request(ctx, "quicbaas.FIDO2", "FinishRegistration",
		&xkmstransport.FinishRegistrationRequest{SessionID: "sess-1", AttestationJSON: "{}"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.FinishRegistrationResponse)
	require.True(t, ok)
	assert.True(t, typed.Success)
	assert.Equal(t, "cred-1", typed.CredentialID)
	assert.Equal(t, "tenant-a", fake.lastTenant)
}

// TestTransport_FIDO2BeginAddCredential pins that the payload-less
// add-credential verb reaches the service with the context tenant.
func TestTransport_FIDO2BeginAddCredential(t *testing.T) {
	fake := &fakeFIDO2Service{}
	tr := newFIDO2Transport(t, fake)
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := tr.Request(ctx, "quicbaas.FIDO2", "BeginAddCredential", nil)
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.BeginRegistrationResponse)
	require.True(t, ok)
	assert.Equal(t, "add-sess", typed.SessionID)
	assert.Equal(t, "tenant-a", fake.lastTenant)
}

func TestTransport_FIDO2BeginAuthentication(t *testing.T) {
	fake := &fakeFIDO2Service{}
	tr := newFIDO2Transport(t, fake)
	ctx := transport.ContextWithTenant(context.Background(), "tenant-b")
	resp, err := tr.Request(ctx, "quicbaas.FIDO2", "BeginAuthentication",
		&xkmstransport.BeginAuthenticationRequest{Email: "alice@example.com"})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.BeginAuthenticationResponse)
	require.True(t, ok)
	assert.Equal(t, "sess-2", typed.SessionID)
	assert.Equal(t, "tenant-b", fake.lastTenant)
}

func TestTransport_FIDO2FinishAuthentication(t *testing.T) {
	fake := &fakeFIDO2Service{}
	tr := newFIDO2Transport(t, fake)
	ctx := transport.ContextWithTenant(context.Background(), "tenant-b")
	resp, err := tr.Request(ctx, "quicbaas.FIDO2", "FinishAuthentication",
		&xkmstransport.FinishAuthenticationRequest{
			SessionID: "sess-2", Email: "alice@example.com", AssertionJSON: "{}",
		})
	require.NoError(t, err)
	typed, ok := resp.(*xkmstransport.FinishAuthenticationResponse)
	require.True(t, ok)
	assert.True(t, typed.Success)
	assert.Equal(t, "alice@example.com", typed.Username)
	assert.Equal(t, "tenant-b", fake.lastTenant)
}

// TestTransport_FIDO2_NoTenantInContext verifies the handler passes an
// empty tenant through so the service layer's canonical typed error
// (ErrFIDO2TenantRequired in production) governs the fail-closed path.
func TestTransport_FIDO2_NoTenantInContext(t *testing.T) {
	fake := &fakeFIDO2Service{}
	tr := newFIDO2Transport(t, fake)
	_, err := tr.Request(context.Background(), "quicbaas.FIDO2", "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Email: "alice@example.com"})
	require.NoError(t, err)
	assert.Equal(t, "", fake.lastTenant)
}

// TestTransport_FIDO2Operations_ServiceError verifies every FIDO2 verb
// propagates the service-layer error unchanged.
func TestTransport_FIDO2Operations_ServiceError(t *testing.T) {
	tr := newFIDO2Transport(t, &fakeFIDO2Service{failNext: true})
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")

	requests := map[string]interface{}{
		"BeginRegistration":    &xkmstransport.BeginRegistrationRequest{},
		"FinishRegistration":   &xkmstransport.FinishRegistrationRequest{},
		"BeginAddCredential":   nil,
		"BeginAuthentication":  &xkmstransport.BeginAuthenticationRequest{},
		"FinishAuthentication": &xkmstransport.FinishAuthenticationRequest{},
	}
	for method, req := range requests {
		_, err := tr.Request(ctx, "quicbaas.FIDO2", method, req)
		require.ErrorIs(t, err, errFIDO2, "method %s", method)
	}
}

// TestTransport_FIDO2Operations_InvalidRequestType verifies every FIDO2
// verb rejects a mistyped request with the typed InvalidRequestTypeError.
func TestTransport_FIDO2Operations_InvalidRequestType(t *testing.T) {
	tr := newFIDO2Transport(t, &fakeFIDO2Service{})
	ctx := context.Background()

	methods := []string{
		"BeginRegistration", "FinishRegistration",
		"BeginAuthentication", "FinishAuthentication",
	}
	for _, method := range methods {
		_, err := tr.Request(ctx, "quicbaas.FIDO2", method, struct{ Bogus string }{})
		var typed *InvalidRequestTypeError
		require.ErrorAs(t, err, &typed, "method %s", method)
	}
}

// TestTransport_FIDO2Operations_ServiceNotConfigured verifies the typed
// not-configured error when FIDO2() returns nil.
func TestTransport_FIDO2Operations_ServiceNotConfigured(t *testing.T) {
	tr := newFIDO2Transport(t, nil)
	_, err := tr.Request(context.Background(), "quicbaas.FIDO2", "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{})
	var typed *ServiceNotConfiguredError
	require.ErrorAs(t, err, &typed)
}
