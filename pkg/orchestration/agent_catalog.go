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

package orchestration

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// Agents — the tenant's named, reusable AI agent definitions
// =============================================================================
//
// AN AGENT IS ONE RUNNABLE COMBINATION OF RUNTIME, MODEL AND PROVIDER, and it
// is the only way a role slot names who does the work. Before Agents existed
// every slot carried its own free-string tuple — a binding's worker, its
// reviewer, its triage, a job's lead, a project's default lead — each one a
// backend name, a model name and an image typed by hand, validated by nothing
// until a spawn refused it. One Agent now carries the tuple once; a slot holds
// an AgentRef, and the reference is checked when it is saved.
//
// The three axes stay independent:
//
//	backend   claude | codex | opencode | openvino   — runs the agent loop
//	model     qwen3-8b | gpt-5.5 | claude-opus-5-5   — what it thinks with
//	provider  OVMS@gpu-host | ollama@gpu-host | ""   — where that model is served

// AgentRole names the kind of work an Agent is FOR. A role slot only accepts an
// Agent that declares the slot's role, so "the triage model" cannot be bound
// as a project's coder by accident.
type AgentRole string

// The roles an Agent may declare.
const (
	// AgentRoleCoding writes and delivers changes.
	AgentRoleCoding AgentRole = "coding"

	// AgentRoleReview judges a pull request.
	AgentRoleReview AgentRole = "review"

	// AgentRoleTriage judges open issues and writes no code.
	AgentRoleTriage AgentRole = "triage"

	// AgentRoleLead decomposes and leads a job.
	AgentRoleLead AgentRole = "lead"

	// AgentRoleProjectManager plans and tracks work across a project.
	AgentRoleProjectManager AgentRole = "project_manager"

	// AgentRoleGeneral is an interactive, general-purpose session.
	AgentRoleGeneral AgentRole = "general"
)

// agentRoles is the closed vocabulary of AgentRole.
var agentRoles = map[AgentRole]bool{
	AgentRoleCoding:         true,
	AgentRoleReview:         true,
	AgentRoleTriage:         true,
	AgentRoleLead:           true,
	AgentRoleProjectManager: true,
	AgentRoleGeneral:        true,
}

// AgentRoles returns every role an Agent may declare, in a stable order.
func AgentRoles() []AgentRole {
	return []AgentRole{
		AgentRoleCoding, AgentRoleReview, AgentRoleTriage,
		AgentRoleLead, AgentRoleProjectManager, AgentRoleGeneral,
	}
}

// Valid reports whether r is a known role.
func (r AgentRole) Valid() bool { return agentRoles[r] }

// String returns the role's name.
func (r AgentRole) String() string { return string(r) }

// agentReasoningEfforts is the vocabulary a runtime's reasoning effort takes.
// A value outside it is refused when the Agent is saved rather than by every
// spawn. For codex it is model_reasoning_effort.
var agentReasoningEfforts = map[string]bool{
	"minimal": true, "low": true, "medium": true, "high": true, "xhigh": true,
}

// ProviderEndpoint locates where an Agent's model is served.
//
// THE ZERO VALUE MEANS "THE RUNTIME'S OWN DEFAULT", which is what a hosted CLI
// wants: codex against OpenAI and claude against Anthropic name no endpoint.
type ProviderEndpoint struct {
	// Name is how the runtime refers to this provider in its own config. It
	// is an identifier, not a hostname.
	Name string `json:"name,omitempty"`

	// BaseURL is the server root INCLUDING the version segment the runtime
	// expects (e.g. "http://gpu-host:9000/v1").
	BaseURL string `json:"base_url,omitempty"`

	// WireAPI names the dialect the endpoint speaks: "responses" or "chat".
	// It cannot be probed, so an endpoint must state it.
	WireAPI string `json:"wire_api,omitempty"`

	// RequiresAuth says the endpoint expects the runtime's vendor credential.
	RequiresAuth bool `json:"requires_auth,omitempty"`
}

// Configured reports whether this endpoint names a server.
func (p ProviderEndpoint) Configured() bool { return strings.TrimSpace(p.BaseURL) != "" }

// AgentAccountPolicy says which pooled provider accounts an Agent's sessions
// draw on. The provider is the backend's own (see WorkerSpawnAccountAuto); the
// policy narrows how one is chosen.
type AgentAccountPolicy struct {
	// Strategy names the registered account selection strategy ("static",
	// "least_usage", ...). Empty takes the registry default.
	Strategy string `json:"strategy,omitempty"`

	// Accounts, when set, are the only accounts this Agent may use and rotate
	// through. Empty leaves the tenant's whole pool for the provider eligible.
	Accounts []string `json:"accounts,omitempty"`
}

// AgentPrompt is the standing instruction an Agent's sessions start with: a
// built-in charter by reference, or a custom system prompt — never both.
//
// A ROLE'S OWN CHARTER WINS. The SDLC runners spawn with the charter their
// role requires (the worker's, the reviewer's, triage's), because what a
// reviewer must do is not an Agent's to redefine. An Agent's prompt is what an
// interactive or lead session starts with when the spawn names none.
//
// THE PROMPT IS OPTIONAL. An Agent with none starts, when a spawn names no
// prompt either, with the built-in charter of the role it is spawned for where
// that role has one, and with no system prompt otherwise (role_charter.go).
type AgentPrompt struct {
	// CharterSet names the registered prompt set the charter lives in.
	CharterSet string `json:"charter_set,omitempty"`

	// Charter names a prompt of CharterSet.
	Charter string `json:"charter,omitempty"`

	// System is a custom system prompt.
	System string `json:"system,omitempty"`
}

// IsZero reports whether the prompt names nothing.
func (p AgentPrompt) IsZero() bool {
	return strings.TrimSpace(p.CharterSet) == "" && strings.TrimSpace(p.Charter) == "" &&
		strings.TrimSpace(p.System) == ""
}

// Validate refuses a prompt that names both a charter and a custom text, or a
// charter no registered set carries.
func (p AgentPrompt) Validate() error {
	charter := strings.TrimSpace(p.Charter) != "" || strings.TrimSpace(p.CharterSet) != ""
	if charter && strings.TrimSpace(p.System) != "" {
		return &InputValidationError{Field: "agent.prompt", Message: "name a built-in charter or a custom system prompt, not both"}
	}
	if !charter {
		return nil
	}
	set, ok := prompts.LookupSet(prompts.SetKind(strings.TrimSpace(p.CharterSet)))
	if !ok {
		return &InputValidationError{Field: "agent.prompt.charter_set", Message: "no prompt set " + strconv.Quote(p.CharterSet) + " is registered"}
	}
	if _, ok := set.Spec().Prompt(strings.TrimSpace(p.Charter)); !ok {
		return &InputValidationError{
			Field:   "agent.prompt.charter",
			Message: "prompt set " + strconv.Quote(p.CharterSet) + " has no charter " + strconv.Quote(p.Charter),
		}
	}
	return nil
}

// Render returns the system prompt text the reference resolves to; empty when
// the prompt names nothing.
func (p AgentPrompt) Render() (string, error) {
	if strings.TrimSpace(p.System) != "" {
		return p.System, nil
	}
	if strings.TrimSpace(p.Charter) == "" {
		return "", nil
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	set, _ := prompts.LookupSet(prompts.SetKind(strings.TrimSpace(p.CharterSet)))
	return set.Render(strings.TrimSpace(p.Charter), nil)
}

// Agent is one tenant-scoped, named AI agent definition.
type Agent struct {
	// ID is the Agent's stable identifier within its tenant: lowercase
	// letters, digits, '.', '_' and '-'. Every reference names it.
	ID string `json:"id"`

	// Name is how an operator reads the Agent.
	Name string `json:"name"`

	// Description says what the Agent is for.
	Description string `json:"description,omitempty"`

	// Roles are the kinds of work this Agent may be bound to. At least one.
	Roles []AgentRole `json:"roles"`

	// Backend is the registered runtime that runs the agent loop.
	Backend string `json:"backend"`

	// Model is what the backend thinks with. REQUIRED: a resolved Agent
	// always names its model, so every spawn, every ledger entry and every
	// model-family decision reads a model rather than a runtime's silent
	// default.
	Model string `json:"model"`

	// Provider locates the model. The zero value is the runtime's default.
	Provider ProviderEndpoint `json:"provider,omitempty"`

	// Image is the container image a session of this Agent runs in. Naming
	// one requires the sandbox; empty leaves the decision to the operator's
	// sandbox policy.
	Image string `json:"image,omitempty"`

	// ReasoningEffort is how hard the model thinks per turn, in the
	// runtime's vocabulary: minimal, low, medium, high or xhigh. Empty takes
	// the runtime's default.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// Account narrows the provider accounts this Agent's sessions draw on.
	Account AgentAccountPolicy `json:"account,omitempty"`

	// Spend is the Agent's own spend limits: sliding windows, in tokens
	// and/or list-price dollars, over every turn a session of this Agent
	// runs, read from the ledger's Agent quota plane. The zero value meters
	// nothing. They are checked when a session of the Agent is spawned: an
	// Agent over a window passes a pool's work to its next member, and a
	// spawn naming it alone is refused (agent_spend.go). The project's and
	// tenant's limits apply on top, unchanged.
	Spend SpendLimits `json:"spend,omitzero"`

	// Prompt is the standing instruction this Agent's sessions start with.
	Prompt AgentPrompt `json:"prompt,omitempty"`

	// MCPScopes are the orchestrator MCP scopes this Agent's sessions hold.
	MCPScopes []string `json:"mcp_scopes,omitempty"`

	// Capabilities are what this Agent can do (e.g. "tool_use",
	// "persistent_sessions"). They are declared, not probed.
	Capabilities []string `json:"capabilities,omitempty"`

	// ContextTokens is what the model accepts, as the operator knows it.
	// Zero declares nothing and the backend learns it from the first refusal.
	ContextTokens int `json:"context_tokens,omitempty"`

	// MaxConcurrent bounds how many jobs may run on this Agent's backend at
	// once when it is chosen by reference. Zero is unbounded.
	MaxConcurrent int `json:"max_concurrent,omitempty"`

	// Placement says which role this Agent is held for when it sits in a
	// pool shared between roles. The zero value prefers nobody.
	Placement RolePolicy `json:"placement,omitempty"`

	// CreatedAt and UpdatedAt are stamped by the store.
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// maxCatalogID bounds an Agent or pool identifier.
const maxCatalogID = 64

// catalogIDMessage is the refusal every malformed identifier gets.
const catalogIDMessage = "must be 1-64 lowercase letters, digits, '.', '_' or '-', starting with a letter or digit"

// validCatalogID reports whether id is usable as an Agent or pool identifier.
func validCatalogID(id string) bool {
	if id == "" || len(id) > maxCatalogID {
		return false
	}
	for i, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '.' || c == '_' || c == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}

// ValidCatalogID reports whether id is usable as an Agent or pool identifier.
func ValidCatalogID(id string) bool { return validCatalogID(id) }

// HasRole reports whether the Agent declares role.
func (a Agent) HasRole(role AgentRole) bool {
	for _, declared := range a.Roles {
		if declared == role {
			return true
		}
	}
	return false
}

// Validate refuses an Agent that could not be spawned or referenced. It checks
// the record's own shape; whether the backend and model exist is a question of
// the live catalog, asked by AgentDirectory when an operator saves the Agent.
func (a Agent) Validate() error {
	checks := []func() error{
		a.validateIdentity,
		a.validateRoles,
		a.validateRuntime,
		a.validateLimits,
		a.Placement.Validate,
		a.Prompt.Validate,
		a.validateSpend,
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

// validateSpend refuses malformed spend limits as bad input on the Agent, so
// every transport answers the catalog's own way (400 / InvalidArgument).
func (a Agent) validateSpend() error {
	if err := a.Spend.Validate(); err != nil {
		return &InputValidationError{Field: "agent.spend", Message: err.Error()}
	}
	return nil
}

func (a Agent) validateIdentity() error {
	if !validCatalogID(a.ID) {
		return &InputValidationError{Field: "agent.id", Message: catalogIDMessage}
	}
	if strings.TrimSpace(a.Name) == "" {
		return &InputValidationError{Field: "agent.name", Message: "an agent is read by its name and must have one"}
	}
	return nil
}

func (a Agent) validateRoles() error {
	if len(a.Roles) == 0 {
		return &InputValidationError{Field: "agent.roles", Message: "an agent declares at least one role it may be bound to"}
	}
	seen := make(map[AgentRole]bool, len(a.Roles))
	for _, role := range a.Roles {
		if !role.Valid() {
			return &InputValidationError{Field: "agent.roles", Message: "unknown role " + strconv.Quote(string(role))}
		}
		if seen[role] {
			return &InputValidationError{Field: "agent.roles", Message: "role " + strconv.Quote(string(role)) + " is listed twice"}
		}
		seen[role] = true
	}
	return nil
}

func (a Agent) validateRuntime() error {
	if strings.TrimSpace(a.Backend) == "" {
		return &InputValidationError{Field: "agent.backend", Message: "an agent names the backend that runs it"}
	}
	if strings.TrimSpace(a.Model) == "" {
		return &InputValidationError{Field: "agent.model", Message: "an agent names its model; a runtime's silent default is not a model"}
	}
	if effort := strings.TrimSpace(a.ReasoningEffort); effort != "" && !agentReasoningEfforts[effort] {
		return &InputValidationError{Field: "agent.reasoning_effort", Message: "must be one of minimal, low, medium, high or xhigh"}
	}
	// An endpoint that names a server must say which dialect it speaks: the
	// runtime cannot probe for it.
	if a.Provider.Configured() && strings.TrimSpace(a.Provider.WireAPI) == "" {
		return &InputValidationError{
			Field:   "agent.provider.wire_api",
			Message: "an endpoint must state its dialect (\"responses\" or \"chat\"); it cannot be probed",
		}
	}
	return nil
}

func (a Agent) validateLimits() error {
	if a.MaxConcurrent < 0 {
		return &InputValidationError{Field: "agent.max_concurrent", Message: "use zero for unbounded, not a negative"}
	}
	if a.ContextTokens < 0 {
		return &InputValidationError{Field: "agent.context_tokens", Message: "use zero for undeclared, not a negative"}
	}
	return nil
}

// ExtraArgs is what a spawn of this Agent adds to the backend's command line:
// the provider endpoint, then the reasoning effort. Only runtimes whose
// dialect is known get anything; argv a CLI does not understand is a startup
// failure rather than a warning.
func (a Agent) ExtraArgs() []string {
	args := a.providerArgs()
	if effort := strings.TrimSpace(a.ReasoningEffort); effort != "" && a.runtime() == "codex" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(effort))
	}
	return args
}

func (a Agent) runtime() string { return strings.ToLower(strings.TrimSpace(a.Backend)) }

// providerArgs renders the provider configuration a runtime needs on argv.
// codex takes `-c dotted.key=TOML` and has no --provider flag.
func (a Agent) providerArgs() []string {
	if !a.Provider.Configured() || a.runtime() != "codex" {
		return nil
	}
	name := strings.TrimSpace(a.Provider.Name)
	if name == "" {
		name = "quicbaas_provider"
	}
	key := "model_providers." + name
	return []string{
		"-c", "model_provider=" + strconv.Quote(name),
		"-c", key + ".name=" + strconv.Quote(name),
		"-c", key + ".base_url=" + strconv.Quote(a.Provider.BaseURL),
		"-c", key + ".wire_api=" + strconv.Quote(strings.ToLower(a.Provider.WireAPI)),
		"-c", key + ".requires_openai_auth=" + strconv.FormatBool(a.Provider.RequiresAuth),
	}
}

// AgentPool is an ORDERED list of Agents: the failover chain for a role slot.
//
// ORDER IS THE POLICY. The first Agent that can take the work gets it; the
// next is tried when the one before it is at capacity, its accounts are
// exhausted, expired or rate-limited, or its backend is unavailable. Put the
// local model first and the metered one second, and the fleet drains to the
// cheap resource until it cannot, then spills.
type AgentPool struct {
	// ID is the pool's stable identifier within its tenant.
	ID string `json:"id"`

	// Name is how an operator reads the pool.
	Name string `json:"name"`

	// Description says what the pool is for.
	Description string `json:"description,omitempty"`

	// Agents are Agent IDs, most preferred first.
	Agents []string `json:"agents"`

	// CreatedAt and UpdatedAt are stamped by the store.
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Validate refuses a pool that selects nothing or names an Agent twice.
func (p AgentPool) Validate() error {
	if !validCatalogID(p.ID) {
		return &InputValidationError{Field: "pool.id", Message: catalogIDMessage}
	}
	if strings.TrimSpace(p.Name) == "" {
		return &InputValidationError{Field: "pool.name", Message: "a pool is read by its name and must have one"}
	}
	if len(p.Agents) == 0 {
		return &InputValidationError{Field: "pool.agents", Message: "a pool with no agents selects nothing"}
	}
	seen := make(map[string]bool, len(p.Agents))
	for i, id := range p.Agents {
		field := "pool.agents[" + strconv.Itoa(i) + "]"
		if !validCatalogID(id) {
			return &InputValidationError{Field: field, Message: catalogIDMessage}
		}
		if seen[id] {
			return &InputValidationError{Field: field, Message: "agent " + strconv.Quote(id) + " is listed twice; order is the failover chain"}
		}
		seen[id] = true
	}
	return nil
}

// AgentRef is a role slot's reference to who does the work: exactly one Agent
// or one pool. The zero value references nothing, which an optional slot reads
// as "not configured".
type AgentRef struct {
	// AgentID names one Agent.
	AgentID string `json:"agent_id,omitempty" yaml:"agent_id,omitempty"`

	// PoolID names one pool.
	PoolID string `json:"pool_id,omitempty" yaml:"pool_id,omitempty"`
}

// AgentRefTo returns a reference to one Agent.
func AgentRefTo(agentID string) AgentRef { return AgentRef{AgentID: agentID} }

// PoolRefTo returns a reference to one pool.
func PoolRefTo(poolID string) AgentRef { return AgentRef{PoolID: poolID} }

// IsZero reports whether the reference names nothing.
func (r AgentRef) IsZero() bool {
	return strings.TrimSpace(r.AgentID) == "" && strings.TrimSpace(r.PoolID) == ""
}

// IsPool reports whether the reference names a pool.
func (r AgentRef) IsPool() bool { return strings.TrimSpace(r.PoolID) != "" }

// Names reports whether the reference is to exactly the given Agent or pool.
func (r AgentRef) Names(other AgentRef) bool {
	return strings.TrimSpace(r.AgentID) == strings.TrimSpace(other.AgentID) &&
		strings.TrimSpace(r.PoolID) == strings.TrimSpace(other.PoolID)
}

// String renders the reference for a person: "agent:<id>" or "pool:<id>".
func (r AgentRef) String() string {
	switch {
	case r.IsPool():
		return "pool:" + strings.TrimSpace(r.PoolID)
	case !r.IsZero():
		return "agent:" + strings.TrimSpace(r.AgentID)
	default:
		return ""
	}
}

// Validate refuses a reference naming both an Agent and a pool, or a
// malformed identifier. field is the path the refusal names. A zero reference
// is valid; a slot that requires one uses RequireAgentRef.
func (r AgentRef) Validate(field string) error {
	agent, pool := strings.TrimSpace(r.AgentID), strings.TrimSpace(r.PoolID)
	switch {
	case agent != "" && pool != "":
		return &InputValidationError{Field: field, Message: "names an agent or a pool, not both"}
	case agent != "" && !validCatalogID(agent):
		return &InputValidationError{Field: field + ".agent_id", Message: catalogIDMessage}
	case pool != "" && !validCatalogID(pool):
		return &InputValidationError{Field: field + ".pool_id", Message: catalogIDMessage}
	}
	return nil
}

// RequireAgentRef refuses a zero reference as well as a malformed one.
func RequireAgentRef(ref AgentRef, field string) error {
	if ref.IsZero() {
		return &InputValidationError{Field: field, Message: "required; name an agent or a pool"}
	}
	return ref.Validate(field)
}

// ProfileLoadReader answers how many jobs are live on a runtime.
//
// IT IS AN INTERFACE OVER DATA THAT ALREADY EXISTS. JobLease carries the
// backend a job runs on and the lease store already counts live leases per
// backend for the concurrency cap — so placement reads the same number the cap
// enforces.
type ProfileLoadReader interface {
	// LiveJobsForBackend returns how many jobs hold a live lease naming this
	// backend, across every project of the tenant.
	LiveJobsForBackend(ctx context.Context, tenantID, backend string) (int, error)
}
