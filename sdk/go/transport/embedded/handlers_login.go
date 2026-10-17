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

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Unified Login Ceremony Handlers (embedded / in-process)
// =============================================================================
//
// These handlers route the unified login ceremony through the QuicBaaS
// LoginCeremony() facade — the single home for the mTLS anti-spoofing model.
//
// The embedded transport is the in-process trust boundary: the caller runs
// inside the daemon's address space, there is no network peer, and TLS is not
// involved. The handlers therefore mark the context embedded-trusted via
// service.WithEmbeddedTrustedContext so an mtls FinishLogin may use the
// caller-supplied LookupKey. A NETWORK transport must NEVER set this marker;
// it must instead populate the verified peer certificate (see the per-transport
// server handlers).
// =============================================================================

// handleBeginLogin routes BeginLogin to the login facade.
func handleBeginLogin(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*qrdbtransport.BeginLoginRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "BeginLogin", "*transport.BeginLoginRequest", typeNameOf(req))
	}
	login := svc.LoginCeremony()
	if login == nil {
		return nil, &service.LoginCeremonyUnavailableError{}
	}
	return login.BeginLogin(service.WithEmbeddedTrustedContext(ctx), r)
}

// handleFinishLogin routes FinishLogin to the login facade. The context is
// marked embedded-trusted because the embedded transport is in-process.
func handleFinishLogin(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*qrdbtransport.FinishLoginRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "FinishLogin", "*transport.FinishLoginRequest", typeNameOf(req))
	}
	login := svc.LoginCeremony()
	if login == nil {
		return nil, &service.LoginCeremonyUnavailableError{}
	}
	result, err := login.FinishLogin(service.WithEmbeddedTrustedContext(ctx), r)
	if err != nil {
		return nil, err
	}
	// Return the SDK transport shape so the in-process path yields exactly the
	// same type as REST / gRPC / QUIC / MCP.
	return &sdktransport.FinishLoginResponse{
		FinishLoginResponse: result.FinishLoginResponse,
		RefreshToken:        result.RefreshToken,
		RefreshExpiresAt:    result.RefreshExpiresAt,
	}, nil
}
