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
	"errors"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestSecurityBarrier_InitializeShamirIsRefused proves Security().Barrier()
// refuses the system init-shamir verb with the typed ceremony refusal and
// never sends it to the server.
func TestSecurityBarrier_InitializeShamirIsRefused(t *testing.T) {
	srv, paths := newBarrierRESTStub(t)

	opts := defaultOptions()
	opts.transportType = TransportREST
	opts.tlsEnabled = true
	opts.tlsConfig = stubTLSConfig(t, srv)
	c := &client{opts: opts, address: strings.TrimPrefix(srv.URL, "https://")}

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}

	resp, err := barrier.BarrierInitializeShamir(context.Background(),
		&qrdbtransport.BarrierInitializeShamirRequest{Threshold: 2, TotalShares: 3})
	if resp != nil {
		t.Errorf("refused init-shamir returned a response: %+v", resp)
	}
	var refused *sdkerrors.SystemShamirInitRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("init-shamir error = %v, want SystemShamirInitRefusedError", err)
	}
	if !sdkerrors.IsClientError(err) || sdkerrors.Code(err) != sdkerrors.CodePreconditionFailed {
		t.Errorf("init-shamir code = %v, want CodePreconditionFailed", sdkerrors.Code(err))
	}
	if !strings.Contains(err.Error(), "quicbaas init initialize") {
		t.Errorf("init-shamir error %q does not name the ceremony", err)
	}
	for _, p := range *paths {
		if strings.Contains(p, "shamir") {
			t.Errorf("refused init-shamir reached the server at %s", p)
		}
	}
}

// TestSecurityBarrier_OtherVerbsStillReachTheServer guards the refusal
// wrapper against swallowing the rest of the barrier surface.
func TestSecurityBarrier_OtherVerbsStillReachTheServer(t *testing.T) {
	srv, _ := newBarrierRESTStub(t)

	opts := defaultOptions()
	opts.transportType = TransportREST
	opts.tlsEnabled = true
	opts.tlsConfig = stubTLSConfig(t, srv)
	c := &client{opts: opts, address: strings.TrimPrefix(srv.URL, "https://")}

	barrier, err := c.Security().Barrier()
	if err != nil {
		t.Fatalf("Security().Barrier(): %v", err)
	}
	status, err := barrier.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus: %v", err)
	}
	if status.Strategy != "passphrase" {
		t.Errorf("strategy: got %q, want passphrase", status.Strategy)
	}
}
