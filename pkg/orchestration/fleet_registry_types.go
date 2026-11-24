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

// FleetNodeStatus represents the lifecycle state of a registered fleet node.
type FleetNodeStatus string

const (
	// FleetNodeStatusRegistered indicates a fleet node has been registered but not yet active.
	FleetNodeStatusRegistered FleetNodeStatus = "registered"

	// FleetNodeStatusRunning indicates a fleet node is actively processing work.
	FleetNodeStatusRunning FleetNodeStatus = "running"

	// FleetNodeStatusIdle indicates a fleet node is available but not currently working.
	FleetNodeStatusIdle FleetNodeStatus = "idle"

	// FleetNodeStatusOffline indicates a fleet node is unreachable.
	FleetNodeStatusOffline FleetNodeStatus = "offline"

	// FleetNodeStatusError indicates a fleet node has encountered an error.
	FleetNodeStatusError FleetNodeStatus = "error"

	// FleetNodeStatusDisabled indicates a fleet node has been administratively disabled.
	// A disabled fleet node cannot claim work or authenticate until re-enabled.
	FleetNodeStatusDisabled FleetNodeStatus = "disabled"
)

// FleetNodeKind classifies how a fleet node registration is provisioned and how the
// central orchestrator should route work to it. The field is optional for
// backward compatibility — empty values are treated as FleetNodeKindExternal.
type FleetNodeKind string

const (
	// FleetNodeKindDaemon identifies a long-lived pkg/orchestration/fleet daemon process that
	// exposes the FleetRuntime gRPC service (see api/proto/fleet_runtime.proto).
	// Registrations of this kind are routed via RemoteACPBackend.
	FleetNodeKindDaemon FleetNodeKind = "daemon"

	// FleetNodeKindContainerScript identifies a fleet node provisioned via the
	// deployments/agents/*/entrypoint.sh script family — a per-session
	// container spawned by the central host's container runtime.
	FleetNodeKindContainerScript FleetNodeKind = "container_script"

	// FleetNodeKindExternal identifies a manually registered fleet node — the legacy
	// path for registrations created before FleetNodeKind existed, or for
	// operators who manage fleet node lifecycle out-of-band.
	FleetNodeKindExternal FleetNodeKind = "external"

	// FleetNodeKindEmbedded identifies a fleet node that runs in-process with
	// quicbaasd and communicates via a bufconn loopback transport rather than
	// a real network socket. The embedded fleet node is a first-class registered
	// fleet node with a real mTLS identity; its gRPC channel simply bypasses the
	// network stack for zero-copy, zero-latency dispatch.
	FleetNodeKindEmbedded FleetNodeKind = "embedded"
)

// EffectiveKind returns the FleetNodeKind for a registration, defaulting to
// FleetNodeKindExternal when the field is unset. This preserves backward
// compatibility with pre-Phase-4 registrations that lack the field.
func (a *FleetNode) EffectiveKind() FleetNodeKind {
	if a == nil || a.Kind == "" {
		return FleetNodeKindExternal
	}
	return a.Kind
}

// FleetNode represents a registered fleet node in the fleet registry.
type FleetNode struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	Name         string          `json:"name"`
	Host         string          `json:"host"`
	Backend      string          `json:"backend"`
	Model        string          `json:"model,omitempty"`
	NodeType     string          `json:"node_type"`
	Kind         FleetNodeKind   `json:"kind,omitempty"`
	Status       FleetNodeStatus `json:"status"`
	Capabilities []string        `json:"capabilities,omitempty"`

	// AdvertisedAddress is the host:port the node itself declared the control
	// plane should dial (fleet serve --advertise). Empty when the node
	// declared none and Host was derived from its bind address.
	AdvertisedAddress string `json:"advertised_address,omitempty"`

	// Reachability is whether the control plane dialed Host when it last
	// registered the node. Empty on a record that predates the field.
	Reachability FleetNodeReachability `json:"reachability,omitempty"`

	// MaxConcurrentJobs is how many orchestration jobs this daemon can run at
	// once -- its contribution to the fleet's worker-slot capacity.
	//
	// THE ONE FACT ADMISSION COULD NOT GET ANYWHERE ELSE. Concurrency used to
	// be bounded per project only, so N projects each inside their own quota
	// oversubscribed the fleet (go-quicbaas#906); the fleet's bound is the sum
	// of this field over the daemons that are alive. See
	// NewRegistryFleetCapacity.
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

	// Images are the container images present on the fleet node's host, as the
	// fleet node last reported them. Placement sends a spawn that names an image
	// only to a fleet node that has it: worker images are loaded onto hosts, not
	// pulled, so a fleet node without one fails the spawn at image pull.
	//
	// NIL MEANS NEVER REPORTED (an older build), which placement treats as
	// unknown rather than empty; an empty list is a host with no images.
	Images []string `json:"images,omitempty"`

	// HostMetrics is the latest host-wide sample the fleet node reported —
	// CPU, load, memory and disk — that the fleet health verdict is formed
	// from (FleetHealthJudge). NIL MEANS NEVER MEASURED, and a node never
	// measured is unhealthy.
	HostMetrics *HostMetrics `json:"host_metrics,omitempty"`

	WorkDir      string            `json:"work_dir,omitempty"`
	LastSeen     int64             `json:"last_seen"`
	RegisteredAt int64             `json:"registered_at"`
	DisabledAt   int64             `json:"disabled_at,omitempty"`
	Version      uint64            `json:"version"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// FleetNodeFilter specifies criteria for listing fleet nodes.
type FleetNodeFilter struct {
	Host     string          `json:"host,omitempty"`
	Status   FleetNodeStatus `json:"status,omitempty"`
	NodeType string          `json:"node_type,omitempty"`
	Backend  string          `json:"backend,omitempty"`
	Model    string          `json:"model,omitempty"`
	Limit    int             `json:"limit,omitempty"`
}

// FleetRegistry manages fleet node registrations backed by QuicBaaS KV store.
// When QuicBaaS runs in Raft cluster mode, registrations are automatically
// replicated across nodes. In standalone mode, registrations are stored in PebbleDB.
type FleetRegistry interface {
	// RegisterFleetNode registers a new fleet node in the fleet registry.
	RegisterFleetNode(ctx context.Context, agent *FleetNode) error

	// GetFleetNode retrieves a fleet node by ID within a tenant namespace.
	GetFleetNode(ctx context.Context, tenantID, nodeID string) (*FleetNode, error)

	// ListFleetNodes lists fleet nodes matching the filter within a tenant.
	ListFleetNodes(ctx context.Context, tenantID string, filter *FleetNodeFilter) ([]*FleetNode, error)

	// UpdateFleetNode updates an existing fleet node registration.
	UpdateFleetNode(ctx context.Context, agent *FleetNode) error

	// RefreshFleetNode converges the record of a fleet node that is registering
	// again onto what it reports NOW, refusing a principal that does not
	// own the record.
	RefreshFleetNode(ctx context.Context, agent *FleetNode) error

	// RemoveFleetNode removes a fleet node from the registry.
	RemoveFleetNode(ctx context.Context, tenantID, nodeID string) error

	// Heartbeat updates the last-seen timestamp for a fleet node and reconciles
	// its backend axis against what the daemon reports it is serving. A nil
	// observation reconciles nothing.
	Heartbeat(ctx context.Context, tenantID, nodeID string, observed *FleetNodeObservation) error

	// DeleteTenantFleetNodes deletes ALL fleet nodes for a tenant (cascade delete).
	DeleteTenantFleetNodes(ctx context.Context, tenantID string) error
}

// =============================================================================
// Fleet Registry Request/Response Types
// =============================================================================

// RegisterFleetNodeRequest contains the parameters for registering a new fleet node.
type RegisterFleetNodeRequest struct {
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	NodeType     string            `json:"node_type"`
	Capabilities []string          `json:"capabilities,omitempty"`
	WorkDir      string            `json:"work_dir,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// RegisterFleetNodeResponse contains the result of a successful fleet node registration.
type RegisterFleetNodeResponse struct {
	Node *FleetNode `json:"node"`
}

// DeployFleetNodeRequest contains the parameters for deploying a fleet node to a host.
type DeployFleetNodeRequest struct {
	TenantID       string            `json:"tenant_id"`
	Host           string            `json:"host"`
	NodeType       string            `json:"node_type"`
	Workspace      string            `json:"workspace"`
	SSHUser        string            `json:"ssh_user"`
	ContainerImage string            `json:"container_image"`
	Env            map[string]string `json:"env,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// DeployFleetNodeResponse contains the result of a successful fleet node deployment.
type DeployFleetNodeResponse struct {
	NodeID      string          `json:"node_id"`
	ContainerID string          `json:"container_id"`
	Status      FleetNodeStatus `json:"status"`
}

// =============================================================================
// Fleet Registry Sentinel Errors
// =============================================================================

var (
	// ErrFleetNodeNotFound indicates a fleet node was not found in the registry.
	ErrFleetNodeNotFound = errors.New("agent registry: not found")

	// ErrFleetNodeAlreadyExists indicates a fleet node with the given ID already exists.
	ErrFleetNodeAlreadyExists = errors.New("agent registry: agent already exists")

	// ErrFleetRegistryInvalidInput indicates invalid input to the fleet registry.
	ErrFleetRegistryInvalidInput = errors.New("agent registry: invalid input")

	// ErrFleetRegistryMarshal indicates a serialization or deserialization failure.
	ErrFleetRegistryMarshal = errors.New("agent registry: marshal error")

	// ErrFleetRegistryRequired indicates that a fleet registry is required but missing.
	ErrFleetRegistryRequired = errors.New("agent: agent registry is required")

	// ErrFleetNodeNameRequired indicates that the fleet node name field is required.
	ErrFleetNodeNameRequired = errors.New("agent: agent name is required")

	// ErrFleetNodeHostRequired indicates that the fleet node host field is required.
	ErrFleetNodeHostRequired = errors.New("agent: agent host is required")

	// ErrFleetNodeTypeRequired indicates that the fleet node type field is required.
	ErrFleetNodeTypeRequired = errors.New("agent: agent type is required")
)

// =============================================================================
// Fleet Registry Typed Errors
// =============================================================================

// FleetNodeNotFoundError indicates a fleet node was not found by ID.
type FleetNodeNotFoundError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetNodeNotFoundError) Error() string {
	return ErrFleetNodeNotFound.Error() + ": tenant=" + e.TenantID + " agent=" + e.NodeID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FleetNodeNotFoundError) Unwrap() error {
	return ErrFleetNodeNotFound
}

// FleetNodeAlreadyExistsError indicates a fleet node with the given ID already exists.
type FleetNodeAlreadyExistsError struct {
	NodeID string
}

// Error implements the error interface.
func (e *FleetNodeAlreadyExistsError) Error() string {
	return ErrFleetNodeAlreadyExists.Error() + ": " + e.NodeID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FleetNodeAlreadyExistsError) Unwrap() error {
	return ErrFleetNodeAlreadyExists
}

// =============================================================================
// Fleet node principal
// =============================================================================

// FleetNodePrincipalMetadataKey is where a fleet record names the identity it
// belongs to: the CommonName of the client certificate the daemon registered
// with, which is the subject go-qrdb's mTLS authenticator resolves that daemon
// as (go-quicbaas#674, iam.EnrollFleetNodeCertIdentity). The mTLS register handler
// has always stamped it.
//
// It is the record's OWNER, not a label. A re-registration for a fleet node ID
// whose record names a different principal is refused rather than applied
// (FleetNodePrincipalMismatchError), so a certificate cannot rewrite the
// registration of a daemon it does not authenticate as. The check is
// load-bearing because the fleet node ID is a SANITISED derivation of the
// CommonName -- iam.SanitizeAppName folds space, slash, colon and backslash to
// "-" -- so two distinct certificate subjects can address one fleet node ID.
const FleetNodePrincipalMetadataKey = "cert_cn"

// FleetNodePrincipalKeyMetadataKey is where a fleet record names the KEY the
// principal presented: the canonical SPKI SHA-256 thumbprint
// (qrdbsdk.ComputeSPKIThumbprint), the same identifier #674 binds onto the
// enrolled user record.
//
// It is REFRESHED on every re-registration and never compared, because a
// re-keyed certificate renewal legitimately changes it and
// EnrollFleetNodeCertIdentity re-pins the binding for exactly that reason.
// Comparing it would lock a re-keyed daemon out of its own record with no
// operator remedy short of deleting it. It is recorded so an operator reading
// the fleet can see which key a record is currently bound to.
const FleetNodePrincipalKeyMetadataKey = "cert_spki"

// FleetNodePrincipal returns the identity a fleet record belongs to, or "" when
// the record names none -- an operator-created registration, which no
// certificate may take over.
func FleetNodePrincipal(agent *FleetNode) string {
	if agent == nil || agent.Metadata == nil {
		return ""
	}
	return agent.Metadata[FleetNodePrincipalMetadataKey]
}

// ErrFleetNodePrincipalMismatch reports a registration for a fleet node ID whose
// record belongs to a different principal.
var ErrFleetNodePrincipalMismatch = errors.New("agent registry: agent id belongs to a different principal")

// FleetNodePrincipalMismatchError names both principals in an identity-takeover
// refusal so an operator can tell a colliding CommonName from an attack.
type FleetNodePrincipalMismatchError struct {
	TenantID  string
	NodeID    string
	Recorded  string
	Presented string
}

// Error implements the error interface.
func (e *FleetNodePrincipalMismatchError) Error() string {
	return ErrFleetNodePrincipalMismatch.Error() + ": tenant=" + e.TenantID +
		" agent=" + e.NodeID + " recorded=" + quoteFleetNodePrincipal(e.Recorded) +
		" presented=" + quoteFleetNodePrincipal(e.Presented)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FleetNodePrincipalMismatchError) Unwrap() error {
	return ErrFleetNodePrincipalMismatch
}

// quoteFleetNodePrincipal renders a principal for an error message, naming an
// absent one rather than printing an empty pair of quotes.
func quoteFleetNodePrincipal(principal string) string {
	if principal == "" {
		return "(none)"
	}
	return strconv.Quote(principal)
}

// =============================================================================
// Fleet node observation
// =============================================================================

// FleetNodeTraitGPU marks a daemon whose host offers a GPU. It rides in the same
// Capabilities slice as the daemon's backend names but is NOT one of them:
// dispatch reads it as a property of the HOST (filterFleetNodesByCapability, and
// the remote backend resolver's gpu clause).
const FleetNodeTraitGPU = "gpu"

// fleetNodeCapabilityTraits is the closed set of Capabilities entries that
// describe the host rather than a backend the daemon serves.
//
// Heartbeat reconciliation rewrites the backend axis of a record and must
// leave these exactly as advertised, because the heartbeat's served list is
// Runtime.Backends() -- the backends the runtime constructed, and nothing
// else. pkg/orchestration/fleet appends the gpu trait on the REGISTER payload alone
// (enrollment.go bootstrap), so a reconciliation that took the served list
// wholesale would silently un-advertise a GPU on the first heartbeat.
var fleetNodeCapabilityTraits = map[string]struct{}{FleetNodeTraitGPU: {}, FleetNodeTraitSigningRelay: {}}

// FleetNodeObservation is what a daemon reports about ITSELF on a heartbeat.
//
// It is distinct from the registration payload: a registration is a claim made
// once at enrollment, while an observation is the running truth, and the two
// diverge whenever a daemon is restarted with a different backend set or
// upgraded in place from an older vocabulary. Reconciling the record against
// it is what keeps dispatch routing on names the daemon actually serves
// (go-quicbaas#699).
//
// A nil *FleetNodeObservation means the caller reported nothing -- the gRPC and
// MCP heartbeat verbs carry no served list -- and reconciles nothing.
type FleetNodeObservation struct {
	// Backends is the set of backend names the daemon's runtime is serving
	// right now (pkg/orchestration/fleet Runtime.Backends()). It carries no host traits.
	//
	// EMPTY IS A FAULT, NOT A CORRECTION. A daemon that constructed no
	// backends serves nothing and can honour no task; reconciling a record
	// down to nothing would quietly retire a fleet member instead of
	// surfacing the fault, so an observation that reports none is refused.
	Backends []string

	// MaxConcurrentJobs is how many orchestration jobs the daemon can run at
	// once, reported on every heartbeat so the fleet's capacity tracks a host
	// being drained or added without anything to keep in step by hand. See
	// FleetNode.MaxConcurrentJobs.
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

	// SigningAgents is the identifier of every daemon sockets root the fleet node
	// can mount from (DiscoverSigningRootIDs), re-read on every heartbeat so the
	// trait follows a daemon that re-created its root. It replaces the record's
	// signing-agent traits.
	//
	// NIL REPORTS NOTHING AND RECONCILES NOTHING — an older build, or a
	// transport that carries no such list. An EMPTY list is a report: the fleet node
	// can reach no signing socket, and its traits are cleared.
	SigningAgents []string

	// SigningRelay reports whether the fleet node can serve a session's signing
	// socket from a relay over its FleetRuntime link (FleetNodeTraitSigningRelay,
	// go-quicbaas#1013), re-read on every heartbeat so the trait follows an
	// fleet node that lost or gained its relay sockets root.
	//
	// NIL REPORTS NOTHING AND RECONCILES NOTHING — an older build, or a
	// transport that carries no such field.
	SigningRelay *bool

	// Images is the list of container images present on the host, or nil
	// when the observation carries no list (an older build, or a failed
	// listing that must not read as "no images").
	Images *[]string

	// Devices is the node's accelerator telemetry on this heartbeat — power,
	// utilisation and memory per GPU, NPU and CPU package — filed as time
	// series and used to measure local turns' energy (fleet_telemetry.go).
	// It reconciles nothing on the registration. NIL REPORTS NOTHING; an
	// empty list is a node with no device it can read.
	Devices []DeviceSample

	// HostMetrics is the node's host-wide sample on this heartbeat (CPU,
	// load, memory, disk), which replaces the one on the record.
	//
	// NIL REPORTS NOTHING AND RECONCILES NOTHING — an older build, or a
	// daemon that could not sample this time. The record keeps its previous
	// sample, which the health verdict ages from its collection time, so a
	// node that stops sampling turns stale at the stale bound rather than
	// staying healthy on old news.
	HostMetrics *HostMetrics
}

// Validate rejects an observation that reports no served backend, or one whose
// list carries a blank name. Exported because the refusal is part of the
// type's contract, not a private invariant of the registry: a transport that
// wants to answer a daemon serving nothing before it reaches the store asks
// the same question here.
func (o *FleetNodeObservation) Validate() error {
	if o == nil {
		return nil
	}
	if len(o.Backends) == 0 {
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryInvalidInput,
			Message:  "heartbeat reported no served backend: a daemon serving nothing cannot be reconciled, it is a fault to fix",
		}
	}
	for _, backend := range o.Backends {
		if strings.TrimSpace(backend) == "" {
			return &FleetRegistryError{
				Sentinel: ErrFleetRegistryInvalidInput,
				Message:  "heartbeat reported an empty backend name",
			}
		}
	}
	if o.MaxConcurrentJobs < 0 {
		return &FleetRegistryError{
			Sentinel: ErrFleetRegistryInvalidInput,
			Message: "heartbeat reported a negative concurrent-job capacity (" +
				strconv.Itoa(o.MaxConcurrentJobs) + "): use zero to report none",
		}
	}
	for _, id := range o.SigningAgents {
		if !validSigningRootID(id) {
			return &FleetRegistryError{
				Sentinel: ErrFleetRegistryInvalidInput,
				Message:  "heartbeat reported a malformed signing-root identifier " + strconv.Quote(id),
			}
		}
	}
	return nil
}

// =============================================================================
// Fleet Node Disable / Enable Typed Errors
// =============================================================================

// ErrFleetNodeDisabled is the sentinel error when a disabled fleet node attempts an action.
var ErrFleetNodeDisabled = errors.New("agent: agent is disabled")

// ErrFleetNodeAlreadyDisabled is the sentinel error for disabling an already-disabled fleet node.
var ErrFleetNodeAlreadyDisabled = errors.New("agent: agent is already disabled")

// ErrFleetNodeNotDisabled is the sentinel error for enabling a fleet node that is not disabled.
var ErrFleetNodeNotDisabled = errors.New("agent: agent is not disabled")

// FleetNodeDisabledError is returned when a disabled fleet node attempts to perform an operation.
type FleetNodeDisabledError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetNodeDisabledError) Error() string {
	return ErrFleetNodeDisabled.Error() + ": tenant=" + e.TenantID + " agent=" + e.NodeID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FleetNodeDisabledError) Unwrap() error { return ErrFleetNodeDisabled }

// FleetNodeAlreadyDisabledError is returned when an operator tries to disable an already-disabled fleet node.
type FleetNodeAlreadyDisabledError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetNodeAlreadyDisabledError) Error() string {
	return ErrFleetNodeAlreadyDisabled.Error() + ": tenant=" + e.TenantID + " agent=" + e.NodeID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FleetNodeAlreadyDisabledError) Unwrap() error { return ErrFleetNodeAlreadyDisabled }

// FleetNodeNotDisabledError is returned when an operator tries to enable a fleet node that is not disabled.
type FleetNodeNotDisabledError struct {
	TenantID string
	NodeID   string
}

// Error implements the error interface.
func (e *FleetNodeNotDisabledError) Error() string {
	return ErrFleetNodeNotDisabled.Error() + ": tenant=" + e.TenantID + " agent=" + e.NodeID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FleetNodeNotDisabledError) Unwrap() error { return ErrFleetNodeNotDisabled }

// FleetRegistryError wraps a sentinel error with an additional message
// for fleet registry operations.
type FleetRegistryError struct {
	Sentinel error
	Message  string
}

// Error implements the error interface.
func (e *FleetRegistryError) Error() string {
	if e.Message != "" {
		return e.Sentinel.Error() + ": " + e.Message
	}
	return e.Sentinel.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FleetRegistryError) Unwrap() error {
	return e.Sentinel
}
