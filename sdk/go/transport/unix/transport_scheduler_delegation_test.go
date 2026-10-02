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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// TestTransport_Scheduler_Delegates proves the Unix transport routes every
// Async().Scheduler() method — jobs and placement — through the gRPC
// scheduler handler. With no server listening each call fails on the
// connection, but never as "unknown scheduler method" or "invalid request
// type", which would mean the dispatch never saw the SDK's own request. The
// gRPC transport tests drive the same handler against a real server.
func TestTransport_Scheduler_Delegates(t *testing.T) {
	requests := map[string]interface{}{
		"CreateJob":      &scheduler.CreateJobRequest{Name: "n", Handler: "h"},
		"GetJob":         &scheduler.GetJobRequest{JobID: 1},
		"GetStatus":      &scheduler.GetStatusRequest{},
		"RegisterNode":   &scheduler.RegisterNodeRequest{Facts: scheduler.NodeFacts{NodeID: "n"}, TTLSeconds: 5},
		"GetNode":        &scheduler.NodeRequest{NodeID: "n"},
		"ListNodes":      &scheduler.ListNodesRequest{},
		"DrainNode":      &scheduler.NodeRequest{NodeID: "n"},
		"Place":          &scheduler.PlacementRequest{WorkloadID: "w"},
		"Release":        &scheduler.WorkloadRequest{WorkloadID: "w"},
		"ListPlacements": &scheduler.PlacementFilter{},
	}
	for method, req := range requests {
		t.Run(method, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, scheduler.ServiceName, method, req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			for _, wrong := range []string{"unknown scheduler method", "invalid request type", "not supported"} {
				if strings.Contains(err.Error(), wrong) {
					t.Fatalf("scheduler %s was not delegated to the gRPC handler: %v", method, err)
				}
			}
		})
	}
}
