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

package server

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// agentRegistrarAdapter bridges the narrow iam.AgentRegistrar interface to
// the richer orchestration.AgentRegistry. The iam package deliberately does
// not import pkg/orchestration to keep layer boundaries clean; this adapter
// lives in pkg/server where both packages meet.
//
// Used by the mTLS register handler (pkg/api/rest/handlers_agent_register.go) to
// publish enrollment-time registrations. The orchestration layer gets the
// same registry instance via Server.agentRegistry + SetAgentRegistry, so
// dispatch sees the daemon the moment enrollment completes — no waiting
// for the first heartbeat.
type agentRegistrarAdapter struct {
	registry orchestration.AgentRegistry
}

// Compile-time interface check.
var _ iam.AgentRegistrar = (*agentRegistrarAdapter)(nil)

// newAgentRegistrarAdapter constructs an adapter over the given registry.
func newAgentRegistrarAdapter(registry orchestration.AgentRegistry) *agentRegistrarAdapter {
	return &agentRegistrarAdapter{registry: registry}
}

// RegisterAgent translates the flat iam.AgentRegistrarRequest into a proper
// orchestration.AgentRegistration and publishes it to the fleet registry.
//
// Multiple backends on a single daemon yield a single registration whose
// Backend field holds the primary backend name plus a Capabilities list of
// all backends. This matches how container-script agents are recorded and
// lets ListAgents(filter.Backend="ollama") find daemons that serve multiple
// backend kinds.
func (a *agentRegistrarAdapter) RegisterAgent(ctx context.Context, req *iam.AgentRegistrarRequest) error {
	if a.registry == nil {
		return nil // No-op when the central is running without orchestration
	}

	now := time.Now().UnixNano()
	primary := ""
	if len(req.Backends) > 0 {
		primary = req.Backends[0]
	}

	reg := &orchestration.AgentRegistration{
		ID:           req.AgentID,
		TenantID:     req.TenantID,
		Name:         req.Name,
		Host:         req.Host,
		Backend:      primary,
		AgentType:    req.AgentType,
		Kind:         orchestration.AgentKind(req.Kind),
		Status:       orchestration.AgentStatusRegistered,
		Capabilities: req.Backends,
		LastSeen:     now,
		RegisteredAt: now,
		Metadata:     req.Metadata,
	}
	return a.registry.RegisterAgent(ctx, reg)
}
