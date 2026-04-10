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

package orchestration

import (
	"context"
	"errors"
)

// AgentStatus represents the lifecycle state of a registered agent.
type AgentStatus string

const (
	// AgentStatusRegistered indicates an agent has been registered but not yet active.
	AgentStatusRegistered AgentStatus = "registered"

	// AgentStatusRunning indicates an agent is actively processing work.
	AgentStatusRunning AgentStatus = "running"

	// AgentStatusIdle indicates an agent is available but not currently working.
	AgentStatusIdle AgentStatus = "idle"

	// AgentStatusOffline indicates an agent is unreachable.
	AgentStatusOffline AgentStatus = "offline"

	// AgentStatusError indicates an agent has encountered an error.
	AgentStatusError AgentStatus = "error"
)

// AgentRegistration represents a registered agent in the fleet registry.
type AgentRegistration struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	AgentType    string            `json:"agent_type"`
	Status       AgentStatus       `json:"status"`
	Capabilities []string          `json:"capabilities,omitempty"`
	ContainerID  string            `json:"container_id,omitempty"`
	WorkDir      string            `json:"work_dir,omitempty"`
	LastSeen     int64             `json:"last_seen"`
	RegisteredAt int64             `json:"registered_at"`
	Version      uint64            `json:"version"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// AgentFilter specifies criteria for listing agents.
type AgentFilter struct {
	Host      string      `json:"host,omitempty"`
	Status    AgentStatus `json:"status,omitempty"`
	AgentType string      `json:"agent_type,omitempty"`
	Backend   string      `json:"backend,omitempty"`
	Model     string      `json:"model,omitempty"`
	Limit     int         `json:"limit,omitempty"`
}

// AgentRegistry manages agent registrations backed by QuicBaaS KV store.
// When QuicBaaS runs in Raft cluster mode, registrations are automatically
// replicated across nodes. In standalone mode, registrations are stored in PebbleDB.
type AgentRegistry interface {
	// RegisterAgent registers a new agent in the fleet registry.
	RegisterAgent(ctx context.Context, agent *AgentRegistration) error

	// GetAgent retrieves an agent by ID within a tenant namespace.
	GetAgent(ctx context.Context, tenantID, agentID string) (*AgentRegistration, error)

	// ListAgents lists agents matching the filter within a tenant.
	ListAgents(ctx context.Context, tenantID string, filter *AgentFilter) ([]*AgentRegistration, error)

	// UpdateAgent updates an existing agent registration.
	UpdateAgent(ctx context.Context, agent *AgentRegistration) error

	// RemoveAgent removes an agent from the registry.
	RemoveAgent(ctx context.Context, tenantID, agentID string) error

	// Heartbeat updates the last-seen timestamp for an agent.
	Heartbeat(ctx context.Context, tenantID, agentID string) error

	// DeleteTenantAgents deletes ALL agents for a tenant (cascade delete).
	DeleteTenantAgents(ctx context.Context, tenantID string) error
}

// =============================================================================
// Agent Registry Request/Response Types
// =============================================================================

// RegisterAgentRequest contains the parameters for registering a new agent.
type RegisterAgentRequest struct {
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	AgentType    string            `json:"agent_type"`
	Capabilities []string          `json:"capabilities,omitempty"`
	WorkDir      string            `json:"work_dir,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// RegisterAgentResponse contains the result of a successful agent registration.
type RegisterAgentResponse struct {
	Agent *AgentRegistration `json:"agent"`
}

// DeployAgentRequest contains the parameters for deploying an agent to a host.
type DeployAgentRequest struct {
	TenantID       string            `json:"tenant_id"`
	Host           string            `json:"host"`
	AgentType      string            `json:"agent_type"`
	Workspace      string            `json:"workspace"`
	SSHUser        string            `json:"ssh_user"`
	ContainerImage string            `json:"container_image"`
	Env            map[string]string `json:"env,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// DeployAgentResponse contains the result of a successful agent deployment.
type DeployAgentResponse struct {
	AgentID     string      `json:"agent_id"`
	ContainerID string      `json:"container_id"`
	Status      AgentStatus `json:"status"`
}

// =============================================================================
// Agent Registry Sentinel Errors
// =============================================================================

var (
	// ErrAgentRegistryNotFound indicates an agent was not found in the registry.
	ErrAgentRegistryNotFound = errors.New("agent registry: not found")

	// ErrAgentRegistryAlreadyExists indicates an agent with the given ID already exists.
	ErrAgentRegistryAlreadyExists = errors.New("agent registry: agent already exists")

	// ErrAgentRegistryInvalidInput indicates invalid input to the agent registry.
	ErrAgentRegistryInvalidInput = errors.New("agent registry: invalid input")

	// ErrAgentRegistryMarshal indicates a serialization or deserialization failure.
	ErrAgentRegistryMarshal = errors.New("agent registry: marshal error")

	// ErrAgentRegistryRequired indicates that an agent registry is required but missing.
	ErrAgentRegistryRequired = errors.New("agent: agent registry is required")

	// ErrAgentNameRequired indicates that the agent name field is required.
	ErrAgentNameRequired = errors.New("agent: agent name is required")

	// ErrAgentHostRequired indicates that the agent host field is required.
	ErrAgentHostRequired = errors.New("agent: agent host is required")

	// ErrAgentTypeRequired indicates that the agent type field is required.
	ErrAgentTypeRequired = errors.New("agent: agent type is required")
)

// =============================================================================
// Agent Registry Typed Errors
// =============================================================================

// AgentNotFoundError indicates an agent was not found by ID.
type AgentNotFoundError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentNotFoundError) Error() string {
	return ErrAgentRegistryNotFound.Error() + ": tenant=" + e.TenantID + " agent=" + e.AgentID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentNotFoundError) Unwrap() error {
	return ErrAgentRegistryNotFound
}

// AgentAlreadyExistsError indicates an agent with the given ID already exists.
type AgentAlreadyExistsError struct {
	AgentID string
}

// Error implements the error interface.
func (e *AgentAlreadyExistsError) Error() string {
	return ErrAgentRegistryAlreadyExists.Error() + ": " + e.AgentID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentAlreadyExistsError) Unwrap() error {
	return ErrAgentRegistryAlreadyExists
}

// AgentRegistryError wraps a sentinel error with an additional message
// for agent registry operations.
type AgentRegistryError struct {
	Sentinel error
	Message  string
}

// Error implements the error interface.
func (e *AgentRegistryError) Error() string {
	if e.Message != "" {
		return e.Sentinel.Error() + ": " + e.Message
	}
	return e.Sentinel.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentRegistryError) Unwrap() error {
	return e.Sentinel
}
