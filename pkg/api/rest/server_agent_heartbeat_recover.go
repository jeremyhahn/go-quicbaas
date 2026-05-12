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
	"context"
	"net/http"
)

// AgentHeartbeatRecoverer is the contract the orchestrator heartbeat
// handler uses to recover from a missing-registry-entry condition. The
// production implementation lives in handlers_agent_heartbeat_recover.go
// (built under the iam tag) and walks the request's mTLS peer cert + the
// AgentRegistrar to re-publish the agent. Builds without iam leave the
// recoverer unset and the heartbeat handler falls through to its
// original 404 path.
//
// Recover returns true ONLY when it has just made the heartbeat
// idempotent — i.e. a follow-up AgentHeartbeat call against the same
// (tenant, agentID) will succeed. Implementations MUST log their
// decision so operators see when an agent has been re-published, and
// MUST NOT panic on any input.
type AgentHeartbeatRecoverer interface {
	Recover(ctx context.Context, r *http.Request, tenantID, agentID string, payload *heartbeatBody) bool
}

// SetAgentHeartbeatRecoverer wires the recoverer used by
// HandleOrchestratorAgentHeartbeat to self-heal after a central restart
// drops its in-memory registry. Pass nil (or never call) to disable —
// the handler then preserves the original 404 contract. Wired from
// pkg/server lifecycle once the iam components are initialized.
//
// Stored as interface{} so the core rest package does not need the iam
// build tag; the iam-tagged tryRecoverAgentHeartbeat implementation
// performs the type assertion at call time.
func (s *Server) SetAgentHeartbeatRecoverer(recoverer interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentHeartbeatRecoverer = recoverer
}

// agentHeartbeatRecovererSnapshot returns the wired recoverer under the
// server's read lock so a concurrent SetAgentHeartbeatRecoverer call
// from another goroutine cannot race the handler reading it.
func (s *Server) agentHeartbeatRecovererSnapshot() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentHeartbeatRecoverer
}

// tryRecoverAgentHeartbeat is the default (no-iam) implementation that
// reports "no recovery available". Builds with the iam tag override
// this method via build-tag specialisation in
// handlers_agent_heartbeat_recover.go so the production type assertion
// stays out of the core rest package.
//
// The split keeps pkg/api/rest/server.go free of the iam dependency
// while still presenting a single call site in
// HandleOrchestratorAgentHeartbeat — the handler does not branch on
// build tags.
func (s *Server) tryRecoverAgentHeartbeat(ctx context.Context, r *http.Request, tenantID, agentID string, payload *heartbeatBody) bool {
	rec := s.agentHeartbeatRecovererSnapshot()
	if rec == nil {
		return false
	}
	if recoverer, ok := rec.(AgentHeartbeatRecoverer); ok {
		return recoverer.Recover(ctx, r, tenantID, agentID, payload)
	}
	return false
}
