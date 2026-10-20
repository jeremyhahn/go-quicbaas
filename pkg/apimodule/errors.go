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

package apimodule

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// PeerCertificateTenantError reports a verified client certificate whose
// tenant qrdb's certificate-to-tenant rule refuses, for example a tenant SAN
// from another SPIFFE trust domain.
type PeerCertificateTenantError struct {
	// Cause is qrdb's refusal.
	Cause error
}

// Error describes the refusal.
func (e *PeerCertificateTenantError) Error() string {
	return "quicbaas module: client certificate names no acceptable tenant: " + e.Cause.Error()
}

// Unwrap returns qrdb's refusal.
func (e *PeerCertificateTenantError) Unwrap() error { return e.Cause }

// ErrorCode classifies the refusal as unauthorized.
func (e *PeerCertificateTenantError) ErrorCode() qberrors.ErrorCode {
	return qberrors.ErrUnauthorized
}

// GRPCStatus answers the refusal as Unauthenticated: the caller's
// credential, not its permissions, is what was refused.
func (e *PeerCertificateTenantError) GRPCStatus() *status.Status {
	return status.New(codes.Unauthenticated, e.Error())
}

// SurfaceComposingError reports a module surface consulted before the
// backing engine finished composing. Authentication fails closed on it.
type SurfaceComposingError struct {
	// Surface names the consulted surface.
	Surface string
}

// Error describes the composing state.
func (e *SurfaceComposingError) Error() string {
	return "quicbaas module: " + e.Surface + " is not yet available (server still composing)"
}

// ErrorCode classifies the composing state as unavailable, the class REST
// (503) and gRPC (Unavailable) answer it with.
func (e *SurfaceComposingError) ErrorCode() qberrors.ErrorCode {
	return qberrors.ErrUnavailable
}
