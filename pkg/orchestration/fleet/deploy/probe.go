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

package deploy

import (
	"context"
	"net"
	"time"
)

// nodeProbeTimeout bounds a single liveness dial. The probe runs inside
// the deploy's wait-online budget and is retried, so one attempt is kept
// short enough that a silently dropped SYN does not consume the budget.
const nodeProbeTimeout = 5 * time.Second

// DialFleetRuntime opens and immediately closes a TCP connection to the
// deployed agent's runtime listener.
//
// This is the only check that distinguishes an agent that is running from
// an agent the fleet registry merely remembers: a registration keeps its
// last-seen status after the process is gone, so a deploy that trusts the
// registry alone can report a healthy agent that no longer holds its
// port. The probe is therefore made from the operator's side after the
// deploy's SSH session is closed, which is the moment a systemd --user
// agent without lingering dies.
func DialFleetRuntime(ctx context.Context, addr string) error {
	if addr == "" {
		return &InvalidSpecError{Field: "agent_bind", Reason: "agent probe address is required"}
	}
	dialer := &net.Dialer{Timeout: nodeProbeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return &FleetNodeUnreachableError{Addr: addr, Cause: err}
	}
	if err := conn.Close(); err != nil {
		return &FleetNodeUnreachableError{Addr: addr, Cause: err}
	}
	return nil
}
