//go:build rest

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

package rest

import (
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// daemonRoutingStatus is the gateway status for each way a request routed to
// a fleet node daemon can fail.
var daemonRoutingStatus = map[orchestration.DaemonRoutingFault]int{
	orchestration.DaemonUnreachable: http.StatusServiceUnavailable,
	orchestration.DaemonFailed:      http.StatusBadGateway,
}

// respondDaemonRoutingError answers a request that failed on the route to a
// fleet node daemon with 503 or 502 and a message naming the daemon address,
// rather than a bare 500 (go-quicbaas#795). It reports false, writing
// nothing, when err is not a routing failure.
func respondDaemonRoutingError(w http.ResponseWriter, r *http.Request, err error) bool {
	routing, fault, ok := orchestration.ClassifyDaemonRoutingError(err)
	if !ok {
		return false
	}
	RespondError(w, r, daemonRoutingStatus[fault], routing)
	return true
}
