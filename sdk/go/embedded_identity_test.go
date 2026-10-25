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

package quicbaas

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// bindRefusedError is what a refusing recordingBinder returns.
type bindRefusedError struct{}

func (bindRefusedError) Error() string { return "identity binding refused" }

// recordingBinder records every binding the embedded transport asks for.
type recordingBinder struct {
	refuse         error
	serviceCalls   int
	serviceTenants []string
	delegated      []*x509.Certificate
}

func (b *recordingBinder) ServiceContext(ctx context.Context) (context.Context, error) {
	b.serviceCalls++
	tenantID, ok := tenantctx.Lookup(ctx)
	if ok {
		b.serviceTenants = append(b.serviceTenants, tenantID)
	}
	if b.refuse != nil {
		return nil, b.refuse
	}
	return ctx, nil
}

func (b *recordingBinder) DelegatedContext(ctx context.Context, chain []*x509.Certificate) (context.Context, error) {
	b.delegated = chain
	if b.refuse != nil {
		return nil, b.refuse
	}
	return ctx, nil
}

// newIdentityClient builds an embedded client over a mock service backed by a
// real attestation service, with the given identity option.
func newIdentityClient(t *testing.T, identity Option) Client {
	t.Helper()
	svc := &testutil.MockQuicBaaS{
		AttestationSvc: service.NewAttestationService(nil, nil,
			service.WithAttestationStore(attestationtest.NewMemoryStore())),
	}
	c, err := NewClient("embedded", WithEmbedded(svc), identity)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestWithEmbeddedIdentity_ServicePrincipalBindsEveryCall(t *testing.T) {
	binder := &recordingBinder{}
	c := newIdentityClient(t, WithEmbeddedIdentity(ServicePrincipalIdentity(binder)))
	attestation := c.Security().Attestation()

	for range 2 {
		if _, err := attestation.ListReferenceValues(context.Background()); err != nil {
			t.Fatalf("ListReferenceValues: %v", err)
		}
	}

	if binder.serviceCalls != 2 {
		t.Fatalf("service principal bound %d times, want once per call (2)", binder.serviceCalls)
	}
	// The identity is bound after the tenant, so the binder sees the
	// system tenant an untenanted embedded call acts for.
	for _, tenantID := range binder.serviceTenants {
		if !tenantctx.IsSystem(tenantID) {
			t.Errorf("binder saw tenant %q, want the system tenant", tenantID)
		}
	}
	if len(binder.serviceTenants) != 2 {
		t.Errorf("binder saw a tenant on %d calls, want 2", len(binder.serviceTenants))
	}
}

func TestWithEmbeddedIdentity_DelegationBindsItsChain(t *testing.T) {
	binder := &recordingBinder{}
	chain := []*x509.Certificate{{}}
	c := newIdentityClient(t, WithEmbeddedIdentity(DelegatedIdentity(binder, chain)))

	if _, err := c.Security().Attestation().ListReferenceValues(context.Background()); err != nil {
		t.Fatalf("ListReferenceValues: %v", err)
	}
	if len(binder.delegated) != 1 || binder.delegated[0] != chain[0] {
		t.Fatalf("delegation bound chain %v, want %v", binder.delegated, chain)
	}
	if binder.serviceCalls != 0 {
		t.Errorf("a delegated client bound the service principal %d times", binder.serviceCalls)
	}
}

func TestWithEmbeddedIdentity_RefusedBindingNeverDispatches(t *testing.T) {
	binder := &recordingBinder{refuse: bindRefusedError{}}
	c := newIdentityClient(t, WithEmbeddedIdentity(ServicePrincipalIdentity(binder)))

	_, err := c.Security().Attestation().ListReferenceValues(context.Background())
	if !stderrors.Is(err, bindRefusedError{}) {
		t.Fatalf("err = %v, want the binder's refusal", err)
	}
}

func TestNewClient_EmbeddedWithoutIdentityIsRefused(t *testing.T) {
	c, err := NewClient("embedded", WithEmbedded(&testutil.MockQuicBaaS{}))
	if c != nil {
		t.Fatal("NewClient returned a client without an embedded identity")
	}
	if !sdkerrors.IsEmbeddedIdentityRequired(err) {
		t.Fatalf("err = %v, want EmbeddedIdentityRequiredError", err)
	}
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want it classified as invalid input", err)
	}
}

func TestWithEmbeddedIdentity_RefusesAnEmptyIdentity(t *testing.T) {
	binder := &recordingBinder{}
	cases := map[string]EmbeddedIdentity{
		"zero value":            {},
		"nil service binder":    ServicePrincipalIdentity(nil),
		"nil delegation binder": DelegatedIdentity(nil, []*x509.Certificate{{}}),
		"empty chain":           DelegatedIdentity(binder, nil),
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			err := WithEmbeddedIdentity(id)(defaultOptions())
			if !sdkerrors.IsInvalidInput(err) {
				t.Fatalf("err = %v, want invalid input", err)
			}
		})
	}
}
