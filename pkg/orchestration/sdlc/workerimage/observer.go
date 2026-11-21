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
	"time"

	utilmetrics "github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// PrometheusObserver exports pass outcomes on the daemon's /metrics:
//
//	quicbaas_sdlc_worker_image_refresh_total{outcome}      passes by outcome
//	quicbaas_sdlc_worker_image_last_outcome_timestamp{outcome}  when each outcome last happened
//
// An alert on a growing "failed" count, or on a stale "promoted"/"current"
// timestamp, is how a refresh that keeps failing reaches a person.
type PrometheusObserver struct {
	total *utilmetrics.CounterVec
	last  *utilmetrics.GaugeVec
}

// NewPrometheusObserver registers the collectors on the daemon's registry.
func NewPrometheusObserver() *PrometheusObserver {
	return &PrometheusObserver{
		total: utilmetrics.NewCounterVec(utilmetrics.CounterOpts{
			Namespace: "quicbaas", Subsystem: "sdlc_worker_image", Name: "refresh_total",
			Help: "Worker image refresh passes, by outcome (current, promoted, partial, failed).",
		}, []string{"outcome"}),
		last: utilmetrics.NewGaugeVec(utilmetrics.GaugeOpts{
			Namespace: "quicbaas", Subsystem: "sdlc_worker_image", Name: "last_outcome_timestamp",
			Help: "Unix time each worker image refresh outcome last occurred.",
		}, []string{"outcome"}),
	}
}

// ObserveRefresh implements Observer.
func (o *PrometheusObserver) ObserveRefresh(outcome Outcome, finished time.Time) {
	o.total.With(string(outcome)).Inc()
	o.last.With(string(outcome)).Set(float64(finished.Unix()))
}
