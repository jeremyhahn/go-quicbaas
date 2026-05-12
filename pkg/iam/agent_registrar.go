//go:build iam

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

package iam

import "context"

// =============================================================================
// Agent Registrar
// =============================================================================
//
// The AgentRegistrar interface is the narrow dependency the mTLS
// registration handler uses to publish newly-registered daemons into
// the central orchestrator's fleet registry. The interface is declared
// here (not imported from pkg/orchestration) to keep the iam package
// free of the orchestration dependency; the glue at
// pkg/server/agent_registrar_adapter.go supplies an adapter that
// forwards to the real orchestration.AgentRegistry.

// AgentRegistrar publishes newly-registered agents into the fleet
// registry. Called once per successful registration redemption at the
// mTLS register endpoint.
type AgentRegistrar interface {
	// RegisterAgent publishes an agent into the fleet registry. The
	// request shape mirrors orchestration.AgentRegistration via string
	// / string-map fields so this interface carries no cross-package
	// dependency.
	RegisterAgent(ctx context.Context, req *AgentRegistrarRequest) error
}

// AgentRegistrarRequest is the data the register handler hands to the
// registrar. It is a flat struct designed to be trivially translatable
// into orchestration.AgentRegistration by the adapter in pkg/server.
type AgentRegistrarRequest struct {
	// AgentID is the opaque identifier assigned to the daemon when its
	// OAuth2 application was registered.
	AgentID string

	// TenantID is the tenant claim extracted from the daemon's mTLS
	// peer certificate.
	TenantID string

	// Name is a human-readable label for the registry row.
	Name string

	// Host is the daemon's reachable bind address.
	Host string

	// Backends is the declared backend list from the register payload.
	Backends []string

	// AgentType is a short label identifying the kind of runtime
	// (e.g. "agent-runtime").
	AgentType string

	// Kind classifies the runtime ("daemon" for long-lived agents).
	Kind string

	// Metadata carries free-form context for audit / debugging.
	Metadata map[string]string
}

// DefaultAgentRuntimeScopes returns the scopes granted to a daemon
// registering via the default agent-runtime role. Kept in sync with
// pkg/orchestration/agent_identity.go.
func DefaultAgentRuntimeScopes() []string {
	return []string{
		"agent:runtime:spawn",
		"agent:runtime:prompt",
		"agent:runtime:status",
	}
}
