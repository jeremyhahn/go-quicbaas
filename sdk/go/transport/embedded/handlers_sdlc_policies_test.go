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
	"testing"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestSDLCEmbedded_TheTrainAndClusteringSurviveARebind proves the in-process
// hop carries both throughput policies in both directions.
func TestSDLCEmbedded_TheTrainAndClusteringSurviveARebind(t *testing.T) {
	sdk := &sdlc.ProjectBinding{
		ProjectID:  "p1",
		MergeTrain: sdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7},
		Clustering: sdlc.ClusteringPolicy{MaxSize: 3},
	}
	domain := sdlcBindingToDomain(sdk)
	if domain.MergeTrain != (pkgsdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7}) ||
		domain.Clustering != (pkgsdlc.ClusteringPolicy{MaxSize: 3}) {
		t.Fatalf("to the module: %+v %+v", domain.MergeTrain, domain.Clustering)
	}
	back := sdlcBindingFromDomain(domain)
	if back.MergeTrain != sdk.MergeTrain || back.Clustering != sdk.Clustering {
		t.Fatalf("back to the SDK: %+v %+v", back.MergeTrain, back.Clustering)
	}
}
