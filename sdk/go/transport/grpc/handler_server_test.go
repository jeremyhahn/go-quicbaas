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

package grpc

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// handlerTestServer is an in-memory gRPC server used by the handler-level
// tests. Callers register one or more proto service implementations via the
// register callback, then dial it to obtain a real *grpc.ClientConn that the
// handlers convert to/from SDK types against. Everything runs over a bufconn
// listener so no host networking is touched and the tests stay fast.
type handlerTestServer struct {
	lis    *bufconn.Listener
	server *grpc.Server
}

// newHandlerServer starts an in-memory gRPC server. The register callback is
// invoked with the *grpc.Server before Serve so proto service implementations
// can be registered.
func newHandlerServer(t *testing.T, register func(s *grpc.Server), opts ...grpc.ServerOption) *handlerTestServer {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer(opts...)
	register(s)
	hs := &handlerTestServer{lis: lis, server: s}
	go func() {
		_ = s.Serve(lis)
	}()
	t.Cleanup(func() {
		s.Stop()
		_ = lis.Close()
	})
	return hs
}

// dial returns a real client connection to the in-memory server. The
// connection is closed automatically when the test finishes.
func (hs *handlerTestServer) dial(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return hs.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("failed to dial in-memory server: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	return conn
}

// failErr returns an Internal gRPC error when fail is set, otherwise nil. It
// lets fake servers share a single error-injection convention.
func failErr(fail bool) error {
	if fail {
		return status.Error(codes.Internal, "boom")
	}
	return nil
}
