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
	"sync"
	"sync/atomic"
)

// EmbeddedNodeRunningError is returned when a Server tries to build an
// embedded go-qrdb node while another Server in the same process already
// holds one. go-quicbaas runs one embedded node per process.
type EmbeddedNodeRunningError struct{}

// Error implements error.
func (e *EmbeddedNodeRunningError) Error() string {
	return "an embedded go-qrdb node is already running in this process; go-quicbaas runs one node per process"
}

// nodeSlot records which Server holds the process's embedded go-qrdb node.
// An unbounded slot lets every Server claim it.
type nodeSlot struct {
	mu        sync.Mutex
	holder    *Server
	unbounded bool
}

// claim takes the slot for s. It succeeds when the slot is free or s already
// holds it, and returns an *EmbeddedNodeRunningError when another Server does.
func (n *nodeSlot) claim(s *Server) error {
	if n.unbounded {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.holder == nil || n.holder == s {
		n.holder = s
		return nil
	}
	return &EmbeddedNodeRunningError{}
}

// release frees the slot when s holds it. It is a no-op otherwise, and on an
// unbounded slot.
func (n *nodeSlot) release(s *Server) {
	if n.unbounded {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.holder == s {
		n.holder = nil
	}
}

// processNodeSlot is the process-wide slot production code claims.
var processNodeSlot = &nodeSlot{}

// activeNodeSlot is the test binary's seam: pkg/server's TestMain stores an
// unbounded slot here, because that one binary builds hundreds of Servers,
// and the guard's own tests swap a real slot in. Production code never
// stores into it.
var activeNodeSlot atomic.Pointer[nodeSlot]

// currentNodeSlot returns the slot in force: activeNodeSlot when a test has
// set it, and processNodeSlot otherwise.
func currentNodeSlot() *nodeSlot {
	if n := activeNodeSlot.Load(); n != nil {
		return n
	}
	return processNodeSlot
}
