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

package orchestrator

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Orchestrator"
)

// Client provides orchestrator management operations including session,
// task, backend, and fleet management.
type Client interface {
	// SpawnSession creates a new agent session.
	SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error)

	// PromptSession sends a prompt to an existing session.
	// PromptSession sends a prompt to an agent session. The call returns as
	// soon as the server has recorded and dispatched the turn; the agent's
	// output arrives on the session event stream. See PromptSessionResponse
	// for the two response shapes.
	PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error)

	// SessionStatus returns the status of a session.
	SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error)

	// CloseSession terminates a session.
	CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error)

	// ListSessions returns every session, in any state (closed and error
	// included), optionally filtered by backend or status.
	ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)

	// RotateSessionAccount moves a session onto a different pooled provider
	// account, keeping its identifier, transcript and cost history. The same
	// verb runs automatically when a turn fails on a classified provider
	// limit; this is the operator's door to it.
	//
	// Only a session spawned with AccountAuto or AccountAllow may rotate — a
	// bare AccountID pin is honored and answered with a conflict.
	RotateSessionAccount(ctx context.Context, req *RotateSessionAccountRequest) (*RotateSessionAccountResponse, error)

	// CreateTask creates a new task.
	CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error)

	// GetTask returns a task by ID.
	GetTask(ctx context.Context, req *GetTaskRequest) (*Task, error)

	// ListTasks returns tasks matching the filter criteria.
	ListTasks(ctx context.Context, req *ListTasksRequest) (*ListTasksResponse, error)

	// ClaimTask claims a pending task for execution.
	ClaimTask(ctx context.Context, req *ClaimTaskRequest) (*ClaimTaskResponse, error)

	// CompleteTask marks a task as completed.
	CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error)

	// BlockTask parks a task until a person readmits it.
	BlockTask(ctx context.Context, req *BlockTaskRequest) (*BlockTaskResponse, error)

	// ReadmitTask returns a blocked task to the pending queue.
	ReadmitTask(ctx context.Context, req *ReadmitTaskRequest) (*ReadmitTaskResponse, error)

	// AttentionList returns one page of the tenant's attention items: every
	// blocked task, plus whatever environmental alerts this node reports —
	// "the factory's inbox for anything only an operator can clear.
	AttentionList(ctx context.Context, req *AttentionListRequest) (*AttentionListResponse, error)

	// AttentionResolve applies an operator's decision to one blocked task:
	// the ruling is posted to the task's forge issue, and the task is either
	// readmitted to the queue or (close_as_done) completed without being
	// readmitted.
	AttentionResolve(ctx context.Context, req *AttentionResolveRequest) (*AttentionItem, error)

	// DeleteTask removes a task and releases its source reservation, so the
	// task's source can be adopted again. Refused while a job holds a live
	// lease on the task.
	DeleteTask(ctx context.Context, req *DeleteTaskRequest) (*DeleteTaskResponse, error)

	// ListBackends returns available backends.
	ListBackends(ctx context.Context, params ...page.Params) (*ListBackendsResponse, error)

	// ListRunners returns every registered job runner and the states it
	// declares, in registry order. It is what a consumer reads instead of
	// keeping its own copy of the engine's job states — the job detail
	// stepper above all.
	ListRunners(ctx context.Context, params ...page.Params) (*ListJobRunnersResponse, error)

	// ListBackendModels returns the model names available on the named backend
	// as seen from the specified agent. AgentID is used as a host affinity hint
	// for the remote backend resolver; pass an empty string or "any" to skip
	// host affinity. The agent need not be online; if the backend exposes no
	// model lister or the agent is unknown, the response is an empty list with
	// no error.
	ListBackendModels(ctx context.Context, req *ListBackendModelsRequest) (*ListBackendModelsResponse, error)

	// Health returns the orchestrator health check report.
	Health(ctx context.Context) (*HealthCheckReport, error)

	// RegisterFleetNode registers a new agent with the fleet.
	RegisterFleetNode(ctx context.Context, req *RegisterFleetNodeRequest) (*RegisterFleetNodeResponse, error)

	// GetFleetNode returns an agent by ID.
	GetFleetNode(ctx context.Context, req *GetFleetNodeRequest) (*FleetNode, error)

	// ListFleetNodes returns agents matching the filter criteria.
	ListFleetNodes(ctx context.Context, req *ListFleetNodesRequest) (*ListFleetNodesResponse, error)

	// RemoveFleetNode removes an agent from the fleet.
	RemoveFleetNode(ctx context.Context, req *RemoveFleetNodeRequest) (*RemoveFleetNodeResponse, error)

	// FleetNodeHeartbeat sends a heartbeat for an agent.
	FleetNodeHeartbeat(ctx context.Context, req *FleetNodeHeartbeatRequest) (*FleetNodeHeartbeatResponse, error)

	// DecomposeTask decomposes a task into structured work items using an agent.
	DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error)

	// SpawnChildJob fans out a child job from a parent job's lead agent.
	// Returns ErrPermissionDenied if the caller is not the lead agent of the parent job.
	SpawnChildJob(ctx context.Context, req *SpawnChildJobRequest) (*SpawnChildJobResponse, error)

	// StartJob starts an autonomous orchestration control loop.
	StartJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error)

	// GetJobStatus returns the current status of an orchestration job.
	GetJobStatus(ctx context.Context, req *GetJobStatusRequest) (*JobStatus, error)

	// ListJobs returns all jobs for a tenant.
	ListJobs(ctx context.Context, req *ListJobsRequest) (*ListJobsResponse, error)

	// CancelJob cancels a running orchestration job.
	CancelJob(ctx context.Context, req *CancelJobRequest) (*CancelJobResponse, error)
	// SubscribeJobEvents returns a channel of real-time events for a running job.
	SubscribeJobEvents(ctx context.Context, req *SubscribeJobEventsRequest) (<-chan *JobEvent, error)

	// GetSessionHistory fetches a page of historical session events. Used
	// by the CLI `orchestrator attach` command and the web UI's scrollback
	// pager to page backward through an in-progress session's transcript.
	GetSessionHistory(ctx context.Context, req *GetSessionHistoryRequest) (*GetSessionHistoryResponse, error)

	// GetSessionConversation fetches a page of a session's CONVERSATION: who
	// said what, which tools ran, and what each answer cost. It is the
	// coarser sibling of GetSessionHistory, which pages the raw event log —
	// a streaming answer is many events and exactly one turn here, and a
	// page boundary always falls between turns.
	GetSessionConversation(ctx context.Context, req *GetSessionConversationRequest) (*GetSessionConversationResponse, error)

	// GetSpendReport returns one scope of the CONSUMPTION LEDGER: the tokens
	// spent under it, the drill-down to the level below, and the allowance
	// status of every pooled account that paid.
	//
	// ONE VERB FOR EVERY LEVEL, because the ledger's attribution chain is a
	// path — tenant/project/task/job/session/round — and a project's spend
	// and a job's spend are the same prefix scan at two depths.
	GetSpendReport(ctx context.Context, req *GetSpendReportRequest) (*GetSpendReportResponse, error)

	// GetTenantSpendPolicy returns a tenant's own spend limits and its live
	// usage against each window (go-quicbaas#1058).
	GetTenantSpendPolicy(ctx context.Context, req *GetTenantSpendPolicyRequest) (*TenantSpendPolicy, error)

	// SetTenantSpendPolicy replaces a tenant's own spend limits; no windows
	// makes the tenant unmetered.
	SetTenantSpendPolicy(ctx context.Context, req *SetTenantSpendPolicyRequest) (*TenantSpendPolicy, error)

	// SubscribeSessionEvents returns a live stream of session events. When
	// req.FromSequence > 0 the stream begins with a historical replay
	// from that sequence, then transitions to live tail when Follow is
	// true. The returned channel closes when the stream is done or the
	// context is cancelled.
	SubscribeSessionEvents(ctx context.Context, req *SubscribeSessionEventsRequest) (<-chan *SessionEventEnvelope, error)

	// Inference runs a stateless fleet-routed inference call against a named
	// backend without requiring a prior session Spawn. Returns the aggregated
	// response after the backend finishes.
	Inference(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error)

	// InferenceStream runs a stateless fleet-routed inference call and returns
	// a channel of InferenceChunk values streamed from the backend. The
	// channel is closed when the backend finishes or the context is cancelled.
	InferenceStream(ctx context.Context, req *InferenceRequest) (<-chan *InferenceChunk, error)

	// CreateWorkflow registers a new named workflow definition.
	CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*WorkflowDefinition, error)

	// ListWorkflows returns registered workflow definitions for a tenant.
	ListWorkflows(ctx context.Context, req *ListWorkflowsRequest) (*ListWorkflowsResponse, error)

	// GetWorkflow returns a single workflow definition by name.
	GetWorkflow(ctx context.Context, req *GetWorkflowRequest) (*WorkflowDefinition, error)

	// UpdateWorkflow replaces an existing workflow definition by name.
	// Returns ErrNotFound if the workflow does not exist.
	UpdateWorkflow(ctx context.Context, req *UpdateWorkflowRequest) (*WorkflowDefinition, error)

	// DeleteWorkflow removes a workflow definition by name.
	// Returns ErrNotFound if the workflow does not exist.
	DeleteWorkflow(ctx context.Context, req *DeleteWorkflowRequest) error

	// DisableFleetNode administratively disables an agent globally. Revokes the
	// agent's OAuth2 credentials and mTLS pin so subsequent token requests fail.
	DisableFleetNode(ctx context.Context, req *DisableFleetNodeRequest) (*DisableFleetNodeResponse, error)

	// EnableFleetNode re-activates a previously disabled agent.
	EnableFleetNode(ctx context.Context, req *EnableFleetNodeRequest) (*EnableFleetNodeResponse, error)

	// SubmitTaskArtifacts stores typed output artifacts for a task.
	SubmitTaskArtifacts(ctx context.Context, req *SubmitTaskArtifactsRequest) (*SubmitTaskArtifactsResponse, error)

	// GetTaskArtifact retrieves a named artifact from a task.
	GetTaskArtifact(ctx context.Context, req *GetTaskArtifactRequest) (*TaskArtifact, error)

	// ListTaskArtifacts returns all output artifact metadata for a task.
	ListTaskArtifacts(ctx context.Context, req *ListTaskArtifactsRequest) (*ListTaskArtifactsResponse, error)

	// GetCostSettings returns a tenant's runtime cost settings — the
	// electricity rate and engine draws its local inference is costed from —
	// and the rates in force. go-quicbaas#1058.
	GetCostSettings(ctx context.Context, req *GetCostSettingsRequest) (*CostSettings, error)

	// SetCostSettings replaces a tenant's runtime cost settings; settings
	// that override nothing restore the configuration.
	SetCostSettings(ctx context.Context, req *SetCostSettingsRequest) (*CostSettings, error)
}

// GetSessionHistoryRequest pages through historical session events.
type GetSessionHistoryRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	SessionID    string `json:"session_id"`
	FromSequence uint64 `json:"from_sequence,omitempty"` // inclusive lower bound; 0 = from start
	Limit        int    `json:"limit,omitempty"`         // <=0 = no limit (server enforces)
}

// GetSessionHistoryResponse carries one page of session events plus the
// cursor for the next page.
type GetSessionHistoryResponse struct {
	Events  []*SessionEventEnvelope `json:"events"`
	NextSeq uint64                  `json:"next_seq,omitempty"` // 0 when end-of-history
}

// SessionTurnRole names who produced a turn: "user", "assistant" or "system".
type SessionTurnRole string

// The roles a SessionTurn can carry.
const (
	// SessionTurnRoleUser is a prompt submitted by an operator.
	SessionTurnRoleUser SessionTurnRole = "user"
	// SessionTurnRoleAssistant is the agent's answer, coalesced from however
	// many fragments the backend streamed.
	SessionTurnRoleAssistant SessionTurnRole = "assistant"
	// SessionTurnRoleSystem is the orchestrator narrating something that
	// happened to the conversation.
	SessionTurnRoleSystem SessionTurnRole = "system"
)

// SessionTokenUsage is a turn's token accounting as the backend reported it.
type SessionTokenUsage struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

// SessionToolUse summarises one tool call inside an assistant turn. The
// arguments and the output stay on the event stream: a history page indexes
// the work rather than carrying it.
type SessionToolUse struct {
	InvocationID string `json:"invocation_id,omitempty"`
	Name         string `json:"name"`
	IsError      bool   `json:"is_error,omitempty"`
	ResultBytes  int64  `json:"result_bytes,omitempty"`
}

// SessionTurn is one record in a session's conversation.
type SessionTurn struct {
	// Sequence is the log sequence of the turn's first event, and the cursor
	// that addresses it.
	Sequence  uint64          `json:"sequence"`
	SessionID string          `json:"session_id"`
	TenantID  string          `json:"tenant_id,omitempty"`
	Role      SessionTurnRole `json:"role"`
	Timestamp time.Time       `json:"timestamp"`
	Text      string          `json:"text"`
	// TruncatedBytes is how much text the server's storage cap removed.
	// Non-zero means Text is not the whole of what was said.
	TruncatedBytes int64              `json:"truncated_bytes,omitempty"`
	Model          string             `json:"model,omitempty"`
	StopReason     string             `json:"stop_reason,omitempty"`
	Tools          []SessionToolUse   `json:"tools,omitempty"`
	Usage          *SessionTokenUsage `json:"usage,omitempty"`
}

// GetSessionConversationRequest pages through a session's conversation.
type GetSessionConversationRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	SessionID string `json:"session_id"`
	// Cursor is a previous page's NextCursor. 0 starts at the beginning of
	// the RETAINED conversation, which retention may have moved forward.
	Cursor uint64 `json:"cursor,omitempty"`
	// Limit is the maximum turns to return. 0 selects the server default
	// (100); the server caps it at 500.
	Limit int `json:"limit,omitempty"`
}

// GetSessionConversationResponse carries one page of a session's conversation.
type GetSessionConversationResponse struct {
	SessionID string         `json:"session_id"`
	TenantID  string         `json:"tenant_id,omitempty"`
	Turns     []*SessionTurn `json:"turns"`
	// NextCursor is the Cursor for the following page, 0 at the end.
	NextCursor uint64 `json:"next_cursor,omitempty"`
	// OldestSequence is the lowest sequence the server still holds. Above 1
	// means retention has dropped earlier turns.
	OldestSequence uint64 `json:"oldest_sequence,omitempty"`
}

// =============================================================================
// Spend reporting — the consumption ledger's read side
// =============================================================================

// SpendUnattributed addresses the subtree of entries that have NO value at a
// chain level — a job that belongs to no task, a turn charged to no project.
//
// Supply it as a scope level to open the row a report marks Unattributed. It
// is unambiguous because the server refuses it as an entity id at the write:
// no real project, task, job or session can bear this identifier.
// Mirrors orchestration.SpendUnattributed.
const SpendUnattributed = "-"

// SpendLevel names one level of the ledger's attribution chain.
// Mirrors orchestration.SpendLevel.
type SpendLevel string

// The chain's levels, outermost first.
const (
	SpendLevelTenant  SpendLevel = "tenant"
	SpendLevelProject SpendLevel = "project"
	SpendLevelTask    SpendLevel = "task"
	SpendLevelJob     SpendLevel = "job"
	SpendLevelSession SpendLevel = "session"
	SpendLevelRound   SpendLevel = "round"
)

// SpendPolicyStatus is what the server could establish about one account's
// allowance. Mirrors orchestration.SpendPolicyStatus.
type SpendPolicyStatus string

// The three answers. THEY ARE DIFFERENT FACTS and a caller must not collapse
// them: "none" says the account was read and meters nothing, "unknown" says
// nothing could read it.
const (
	// SpendPolicyUnknown means the deployment wires no account store, or the
	// record is gone. Render it as unknown, never as unmetered.
	SpendPolicyUnknown SpendPolicyStatus = "unknown"
	// SpendPolicyNone means the account carries no cap. This is the honest
	// "no spend policy" — not a budget of zero.
	SpendPolicyNone SpendPolicyStatus = "none"
	// SpendPolicyMetered means the account carries a quota window, a breaker
	// rule, or both.
	SpendPolicyMetered SpendPolicyStatus = "metered"
)

// SpendWindowStatus is one sliding quota window and what has been drawn
// against it. Mirrors orchestration.SpendWindowStatus.
type SpendWindowStatus struct {
	Name string `json:"name"`
	// Size is the window's length in NANOSECONDS, matching Go's
	// time.Duration JSON encoding on the server.
	Size            int64 `json:"size"`
	MaxTokens       int64 `json:"max_tokens"`
	UsedTokens      int64 `json:"used_tokens"`
	RemainingTokens int64 `json:"remaining_tokens"`
	// The dollar axis, in millionths of a US dollar at list price over the
	// window's priced turns; MaxEquivalentMicros is 0 when the window does not
	// meter dollars (and MaxTokens 0 when it does not meter tokens).
	MaxEquivalentMicros       int64 `json:"max_equivalent_micros,omitempty"`
	UsedEquivalentMicros      int64 `json:"used_equivalent_micros"`
	RemainingEquivalentMicros int64 `json:"remaining_equivalent_micros"`
	Exhausted                 bool  `json:"exhausted"`
	// FreesAt is when the window's oldest entry ages out. NIL when the
	// window holds nothing, because there is nothing to age out.
	//
	// A POINTER BECAUSE `omitempty` DOES NOT OMIT A ZERO time.Time, and this
	// type is RE-MARSHALLED: the Wails desktop binding hands it straight back
	// to the frontend, so a zero instant would reach a renderer as
	// "0001-01-01T00:00:00Z" and draw as January of year 1.
	FreesAt *time.Time `json:"frees_at,omitempty"`
}

// SpendBreakerVerdict is an account's derived circuit-breaker state and the
// evidence it was derived from. Mirrors orchestration.BreakerVerdict.
type SpendBreakerVerdict struct {
	// State is "closed", "open" or "half_open".
	State     string `json:"state"`
	Failures  int    `json:"failures"`
	Successes int    `json:"successes"`

	// Each instant is nil when it never happened, rather than the epoch. See
	// SpendWindowStatus.FreesAt for why these are pointers.
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`

	// OpenUntil is when an open breaker becomes half-open; nil otherwise.
	OpenUntil *time.Time `json:"open_until,omitempty"`
}

// SpendTotals is what a set of consumption entries sums to: the shape of a
// report's own totals and of every breakdown row under it. Mirrors
// orchestration.ConsumptionTotals, and is embedded so its fields sit flat on
// the JSON wire.
type SpendTotals struct {
	Entries int `json:"entries"`

	// InputTokens is the FRESH prompt; the two cache dimensions are prompt
	// tokens served from and written into the provider's cache.
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`

	// EquivalentMicros is the list-price equivalent of the PRICED entries, in
	// millionths of the report's Currency — what the tokens would have cost
	// on the provider's metered API.
	//
	// CONSULT PricedEntries BEFORE RENDERING IT. Zero priced entries means
	// nothing was priced — the models are not in the server's price table —
	// and the figure is a dash, never a zero. UnpricedEntries > 0 beside
	// priced ones means the figure is a floor.
	EquivalentMicros int64 `json:"equivalent_micros"`
	PricedEntries    int   `json:"priced_entries"`
	UnpricedEntries  int   `json:"unpriced_entries"`

	// BilledMicros is what the turns added to the bill actually paid: zero
	// per turn on a subscription, the list price on a metered key, the
	// operator's rate for an unpooled turn. Consult BilledEntries the same
	// way as PricedEntries.
	BilledMicros    int64 `json:"billed_micros"`
	BilledEntries   int   `json:"billed_entries"`
	UnbilledEntries int   `json:"unbilled_entries"`

	DurationMillis int64 `json:"duration_ms"`

	// EnergyMilliWh is the energy drawn by the turns costed from electricity,
	// in milliwatt-hours (a kWh is 1e6), and ElectricityEntries counts them.
	// Consult ElectricityEntries before rendering it: none is a dash.
	EnergyMilliWh      int64 `json:"energy_mwh"`
	ElectricityEntries int   `json:"electricity_entries"`

	// MeasuredEnergyEntries counts the electricity entries whose energy came
	// from fleet device telemetry rather than a configured draw; the rest of
	// ElectricityEntries were estimated.
	MeasuredEnergyEntries int `json:"measured_energy_entries"`

	Successes int `json:"successes"`
	Failures  int `json:"failures"`
}

// SpendAccountBilling is how a pooled account is billed. Mirrors
// orchestration.AccountBilling.
type SpendAccountBilling struct {
	// Mode is "subscription", "api", or empty when not configured.
	Mode string `json:"mode,omitempty"`
	// MonthlyFeeMicros is a subscription's flat monthly fee; 0 when not
	// configured.
	MonthlyFeeMicros int64 `json:"monthly_fee_micros,omitempty"`
}

// SpendAccount is one pooled account's contribution to a scope, and the
// allowance it draws against. Mirrors orchestration.SpendAccount.
type SpendAccount struct {
	AccountID   string              `json:"account_id"`
	DisplayName string              `json:"display_name,omitempty"`
	Provider    string              `json:"provider,omitempty"`
	Billing     SpendAccountBilling `json:"billing,omitempty"`
	SpendTotals

	PolicyStatus SpendPolicyStatus   `json:"policy_status"`
	Windows      []SpendWindowStatus `json:"windows,omitempty"`
	// Breaker is nil when the account carries no breaker rule, which is not
	// the same as a closed breaker.
	Breaker *SpendBreakerVerdict `json:"breaker,omitempty"`

	// SubscriptionFeeMicros is the part of a subscription's monthly fee that
	// falls in the report's range. Set only on a tenant-level report and only
	// for a subscription whose fee is configured; nil renders as a dash.
	SubscriptionFeeMicros *int64 `json:"subscription_fee_micros,omitempty"`
}

// SpendModel is one engine/model pair's share of a scope's spend.
// Mirrors orchestration.SpendModel.
type SpendModel struct {
	Engine string `json:"engine"`
	Model  string `json:"model"`
	SpendTotals
}

// SpendChild is one drill-down row: the next chain level below the scope.
// Mirrors orchestration.SpendChild.
type SpendChild struct {
	// ID is the child's identifier, empty when Unattributed.
	ID    string     `json:"id,omitempty"`
	Level SpendLevel `json:"level"`
	// Unattributed marks the subtree the ledger files under its no-value
	// sentinel — a job with no task. Real spend with no parent at this
	// level, to be labelled rather than shown as an entity named "-".
	Unattributed bool `json:"unattributed,omitempty"`
	// Name is the child's human name where the level has one (a project),
	// empty when unattributed or when the entity no longer exists.
	Name string `json:"name,omitempty"`
	SpendTotals
}

// GetSpendReportRequest addresses one scope of the attribution chain.
//
// The levels are a PATH: each may be set only if every level above it is. A
// scope that skips one is refused by the server rather than answered by a
// scan of the tenant's whole history.
type GetSpendReportRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	JobID     string `json:"job_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	// Round selects one round below the session, applied only when RoundSet
	// is true so round 0 — a real level — is distinguishable from "every
	// round".
	Round    int  `json:"round,omitempty"`
	RoundSet bool `json:"round_set,omitempty"`

	// From and To narrow the report to the turns that occurred in
	// [From, To). From alone ranges up to now; To alone is refused. Both nil
	// report the scope's whole history.
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`

	// Host narrows the report to the turns that ran on one host, as the
	// report's Hosts breakdown names it; "-" selects the turns that name
	// none. Empty reports every host.
	Host string `json:"host,omitempty"`

	// Agent narrows the report to the turns of the sessions one tenant Agent
	// ran, as the report's Agents breakdown names it; "-" selects the
	// raw-backend sessions. Empty reports every Agent.
	Agent string `json:"agent,omitempty"`
}

// SpendAgent is one tenant Agent's share of a scope's spend.
type SpendAgent struct {
	// AgentID is the Agent's catalog ID, empty when Unattributed.
	AgentID string `json:"agent_id,omitempty"`
	// Name is the Agent's name when it still exists.
	Name string `json:"name,omitempty"`
	// Unattributed marks the turns of raw-backend sessions.
	Unattributed bool `json:"unattributed,omitempty"`
	SpendTotals
}

// SpendHost is one host's share of a scope's spend.
type SpendHost struct {
	// Host is the host the turns ran on, empty when Unattributed.
	Host string `json:"host,omitempty"`
	// Unattributed marks the turns that name no host.
	Unattributed bool `json:"unattributed,omitempty"`
	SpendTotals
}

// GetSpendReportResponse is one scope's consumption and its drill-down.
type GetSpendReportResponse struct {
	Level     SpendLevel `json:"level"`
	TenantID  string     `json:"tenant_id"`
	ProjectID string     `json:"project_id,omitempty"`
	TaskID    string     `json:"task_id,omitempty"`
	JobID     string     `json:"job_id,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	Round     int        `json:"round,omitempty"`

	// ProjectName is the name of the project the scope addresses, when it
	// still exists.
	ProjectName string `json:"project_name,omitempty"`

	SpendTotals

	// Currency is what every money figure on the report is denominated in.
	Currency string `json:"currency"`

	// FirstAt and LastAt bound the entries summed; both nil when Entries is
	// zero. Pointers for the reason SpendWindowStatus.FreesAt is one.
	FirstAt *time.Time `json:"first_at,omitempty"`
	LastAt  *time.Time `json:"last_at,omitempty"`

	// From and To echo the range the report was narrowed to; both nil for a
	// report over the scope's whole history.
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`

	// SubscriptionFeeMicros sums the accounts' fee shares: what the tenant's
	// subscriptions cost over the range. Nil below the tenant level or when
	// no subscription carries a fee.
	SubscriptionFeeMicros *int64 `json:"subscription_fee_micros,omitempty"`

	Accounts []SpendAccount `json:"accounts,omitempty"`
	Models   []SpendModel   `json:"models,omitempty"`

	// ChildLevel names the level Children are at, empty at the deepest level
	// where there is nothing below to attribute to.
	ChildLevel SpendLevel   `json:"child_level,omitempty"`
	Children   []SpendChild `json:"children,omitempty"`

	// ScopePolicyStatus and ScopeWindows are the scope's OWN spend limits —
	// a project's at the project level, the tenant's at the tenant level —
	// with live usage against each window. A spent one defers admission.
	// Both are empty at the deeper levels, which carry no limits.
	ScopePolicyStatus SpendPolicyStatus   `json:"scope_policy_status,omitempty"`
	ScopeWindows      []SpendWindowStatus `json:"scope_windows,omitempty"`

	// Host echoes the host the report was narrowed to, empty for every host;
	// Hosts breaks the totals down by the host each turn ran on.
	Host  string      `json:"host,omitempty"`
	Hosts []SpendHost `json:"hosts,omitempty"`

	// Agent echoes the tenant Agent the report was narrowed to; Agents breaks
	// the totals down by the Agent each turn's session ran as.
	Agent  string       `json:"agent,omitempty"`
	Agents []SpendAgent `json:"agents,omitempty"`

	// AgentPolicyStatus and AgentWindows are the narrowed-to Agent's own
	// spend limits with live usage against each window — what a spawn of the
	// Agent is checked against. Empty when not narrowed to one Agent.
	AgentPolicyStatus SpendPolicyStatus   `json:"agent_policy_status,omitempty"`
	AgentWindows      []SpendWindowStatus `json:"agent_windows,omitempty"`
}

// SubscribeSessionEventsRequest opens a live stream over an existing
// session's event log.
type SubscribeSessionEventsRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	SessionID    string `json:"session_id"`
	FromSequence uint64 `json:"from_sequence,omitempty"` // 0 = only new events; 1+ = replay-then-tail
	Follow       bool   `json:"follow"`                  // true = continue streaming after replay
}

// SessionEventEnvelope wraps one event delivered over a subscribe stream.
// PayloadJSON is the raw encoded orchestration.SessionEvent — clients
// json.Unmarshal this themselves, or use a downstream helper that dispatches
// on Kind. Exposing the raw payload keeps the SDK stable as new event kinds
// are added.
type SessionEventEnvelope struct {
	SessionID   string    `json:"session_id"`
	TenantID    string    `json:"tenant_id,omitempty"`
	Sequence    uint64    `json:"sequence"`
	PayloadJSON []byte    `json:"payload_json"`
	Timestamp   time.Time `json:"timestamp"`
	FromReplay  bool      `json:"from_replay,omitempty"`
}

// Session management types

// SandboxMode mirrors orchestration.SandboxMode for SDK callers so the SDK
// does not force a dependency on the server-side orchestration package.
type SandboxMode int32

const (
	// SandboxModeDefault defers to the server's configured policy.
	SandboxModeDefault SandboxMode = 0
	// SandboxModeRequired forces container-sandboxed execution.
	SandboxModeRequired SandboxMode = 1
	// SandboxModeNone forces raw process execution, subject to policy.
	SandboxModeNone SandboxMode = 2
)

// SpawnSessionRequest is a request to create a new agent session.
type SpawnSessionRequest struct {
	// AgentID or PoolID names the tenant Agent or pool the session runs as.
	// The Agent supplies backend, model, image and arguments, so a request
	// naming one names no Backend or Model; a pool fails over along its
	// Agents.
	AgentID string `json:"agent_id,omitempty"`
	PoolID  string `json:"pool_id,omitempty"`
	// Role is the kind of work the session does ("general" for an
	// interactive session), for a pooled Agent's placement policy.
	Role string `json:"role,omitempty"`

	// PromptAppend is run-time prompt material appended after the prompt the
	// spawn resolves: the Agent's own, else its role's built-in charter. At
	// most 64 KiB.
	PromptAppend string `json:"prompt_append,omitempty"`

	Backend      string            `json:"backend,omitempty"`
	ProjectID    string            `json:"project_id,omitempty"`
	Model        string            `json:"model,omitempty"`
	WorkDir      string            `json:"work_dir,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	SandboxMode  SandboxMode       `json:"sandbox_mode,omitempty"`
	Sandboxed    bool              `json:"sandboxed,omitempty"`     // deprecated: use SandboxMode
	HostAffinity string            `json:"host_affinity,omitempty"` // pin to a specific fleet node daemon (Name or Host)

	// HostAffinityStrict, when true, forces hard host_affinity semantics:
	// the server returns NoMatchingFleetNodeError (HTTP 400) if the named host
	// is offline rather than falling back to any available daemon. Defers
	// to the operator-level orchestration.host_affinity.mode default when
	// false.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`

	// Persistent asks for a durable multi-turn session: the backend registers
	// it so Prompt, Status, Close and resume all address the SAME agent
	// process, and its record survives a daemon restart. A non-persistent
	// session runs one-shot-per-prompt, is never resumable, and is concluded
	// rather than restored at boot.
	//
	// A POINTER BECAUSE ABSENT AND FALSE ARE DIFFERENT ANSWERS. Unset means
	// "the deployment's default", which resolves to persistent -- the
	// behaviour REST callers have always had, since the REST handler defaults
	// an omitted field to true. A plain bool would make every caller that did
	// not think about it ask for one-shot, which is the defect this field
	// exists to close: gRPC, Unix and embedded callers were silently getting
	// one-shot sessions while REST and MCP callers got durable ones, so a
	// follow-up prompt reached a process that was never registered.
	Persistent *bool `json:"persistent,omitempty"`

	// MCPServers preconfigures MCP servers for the spawned agent (e.g. the
	// QuicBaaS MCP endpoint, giving the agent cert-authenticated access to
	// platform tools). Serialized as mcp_servers; the server forwards each
	// server's connection material to the spawned agent's environment.
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`

	// --- Pooled provider account selection (see sdk/go/client/account) -----

	// AccountID pins the session to a specific pooled provider account.
	// Leaving both AccountID and AccountAuto unset preserves the pre-account
	// behavior: the backend authenticates however its CLI already does.
	AccountID string `json:"account_id,omitempty"`

	// AccountAuto requests strategy-based account selection when AccountID
	// is empty. This is the only way to reach the dynamic (least-usage)
	// strategy from outside the process.
	AccountAuto bool `json:"account_auto,omitempty"`

	// AccountStrategy names the selection strategy ("static",
	// "least_usage"). Empty selects the registry default. Ignored unless
	// AccountAuto is true.
	AccountStrategy string `json:"account_strategy,omitempty"`

	// AccountAllow optionally names the accounts this session may use and
	// rotate through — "these three seats, nothing else". It is an additive
	// filter, never a source: an ID the tenant does not own is simply not a
	// candidate.
	//
	// Together with AccountAuto it is what authorizes rotation. A session with
	// neither is treated as a deliberate pin and is never moved off its
	// account, so a caller that wants RotateSessionAccount to work on a
	// specific set of seats sets this.
	AccountAllow []string `json:"account_allow,omitempty"`
}

// RotateSessionAccountRequest asks the server to move one session onto a
// different pooled provider account.
type RotateSessionAccountRequest struct {
	// SessionID is the session to move. Required.
	SessionID string `json:"session_id"`

	// Reason is recorded on the closed account segment and in the session
	// transcript. Optional.
	Reason string `json:"reason,omitempty"`

	// HoldOutgoing holds the account being left out of the pool until
	// AvailableAt, so the next selection — for this session or any other —
	// does not walk straight back into the seat that was just condemned. An
	// operator rotating for a reason of their own (rebalancing, retiring a
	// seat) leaves it false.
	HoldOutgoing bool `json:"hold_outgoing,omitempty"`

	// AvailableAt is when a held outgoing account rejoins the pool. Ignored
	// unless HoldOutgoing is set; the zero time applies the server's default
	// backoff.
	AvailableAt time.Time `json:"available_at,omitempty"`
}

// RotateSessionAccountResponse reports the outcome of a rotation.
type RotateSessionAccountResponse struct {
	// SessionID is the session, unchanged — a rotation never renames one.
	SessionID string `json:"session_id"`

	// PreviousAccountID is the account the session was moved off.
	PreviousAccountID string `json:"previous_account_id,omitempty"`

	// AccountID is the replacement now in force.
	AccountID string `json:"account_id"`

	// Strategy names the policy that selected the replacement.
	Strategy string `json:"strategy,omitempty"`

	// Mode reports which mechanism moved the session: "in_place" swapped the
	// stored credential configuration with no process restart and no replay;
	// "resume" closed the agent process and reconstituted it against the
	// replacement, replaying the transcript.
	Mode string `json:"mode"`
}

// MCPServerConfig declares an MCP server made available to a spawned agent.
// Mirrors orchestration.MCPServerConfig: an HTTP server (such as the QuicBaaS
// MCP endpoint) is described by URL + Env; a stdio server by Command + Args.
// Env carries connection material (e.g. the agent's mTLS cert/key file paths)
// and round-trips losslessly to the spawned agent.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// SpawnSessionResponse is the response from creating a new session.
type SpawnSessionResponse struct {
	SessionID   string `json:"session_id"`
	Backend     string `json:"backend"`
	Model       string `json:"model,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	// ResolvedAgentID is the tenant Agent the session runs as when the spawn
	// named an Agent or a pool; Failovers are the Agents a pooled spawn
	// passed over first, in order.
	ResolvedAgentID string           `json:"resolved_agent_id,omitempty"`
	Failovers       []agents.Attempt `json:"failovers,omitempty"`
}

// PromptSessionRequest is a request to send a prompt to a session.
type PromptSessionRequest struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
}

// PromptSessionResponse is the response from prompting a session.
//
// Prompting an existing session is asynchronous: the call returns as soon as
// the server has recorded the turn and dispatched it, long before the agent
// has finished. Such a response has Accepted true, Done false, an empty
// Response, and a TurnID — the turn's output arrives on the session event
// stream (SubscribeSessionEvents), and the turn ends when a turn_completed or
// turn_failed event carrying the same TurnID appears there.
//
// Prompting a backend with no session is a stateless one-shot and stays
// synchronous: Done is true and Response holds the whole turn.
//
// Failures are reported as errors, never as a zero-valued response. Build the
// response with NewPromptSessionAccepted or NewPromptSessionResponse so the
// flattening and completion rules stay in one place.
type PromptSessionResponse struct {
	// SessionID identifies the session that produced the turn.
	SessionID string `json:"session_id"`

	// Response is the concatenated text of the turn's content blocks. Empty
	// for an accepted asynchronous turn, which has produced no output yet.
	Response string `json:"response"`

	// Done reports that the agent's turn completed. Never true for an
	// accepted asynchronous turn; observe its terminal state on the session
	// event stream instead.
	Done bool `json:"done"`

	// Accepted reports that the server took the turn for background
	// execution. Mutually exclusive with Done.
	Accepted bool `json:"accepted,omitempty"`

	// TurnID correlates an accepted turn with its turn_started event and with
	// the turn_completed / turn_failed event that closes it.
	TurnID string `json:"turn_id,omitempty"`

	// Sequence is the session-log sequence of the accepted turn's
	// turn_started event. Pass it as SubscribeSessionEventsRequest.FromSequence
	// to receive the whole turn with no gap and no duplicates.
	Sequence uint64 `json:"sequence,omitempty"`

	// Model is the model that produced the turn, when the backend reports it.
	Model string `json:"model,omitempty"`

	// StopReason is why the agent stopped ("end_turn", "max_tokens", ...).
	// Empty for an accepted turn, which has not stopped yet.
	StopReason string `json:"stop_reason,omitempty"`
}

// SessionStatusRequest is a request to check session status.
type SessionStatusRequest struct {
	SessionID string `json:"session_id"`
}

// SessionStatusResponse is the response from checking session status.
type SessionStatusResponse struct {
	SessionID   string `json:"session_id"`
	Backend     string `json:"backend"`
	Model       string `json:"model,omitempty"`
	Status      string `json:"status"`
	ContainerID string `json:"container_id,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	LastActive  int64  `json:"last_active"`

	// JobID names the orchestration job the session works for, empty when it
	// belongs to none.
	JobID string `json:"job_id,omitempty"`

	// Runner is the job runner that drove that job — "sdlc", "review",
	// "triage", "workflow" — and so which ROLE the session played. Empty
	// when the session belongs to no job, or to one no longer on record.
	Runner string `json:"runner,omitempty"`

	// AgentID is the fleet node the session's job was dispatched to, empty
	// when it belongs to no job or the work ran in-process.
	AgentID string `json:"agent_id,omitempty"`
}

// CloseSessionRequest is a request to close a session.
type CloseSessionRequest struct {
	SessionID string `json:"session_id"`
}

// CloseSessionResponse is the response from closing a session.
type CloseSessionResponse struct {
	SessionID string `json:"session_id"`
	Closed    bool   `json:"closed"`
}

// ListSessionsRequest is a request to list sessions.
type ListSessionsRequest struct {
	Backend string `json:"backend,omitempty"`
	Status  string `json:"status,omitempty"`

	// ProjectID narrows the listing to one project's sessions.
	ProjectID string `json:"project_id,omitempty"`

	// JobID narrows the listing to the sessions one job spawned.
	JobID string `json:"job_id,omitempty"`

	// Runner narrows the listing to the sessions whose job one runner drove
	// — "sdlc", "review", "triage", "workflow" — which is the ROLE the
	// session played.
	Runner string `json:"runner,omitempty"`

	// AgentID narrows the listing to the sessions running on one fleet
	// agent.
	AgentID string `json:"agent_id,omitempty"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
}

// ListSessionsResponse is the response from listing sessions.
type ListSessionsResponse struct {
	Sessions []*SessionStatusResponse `json:"sessions"`
	Count    int                      `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// Task management types

// TaskResult represents the result of a completed task.
type TaskResult struct {
	Output   string            `json:"output"`
	Files    []string          `json:"files,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Task represents a task in the orchestrator.
type Task struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	TeamName     string            `json:"team_name"`
	Subject      string            `json:"subject"`
	Description  string            `json:"description,omitempty"`
	Status       string            `json:"status"`
	AssignedTo   string            `json:"assigned_to,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	CreatedAt    int64             `json:"created_at"`
	ClaimedAt    int64             `json:"claimed_at,omitempty"`
	CompletedAt  int64             `json:"completed_at,omitempty"`
	Version      uint64            `json:"version"`
	Result       *TaskResult       `json:"result,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`

	// AutoSpawn is true when the task was created with immediate dispatch.
	AutoSpawn bool `json:"auto_spawn,omitempty"`

	// SpawnedJobID is the ID of the auto-spawned job, empty when not spawned.
	SpawnedJobID string `json:"spawned_job_id,omitempty"`

	// SpawnedAt is a nanosecond Unix timestamp of when the job was dispatched.
	SpawnedAt int64 `json:"spawned_at,omitempty"`

	// ProjectID scopes the task to a registered project.
	ProjectID string `json:"project_id,omitempty"`

	// OutputArtifacts are the typed artifacts the task has produced.
	OutputArtifacts []*TaskArtifact `json:"output_artifacts,omitempty"`

	// Source records where the task came from. Nil means no recorded origin.
	Source *TaskSource `json:"source,omitempty"`

	// Priority orders the task for dispatch; higher is dispatched first.
	Priority int `json:"priority,omitempty"`

	// Attempts counts the jobs dispatched for the task so far.
	Attempts int `json:"attempts,omitempty"`

	// AttemptBudget bounds Attempts; zero means the engine default.
	AttemptBudget int `json:"attempt_budget,omitempty"`

	// EnvironmentFaults counts attempts that ended because of the
	// environment rather than the work.
	EnvironmentFaults int `json:"environment_faults,omitempty"`

	// QuestionAttempts counts attempts that ended with the agent asking a
	// person rather than judging the work. Refunded against the budget like
	// EnvironmentFaults, but reported separately: one means the factory is
	// broken, the other means the worker did the right thing and is waiting.
	QuestionAttempts int `json:"question_attempts,omitempty"`

	// ConsecutiveEnvironmentFaults counts environment faults since the last
	// attempt that got past the environment.
	ConsecutiveEnvironmentFaults int `json:"consecutive_environment_faults,omitempty"`

	// RetryNotBefore is a nanosecond Unix instant before which the task must
	// not be dispatched again; zero means now.
	RetryNotBefore int64 `json:"retry_not_before,omitempty"`

	// BlockedReason says why the task is blocked.
	BlockedReason string `json:"blocked_reason,omitempty"`

	// BlockKind classifies a block: needs_info, blocked or escalated.
	BlockKind string `json:"block_kind,omitempty"`

	// StatusChangedAt is the nanosecond Unix instant of the last status
	// transition.
	StatusChangedAt int64 `json:"status_changed_at,omitempty"`

	// Runner names the JobRunner that drives an attempt at the task; empty
	// means the engine's lead-driven "workflow" runner.
	Runner string `json:"runner,omitempty"`

	// PromptAppend is run-time prompt material every attempt at the task
	// carries to the sessions it spawns.
	PromptAppend string `json:"prompt_append,omitempty"`

	// Routing are the placement hints a dispatch reads, kept on the record
	// so a requeue dispatches under the same constraints.
	Routing TaskRouting `json:"routing,omitzero"`

	// DispatchReason says why the last dispatch produced no job. A
	// successful dispatch clears it.
	DispatchReason string `json:"dispatch_reason,omitempty"`

	// BlockFaultClass names the environment fault class a block came from;
	// empty for an operator hold, a question, or an unclassified escalation.
	BlockFaultClass string `json:"block_fault_class"`

	// ConclusionReason is the typed reason the task stopped — completed,
	// budget_exhausted, abandoned, fault_bounded, fault_held,
	// parked_needs_info, parked_blocked, parked_escalated, runner_parked,
	// readmitted, already_satisfied or superseded. Empty while it is open.
	ConclusionReason string `json:"conclusion_reason"`

	// ChargedAttempts is Attempts less those refunded as environment faults
	// or questions. Derived by the server on read.
	ChargedAttempts int `json:"charged_attempts"`

	// EffectiveBudget is AttemptBudget, or the engine default the task runs
	// under when it declares none. Derived by the server on read.
	EffectiveBudget int `json:"effective_budget"`

	// FaultClass names the class of the fault behind the task's latest
	// concluded attempt; for a blocked task, the block's own class. Derived
	// by the server on read.
	FaultClass string `json:"fault_class"`

	// FaultOrigin says whose failure stopped the task: "environment" (the
	// fleet, an account pool, a forge), "work" (the task failed on its own
	// merits) or "undiagnosed" (its node stopped and nothing diagnosed why).
	// Empty for a task that did not stop on a failure. Derived by the server
	// on read.
	FaultOrigin string `json:"fault_origin"`
}

// TaskRouting are the placement hints a dispatch reads when it decides where
// an attempt at a task runs. The zero value constrains nothing.
type TaskRouting struct {
	// Backend restricts the attempt to agents running this backend.
	Backend string `json:"backend,omitempty"`

	// HostAffinity pins the attempt to an agent by Name or Host.
	HostAffinity string `json:"host_affinity,omitempty"`

	// GpuRequired restricts the attempt to agents advertising "gpu".
	GpuRequired bool `json:"gpu_required,omitempty"`
}

// TaskSource records where a task came from. Kind is scheduler, operator,
// job, or a kind a registered task source declared; (Kind, Ref) is unique
// within a tenant and project.
type TaskSource struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	URL  string `json:"url,omitempty"`
}

// CreateTaskRequest is a request to create a new task.
type CreateTaskRequest struct {
	TenantID     string            `json:"tenant_id"`
	TeamName     string            `json:"team_name,omitempty"`
	Subject      string            `json:"subject"`
	Description  string            `json:"description,omitempty"`
	AssignedTo   string            `json:"assigned_to,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`

	// ProjectID scopes the task to a registered project. When non-empty,
	// ClaimTask enforces the project's FleetNodeRoles access matrix against the
	// claiming agent. Empty means the task is unscoped (legacy: allow all).
	ProjectID string `json:"project_id,omitempty"`

	// AutoSpawn, when true, dispatches a one-shot job on the best-matching
	// agent immediately. The task is persisted regardless; if no agent matches
	// the routing constraints, ErrNoMatchingFleetNode is returned with an empty
	// SpawnedJobID on the returned task.
	AutoSpawn bool `json:"auto_spawn,omitempty"`

	// Backend restricts agent selection during auto-spawn to agents whose
	// Backend field matches this value. Ignored when AutoSpawn is false.
	Backend string `json:"backend,omitempty"`

	// GpuRequired restricts auto-spawn dispatch to agents that advertise
	// "gpu" in their capabilities. Ignored when AutoSpawn is false.
	GpuRequired bool `json:"gpu_required,omitempty"`

	// HostAffinity pins the task to an agent by Name or Host. Empty means
	// any agent.
	HostAffinity string `json:"host_affinity,omitempty"`

	// Source records where the task came from. The server refuses a second
	// task for the same (Kind, Ref) within the tenant and project.
	Source *TaskSource `json:"source,omitempty"`

	// Priority orders the task for dispatch; higher is dispatched first.
	Priority int `json:"priority,omitempty"`

	// AttemptBudget bounds the jobs dispatched for the task.
	AttemptBudget int `json:"attempt_budget,omitempty"`

	// Runner names the JobRunner that drives attempts at the task. Empty
	// resolves to the engine's own "workflow" runner; a name nothing
	// registered is refused.
	Runner string `json:"runner,omitempty"`

	// PromptAppend is run-time prompt material every attempt at the task
	// carries to the sessions it spawns — the worker's, for a module
	// runner. At most 64 KiB.
	PromptAppend string `json:"prompt_append,omitempty"`
}

// GetTaskRequest is a request to get a task by ID.
type GetTaskRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
}

// ListTasksRequest is a request to list tasks.
type ListTasksRequest struct {
	TenantID   string `json:"tenant_id"`
	Status     string `json:"status,omitempty"`
	TeamName   string `json:"team_name,omitempty"`
	AssignedTo string `json:"assigned_to,omitempty"`

	// ProjectID restricts the listing to one project.
	ProjectID string `json:"project_id,omitempty"`

	// Statuses restricts the listing to tasks in any of these statuses. It
	// composes with Status: both must hold when both are set.
	Statuses []string `json:"statuses,omitempty"`

	// AutoSpawn restricts the listing to the tasks that asked for immediate
	// dispatch (true) or to the ones queued for a claim (false). Nil does
	// not narrow: a bool could not tell "queued only" from "no preference".
	AutoSpawn *bool `json:"auto_spawn,omitempty"`

	// AgentID restricts the listing to the tasks whose LATEST ATTEMPT ran on
	// one fleet node. The agent is the attempt's job's, not the task's; the
	// server resolves it for the listing.
	AgentID string `json:"agent_id,omitempty"`

	// JobID restricts the listing to the task whose latest attempt ran as
	// that job.
	JobID string `json:"job_id,omitempty"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
}

// ListTasksResponse is the response from listing tasks.
type ListTasksResponse struct {
	Tasks []*Task `json:"tasks"`
	Count int     `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// ClaimTaskRequest is a request to claim a task for execution.
type ClaimTaskRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
	AgentID  string `json:"agent_id"`
}

// ClaimTaskResponse is the response from claiming a task.
type ClaimTaskResponse struct {
	TaskID  string `json:"task_id"`
	AgentID string `json:"agent_id"`
	Claimed bool   `json:"claimed"`
}

// CompleteTaskRequest is a request to mark a task as completed.
type CompleteTaskRequest struct {
	TenantID string      `json:"tenant_id"`
	TaskID   string      `json:"task_id"`
	Result   *TaskResult `json:"result,omitempty"`
}

// CompleteTaskResponse is the response from completing a task.
type CompleteTaskResponse struct {
	TaskID    string `json:"task_id"`
	Completed bool   `json:"completed"`
}

// BlockTaskRequest parks a task until a person readmits it.
type BlockTaskRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`

	// Kind is why the task is parked: needs_info, blocked or escalated.
	Kind string `json:"kind"`

	// Reason is surfaced to the person the task waits for.
	Reason string `json:"reason"`
}

// BlockTaskResponse is the response from blocking a task.
type BlockTaskResponse struct {
	TaskID  string `json:"task_id"`
	Kind    string `json:"kind,omitempty"`
	Blocked bool   `json:"blocked"`
}

// ReadmitTaskRequest returns a blocked task to the pending queue.
type ReadmitTaskRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
}

// ReadmitTaskResponse is the response from readmitting a task.
type ReadmitTaskResponse struct {
	TaskID     string `json:"task_id"`
	Readmitted bool   `json:"readmitted"`
}

// AttentionItem is one thing only an operator can clear: a parked task, or
// an environmental alert (a fleet node offline, a pooled credential
// erroring, the node's barrier sealed). Mirrors sdlc.AttentionItem.
type AttentionItem struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`

	ProjectID   string `json:"project_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`

	Subject string `json:"subject"`

	IssueRef string `json:"issue_ref,omitempty"`
	IssueURL string `json:"issue_url,omitempty"`

	BlockKind string `json:"block_kind,omitempty"`
	Condition string `json:"condition,omitempty"`

	Question       string `json:"question,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
	Truncated      bool   `json:"truncated,omitempty"`

	// RulingPosted is true when the parked task's issue carries an
	// authorized ruler's comment newer than the park.
	RulingPosted bool `json:"ruling_posted,omitempty"`

	Message  string `json:"message,omitempty"`
	Severity string `json:"severity,omitempty"`

	CreatedAt       int64 `json:"created_at,omitempty"`
	StatusChangedAt int64 `json:"status_changed_at,omitempty"`
	Attempts        int   `json:"attempts,omitempty"`

	Actions []string `json:"actions,omitempty"`
}

// AttentionListRequest is a request to list the tenant's attention items.
type AttentionListRequest struct {
	TenantID string `json:"tenant_id"`

	// ProjectID restricts the listing to one project.
	ProjectID string `json:"project_id,omitempty"`

	// Kind restricts the listing to one AttentionKind (parked_task,
	// agent_offline, account_error, barrier_sealed, project_idle).
	Kind string `json:"kind,omitempty"`

	// Params pages, orders and searches the listing; see package page.
	page.Params
}

// AttentionListResponse is the response from listing attention items.
type AttentionListResponse struct {
	Items []*AttentionItem `json:"items"`
	Count int              `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// AttentionResolveRequest applies an operator's decision to one blocked task.
type AttentionResolveRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`

	// Action is one of approve_recommendation, answer, allow_another_round,
	// close_as_done, readmit, note. Required.
	Action string `json:"action"`

	// Answer carries the operator's free text. Required for "answer" and
	// "note"; optional context for "close_as_done" and optional round scope
	// for "allow_another_round"; ignored otherwise.
	Answer string `json:"answer,omitempty"`
}

// DeleteTaskRequest removes a task and releases its source reservation.
type DeleteTaskRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
}

// DeleteTaskResponse is the response from deleting a task.
type DeleteTaskResponse struct {
	TaskID  string `json:"task_id"`
	Deleted bool   `json:"deleted"`
}

// Backend management types

// BackendInfo represents information about an available backend.
type BackendInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Available    bool   `json:"available"`
	DefaultModel string `json:"default_model,omitempty"`
}

type ListBackendsRequest struct {
	page.Params
}

// ListBackendsResponse is the response from listing backends.
type ListBackendsResponse struct {
	Backends []*BackendInfo `json:"backends"`
	Count    int            `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// Job runner types

// JobStateSpec is one state a runner drives a job through. It mirrors
// orchestration.JobStateSpec minus IdleThreshold, which is the stall
// detector's tuning and is not part of the wire surface.
type JobStateSpec struct {
	// State is the state as it appears on JobStatus.
	State string `json:"state"`

	// Label is how the state is named to a person.
	Label string `json:"label"`

	// SessionBacked says an agent session is doing the work in this state.
	SessionBacked bool `json:"session_backed"`
}

// JobRunnerInfo is one registered runner and the ordered, non-terminal
// states it declares.
type JobRunnerInfo struct {
	// Name is what JobConfig.Runner selects the runner by.
	Name string `json:"name"`

	// States are the runner's states in the order it drives them.
	States []JobStateSpec `json:"states"`
}

type ListRunnersRequest struct {
	page.Params
}

// ListJobRunnersResponse is the response from listing job runners.
type ListJobRunnersResponse struct {
	Runners []*JobRunnerInfo `json:"runners"`
	Count   int              `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// ListBackendModelsRequest is a request to list the models available on a
// specific backend, as seen from a specific agent. AgentID is used as a
// host affinity hint by the remote backend resolver; pass empty or "any"
// to skip host affinity. TenantID defaults to "default" on the server when
// omitted; setting it explicitly is required when the resolver dispatches
// to a multi-tenant fleet daemon.
type ListBackendModelsRequest struct {
	NodeID      string `json:"node_id,omitempty"`
	BackendName string `json:"backend_name"`
	TenantID    string `json:"tenant_id,omitempty"`
}

// ListBackendModelsResponse is the response from listing models for a backend.
// Models is always non-nil; an empty slice means the backend exposes no model
// lister or the agent has no models loaded yet.
type ListBackendModelsResponse struct {
	Backend string   `json:"backend"`
	Models  []string `json:"models"`
	Count   int      `json:"count"`
}

// HealthCheckReport represents the orchestrator health check report.
type HealthCheckReport struct {
	Healthy    bool              `json:"healthy"`
	Status     string            `json:"status"`
	Components map[string]string `json:"components,omitempty"`
	Timestamp  int64             `json:"timestamp"`
}

// Fleet management types

// RegisterFleetNodeRequest is a request to register a new agent.
type RegisterFleetNodeRequest struct {
	TenantID string            `json:"tenant_id"`
	Name     string            `json:"name"`
	Host     string            `json:"host"`
	NodeType string            `json:"node_type"`
	Backend  string            `json:"backend,omitempty"`
	Model    string            `json:"model,omitempty"`
	WorkDir  string            `json:"work_dir,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RegisterFleetNodeResponse is the response from registering an agent.
type RegisterFleetNodeResponse struct {
	Node *FleetNode `json:"node"`
}

// FleetNode represents a registered agent.
type FleetNode struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	NodeType     string            `json:"node_type"`
	Status       string            `json:"status"`
	WorkDir      string            `json:"work_dir,omitempty"`
	LastSeen     int64             `json:"last_seen"`
	RegisteredAt int64             `json:"registered_at"`
	Version      uint64            `json:"version"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// GetFleetNodeRequest is a request to get an agent by ID.
type GetFleetNodeRequest struct {
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id"`
}

// ListFleetNodesRequest is a request to list agents.
type ListFleetNodesRequest struct {
	TenantID string `json:"tenant_id"`
	Host     string `json:"host,omitempty"`
	Status   string `json:"status,omitempty"`
	NodeType string `json:"node_type,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Model    string `json:"model,omitempty"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
}

// ListFleetNodesResponse is the response from listing agents.
type ListFleetNodesResponse struct {
	Nodes []*FleetNode `json:"nodes"`
	Count int          `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// RemoveFleetNodeRequest is a request to remove an agent.
type RemoveFleetNodeRequest struct {
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id"`
}

// RemoveFleetNodeResponse is the response from removing an agent.
type RemoveFleetNodeResponse struct {
	NodeID  string `json:"node_id"`
	Removed bool   `json:"removed"`
}

// FleetNodeHeartbeatRequest is a request to send an agent heartbeat.
type FleetNodeHeartbeatRequest struct {
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id"`
}

// FleetNodeHeartbeatResponse is the response from sending an agent heartbeat.
type FleetNodeHeartbeatResponse struct {
	NodeID    string `json:"node_id"`
	Heartbeat bool   `json:"heartbeat"`
}

// Task decomposition types

// DecomposeTaskRequest is a request to decompose a task into work items.
type DecomposeTaskRequest struct {
	Backend    string   `json:"backend,omitempty"`
	Model      string   `json:"model,omitempty"`
	Task       string   `json:"task"`
	ProjectIDs []string `json:"project_ids,omitempty"`
}

// DecomposeTaskResponse is the response from decomposing a task.
type DecomposeTaskResponse struct {
	WorkItems []*WorkItem `json:"work_items"`
	Timestamp int64       `json:"timestamp"`
}

// WorkItem represents a decomposed unit of work.
type WorkItem struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Priority     int      `json:"priority"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// InferenceRequest is the SDK request for a stateless fleet-routed inference
// call. Maps to POST /api/v1/orchestrator/inference with stream=false or
// stream=true.
type InferenceRequest struct {
	// Backend is the named backend to route to (e.g., "ollama"). Required.
	Backend string `json:"backend"`

	// Model is the model to use. When empty the backend's default is used.
	Model string `json:"model,omitempty"`

	// Prompt is the user prompt text. Required.
	Prompt string `json:"prompt"`

	// SystemPrompt is an optional system message.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// HostAffinity pins the call to a specific fleet node daemon by Name or Host.
	HostAffinity string `json:"host_affinity,omitempty"`

	// HostAffinityStrict, when true, forces hard host_affinity semantics:
	// the server returns NoMatchingFleetNodeError (HTTP 400) if the named host
	// is offline. Defers to the operator-level
	// orchestration.host_affinity.mode default when false.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`

	// Metadata is an opaque string map forwarded to the backend unchanged.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// InferenceContentBlock is one content fragment from a model response.
type InferenceContentBlock struct {
	// Type is the content type ("text", "tool_use", etc.).
	Type string `json:"type"`

	// Text holds the text content when Type is "text".
	Text string `json:"text,omitempty"`
}

// InferenceResponse is the aggregated response from a non-streaming
// Inference call.
type InferenceResponse struct {
	// Content holds all content blocks from the response.
	Content []InferenceContentBlock `json:"content"`

	// Model is the model that produced the response.
	Model string `json:"model,omitempty"`

	// StopReason is the reason the model stopped generating
	// (e.g., "end_turn", "max_tokens").
	StopReason string `json:"stop_reason,omitempty"`
}

// InferenceChunk is one delta frame from a streaming InferenceStream call.
type InferenceChunk struct {
	// Delta is the content emitted in this chunk.
	Delta []InferenceContentBlock `json:"delta"`

	// Model is set on the final chunk.
	Model string `json:"model,omitempty"`

	// StopReason is set on the final chunk.
	StopReason string `json:"stop_reason,omitempty"`

	// Final is true on the last chunk.
	Final bool `json:"final,omitempty"`
}

// =============================================================================
// Workflow Management Types
// =============================================================================

// WorkflowMode mirrors orchestration.WorkflowMode for SDK callers.
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

// AgentRoleSpec describes a single agent role within a workflow definition.
type AgentRoleSpec struct {
	Role         string            `json:"role"`
	Backend      string            `json:"backend"`
	Model        string            `json:"model,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Instructions string            `json:"instructions,omitempty"`

	// PromptAppend is run-time prompt material appended to every session of
	// this role, after the role's system prompt or its Agent's. At most
	// 64 KiB.
	PromptAppend string `json:"prompt_append,omitempty"`
}

// WorkflowSettings holds operational tuning parameters for a workflow.
type WorkflowSettings struct {
	MaxIterations int    `json:"max_iterations,omitempty"`
	Topology      string `json:"topology,omitempty"`
	Isolation     string `json:"isolation,omitempty"`
	PeerMessaging bool   `json:"peer_messaging,omitempty"`
	FileLocking   bool   `json:"file_locking,omitempty"`
	VectorDB      bool   `json:"vector_db,omitempty"`
	Verbosity     string `json:"verbosity,omitempty"`
	GlobalNumCtx  int    `json:"global_num_ctx,omitempty"`

	// Hooks names the registered lifecycle hook set every job of the
	// workflow runs; empty runs none.
	Hooks string `json:"hooks,omitempty"`
}

// WorkflowDefinition describes a complete orchestration workflow including
// agent roles, capabilities, and operational settings.
type WorkflowDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Mode        WorkflowMode     `json:"mode"`
	Agents      []AgentRoleSpec  `json:"agents"`
	Settings    WorkflowSettings `json:"settings,omitempty"`
}

// CreateWorkflowRequest is a request to register a new workflow definition.
type CreateWorkflowRequest struct {
	TenantID   string              `json:"tenant_id"`
	Definition *WorkflowDefinition `json:"definition"`
}

// ListWorkflowsRequest is a request to list workflow definitions.
type ListWorkflowsRequest struct {
	TenantID  string       `json:"tenant_id"`
	ProjectID string       `json:"project_id,omitempty"`
	Mode      WorkflowMode `json:"mode,omitempty"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
}

// ListWorkflowsResponse is the response from listing workflow definitions.
type ListWorkflowsResponse struct {
	Workflows []*WorkflowDefinition `json:"workflows"`
	Count     int                   `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// GetWorkflowRequest is a request to get a workflow definition by name.
type GetWorkflowRequest struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

// UpdateWorkflowRequest is a request to replace an existing workflow definition.
type UpdateWorkflowRequest struct {
	TenantID   string              `json:"tenant_id"`
	Name       string              `json:"name"`
	Definition *WorkflowDefinition `json:"definition"`
}

// DeleteWorkflowRequest is a request to remove a workflow definition by name.
type DeleteWorkflowRequest struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

// Internal request types for parameterless methods.
type healthRequest struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new orchestrator client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// SpawnSession creates a new agent session.
func (c *client) SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	if err := validateSpawnSessionRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SpawnSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SpawnSession", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SpawnSession", "nil response from server")
	}

	result, ok := resp.(*SpawnSessionResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SpawnSession", "invalid response type")
	}

	return result, nil
}

// PromptSession sends a prompt to an existing session.
func (c *client) PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	if err := validatePromptSessionRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.PromptSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "PromptSession", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.PromptSession", "nil response from server")
	}

	result, ok := resp.(*PromptSessionResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.PromptSession", "invalid response type")
	}

	return result, nil
}

// SessionStatus returns the status of a session.
func (c *client) SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
	if err := validateSessionStatusRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SessionStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SessionStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SessionStatus", "nil response from server")
	}

	result, ok := resp.(*SessionStatusResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SessionStatus", "invalid response type")
	}

	return result, nil
}

// CloseSession terminates a session.
func (c *client) CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error) {
	if err := validateCloseSessionRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CloseSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CloseSession", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CloseSession", "nil response from server")
	}

	result, ok := resp.(*CloseSessionResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CloseSession", "invalid response type")
	}

	return result, nil
}

// RotateSessionAccount moves a session onto a different pooled provider account.
func (c *client) RotateSessionAccount(ctx context.Context, req *RotateSessionAccountRequest) (*RotateSessionAccountResponse, error) {
	if err := validateRotateSessionAccountRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.RotateSessionAccount", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RotateSessionAccount", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.RotateSessionAccount", "nil response from server")
	}

	result, ok := resp.(*RotateSessionAccountResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.RotateSessionAccount", "invalid response type")
	}

	return result, nil
}

// ListSessions returns every session, in any state (closed and error
// included), optionally filtered by backend or status.
func (c *client) ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListSessions", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListSessions", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListSessions", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListSessionsResponse{Sessions: []*SessionStatusResponse{}, Count: 0}, nil
	}

	result, ok := resp.(*ListSessionsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListSessions", "invalid response type")
	}

	return result, nil
}

// CreateTask creates a new task.
func (c *client) CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error) {
	if err := validateCreateTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CreateTask", "nil response from server")
	}

	result, ok := resp.(*Task)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CreateTask", "invalid response type")
	}

	return result, nil
}

// GetTask returns a task by ID.
func (c *client) GetTask(ctx context.Context, req *GetTaskRequest) (*Task, error) {
	if err := validateGetTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetTask", "nil response from server")
	}

	result, ok := resp.(*Task)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetTask", "invalid response type")
	}

	return result, nil
}

// ListTasks returns tasks matching the filter criteria.
func (c *client) ListTasks(ctx context.Context, req *ListTasksRequest) (*ListTasksResponse, error) {
	if err := validateListTasksRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListTasks", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListTasks", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListTasksResponse{Tasks: []*Task{}, Count: 0}, nil
	}

	result, ok := resp.(*ListTasksResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListTasks", "invalid response type")
	}

	return result, nil
}

// ClaimTask claims a pending task for execution.
func (c *client) ClaimTask(ctx context.Context, req *ClaimTaskRequest) (*ClaimTaskResponse, error) {
	if err := validateClaimTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ClaimTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ClaimTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.ClaimTask", "nil response from server")
	}

	result, ok := resp.(*ClaimTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ClaimTask", "invalid response type")
	}

	return result, nil
}

// BlockTask parks a task until a person readmits it.
func (c *client) BlockTask(ctx context.Context, req *BlockTaskRequest) (*BlockTaskResponse, error) {
	if err := validateBlockTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.BlockTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "BlockTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.BlockTask", "nil response from server")
	}

	result, ok := resp.(*BlockTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.BlockTask", "invalid response type")
	}

	return result, nil
}

// ReadmitTask returns a blocked task to the pending queue.
func (c *client) ReadmitTask(ctx context.Context, req *ReadmitTaskRequest) (*ReadmitTaskResponse, error) {
	if err := validateReadmitTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ReadmitTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ReadmitTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.ReadmitTask", "nil response from server")
	}

	result, ok := resp.(*ReadmitTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ReadmitTask", "invalid response type")
	}

	return result, nil
}

// AttentionList returns one page of the tenant's attention items.
func (c *client) AttentionList(ctx context.Context, req *AttentionListRequest) (*AttentionListResponse, error) {
	if err := validateAttentionListRequest(req); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.AttentionList", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "AttentionList", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &AttentionListResponse{Items: []*AttentionItem{}, Count: 0}, nil
	}

	result, ok := resp.(*AttentionListResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.AttentionList", "invalid response type")
	}
	return result, nil
}

// AttentionResolve applies an operator's decision to one blocked task.
func (c *client) AttentionResolve(ctx context.Context, req *AttentionResolveRequest) (*AttentionItem, error) {
	if err := validateAttentionResolveRequest(req); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.AttentionResolve", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "AttentionResolve", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.AttentionResolve", "nil response from server")
	}

	result, ok := resp.(*AttentionItem)
	if !ok {
		return nil, errors.NewInternal("orchestrator.AttentionResolve", "invalid response type")
	}
	return result, nil
}

// DeleteTask removes a task and releases its source reservation.
func (c *client) DeleteTask(ctx context.Context, req *DeleteTaskRequest) (*DeleteTaskResponse, error) {
	if err := validateDeleteTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.DeleteTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.DeleteTask", "nil response from server")
	}

	result, ok := resp.(*DeleteTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.DeleteTask", "invalid response type")
	}

	return result, nil
}

// CompleteTask marks a task as completed.
func (c *client) CompleteTask(ctx context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error) {
	if err := validateCompleteTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CompleteTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CompleteTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CompleteTask", "nil response from server")
	}

	result, ok := resp.(*CompleteTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CompleteTask", "invalid response type")
	}

	return result, nil
}

// ListBackends returns all available backends.
func (c *client) ListBackends(ctx context.Context, params ...page.Params) (*ListBackendsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListBackends", "context is required")
	}
	if len(params) > 1 {
		return nil, errors.NewInvalidInput("orchestrator.ListBackends", "at most one page parameter set is allowed")
	}

	req := &ListBackendsRequest{}
	if len(params) == 1 {
		req.Params = params[0]
	}

	if len(params) == 0 {
		return c.listAllBackends(ctx, req)
	}
	return c.listBackendsPage(ctx, req)
}

func (c *client) listBackendsPage(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error) {
	resp, err := c.transport.Request(ctx, serviceName, "ListBackends", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListBackendsResponse{Backends: []*BackendInfo{}, Count: 0}, nil
	}

	result, ok := resp.(*ListBackendsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListBackends", "invalid response type")
	}
	if result.Backends != nil {
		result.Count = len(result.Backends)
	}

	return result, nil
}

func (c *client) listAllBackends(ctx context.Context, req *ListBackendsRequest) (*ListBackendsResponse, error) {
	req.Limit = 100
	result := &ListBackendsResponse{Backends: []*BackendInfo{}}
	for {
		pageResp, err := c.listBackendsPage(ctx, req)
		if err != nil {
			return nil, err
		}
		result.Backends = append(result.Backends, pageResp.Backends...)
		result.Total = pageResp.Total
		result.Limit = pageResp.Limit
		result.Offset = 0
		result.Facets = pageResp.Facets
		if !pageResp.HasMore() {
			result.Count = len(result.Backends)
			return result, nil
		}
		if pageResp.NextOffset <= req.Offset {
			return nil, errors.NewInternal("orchestrator.ListBackends", "next_offset did not advance")
		}
		req.Offset = pageResp.NextOffset
	}
}

// ListRunners returns every registered job runner and the states it declares.
func (c *client) ListRunners(ctx context.Context, params ...page.Params) (*ListJobRunnersResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListRunners", "context is required")
	}
	if len(params) > 1 {
		return nil, errors.NewInvalidInput("orchestrator.ListRunners", "at most one page parameter set is allowed")
	}

	req := &ListRunnersRequest{}
	if len(params) == 1 {
		req.Params = params[0]
	}

	if len(params) == 0 {
		return c.listAllRunners(ctx, req)
	}
	return c.listRunnersPage(ctx, req)
}

func (c *client) listRunnersPage(ctx context.Context, req *ListRunnersRequest) (*ListJobRunnersResponse, error) {
	resp, err := c.transport.Request(ctx, serviceName, "ListRunners", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListJobRunnersResponse{Runners: []*JobRunnerInfo{}, Count: 0}, nil
	}

	result, ok := resp.(*ListJobRunnersResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListRunners", "invalid response type")
	}
	if result.Runners != nil {
		result.Count = len(result.Runners)
	}

	return result, nil
}

func (c *client) listAllRunners(ctx context.Context, req *ListRunnersRequest) (*ListJobRunnersResponse, error) {
	req.Limit = 100
	result := &ListJobRunnersResponse{Runners: []*JobRunnerInfo{}}
	for {
		pageResp, err := c.listRunnersPage(ctx, req)
		if err != nil {
			return nil, err
		}
		result.Runners = append(result.Runners, pageResp.Runners...)
		result.Total = pageResp.Total
		result.Limit = pageResp.Limit
		result.Offset = 0
		result.Facets = pageResp.Facets
		if !pageResp.HasMore() {
			result.Count = len(result.Runners)
			return result, nil
		}
		if pageResp.NextOffset <= req.Offset {
			return nil, errors.NewInternal("orchestrator.ListRunners", "next_offset did not advance")
		}
		req.Offset = pageResp.NextOffset
	}
}

// ListBackendModels returns the model names available on the named backend
// as seen from the specified agent. The transport layer translates this into
// a REST GET against /api/v1/fleet/{agent_id}/backends/{backend_name}/models
// or the equivalent gRPC RPC; both paths return an empty slice (not an error)
// when the agent is unknown or the backend exposes no model lister.
func (c *client) ListBackendModels(ctx context.Context, req *ListBackendModelsRequest) (*ListBackendModelsResponse, error) {
	if err := validateListBackendModelsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListBackendModels", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListBackendModels", req)
	if err != nil {
		return nil, err
	}

	// A nil response is treated as "no models" rather than an error so callers
	// can render an empty list without special-casing missing agents/backends.
	if resp == nil {
		return &ListBackendModelsResponse{Backend: req.BackendName, Models: []string{}, Count: 0}, nil
	}

	result, ok := resp.(*ListBackendModelsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListBackendModels", "invalid response type")
	}

	if result.Models == nil {
		result.Models = []string{}
	}

	return result, nil
}

// Health returns the orchestrator health check report.
func (c *client) Health(ctx context.Context) (*HealthCheckReport, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.Health", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Health", &healthRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.Health", "nil response from server")
	}

	result, ok := resp.(*HealthCheckReport)
	if !ok {
		return nil, errors.NewInternal("orchestrator.Health", "invalid response type")
	}

	return result, nil
}

// RegisterFleetNode registers a new agent with the fleet.
func (c *client) RegisterFleetNode(ctx context.Context, req *RegisterFleetNodeRequest) (*RegisterFleetNodeResponse, error) {
	if err := validateRegisterFleetNodeRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.RegisterFleetNode", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RegisterFleetNode", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.RegisterFleetNode", "nil response from server")
	}

	result, ok := resp.(*RegisterFleetNodeResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.RegisterFleetNode", "invalid response type")
	}

	return result, nil
}

// GetFleetNode returns an agent by ID.
func (c *client) GetFleetNode(ctx context.Context, req *GetFleetNodeRequest) (*FleetNode, error) {
	if err := validateGetFleetNodeRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetFleetNode", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetFleetNode", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetFleetNode", "nil response from server")
	}

	result, ok := resp.(*FleetNode)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetFleetNode", "invalid response type")
	}

	return result, nil
}

// ListFleetNodes returns agents matching the filter criteria.
func (c *client) ListFleetNodes(ctx context.Context, req *ListFleetNodesRequest) (*ListFleetNodesResponse, error) {
	if err := validateListFleetNodesRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListFleetNodes", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListFleetNodes", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListFleetNodesResponse{Nodes: []*FleetNode{}, Count: 0}, nil
	}

	result, ok := resp.(*ListFleetNodesResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListFleetNodes", "invalid response type")
	}

	return result, nil
}

// RemoveFleetNode removes an agent from the fleet.
func (c *client) RemoveFleetNode(ctx context.Context, req *RemoveFleetNodeRequest) (*RemoveFleetNodeResponse, error) {
	if err := validateRemoveFleetNodeRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.RemoveFleetNode", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RemoveFleetNode", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.RemoveFleetNode", "nil response from server")
	}

	result, ok := resp.(*RemoveFleetNodeResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.RemoveFleetNode", "invalid response type")
	}

	return result, nil
}

// FleetNodeHeartbeat sends a heartbeat for an agent.
func (c *client) FleetNodeHeartbeat(ctx context.Context, req *FleetNodeHeartbeatRequest) (*FleetNodeHeartbeatResponse, error) {
	if err := validateFleetNodeHeartbeatRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.FleetNodeHeartbeat", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "FleetNodeHeartbeat", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.FleetNodeHeartbeat", "nil response from server")
	}

	result, ok := resp.(*FleetNodeHeartbeatResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.FleetNodeHeartbeat", "invalid response type")
	}

	return result, nil
}

// DecomposeTask decomposes a task into structured work items using an agent.
func (c *client) DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error) {
	if err := validateDecomposeTaskRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.DecomposeTask", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DecomposeTask", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.DecomposeTask", "nil response from server")
	}

	result, ok := resp.(*DecomposeTaskResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.DecomposeTask", "invalid response type")
	}

	return result, nil
}

// CreateWorkflow registers a new named workflow definition.
func (c *client) CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*WorkflowDefinition, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "tenant ID is required")
	}
	if req.Definition == nil {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "definition is required")
	}
	if req.Definition.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.CreateWorkflow", "workflow name is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateWorkflow", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CreateWorkflow", "nil response from server")
	}
	result, ok := resp.(*WorkflowDefinition)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CreateWorkflow", "invalid response type")
	}
	return result, nil
}

// ListWorkflows returns all registered workflow definitions for a tenant.
func (c *client) ListWorkflows(ctx context.Context, req *ListWorkflowsRequest) (*ListWorkflowsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListWorkflows", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListWorkflows", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.ListWorkflows", "tenant ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListWorkflows", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ListWorkflowsResponse{Workflows: []*WorkflowDefinition{}}, nil
	}
	result, ok := resp.(*ListWorkflowsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListWorkflows", "invalid response type")
	}
	if result.Workflows != nil {
		result.Count = len(result.Workflows)
	}
	return result, nil
}

// GetWorkflow returns a single workflow definition by name.
func (c *client) GetWorkflow(ctx context.Context, req *GetWorkflowRequest) (*WorkflowDefinition, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "tenant ID is required")
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetWorkflow", "workflow name is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetWorkflow", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetWorkflow", "nil response from server")
	}
	result, ok := resp.(*WorkflowDefinition)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetWorkflow", "invalid response type")
	}
	return result, nil
}

// UpdateWorkflow replaces an existing workflow definition by name.
func (c *client) UpdateWorkflow(ctx context.Context, req *UpdateWorkflowRequest) (*WorkflowDefinition, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "tenant ID is required")
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "workflow name is required")
	}
	if req.Definition == nil {
		return nil, errors.NewInvalidInput("orchestrator.UpdateWorkflow", "definition is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateWorkflow", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.UpdateWorkflow", "nil response from server")
	}
	result, ok := resp.(*WorkflowDefinition)
	if !ok {
		return nil, errors.NewInternal("orchestrator.UpdateWorkflow", "invalid response type")
	}
	return result, nil
}

// DeleteWorkflow removes a workflow definition by name.
func (c *client) DeleteWorkflow(ctx context.Context, req *DeleteWorkflowRequest) error {
	if ctx == nil {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "context is required")
	}
	if req == nil {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "tenant ID is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("orchestrator.DeleteWorkflow", "workflow name is required")
	}

	_, err := c.transport.Request(ctx, serviceName, "DeleteWorkflow", req)
	return err
}

// DisableFleetNode administratively disables an agent globally.
func (c *client) DisableFleetNode(ctx context.Context, req *DisableFleetNodeRequest) (*DisableFleetNodeResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.DisableFleetNode", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.DisableFleetNode", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.DisableFleetNode", "tenant ID is required")
	}
	if req.NodeID == "" {
		return nil, errors.NewInvalidInput("orchestrator.DisableFleetNode", "agent ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DisableFleetNode", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.DisableFleetNode", "nil response from server")
	}

	result, ok := resp.(*DisableFleetNodeResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.DisableFleetNode", "invalid response type")
	}
	return result, nil
}

// EnableFleetNode re-activates a previously disabled agent.
func (c *client) EnableFleetNode(ctx context.Context, req *EnableFleetNodeRequest) (*EnableFleetNodeResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.EnableFleetNode", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.EnableFleetNode", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.EnableFleetNode", "tenant ID is required")
	}
	if req.NodeID == "" {
		return nil, errors.NewInvalidInput("orchestrator.EnableFleetNode", "agent ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "EnableFleetNode", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.EnableFleetNode", "nil response from server")
	}

	result, ok := resp.(*EnableFleetNodeResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.EnableFleetNode", "invalid response type")
	}
	return result, nil
}

// SubmitTaskArtifacts stores typed output artifacts for a task.
func (c *client) SubmitTaskArtifacts(ctx context.Context, req *SubmitTaskArtifactsRequest) (*SubmitTaskArtifactsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "tenant ID is required")
	}
	if req.TaskID == "" {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "task ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SubmitTaskArtifacts", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SubmitTaskArtifacts", "nil response from server")
	}

	result, ok := resp.(*SubmitTaskArtifactsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SubmitTaskArtifacts", "invalid response type")
	}
	return result, nil
}

// GetTaskArtifact retrieves a named artifact from a task.
func (c *client) GetTaskArtifact(ctx context.Context, req *GetTaskArtifactRequest) (*TaskArtifact, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "tenant ID is required")
	}
	if req.TaskID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "task ID is required")
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "artifact name is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetTaskArtifact", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetTaskArtifact", "nil response from server")
	}

	result, ok := resp.(*TaskArtifact)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetTaskArtifact", "invalid response type")
	}
	return result, nil
}

// ListTaskArtifacts returns all output artifact metadata for a task.
func (c *client) ListTaskArtifacts(ctx context.Context, req *ListTaskArtifactsRequest) (*ListTaskArtifactsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "tenant ID is required")
	}
	if req.TaskID == "" {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "task ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListTaskArtifacts", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ListTaskArtifactsResponse{TaskID: req.TaskID, Artifacts: []*TaskArtifact{}, Count: 0}, nil
	}

	result, ok := resp.(*ListTaskArtifactsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListTaskArtifacts", "invalid response type")
	}
	if result.Artifacts != nil {
		result.Count = len(result.Artifacts)
	}
	return result, nil
}

// Validation functions

func validateSpawnSessionRequest(req *SpawnSessionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SpawnSession", "request is required")
	}
	named := 0
	for _, field := range []string{req.Backend, req.AgentID, req.PoolID} {
		if field != "" {
			named++
		}
	}
	switch {
	case named == 0:
		return errors.NewInvalidInput("orchestrator.SpawnSession", "an agent, a pool or a backend is required")
	case named > 1:
		return errors.NewInvalidInput("orchestrator.SpawnSession", "name one of an agent, a pool or a backend")
	}
	return nil
}

func validatePromptSessionRequest(req *PromptSessionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.PromptSession", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.PromptSession", "session ID is required")
	}
	if req.Prompt == "" {
		return errors.NewInvalidInput("orchestrator.PromptSession", "prompt is required")
	}
	return nil
}

func validateSessionStatusRequest(req *SessionStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SessionStatus", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.SessionStatus", "session ID is required")
	}
	return nil
}

func validateCloseSessionRequest(req *CloseSessionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CloseSession", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.CloseSession", "session ID is required")
	}
	return nil
}

func validateRotateSessionAccountRequest(req *RotateSessionAccountRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.RotateSessionAccount", "request is required")
	}
	if req.SessionID == "" {
		return errors.NewInvalidInput("orchestrator.RotateSessionAccount", "session ID is required")
	}
	return nil
}

func validateCreateTaskRequest(req *CreateTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CreateTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.CreateTask", "tenant ID is required")
	}
	if req.Subject == "" {
		return errors.NewInvalidInput("orchestrator.CreateTask", "subject is required")
	}
	return nil
}

func validateGetTaskRequest(req *GetTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.GetTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.GetTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.GetTask", "task ID is required")
	}
	return nil
}

func validateListTasksRequest(req *ListTasksRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListTasks", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ListTasks", "tenant ID is required")
	}
	return nil
}

func validateClaimTaskRequest(req *ClaimTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "task ID is required")
	}
	if req.AgentID == "" {
		return errors.NewInvalidInput("orchestrator.ClaimTask", "agent ID is required")
	}
	return nil
}

func validateBlockTaskRequest(req *BlockTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.BlockTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.BlockTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.BlockTask", "task ID is required")
	}
	if req.Kind == "" {
		return errors.NewInvalidInput("orchestrator.BlockTask", "kind is required")
	}
	if req.Reason == "" {
		return errors.NewInvalidInput("orchestrator.BlockTask", "reason is required")
	}
	return nil
}

func validateReadmitTaskRequest(req *ReadmitTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ReadmitTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ReadmitTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.ReadmitTask", "task ID is required")
	}
	return nil
}

func validateAttentionListRequest(req *AttentionListRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.AttentionList", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.AttentionList", "tenant ID is required")
	}
	return nil
}

func validateAttentionResolveRequest(req *AttentionResolveRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.AttentionResolve", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.AttentionResolve", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.AttentionResolve", "task ID is required")
	}
	if req.Action == "" {
		return errors.NewInvalidInput("orchestrator.AttentionResolve", "action is required")
	}
	return nil
}

func validateDeleteTaskRequest(req *DeleteTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.DeleteTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.DeleteTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.DeleteTask", "task ID is required")
	}
	return nil
}

func validateCompleteTaskRequest(req *CompleteTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CompleteTask", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.CompleteTask", "tenant ID is required")
	}
	if req.TaskID == "" {
		return errors.NewInvalidInput("orchestrator.CompleteTask", "task ID is required")
	}
	return nil
}

func validateRegisterFleetNodeRequest(req *RegisterFleetNodeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.RegisterFleetNode", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.RegisterFleetNode", "tenant ID is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("orchestrator.RegisterFleetNode", "name is required")
	}
	if req.Host == "" {
		return errors.NewInvalidInput("orchestrator.RegisterFleetNode", "host is required")
	}
	if req.NodeType == "" {
		return errors.NewInvalidInput("orchestrator.RegisterFleetNode", "agent type is required")
	}
	return nil
}

func validateGetFleetNodeRequest(req *GetFleetNodeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.GetFleetNode", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.GetFleetNode", "tenant ID is required")
	}
	if req.NodeID == "" {
		return errors.NewInvalidInput("orchestrator.GetFleetNode", "agent ID is required")
	}
	return nil
}

func validateListFleetNodesRequest(req *ListFleetNodesRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListFleetNodes", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ListFleetNodes", "tenant ID is required")
	}
	return nil
}

func validateListBackendModelsRequest(req *ListBackendModelsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListBackendModels", "request is required")
	}
	if req.BackendName == "" {
		return errors.NewInvalidInput("orchestrator.ListBackendModels", "backend name is required")
	}
	return nil
}

func validateRemoveFleetNodeRequest(req *RemoveFleetNodeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.RemoveFleetNode", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.RemoveFleetNode", "tenant ID is required")
	}
	if req.NodeID == "" {
		return errors.NewInvalidInput("orchestrator.RemoveFleetNode", "agent ID is required")
	}
	return nil
}

func validateFleetNodeHeartbeatRequest(req *FleetNodeHeartbeatRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.FleetNodeHeartbeat", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.FleetNodeHeartbeat", "tenant ID is required")
	}
	if req.NodeID == "" {
		return errors.NewInvalidInput("orchestrator.FleetNodeHeartbeat", "agent ID is required")
	}
	return nil
}

func validateDecomposeTaskRequest(req *DecomposeTaskRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.DecomposeTask", "request is required")
	}
	if req.Task == "" {
		return errors.NewInvalidInput("orchestrator.DecomposeTask", "task is required")
	}
	return nil
}

// StartJob starts an autonomous orchestration control loop.
func (c *client) SpawnChildJob(ctx context.Context, req *SpawnChildJobRequest) (*SpawnChildJobResponse, error) {
	if err := validateSpawnChildJobRequest(req); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SpawnChildJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SpawnChildJob", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.SpawnChildJob", "nil response from server")
	}
	result, ok := resp.(*SpawnChildJobResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.SpawnChildJob", "invalid response type")
	}
	return result, nil
}

func (c *client) StartJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error) {
	if err := validateStartJobRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.StartJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "StartJob", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.StartJob", "nil response from server")
	}

	result, ok := resp.(*StartJobResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.StartJob", "invalid response type")
	}

	return result, nil
}

// GetJobStatus returns the current status of an orchestration job.
func (c *client) GetJobStatus(ctx context.Context, req *GetJobStatusRequest) (*JobStatus, error) {
	if err := validateGetJobStatusRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetJobStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetJobStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.GetJobStatus", "nil response from server")
	}

	result, ok := resp.(*JobStatus)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetJobStatus", "invalid response type")
	}

	return result, nil
}

// ListJobs returns the tenant's jobs, narrowed to the attempts at one task
// when the request names one.
func (c *client) ListJobs(ctx context.Context, req *ListJobsRequest) (*ListJobsResponse, error) {
	if err := validateListJobsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.ListJobs", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListJobs", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.ListJobs", "nil response from server")
	}

	result, ok := resp.(*ListJobsResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.ListJobs", "invalid response type")
	}

	return result, nil
}

// CancelJob cancels a running orchestration job.
func (c *client) CancelJob(ctx context.Context, req *CancelJobRequest) (*CancelJobResponse, error) {
	if err := validateCancelJobRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.CancelJob", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CancelJob", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("orchestrator.CancelJob", "nil response from server")
	}

	result, ok := resp.(*CancelJobResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.CancelJob", "invalid response type")
	}

	return result, nil
}

// Inference runs a stateless fleet-routed inference call against the named
// backend. Waits for the full aggregated response before returning. For
// interactive streaming use InferenceStream.
func (c *client) Inference(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "request is required")
	}
	if req.Backend == "" {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "backend is required")
	}
	if req.Prompt == "" {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "prompt is required")
	}

	// The transport handler decorates with stream=false and POSTs to
	// /api/v1/orchestrator/inference.
	resp, err := c.transport.Request(ctx, serviceName, "Inference", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("orchestrator.Inference", "nil response from server")
	}
	result, ok := resp.(*InferenceResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.Inference", "invalid response type")
	}
	return result, nil
}

// InferenceStream runs a stateless fleet-routed inference call and returns a
// channel of InferenceChunk values as the backend streams them. The channel
// is closed when the backend finishes or the context is cancelled.
func (c *client) InferenceStream(ctx context.Context, req *InferenceRequest) (<-chan *InferenceChunk, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "request is required")
	}
	if req.Backend == "" {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "backend is required")
	}
	if req.Prompt == "" {
		return nil, errors.NewInvalidInput("orchestrator.InferenceStream", "prompt is required")
	}

	stream, err := c.transport.RequestStream(ctx, serviceName, "InferenceStream", req)
	if err != nil {
		return nil, err
	}

	sub := &inferenceStreamSubscription{
		stream: stream,
		chunks: make(chan *InferenceChunk, 64),
	}
	go sub.receiveLoop(ctx)
	return sub.chunks, nil
}

// inferenceStreamSubscription manages the lifecycle of an inference stream.
type inferenceStreamSubscription struct {
	stream transport.Stream
	chunks chan *InferenceChunk
	closed atomic.Bool
}

// receiveLoop reads chunks from the transport stream and forwards them to the
// channel. Terminates when the stream closes, errors, or the context is done.
func (s *inferenceStreamSubscription) receiveLoop(ctx context.Context) {
	defer func() {
		close(s.chunks)
		s.stream.Close()
		s.closed.Store(true)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := s.stream.Recv()
			if err != nil {
				return
			}
			chunk, ok := msg.(*InferenceChunk)
			if !ok {
				continue
			}
			select {
			case s.chunks <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}
}

// SubscribeJobEvents subscribes to real-time events for an orchestration job.
func (c *client) SubscribeJobEvents(ctx context.Context, req *SubscribeJobEventsRequest) (<-chan *JobEvent, error) {
	if err := validateSubscribeJobEventsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "context is required")
	}

	stream, err := c.transport.RequestStream(ctx, serviceName, "SubscribeJobEvents", req)
	if err != nil {
		return nil, err
	}

	sub := &jobEventSubscription{
		stream: stream,
		events: make(chan *JobEvent, 64),
	}

	go sub.receiveLoop(ctx)

	return sub.events, nil
}

// jobEventSubscription manages the lifecycle of a job event stream.
type jobEventSubscription struct {
	stream transport.Stream
	events chan *JobEvent
	closed atomic.Bool
}

// receiveLoop reads events from the stream and forwards them to the channel.
func (s *jobEventSubscription) receiveLoop(ctx context.Context) {
	defer func() {
		close(s.events)
		s.stream.Close()
		s.closed.Store(true)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := s.stream.Recv()
			if err != nil {
				return
			}

			event, ok := msg.(*JobEvent)
			if !ok {
				continue
			}

			select {
			case s.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}
}

// =============================================================================
// Session Event Subscription + History (Phase 4)
// =============================================================================

// GetSpendReport implements Client.GetSpendReport. One-shot scoped read of the
// consumption ledger; every transport carries it.
func (c *client) GetSpendReport(ctx context.Context, req *GetSpendReportRequest) (*GetSpendReportResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetSpendReport", "context is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetSpendReport", "request is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "GetSpendReport", req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*GetSpendReportResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetSpendReport", "invalid response type")
	}
	return out, nil
}

// GetSessionConversation implements Client.GetSessionConversation. One-shot
// paged read; every transport carries it.
func (c *client) GetSessionConversation(ctx context.Context, req *GetSessionConversationRequest) (*GetSessionConversationResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionConversation", "context is required")
	}
	if req == nil || req.SessionID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionConversation", "session_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "GetSessionConversation", req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*GetSessionConversationResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetSessionConversation", "invalid response type")
	}
	return out, nil
}

// GetSessionHistory implements Client.GetSessionHistory. One-shot REST/gRPC
// call for a page of historical session events.
func (c *client) GetSessionHistory(ctx context.Context, req *GetSessionHistoryRequest) (*GetSessionHistoryResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionHistory", "context is required")
	}
	if req == nil || req.SessionID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionHistory", "session_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "GetSessionHistory", req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*GetSessionHistoryResponse)
	if !ok {
		return nil, errors.NewInternal("orchestrator.GetSessionHistory", "invalid response type")
	}
	return out, nil
}

// SubscribeSessionEvents implements Client.SubscribeSessionEvents. Opens a
// streaming subscription (gRPC stream or SSE depending on transport) and
// returns a buffered channel that receives each event in order.
func (c *client) SubscribeSessionEvents(ctx context.Context, req *SubscribeSessionEventsRequest) (<-chan *SessionEventEnvelope, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("orchestrator.SubscribeSessionEvents", "context is required")
	}
	if req == nil || req.SessionID == "" {
		return nil, errors.NewInvalidInput("orchestrator.SubscribeSessionEvents", "session_id is required")
	}
	stream, err := c.transport.RequestStream(ctx, serviceName, "SubscribeSessionEvents", req)
	if err != nil {
		return nil, err
	}
	sub := &sessionEventSubscription{
		stream: stream,
		events: make(chan *SessionEventEnvelope, 64),
	}
	go sub.receiveLoop(ctx)
	return sub.events, nil
}

// sessionEventSubscription wraps a transport.Stream and forwards each
// received SessionEventEnvelope to its buffered channel. The channel is
// closed when the stream errors or the context is cancelled; consumers
// detect end-of-stream via the closed-channel signal.
type sessionEventSubscription struct {
	stream transport.Stream
	events chan *SessionEventEnvelope
	closed atomic.Bool
}

func (s *sessionEventSubscription) receiveLoop(ctx context.Context) {
	defer func() {
		close(s.events)
		s.stream.Close()
		s.closed.Store(true)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		msg, err := s.stream.Recv()
		if err != nil {
			return
		}
		env, ok := msg.(*SessionEventEnvelope)
		if !ok {
			continue
		}
		select {
		case s.events <- env:
		case <-ctx.Done():
			return
		}
	}
}

// Validation functions for job operations.

func validateSpawnChildJobRequest(req *SpawnChildJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SpawnChildJob", "request is required")
	}
	if req.ParentJobID == "" {
		return errors.NewInvalidInput("orchestrator.SpawnChildJob", "parent_job_id is required")
	}
	return nil
}

func validateStartJobRequest(req *StartJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.StartJob", "request is required")
	}
	if req.Description == "" {
		return errors.NewInvalidInput("orchestrator.StartJob", "description is required")
	}
	return nil
}

func validateGetJobStatusRequest(req *GetJobStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.GetJobStatus", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.GetJobStatus", "tenant ID is required")
	}
	if req.JobID == "" {
		return errors.NewInvalidInput("orchestrator.GetJobStatus", "job ID is required")
	}
	return nil
}

func validateListJobsRequest(req *ListJobsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.ListJobs", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.ListJobs", "tenant ID is required")
	}
	return nil
}

func validateCancelJobRequest(req *CancelJobRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.CancelJob", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.CancelJob", "tenant ID is required")
	}
	if req.JobID == "" {
		return errors.NewInvalidInput("orchestrator.CancelJob", "job ID is required")
	}
	return nil
}

func validateSubscribeJobEventsRequest(req *SubscribeJobEventsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "tenant ID is required")
	}
	if req.JobID == "" {
		return errors.NewInvalidInput("orchestrator.SubscribeJobEvents", "job ID is required")
	}
	return nil
}

// =============================================================================
// Job Orchestration Types
// =============================================================================

// StartJobRequest starts an autonomous orchestration job.
type StartJobRequest struct {
	TenantID    string `json:"tenant_id"`
	Description string `json:"description"`
	// Lead is the Agent or pool the job's lead runs as. Zero takes the
	// project's default lead.
	Lead agents.AgentRef `json:"lead,omitzero"`
	// LeadAgentID pins the job's lead to a specific registered fleet node.
	LeadAgentID   string `json:"lead_agent_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	MaxIterations int    `json:"max_iterations,omitempty"`
	PollInterval  string `json:"poll_interval,omitempty"`
	WorkflowName  string `json:"workflow_name,omitempty"`
	WorkflowFile  string `json:"workflow_file,omitempty"`

	// Runner names the JobRunner that drives the job's states. Empty selects
	// the engine's own "workflow" runner: a lead agent decomposing the
	// description into a DAG of work items.
	Runner string `json:"runner,omitempty"`

	// PromptAppend is run-time prompt material appended to every session
	// the job spawns, the lead's included. At most 64 KiB.
	PromptAppend string `json:"prompt_append,omitempty"`

	// TaskID binds the job to the task it is an attempt at; Attempt is the
	// 1-based ordinal of that attempt.
	TaskID  string `json:"task_id,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
}

// StartJobResponse contains the result of starting an orchestration job.
type StartJobResponse struct {
	JobID     string `json:"job_id"`
	Timestamp int64  `json:"timestamp"`
}

// GetJobStatusRequest is a request to get the status of a job.
type GetJobStatusRequest struct {
	TenantID string `json:"tenant_id"`
	JobID    string `json:"job_id"`
}

// JobStatus reports the current state of an orchestration job.
type JobStatus struct {
	JobID       string          `json:"job_id"`
	State       string          `json:"state"`
	Iteration   int             `json:"iteration"`
	TaskCount   int             `json:"task_count"`
	Completed   int             `json:"completed"`
	Failed      int             `json:"failed"`
	Description string          `json:"description"`
	StartedAt   int64           `json:"started_at"`
	UpdatedAt   int64           `json:"updated_at"`
	Error       string          `json:"error,omitempty"`
	Events      []JobAuditEvent `json:"events,omitempty"`

	// TaskID and Attempt bind the job to the task it is an attempt at.
	TaskID  string `json:"task_id,omitempty"`
	Attempt int    `json:"attempt,omitempty"`

	// Node names the daemon running the job's control loop.
	Node string `json:"node,omitempty"`

	// HeartbeatAt is the nanosecond Unix instant the loop last proved alive.
	HeartbeatAt int64 `json:"heartbeat_at,omitempty"`

	// FailureClass classifies a failed job's cause.
	FailureClass string `json:"failure_class,omitempty"`

	// LeadSessionID is the lead agent's session; SessionIDs lists every
	// session the job holds, lead first.
	LeadSessionID string   `json:"lead_session_id,omitempty"`
	SessionIDs    []string `json:"session_ids,omitempty"`

	// Stage is the last lifecycle moment the job passed through (prepare,
	// reviewable, complete, discard, blocked); empty when the workflow
	// runs no hooks.
	Stage string `json:"stage,omitempty"`

	// Runner names the JobRunner that drove the job's states.
	Runner string `json:"runner,omitempty"`

	// PromptAppend is the run-time prompt material the job appended to
	// every session it spawned: the request's, or its task's.
	PromptAppend string `json:"prompt_append,omitempty"`

	// --- Provenance: who ran the attempt, and what it left behind ---------
	//
	// Mirrors pkg/orchestration/job_provenance.go. AgentID and Stages are
	// recorded by the run; the rest are the task's facts, which the server
	// lays over the status when GetJobStatus reads it. A job read from
	// ListJobs carries none of them.

	// AgentID is the FLEET agent the work was dispatched to, which is not
	// Node: that is the daemon whose control loop drove the job.
	AgentID string `json:"agent_id,omitempty"`

	// Stages is every lifecycle moment the job entered, in order.
	Stages []JobStageRecord `json:"stages,omitempty"`

	// Attempts is every attempt at the job's task, oldest first, with the
	// one this status belongs to marked Current.
	Attempts []JobAttemptRecord `json:"attempts,omitempty"`

	// AttemptBudget is the number of charged attempts the task allows.
	AttemptBudget int `json:"attempt_budget,omitempty"`

	// TerminalReason is the typed reason the task stopped, from the closed
	// vocabulary in pkg/orchestration/task_conclusion.go.
	TerminalReason string `json:"terminal_reason,omitempty"`

	// ReadmitAllowed says whether a person may return the task to the queue.
	ReadmitAllowed bool `json:"readmit_allowed,omitempty"`

	// Source is where the job's TASK came from — the forge issue it was
	// adopted from, with the ref (`owner/repo#62`) and the URL the origin
	// recorded. It is the task's own TaskSource, so a consumer names a job's
	// issue with the code it already uses for a task's. Unlike the rest of
	// this block it is also on the LISTING, because a paged jobs view holds
	// no tasks to resolve it from (go-quicbaas#976).
	Source *TaskSource `json:"source,omitempty"`

	// Delivery is the git side of THIS ATTEMPT — branch, head, pull request
	// and review rounds — read off the artifacts the runner published.
	Delivery *JobDelivery `json:"delivery,omitempty"`

	// Verification is the last validator pipeline's results.
	Verification []JobVerificationResult `json:"verification,omitempty"`
}

// JobStageRecord is one lifecycle moment the job entered.
type JobStageRecord struct {
	Stage string `json:"stage"`

	// EnteredAt is the Unix-nanosecond instant the moment was entered.
	EnteredAt int64 `json:"entered_at"`
}

// JobAttemptRecord is one attempt at the job's task.
type JobAttemptRecord struct {
	Attempt int    `json:"attempt"`
	JobID   string `json:"job_id"`

	// Current marks the attempt the status being read belongs to.
	Current bool `json:"current,omitempty"`
}

// JobReviewRound is one reviewer's verdict on one head of the job's branch.
type JobReviewRound struct {
	Round    int    `json:"round"`
	Reviewer string `json:"reviewer"`

	// Verdict is the reviewing module's own vocabulary: approve,
	// request_changes, error.
	Verdict string `json:"verdict"`

	HeadSHA  string `json:"head_sha,omitempty"`
	Findings string `json:"findings,omitempty"`

	// SubmittedAt is an RFC 3339 instant.
	SubmittedAt string `json:"submitted_at,omitempty"`
}

// JobDelivery is the git side of one attempt at the job's task.
type JobDelivery struct {
	Branch            string           `json:"branch,omitempty"`
	HeadSHA           string           `json:"head_sha,omitempty"`
	PullRequestNumber int              `json:"pull_request_number,omitempty"`
	PullRequestURL    string           `json:"pull_request_url,omitempty"`
	PullRequestState  string           `json:"pull_request_state,omitempty"`
	Reviews           []JobReviewRound `json:"reviews,omitempty"`
}

// JobVerificationResult is one verification command's outcome.
type JobVerificationResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
	Details string `json:"details,omitempty"`
}

// JobAuditEvent records a single event in the job's complete audit trail.
type JobAuditEvent struct {
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
	Details   string `json:"details"`
}

// CancelJobRequest is a request to cancel a running job.
type CancelJobRequest struct {
	TenantID string `json:"tenant_id"`
	JobID    string `json:"job_id"`
}

// CancelJobResponse contains the result of cancelling a job.
type CancelJobResponse struct {
	JobID     string `json:"job_id"`
	Cancelled bool   `json:"cancelled"`
}

// ListJobsRequest is a request to list a tenant's jobs.
type ListJobsRequest struct {
	TenantID string `json:"tenant_id"`

	// TaskID narrows the listing to the attempts at one task. Empty lists
	// every job the tenant owns.
	TaskID string `json:"task_id,omitempty"`

	// Runner narrows the listing to the jobs one runner drove — "sdlc",
	// "triage", "review".
	Runner string `json:"runner,omitempty"`

	// ProjectID narrows the listing to one project's jobs.
	ProjectID string `json:"project_id,omitempty"`

	// Statuses narrows the listing to jobs in any of these states.
	Statuses []string `json:"statuses,omitempty"`

	// JobIDs narrows the listing to the named jobs, so a caller holding a
	// page of sessions or tasks resolves the jobs they name in ONE call
	// instead of one request per row. Empty lists every job.
	JobIDs []string `json:"job_ids,omitempty"`

	// AgentID narrows the listing to the jobs dispatched to one fleet node.
	// A job that ran in-process carries none and matches no agent name.
	AgentID string `json:"agent_id,omitempty"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
}

// ListJobsResponse contains one page of a tenant's jobs.
type ListJobsResponse struct {
	Jobs []JobSummary `json:"jobs"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}

// JobSummary provides a summary of an orchestration job.
type JobSummary struct {
	JobID       string `json:"job_id"`
	State       string `json:"state"`
	Iteration   int    `json:"iteration"`
	TaskCount   int    `json:"task_count"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
	Description string `json:"description"`
	StartedAt   int64  `json:"started_at"`
	UpdatedAt   int64  `json:"updated_at"`
	Error       string `json:"error,omitempty"`
	EventCount  int    `json:"event_count"`
	TaskID      string `json:"task_id,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`

	// Source is the forge issue behind the job's task, filled by the server
	// for the page it answers. A jobs view is paged and holds no tasks of its
	// own, so without this the row cannot name its issue (go-quicbaas#976).
	Source *TaskSource `json:"source,omitempty"`
}

// SubscribeJobEventsRequest is a request to subscribe to real-time job events.
type SubscribeJobEventsRequest struct {
	TenantID string `json:"tenant_id"`
	JobID    string `json:"job_id"`
}

// JobEvent represents a real-time event from a running orchestration job.
type JobEvent struct {
	JobID     string            `json:"job_id"`
	Type      string            `json:"type"`
	State     string            `json:"state"`
	Message   string            `json:"message"`
	Iteration int               `json:"iteration"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
}

// TaskSpec is an inline task definition supplied when spawning a child job.
type TaskSpec struct {
	Title                string   `json:"title,omitempty"`
	Description          string   `json:"description,omitempty"`
	Dependencies         []string `json:"dependencies,omitempty"`
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
}

// SpawnChildJobRequest fans out a child job from the calling lead agent.
type SpawnChildJobRequest struct {
	// ParentJobID is the job whose lead agent is initiating the spawn. Required.
	ParentJobID string `json:"parent_job_id"`
	// CallerAgentID is the fleet node ID of the calling agent. Required.
	CallerAgentID string `json:"caller_agent_id,omitempty"`
	// WorkflowID optionally names an existing workflow definition.
	WorkflowID string `json:"workflow_id,omitempty"`
	// ProjectID scopes the child job to a registered project.
	ProjectID string `json:"project_id,omitempty"`
	// Tasks provides inline task specs when no workflow is referenced.
	Tasks []*TaskSpec `json:"tasks,omitempty"`
	// HostAffinity pins the child to a specific fleet node daemon.
	HostAffinity string `json:"host_affinity,omitempty"`
	// GpuRequired restricts dispatch to GPU-capable daemons.
	GpuRequired bool `json:"gpu_required,omitempty"`
	// HostAffinityStrict treats HostAffinity as a hard constraint.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`
}

// SpawnChildJobResponse is returned after the child job has been accepted.
type SpawnChildJobResponse struct {
	ChildJobID string `json:"child_job_id"`
}

// =============================================================================
// Agent Admin Types
// =============================================================================

// DisableFleetNodeRequest is a request to administratively disable an agent.
type DisableFleetNodeRequest struct {
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id"`
}

// DisableFleetNodeResponse is returned after an agent has been disabled.
type DisableFleetNodeResponse struct {
	NodeID     string    `json:"node_id"`
	Disabled   bool      `json:"disabled"`
	DisabledAt time.Time `json:"disabled_at"`
	Timestamp  time.Time `json:"timestamp"`
}

// EnableFleetNodeRequest is a request to re-enable a previously disabled agent.
type EnableFleetNodeRequest struct {
	TenantID string `json:"tenant_id"`
	NodeID   string `json:"node_id"`
}

// EnableFleetNodeResponse is returned after an agent has been re-enabled.
type EnableFleetNodeResponse struct {
	NodeID    string    `json:"node_id"`
	Enabled   bool      `json:"enabled"`
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Task Artifact Types
// =============================================================================

// TaskArtifact represents a typed output artifact produced by a task.
type TaskArtifact struct {
	// Kind classifies where the content lives: "inline", "file", "commit",
	// "kv_key" or "url". It decides which of Content and Ref is meaningful.
	Kind string `json:"kind,omitempty"`

	// Name is the unique artifact name within the task.
	Name string `json:"name"`

	// MIMEType describes the content type (e.g. "text/plain").
	MIMEType string `json:"mime_type,omitempty"`

	// Size is the total byte length of the artifact.
	Size int64 `json:"size,omitempty"`

	// Content holds inline bytes when Kind is "inline".
	Content []byte `json:"content,omitempty"`

	// Ref locates the artifact for every non-inline kind.
	Ref *TaskArtifactRef `json:"ref,omitempty"`

	// Metadata holds the key-value pairs the producer attached. The SDLC
	// module puts the issue, the comment id and the outcome here, which is
	// what makes a task's history readable without fetching each artifact.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// TaskArtifactRef locates a non-inline artifact.
//
// IT IS AN OBJECT ON THE WIRE, AND WAS DECLARED HERE AS A STRING. Every
// transport but the embedded one serves the orchestration Artifact verbatim,
// so decoding a task that carried any artifact failed outright with
// "cannot unmarshal object into Go struct field TaskArtifact.output_artifacts.ref
// of type string" — and since the SDLC module writes an outcome_comment
// artifact on every issue it touches, that was every task that had ever run.
// `quicbaas orchestrator task get` could not read a single real task.
type TaskArtifactRef struct {
	// AgentID routes fetch requests for file-kind artifacts.
	AgentID string `json:"agent_id,omitempty"`

	// Path is the filesystem path, KV key, or full URL depending on Kind.
	Path string `json:"path,omitempty"`

	// SHA is the content hash for file/kv_key kinds, or the git commit SHA
	// for commit-kind artifacts.
	SHA string `json:"sha,omitempty"`

	// Branch is the git branch for commit-kind artifacts.
	Branch string `json:"branch,omitempty"`
}

// SubmitTaskArtifactsRequest stores typed artifacts for a task.
type SubmitTaskArtifactsRequest struct {
	TenantID  string          `json:"tenant_id"`
	TaskID    string          `json:"task_id"`
	Artifacts []*TaskArtifact `json:"artifacts"`
}

// SubmitTaskArtifactsResponse confirms how many artifacts were stored.
type SubmitTaskArtifactsResponse struct {
	TaskID string `json:"task_id"`
	Stored int    `json:"stored"`
}

// GetTaskArtifactRequest retrieves a named artifact from a task.
type GetTaskArtifactRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`
	Name     string `json:"name"`
}

// ListTaskArtifactsRequest lists all artifacts for a task.
type ListTaskArtifactsRequest struct {
	TenantID string `json:"tenant_id"`
	TaskID   string `json:"task_id"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
}

// ListTaskArtifactsResponse carries artifact metadata (no inline Data resolved).
type ListTaskArtifactsResponse struct {
	TaskID    string          `json:"task_id"`
	Artifacts []*TaskArtifact `json:"artifacts"`
	Count     int             `json:"count"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
}
