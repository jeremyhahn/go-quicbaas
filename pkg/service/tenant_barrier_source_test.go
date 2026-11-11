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
	"errors"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// isBarrierUnavailable reports whether err is a QBError with the
// ErrUnavailable code.
func isBarrierUnavailable(err error) bool {
	var qe *pkgerrors.QBError
	return errors.As(err, &qe) && qe.Code == pkgerrors.ErrUnavailable
}

// The facade asks its source for the qrdb client on every call, so a client
// replaced after the facade was built is the one the next call uses.
func TestTenantBarrierServiceFrom_AsksItsSourceOnEveryCall(t *testing.T) {
	first, second := &mockTenantClient{}, &mockTenantClient{}
	var current qrdbtransport.TenantClient = first
	svc := NewTenantBarrierServiceFrom(func() qrdbtransport.TenantClient { return current })

	if err := svc.TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatalf("unseal on the first client: %v", err)
	}
	if first.unsealReq == nil || second.unsealReq != nil {
		t.Fatalf("the first call must reach the first client only: first=%+v second=%+v", first.unsealReq, second.unsealReq)
	}

	first.unsealReq = nil
	current = second
	if err := svc.TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatalf("unseal on the second client: %v", err)
	}
	if first.unsealReq != nil || second.unsealReq == nil {
		t.Fatalf("after the client is replaced the call must reach the new client only: first=%+v second=%+v", first.unsealReq, second.unsealReq)
	}

	if err := svc.TenantBarrierSeal(tenantCtx("acme"), "acme"); err != nil {
		t.Fatalf("seal on the second client: %v", err)
	}
	if first.sealReq != nil || second.sealReq == nil {
		t.Fatalf("seal must reach the new client only: first=%+v second=%+v", first.sealReq, second.sealReq)
	}
}

// While the source has no client every verb answers ErrUnavailable, and the
// tenant binding is still checked first.
func TestTenantBarrierServiceFrom_UnavailableWithoutAClient(t *testing.T) {
	sources := map[string]func() qrdbtransport.TenantClient{
		"source returns nil": func() qrdbtransport.TenantClient { return nil },
		"nil source":         nil,
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			svc := NewTenantBarrierServiceFrom(source)
			ctx := tenantCtx("acme")

			_, initErr := svc.TenantBarrierInit(ctx, "acme", []byte("pp"), 0, 0, "passphrase")
			_, shareErr := svc.TenantBarrierUnsealShare(ctx, "acme", "share")
			_, statusErr := svc.TenantBarrierStatus(ctx, "acme")
			verbs := map[string]error{
				"init":         initErr,
				"unseal":       svc.TenantBarrierUnseal(ctx, "acme", []byte("key")),
				"unseal-share": shareErr,
				"seal":         svc.TenantBarrierSeal(ctx, "acme"),
				"status":       statusErr,
			}
			for verb, err := range verbs {
				if !isBarrierUnavailable(err) {
					t.Errorf("%s without a client: expected ErrUnavailable, got %v", verb, err)
				}
			}

			if err := svc.TenantBarrierUnseal(tenantCtx("other"), "acme", []byte("key")); !isForbidden(err) {
				t.Errorf("cross-tenant unseal without a client: expected ErrForbidden, got %v", err)
			}
			if err := svc.TenantBarrierUnseal(context.Background(), "acme", []byte("key")); !isForbidden(err) {
				t.Errorf("unseal with no principal and no client: expected ErrForbidden, got %v", err)
			}
		})
	}
}
