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

// =============================================================================
// Communication Topology
// =============================================================================

// CommunicationTopology defines the inter-agent messaging topology.
type CommunicationTopology string

const (
	// TopologyHubAndSpoke routes all messages through the lead agent.
	TopologyHubAndSpoke CommunicationTopology = "hub_and_spoke"

	// TopologyHybrid allows worker-to-worker advisory messages only.
	TopologyHybrid CommunicationTopology = "hybrid"

	// TopologyPeerToPeer allows unrestricted inter-agent messaging.
	TopologyPeerToPeer CommunicationTopology = "peer_to_peer"
)

// validTopologies is the set of valid communication topologies for O(1) validation.
var validTopologies = map[CommunicationTopology]struct{}{
	TopologyHubAndSpoke: {},
	TopologyHybrid:      {},
	TopologyPeerToPeer:  {},
}

// =============================================================================
// Workspace Isolation
// =============================================================================

// WorkspaceIsolation defines how agents share or isolate workspaces.
type WorkspaceIsolation string

const (
	// IsolationShared uses a single shared workspace for all agents.
	IsolationShared WorkspaceIsolation = "shared"

	// IsolationWorktree uses separate git worktrees per agent.
	IsolationWorktree WorkspaceIsolation = "worktree"
)

// validIsolations is the set of valid workspace isolation modes for O(1) validation.
var validIsolations = map[WorkspaceIsolation]struct{}{
	IsolationShared:   {},
	IsolationWorktree: {},
}

// =============================================================================
// Workflow Mode
// =============================================================================

// WorkflowMode defines the operational mode for an orchestration workflow.
type WorkflowMode string

const (
	// WorkflowModeCanned runs a fully predetermined workflow with fixed steps.
	WorkflowModeCanned WorkflowMode = "canned"

	// WorkflowModeConstrained allows the lead agent to decompose tasks but within
	// defined agent roles and capabilities from the workflow definition.
	WorkflowModeConstrained WorkflowMode = "constrained"

	// WorkflowModeAutonomous gives the lead agent full control over task
	// decomposition, agent spawning, and workflow execution.
	WorkflowModeAutonomous WorkflowMode = "autonomous"
)

// validWorkflowModes is the set of valid workflow modes for O(1) validation.
var validWorkflowModes = map[WorkflowMode]struct{}{
	WorkflowModeCanned:      {},
	WorkflowModeConstrained: {},
	WorkflowModeAutonomous:  {},
}

// =============================================================================
// Overflow and Budget Actions
// =============================================================================

// validOverflowActions is the set of valid overflow actions for O(1) validation.
var validOverflowActions = map[string]struct{}{
	"reject":      {},
	"drop_oldest": {},
}

// validBudgetActions is the set of valid budget actions for O(1) validation.
var validBudgetActions = map[string]struct{}{
	"warn":     {},
	"pause":    {},
	"shutdown": {},
}

// =============================================================================
// Workflow Definition
// =============================================================================

// WorkflowDefinition describes a complete orchestration workflow including
// agent roles, capabilities, and operational settings.
type WorkflowDefinition struct {
	Name        string           `yaml:"name" json:"name"`
	Description string           `yaml:"description" json:"description"`
	Mode        WorkflowMode     `yaml:"mode" json:"mode"`
	Agents      []AgentRoleSpec  `yaml:"agents" json:"agents"`
	Settings    WorkflowSettings `yaml:"settings" json:"settings"`
}

// Validate checks that the WorkflowDefinition is well-formed.
// It verifies the name is non-empty, the mode is valid, at least one agent
// is defined, and that canned/constrained modes include a lead agent role.
// It also validates all settings fields.
func (wd *WorkflowDefinition) Validate() error {
	if wd.Name == "" {
		return &WorkflowValidationError{
			Field:   "name",
			Message: "workflow name is required",
		}
	}
	if _, ok := validWorkflowModes[wd.Mode]; !ok {
		return &WorkflowValidationError{
			Field:   "mode",
			Message: "invalid workflow mode: " + string(wd.Mode),
		}
	}
	if len(wd.Agents) == 0 {
		return &WorkflowValidationError{
			Field:   "agents",
			Message: "at least one agent is required",
		}
	}
	// Canned and constrained modes require a lead agent role.
	if wd.Mode == WorkflowModeCanned || wd.Mode == WorkflowModeConstrained {
		hasLead := false
		for i := range wd.Agents {
			if wd.Agents[i].Role == "lead" {
				hasLead = true
				break
			}
		}
		if !hasLead {
			return &WorkflowValidationError{
				Field:   "agents",
				Message: "canned and constrained modes require a lead agent role",
			}
		}
	}
	// Validate settings.
	if err := wd.Settings.Validate(); err != nil {
		return err
	}
	return nil
}

// =============================================================================
// Agent Role Spec
// =============================================================================

// AgentRoleSpec defines a single agent role within a workflow, including
// its backend, model, capabilities, MCP permission scopes, and sandbox
// configuration for ACP agent execution.
type AgentRoleSpec struct {
	Role         string   `yaml:"role" json:"role"`
	Backend      string   `yaml:"backend" json:"backend"`
	Model        string   `yaml:"model" json:"model"`
	Capabilities []string `yaml:"capabilities" json:"capabilities"`
	MCPScopes    []string `yaml:"mcp_scopes" json:"mcp_scopes"`
	Count        int      `yaml:"count,omitempty" json:"count,omitempty"`

	// Sandbox configuration for ACP agent execution.
	WorkspacePath   string            `yaml:"workspace_path,omitempty" json:"workspace_path,omitempty"`     // Host path to bind-mount as /workspace
	PermissionMode  string            `yaml:"permission_mode,omitempty" json:"permission_mode,omitempty"`   // e.g., "bypassPermissions", "default"
	SkipPermissions bool              `yaml:"skip_permissions,omitempty" json:"skip_permissions,omitempty"` // --dangerously-skip-permissions
	Env             map[string]string `yaml:"env,omitempty" json:"env,omitempty"`                           // Additional environment variables

	// Container sandbox configuration. When Sandboxed is true, the agent
	// runs inside an OCI container with the workspace bind-mounted.
	Sandboxed      bool   `yaml:"sandboxed,omitempty" json:"sandboxed,omitempty"`             // Run in container
	ContainerImage string `yaml:"container_image,omitempty" json:"container_image,omitempty"` // Container image name

	// NumCtx specifies the Ollama context window size for this agent
	// (e.g., 32768). The model name in the workflow should already
	// include the context suffix (e.g., "ollama/llama3.3:70b-32k").
	// This field is informational for documentation and validation.
	NumCtx int `yaml:"num_ctx,omitempty" json:"num_ctx,omitempty"`
}

// =============================================================================
// Workflow Settings
// =============================================================================

// WorkflowSettings contains operational parameters for workflow execution
// including iteration limits, parallelism, timeouts, communication topology,
// workspace isolation, peer messaging, file locking, vector DB, cost controls,
// and recovery settings.
type WorkflowSettings struct {
	MaxIterations  int               `yaml:"max_iterations" json:"max_iterations"`
	MaxParallelism int               `yaml:"max_parallelism" json:"max_parallelism"`
	PollInterval   string            `yaml:"poll_interval" json:"poll_interval"`
	TimeoutPerItem string            `yaml:"timeout_per_item" json:"timeout_per_item"`
	TotalTimeout   string            `yaml:"total_timeout" json:"total_timeout"`
	ReviewCriteria []ReviewCriterion `yaml:"review_criteria" json:"review_criteria"`

	Topology      CommunicationTopology `yaml:"topology" json:"topology"`
	Isolation     WorkspaceIsolation    `yaml:"isolation" json:"isolation"`
	PeerMessaging PeerMessagingSettings `yaml:"peer_messaging" json:"peer_messaging"`
	FileLocking   FileLockSettings      `yaml:"file_locking" json:"file_locking"`
	VectorDB      VectorDBSettings      `yaml:"vector_db" json:"vector_db"`
	CostControls  CostControlSettings   `yaml:"cost_controls" json:"cost_controls"`
	Recovery      RecoverySettings      `yaml:"recovery" json:"recovery"`

	// Verbosity controls what audit events are displayed to the user.
	// "info"    - state transitions, task assignments, summaries (default)
	// "verbose" - above + full agent responses, tool calls
	// "debug"   - above + raw protocol messages, token usage
	// The audit trail always captures everything regardless of this setting.
	Verbosity string `yaml:"verbosity" json:"verbosity"`

	// NumCtx specifies the default Ollama context window size for all
	// agents in the workflow (e.g., 32768). Individual agent specs can
	// override this value with their own NumCtx setting.
	NumCtx int `yaml:"num_ctx,omitempty" json:"num_ctx,omitempty"`
}

// ApplyDefaults fills in zero-value fields with production-safe defaults.
func (ws *WorkflowSettings) ApplyDefaults() {
	if ws.Topology == "" {
		ws.Topology = TopologyHubAndSpoke
	}
	if ws.Isolation == "" {
		ws.Isolation = IsolationShared
	}
	// PeerMessaging defaults.
	if ws.PeerMessaging.MaxMessageSize == 0 {
		ws.PeerMessaging.MaxMessageSize = 10240
	}
	if ws.PeerMessaging.MaxInboxSize == 0 {
		ws.PeerMessaging.MaxInboxSize = 1000
	}
	if ws.PeerMessaging.OverflowAction == "" {
		ws.PeerMessaging.OverflowAction = "reject"
	}
	if ws.PeerMessaging.InboxTTL == "" {
		ws.PeerMessaging.InboxTTL = "1h"
	}
	// FileLocking defaults.
	if ws.FileLocking.LeaseTTL == "" {
		ws.FileLocking.LeaseTTL = "5m"
	}
	if ws.FileLocking.HeartbeatInterval == "" {
		ws.FileLocking.HeartbeatInterval = "30s"
	}
	if ws.FileLocking.MaxRenewals == 0 {
		ws.FileLocking.MaxRenewals = 60
	}
	// VectorDB defaults.
	if ws.VectorDB.IndexType == "" {
		ws.VectorDB.IndexType = "auto"
	}
	// Recovery defaults.
	if ws.Recovery.MaxRestarts == 0 {
		ws.Recovery.MaxRestarts = 3
	}
	if ws.Recovery.RestartBackoff == "" {
		ws.Recovery.RestartBackoff = "30s"
	}
}

// Validate checks all settings fields for validity.
func (ws *WorkflowSettings) Validate() error {
	if err := ValidateTopology(ws.Topology); err != nil {
		return err
	}
	if err := ValidateIsolation(ws.Isolation); err != nil {
		return err
	}
	if err := ValidateOverflowAction(ws.PeerMessaging.OverflowAction); err != nil {
		return err
	}
	if err := ValidateBudgetAction(ws.CostControls.BudgetAction); err != nil {
		return err
	}
	return nil
}

// =============================================================================
// Peer Messaging Settings
// =============================================================================

// PeerMessagingSettings controls peer-to-peer messaging behavior.
type PeerMessagingSettings struct {
	MaxMessageSize int    `yaml:"max_message_size" json:"max_message_size"`
	MaxInboxSize   int    `yaml:"max_inbox_size" json:"max_inbox_size"`
	OverflowAction string `yaml:"overflow_action" json:"overflow_action"` // "reject" or "drop_oldest"
	InboxTTL       string `yaml:"inbox_ttl" json:"inbox_ttl"`
}

// =============================================================================
// File Lock Settings
// =============================================================================

// FileLockSettings controls distributed file lock behavior.
type FileLockSettings struct {
	LeaseTTL          string `yaml:"lease_ttl" json:"lease_ttl"`
	HeartbeatInterval string `yaml:"heartbeat_interval" json:"heartbeat_interval"`
	MaxRenewals       int    `yaml:"max_renewals" json:"max_renewals"`
}

// =============================================================================
// Vector DB Settings
// =============================================================================

// VectorDBSettings controls auto-indexing vector database behavior.
type VectorDBSettings struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`
	IndexType      string   `yaml:"index_type" json:"index_type"`
	AutoIndex      bool     `yaml:"auto_index" json:"auto_index"`
	Triggers       []string `yaml:"triggers" json:"triggers"`
	GPUAccelerated bool     `yaml:"gpu_accelerated" json:"gpu_accelerated"`
}

// =============================================================================
// Cost Control Settings
// =============================================================================

// CostControlSettings controls token budget limits.
type CostControlSettings struct {
	MaxTokensPerJob   int64  `yaml:"max_tokens_per_job" json:"max_tokens_per_job"`
	MaxTokensPerAgent int64  `yaml:"max_tokens_per_agent" json:"max_tokens_per_agent"`
	BudgetAction      string `yaml:"budget_action" json:"budget_action"` // "warn", "pause", or "shutdown"
}

// =============================================================================
// Recovery Settings
// =============================================================================

// RecoverySettings controls crash recovery behavior.
type RecoverySettings struct {
	AutoRestart    bool   `yaml:"auto_restart" json:"auto_restart"`
	MaxRestarts    int    `yaml:"max_restarts" json:"max_restarts"`
	RestartBackoff string `yaml:"restart_backoff" json:"restart_backoff"`
}

// =============================================================================
// Review Criterion
// =============================================================================

// ReviewCriterion defines a single validation check applied during the
// review phase of a workflow execution.
type ReviewCriterion struct {
	Name      string `yaml:"name" json:"name"`
	Validator string `yaml:"validator" json:"validator"` // "agent", "command", "policy"
	Config    string `yaml:"config" json:"config"`
	Required  bool   `yaml:"required" json:"required"`
}

// =============================================================================
// Validation Functions
// =============================================================================

// ValidateTopology checks that the topology is a known valid value.
// An empty topology is accepted as it will be defaulted.
func ValidateTopology(t CommunicationTopology) error {
	if t == "" {
		return nil
	}
	if _, ok := validTopologies[t]; !ok {
		return &WorkflowValidationError{
			Field:   "topology",
			Message: "invalid topology: " + string(t),
		}
	}
	return nil
}

// ValidateIsolation checks that the isolation mode is a known valid value.
// An empty isolation is accepted as it will be defaulted.
func ValidateIsolation(i WorkspaceIsolation) error {
	if i == "" {
		return nil
	}
	if _, ok := validIsolations[i]; !ok {
		return &WorkflowValidationError{
			Field:   "isolation",
			Message: "invalid isolation: " + string(i),
		}
	}
	return nil
}

// ValidateOverflowAction checks that the overflow action is "reject" or "drop_oldest".
// An empty action is accepted as it will be defaulted.
func ValidateOverflowAction(action string) error {
	if action == "" {
		return nil
	}
	if _, ok := validOverflowActions[action]; !ok {
		return &WorkflowValidationError{
			Field:   "overflow_action",
			Message: "invalid overflow action: " + action + " (must be \"reject\" or \"drop_oldest\")",
		}
	}
	return nil
}

// ValidateBudgetAction checks that the budget action is "warn", "pause", or "shutdown".
// An empty action is accepted as it will be defaulted.
func ValidateBudgetAction(action string) error {
	if action == "" {
		return nil
	}
	if _, ok := validBudgetActions[action]; !ok {
		return &WorkflowValidationError{
			Field:   "budget_action",
			Message: "invalid budget action: " + action + " (must be \"warn\", \"pause\", or \"shutdown\")",
		}
	}
	return nil
}
