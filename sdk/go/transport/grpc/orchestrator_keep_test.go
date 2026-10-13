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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestOrchestratorHandler_SpawnKeepCrossesTheWire(t *testing.T) {
	fake := &promptAppendServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterOrchestratorServiceServer(s, fake) })
	h := newOrchestratorHandler(srv.dial(t))

	cases := []struct {
		name string
		keep bool
	}{
		{name: "kept", keep: true},
		{name: "default", keep: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "SpawnSession",
				&orchestrator.SpawnSessionRequest{Backend: "claude", Keep: tc.keep})
			require.NoError(t, err)

			fake.mu.Lock()
			defer fake.mu.Unlock()
			assert.Equal(t, tc.keep, fake.spawn.GetKeep())
		})
	}
}
