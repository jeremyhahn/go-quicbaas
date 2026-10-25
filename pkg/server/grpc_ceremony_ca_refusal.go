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

	ggrpc "google.golang.org/grpc"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// qrdbCeremonyInitializeMethod is the full gRPC method name of qrdb's
// ceremony initialization RPC, served on both the TCP gRPC listener and the
// Unix socket.
const qrdbCeremonyInitializeMethod = "/qrdb.v1.QRDBService/CeremonyInitialize"

// Admin-credential modes that need the platform ceremony CA.
const (
	ceremonyModeCSRSign  = "csr-sign"
	ceremonyModeGenerate = "generated-admin"
)

// CeremonyCATransportRefusedError is returned when a ceremony initialization
// selecting a CA-needing admin-credential mode (CSR-sign or generated admin)
// arrives over the qrdb.v1 gRPC or Unix surface. The go-qrdb SDK does not
// re-export the option that hands that surface the platform ceremony CA
// (go-qrdb #1129), so the request is refused here instead of reaching qrdb's
// ceremony core without a CA.
type CeremonyCATransportRefusedError struct {
	Mode string
}

func (e *CeremonyCATransportRefusedError) Error() string {
	return "ceremony: the " + e.Mode + " admin-credential mode needs the platform ceremony CA, " +
		"which the gRPC and Unix transports cannot reach until go-qrdb #1129; " +
		"use REST (POST /api/v1/init/initialize, quicbaas init initialize --transport rest) or MCP, " +
		"or --admin-cert, which works on every transport"
}

// ceremonyAdminSelector is the part of qrdb's CeremonyInitializeRequest that
// selects the admin-credential mode.
type ceremonyAdminSelector interface {
	GetAdminCsrPem() string
	GetAdminGenerate() bool
}

// ceremonyCARefusalUnaryInterceptor refuses qrdb.v1 CeremonyInitialize
// requests whose admin-credential mode needs the ceremony CA, with gRPC
// FailedPrecondition and a CeremonyCATransportRefusedError. Every other
// request, including bring-your-own-certificate initialization, passes
// through unchanged.
func ceremonyCARefusalUnaryInterceptor() ggrpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (any, error) {
		if info.FullMethod != qrdbCeremonyInitializeMethod {
			return handler(ctx, req)
		}
		mode := ceremonyCAMode(req)
		if mode == "" {
			return handler(ctx, req)
		}
		return nil, grpcapi.ToGRPCError(qberrors.Wrap(
			&CeremonyCATransportRefusedError{Mode: mode},
			qberrors.ErrPreconditionFailed,
			"server.CeremonyInitialize",
		))
	}
}

// ceremonyCAMode returns the CA-needing admin-credential mode req selects, or
// "" when it selects none.
func ceremonyCAMode(req any) string {
	selector, ok := req.(ceremonyAdminSelector)
	if !ok {
		return ""
	}
	if selector.GetAdminCsrPem() != "" {
		return ceremonyModeCSRSign
	}
	if selector.GetAdminGenerate() {
		return ceremonyModeGenerate
	}
	return ""
}
