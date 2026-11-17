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

package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// statusOnlyBarrier is a BarrierServicer double that answers BarrierStatus;
// every other method is the embedded nil interface and is never called.
type statusOnlyBarrier struct {
	qrdbsdk.BarrierServicer
	calls int
}

func (b *statusOnlyBarrier) BarrierStatus(context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	b.calls++
	return &qrdbsdk.BarrierStatusInfo{Sealed: true, Strategy: "proxy-test"}, nil
}

// TestQRDBBarrierServicerProxy_ServesBarrierWiredAfterRegistration is the
// #2488 regression: the node's gRPC server registers the barrier before any
// barrier servicer is wired. The proxy built then must serve the barrier once
// it is wired, because it resolves the servicer per call.
func TestQRDBBarrierServicerProxy_ServesBarrierWiredAfterRegistration(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if s.qrdbBarrierServicer() != nil {
		t.Fatal("precondition: no barrier servicer is wired at registration time")
	}
	proxy := newQRDBBarrierServicerProxy(s)

	backing := &statusOnlyBarrier{}
	s.sealedWindowClient = &qrdbsdk.Client{}
	s.ceremonyServices = &CeremonyServices{Barrier: backing}

	info, err := proxy.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus after wiring: %v", err)
	}
	if backing.calls != 1 {
		t.Fatalf("BarrierStatus must delegate to the wired servicer once, got %d calls", backing.calls)
	}
	if info == nil || !info.Sealed || info.Strategy != "proxy-test" {
		t.Fatalf("BarrierStatus must return the wired servicer's answer, got %+v", info)
	}
}

// TestQRDBBarrierServicerProxy_UnimplementedWhenNothingWired pins the answer
// of a node with no barrier servicer: the same Unimplemented "barrier service
// is not configured" an unregistered barrier gave.
func TestQRDBBarrierServicerProxy_UnimplementedWhenNothingWired(t *testing.T) {
	proxy := newQRDBBarrierServicerProxy(&Server{logger: buildTestLogger(t)})
	ctx := context.Background()

	_, statusErr := proxy.BarrierStatus(ctx)
	_, hasErr := proxy.BarrierHasRecoveryKeys(ctx)
	errs := map[string]error{
		"BarrierStatus":          statusErr,
		"BarrierSeal":            proxy.BarrierSeal(ctx),
		"BarrierUnseal":          proxy.BarrierUnseal(ctx, []byte("x")),
		"BarrierHasRecoveryKeys": hasErr,
	}
	for name, err := range errs {
		if status.Code(err) != codes.Unimplemented {
			t.Fatalf("%s: expected codes.Unimplemented, got %v", name, err)
		}
		if status.Convert(err).Message() != "barrier service is not configured" {
			t.Fatalf("%s: unexpected message %q", name, status.Convert(err).Message())
		}
	}

	if _, err := newQRDBBarrierServicerProxy(nil).BarrierStatus(ctx); status.Code(err) != codes.Unimplemented {
		t.Fatalf("a proxy without a server must answer Unimplemented, got %v", err)
	}
}
