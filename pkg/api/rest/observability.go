//go:build rest

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

package rest

import (
	"strconv"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

var (
	// restShardRequests counts REST operations by shard and entity type.
	restShardRequests    *metrics.CounterVec
	restShardMetricsOnce sync.Once
)

func ensureRestShardMetrics() {
	restShardMetricsOnce.Do(func() {
		restShardRequests = metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "routing",
			Name:      "rest_requests_total",
			Help:      "REST requests routed, labeled by shard and entity type",
		}, []string{"shard_id", "entity_type", "operation"})
	})
}

func incRestShardRequest(shardID uint64, entityType, op string) {
	ensureRestShardMetrics()
	restShardRequests.With(
		strconv.FormatUint(shardID, 10),
		entityType,
		op,
	).Inc()
}
