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

// Package agents is the SDK client for a tenant's AI agent catalog: the named
// Agents (runtime + model + provider) every role slot references, and the
// ordered pools a slot fails over along. It mirrors orchestration.Agent and
// orchestration.AgentPool so SDK callers need not import the server-side
// orchestration package.
//
// An Agent is not a fleet node. A fleet node is a daemon that serves backends;
// an Agent names which backend, model and image a slot runs.
package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// ServiceName is the transport service the Agents verbs route under.
const ServiceName = "quicbaas.Agents"

// =============================================================================
// Types
// =============================================================================

// AgentRef is a role slot's reference: exactly one Agent or one pool. The zero
// value references nothing.
type AgentRef struct {
	AgentID string `json:"agent_id,omitempty" yaml:"agent_id,omitempty"`
	PoolID  string `json:"pool_id,omitempty" yaml:"pool_id,omitempty"`
}

// IsZero reports whether the reference names nothing.
func (r AgentRef) IsZero() bool {
	return strings.TrimSpace(r.AgentID) == "" && strings.TrimSpace(r.PoolID) == ""
}

// String renders the reference for a person: "agent:<id>" or "pool:<id>".
func (r AgentRef) String() string {
	switch {
	case strings.TrimSpace(r.PoolID) != "":
		return "pool:" + r.PoolID
	case strings.TrimSpace(r.AgentID) != "":
		return "agent:" + r.AgentID
	default:
		return ""
	}
}

// ParseAgentRef reads "agent:<id>" or "pool:<id>"; a bare ID is an Agent.
// Empty is the zero reference.
func ParseAgentRef(text string) (AgentRef, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return AgentRef{}, nil
	}
	kind, id, qualified := strings.Cut(text, ":")
	if !qualified {
		return AgentRef{AgentID: text}, nil
	}
	switch strings.TrimSpace(kind) {
	case "agent":
		return AgentRef{AgentID: strings.TrimSpace(id)}, nil
	case "pool":
		return AgentRef{PoolID: strings.TrimSpace(id)}, nil
	default:
		return AgentRef{}, errors.NewInvalidInput("agents.ParseAgentRef", "a reference is agent:<id> or pool:<id>, got "+text)
	}
}

// ProviderEndpoint locates where an Agent's model is served; the zero value is
// the runtime's own default.
type ProviderEndpoint struct {
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	BaseURL      string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	WireAPI      string `json:"wire_api,omitempty" yaml:"wire_api,omitempty"`
	RequiresAuth bool   `json:"requires_auth,omitempty" yaml:"requires_auth,omitempty"`
}

// AccountPolicy narrows the pooled provider accounts an Agent draws on.
type AccountPolicy struct {
	Strategy string   `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	Accounts []string `json:"accounts,omitempty" yaml:"accounts,omitempty"`
}

// Prompt is a built-in charter by reference or a custom system prompt.
type Prompt struct {
	CharterSet string `json:"charter_set,omitempty" yaml:"charter_set,omitempty"`
	Charter    string `json:"charter,omitempty" yaml:"charter,omitempty"`
	System     string `json:"system,omitempty" yaml:"system,omitempty"`
}

// RolePolicy says which role an Agent in a shared pool is held for.
type RolePolicy struct {
	Prefer    string `json:"prefer,omitempty" yaml:"prefer,omitempty"`
	Reserve   int    `json:"reserve,omitempty" yaml:"reserve,omitempty"`
	Exclusive bool   `json:"exclusive,omitempty" yaml:"exclusive,omitempty"`
}

// Agent is one tenant AI agent definition. Roles are "coding", "review",
// "triage", "lead", "project_manager" and "general".
type Agent struct {
	ID              string           `json:"id" yaml:"id"`
	Name            string           `json:"name" yaml:"name"`
	Description     string           `json:"description,omitempty" yaml:"description,omitempty"`
	Roles           []string         `json:"roles" yaml:"roles"`
	Backend         string           `json:"backend" yaml:"backend"`
	Model           string           `json:"model" yaml:"model"`
	Provider        ProviderEndpoint `json:"provider,omitempty" yaml:"provider,omitempty"`
	Image           string           `json:"image,omitempty" yaml:"image,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty" yaml:"reasoning_effort,omitempty"`
	Account         AccountPolicy    `json:"account,omitempty" yaml:"account,omitempty"`
	Prompt          Prompt           `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	MCPScopes       []string         `json:"mcp_scopes,omitempty" yaml:"mcp_scopes,omitempty"`
	Capabilities    []string         `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	ContextTokens   int              `json:"context_tokens,omitempty" yaml:"context_tokens,omitempty"`
	MaxConcurrent   int              `json:"max_concurrent,omitempty" yaml:"max_concurrent,omitempty"`
	Placement       RolePolicy       `json:"placement,omitempty" yaml:"placement,omitempty"`
	// Spend is the Agent's own spend limits, checked when a session of it
	// is spawned. The zero value meters nothing.
	Spend     spend.Limits `json:"spend,omitzero" yaml:"spend,omitempty"`
	CreatedAt time.Time    `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt time.Time    `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// Pool is an ordered failover list of Agent IDs, most preferred first.
type Pool struct {
	ID          string    `json:"id" yaml:"id"`
	Name        string    `json:"name" yaml:"name"`
	Description string    `json:"description,omitempty" yaml:"description,omitempty"`
	Agents      []string  `json:"agents" yaml:"agents"`
	CreatedAt   time.Time `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// Usage is one thing that references an Agent or pool: its kind ("pool",
// "binding", "project", "config"), its ID, and the slot the reference sits in.
type Usage struct {
	Kind string `json:"kind" yaml:"kind"`
	ID   string `json:"id" yaml:"id"`
	Slot string `json:"slot" yaml:"slot"`
}

// Attempt is one Agent a pooled spawn passed over, and why.
type Attempt struct {
	AgentID string `json:"agent_id" yaml:"agent_id"`
	Reason  string `json:"reason" yaml:"reason"`
	Detail  string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// =============================================================================
// Transport requests and responses
// =============================================================================

// ListAgentsRequest lists a tenant's Agents.
type ListAgentsRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
}

// AgentRequest names one Agent.
type AgentRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	AgentID  string `json:"agent_id"`
}

// SaveAgentRequest creates (AgentID empty) or replaces (AgentID set) an Agent.
type SaveAgentRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	AgentID  string `json:"agent_id,omitempty"`
	Agent    *Agent `json:"agent"`
}

// ListPoolsRequest lists a tenant's pools.
type ListPoolsRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
}

// PoolRequest names one pool.
type PoolRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	PoolID   string `json:"pool_id"`
}

// SavePoolRequest creates (PoolID empty) or replaces (PoolID set) a pool.
type SavePoolRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	PoolID   string `json:"pool_id,omitempty"`
	Pool     *Pool  `json:"pool"`
}

// AgentList is the answer to ListAgents.
type AgentList struct {
	Agents []*Agent `json:"agents"`
}

// PoolList is the answer to ListPools.
type PoolList struct {
	Pools []*Pool `json:"pools"`
}

// UsageList is the answer to a used-by query.
type UsageList struct {
	UsedBy []Usage `json:"used_by"`
}

// Deleted is the answer to a delete.
type Deleted struct {
	Deleted bool `json:"deleted"`
}

// =============================================================================
// Client
// =============================================================================

// Client is the tenant AI agent catalog.
type Client interface {
	// ListAgents lists the tenant's Agents, ID-ordered.
	ListAgents(ctx context.Context, tenantID string) ([]*Agent, error)
	// GetAgent reads one Agent.
	GetAgent(ctx context.Context, tenantID, agentID string) (*Agent, error)
	// CreateAgent stores a new Agent, checked against the live backend and
	// model catalog.
	CreateAgent(ctx context.Context, tenantID string, agent *Agent) (*Agent, error)
	// UpdateAgent replaces an Agent; a role a slot still needs cannot be
	// dropped.
	UpdateAgent(ctx context.Context, tenantID, agentID string, agent *Agent) (*Agent, error)
	// DeleteAgent removes an Agent nothing references; the refusal names
	// every user.
	DeleteAgent(ctx context.Context, tenantID, agentID string) error
	// AgentUsedBy lists everything that references an Agent.
	AgentUsedBy(ctx context.Context, tenantID, agentID string) ([]Usage, error)

	// ListPools lists the tenant's pools, ID-ordered.
	ListPools(ctx context.Context, tenantID string) ([]*Pool, error)
	// GetPool reads one pool.
	GetPool(ctx context.Context, tenantID, poolID string) (*Pool, error)
	// CreatePool stores a new pool; every Agent it names must exist.
	CreatePool(ctx context.Context, tenantID string, pool *Pool) (*Pool, error)
	// UpdatePool replaces a pool; order is the failover chain.
	UpdatePool(ctx context.Context, tenantID, poolID string, pool *Pool) (*Pool, error)
	// DeletePool removes a pool nothing references.
	DeletePool(ctx context.Context, tenantID, poolID string) error
	// PoolUsedBy lists everything that references a pool.
	PoolUsedBy(ctx context.Context, tenantID, poolID string) ([]Usage, error)
}

// CapableTransport is declared by a transport that carries the Agents
// service.
type CapableTransport interface {
	SupportsAgents() bool
}

type client struct {
	transport transport.Transport
}

// NewClient creates an Agents client over the transport.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{transport: t}
}

// call dispatches one verb, refusing a transport that does not declare the
// service and a response of the wrong type.
func call[T any](ctx context.Context, c *client, method string, req interface{}) (T, error) {
	var zero T
	op := "agents." + method
	if capable, ok := c.transport.(CapableTransport); !ok || !capable.SupportsAgents() {
		return zero, errors.NewAgentsTransportUnsupported(op, fmt.Sprintf("%T", c.transport))
	}
	if ctx == nil {
		return zero, errors.NewInvalidInput(op, "context is required")
	}
	resp, err := c.transport.Request(ctx, ServiceName, method, req)
	if err != nil {
		return zero, err
	}
	out, ok := resp.(T)
	if !ok {
		return zero, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}

// require refuses an empty identifier.
func require(method, field, value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.NewInvalidInput("agents."+method, field+" is required")
	}
	return nil
}

// ListAgents implements Client.
func (c *client) ListAgents(ctx context.Context, tenantID string) ([]*Agent, error) {
	list, err := call[*AgentList](ctx, c, "ListAgents", &ListAgentsRequest{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	return list.Agents, nil
}

// GetAgent implements Client.
func (c *client) GetAgent(ctx context.Context, tenantID, agentID string) (*Agent, error) {
	if err := require("GetAgent", "agent_id", agentID); err != nil {
		return nil, err
	}
	return call[*Agent](ctx, c, "GetAgent", &AgentRequest{TenantID: tenantID, AgentID: agentID})
}

// CreateAgent implements Client.
func (c *client) CreateAgent(ctx context.Context, tenantID string, agent *Agent) (*Agent, error) {
	if agent == nil {
		return nil, errors.NewInvalidInput("agents.CreateAgent", "agent is required")
	}
	return call[*Agent](ctx, c, "CreateAgent", &SaveAgentRequest{TenantID: tenantID, Agent: agent})
}

// UpdateAgent implements Client.
func (c *client) UpdateAgent(ctx context.Context, tenantID, agentID string, agent *Agent) (*Agent, error) {
	if err := require("UpdateAgent", "agent_id", agentID); err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, errors.NewInvalidInput("agents.UpdateAgent", "agent is required")
	}
	return call[*Agent](ctx, c, "UpdateAgent", &SaveAgentRequest{TenantID: tenantID, AgentID: agentID, Agent: agent})
}

// DeleteAgent implements Client.
func (c *client) DeleteAgent(ctx context.Context, tenantID, agentID string) error {
	if err := require("DeleteAgent", "agent_id", agentID); err != nil {
		return err
	}
	_, err := call[*Deleted](ctx, c, "DeleteAgent", &AgentRequest{TenantID: tenantID, AgentID: agentID})
	return err
}

// AgentUsedBy implements Client.
func (c *client) AgentUsedBy(ctx context.Context, tenantID, agentID string) ([]Usage, error) {
	if err := require("AgentUsedBy", "agent_id", agentID); err != nil {
		return nil, err
	}
	list, err := call[*UsageList](ctx, c, "AgentUsedBy", &AgentRequest{TenantID: tenantID, AgentID: agentID})
	if err != nil {
		return nil, err
	}
	return list.UsedBy, nil
}

// ListPools implements Client.
func (c *client) ListPools(ctx context.Context, tenantID string) ([]*Pool, error) {
	list, err := call[*PoolList](ctx, c, "ListPools", &ListPoolsRequest{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	return list.Pools, nil
}

// GetPool implements Client.
func (c *client) GetPool(ctx context.Context, tenantID, poolID string) (*Pool, error) {
	if err := require("GetPool", "pool_id", poolID); err != nil {
		return nil, err
	}
	return call[*Pool](ctx, c, "GetPool", &PoolRequest{TenantID: tenantID, PoolID: poolID})
}

// CreatePool implements Client.
func (c *client) CreatePool(ctx context.Context, tenantID string, pool *Pool) (*Pool, error) {
	if pool == nil {
		return nil, errors.NewInvalidInput("agents.CreatePool", "pool is required")
	}
	return call[*Pool](ctx, c, "CreatePool", &SavePoolRequest{TenantID: tenantID, Pool: pool})
}

// UpdatePool implements Client.
func (c *client) UpdatePool(ctx context.Context, tenantID, poolID string, pool *Pool) (*Pool, error) {
	if err := require("UpdatePool", "pool_id", poolID); err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, errors.NewInvalidInput("agents.UpdatePool", "pool is required")
	}
	return call[*Pool](ctx, c, "UpdatePool", &SavePoolRequest{TenantID: tenantID, PoolID: poolID, Pool: pool})
}

// DeletePool implements Client.
func (c *client) DeletePool(ctx context.Context, tenantID, poolID string) error {
	if err := require("DeletePool", "pool_id", poolID); err != nil {
		return err
	}
	_, err := call[*Deleted](ctx, c, "DeletePool", &PoolRequest{TenantID: tenantID, PoolID: poolID})
	return err
}

// PoolUsedBy implements Client.
func (c *client) PoolUsedBy(ctx context.Context, tenantID, poolID string) ([]Usage, error) {
	if err := require("PoolUsedBy", "pool_id", poolID); err != nil {
		return nil, err
	}
	list, err := call[*UsageList](ctx, c, "PoolUsedBy", &PoolRequest{TenantID: tenantID, PoolID: poolID})
	if err != nil {
		return nil, err
	}
	return list.UsedBy, nil
}
