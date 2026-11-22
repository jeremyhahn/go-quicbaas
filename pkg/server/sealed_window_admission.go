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

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsealedwindow "github.com/jeremyhahn/go-qrdb/pkg/api/sealedwindow"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// sealedListenerProbe is the listener probe of the boot-time sealed window.
type sealedListenerProbe = qrdbsealedwindow.ListenerProbe

// newSealedWindowListener builds the sealed window's ONE listener probe over
// the node's live barrier state (SealedWindowProbe) and its ceremony service.
// Every sealed transport is given this same probe so they share one
// Admission: an unseal on any transport waits for the anonymous requests in
// flight on all of them, and every request admitted after it is refused.
func (s *Server) newSealedWindowListener() sealedListenerProbe {
	return qrdbsdk.NewSealedWindowListenerProbe(s.SealedWindowProbe(), s.qrdbCeremonyServicer())
}

// sealedListener returns the sealed window's listener probe, or a probe that
// refuses everything when none was built.
func (s *Server) sealedListener() sealedListenerProbe {
	return qrdbsealedwindow.OrFailClosed(s.sealedWindowListener)
}

// sealedAdmissionUnaryInterceptor holds probe's Admission for each sealed
// gRPC call, exclusively for a Transition method (see
// sealedwindow.GRPCFullMethodTransition), and refuses the call with
// codes.Unavailable once the barrier is unsealed.
func sealedAdmissionUnaryInterceptor(probe sealedListenerProbe) ggrpc.UnaryServerInterceptor {
	transition := sealedwindow.GRPCFullMethodTransition()
	return func(ctx context.Context, req any, info *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (any, error) {
		_, isTransition := transition[info.FullMethod]
		release := probe.Admit(isTransition)
		defer release()
		if !probe.IsSealed() {
			return nil, status.Error(codes.Unavailable, qrdbsealedwindow.ClosedMessage)
		}
		return handler(ctx, req)
	}
}

// sealedAdmissionStreamInterceptor is the streaming sibling of
// sealedAdmissionUnaryInterceptor.
func sealedAdmissionStreamInterceptor(probe sealedListenerProbe) ggrpc.StreamServerInterceptor {
	transition := sealedwindow.GRPCFullMethodTransition()
	return func(srv any, ss ggrpc.ServerStream, info *ggrpc.StreamServerInfo, handler ggrpc.StreamHandler) error {
		_, isTransition := transition[info.FullMethod]
		release := probe.Admit(isTransition)
		defer release()
		if !probe.IsSealed() {
			return status.Error(codes.Unavailable, qrdbsealedwindow.ClosedMessage)
		}
		return handler(srv, ss)
	}
}
