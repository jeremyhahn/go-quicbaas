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

package workerimage

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	utilmetrics "github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// TestPrometheusObserver_CountsEveryOutcome proves a failed pass reaches the
// daemon's /metrics registry, which is what an alert watches.
func TestPrometheusObserver_CountsEveryOutcome(t *testing.T) {
	observer := NewPrometheusObserver()
	finished := time.Unix(1_800_000_000, 0)
	observer.ObserveRefresh(OutcomeFailed, finished)
	observer.ObserveRefresh(OutcomeFailed, finished)

	want := `
# HELP quicbaas_sdlc_worker_image_last_outcome_timestamp Unix time each worker image refresh outcome last occurred.
# TYPE quicbaas_sdlc_worker_image_last_outcome_timestamp gauge
quicbaas_sdlc_worker_image_last_outcome_timestamp{outcome="failed"} 1.8e+09
`
	if err := testutil.GatherAndCompare(utilmetrics.Registry(), strings.NewReader(want),
		"quicbaas_sdlc_worker_image_last_outcome_timestamp"); err != nil {
		t.Fatal(err)
	}
	count, err := testutil.GatherAndCount(utilmetrics.Registry(), "quicbaas_sdlc_worker_image_refresh_total")
	if err != nil || count < 1 {
		t.Fatalf("refresh_total series = %d, %v", count, err)
	}
}
