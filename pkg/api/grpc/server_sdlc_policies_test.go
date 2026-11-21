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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// TestSDLCConverters_TheTrainAndClusteringSurviveARebind is the gRPC half of
// the rebind regression for the two throughput policies: a bind replaces the
// record, so a mapper that dropped `disabled` would turn an operator's
// disabled train back on at the next rebind.
func TestSDLCConverters_TheTrainAndClusteringSurviveARebind(t *testing.T) {
	binding := &sdlc.ProjectBinding{
		ProjectID:  "p1",
		MergeTrain: sdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7},
		Clustering: sdlc.ClusteringPolicy{Disabled: true, MaxSize: 3},
	}
	back := bindingFromProto(bindingToProto(binding))
	if back.MergeTrain != binding.MergeTrain || back.Clustering != binding.Clustering {
		t.Fatalf("round trip: train %+v clustering %+v, want %+v %+v",
			back.MergeTrain, back.Clustering, binding.MergeTrain, binding.Clustering)
	}
	if zero := bindingFromProto(bindingToProto(&sdlc.ProjectBinding{ProjectID: "p"})); zero.MergeTrain != (sdlc.MergeTrainPolicy{}) ||
		zero.Clustering != (sdlc.ClusteringPolicy{}) {
		t.Fatalf("the defaults did not survive as defaults: %+v %+v", zero.MergeTrain, zero.Clustering)
	}
}
