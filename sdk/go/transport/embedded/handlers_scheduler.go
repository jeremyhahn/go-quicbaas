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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// Async().Scheduler() in process: every job and placement method is served
// by scheduler.Dispatch against the server's own SchedulerService and
// PlacementService, so the embedded transport maps the SDK types exactly as
// the wire transports do.
func init() {
	for _, method := range scheduler.Methods() {
		unaryHandlers[scheduler.ServiceName+"/"+method] = schedulerHandler(method)
	}
}

// schedulerHandler serves one scheduler method in process.
//
// The tenant the services scope by is bound by the transport before any
// handler runs (Transport.Request, inProcessTenant).
func schedulerHandler(method string) handlerFunc {
	return func(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
		return scheduler.Dispatch(ctx, scheduler.Backends{Jobs: svc.Scheduler(), Placement: svc.Placement()}, method, req)
	}
}
