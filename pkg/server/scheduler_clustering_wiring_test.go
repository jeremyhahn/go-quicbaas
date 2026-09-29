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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestSchedulerClustering_StandaloneBootSuppliesNothing pins the standalone
// half of the docs/18 V4 wiring: a single-node boot hands the service builder
// no clustering, which is what selects the single-node engine there.
func TestSchedulerClustering_StandaloneBootSuppliesNothing(t *testing.T) {
	for _, mode := range []string{"", config.ServerModeStandalone} {
		t.Run("mode="+mode, func(t *testing.T) {
			s := &Server{config: &config.Config{Server: config.ServerConfig{Mode: mode}}}
			if got := s.schedulerClustering(); got != nil {
				t.Fatalf("a standalone boot supplied clustering %+v; it must supply none", got)
			}
		})
	}
}

// TestSchedulerClustering_ClusteredBootNeverSuppliesNothing is the half that
// closes docs/18 V4 at the server boundary. A clustered boot must never return
// nil, because nil is how the builder recognises a standalone boot: a nil here
// would put the single-node engine — no leader gate, no firing claim — on every
// node. When the qrdb client cannot supply the leader checker and claim plane,
// the value comes back incomplete, and the builder refuses it
// (TestBuildSchedulerEngine_IncompleteClusteringRefuses in pkg/service).
func TestSchedulerClustering_ClusteredBootNeverSuppliesNothing(t *testing.T) {
	closed := &ShardedClient{}
	closed.closed.Store(true)

	cases := []struct {
		name   string
		mode   string
		client *ShardedClient
	}{
		{name: "cluster without a qrdb client", mode: config.ServerModeCluster},
		{name: "devcluster without a qrdb client", mode: config.ServerModeDevCluster},
		{name: "cluster whose qrdb client cannot serve the shard", mode: config.ServerModeCluster, client: closed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Server: config.ServerConfig{Mode: tc.mode}}
			cfg.Raft.NodeID = 3
			s := &Server{config: cfg, logger: buildTestLogger(t), qrdbClient: tc.client}

			got := s.schedulerClustering()
			if got == nil {
				t.Fatal("a clustered boot supplied no clustering; the builder would read that as standalone")
			}
			if got.NodeID != "3" {
				t.Errorf("NodeID = %q, want the Raft node ID %q", got.NodeID, "3")
			}
			if got.Leader != nil || got.Claims != nil || got.Marks != nil {
				t.Errorf("parts appeared from nowhere: leader=%v claims=%v marks=%v",
					got.Leader != nil, got.Claims != nil, got.Marks != nil)
			}
		})
	}
}
