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
	"strconv"
	"strings"
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
	ID           string      `json:"id"`
	TenantID     string      `json:"tenant_id"`
	Name         string      `json:"name"`
	Host         string      `json:"host"`
	Backend      string      `json:"backend"`
	Model        string      `json:"model,omitempty"`
	AgentType    string      `json:"agent_type"`
	Kind         AgentKind   `json:"kind,omitempty"`
	Status       AgentStatus `json:"status"`
	Capabilities []string    `json:"capabilities,omitempty"`

	// MaxConcurrentJobs is how many orchestration jobs this daemon can run at
	// once -- its contribution to the fleet's worker-slot capacity.
	//
	// THE ONE FACT ADMISSION COULD NOT GET ANYWHERE ELSE. Concurrency used to
	// be bounded per project only, so N projects each inside their own quota
	// oversubscribed the fleet (go-quicbaas#906); the fleet's bound is the sum
	// of this field over the daemons that are alive. See
	// NewAgentFleetCapacity.
	//
	// ZERO MEANS UNREPORTED, NOT ZERO SLOTS. A daemon that predates the field,
	// or one whose operator set no limit, has told us nothing about its
	// capacity -- and a fleet sum that read it as zero would refuse every job
	// on the host. An unreported daemon makes the fleet sum a lower bound
	// rather than a bound, and the measurement says so instead of enforcing it.
	//
	// It is a DECLARATION, not an enforcement point: the daemon does not refuse
	// a spawn past this number, admission does. One authority per bound.
	MaxConcurrentJobs int `json:"max_concurrent_jobs,omitempty"`

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

	// RefreshAgent converges the record of an agent that is registering
	// again onto what it reports NOW, refusing a principal that does not
	// own the record.
	RefreshAgent(ctx context.Context, agent *AgentRegistration) error

	// RemoveAgent removes an agent from the registry.
	RemoveAgent(ctx context.Context, tenantID, agentID string) error

	// Heartbeat updates the last-seen timestamp for an agent and reconciles
	// its backend axis against what the daemon reports it is serving. A nil
	// observation reconciles nothing.
	Heartbeat(ctx context.Context, tenantID, agentID string, observed *AgentObservation) error

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
// Agent principal
// =============================================================================

// AgentPrincipalMetadataKey is where a fleet record names the identity it
// belongs to: the CommonName of the client certificate the daemon registered
// with, which is the subject go-qrdb's mTLS authenticator resolves that daemon
// as (go-quicbaas#674, iam.EnrollAgentCertIdentity). The mTLS register handler
// has always stamped it.
//
// It is the record's OWNER, not a label. A re-registration for an agent ID
// whose record names a different principal is refused rather than applied
// (AgentPrincipalMismatchError), so a certificate cannot rewrite the
// registration of a daemon it does not authenticate as. The check is
// load-bearing because the agent ID is a SANITISED derivation of the
// CommonName -- iam.SanitizeAppName folds space, slash, colon and backslash to
// "-" -- so two distinct certificate subjects can address one agent ID.
const AgentPrincipalMetadataKey = "cert_cn"

// AgentPrincipalKeyMetadataKey is where a fleet record names the KEY the
// principal presented: the canonical SPKI SHA-256 thumbprint
// (qrdbsdk.ComputeSPKIThumbprint), the same identifier #674 binds onto the
// enrolled user record.
//
// It is REFRESHED on every re-registration and never compared, because a
// re-keyed certificate renewal legitimately changes it and
// EnrollAgentCertIdentity re-pins the binding for exactly that reason.
// Comparing it would lock a re-keyed daemon out of its own record with no
// operator remedy short of deleting it. It is recorded so an operator reading
// the fleet can see which key a record is currently bound to.
const AgentPrincipalKeyMetadataKey = "cert_spki"

// AgentPrincipal returns the identity a fleet record belongs to, or "" when
// the record names none -- an operator-created registration, which no
// certificate may take over.
func AgentPrincipal(agent *AgentRegistration) string {
	if agent == nil || agent.Metadata == nil {
		return ""
	}
	return agent.Metadata[AgentPrincipalMetadataKey]
}

// ErrAgentPrincipalMismatch reports a registration for an agent ID whose
// record belongs to a different principal.
var ErrAgentPrincipalMismatch = errors.New("agent registry: agent id belongs to a different principal")

// AgentPrincipalMismatchError names both principals in an identity-takeover
// refusal so an operator can tell a colliding CommonName from an attack.
type AgentPrincipalMismatchError struct {
	TenantID  string
	AgentID   string
	Recorded  string
	Presented string
}

// Error implements the error interface.
func (e *AgentPrincipalMismatchError) Error() string {
	return ErrAgentPrincipalMismatch.Error() + ": tenant=" + e.TenantID +
		" agent=" + e.AgentID + " recorded=" + quoteAgentPrincipal(e.Recorded) +
		" presented=" + quoteAgentPrincipal(e.Presented)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentPrincipalMismatchError) Unwrap() error {
	return ErrAgentPrincipalMismatch
}

// quoteAgentPrincipal renders a principal for an error message, naming an
// absent one rather than printing an empty pair of quotes.
func quoteAgentPrincipal(principal string) string {
	if principal == "" {
		return "(none)"
	}
	return strconv.Quote(principal)
}

// =============================================================================
// Agent observation
// =============================================================================

// AgentTraitGPU marks a daemon whose host offers a GPU. It rides in the same
// Capabilities slice as the daemon's backend names but is NOT one of them:
// dispatch reads it as a property of the HOST (filterAgentsByCapability, and
// the remote backend resolver's gpu clause).
const AgentTraitGPU = "gpu"

// agentCapabilityTraits is the closed set of Capabilities entries that
// describe the host rather than a backend the daemon serves.
//
// Heartbeat reconciliation rewrites the backend axis of a record and must
// leave these exactly as advertised, because the heartbeat's served list is
// Runtime.Backends() -- the backends the runtime constructed, and nothing
// else. pkg/agent appends the gpu trait on the REGISTER payload alone
// (enrollment.go bootstrap), so a reconciliation that took the served list
// wholesale would silently un-advertise a GPU on the first heartbeat.
var agentCapabilityTraits = map[string]struct{}{AgentTraitGPU: {}}

// AgentObservation is what a daemon reports about ITSELF on a heartbeat.
//
// It is distinct from the registration payload: a registration is a claim made
// once at enrollment, while an observation is the running truth, and the two
// diverge whenever a daemon is restarted with a different backend set or
// upgraded in place from an older vocabulary. Reconciling the record against
// it is what keeps dispatch routing on names the daemon actually serves
// (go-quicbaas#699).
//
// A nil *AgentObservation means the caller reported nothing -- the gRPC and
// MCP heartbeat verbs carry no served list -- and reconciles nothing.
type AgentObservation struct {
	// Backends is the set of backend names the daemon's runtime is serving
	// right now (pkg/agent Runtime.Backends()). It carries no host traits.
	//
	// EMPTY IS A FAULT, NOT A CORRECTION. A daemon that constructed no
	// backends serves nothing and can honour no task; reconciling a record
	// down to nothing would quietly retire a fleet member instead of
	// surfacing the fault, so an observation that reports none is refused.
	Backends []string

	// MaxConcurrentJobs is how many orchestration jobs the daemon can run at
	// once, reported on every heartbeat so the fleet's capacity tracks a host
	// being drained or added without anything to keep in step by hand. See
	// AgentRegistration.MaxConcurrentJobs.
	//
	// ZERO RECONCILES NOTHING, which is the same shape a nil observation has
	// for the backend axis: a daemon that reports no count has not reported
	// that its count is zero, and overwriting a number the registration
	// already carries with an unreported one would drop a host out of the
	// fleet sum on its first heartbeat from an older build.
	//
	// A NEGATIVE COUNT IS REFUSED by Validate. It cannot be a capacity, and
	// silently clamping it to zero would turn a caller's bug into a daemon
	// that quietly stopped counting.
	MaxConcurrentJobs int
}

// Validate rejects an observation that reports no served backend, or one whose
// list carries a blank name. Exported because the refusal is part of the
// type's contract, not a private invariant of the registry: a transport that
// wants to answer a daemon serving nothing before it reaches the store asks
// the same question here.
func (o *AgentObservation) Validate() error {
	if o == nil {
		return nil
	}
	if len(o.Backends) == 0 {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "heartbeat reported no served backend: a daemon serving nothing cannot be reconciled, it is a fault to fix",
		}
	}
	for _, backend := range o.Backends {
		if strings.TrimSpace(backend) == "" {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryInvalidInput,
				Message:  "heartbeat reported an empty backend name",
			}
		}
	}
	if o.MaxConcurrentJobs < 0 {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message: "heartbeat reported a negative concurrent-job capacity (" +
				strconv.Itoa(o.MaxConcurrentJobs) + "): use zero to report none",
		}
	}
	return nil
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
