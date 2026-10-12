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

package orchestration

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// DaemonRoutingFault classifies a request the control plane could not
// complete on a fleet node daemon.
type DaemonRoutingFault int

const (
	// DaemonUnreachable: the daemon could not be dialed or did not answer in
	// time. A gateway reports it as 503 Service Unavailable.
	DaemonUnreachable DaemonRoutingFault = iota + 1
	// DaemonFailed: the daemon answered with a server-side failure. A gateway
	// reports it as 502 Bad Gateway.
	DaemonFailed
)

// DaemonRoutingError is a request routed to the fleet node daemon at Address
// for Backend that did not complete there (go-quicbaas#795). It names the
// address so an operator can see which node to fix, and wraps the transport
// or RPC error unchanged.
type DaemonRoutingError struct {
	Backend string
	Address string
	Op      string
	Err     error
}

// Error implements the error interface.
func (e *DaemonRoutingError) Error() string {
	if fault, ok := e.Fault(); ok && fault == DaemonUnreachable {
		return fmt.Sprintf("backend %q: fleet node daemon at %s is unreachable: %v", e.Backend, e.Address, e.Err)
	}
	return fmt.Sprintf("backend %q: fleet node daemon at %s failed %s: %v", e.Backend, e.Address, e.Op, e.Err)
}

// Unwrap returns the wrapped error for errors.Is / errors.As support.
func (e *DaemonRoutingError) Unwrap() error { return e.Err }

// daemonRoutingFaults maps the gRPC codes that describe the route rather than
// the request. Every other code (PermissionDenied, NotFound, InvalidArgument,
// ...) is the daemon's answer about the request and keeps its own mapping.
var daemonRoutingFaults = map[codes.Code]DaemonRoutingFault{
	codes.Unavailable:      DaemonUnreachable,
	codes.DeadlineExceeded: DaemonUnreachable,
	codes.Unknown:          DaemonFailed,
	codes.Internal:         DaemonFailed,
	codes.Unimplemented:    DaemonFailed,
	codes.DataLoss:         DaemonFailed,
}

// Fault classifies the failure. It reports false when the daemon answered
// about the request itself, which callers map by its own meaning.
func (e *DaemonRoutingError) Fault() (DaemonRoutingFault, bool) {
	var dialErr *RemoteBackendDialError
	if errors.As(e.Err, &dialErr) || errors.Is(e.Err, context.DeadlineExceeded) {
		return DaemonUnreachable, true
	}
	st, ok := grpcstatus.FromError(e.Err)
	if !ok {
		return 0, false
	}
	fault, ok := daemonRoutingFaults[st.Code()]
	return fault, ok
}

// ClassifyDaemonRoutingError reports the routing failure err carries, if any.
func ClassifyDaemonRoutingError(err error) (*DaemonRoutingError, DaemonRoutingFault, bool) {
	var routing *DaemonRoutingError
	if !errors.As(err, &routing) {
		return nil, 0, false
	}
	fault, ok := routing.Fault()
	if !ok {
		return nil, 0, false
	}
	return routing, fault, true
}
