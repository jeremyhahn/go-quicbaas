//go:build !integration

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

// agent_registrar_adapter_test.go covers the adapter that bridges the
// narrow iam.AgentRegistrar interface to the richer orchestration registry.

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestNewAgentRegistrarAdapter(t *testing.T) {
	registry := orchestration.NewInMemoryAgentRegistry()
	a := newAgentRegistrarAdapter(registry)
	require.NotNil(t, a)
	assert.Same(t, registry, a.registry)
}

func TestAgentRegistrarAdapter_RegisterAgent_NilRegistryIsNoOp(t *testing.T) {
	a := newAgentRegistrarAdapter(nil)
	require.NotNil(t, a)
	// With a nil registry the adapter must not panic and must return
	// nil so the central can continue without orchestration.
	err := a.RegisterAgent(context.Background(), &iam.AgentRegistrarRequest{
		AgentID: "agent-1",
		Name:    "daemon-A",
		Host:    "10.0.0.1",
	})
	require.NoError(t, err)
}

func TestAgentRegistrarAdapter_RegisterAgent_PublishesToRegistry(t *testing.T) {
	registry := orchestration.NewInMemoryAgentRegistry()
	a := newAgentRegistrarAdapter(registry)

	err := a.RegisterAgent(context.Background(), &iam.AgentRegistrarRequest{
		AgentID:   "agent-1",
		TenantID:  "default",
		Name:      "daemon-A",
		Host:      "10.0.0.1:5555",
		Backends:  []string{"ollama", "claude"},
		AgentType: "agent-runtime",
	})
	require.NoError(t, err)

	// The registration must reach the in-memory registry under the
	// supplied AgentID.
	got, err := registry.GetAgent(context.Background(), "default", "agent-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "daemon-A", got.Name)
	assert.Equal(t, "10.0.0.1:5555", got.Host)
	// Primary backend selection: first backend listed.
	assert.Equal(t, "ollama", got.Backend)
	assert.Equal(t, []string{"ollama", "claude"}, got.Capabilities)
}

func TestAgentRegistrarAdapter_RegisterAgent_EmptyBackendsYieldsEmptyPrimary(t *testing.T) {
	registry := orchestration.NewInMemoryAgentRegistry()
	a := newAgentRegistrarAdapter(registry)

	err := a.RegisterAgent(context.Background(), &iam.AgentRegistrarRequest{
		AgentID:  "agent-2",
		TenantID: "default",
		Name:     "daemon-B",
		Host:     "10.0.0.2",
	})
	require.NoError(t, err)
	got, err := registry.GetAgent(context.Background(), "default", "agent-2")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got.Backend, "empty Backends must leave the primary backend label empty")
}
