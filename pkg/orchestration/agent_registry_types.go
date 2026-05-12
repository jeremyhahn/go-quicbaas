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

	// AgentStatusDisabled indicates an agent has been administratively disabled.
	// A disabled agent cannot claim work or authenticate until re-enabled.
	AgentStatusDisabled AgentStatus = "disabled"
)

// AgentKind classifies how an agent registration is provisioned and how the
// central orchestrator should route work to it. The field is optional for
// backward compatibility — empty values are treated as AgentKindExternal.
type AgentKind string

const (
	// AgentKindDaemon identifies a long-lived pkg/agent daemon process that
	// exposes the AgentRuntime gRPC service (see api/proto/agent_runtime.proto).
	// Registrations of this kind are routed via RemoteACPBackend.
	AgentKindDaemon AgentKind = "daemon"

	// AgentKindContainerScript identifies an agent provisioned via the
	// deployments/agents/*/entrypoint.sh script family — a per-session
	// container spawned by the central host's container runtime.
	AgentKindContainerScript AgentKind = "container_script"

	// AgentKindExternal identifies a manually registered agent — the legacy
	// path for registrations created before AgentKind existed, or for
	// operators who manage agent lifecycle out-of-band.
	AgentKindExternal AgentKind = "external"

	// AgentKindEmbedded identifies an agent that runs in-process with
	// quicbaasd and communicates via a bufconn loopback transport rather than
	// a real network socket. The embedded agent is a first-class registered
	// agent with a real mTLS identity; its gRPC channel simply bypasses the
	// network stack for zero-copy, zero-latency dispatch.
	AgentKindEmbedded AgentKind = "embedded"
)

// EffectiveKind returns the AgentKind for a registration, defaulting to
// AgentKindExternal when the field is unset. This preserves backward
// compatibility with pre-Phase-4 registrations that lack the field.
func (a *AgentRegistration) EffectiveKind() AgentKind {
	if a == nil || a.Kind == "" {
		return AgentKindExternal
	}
	return a.Kind
}

// AgentRegistration represents a registered agent in the fleet registry.
type AgentRegistration struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	AgentType    string            `json:"agent_type"`
	Kind         AgentKind         `json:"kind,omitempty"`
	Status       AgentStatus       `json:"status"`
	Capabilities []string          `json:"capabilities,omitempty"`
	ContainerID  string            `json:"container_id,omitempty"`
	WorkDir      string            `json:"work_dir,omitempty"`
	LastSeen     int64             `json:"last_seen"`
	RegisteredAt int64             `json:"registered_at"`
	DisabledAt   int64             `json:"disabled_at,omitempty"`
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

// =============================================================================
// Agent Disable / Enable Typed Errors
// =============================================================================

// ErrAgentDisabled is the sentinel error when a disabled agent attempts an action.
var ErrAgentDisabled = errors.New("agent: agent is disabled")

// ErrAgentAlreadyDisabled is the sentinel error for disabling an already-disabled agent.
var ErrAgentAlreadyDisabled = errors.New("agent: agent is already disabled")

// ErrAgentNotDisabled is the sentinel error for enabling an agent that is not disabled.
var ErrAgentNotDisabled = errors.New("agent: agent is not disabled")

// AgentDisabledError is returned when a disabled agent attempts to perform an operation.
type AgentDisabledError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentDisabledError) Error() string {
	return ErrAgentDisabled.Error() + ": tenant=" + e.TenantID + " agent=" + e.AgentID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentDisabledError) Unwrap() error { return ErrAgentDisabled }

// AgentAlreadyDisabledError is returned when an operator tries to disable an already-disabled agent.
type AgentAlreadyDisabledError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentAlreadyDisabledError) Error() string {
	return ErrAgentAlreadyDisabled.Error() + ": tenant=" + e.TenantID + " agent=" + e.AgentID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentAlreadyDisabledError) Unwrap() error { return ErrAgentAlreadyDisabled }

// AgentNotDisabledError is returned when an operator tries to enable an agent that is not disabled.
type AgentNotDisabledError struct {
	TenantID string
	AgentID  string
}

// Error implements the error interface.
func (e *AgentNotDisabledError) Error() string {
	return ErrAgentNotDisabled.Error() + ": tenant=" + e.TenantID + " agent=" + e.AgentID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentNotDisabledError) Unwrap() error { return ErrAgentNotDisabled }

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
