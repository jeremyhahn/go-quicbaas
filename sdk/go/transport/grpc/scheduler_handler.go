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

package grpc

import (
	"context"

	"google.golang.org/grpc"

	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// isSchedulerService reports whether service targets Async().Scheduler().
// The SDK scheduler types are plain Go structs, so they cannot ride the
// untyped conn.Invoke fallback; this branch also serves the Unix transport,
// which delegates to gRPC verbatim.
func isSchedulerService(service string) bool {
	return service == scheduler.ServiceName
}

// handleSchedulerRequest serves a job or placement method over the generated
// SchedulerService client: pkg/api/grpc presents both halves in
// service-layer shape and scheduler.Dispatch maps them to the SDK types.
func handleSchedulerRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	client, err := apigrpc.NewClientWithConn(conn)
	if err != nil {
		return nil, err
	}
	return scheduler.Dispatch(ctx, scheduler.Backends{Jobs: client.Jobs(), Placement: client.Placement()}, method, req)
}
