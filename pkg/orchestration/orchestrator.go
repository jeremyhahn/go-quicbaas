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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// agentServiceImpl implements the AgentService interface.
type agentServiceImpl struct {
	store            AgentStore
	backends         sync.Map // map[string]AgentBackend
	projects         sync.Map // map[string]*RegisteredProject
	sessions         sync.Map // map[string]*sessionRecord
	tenantGuard      *TenantGuard
	taskStore        TaskStore          // distributed task store
	channelServer    *ChannelServer     // MCP channel for Claude Code events
	containerRuntime ContainerRuntime   // OCI container runtime
	messaging        MessagingPublisher // pub/sub for cross-host coordination
	leadAgent        *LeadAgentConfig   // optional lead agent for auto-orchestration
	leadSessionID    string             // session ID for the lead agent
	agentRegistry    AgentRegistry      // fleet agent registry
	jobs             sync.Map           // map[string]*runningJob
	kvStore          interfaces.KVStore // KV store for job persistence
	workflowLoader   *WorkflowLoader    // workflow loader for job resumption
	logger           interfaces.Logger
	eventSubscribers sync.Map // map[string]*subscriberList keyed by tenantID/jobID
	sandboxPolicy    SandboxPolicy
	trustPolicy      TrustPolicy           // integrity floor every spawn resolves against
	tenantPolicies   TenantPolicyProvider  // optional resolver for per-tenant overrides
	remoteResolver   RemoteBackendResolver // optional: routes daemon-kind registrations to RemoteACPBackend
	sessionLog       SessionEventLog       // optional: append-only per-session event log for attach/replay
	projectAuthz     *ProjectAuthz         // optional: enforces per-project agent role matrix
	costTracker      CostTracker           // optional: per-tenant/account/project/job token counters
	ledger           ConsumptionLedger     // optional: the append-only spend authority every cap is decided from
	lifecyclePolicy  JobLifecyclePolicy    // operator command allowlist + per-stage bounds handed to every control loop
	projectStore     ProjectStore          // optional: durable project records for managed workspaces
	mountPolicy      *MountPolicy          // optional: bind-mount policy; nil denies every host mount

	// accountStore is the pooled provider account metadata store. Optional:
	// without it rotation still selects a replacement, but the limit hold on
	// the account it left is not durable. See SetAccountStore.
	accountStore AccountStore

	// accountLeases withdraws a pooled credential's DELIVERY GRANT when the
	// session that held it ends, and when a rotation supersedes it. Optional:
	// without it a conclusion still tears the session down, but the grant over
	// the credential the session was handed is never revoked. See
	// SetAccountCredentialLeaser and account_lease.go.
	accountLeases AccountCredentialLeaser

	// limitClassifier decides whether a failed turn was a provider limit.
	// Nil selects the built-in heuristic (see NewAccountLimitClassifier).
	limitClassifier AccountLimitClassifier

	// accountLimitBackoff is how long an account is held out of the pool when
	// the provider named no retry time. Zero selects
	// DefaultAccountLimitBackoff.
	accountLimitBackoff time.Duration

	// resumeMu serializes the reconstitution of one session so two prompts
	// arriving together cannot each start a replacement agent process for it.
	resumeMu keyedMutex

	// transcriptReplayBudget bounds, in characters, the transcript primed into
	// a resumed agent. Zero selects DefaultReplayBudget.
	transcriptReplayBudget int

	// turnsInFlight counts asynchronous turns accepted by PromptSessionAsync
	// that have not yet written their terminal event. Shutdown waits on it so
	// a transcript never stops mid-turn.
	turnsInFlight sync.WaitGroup

	// jobsInFlight counts the job goroutines launchJob has started and that
	// have not yet concluded. Shutdown waits on it so the stores a control
	// loop writes to do not go away underneath it; see shutdown.go.
	jobsInFlight sync.WaitGroup

	// draining is set by Shutdown and never cleared: the service refuses to
	// start work it has no time left to do.
	draining atomic.Bool

	// node names this daemon on every lease and job status it writes; see
	// SetNode. Empty until the host names it.
	node string

	// jobLeases decides which node runs a job; leaseTTL is the heartbeat age
	// past which a lease is stale. Never nil: NewAgentService installs a
	// single-process store and a clustered host replaces it. See
	// job_admission.go.
	jobLeases JobLeaseStore
	leaseTTL  time.Duration

	// idleThresholds, reaper and stalls are the maintenance sweep, built
	// once on the first Sweep from the wiring in place then.
	idleThresholds JobIdleThresholds
	reaper         *JobReaper
	stalls         *JobStallDetector
	sweepOnce      sync.Once
	sweepErr       error

	// tenants enumerates the tenants a recovery or maintenance pass must
	// visit, because the durable store routes by the context's tenant and a
	// pass carries none of its own. Nil on a service over a single, unrouted
	// store. See SetTenantLister.
	tenants TenantLister

	// agentHeartbeatTimeout is how long a registered agent may stay silent
	// before the session sweep reads its silence as absence. Zero selects
	// DefaultAgentHeartbeatTimeout; see SetAgentHeartbeatTimeout.
	agentHeartbeatTimeout time.Duration

	// reconcileMu serializes the DURABLE-ONLY conclusion of one session — the
	// path taken when no live record exists to arbitrate on (see
	// session_reconcile.go). The live path has its own compare-and-swap; this
	// is the same single-conclusion guarantee for a record whose process died
	// with another daemon, so two sweeps, or a sweep and an operator's close,
	// cannot both write an ending.
	reconcileMu keyedMutex
}

// TenantPolicyProvider supplies per-tenant policy overrides that can tighten
// (but never loosen) the server-level SandboxPolicy. Implementations are
// typically backed by the IAM subsystem.
type TenantPolicyProvider interface {
	// SandboxPolicy returns the tenant's sandbox policy override if one is
	// configured, or nil to defer to the server-level policy.
	SandboxPolicy(tenantID string) *SandboxPolicy
}

// TenantTrustPolicyProvider is the trust-axis counterpart of
// TenantPolicyProvider, implemented additionally by providers that also carry
// a per-tenant integrity floor. It is a separate, optional interface so a
// provider that only knows about the sandbox axis keeps satisfying
// TenantPolicyProvider unchanged; the orchestrator type-asserts for it and
// falls back to the server-level TrustPolicy when it is absent.
//
// Like the sandbox override, a trust override may only TIGHTEN the server
// policy — see ResolveTrustLevel.
type TenantTrustPolicyProvider interface {
	// TrustPolicy returns the tenant's trust policy override if one is
	// configured, or nil to defer to the server-level policy.
	TrustPolicy(tenantID string) *TrustPolicy
}

// RemoteBackendResolver lets the BackendDispatcher route a backend name to a
// remote daemon (e.g., RemoteACPBackend) before falling back to locally
// registered backends. Returning (backend, true) wins; (nil, false) defers
// to the local lookup. Implementations are typically driven by the
// AgentRegistry, matching registrations where kind=daemon and backend=name.
//
// Routing hints beyond backend+projectID (e.g. host affinity) are threaded
// via the context using WithHostAffinity / HostAffinityFromContext so this
// function signature stays stable as we add more selectors.
type RemoteBackendResolver func(ctx context.Context, backend, projectID string) (AgentBackend, bool)

// hostAffinityKey is the unexported context key used to thread a requested
// host affinity from SpawnSession down into a RemoteBackendResolver.
type hostAffinityKey struct{}

// WithHostAffinity returns a context carrying the given host affinity hint.
// Resolvers use HostAffinityFromContext to read it back.
func WithHostAffinity(ctx context.Context, host string) context.Context {
	if host == "" {
		return ctx
	}
	return context.WithValue(ctx, hostAffinityKey{}, host)
}

// HostAffinityFromContext returns the host affinity hint, or "" when none
// was set. Safe to call on any context.
func HostAffinityFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(hostAffinityKey{}).(string); ok {
		return v
	}
	return ""
}

// gpuRequiredKey is the unexported context key used to thread a GPU
// requirement from Spawn / Infer requests into the RemoteBackendResolver.
type gpuRequiredKey struct{}

// WithGpuRequired returns a context that signals the resolver must restrict
// candidate daemons to those advertising "gpu" in their Capabilities slice.
func WithGpuRequired(ctx context.Context) context.Context {
	return context.WithValue(ctx, gpuRequiredKey{}, true)
}

// GpuRequiredFromContext returns true when the context carries a GPU
// requirement set via WithGpuRequired. Safe to call on any context.
func GpuRequiredFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(gpuRequiredKey{}).(bool)
	return v
}

// runningJob tracks a running job and its cancellation function.
type runningJob struct {
	run    *JobRun
	cancel context.CancelFunc
}

// eventSubscriber holds a subscriber channel and its done signal.
type eventSubscriber struct {
	ch   chan *JobEvent
	done chan struct{}
}

// subscriberList wraps a slice of event subscribers. Using a pointer to this
// struct as the sync.Map value enables CompareAndSwap since pointers are comparable.
type subscriberList struct {
	subs []*eventSubscriber
}

// sessionRecord tracks which backend owns a session in memory.
// This provides fast in-memory lookup for active sessions while the
// AgentStore handles durable persistence.
type sessionRecord struct {
	Backend     string
	ProjectID   string
	Model       string
	TenantID    string
	ContainerID string
	CreatedAt   int64

	// JobID mirrors the durable record's: it is the job this session works
	// for, and the conclusion needs it to tell that job the session has ended
	// (session_reconcile.go). Mirrored rather than read back because the
	// ending is on the hot path of every close and every exit, and a KV read
	// per conclusion to recover a field the spawn already knew would be a
	// round trip for nothing. Empty for a session no job spawned.
	JobID string

	// accountID is the pooled provider account this session's agent process
	// authenticates as, mirrored from the durable SessionRecord so the turn
	// path can attribute token usage to it without a KV read on every turn.
	// Empty when the session did not use the account pool.
	//
	// Atomic because rotation moves it while turns are reading it to attribute
	// their usage: a torn read here would charge tokens to a half-written
	// account ID, which is worse than charging them to either account. Access
	// through AccountID / SetAccountID.
	accountID atomic.Pointer[string]

	// credentialLeaseIDs mirrors the durable record's delivery grants, so the
	// LIVE conclusion path can withdraw them without a KV read on the hot path
	// of every close and every process exit.
	//
	// Atomic because a rotation appends to it while a conclusion may be reading
	// it: the slice is REPLACED rather than appended in place, so a reader that
	// loaded the old pointer sees a coherent older set instead of a slice being
	// grown underneath it. The absent case (nil pointer) is a session with no
	// grants, which is the common one.
	credentialLeaseIDs atomic.Pointer[[]string]

	// Persistent mirrors the durable record: the session holds a long-running
	// agent process carrying conversation context across turns.
	Persistent bool

	// resumable marks a record reconstituted from the durable store at boot
	// whose agent process is gone. The session is addressable — Status and
	// Prompt resolve it — but the process is recreated on the next prompt, not
	// at boot. It is cleared once the backend has reconstituted the session.
	//
	// Atomic because Status and Prompt read it concurrently while a resume
	// flips it, and the record is shared by pointer through s.sessions.
	resumable atomic.Bool

	// conclusionMu serializes the ownership handoff between `concluded` and
	// `conclusion`, so an owner elected by the swap is always PUBLISHED by the
	// time any loser can observe the swap. Without it the two are separate
	// atomics with a window between them, and a loser landing in that window
	// sees an owner that exists but cannot be followed.
	//
	// The handoff is deliberately tiny: it is held only while electing,
	// publishing or releasing an owner — never across a backend teardown or a
	// durable write, which is why it cannot serialize the slow paths.
	conclusionMu sync.Mutex

	// concluded and persisted are the two facts a session's ending consists of,
	// kept apart because conflating them made a failed durable write
	// permanently unrecoverable.
	//
	// concluded says the TEARDOWN happened — the process was released, its
	// provisioned material freed. It is the compare-and-swap that makes
	// conclusion single-owner: an operator close, the supervisor's exit
	// observer and the spawn-time reconcile contend on it and exactly one wins,
	// and the winner is the only caller that may tear anything down.
	//
	// persisted says the terminal state was DURABLY RECORDED. It is a separate
	// swap because the store write can fail after a successful teardown, and a
	// single flag would then report the session concluded while the record
	// still said ACTIVE — resurrecting a dead session as resumable across a
	// restart. Every later caller that loses `concluded` retries the PERSIST
	// (never the teardown) until this one is set. See session_conclusion.go.
	concluded atomic.Bool
	persisted atomic.Bool

	// conclusion is what the winner of `concluded` publishes about the ending
	// it owns: the reason, and a channel closed once its teardown ATTEMPT has
	// finished, whether it succeeded or not.
	//
	// WITHOUT IT A LOSER PERSISTS BLIND. The bare CAS says somebody owns the
	// conclusion; it says nothing about whether they have finished tearing down
	// or whether the teardown worked. A loser that went straight to the durable
	// write could mark the record CLOSED, emit the terminal event under its OWN
	// reason, and evict the live record while the winner was still inside
	// backend.Close — and if that Close then failed, the winner's release of
	// ownership would have nothing left to retry with and a durable record
	// falsely saying closed. Nil while no conclusion is in flight.
	conclusion atomic.Pointer[sessionConclusion]

	// logMu serializes the session's log transition — read metadata, close it,
	// append the ending — among conclusion callers.
	//
	// The metadata is what gates every other writer, so the decision to close
	// it and the ending that follows have to be one step: two callers that both
	// read ACTIVE would both append a terminal event. A per-session critical
	// section is the smallest thing that makes the read-decide-write atomic
	// without widening a lock inside the log implementations.
	logMu sync.Mutex

	// HostAffinity records the daemon host the session was actually spawned
	// on. Spawn resolves a backend via the RemoteBackendResolver (which applies
	// host-affinity, tenant, and GPU filtering) and the live session lives
	// in-memory on that one instance. Prompt / Status / Close MUST re-resolve
	// to the same instance; otherwise an empty or different host-affinity hint
	// on the follow-up call can route to a different daemon (or the local
	// fallback backend) where the session does not exist, surfacing as
	// "acp: session not found". Replaying this stored host on the follow-up
	// context makes re-resolution deterministic. Empty when the session ran on
	// a local (non-remote) backend, in which case no replay is needed.
	HostAffinity string

	// BackendLocal is true when Spawn resolved to the local in-process backend
	// (the RemoteBackendResolver declined the request). The live session lives
	// only in that local backend's in-memory registry. Prompt / Status / Close
	// MUST resolve to the SAME local backend; without this marker a follow-up
	// call would re-run the resolver, and a remote daemon that registered AFTER
	// Spawn (registration-timing) would now claim the backend name, routing the
	// follow-up to a different, empty instance — surfacing as
	// "acp: session not found". When true, pinSessionBackend forces local
	// resolution via the localBackendPin context marker. HostAffinity is empty
	// in this case because local backends are not host-bound.
	BackendLocal bool
}

// AccountID returns the pooled provider account currently serving the session,
// or "" when it uses none.
func (r *sessionRecord) AccountID() string {
	if id := r.accountID.Load(); id != nil {
		return *id
	}
	return ""
}

// SetAccountID records the pooled provider account now serving the session.
func (r *sessionRecord) SetAccountID(accountID string) {
	r.accountID.Store(&accountID)
}

// CredentialLeaseIDs returns the delivery grants recorded on the live session.
func (r *sessionRecord) CredentialLeaseIDs() []string {
	if ids := r.credentialLeaseIDs.Load(); ids != nil {
		return *ids
	}
	return nil
}

// SetCredentialLeaseIDs replaces the recorded delivery grants.
func (r *sessionRecord) SetCredentialLeaseIDs(ids []string) {
	if len(ids) == 0 {
		r.credentialLeaseIDs.Store(nil)
		return
	}
	copied := append([]string(nil), ids...)
	r.credentialLeaseIDs.Store(&copied)
}

// AddCredentialLease records one more delivery grant, ignoring an empty ID and
// never recording the same grant twice.
//
// It replaces the slice rather than appending in place so a concurrent reader
// never observes a partially grown one.
func (r *sessionRecord) AddCredentialLease(leaseID string) {
	if leaseID == "" {
		return
	}
	current := r.CredentialLeaseIDs()
	for _, existing := range current {
		if existing == leaseID {
			return
		}
	}
	next := make([]string, 0, len(current)+1)
	next = append(next, current...)
	next = append(next, leaseID)
	r.credentialLeaseIDs.Store(&next)
}

// resolvedHostBackend is implemented by backends that route to a specific
// daemon host (e.g. RemoteACPBackend). Spawn uses it to capture the host the
// session landed on so Prompt / Status / Close can pin re-resolution to the
// same instance. Local in-process backends do not implement it.
type resolvedHostBackend interface {
	ResolvedHost() string
}

// resolvedAgentBackend is implemented by backends bound to a registered fleet
// agent. Spawn captures the agent's ID so the session — and the job that
// spawned it — can name the daemon that actually did the work rather than
// only the host it happened to be reachable at (go-quicbaas#796). The host is
// a routing address and two agents may share one; the ID is the fleet's key.
type resolvedAgentBackend interface {
	ResolvedAgentID() string
}

// spawnAgentID returns the fleet agent a resolved backend is bound to, empty
// for a local in-process backend that is bound to none.
func spawnAgentID(backend AgentBackend) string {
	if ab, ok := backend.(resolvedAgentBackend); ok {
		return ab.ResolvedAgentID()
	}
	return ""
}

// localBackendPinKey is the unexported context key used to force
// lookupBackendCtx to bypass the RemoteBackendResolver and resolve against the
// locally registered backend table. It is set by pinSessionBackend for sessions
// whose Spawn landed on the local in-process backend, guaranteeing the whole
// session lifecycle stays on the same instance even after a remote daemon
// registers later.
type localBackendPinKey struct{}

// withLocalBackendPin returns a context that instructs lookupBackendCtx to skip
// remote resolution and use the local backend table directly.
func withLocalBackendPin(ctx context.Context) context.Context {
	return context.WithValue(ctx, localBackendPinKey{}, true)
}

// localBackendPinned reports whether the context carries a local-backend pin.
// Safe to call on any context.
func localBackendPinned(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(localBackendPinKey{}).(bool)
	return v
}

// sessionHostAffinity returns the host a resolved backend is bound to, or the
// caller-supplied request affinity as a fallback. Returns "" for local
// backends with no request affinity, signalling that no replay is needed.
func sessionHostAffinity(backend AgentBackend, requestAffinity string) string {
	if hb, ok := backend.(resolvedHostBackend); ok {
		if host := hb.ResolvedHost(); host != "" {
			return host
		}
	}
	return requestAffinity
}

// NewAgentService creates a new AgentService with the given store, backends, and logger.
func NewAgentService(
	store AgentStore,
	backends map[string]AgentBackend,
	logger interfaces.Logger,
) (AgentService, error) {
	if store == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentStoreRequired,
			Message:  "agent store must not be nil",
		}
	}
	if logger == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "logger must not be nil",
		}
	}

	// Every job this service runs is leased. The single-process store is
	// the correct store for a service whose state lives in this process; a
	// clustered host installs the transaction-plane store over it.
	leases, err := NewProcessJobLeaseStore(DefaultJobLeaseTTL)
	if err != nil {
		return nil, err
	}

	svc := &agentServiceImpl{
		store:     store,
		logger:    logger,
		jobLeases: leases,
		leaseTTL:  DefaultJobLeaseTTL,
	}

	for name, backend := range backends {
		svc.backends.Store(name, backend)
	}

	return svc, nil
}

// SetTenantGuard configures the tenant guard for strict tenant isolation.
// When set, all public methods enforce tenant boundaries and block
// SuperAdmin cross-tenant access for orchestration operations.
func (s *agentServiceImpl) SetTenantGuard(guard *TenantGuard) {
	s.tenantGuard = guard
}

// SetTaskStore configures the distributed task store for task management.
// When set, task creation, claiming, and completion operations become available.
func (s *agentServiceImpl) SetTaskStore(store TaskStore) {
	s.taskStore = store
}

// SetChannelServer configures the MCP channel server for pushing events
// into Claude Code sessions. When set, task lifecycle events are forwarded
// to the channel server as notifications.
func (s *agentServiceImpl) SetChannelServer(cs *ChannelServer) {
	s.channelServer = cs
}

// SetContainerRuntime configures the OCI container runtime for sandboxed
// agent sessions. When set, SpawnSession requests with Sandboxed=true
// will launch containers instead of spawning directly via the backend.
func (s *agentServiceImpl) SetContainerRuntime(rt ContainerRuntime) {
	s.containerRuntime = rt
}

// SetMessaging configures the messaging publisher for cross-host event
// coordination. When set, session and task lifecycle events are published
// to messaging channels for consumption by other nodes or services.
func (s *agentServiceImpl) SetMessaging(pub MessagingPublisher) {
	s.messaging = pub
}

// SetLeadAgent configures an optional LLM lead agent for intelligent
// task orchestration. When set, the lead agent decomposes tasks and
// delegates to teammates. When nil, tasks are managed programmatically.
func (s *agentServiceImpl) SetLeadAgent(config *LeadAgentConfig) {
	s.leadAgent = config
}

// SetLeadSessionID stores the session ID of the persistent lead agent session.
// This session is used for intelligent task decomposition when no explicit
// backend is specified in DecomposeTask requests.
func (s *agentServiceImpl) SetLeadSessionID(id string) {
	s.leadSessionID = id
}

// SetAgentRegistry configures the agent fleet registry for tracking
// and managing agents across hosts. When set, agent registration,
// heartbeat, and fleet management operations become available.
func (s *agentServiceImpl) SetAgentRegistry(registry AgentRegistry) {
	s.agentRegistry = registry
}

// SetKVStore configures the KV store for durable job persistence.
// When set, job status is persisted to the KV store on every state
// transition and can be recovered on startup.
func (s *agentServiceImpl) SetKVStore(kv interfaces.KVStore) {
	s.kvStore = kv
}

// SetWorkflowLoader configures the workflow loader for job resumption.
// When set, recovered jobs can reload their workflow definitions by name.
func (s *agentServiceImpl) SetWorkflowLoader(wl *WorkflowLoader) {
	s.workflowLoader = wl
}

// ListWorkflows returns the context tenant's registered workflow definitions
// sorted by name.
func (s *agentServiceImpl) ListWorkflows(ctx context.Context) ([]*WorkflowDefinition, error) {
	if s.workflowLoader == nil {
		return []*WorkflowDefinition{}, nil
	}
	return s.workflowLoader.ListDefinitions(StorageTenant(ctx)), nil
}

// GetWorkflow returns the context tenant's workflow definition by name.
func (s *agentServiceImpl) GetWorkflow(ctx context.Context, name string) (*WorkflowDefinition, error) {
	tenantID := StorageTenant(ctx)
	if s.workflowLoader == nil {
		return nil, &WorkflowNotFoundError{TenantID: tenantID, Name: name}
	}
	return s.workflowLoader.Get(tenantID, name)
}

// RegisterWorkflow validates and registers a workflow definition under the
// context tenant.
func (s *agentServiceImpl) RegisterWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	if s.workflowLoader == nil {
		s.workflowLoader = NewWorkflowLoader()
	}
	return s.workflowLoader.Register(StorageTenant(ctx), def)
}

// UpdateWorkflow replaces the context tenant's existing workflow definition
// by name.
func (s *agentServiceImpl) UpdateWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	tenantID := StorageTenant(ctx)
	if s.workflowLoader == nil {
		return &WorkflowNotFoundError{TenantID: tenantID, Name: def.Name}
	}
	return s.workflowLoader.Update(tenantID, def)
}

// DeleteWorkflow removes the context tenant's workflow definition by name.
func (s *agentServiceImpl) DeleteWorkflow(ctx context.Context, name string) error {
	tenantID := StorageTenant(ctx)
	if s.workflowLoader == nil {
		return &WorkflowNotFoundError{TenantID: tenantID, Name: name}
	}
	return s.workflowLoader.Delete(tenantID, name)
}

// SpawnChildJob fans out a child job from a parent job's lead agent.
// The calling agent (identified via req.CallerAgentID populated by the gRPC
// handler from mTLS context) must be the registered lead of the parent job.
// Any other agent receives NotLeadOfParentError (mapped to HTTP 403).
//
// Pattern for agentic fan-out:
//
//	Agent A (lead) decomposes work and calls SpawnChildJob for each sub-task.
//	Child jobs run on different hosts; their status is retrievable via GetJobStatus.
//	The parent's ChildJobIDs slice is updated atomically after each successful spawn.
func (s *agentServiceImpl) SpawnChildJob(ctx context.Context, req *SpawnChildJobRequest) (*SpawnChildJobResponse, error) {
	if req == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "spawn child job request must not be nil",
		}
	}

	if req.ParentJobID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrParentJobNotFound,
			Message:  "parent_job_id is required",
		}
	}

	// Resolve tenant from context.
	tenantID := TenantFromContext(ctx)
	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required",
		}
	}

	// Load parent job status from memory or KV.
	parentStatus, err := s.GetJobStatus(ctx, tenantID, req.ParentJobID)
	if err != nil {
		return nil, &ParentJobNotFoundError{ParentJobID: req.ParentJobID}
	}

	// Enforce lead-only constraint: the caller must match the parent's LeadAgentID.
	if parentStatus.Config != nil && parentStatus.Config.LeadAgentID != "" {
		if req.CallerAgentID != parentStatus.Config.LeadAgentID {
			return nil, &NotLeadOfParentError{
				ParentJobID:   req.ParentJobID,
				CallerAgentID: req.CallerAgentID,
				LeadAgentID:   parentStatus.Config.LeadAgentID,
			}
		}
	}

	// Build the description for the child job from inline task specs.
	description := "child of " + req.ParentJobID
	if len(req.Tasks) > 0 {
		description = req.Tasks[0].Description
		if len(req.Tasks) > 1 {
			description += " (+ " + itoa(len(req.Tasks)-1) + " more tasks)"
		}
	}

	childReq := &StartJobRequest{
		TenantID:     tenantID,
		Description:  description,
		ProjectID:    req.ProjectID,
		WorkflowName: req.WorkflowID,
	}

	resp, startErr := s.StartJob(ctx, childReq)
	if startErr != nil {
		return nil, startErr
	}

	childJobID := resp.JobID

	// Tag the child job with parent lineage. The running child records it
	// on its run so its next snapshot carries it; the persisted record is
	// updated too, for a child that has already written its last status.
	spawnedAt := time.Now().UnixNano()
	if val, ok := s.jobs.Load(tenantID + "/" + childJobID); ok {
		if rj, ok := val.(*runningJob); ok && rj.run != nil {
			rj.run.RecordParent(req.ParentJobID, req.CallerAgentID, spawnedAt)
		}
	}
	if s.kvStore != nil {
		if childStatus, loadErr := s.loadJobStatus(tenantID, childJobID); loadErr == nil {
			childStatus.ParentJobID = req.ParentJobID
			childStatus.SpawnedBy = req.CallerAgentID
			childStatus.SpawnedAt = spawnedAt
			s.persistJobStatus(tenantID, childStatus)
		}
	}

	// Atomically append the child ID to the parent's ChildJobIDs.
	s.appendChildJobID(ctx, tenantID, req.ParentJobID, childJobID)

	s.logger.Infof("orchestration: spawned child job %s from parent %s by agent %s",
		childJobID, req.ParentJobID, req.CallerAgentID)

	return &SpawnChildJobResponse{ChildJobID: childJobID}, nil
}

// appendChildJobID records childID on the parent job's lineage.
// It updates both the running job and the KV store.
//
// The running job's lineage is recorded on the RUN, never on the status it
// has published: a published status is read by every status poll and by the
// run's next snapshot, so appending to its slice in place would be a write
// under those readers. The child reaches the record at the next snapshot.
func (s *agentServiceImpl) appendChildJobID(ctx context.Context, tenantID, parentJobID, childJobID string) {
	jobKey := tenantID + "/" + parentJobID

	// Update in-memory running job if present.
	if val, ok := s.jobs.Load(jobKey); ok {
		if rj, ok := val.(*runningJob); ok && rj.run != nil {
			rj.run.AddChildJob(childJobID)
		}
	}

	// Persist to KV store for durability and cross-restart visibility.
	if s.kvStore == nil {
		return
	}
	if parentStatus, err := s.loadJobStatus(tenantID, parentJobID); err == nil {
		parentStatus.ChildJobIDs = append(parentStatus.ChildJobIDs, childJobID)
		s.persistJobStatus(tenantID, parentStatus)
	}
}

// SubmitTaskArtifacts delegates to the task store's artifact submission.
func (s *agentServiceImpl) SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []Artifact) error {
	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired, Message: "task store is required for artifact submission"}
	}
	return s.taskStore.SubmitTaskArtifacts(ctx, tenantID, taskID, artifacts)
}

// GetTaskArtifact retrieves a named artifact, resolving KV refs to bytes.
func (s *agentServiceImpl) GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*Artifact, error) {
	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired, Message: "task store is required for artifact retrieval"}
	}
	return s.taskStore.GetTaskArtifact(ctx, tenantID, taskID, name)
}

// ListTaskArtifacts returns output artifact metadata for a task.
func (s *agentServiceImpl) ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]Artifact, error) {
	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired, Message: "task store is required for artifact listing"}
	}
	return s.taskStore.ListTaskArtifacts(ctx, tenantID, taskID)
}

// SetSandboxPolicy sets the server-level policy that governs how
// SandboxModeDefault is resolved and which explicit SandboxMode values are
// permitted. Unset policies default to SandboxPolicyRequired.
func (s *agentServiceImpl) SetSandboxPolicy(policy SandboxPolicy) {
	s.sandboxPolicy = policy
}

// SetTenantPolicyProvider registers a resolver for per-tenant SandboxPolicy
// overrides. Passing nil removes the provider.
func (s *agentServiceImpl) SetTenantPolicyProvider(p TenantPolicyProvider) {
	s.tenantPolicies = p
}

// SetRemoteBackendResolver installs a resolver that routes daemon-kind
// registrations to their RemoteACPBackend before the locally registered
// backends are consulted. Passing nil disables remote routing.
func (s *agentServiceImpl) SetRemoteBackendResolver(r RemoteBackendResolver) {
	s.remoteResolver = r
}

// SetProjectAuthz configures the project-level authorization helper. When set,
// ClaimTask enforces the project's AgentRoles matrix for scoped tasks.
// Unscoped tasks (empty ProjectID) bypass authorization unchanged.
func (s *agentServiceImpl) SetProjectAuthz(authz *ProjectAuthz) {
	s.projectAuthz = authz
}

// SetProjectStore configures the durable project store consulted at spawn
// time. When set, a spawn scoped to a server-managed project resolves its
// work directory from the managed workspace and is refused with
// ProjectWorkspaceNotReadyError until provisioning reaches the ready state.
func (s *agentServiceImpl) SetProjectStore(store ProjectStore) {
	s.projectStore = store
}

// SetMountPolicy configures the policy that authorizes every host bind mount a
// spawn request would place in an agent sandbox — the resolved work directory,
// any caller-supplied container-spec mounts, and ExtraMounts. Bind mounts are
// deny-by-default: with no policy wired, every one of them is refused.
func (s *agentServiceImpl) SetMountPolicy(policy *MountPolicy) {
	s.mountPolicy = policy
}

// SetTrustPolicy configures the integrity floor every spawn resolves against
// (orchestration.trust_policy). The zero value, TrustPolicyOptIn, resolves
// TrustLevelDefault to TrustLevelNone and rejects nothing; a stricter policy
// raises the floor for every request, including the ones that name no level at
// all, and refuses a level the target host cannot prove.
func (s *agentServiceImpl) SetTrustPolicy(policy TrustPolicy) {
	s.trustPolicy = policy
}

// TrustPolicy returns the configured integrity floor.
func (s *agentServiceImpl) TrustPolicy() TrustPolicy {
	return s.trustPolicy
}

// SetCostTracker configures the token ledger every completed turn is recorded
// against. Without it a turn's reported usage is discarded, and
// LeastUsageAccountStrategy has nothing to balance on.
func (s *agentServiceImpl) SetCostTracker(tracker CostTracker) {
	s.costTracker = tracker
}

// CostTracker returns the configured token ledger, or nil when none is wired.
func (s *agentServiceImpl) CostTracker() CostTracker {
	return s.costTracker
}

// SetConsumptionLedger installs the append-only record the spend authority
// decides every cap from (see account_ledger.go). Without it a turn's
// consumption is reported by the counters above and enforced by nothing: an
// account's quota window reads empty forever and its breaker never trips.
//
// SEPARATE FROM SetCostTracker BECAUSE THEY ARE DIFFERENT OBLIGATIONS. The
// counters may fail a write and cost a dashboard some accuracy; a gap here is
// an account spending past a cap. Wiring one and not the other is a
// deployment choice the daemon makes explicitly rather than a default that
// hides which of the two is missing.
func (s *agentServiceImpl) SetConsumptionLedger(ledger ConsumptionLedger) {
	s.ledger = ledger
}

// ConsumptionLedger returns the configured spend authority's record, or nil
// when none is wired.
func (s *agentServiceImpl) ConsumptionLedger() ConsumptionLedger {
	return s.ledger
}

// SetSessionEventLog installs the append-only session event log. When set,
// session lifecycle events (spawn, prompt submission, close) are appended
// for attacher consumption. Passing nil disables the log — useful for
// embedded deployments that don't need scrollback.
func (s *agentServiceImpl) SetSessionEventLog(log SessionEventLog) {
	s.sessionLog = log
}

// SessionEventLog returns the currently-installed session event log, or
// nil when unset. Exposed so the REST/gRPC handlers can Range and Subscribe
// on the same instance the orchestrator appends to.
func (s *agentServiceImpl) SessionEventLog() SessionEventLog {
	return s.sessionLog
}

// appendSessionEvent persists a session event on behalf of the orchestrator
// control loop. The sequence number is assigned by AppendNextSessionEvent, the
// package's single write path, which retries when a concurrent writer (a
// background turn goroutine, another transport, or a live attacher recording
// presence) wins the race for the next sequence.
//
// Errors are logged but never propagated — append is a side effect of the main
// operation (spawn, prompt, close) and must not fail those operations on its
// own.
func (s *agentServiceImpl) appendSessionEvent(ctx context.Context, event *SessionEvent) {
	if s.sessionLog == nil {
		return
	}
	if err := AppendNextSessionEvent(ctx, s.sessionLog, event); err != nil {
		s.logger.Warnf("orchestration: append session event %s seq=%d: %v", event.SessionID, event.Sequence, err)
	}
}

// deleteSessionRecord removes a session everywhere it is durable: the store
// record and the transcript filed beside it.
//
// THE TWO GO TOGETHER OR NEITHER MEANS ANYTHING. A record removed while its
// conversation stayed readable leaves an operator able to open, page and
// stream a session the system reports does not exist; a transcript removed
// while the record stayed would leave a session that lists but says nothing.
// Both callers are spawn unwind paths, where the alternative to deleting the
// transcript is leaking one per failed spawn forever.
//
// Failures are reported and not returned: every caller is already unwinding a
// failure it is about to return, and replacing that cause with a cleanup error
// would hide why the spawn failed in the first place.
func (s *agentServiceImpl) deleteSessionRecord(ctx context.Context, storageTenant, sessionID string) {
	if delErr := s.store.DeleteSession(ctx, storageTenant, sessionID); delErr != nil {
		s.logger.Warnf("session %s: the false record could not be removed either: %s",
			sessionID, delErr.Error())
	}
	if s.sessionLog == nil {
		return
	}
	if delErr := s.sessionLog.DeleteSession(ctx, storageTenant, sessionID); delErr != nil {
		s.logger.Warnf("session %s: the transcript of the false record could not be removed: %s",
			sessionID, delErr.Error())
	}
}

// sessionCreator extracts a friendly identifier for the operator who
// initiated this request. Used to stamp SessionMeta.CreatedBy so attachers
// see who started each session. Falls back to the tenant ID when no
// richer context exists; refined by downstream IAM scope propagation.
func sessionCreator(ctx context.Context) string {
	if creator, ok := ctx.Value(creatorContextKey{}).(string); ok && creator != "" {
		return creator
	}
	return TenantFromContext(ctx)
}

// creatorContextKey threads the calling operator's identity down into
// orchestrator handlers. Used by gRPC/REST interceptors; defined here so
// the orchestrator can read it without a cross-package dependency.
type creatorContextKey struct{}

// WithSessionCreator returns a context stamped with the operator's identity
// for SessionMeta.CreatedBy. Interceptors call this; downstream orchestrator
// code calls sessionCreator() to read it back.
func WithSessionCreator(ctx context.Context, operatorID string) context.Context {
	if operatorID == "" {
		return ctx
	}
	return context.WithValue(ctx, creatorContextKey{}, operatorID)
}

// resolveSandboxMode turns a request's SandboxMode into the effective mode
// the backend should honor, given the server policy, any tenant override,
// and whether the orchestrator or its selected backend has access to a
// container runtime.
func (s *agentServiceImpl) resolveSandboxMode(
	ctx context.Context,
	req *SpawnSessionRequest,
	backendHasRuntime bool,
) (SandboxMode, error) {
	var tenantOverride *SandboxPolicy
	if s.tenantPolicies != nil {
		tenantOverride = s.tenantPolicies.SandboxPolicy(TenantFromContext(ctx))
	}
	hasRuntime := backendHasRuntime ||
		(s.containerRuntime != nil && s.containerRuntime.IsAvailable())
	return ResolveSandboxMode(req.EffectiveSandboxMode(), s.sandboxPolicy, tenantOverride, hasRuntime)
}

// resolveTrustLevel turns a requested TrustLevel into the effective integrity
// tier the spawn must run at, given the server's TrustPolicy, any tenant
// override, and what the runtime that will launch the agent can actually
// prove. It is the trust-axis twin of resolveSandboxMode and is called from the
// same pre-side-effect chokepoint, so a tier the host cannot satisfy fails the
// spawn instead of silently degrading to no integrity guarantee at all.
func (s *agentServiceImpl) resolveTrustLevel(
	ctx context.Context,
	requested TrustLevel,
	backend AgentBackend,
) (TrustLevel, error) {
	var tenantOverride *TrustPolicy
	if provider, ok := s.tenantPolicies.(TenantTrustPolicyProvider); ok {
		tenantOverride = provider.TrustPolicy(TenantFromContext(ctx))
	}
	return ResolveTrustLevel(requested, s.trustPolicy, tenantOverride, s.trustCapabilities(backend))
}

// trustCapabilities reports what the runtime that will launch this spawn can
// prove. The backend's own runtime is consulted first because an ACP backend
// with a container runtime launches the agent itself; the orchestrator's
// runtime is the fallback for the backends whose containers it launches.
//
// The zero value — no capability at all — is returned for a remote spawn and
// for a spawn with no runtime, so a required tier fails closed rather than
// resolving to an unprovable claim.
func (s *agentServiceImpl) trustCapabilities(backend AgentBackend) TrustCapabilities {
	if acp, ok := backend.(*ACPBackend); ok && acp.containerRuntime != nil {
		return runtimeTrustCapabilities(acp.containerRuntime)
	}
	return runtimeTrustCapabilities(s.containerRuntime)
}

// pinVerifyingRuntime is the Tier 0 capability expressed as behavior: a
// runtime that verifies, after launch, that the container it created came from
// the pinned content digest. ExecContainerRuntime satisfies it; a runtime that
// does not is not credited with digest pinning it cannot enforce.
type pinVerifyingRuntime interface {
	VerifyPinnedImage(ctx context.Context, spec *ContainerSpec, containerID string) error
}

// runtimeTrustCapabilities reports the tiers one runtime can satisfy.
func runtimeTrustCapabilities(runtime ContainerRuntime) TrustCapabilities {
	if runtime == nil || !runtime.IsAvailable() {
		return TrustCapabilities{}
	}
	// An attesting runtime answers for itself across all three tiers.
	if attesting, ok := runtime.(AttestingContainerRuntime); ok {
		return attesting.TrustCapabilities()
	}
	if _, ok := runtime.(pinVerifyingRuntime); ok {
		return TrustCapabilities{DigestPinning: true}
	}
	return TrustCapabilities{}
}

// validateTenant checks tenant access if the guard is configured.
// Returns nil if no guard is set (backward compatible).
func (s *agentServiceImpl) validateTenant(ctx context.Context) error {
	if s.tenantGuard == nil {
		return nil
	}
	tenantID := TenantFromContext(ctx)
	if tenantID == "" {
		return &TenantRequiredError{Operation: "orchestration"}
	}
	return s.tenantGuard.ValidateTenantAccess(ctx, tenantID)
}

// DiscoverProjects walks the root path to find agent-enabled projects.
func (s *agentServiceImpl) DiscoverProjects(ctx context.Context, req *DiscoverProjectsRequest) (*DiscoverProjectsResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.RootPath == "" {
		return nil, &AgentServiceError{Sentinel: ErrRootPathRequired}
	}

	info, err := os.Stat(req.RootPath)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrRootPathRequired,
			Message:  "path does not exist: " + req.RootPath,
		}
	}
	if !info.IsDir() {
		return nil, &AgentServiceError{
			Sentinel: ErrRootPathRequired,
			Message:  "path is not a directory: " + req.RootPath,
		}
	}

	absRoot, err := filepath.Abs(req.RootPath)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrRootPathRequired,
			Message:  "cannot resolve absolute path: " + err.Error(),
		}
	}

	var projects []*DiscoveredProject

	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "cannot read directory: " + err.Error(),
		}
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(absRoot, entry.Name())
		project := s.scanProjectDir(dirPath, entry.Name())
		if project != nil {
			projects = append(projects, project)
		}
	}

	// Also check the root itself
	rootProject := s.scanProjectDir(absRoot, filepath.Base(absRoot))
	if rootProject != nil {
		projects = append(projects, rootProject)
	}

	return &DiscoverProjectsResponse{
		Projects:  projects,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// scanProjectDir checks a directory for CLAUDE.md and .claude/agents/ files.
func (s *agentServiceImpl) scanProjectDir(dirPath, name string) *DiscoveredProject {
	hasClaudeMD := false
	hasAgents := false
	var agentFiles []string

	// Check for CLAUDE.md
	claudeMDPath := filepath.Join(dirPath, "CLAUDE.md")
	if _, err := os.Stat(claudeMDPath); err == nil {
		hasClaudeMD = true
	}

	// Check for .claude/agents/ directory
	agentsDir := filepath.Join(dirPath, ".claude", "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				hasAgents = true
				agentFiles = append(agentFiles, filepath.Join(agentsDir, entry.Name()))
			}
		}
	}

	if !hasClaudeMD && !hasAgents {
		return nil
	}

	return &DiscoveredProject{
		Name:        name,
		Path:        dirPath,
		HasClaudeMD: hasClaudeMD,
		HasAgents:   hasAgents,
		AgentFiles:  agentFiles,
	}
}

// RegisterProject registers a project for agent orchestration.
func (s *agentServiceImpl) RegisterProject(ctx context.Context, req *RegisterProjectRequest) (*RegisterProjectResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, &AgentServiceError{Sentinel: ErrProjectNameRequired}
	}
	if req.Path == "" {
		return nil, &AgentServiceError{Sentinel: ErrProjectPathRequired}
	}

	projectID, err := generateID()
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to generate project ID: " + err.Error(),
		}
	}

	now := time.Now().UnixNano()

	project := &RegisteredProject{
		ProjectID:      projectID,
		Name:           req.Name,
		Path:           req.Path,
		Dependencies:   req.Dependencies,
		DefaultBackend: req.DefaultBackend,
		DefaultModel:   req.DefaultModel,
		Labels:         req.Labels,
		CreatedAt:      now,
	}

	s.projects.Store(projectID, project)

	s.logger.Debugf("registered project: id=%s name=%s path=%s", projectID, req.Name, req.Path)

	return &RegisterProjectResponse{
		ProjectID: projectID,
		Timestamp: now,
	}, nil
}

// ListProjects returns all registered projects sorted by name.
func (s *agentServiceImpl) ListProjects(ctx context.Context, req *ListProjectsRequest) (*ListProjectsResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	var projects []*RegisteredProject

	s.projects.Range(func(key, value interface{}) bool {
		if project, ok := value.(*RegisteredProject); ok {
			projects = append(projects, project)
		}
		return true
	})

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})

	// Apply pagination if requested
	if req.PageSize > 0 && len(projects) > req.PageSize {
		startIdx := 0
		if req.PageToken != "" {
			for i, p := range projects {
				if p.ProjectID == req.PageToken {
					startIdx = i
					break
				}
			}
		}

		endIdx := startIdx + req.PageSize
		if endIdx > len(projects) {
			endIdx = len(projects)
		}

		nextToken := ""
		if endIdx < len(projects) {
			nextToken = projects[endIdx].ProjectID
		}

		return &ListProjectsResponse{
			Projects:      projects[startIdx:endIdx],
			NextPageToken: nextToken,
			Timestamp:     time.Now().UnixNano(),
		}, nil
	}

	return &ListProjectsResponse{
		Projects:  projects,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// unwindSpawnedProcess releases an agent process that a spawn created and then
// could not finish wiring up.
//
// A FAILED SPAWN THAT LEAVES ITS PROCESS RUNNING LEAKS IT PERMANENTLY. The
// caller is told the spawn failed, so it will not close the session; there is
// no live entry, so nothing in this process holds it; and reconciliation reads
// a record with no live entry as a session with no process, so it concludes
// the record without ever asking the backend to close. The one moment anything
// still knows the session ID and the backend together is here.
//
// The close failure is logged rather than returned: the caller is already
// being handed the failure that matters, and replacing it with a teardown
// error would hide the cause.
func (s *agentServiceImpl) unwindSpawnedProcess(
	ctx context.Context, backend AgentBackend, sessionID, because string) {
	if backend == nil || sessionID == "" {
		return
	}
	if err := backend.Close(ctx, sessionID); err != nil {
		s.logger.Warnf("orchestration: %s and its agent process could not be released: %s "+
			"(session %s may still be running)", because, err.Error(), sessionID)
	}
}

// SpawnSession creates a persistent agent session on a backend.
// When req.Sandboxed is true and a container runtime is configured,
// the session is launched inside a container. Otherwise the backend
// spawns the session directly.
func (s *agentServiceImpl) SpawnSession(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.Backend == "" {
		return nil, &AgentServiceError{Sentinel: ErrBackendRequired}
	}

	// Thread host-affinity and GPU requirement into the context so the
	// remote-backend resolver can filter candidate daemons accordingly.
	ctx = WithHostAffinity(ctx, req.HostAffinity)
	if req.GpuRequired {
		ctx = WithGpuRequired(ctx)
	}

	backend, fromRemote, err := s.resolveBackendForSpawn(ctx, req.Backend, req.ProjectID)
	if err != nil {
		return nil, err
	}

	// Capture which instance the resolver bound this session to so Prompt /
	// Status / Close can pin re-resolution to the same backend instance.
	//   - fromRemote: capture the daemon host (HostAffinity replay).
	//   - !fromRemote: mark BackendLocal so follow-ups bypass the resolver and
	//     stay on this local instance. See sessionRecord for why this is
	//     required (spawn→local / prompt→remote divergence).
	backendLocal := !fromRemote
	sessionHost := ""
	spawnedOnAgent := ""
	if fromRemote {
		sessionHost = sessionHostAffinity(backend, req.HostAffinity)
		spawnedOnAgent = spawnAgentID(backend)
	}

	// Resolve model: request > project default > empty (let backend decide)
	model := req.Model
	if model == "" && req.ProjectID != "" {
		if project, ok := s.lookupProject(req.ProjectID); ok {
			if model == "" {
				model = project.DefaultModel
			}
		}
	}

	// Resolve work directory: request > managed project workspace >
	// registered project path > current directory. A server-managed
	// workspace additionally gates the spawn on its provisioning state:
	// starting an agent in a half-cloned tree produces work against the
	// wrong base, so anything but ready is refused.
	workDir := req.WorkDir
	if workDir == "" && req.ProjectID != "" {
		managedDir, managedErr := s.managedWorkspaceDir(ctx, req.ProjectID)
		if managedErr != nil {
			return nil, managedErr
		}
		workDir = managedDir
	}
	if workDir == "" && req.ProjectID != "" {
		if project, ok := s.lookupProject(req.ProjectID); ok {
			workDir = project.Path
		}
	}
	if workDir == "" {
		workDir = WorkDirCurrent
	}

	// The event channel keeps the caller's own tenant (empty for an untenanted
	// caller) so messaging topics are unchanged, while durable records are
	// filed under the resolved storage tenant, which is never empty.
	tenantID := TenantFromContext(ctx)
	storageTenant := StorageTenant(ctx)
	now := time.Now().UnixNano()

	// Sandboxed container path — only used for backends that don't handle
	// containerization internally (e.g., non-ACP backends). ACP backends
	// with SetContainerRuntime handle their own container lifecycle.
	//
	// A spawn dispatched to a remote/embedded agent (fromRemote) is sandboxed
	// by THAT agent's own runtime (its SandboxPolicy + container runtime +
	// image map). The control plane must not also spawn a local detached
	// container for it: that container carries no interactive ACP transport,
	// so the follow-up Prompt — pinned to the remote agent — would never find
	// the session. Treat remote spawns as "backend handles containers" so the
	// local container path below is skipped and the spawn is dispatched to the
	// agent over the wire.
	acpBackend, isACP := backend.(*ACPBackend)
	backendHandlesContainers := fromRemote || (isACP && acpBackend.containerRuntime != nil)

	// Resolve sandbox mode against server policy + tenant override + runtime
	// availability. This fails fast before any side effects (container spawn
	// or backend call), giving callers a deterministic policy violation error.
	effectiveMode, modeErr := s.resolveSandboxMode(ctx, req, backendHandlesContainers)
	if modeErr != nil {
		return nil, modeErr
	}

	// Resolve the integrity tier against the operator's TrustPolicy + tenant
	// override + host capability, on the same pre-side-effect chokepoint as the
	// sandbox mode. The RESOLVED level — never req.TrustLevel — is what the
	// container spec and the backend request carry, so a request that names no
	// level still runs at the floor the operator requires.
	effectiveTrust, trustErr := s.resolveTrustLevel(ctx, req.TrustLevel, backend)
	if trustErr != nil {
		return nil, trustErr
	}

	// Every host path this spawn would bind-mount into the sandbox is
	// authorized against the operator's mount policy before any side effect
	// (container spawn or backend call). Deny-by-default: with no policy wired
	// every host mount is refused.
	if mountErr := s.authorizeSandboxMounts(ctx, req, workDir, effectiveMode); mountErr != nil {
		return nil, mountErr
	}

	// Network isolation is the sandbox axis's second dimension: a container that
	// still shares the host's network namespace can dial every loopback-bound
	// service on it. Like the trust tier, the network is resolved from operator
	// configuration and a caller that asked for the host namespace is refused —
	// see sandbox_network_policy.go.
	effectiveNetwork, networkErr := s.resolveSandboxNetwork(backend, containerConfigNetwork(req))
	if networkErr != nil {
		return nil, networkErr
	}

	// The storage tier is resolved on the same pre-side-effect chokepoint, and
	// for the same reason: a project whose record names a tier the daemon
	// cannot honour must fail before a container exists, not after. The plan
	// travels to whichever path builds the container — the detached spawn
	// below, or a backend that launches its own sandbox via spawnReq.Storage.
	effectiveStorage, storageErr := s.resolveWorkspaceStorage(ctx, req.ProjectID)
	if storageErr != nil {
		return nil, storageErr
	}

	// The durable record is built before the spawn so it captures the operator's
	// request, not the request as the backend leaves it. Spawn mutates the
	// environment in place to carry freshly minted MCP material whose
	// certificates are deleted when the session closes; persisting that would
	// resume the session against material that no longer exists. See
	// newDurableSessionRecord.
	durable := newDurableSessionRecord(
		req, storageTenant, workDir, model, effectiveMode, effectiveTrust, sessionHost, backendLocal)

	if effectiveMode == SandboxModeRequired && !backendHandlesContainers && s.containerRuntime != nil && s.containerRuntime.IsAvailable() {
		containerID, spawnErr := s.spawnSandboxedSession(
			ctx, req, workDir, model, effectiveTrust, effectiveNetwork, effectiveStorage)
		if spawnErr != nil {
			return nil, spawnErr
		}

		sessionID, idErr := generateID()
		if idErr != nil {
			return nil, &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message:  "failed to generate session ID: " + idErr.Error(),
			}
		}

		// Durable first, live second — the same ordering the backend branch
		// uses and for the same reason: the live record is the arbitration
		// ground, so its presence must imply that there is something to record.
		durable.SessionID = sessionID
		durable.ContainerID = containerID
		durable.CreatedAt = now
		durable.LastActivity = now
		if storeErr := s.store.PutSession(ctx, durable); storeErr != nil {
			return nil, &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message:  "failed to persist session: " + storeErr.Error(),
			}
		}

		s.sessions.Store(sessionID, &sessionRecord{
			Backend:      req.Backend,
			ProjectID:    req.ProjectID,
			Model:        model,
			TenantID:     storageTenant,
			ContainerID:  containerID,
			CreatedAt:    now,
			JobID:        req.JobID,
			HostAffinity: sessionHost,
			BackendLocal: backendLocal,
			Persistent:   req.Persistent,
		})

		// The same race, on the branch that has no backend session to ask
		// about: `docker run -d` returns as happily for a container that exits
		// immediately as for one that runs. See reconcileSpawnedContainerState.
		if _, reconcileErr := s.reconcileSpawnedContainerState(
			ctx, tenantID, storageTenant, sessionID, containerID); reconcileErr != nil {
			s.sessions.Delete(sessionID)
			s.deleteSessionRecord(ctx, storageTenant, sessionID)
			return nil, reconcileErr
		}

		s.logger.Debugf("spawned sandboxed session: id=%s backend=%s container=%s",
			sessionID, req.Backend, truncateID(containerID))

		s.publishEvent(ctx, tenantID, "session_spawned", map[string]string{
			"session_id":   sessionID,
			"backend":      req.Backend,
			"container_id": containerID,
			"sandboxed":    "true",
		})

		return &SpawnSessionResponse{
			SessionID:   sessionID,
			Backend:     req.Backend,
			Model:       model,
			ContainerID: containerID,
			AgentID:     spawnedOnAgent,
			Timestamp:   now,
		}, nil
	}

	// Standard backend spawn path. Sandboxed is derived from the resolved
	// effective mode for legacy backends that still consult it.
	spawnReq := &SpawnRequest{
		ProjectID: req.ProjectID,
		// The job scopes the credential delivery grant the backend's account
		// resolution takes out (account_lease.go), so a job's deliveries are
		// listable and revocable as a set.
		JobID:           req.JobID,
		WorkDir:         workDir,
		Model:           model,
		MCPServers:      req.MCPServers,
		SystemPrompt:    req.SystemPrompt,
		Env:             req.Env,
		ExtraArgs:       req.ExtraArgs,
		ExtraMounts:     append([]ContainerMount(nil), req.ExtraMounts...),
		Persistent:      req.Persistent,
		SandboxMode:     effectiveMode,
		Sandboxed:       effectiveMode == SandboxModeRequired,
		TrustLevel:      effectiveTrust,
		Storage:         effectiveStorage,
		ContainerImage:  req.ContainerImage,
		AccountID:       req.AccountID,
		AccountAuto:     req.AccountAuto,
		AccountStrategy: req.AccountStrategy,
		AccountAllow:    append([]string(nil), req.AccountAllow...),
	}

	session, err := backend.Spawn(ctx, spawnReq)
	if err != nil {
		return nil, err
	}

	// Track the session in memory for fast lookup.
	//
	// THE MODEL IS THE BACKEND'S ANSWER, NOT THE REQUEST'S. `model` here is
	// what the caller asked for, which for every factory spawn is whatever the
	// role config named and for some backends is nothing at all; the backend
	// resolves the real one and reports it on session.Model. The durable record
	// below already prefers that answer — the live record did not, so the two
	// disagreed about the same session, and the live one is what a later prompt
	// reads. Same guard as the durable record: a backend that reports no model
	// leaves the requested value in place rather than blanking it.
	sessionModel := model
	if session.Model != "" {
		sessionModel = session.Model
	}
	mem := &sessionRecord{
		Backend:      req.Backend,
		ProjectID:    req.ProjectID,
		Model:        sessionModel,
		TenantID:     storageTenant,
		ContainerID:  session.ContainerID,
		CreatedAt:    session.CreatedAt,
		JobID:        req.JobID,
		HostAffinity: sessionHost,
		BackendLocal: backendLocal,
		Persistent:   req.Persistent,
	}
	mem.SetAccountID(session.AccountID)
	mem.AddCredentialLease(session.CredentialLeaseID)

	// THE DURABLE RECORD IS WRITTEN BEFORE THE LIVE ONE, and the order is the
	// spawn-window family's own principle applied one more time: every surface
	// a concluder can touch must exist before the process can conclude.
	//
	// The live record is the arbitration ground — a concluder that finds it
	// proceeds to tear down and then to record. Installed FIRST, it opened a
	// window in which an exit observer could win the arbitration, tear the
	// backend down, find no durable record to close, and evict everything;
	// PutSession then wrote ACTIVE for a session whose process was already
	// gone, and the reconcile that exists to catch exactly that found no
	// backend session left to ask about. The original dead-but-ACTIVE bug,
	// reintroduced through the door built to close it.
	//
	// Written second, the live record's presence now implies the durable one's,
	// so "arbitration is possible" and "there is something to record" become
	// the same fact.
	durable.SessionID = session.ID
	if session.Model != "" {
		durable.Model = session.Model
	}
	// Record the account the backend RESOLVED to, not the one the caller asked
	// for. A spawn that requested strategy-based selection named no account at
	// all; persisting the resolved one is what lets a resume replay the same
	// account instead of re-running selection against a pool whose usage has
	// moved on. See spawnRequestFromRecord.
	if session.AccountID != "" {
		durable.AccountID = session.AccountID
		durable.AccountStrategy = session.AccountStrategy
		// Open the first account segment so cost attribution is a history from
		// the very first turn rather than becoming one only after a rotation.
		durable.AccountHistory = []AccountAssignment{{
			AccountID: session.AccountID,
			Strategy:  session.AccountStrategy,
			From:      session.CreatedAt,
		}}
		// AND THE DELIVERY GRANT, on the record that outlives this process. A
		// grant recorded only in memory is one a node restart loses, which is
		// precisely the case where the credential it governs is still sitting
		// in a container nothing is watching.
		durable.AddCredentialLease(session.CredentialLeaseID)
	}
	// A backend that launched its own sandbox reports the container it put the
	// agent in. Recording it here is what lets the engine act INSIDE that
	// container afterwards — the delivery's commit and push run there — and
	// what lets a resumed record still name it (orchestrator_resume.go).
	durable.ContainerID = session.ContainerID
	durable.CreatedAt = session.CreatedAt
	durable.LastActivity = now
	if err := s.store.PutSession(ctx, durable); err != nil {
		// Nothing to roll back in memory: the live record is installed below,
		// deliberately after this write. The AGENT PROCESS is another matter --
		// backend.Spawn already started one, and returning without releasing it
		// leaks it past every reader: there is no durable record naming it and
		// no live entry either, so neither the sweep nor an operator can ever
		// find it to close.
		s.unwindSpawnedProcess(ctx, backend, session.ID, "the session record could not be persisted")
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to persist session: " + err.Error(),
		}
	}

	// Register the session in the event log so attachers can list + scroll
	// back from seq=1. A nil log is a deployment that keeps no transcript and
	// is a no-op here; a log that is present and REFUSES is a failure, and the
	// difference is the whole of the next two paragraphs.
	//
	// THIS HAPPENS BEFORE THE LIVE RECORD IS PUBLISHED, and the order is the
	// correctness argument rather than a preference. Publishing first made the
	// session arbitrable by an exit observer while its log had no metadata at
	// all -- and concludeLog reads missing metadata as "no session to
	// terminate" and reports success. A process that died in that window was
	// therefore concluded on the strength of a log that had never heard of it:
	// the durable record went CLOSED and the live entry was evicted, and then
	// this registration wrote ACTIVE metadata for a session that had already
	// ended. What an operator was left with is a transcript claiming the
	// session is open, containing no ending, sitting behind a record that says
	// it is closed. Registering first means an exit observer always finds a log
	// it can actually close.
	//
	// It is also why the registration precedes the reconcile below.
	// KindSessionClosed is documented as always the last event of a session,
	// and the KV log derives its next sequence from the metadata -- so a
	// terminal event appended BEFORE the metadata exists lands at a sequence
	// this registration then resets, and the next append overwrites it. A
	// dead-at-spawn session would have its ending erased by its own beginning.
	if s.sessionLog != nil {
		// storageTenant, not tenantID: the transcript is a durable record and
		// is filed exactly where the session record is, so an untenanted
		// spawn's conversation is readable by the reader that resolves the
		// same absent tenant to "system". tenantID stays the caller's raw
		// value above because it addresses messaging TOPICS, which are not
		// keyspaces and must not gain a tenant the caller never asked for.
		// go-quicbaas#793.
		//
		// A REFUSAL FAILS THE SPAWN, and is not discarded as it used to be.
		// The ordering above only buys anything if the metadata is actually
		// there: a swallowed failure publishes an arbitrable session over a
		// log that cannot be closed, which is the exact half-conclusion this
		// block was just reordered to prevent. Failing here leaves the durable
		// record ACTIVE with no live entry, which is a state the reconciliation
		// sweep already knows how to end -- a session whose daemon holds no
		// process for it -- so the failure is recoverable rather than stranded.
		if err := s.sessionLog.PutMeta(ctx, &SessionMeta{
			SessionID:    session.ID,
			TenantID:     storageTenant,
			ProjectID:    req.ProjectID,
			Backend:      req.Backend,
			Model:        model,
			Status:       SessionStateActive,
			CreatedAt:    time.Unix(0, now).UTC(),
			CreatedBy:    sessionCreator(ctx),
			LastActivity: time.Unix(0, now).UTC(),
		}); err != nil {
			// UNWOUND IN FULL, in the reverse of the order it was built. The
			// durable record written moments ago would otherwise sit ACTIVE
			// naming a process nothing holds, and the sweep cannot release
			// that process for it: reconciliation treats a record with no live
			// entry as having no process at all and skips the backend close.
			// So the spawn releases what the spawn created.
			s.unwindSpawnedProcess(ctx, backend, session.ID,
				"the session could not be registered in the event log")
			if delErr := s.store.DeleteSession(ctx, storageTenant, session.ID); delErr != nil {
				s.logger.Warnf("orchestration: session %s failed to register in the event log and its "+
					"record could not be removed: %s; the reconciliation sweep will conclude it",
					session.ID, delErr.Error())
			}
			return nil, &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message:  "failed to register session " + session.ID + " in the event log: " + err.Error(),
			}
		}
		s.appendSessionEvent(ctx, &SessionEvent{
			SessionID: session.ID,
			TenantID:  tenantID,
			Timestamp: time.Unix(0, now).UTC(),
			Source:    "system:orchestrator",
			Kind:      KindSystemMessage,
			System: &SystemMessagePayload{
				Level:   "info",
				Message: "session spawned",
				Code:    "session_spawned",
			},
		})
	}

	// Now the session is arbitrable. An exit observer arriving from here on
	// finds both records and can conclude the session completely.
	s.sessions.Store(session.ID, mem)

	s.logger.Debugf("spawned session: id=%s backend=%s project=%s", session.ID, req.Backend, req.ProjectID)

	s.publishEvent(ctx, tenantID, "session_spawned", map[string]string{
		"session_id": session.ID,
		"backend":    req.Backend,
	})

	// THE RECORD IS RECONCILED AGAINST THE PROCESS THE MOMENT IT EXISTS.
	//
	// A supervised agent subprocess can die between backend.Spawn returning and
	// the PutSession above — a CLI that emits its init response and then exits
	// on a bad credential does exactly that. The supervisor's exit observer
	// fires in that window, finds no durable record to close, and does not
	// retry; PutSession then writes the record ACTIVE, and every store-backed
	// reader afterwards — the stall detector included — believes a dead agent is
	// live, permanently. The backend's own in-memory Status says closed and
	// nobody asks it.
	//
	// So the two halves of the window are closed by two different things and
	// they meet here: an exit BEFORE the write is caught by this check, an exit
	// AFTER it by the observer. Both converge on the same closed record, and
	// running both twice is harmless. The conclusion writes the log's terminal
	// state itself, which is why nothing above needs to guess at it.
	if _, reconcileErr := s.reconcileSpawnedSessionState(
		ctx, backend, tenantID, storageTenant, session.ID); reconcileErr != nil {
		// The record is known false and could not be corrected. Unwind rather
		// than report success over it — see reconcileSpawnedSessionState.
		s.sessions.Delete(session.ID)
		s.deleteSessionRecord(ctx, storageTenant, session.ID)
		return nil, reconcileErr
	}

	return &SpawnSessionResponse{
		SessionID:   session.ID,
		Backend:     req.Backend,
		Model:       session.Model,
		ContainerID: session.ContainerID,
		AgentID:     spawnedOnAgent,
		Timestamp:   now,
	}, nil
}

// WorkDirCurrent is the work-directory value a spawn falls back to when no
// request field, managed workspace, or registered project supplies one. It
// names the daemon's own working directory, is never a host bind-mount source,
// and is the one work-directory value the mount policy does not apply to.
//
// It is exported because the remote agent daemon (pkg/agent) authorizes the
// same field against the same policy and must recognize the same sentinel; a
// second copy of the literal in that package could drift from this one.
const WorkDirCurrent = "."

// authorizeSandboxMounts authorizes every host path this spawn would
// bind-mount into an agent sandbox against the operator's MountPolicy, scoped
// to the requesting tenant. It runs before any side effect, so a rejected
// spawn has started no container and called no backend.
//
// Three sources reach a container runtime and all three are caller-controlled:
//
//   - work_dir, which BOTH sandbox paths bind-mount read-write into the
//     container (spawnSandboxedSession below and
//     ACPBackend.spawnContainerJSONRPC, which maps it to /workspace). The
//     container runs as the daemon's own uid, so an unchecked work_dir of "/"
//     is host root read-write for the agent. This is why the check lives here
//     rather than in either backend: one policy decision, both paths.
//   - container_config.mounts, the caller-supplied full container spec.
//   - extra_mounts.
//
// The work directory and the container spec's mounts are authorized when the
// spawn will actually be containerized, which is exactly when they become host
// bind mounts; extra_mounts is authorized on every path, since it exists only
// to be mounted and a request carrying it under a non-sandboxed mode is a
// request the operator should hear about either way.
func (s *agentServiceImpl) authorizeSandboxMounts(
	ctx context.Context,
	req *SpawnSessionRequest,
	workDir string,
	mode SandboxMode,
) error {
	tenantID := StorageTenant(ctx)

	if len(req.ExtraMounts) > 0 {
		if err := s.mountPolicy.Authorize(tenantID, MountFieldExtraMounts, req.ExtraMounts); err != nil {
			return err
		}
	}
	if mode != SandboxModeRequired {
		return nil
	}
	if workDir != "" && workDir != WorkDirCurrent {
		if err := s.mountPolicy.AuthorizeSource(tenantID, MountFieldWorkDir, workDir); err != nil {
			return err
		}
	}
	if req.ContainerConfig != nil && len(req.ContainerConfig.Mounts) > 0 {
		if err := s.mountPolicy.Authorize(
			tenantID, MountFieldContainerConfig, req.ContainerConfig.Mounts); err != nil {
			return err
		}
	}
	return nil
}

// spawnSandboxedSession builds a ContainerSpec and runs the container.
//
// Everything privileged on the spec was decided by SpawnSession before this
// function was reached: every bind-mount source passed authorizeSandboxMounts,
// trustLevel is the tier RESOLVED against the operator's TrustPolicy, and
// network is the network resolved from operator configuration. None of the
// three is the value the caller asked for.
func (s *agentServiceImpl) spawnSandboxedSession(
	ctx context.Context,
	req *SpawnSessionRequest,
	workDir string,
	model string,
	trustLevel TrustLevel,
	network SandboxNetwork,
	storage WorkspaceStoragePlan,
) (string, error) {
	// Use the provided container config, or build one from request fields.
	var spec *ContainerSpec
	if req.ContainerConfig != nil {
		spec = req.ContainerConfig
	} else {
		image := req.ContainerImage
		if image == "" {
			return "", &AgentServiceError{
				Sentinel: ErrContainerImageRequired,
				Message:  "sandboxed session requires a container image",
			}
		}
		spec = &ContainerSpec{
			Image:      image,
			Env:        req.Env,
			WorkDir:    workDir,
			AutoRemove: true,
			Labels: map[string]string{
				"quicbaas.backend": req.Backend,
				"quicbaas.model":   model,
			},
		}
	}

	// The resolved tier and network override whatever the spec carried: a
	// caller-supplied ContainerConfig must not be able to name a weaker trust
	// level than the policy resolved to, nor a network the operator did not
	// choose. A request naming host networking never reaches this point —
	// resolveSandboxNetwork already failed the spawn.
	spec.TrustLevel = trustLevel
	spec.Network = network.String()

	// Add project directory as a mount if a work dir is set and no mounts
	// configured. Only an absolute path can be a host bind-mount source; the
	// "." fallback names the daemon's own directory and is carried as the
	// container's WorkDir alone.
	if filepath.IsAbs(workDir) && len(spec.Mounts) == 0 {
		spec.Mounts = []ContainerMount{{
			Source: workDir,
			Target: workDir,
		}}
	}

	// Extra mounts were already authorized against the mount policy by
	// SpawnSession before any side effects; append them verbatim.
	spec.Mounts = append(spec.Mounts, req.ExtraMounts...)

	// The project's storage tier is applied LAST, once every mount the spawn
	// carries is on the spec, because the memory budget it validates is a
	// property of the whole container rather than of the checkout alone. An
	// inert plan — every project's, until an operator configures one — leaves
	// the spec byte-identical. See ApplyWorkspaceStorage, which the ACP
	// backend's own sandbox path calls too: that is the path a software-factory
	// worker takes, and a tier wired only here would never reach one.
	if err := ApplyWorkspaceStorage(spec, workDir, storage); err != nil {
		return "", err
	}

	containerID, err := s.containerRuntime.Run(ctx, spec)
	if err != nil {
		return "", err
	}

	return containerID, nil
}

// promptTurnPlan is a validated, fully-resolved prompt request: the backend
// that will run the turn, the model it will use, and a context pinned to the
// instance that owns the session. Planning is separated from execution so the
// asynchronous path can fail fast on a bad request — before it acknowledges
// anything — and then hand the identical plan to a background goroutine.
type promptTurnPlan struct {
	// ctx is the caller's context pinned to the session's backend instance.
	ctx context.Context

	// backend is the resolved agent backend that runs the turn.
	backend AgentBackend

	// backendName is the backend's registry name, used to attribute events.
	backendName string

	// model is the resolved model, which may be empty when the backend picks.
	model string

	// tenantID scopes every session-log write for this turn.
	tenantID string

	// operator identifies who submitted the prompt, for event attribution.
	operator string

	// request is the backend-level prompt built from the service request.
	request *PromptRequest

	// turnID is this turn's identity, minted once when the turn is planned.
	//
	// PLANNED, NOT RECORDED, and the difference is what makes it an
	// idempotency key. The consumption ledger absorbs a repeated entry id as
	// a retry rather than charging twice, which is only worth anything if the
	// id belongs to the TURN — an id minted at the moment of recording would
	// be new on every attempt to record, so a retried write would be a second
	// charge. Minting it here also gives the synchronous and asynchronous
	// paths one identity: the async ack returns this value, and the entry the
	// ledger holds for that turn is filed under the same one.
	turnID string
}

// planPromptTurn validates a prompt request and resolves everything the turn
// needs to run. Every error it returns is a caller error, so both the
// synchronous and the asynchronous entry points can surface it directly.
func (s *agentServiceImpl) planPromptTurn(ctx context.Context, req *PromptSessionRequest) (*promptTurnPlan, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.Prompt == "" {
		return nil, &AgentServiceError{Sentinel: ErrPromptRequired}
	}

	// A CONCLUDED SESSION TAKES NO MORE TURNS, and the refusal happens HERE —
	// before the backend is resolved, before a turn is planned, and above all
	// before anything is appended to the session's event log. A prompt against
	// an ended session used to reach the backend, fail there, and leave its
	// attempt written after the terminal event; refusing at the top means the
	// log's "closed is last" invariant is never even tested by this path.
	if req.SessionID != "" && s.sessionConcluded(ctx, req.SessionID) {
		return nil, &AgentServiceError{
			Sentinel: ErrSessionClosed,
			Message:  "session " + req.SessionID + " has been concluded and takes no further turns",
		}
	}

	// The session's own record, read ONCE: both the backend and the model fall
	// back to what the session was spawned with, and loading it twice invited
	// the two to disagree — which is exactly what happened, because only the
	// backend ever had a fallback at all (see the model resolution below).
	var spawned *sessionRecord
	if req.SessionID != "" {
		if rec, ok := s.sessions.Load(req.SessionID); ok {
			if sr, ok := rec.(*sessionRecord); ok {
				spawned = sr
			}
		}
	}

	// Determine which backend to use: explicit request field first, then the
	// backend recorded when the session was spawned.
	backendName := req.Backend
	if backendName == "" && spawned != nil {
		backendName = spawned.Backend
	}
	if backendName == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrBackendRequired,
			Message:  "either backend or session_id must be provided",
		}
	}

	// Pin re-resolution to the backend instance that holds the live session so
	// the follow-up prompt reaches the same instance Spawn used (remote daemon
	// or local backend).
	if req.SessionID != "" {
		ctx = s.pinSessionBackend(ctx, req.SessionID)
	}

	backend, err := s.lookupBackendCtx(ctx, backendName, req.ProjectID)
	if err != nil {
		return nil, err
	}

	// The model, resolved the same way the backend just was.
	//
	// THE SESSION FALLBACK IS THE ONE THAT MATTERS AND IT WAS MISSING. No
	// factory prompt names a model: the SDLC and workflow runners set Model on
	// the SPAWN request and never on the prompt, and DefaultModel is optional
	// and unset on the burn-in projects. So every turn planned an empty model,
	// and the ledger — which requires one — refused every single consumption
	// entry with "model: must not be empty", leaving account spend caps
	// silently uncounted while the turns themselves succeeded.
	//
	// A session always knows its model: the backend resolves one and reports it
	// back, so reading it here asks the session what it is actually running
	// rather than re-deriving what somebody might have asked for.
	model := req.Model
	if model == "" && req.ProjectID != "" {
		if project, ok := s.lookupProject(req.ProjectID); ok {
			model = project.DefaultModel
		}
	}
	if model == "" && spawned != nil {
		model = spawned.Model
	}

	// The turn's identity, minted before anything runs so both entry points
	// carry the same one. A failure here fails the turn rather than running
	// it unidentified: a turn the ledger cannot key is a charge that cannot
	// be deduplicated, and the caller can retry.
	turnID, err := generateID()
	if err != nil {
		return nil, err
	}

	return &promptTurnPlan{
		ctx:         ctx,
		backend:     backend,
		backendName: backendName,
		model:       model,
		tenantID:    TenantFromContext(ctx),
		operator:    sessionCreator(ctx),
		turnID:      turnID,
		request: &PromptRequest{
			SessionID:     req.SessionID,
			ProjectID:     req.ProjectID,
			Prompt:        req.Prompt,
			Model:         model,
			SystemPrompt:  req.SystemPrompt,
			EventCallback: req.EventCallback,
		},
	}, nil
}

// completedTurn describes this turn to the recording path. sessionID is passed
// in because the backend may name the session a request left blank.
// servedModel is the model the backend reports it actually ran, which
// overrides the planned one. Empty keeps the plan's — a failed turn has no
// response to ask, and a backend that reports nothing has not contradicted
// anything.
//
// WHY THE ANSWER WINS OVER THE REQUEST: the ledger's per-model breakdown is
// only honest if it names what served the turn. A CLI that silently substitutes
// a model — a quota fallback, an alias resolving to a dated snapshot — would
// otherwise have its spend filed under a model that never ran.
func (p *promptTurnPlan) completedTurn(sessionID, servedModel string) completedTurn {
	model := p.model
	if servedModel != "" {
		model = servedModel
	}
	return completedTurn{
		turnID:    p.turnID,
		sessionID: sessionID,
		projectID: p.request.ProjectID,
		engine:    p.backendName,
		model:     model,
	}
}

// appendPromptSubmitted records the operator's prompt on the session log
// before the turn is dispatched, so attachers see what was asked even when
// the backend call fails outright.
func (p *promptTurnPlan) appendPromptSubmitted(s *agentServiceImpl) {
	if s.sessionLog == nil || p.request.SessionID == "" {
		return
	}
	s.appendSessionEvent(p.ctx, &SessionEvent{
		SessionID: p.request.SessionID,
		TenantID:  p.tenantID,
		Timestamp: time.Now().UTC(),
		Source:    "operator:" + p.operator,
		Kind:      KindPromptSubmitted,
		Prompt: &PromptSubmittedPayload{
			Prompt:       p.request.Prompt,
			Model:        p.model,
			SystemPrompt: p.request.SystemPrompt,
		},
	})
}

// runPromptTurn dispatches the planned turn to the backend and records the
// agent's output on the session log. It is shared by the synchronous and
// asynchronous entry points so both produce an identical transcript.
//
// ctx is passed explicitly rather than taken from the plan: the asynchronous
// path runs on a context detached from the request that accepted the turn.
func (s *agentServiceImpl) runPromptTurn(ctx context.Context, plan *promptTurnPlan) (*PromptSessionResponse, error) {
	// A session restored from the durable store has no agent process until
	// something needs one. Reconstituting it here — rather than at boot —
	// keeps a daemon restart from forking a subprocess for every session
	// anyone ever spawned, and puts the restoration on a context that carries
	// the caller's tenant. A failure is returned rather than swallowed: the
	// alternative is a fresh, context-free agent answering as though it
	// remembered the conversation.
	if err := s.ensureSessionLive(ctx, plan.backend, plan.request.SessionID); err != nil {
		return nil, err
	}

	// Stream the backend's transcript events (assistant chunks, chain-of-
	// thought, tool calls/results) onto the session log live, so attachers
	// watch the agent work instead of waiting for one final aggregate.
	// streamedChunks counts KindAssistantChunk events so the post-turn
	// aggregate append below can be skipped when the answer already streamed —
	// otherwise the transcript would show the whole answer twice.
	var streamedChunks atomic.Uint64
	if s.sessionLog != nil && plan.request.SessionID != "" {
		knownSessionID := plan.request.SessionID
		callerCallback := plan.request.EventCallback
		// Backend notification handlers fire from their own goroutines, so
		// serialize stamping + append; appendSessionEvent retries sequence
		// races but ordering within the turn should reflect emission order.
		var callbackMu sync.Mutex
		plan.request.EventCallback = func(ev *SessionEvent) {
			if ev == nil {
				return
			}
			callbackMu.Lock()
			if ev.SessionID == "" {
				ev.SessionID = knownSessionID
			}
			ev.TenantID = plan.tenantID
			ev.Source = "agent:" + plan.backendName
			if ev.Timestamp.IsZero() {
				ev.Timestamp = time.Now().UTC()
			}
			if ev.Kind == KindAssistantChunk {
				streamedChunks.Add(1)
			}
			s.appendSessionEvent(ctx, ev)
			callbackMu.Unlock()
			if callerCallback != nil {
				callerCallback(ev)
			}
		}
	}

	resp, err := plan.backend.Prompt(ctx, plan.request)
	if err != nil {
		// A FAILED TURN IS A LEDGER ENTRY TOO. Its tokens were spent if the
		// backend reported any, and its OCCURRENCE is what the account's
		// circuit breaker is derived from — a ledger holding only successes
		// says every account is healthy, however hard it is failing. Recorded
		// before the error is returned so the breaker sees it whether the
		// caller retries, rotates, or gives up.
		s.recordFailedTurn(ctx, plan.completedTurn(plan.request.SessionID, ""), failedTurnUsage(err))
		return nil, err
	}

	// Update last activity in the store for persistent sessions.
	sessionID := plan.request.SessionID
	if sessionID == "" {
		sessionID = resp.SessionID
	}
	if sessionID != "" {
		if updateErr := s.store.UpdateSessionActivity(ctx, StorageTenant(ctx), sessionID); updateErr != nil {
			s.logger.Warnf("failed to update session activity: session=%s err=%s", sessionID, updateErr.Error())
		}
	}

	// Append assistant output as a single aggregate AssistantChunk — but only
	// when the backend streamed no chunks of its own during the turn. A
	// streaming backend already put the answer on the log piece by piece;
	// appending the aggregate too would show it twice.
	if s.sessionLog != nil && sessionID != "" && streamedChunks.Load() == 0 {
		for _, block := range resp.Content {
			if block.Type != "text" || block.Text == "" {
				continue
			}
			s.appendSessionEvent(ctx, &SessionEvent{
				SessionID: sessionID,
				TenantID:  plan.tenantID,
				Timestamp: time.Now().UTC(),
				Source:    "agent:" + plan.backendName,
				Kind:      KindAssistantChunk,
				AssistantChunk: &AssistantChunkPayload{
					Text:       block.Text,
					StopReason: resp.StopReason,
					Model:      resp.Model,
					// WHAT THE ANSWER COST, ON THE ANSWER ITSELF. The cost
					// ledger already has it, but the ledger is keyed by turn
					// and account: an operator reading a conversation would
					// have to join two stores to answer "what did this reply
					// cost". A non-streaming turn produces exactly one
					// aggregate, so the count lands once. go-quicbaas#793.
					Usage: resp.Usage,
				},
			})
		}
	}

	// Charge the turn's reported token usage to its tenant, pooled provider
	// account, project, and (inside a control-loop job) job/agent. This is the
	// single point where a completed turn becomes cost data: it runs on both
	// the synchronous and asynchronous paths because both funnel through here.
	s.recordTurnUsage(ctx, plan.completedTurn(sessionID, resp.Model), resp.Usage)

	return &PromptSessionResponse{
		SessionID:  resp.SessionID,
		Content:    resp.Content,
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Timestamp:  time.Now().UnixNano(),
		Usage:      resp.Usage,
	}, nil
}

// PromptSession sends a prompt to an agent session or backend and blocks until
// the turn finishes.
//
// This is the orchestrator's internal turn primitive: the control loop, the
// task decomposer, and the response validators all need the completed turn in
// hand to make their next decision, and they run inside the daemon where no
// request deadline applies. Operator-facing transports must use
// PromptSessionAsync instead — a real agent turn runs for minutes, far beyond
// any transport's request timeout.
func (s *agentServiceImpl) PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	plan, err := s.planPromptTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	plan.appendPromptSubmitted(s)
	return s.runPromptTurn(plan.ctx, plan)
}

// PromptSessionAsync implements AgentService.
func (s *agentServiceImpl) PromptSessionAsync(ctx context.Context, req *PromptSessionRequest) (*PromptTurnAck, error) {
	plan, err := s.planPromptTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	if plan.request.SessionID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrSessionIDRequired,
			Message:  "an asynchronous turn publishes its output to a session event log, so a session is required; spawn one first or use Infer for a stateless call",
		}
	}
	if s.sessionLog == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrSessionLogRequired,
			Message:  "an asynchronous turn publishes its output to the session event log",
		}
	}

	// The turn's identity comes from the plan, not from a second mint here:
	// the ack a client correlates on and the ledger entry the turn is charged
	// to must be the same value, or a caller holding a turn id has no way to
	// find what it cost.
	turnID := plan.turnID

	// Record the prompt and open the turn BEFORE acknowledging, so a client
	// that attaches the instant it sees the ack cannot miss the turn's start.
	plan.appendPromptSubmitted(s)
	started := &SessionEvent{
		SessionID: plan.request.SessionID,
		TenantID:  plan.tenantID,
		Timestamp: time.Now().UTC(),
		Source:    "operator:" + plan.operator,
		Kind:      KindTurnStarted,
		TurnStarted: &TurnStartedPayload{
			TurnID:  turnID,
			Backend: plan.backendName,
			Model:   plan.model,
		},
	}
	if appendErr := AppendNextSessionEvent(ctx, s.sessionLog, started); appendErr != nil {
		// Unlike the best-effort appends elsewhere, this one is the contract:
		// without a persisted turn_started there is nothing for the caller to
		// attach to, so refuse the turn rather than run it unobservably.
		return nil, &AgentServiceError{
			Sentinel: ErrTurnNotAccepted,
			Message:  appendErr.Error(),
		}
	}

	s.dispatchTurn(plan, turnID)

	return &PromptTurnAck{
		SessionID: plan.request.SessionID,
		TurnID:    turnID,
		Status:    TurnStatusAccepted,
		Sequence:  started.Sequence,
		Backend:   plan.backendName,
		Model:     plan.model,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// dispatchTurn runs an accepted turn in the background and closes it with a
// terminal turn_completed or turn_failed event.
//
// The turn runs on context.WithoutCancel: it keeps the caller's values (tenant
// binding, authenticated principal, backend pinning) while shedding the
// caller's cancellation, which is the whole point of the asynchronous
// contract — the agent must survive the HTTP request that started it ending,
// whether by normal completion or client disconnect.
//
// turnsInFlight tracks the goroutine so a shutting-down orchestrator can wait
// for accepted turns to close out their event logs instead of tearing the
// process down mid-transcript.
func (s *agentServiceImpl) dispatchTurn(plan *promptTurnPlan, turnID string) {
	turnCtx := context.WithoutCancel(plan.ctx)
	startedAt := time.Now()

	s.turnsInFlight.Add(1)
	go func() {
		defer s.turnsInFlight.Done()

		resp, err := s.runPromptTurn(turnCtx, plan)
		elapsed := time.Since(startedAt).Milliseconds()

		event := &SessionEvent{
			SessionID: plan.request.SessionID,
			TenantID:  plan.tenantID,
			Timestamp: time.Now().UTC(),
			Source:    "agent:" + plan.backendName,
		}
		if err != nil {
			s.logger.Warnf("orchestration: turn failed: session=%s turn=%s err=%s",
				plan.request.SessionID, turnID, err.Error())
			event.Kind = KindTurnFailed
			event.TurnFailed = &TurnFailedPayload{
				TurnID:     turnID,
				Error:      err.Error(),
				DurationMS: elapsed,
			}
		} else {
			event.Kind = KindTurnCompleted
			event.TurnCompleted = &TurnCompletedPayload{
				TurnID:     turnID,
				StopReason: resp.StopReason,
				Model:      resp.Model,
				DurationMS: elapsed,
				// The terminal event is where a STREAMING turn reports its
				// cost: the fragments were on the log before the backend
				// totalled anything, so this is the first moment the count
				// exists. go-quicbaas#793.
				Usage: resp.Usage,
			}
		}
		s.appendSessionEvent(turnCtx, event)

		// A turn that failed because the provider refused the session's account
		// is the one failure the orchestrator can answer by itself. Rotation
		// runs AFTER the terminal event so the transcript records what actually
		// happened — the turn failed, and then the session moved — rather than
		// hiding the failure behind an automatic retry the operator never asked
		// for. The next prompt runs on the replacement account.
		if err != nil {
			s.rotateOnAccountLimit(turnCtx, plan.request.SessionID, err)
		}
	}()
}

// WaitForTurns blocks until every turn accepted by PromptSessionAsync has
// written its terminal turn_completed or turn_failed event.
//
// It waits without a bound, which is why daemon shutdown does not call it:
// an agent turn runs for minutes, and a stop that stalls that long is worse
// than a transcript that resumes on restart. Shutdown waits for the same
// turns inside its drain window instead, and gives up on what does not fit.
// This is for callers that genuinely need a quiescent log — tests above all
// — so nothing has to poll or sleep waiting for a background turn to land.
func (s *agentServiceImpl) WaitForTurns() {
	s.turnsInFlight.Wait()
}

// Infer runs a stateless fleet-routed inference call against the named
// backend. The backend is resolved via the RemoteBackendResolver (which
// applies host-affinity and tenant filtering) so it follows the same
// routing path as Spawn and Prompt. The call is rejected with
// InferUnsupportedError when the resolved backend's Capabilities show
// PersistentSessions=true, since those backends require a Spawn first.
func (s *agentServiceImpl) Infer(ctx context.Context, req *InferRequest) (<-chan *InferChunk, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, &AgentServiceError{Sentinel: ErrBackendRequired, Message: "request is required"}
	}
	if req.Backend == "" {
		return nil, &AgentServiceError{Sentinel: ErrBackendRequired}
	}
	if req.Prompt == "" {
		return nil, &AgentServiceError{Sentinel: ErrPromptRequired}
	}

	// Thread host affinity and GPU requirement through the context so the
	// RemoteBackendResolver can filter candidate daemons accordingly.
	inferCtx := ctx
	if req.HostAffinity != "" {
		inferCtx = WithHostAffinity(inferCtx, req.HostAffinity)
	}
	if req.GpuRequired {
		inferCtx = WithGpuRequired(inferCtx)
	}

	backend, err := s.lookupBackendCtx(inferCtx, req.Backend, "")
	if err != nil {
		return nil, err
	}

	// RemoteACPBackend implements Infer natively; the remote daemon
	// enforces its own per-backend Infer-support check (Runtime.Infer
	// rejects Spawn-required backends). Local sessionless backends
	// (OllamaBackend) are wrapped: call Prompt and emit the result as a
	// single final chunk so callers have a uniform channel interface.
	type inferrer interface {
		Infer(ctx context.Context, req *InferRequest) (<-chan *InferChunk, error)
	}

	if r, ok := backend.(inferrer); ok {
		return r.Infer(inferCtx, req)
	}

	// Local-only path: reject session-based backends that require Spawn
	// first. Remote daemons never reach this branch because the remote
	// backend adapter implements Infer above.
	if backend.Capabilities().PersistentSessions {
		return nil, &InferUnsupportedError{Backend: req.Backend, Operation: "infer"}
	}

	// Local backend fallback: call Prompt and bridge the result to a channel.
	promptReq := &PromptRequest{
		Prompt:       req.Prompt,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
	}
	resp, err := backend.Prompt(inferCtx, promptReq)
	if err != nil {
		return nil, &InferBackendError{Op: "Prompt", Err: err}
	}

	ch := make(chan *InferChunk, 1)
	ch <- &InferChunk{
		Delta:      resp.Content,
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Final:      true,
	}
	close(ch)
	return ch, nil
}

// SessionStatus returns the status of an active session.
// It first checks in-memory sessions for active backend connections,
// then falls back to the persistent store for session metadata.
func (s *agentServiceImpl) SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.SessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired}
	}

	// A CONCLUDED SESSION IS CLOSED, and that is decided before anything is
	// asked of a backend. The live record survives its conclusion — it carries
	// the compare-and-swap that makes the ending single-owner
	// (session_conclusion.go) — so every reader of that record has to respect
	// the decision written on it, or the retained entry becomes a new way to
	// report a dead session as live. Asking the backend instead would report
	// "session not found", which is a different and less useful answer than
	// "this session was closed".
	if mem, ok := s.liveSessionRecord(req.SessionID); ok && mem.concluded.Load() {
		lastActivity := mem.CreatedAt
		if record, storeErr := s.store.GetSession(ctx, mem.TenantID, req.SessionID); storeErr == nil {
			lastActivity = record.LastActivity
		}
		return &SessionStatusResponse{
			SessionID:    req.SessionID,
			State:        SessionStateClosed,
			LastActivity: lastActivity,
			Uptime:       0,
			Timestamp:    time.Now().UnixNano(),
		}, nil
	}

	// Try in-memory sessions first (active backend connections)
	backendName, err := s.findSessionBackend(req.SessionID)
	if err != nil {
		// Not in memory; check the persistent store
		record, storeErr := s.store.GetSession(ctx, StorageTenant(ctx), req.SessionID)
		if storeErr != nil {
			// Return the original session-not-found error
			return nil, err
		}

		return &SessionStatusResponse{
			SessionID:    record.SessionID,
			State:        record.Status,
			LastActivity: record.LastActivity,
			Uptime:       0,
			Timestamp:    time.Now().UnixNano(),
		}, nil
	}

	// A session restored from the durable store has no agent process to ask,
	// so its state is reported from the registry. Asking the backend would
	// report it as missing, which is exactly the wrong answer: the session is
	// intact and the next prompt reconstitutes it.
	if mem, ok := s.liveSessionRecord(req.SessionID); ok && mem.resumable.Load() {
		lastActivity := mem.CreatedAt
		if record, storeErr := s.store.GetSession(ctx, mem.TenantID, req.SessionID); storeErr == nil {
			lastActivity = record.LastActivity
		}
		return &SessionStatusResponse{
			SessionID:    req.SessionID,
			State:        SessionStateResumable,
			LastActivity: lastActivity,
			Uptime:       0,
			Timestamp:    time.Now().UnixNano(),
		}, nil
	}

	// Pin re-resolution to the backend instance that holds the live session.
	ctx = s.pinSessionBackend(ctx, req.SessionID)

	backend, err := s.lookupBackendCtx(ctx, backendName, "")
	if err != nil {
		return nil, err
	}

	status, err := backend.Status(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	return &SessionStatusResponse{
		SessionID:    status.SessionID,
		State:        status.State,
		LastActivity: status.LastActivity,
		Uptime:       status.Uptime,
		Timestamp:    time.Now().UnixNano(),
	}, nil
}

// CloseSession terminates an active session.
func (s *agentServiceImpl) CloseSession(ctx context.Context, req *CloseSessionRequest) (*CloseSessionResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.SessionID == "" {
		return nil, &AgentServiceError{Sentinel: ErrSessionIDRequired}
	}

	tenantID := TenantFromContext(ctx)
	storageTenant := StorageTenant(ctx)

	backendName, err := s.findSessionBackend(req.SessionID)
	if err != nil {
		// NO LIVE ENTRY IS NOT AUTOMATICALLY NOT-FOUND. A confirmed conclusion
		// releases the live record and leaves the durable one as the tombstone
		// (session_conclusion.go), so the store is what says whether this
		// session ever existed and how it ended. A closed record means the
		// operator's request is already satisfied.
		record, storeErr := s.store.GetSession(ctx, storageTenant, req.SessionID)
		if storeErr != nil {
			return nil, err
		}
		if record.Status == SessionStateClosed {
			return &CloseSessionResponse{
				SessionID: req.SessionID,
				Timestamp: time.Now().UnixNano(),
			}, nil
		}

		// A STALE RECORD IS CLOSEABLE, AND THIS IS THE VERB THAT CLOSES IT.
		// The session has a durable record that says it is running and no live
		// entry and no backend that will admit to it — a session whose process
		// died with a previous daemon, which is exactly what an operator is
		// looking at when they ask for it to be closed. Refusing with "not
		// found" left the only records that NEED closing as the only ones the
		// close verb could not touch.
		if _, concludeErr := s.concludeAbsentSession(
			ctx, tenantID, storageTenant, req.SessionID, ConcludedByOperator); concludeErr != nil {
			return nil, concludeErr
		}
		s.publishEvent(ctx, tenantID, "session_closed", map[string]string{
			"session_id": req.SessionID,
			"backend":    record.Backend,
		})
		return &CloseSessionResponse{
			SessionID: req.SessionID,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	// Pin re-resolution to the backend instance that holds the live session.
	ctx = s.pinSessionBackend(ctx, req.SessionID)

	backend, err := s.lookupBackendCtx(ctx, backendName, "")
	if err != nil {
		return nil, err
	}

	// EVERY ENDING GOES THROUGH ONE TRANSITION (session_conclusion.go). The
	// operator's close, the supervisor's exit observer and the spawn-time
	// reconcile contend on one compare-and-swap, and the winner does all of it:
	// backend teardown, container stop, store, log metadata, and exactly one
	// terminal event carrying who ended the session and why.
	concluded, concludeErr := s.concludeSession(ctx, tenantID, storageTenant, req.SessionID, ConcludedByOperator, conclusionClaim, backend)
	if concludeErr != nil {
		// The agent process is still there. Reporting the session closed over a
		// teardown that failed would be the false-liveness state inverted.
		return nil, concludeErr
	}
	if !concluded {
		// IDEMPOTENT SUCCESS, NOT NOT-FOUND. The operator asked for the session
		// to be closed and it is closed — by the process exiting a moment
		// earlier, most likely, which is the ordinary race between a close and
		// the death it causes. Reporting a failure here would tell the truth
		// about who performed the transition and a lie about the outcome.
		s.logger.Debugf("session %s was already concluded when the operator closed it", req.SessionID)
		return &CloseSessionResponse{
			SessionID: req.SessionID,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	s.logger.Debugf("closed session: id=%s backend=%s", req.SessionID, backendName)

	s.publishEvent(ctx, tenantID, "session_closed", map[string]string{
		"session_id": req.SessionID,
		"backend":    backendName,
	})

	return &CloseSessionResponse{
		SessionID: req.SessionID,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// ListSessions returns all sessions, merging live in-memory state with
// persisted store records. A session that survived a restart but has no agent
// process reports as resumable, so an operator can tell at a glance which
// sessions are running and which are waiting to be reconstituted.
//
// Results are scoped to the requesting tenant. A caller with no tenant — the
// embedded, single-tenant case — sees every session; a caller with one sees
// only its own, in memory and in the store alike.
func (s *agentServiceImpl) ListSessions(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	requestingTenant := TenantFromContext(ctx)

	// THE STATE FILTER IS RESOLVED BEFORE ANYTHING IS LISTED, so a caller that
	// named a state this daemon does not use learns that instead of being told
	// there are no such sessions.
	if req.Status != "" && !req.Status.Valid() {
		return nil, &AgentServiceError{
			Sentinel: ErrSessionStateUnknown,
			Message:  string(req.Status) + "; known states are " + strings.Join(SessionStates(), ", "),
		}
	}

	// Build a set of in-memory (live) session IDs
	liveSet := make(map[string]struct{})
	sessions := make([]*SessionInfo, 0)

	// listErr carries a durable-read failure out of the walk. sync.Map.Range
	// cannot return one, so the walk stops by reporting false and the error is
	// answered below.
	var listErr error

	s.sessions.Range(func(key, value interface{}) bool {
		sessionID, ok := key.(string)
		if !ok {
			return true
		}
		rec, ok := value.(*sessionRecord)
		if !ok {
			return true
		}

		// Apply backend filter if specified
		if req.Backend != "" && rec.Backend != req.Backend {
			return true
		}

		// Apply tenant filter. A record is another tenant's the moment the
		// caller names a tenant that is not its owner, guard or no guard.
		if requestingTenant != "" && rec.TenantID != requestingTenant {
			return true
		}

		liveSet[sessionID] = struct{}{}

		state := SessionStateActive
		switch {
		case rec.concluded.Load():
			// See SessionStatus: the record outlives its conclusion, so the
			// conclusion is what it reports.
			state = SessionStateClosed
		case rec.resumable.Load():
			state = SessionStateResumable
		}
		info := &SessionInfo{
			SessionID: sessionID,
			ProjectID: rec.ProjectID,
			Backend:   rec.Backend,
			Model:     rec.Model,
			State:     state,
			CreatedAt: rec.CreatedAt,
		}

		// Enrich with store data if available.
		//
		// AND THE DURABLE CLOSED VERDICT WINS. The state above is an
		// ASSUMPTION — a live map entry is reported active because nothing
		// asked otherwise — while a record marked closed is something the
		// daemon observed and wrote down: a process that exited, or one that
		// was already gone when its record was persisted. Preferring the
		// assumption would list a dead agent as active for as long as its map
		// entry survives.
		record, recErr := s.store.GetSession(ctx, rec.TenantID, sessionID)
		switch {
		case recErr == nil:
			info.LastActivity = record.LastActivity
			// THE DURABLE SPAWN TIME ANSWERS WHEN THE LIVE ONE CANNOT. The
			// live entry copies whatever the backend reported at spawn, and a
			// backend that reported none left every listing of that session
			// showing it created at the epoch. The store stamps its own on
			// write, so it always has one.
			if info.CreatedAt == 0 {
				info.CreatedAt = record.CreatedAt
			}
			// A STATE THE DAEMON OBSERVED BEATS ONE IT ASSUMED. Closed is
			// such a state, and so is ERROR: recordResumeFailure writes it
			// when a session could not be brought back, with the cause beside
			// it. Reporting that session as ACTIVE because a live map entry
			// exists is the same false liveness as reporting a concluded one,
			// and it is the worse half -- an operator retrying a session the
			// daemon already knows is broken.
			if record.Status == SessionStateClosed || record.Status == SessionStateError {
				info.State = record.Status
			}
		case errors.Is(recErr, ErrAgentStoreNotFound):
			// A live session with no durable record yet is ordinary -- the
			// record is written at spawn and this one may be mid-flight. The
			// live assumption stands because nothing contradicts it.
		default:
			// A STORE THAT WILL NOT ANSWER IS NOT A STORE THAT SAID ACTIVE.
			// The durable CLOSED verdict overrides the live map's assumption a
			// few lines up, so discarding the read that carries it reports the
			// assumption as fact: a concluded session listed as live, which is
			// the false liveness this listing exists to remove. The walk stops
			// and the caller is told.
			listErr = recErr
			return false
		}

		// FILTERED ON THE STATE THE CALLER WILL SEE, which is why this is here
		// and not beside the backend filter above: the durable CLOSED verdict
		// overrides the live map's assumption a few lines up, so filtering
		// before it would answer for a state the listing does not report.
		if req.Status != "" && info.State != req.Status {
			return true
		}

		sessions = append(sessions, info)
		return true
	})
	if listErr != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to read a session's durable record: " + listErr.Error(),
		}
	}

	// Merge in persisted sessions not currently live. The scan is bounded to
	// the caller's tenant subtree, so no other tenant's sessions are reachable.
	//
	// A FAILURE HERE FAILS THE CALL. It used to be logged and stepped over,
	// which was merely lossy while the durable half was a supplement to the
	// live map. It is not a supplement any more: reconciliation writes the
	// terminal records, so the closed, error and resumable sessions exist ONLY
	// here. Swallowing the failure answers `status=closed` with an empty 200 --
	// "there are no closed sessions" -- when the truth is "I could not look",
	// and an empty success is the one answer a caller cannot tell apart from a
	// real one. Fail-closed, exactly as jobTerminalState and concludeLog do
	// with the reads they depend on.
	records, err := s.listStoredSessions(ctx, requestingTenant)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to list sessions from the store: " + err.Error(),
		}
	}
	for _, record := range records {
		if _, isLive := liveSet[record.SessionID]; isLive {
			continue
		}

		// Apply backend filter
		if req.Backend != "" && record.Backend != req.Backend {
			continue
		}

		if req.Status != "" && record.Status != req.Status {
			continue
		}

		sessions = append(sessions, &SessionInfo{
			SessionID:    record.SessionID,
			ProjectID:    record.ProjectID,
			Backend:      record.Backend,
			Model:        record.Model,
			State:        record.Status,
			CreatedAt:    record.CreatedAt,
			LastActivity: record.LastActivity,
		})
	}

	// PAGING IS THE LAST STEP, after both halves of the merge are in hand:
	// Total must count the sessions the filters left, and a page cut before
	// the durable records were merged in would count only the live ones.
	// ApplyPage also supplies the deterministic creation-time order this
	// listing has always answered in (see sessionPageSpec).
	page, err := ApplyPage(sessions, req.Page, sessionPageSpec)
	if err != nil {
		return nil, err
	}

	return &ListSessionsResponse{
		Sessions:   page.Items,
		Timestamp:  time.Now().UnixNano(),
		Total:      page.Total,
		Limit:      page.Limit,
		Offset:     page.Offset,
		NextOffset: page.NextOffset,
	}, nil
}

// listStoredSessions reads the durable session records visible to a caller:
// one tenant's subtree when the caller names a tenant, every tenant's when it
// does not (the embedded, single-tenant case).
func (s *agentServiceImpl) listStoredSessions(ctx context.Context, tenantID string) ([]*SessionRecord, error) {
	if tenantID == "" {
		return s.store.ListAllSessions(ctx)
	}
	return s.store.ListSessions(ctx, tenantID)
}

// DecomposeTask uses an agent to decompose a task into work items.
// When req.Backend is empty and a lead agent session is configured, the lead
// session is used for decomposition via PromptSession. When req.Backend is
// provided, the named backend is called directly.
func (s *agentServiceImpl) DecomposeTask(ctx context.Context, req *DecomposeTaskRequest) (*DecomposeTaskResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req.Task == "" {
		return nil, &AgentServiceError{Sentinel: ErrTaskRequired}
	}

	// Build the decomposition prompt used by both paths.
	// Fetch available backends to include in the decomposition prompt.
	availableBackends, _ := s.ListBackends(ctx)
	prompt := buildDecompositionPrompt(req.Task, req.ProjectIDs, availableBackends...)

	// Lead agent session path: when no backend is specified and a persistent
	// lead session exists, route the decomposition through PromptSession.
	if req.Backend == "" && s.leadSessionID != "" {
		sessionResp, err := s.PromptSession(ctx, &PromptSessionRequest{
			SessionID:    s.leadSessionID,
			Prompt:       prompt,
			Model:        req.Model,
			SystemPrompt: plannerSystemPrompt(),
		})
		if err != nil {
			return nil, err
		}

		// Convert PromptSessionResponse to PromptResponse for shared parsing.
		backendResp := &PromptResponse{
			SessionID:  sessionResp.SessionID,
			Content:    sessionResp.Content,
			Model:      sessionResp.Model,
			StopReason: sessionResp.StopReason,
		}

		return s.buildDecomposeResponse(backendResp)
	}

	// Direct backend path: resolve backend name, defaulting to the first
	// available backend when none is specified.
	backendName := req.Backend
	if backendName == "" {
		backendName = s.defaultBackendName()
		if backendName == "" {
			return nil, &AgentServiceError{Sentinel: ErrBackendRequired}
		}
	}

	backend, err := s.lookupBackend(backendName)
	if err != nil {
		return nil, err
	}

	promptReq := &PromptRequest{
		Prompt:       prompt,
		Model:        req.Model,
		SystemPrompt: plannerSystemPrompt(),
	}

	resp, err := backend.Prompt(ctx, promptReq)
	if err != nil {
		return nil, err
	}

	return s.buildDecomposeResponse(resp)
}

// buildDecomposeResponse parses a backend prompt response into a
// DecomposeTaskResponse containing structured work items.
func (s *agentServiceImpl) buildDecomposeResponse(resp *PromptResponse) (*DecomposeTaskResponse, error) {
	workItems, err := parseWorkItems(resp)
	if err != nil {
		s.logger.Warnf("failed to parse decomposition response as JSON, returning raw: %s", err.Error())
		// Return a single work item with the raw response.
		workItems = []*WorkItem{{
			ID:          "raw-1",
			Title:       "Unparsed task",
			Description: extractTextContent(resp),
			Priority:    1,
		}}
	}

	return &DecomposeTaskResponse{
		WorkItems: workItems,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// BackendToolGuidance returns backend-specific tool usage instructions.
func (s *agentServiceImpl) BackendToolGuidance(backendName string) string {
	val, ok := s.backends.Load(backendName)
	if !ok {
		return ""
	}
	backend, ok := val.(AgentBackend)
	if !ok {
		return ""
	}
	return backend.ToolGuidance()
}

// ListBackendModels returns model names available on the named backend.
// Backends that implement ModelLister are queried directly; others return
// ErrBackendNotFound if unknown, or an empty slice when no lister is available.
func (s *agentServiceImpl) ListBackendModels(ctx context.Context, backendName string) ([]string, error) {
	backend, err := s.lookupBackendCtx(ctx, backendName, "")
	if err != nil {
		return nil, err
	}

	lister, ok := backend.(ModelLister)
	if !ok {
		// Backend exists but does not expose a model catalog.
		return []string{}, nil
	}

	return lister.ListModels(ctx)
}

// ListBackends returns all registered agent backends.
func (s *agentServiceImpl) ListBackends(ctx context.Context) ([]*AgentBackendInfo, error) {
	// ListBackends is a global operation - backends are not tenant-scoped.
	// No tenant validation required.

	backends := make([]*AgentBackendInfo, 0)

	s.backends.Range(func(key, value interface{}) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		backend, ok := value.(AgentBackend)
		if !ok {
			return true
		}
		caps := backend.Capabilities()

		backends = append(backends, &AgentBackendInfo{
			Name:         name,
			Type:         backend.Type(),
			Capabilities: caps,
		})

		return true
	})

	// Sort by name for deterministic output
	sort.Slice(backends, func(i, j int) bool {
		return backends[i].Name < backends[j].Name
	})

	return backends, nil
}

// RegisterBackend adds a new agent backend to the service.
func (s *agentServiceImpl) RegisterBackend(ctx context.Context, req *RegisterBackendRequest) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if req.Name == "" {
		return &AgentServiceError{Sentinel: ErrBackendNameRequired}
	}
	if req.Backend == nil {
		return &AgentServiceError{Sentinel: ErrBackendNilRequired}
	}

	s.backends.Store(req.Name, req.Backend)

	s.logger.Debugf("registered backend: name=%s type=%s", req.Name, req.Backend.Type())

	return nil
}

// HealthCheck performs a health check on all active agent sessions by
// creating an ephemeral AgentHealthMonitor and delegating to CheckAll.
func (s *agentServiceImpl) HealthCheck(ctx context.Context) (*HealthCheckReport, error) {
	// HealthCheck is a global operation - not tenant-scoped.
	// No tenant validation required.

	monitor := NewAgentHealthMonitor(s, s.logger)
	report := monitor.CheckAll(ctx)
	return report, nil
}

// =============================================================================
// Task Management
// =============================================================================

// CreateTask creates a new orchestration task via the task store.
// It validates the tenant, generates a task ID, persists the task, and
// publishes lifecycle events to messaging and channel server when configured.
//
// When req.AutoSpawn is true, the task is immediately dispatched as a
// one-shot job on the best-matching agent. The task is always persisted first;
// if no matching agent exists, it is returned with empty SpawnedJobID and
// ErrNoMatchingAgent is returned.
func (s *agentServiceImpl) CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if req.TenantID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if req.Subject == "" {
		return nil, &AgentServiceError{Sentinel: ErrTaskSubjectRequired}
	}
	if err := req.Source.Validate(); err != nil {
		return nil, err
	}
	// A project's NAME is not its id, and nothing downstream would say so:
	// see ValidateProjectID and #613.
	if err := ValidateProjectID("project_id", req.ProjectID); err != nil {
		return nil, err
	}
	if err := ValidateJobRunnerName(req.Runner); err != nil {
		return nil, err
	}

	taskID, err := GenerateTaskID()
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to generate task ID: " + err.Error(),
		}
	}

	task := &Task{
		ID:            taskID,
		TenantID:      req.TenantID,
		TeamName:      req.TeamName,
		Subject:       req.Subject,
		Description:   req.Description,
		AssignedTo:    req.AssignedTo,
		Dependencies:  req.Dependencies,
		Metadata:      req.Metadata,
		Status:        TaskStatusPending,
		AutoSpawn:     req.AutoSpawn,
		ProjectID:     req.ProjectID,
		Source:        req.Source,
		Priority:      req.Priority,
		AttemptBudget: req.AttemptBudget,
		Runner:        ResolveJobRunnerName(req.Runner),
		Routing: TaskRouting{
			Backend:      req.Backend,
			HostAffinity: req.HostAffinity,
			GpuRequired:  req.GpuRequired,
		},
	}

	// A PARKED CREATION IS ONE WRITE. See CreateTaskRequest.Park: the
	// alternative is create-then-block, whose window is exactly long enough for
	// a sweep to start the job the park exists to prevent.
	if err := applyCreateTaskPark(task, req.Park); err != nil {
		return nil, err
	}

	if err := s.taskStore.CreateTask(ctx, task); err != nil {
		return nil, err
	}

	s.logger.Debugf("created task: id=%s tenant=%s subject=%s auto_spawn=%v parked=%v",
		taskID, req.TenantID, req.Subject, req.AutoSpawn, req.Park != nil)

	s.publishEvent(ctx, req.TenantID, "task_created", map[string]string{
		"task_id":    taskID,
		"subject":    req.Subject,
		"auto_spawn": boolStr(req.AutoSpawn),
	})

	// A PARKED TASK IS ANNOUNCED AS PARKED, through the same two surfaces an
	// operator park goes through (see BlockTask). Without this the issue a
	// person is watching keeps the claiming label and says nothing about why
	// the work has not started, which is the state the surfacing hook exists to
	// end.
	if req.Park != nil {
		s.publishEvent(ctx, req.TenantID, "task_blocked", map[string]string{
			"task_id":    taskID,
			"block_kind": string(task.BlockKind),
			"reason":     task.BlockedReason,
		})
		s.surfaceTaskVerb(ctx, req.TenantID, taskID, blockKindConclusions[task.BlockKind])
		return task, nil
	}

	// When AutoSpawn is requested, dispatch the task immediately as a one-shot job.
	// The task is already persisted; spawn errors are returned to the caller while
	// the task remains queryable with empty SpawnedJobID.
	if req.AutoSpawn {
		spawnErr := s.autoSpawnTask(ctx, task)
		if spawnErr != nil {
			s.recordDispatchRefusal(ctx, task, spawnErr)
			return task, spawnErr
		}
	}

	return task, nil
}

// applyCreateTaskPark stamps a requested park onto a task being created.
//
// IT VALIDATES THE SAME TWO THINGS BlockTask DOES, from the same authorities: a
// kind in the closed enum and a non-empty reason. The rules are not restated —
// TaskBlockKind.Validate owns the first — because a second copy would be the one
// that goes stale, and the failure that follows is a task blocked with a kind
// nothing downstream can classify.
func applyCreateTaskPark(task *Task, park *BlockReason) error {
	if park == nil {
		return nil
	}
	if err := park.Kind.Validate(); err != nil {
		return err
	}
	if park.Message == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "park reason must not be empty; a park that cannot say why it happened cannot be acted on",
		}
	}
	task.Status = TaskStatusBlocked
	task.BlockKind = park.Kind
	task.BlockedReason = park.Message
	// The park names itself from the same closed vocabulary an attempt's
	// conclusion does, exactly as taskStore.block does, so a reader of the
	// record never has to tell a parked adoption from an operator hold by
	// inference.
	task.ConclusionReason = blockKindConclusions[park.Kind]
	return nil
}

// autoSpawnTask dispatches a task immediately as a one-shot orchestration job.
//
// WHAT IT TAKES TO DISPATCH DEPENDS ON THE RUNNER. The engine's lead-driven
// "workflow" runner assigns work items to fleet agents and runs the pipeline
// the project declared, so it needs a matching online agent (backend → gpu →
// host_affinity) and the project's default workflow, and refuses without
// either. A module's runner spawns its own sessions and owns its own state
// sequence, so it needs neither.
//
// THE PROJECT'S CONCURRENCY QUOTA BINDS EVERY RUNNER, and it did not before
// (go-quicbaas#898). Quotas.MaxConcurrency was enforced only inside the two
// sweep paths — the issue poller's adoptionCapacity and the dispatcher's own
// slot arithmetic — so a manually triggered CreateTask(AutoSpawn: true) walked
// straight past it and a project capped at two could be handed twenty jobs by
// a caller in a loop. The sweeps then had nothing to reclaim: the jobs were
// already leased and running.
//
// The task is always persisted before this is called, so callers always get a
// valid task back even when the dispatch is refused.
//
// THE TASK RECORDS THE ATTEMPT BEFORE THE ATTEMPT EXISTS. The job's identity is
// minted here and claimed onto the task under the store's version check, and
// only a spawner that wins that claim starts a job; see ClaimTaskSpawn in
// task_dispatcher.go for why the other order let two spawners each charge the
// same task an attempt (#670).
func (s *agentServiceImpl) autoSpawnTask(ctx context.Context, task *Task) error {
	// BEFORE THE CLAIM, because the claim charges the task an attempt. A task
	// refused for capacity has not been attempted, and a refusal that had
	// already spent an attempt would exhaust a budget on jobs that never ran.
	if err := s.checkProjectConcurrency(ctx, task); err != nil {
		return err
	}

	runnerName := ResolveJobRunnerName(task.Runner)

	// The job is an attempt at the task: it carries the task's project so the
	// workspace and role matrix resolve, the runner the task named, and the
	// task ID and attempt ordinal so the job and the task can find each other
	// from either side. The ordinal is the claim's to fill in below, because
	// the claim is what decides whose attempt this is.
	jobReq := &StartJobRequest{
		TenantID:    task.TenantID,
		Description: taskJobDescription(task),
		LeadBackend: task.Routing.Backend,
		ProjectID:   task.ProjectID,
		Runner:      runnerName,
		TaskID:      task.ID,
	}

	agentID := ""
	if runnerName == JobRunnerWorkflow {
		matched, matchErr := s.matchFleetAgentForTask(ctx, task)
		if matchErr != nil {
			return matchErr
		}
		agentID = matched

		workflowName, wfErr := s.projectDefaultWorkflow(ctx, task.TenantID, task.ProjectID)
		if wfErr != nil {
			return wfErr
		}
		jobReq.WorkflowName = workflowName
	}

	jobID, idErr := generateJobID()
	if idErr != nil {
		return &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "auto-spawn: failed to generate job ID: " + idErr.Error(),
		}
	}

	// The claim charges the attempt and refreshes task from the record the
	// store accepted, so the caller's copy is the persisted one either way.
	claim, claimErr := ClaimTaskSpawn(ctx, s.taskStore, task, jobID, time.Now().UnixNano())
	if claimErr != nil {
		return claimErr
	}
	if !claim.Claimed {
		// Another spawner is already this task's open attempt. Reporting its
		// job rather than starting a second one is the whole point: the task
		// is dispatched, which is what the caller asked for.
		s.logger.Infof("auto-spawn: task=%s is already dispatched as job=%s; no second attempt started",
			task.ID, claim.JobID)
		return nil
	}
	jobReq.Attempt = claim.Attempt

	if _, jobErr := s.startJob(ctx, jobReq, jobID); jobErr != nil {
		// The claim named a job that will never run, so it is given back and
		// the attempt refunded; the next sweep may try again.
		if releaseErr := ReleaseTaskSpawnClaim(ctx, s.taskStore, task, jobID); releaseErr != nil {
			s.logger.Warnf("auto-spawn: task=%s job=%s: the claim could not be released: %s",
				task.ID, jobID, releaseErr.Error())
		}
		return &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "auto-spawn: failed to start job: " + jobErr.Error(),
		}
	}

	s.logger.Infof("auto-spawn: task=%s job=%s runner=%s dispatched to agent=%s",
		task.ID, jobID, runnerName, agentID)

	// The job's outcome reaches the task through concludeAttempt, on the
	// goroutine that runs the job (finishJob), or through the reaper when
	// the node running it dies. Nothing here has to outlive the caller.
	s.publishEvent(ctx, task.TenantID, "task_auto_spawned", map[string]string{
		"task_id": task.ID,
		"job_id":  jobID,
		"runner":  runnerName,
		"agent":   agentID,
	})

	return nil
}

// taskJobDescription is the description an attempt at the task runs under.
func taskJobDescription(task *Task) string {
	if task.Description == "" {
		return task.Subject
	}
	return task.Subject + ": " + task.Description
}

// matchFleetAgentForTask picks the online fleet agent a lead-driven attempt
// at the task assigns work to, applying the same filters as the control loop
// (backend → gpu → host_affinity) to the task's persisted routing hints.
func (s *agentServiceImpl) matchFleetAgentForTask(ctx context.Context, task *Task) (string, error) {
	if s.agentRegistry == nil {
		return "", &NoMatchingAgentError{
			Constraint: "agent_registry",
			Detail:     "agent registry not configured for auto-spawn",
		}
	}

	item := &WorkItem{
		ID:           task.ID,
		Title:        task.Subject,
		Description:  task.Description,
		Backend:      task.Routing.Backend,
		HostAffinity: task.Routing.HostAffinity,
	}
	if task.Routing.GpuRequired {
		item.RequiredCapabilities = []string{"gpu"}
	}

	agents, listErr := s.agentRegistry.ListAgents(ctx, task.TenantID, &AgentFilter{})
	if listErr != nil {
		return "", &NoMatchingAgentError{
			Constraint: "agent_list",
			Detail:     "failed to list agents: " + listErr.Error(),
		}
	}

	// Filter to online agents only.
	online := make([]*AgentRegistration, 0, len(agents))
	for _, a := range agents {
		if a.Status != AgentStatusOffline && a.Status != AgentStatusError {
			online = append(online, a)
		}
	}

	// Only agents the task's project admits are candidates. A dispatch hands
	// the project's work to a fleet agent that never has to claim it, so
	// without this the role matrix would govern the claim path and nothing
	// else, and a denied agent would simply be given the work instead
	// (go-quicbaas#634).
	eligible, authzErr := s.projectEligibleAgents(ctx, task, online)
	if authzErr != nil {
		return "", authzErr
	}

	// Use the control loop's matching logic (backend → gpu → host_affinity).
	// We instantiate a temporary loop purely for the matching helper.
	cl := NewControlLoop(s, s.logger)
	backends, _ := s.ListBackends(ctx)

	agentID, _, matchErr := cl.matchAgentForTask(item, eligible, backends)
	if matchErr != nil {
		return "", matchErr
	}
	if agentID == "" {
		return "", &NoMatchingAgentError{
			Constraint: "agent_selection",
			Detail:     "no online agents satisfy backend/gpu/host_affinity constraints",
		}
	}
	return agentID, nil
}

// projectEligibleAgents narrows the online fleet to the agents the task's
// project admits as workers.
//
// It filters rather than checking the agent the matcher happened to pick: a
// project's role matrix normally names only its own agents, so the fleet-wide
// matcher would often land on an agent the project never listed, and refusing
// that pick would strand a task whose own agents were online and eligible.
//
// An empty result is a refusal with a reason, not a silent "no agent". A
// project the store cannot produce surfaces ProjectUnresolvedError unchanged,
// because a dispatcher that cannot read the rule must not dispatch; a project
// that simply admits none of the online agents surfaces a NoMatchingAgentError
// naming that constraint, so the task's recorded dispatch reason says the role
// matrix refused rather than blaming routing.
func (s *agentServiceImpl) projectEligibleAgents(ctx context.Context, task *Task, online []*AgentRegistration) ([]*AgentRegistration, error) {
	if s.projectAuthz == nil || task.ProjectID == "" {
		return online, nil
	}

	eligible := make([]*AgentRegistration, 0, len(online))
	var unresolved *ProjectUnresolvedError
	for _, agent := range online {
		if agent == nil {
			continue
		}
		err := s.authorizeAgentForProject(ctx, task.TenantID, task.ProjectID, agent.ID, ProjectRoleWorker)
		switch {
		case err == nil:
			eligible = append(eligible, agent)
		case errors.As(err, &unresolved):
			return nil, err
		case errors.Is(err, ErrProjectAccessDenied):
			continue
		default:
			return nil, err
		}
	}

	if len(eligible) == 0 && len(online) > 0 {
		return nil, &NoMatchingAgentError{
			Constraint: "project_role",
			Detail: "no online agent holds the " + string(ProjectRoleWorker) +
				" role in project " + task.ProjectID,
		}
	}
	return eligible, nil
}

// checkProjectConcurrency refuses an auto-spawn the project has no concurrency
// slot for.
//
// COMPUTED AS THE DISPATCHER COMPUTES IT — the quota less the project's live
// job leases — because this is the same question the dispatcher answers at the
// same moment: may one more job START now. The issue poller's capacity
// additionally subtracts the tasks already queued, and rightly so, because it
// is deciding whether to CREATE more work; a task that already exists must not
// be made to count against itself.
//
// Liveness comes from the lease store rather than from a counter, so a job
// whose node died stops holding its slot as soon as its lease goes stale, and
// nothing has to be decremented by the path that failed.
//
// THE REFUSAL IS ProjectCapacityExceededError, the same type the capacity
// lease store raises, and not a second one meaning the same thing: it already
// maps to the FaultProjectCapacityExceeded class, which is retryable and
// carries a retry-at, so a task refused here gets the same treatment as a job
// refused at lease acquisition. The two checks are not redundant — this one is
// ADMISSION, refusing before the task is charged an attempt, and that one is
// the floor underneath every path including the ones that never came through
// here.
//
// It answers nil wherever the quota cannot be decided: no project store, an
// unscoped task, no lease store, a project that does not exist, or a project
// with no cap. Every one of those is a deployment in which no quota was ever
// configured, and refusing a dispatch on the grounds that a limit could not be
// read would stop a node that has always worked. A store that ERRORS is a
// different thing and is returned: dispatching on a guess about a limit is how
// the fleet was oversubscribed in the first place.
func (s *agentServiceImpl) checkProjectConcurrency(ctx context.Context, task *Task) error {
	if s.projectStore == nil || s.jobLeases == nil || task == nil || task.ProjectID == "" {
		return nil
	}
	project, err := s.projectStore.Get(ctx, task.TenantID, task.ProjectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	limit := project.Quotas.MaxConcurrency
	if limit <= 0 {
		return nil
	}
	// ONE CLOCK READ for both the liveness cut-off and the retry-at, so the
	// instant a lease is judged stale from is the instant the caller is told
	// to come back after.
	now := time.Now().UTC()
	inflight, err := CountLiveJobLeases(ctx, s.jobLeases, task.TenantID, task.ProjectID, now, s.leaseTTL)
	if err != nil {
		return err
	}
	if inflight < limit {
		return nil
	}
	return &ProjectCapacityExceededError{
		TenantID:  task.TenantID,
		ProjectID: task.ProjectID,
		Live:      inflight,
		Limit:     limit,
		RetryAt:   now.Add(s.leaseTTL),
	}
}

// recordDispatchRefusal writes why a dispatch produced no job onto the task,
// so a pending task that is waiting says what it is waiting for. It is the
// creation-time half of that answer; the dispatcher records the standing one
// on every sweep. Both go through SetTaskDispatchReason, which owns the
// compare-and-swap and the rules about when a reason is stale. A refusal is
// diagnostic: failing to record one never fails the caller.
func (s *agentServiceImpl) recordDispatchRefusal(ctx context.Context, task *Task, reason error) {
	if s.taskStore == nil || task == nil || reason == nil {
		return
	}
	text := reason.Error()
	changed, err := SetTaskDispatchReason(ctx, s.taskStore, task.TenantID, task.ID, text)
	if err != nil {
		s.logger.Warnf("dispatch: task=%s refusal not recorded: %s", task.ID, err.Error())
		return
	}
	if !changed {
		return
	}
	task.DispatchReason = text
	s.logger.Warnf("dispatch: task=%s refused: %s", task.ID, text)
}

// projectDefaultWorkflow resolves the workflow a task scoped to projectID
// should run, from the project's DefaultWorkflow. An unscoped task, a node
// without a project store, or a project that does not exist yields "" — the
// job then runs the workflow the request or the lead backend selects, exactly
// as it did before projects declared a default. A store that cannot answer is
// an error, not an empty name: dispatching an attempt on a guess about which
// pipeline to run is worse than not dispatching it.
func (s *agentServiceImpl) projectDefaultWorkflow(ctx context.Context, tenantID, projectID string) (string, error) {
	if s.projectStore == nil || projectID == "" {
		return "", nil
	}
	project, err := s.projectStore.Get(ctx, tenantID, projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return "", nil
		}
		return "", err
	}
	return project.DefaultWorkflow, nil
}

// spawnStateProbeTimeout bounds the post-persist liveness probe.
//
// SHORT, BECAUSE IT SITS ON THE SPAWN RETURN PATH. For a local backend the
// probe is a sync.Map read and costs nothing; for a remote agent it is a gRPC
// call that may also mint a token over HTTP, and a best-effort check must not
// be able to hold a caller's spawn open on remote I/O. Exceeding this bound is
// not a failure — the probe simply did not answer in time, which is the same
// outcome as a backend that cannot answer at all.
const spawnStateProbeTimeout = 3 * time.Second

// reconcileSpawnedSessionState closes a just-persisted session record whose
// process is already gone, and reports whether it did.
//
// THE TWO OUTCOMES ARE NOT THE SAME KIND OF THING, which is why this returns
// both a verdict and an error:
//
//   - The probe could not answer — no backend, an unsupported Status, a remote
//     agent that did not reply inside the bound. Nothing is known, nothing is
//     written, and the spawn proceeds exactly as it did before this check
//     existed. Best effort is right here: the check adds information when it
//     can and takes nothing away when it cannot.
//   - The probe answered CLOSED and the write failed. Now the daemon knows the
//     record it just persisted is false, and returning success would knowingly
//     leave the durable false-liveness state this whole path exists to remove.
//     That is an error, and the caller unwinds the spawn on it.
func (s *agentServiceImpl) reconcileSpawnedSessionState(
	ctx context.Context, backend AgentBackend, tenantID, storageTenant, sessionID string) (bool, error) {
	if backend == nil || s.store == nil || sessionID == "" {
		return false, nil
	}
	// THE BOUND HOLDS FOR EVERYTHING THAT HONOURS A CONTEXT, which is every
	// backend's Status in this tree — and it is worth naming the one segment it
	// cannot reach. RemoteACPBackend.authCtx mints an OAuth2 token through
	// TokenSource.Token(), which takes no context and can perform its own HTTP
	// I/O; no caller can bound that, and it already sits on the Spawn this
	// probe follows rather than being introduced here. Giving the token source
	// a context is a change to every remote RPC, which is wider than this fix —
	// filed as #193.
	probeCtx, cancel := context.WithTimeout(ctx, spawnStateProbeTimeout)
	defer cancel()

	status, err := backend.Status(probeCtx, sessionID)
	if err != nil || status == nil || status.State != SessionStateClosed {
		return false, nil
	}
	// Through the one transition, like every other ending — so a process that
	// died inside the spawn window releases the same resources and writes the
	// same single attributed terminal event that any other conclusion does.
	// The exit observer may get here first, in the microseconds between the
	// process dying and this probe. Either way the postcondition below is
	// checked: reconcile's whole job is to guarantee that a session known dead
	// is not left durably ACTIVE, and skipping the check when somebody else won
	// the transition would skip it in exactly the case where that somebody
	// raced the durable write it depends on.
	_, concludeErr := s.concludeSession(ctx, tenantID, storageTenant, sessionID, ConcludedByReconcile, conclusionNudge, backend)
	if concludeErr != nil {
		return true, concludeErr
	}
	record, err := s.store.GetSession(ctx, storageTenant, sessionID)
	if err == nil && record.Status == SessionStateClosed {
		s.logger.Warnf("session %s exited before its record was persisted; record closed", sessionID)
		return true, nil
	}

	// "NOT CLOSED YET" IS NOT "COULD NOT BE CLOSED". The nudge above returns
	// without waiting when another caller owns the conclusion and is still
	// tearing the process down — which is the ordinary outcome here, since the
	// exit observer fires in the same microseconds this probe runs. Reading the
	// store at that instant finds ACTIVE, and treating that as failure made the
	// caller unwind: it deleted the live entry AND the durable record, and the
	// winner then reached finishDurable, found no record to close, and correctly
	// treated the absence as vacuously recorded. The session did not end up
	// listed as closed. It ended up not existing.
	//
	// So an owned conclusion is respected. Ownership is only held by a caller
	// that won the swap and has not failed — a failed teardown gives the right
	// back (concludeSession) — so this is "somebody is closing it", not "somebody
	// might". If that winner's durable half then fails, the record stays ACTIVE
	// with its live entry intact, which is the shape the reconciliation sweep
	// ends. Recoverable, rather than deleted.
	if s.sessionConclusionInFlight(sessionID) {
		s.logger.Warnf("session %s exited before its record was persisted; its conclusion is "+
			"already owned by another caller and will be recorded there", sessionID)
		return true, nil
	}

	return true, &AgentServiceError{
		Sentinel: ErrAgentServiceUnavailable,
		Message: "session " + sessionID + " exited before its record was persisted and the record " +
			"could not be closed",
	}
}

// sessionConclusionInFlight reports whether some caller currently OWNS this
// session's conclusion.
//
// Ownership is the concluded/conclusion pair published under conclusionMu by
// the caller that won the swap, and it is given back when a teardown fails
// (concludeSession), so holding it means a conclusion is genuinely running
// rather than merely attempted. A conclusion that has completely finished
// deletes the live entry (finishDurable), so the presence of a live entry that
// still claims ownership is exactly "started and not yet durable".
func (s *agentServiceImpl) sessionConclusionInFlight(sessionID string) bool {
	mem, ok := s.liveSessionRecord(sessionID)
	if !ok {
		return false
	}
	return mem.concluded.Load() && mem.conclusion.Load() != nil
}

// reconcileSpawnedContainerState is the sandboxed-container half of the same
// race, and it is a genuinely different probe: that branch generates its own
// session id and starts a DETACHED container, so there is no backend session to
// ask about and `docker run -d` returns as happily for a container that exits
// immediately as for one that runs.
func (s *agentServiceImpl) reconcileSpawnedContainerState(
	ctx context.Context, tenantID, storageTenant, sessionID, containerID string) (bool, error) {
	if s.containerRuntime == nil || s.store == nil || sessionID == "" || containerID == "" {
		return false, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, spawnStateProbeTimeout)
	defer cancel()

	running, err := s.containerRuntime.IsRunning(probeCtx, containerID)
	switch {
	case err == nil && running:
		return false, nil
	case err != nil && !errors.As(err, new(*ContainerNotFoundError)):
		// The probe could not answer. Nothing is known; nothing is written.
		return false, nil
	}
	// Either the runtime reported it stopped, or the container is GONE — which
	// on this path means exited, not unknown: the detached sandbox spec sets
	// AutoRemove, so `--rm` deletes a container the moment it exits and an
	// inspect a heartbeat later finds nothing. Treating that as unanswerable
	// would leave the fast-failing container — the exact case this probe
	// exists for — persisted active.
	// Checked on both paths, for the reason the backend branch states.
	_, concludeErr := s.concludeSession(ctx, tenantID, storageTenant, sessionID, ConcludedByReconcile, conclusionNudge, nil)
	if concludeErr != nil {
		return true, concludeErr
	}
	if record, err := s.store.GetSession(ctx, storageTenant, sessionID); err != nil || record.Status != SessionStateClosed {
		return true, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message: "container " + truncateID(containerID) + " for session " + sessionID +
				" exited before its record was persisted and the record could not be closed",
		}
	}
	s.logger.Warnf("session %s container exited before its record was persisted; record closed", sessionID)
	return true, nil
}

// boolStr converts a bool to "true" or "false" for metadata maps.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// GetTask retrieves a task by ID within a tenant namespace.
func (s *agentServiceImpl) GetTask(ctx context.Context, tenantID, taskID string) (*Task, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}

	return s.taskStore.GetTask(ctx, tenantID, taskID)
}

// ListTasks lists tasks matching the filter within a tenant.
func (s *agentServiceImpl) ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if s.taskStore == nil {
		return nil, &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return nil, &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}

	return s.taskStore.ListTasks(ctx, tenantID, filter)
}

// ClaimTask atomically claims a task for an agent using compare-and-swap.
// When the task is scoped to a project (Task.ProjectID non-empty) and a
// ProjectAuthz helper is configured, the claiming agent must hold at least
// the worker role in that project. Agents with the denied or observer role,
// or agents absent from a scoped project's role matrix, are rejected with
// ProjectAccessDeniedError. Empty AgentRoles (legacy unscoped project) allows all.
func (s *agentServiceImpl) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}
	if agentID == "" {
		return &AgentServiceError{Sentinel: ErrAgentIDRequired}
	}

	// Enforce project-level access when the task is project-scoped.
	if s.projectAuthz != nil {
		task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
		if err != nil {
			return err
		}
		if authzErr := s.authorizeAgentForProject(
			ctx, tenantID, task.ProjectID, agentID, ProjectRoleWorker,
		); authzErr != nil {
			return authzErr
		}
	}

	if err := s.taskStore.ClaimTask(ctx, tenantID, taskID, agentID); err != nil {
		return err
	}

	s.logger.Debugf("claimed task: id=%s tenant=%s agent=%s", taskID, tenantID, agentID)

	s.publishEvent(ctx, tenantID, "task_claimed", map[string]string{
		"task_id":  taskID,
		"agent_id": agentID,
	})

	return nil
}

// authorizeAgentForProject refuses work done on an agent's behalf when the
// task's project does not grant that agent at least requiredRole.
//
// IT IS THE ONE PLACE THE PER-PROJECT ROLE MATRIX IS CONSULTED on a task
// write, so the paths that hand an agent a task cannot drift apart: a claim
// asks it, and so does the dispatch that hands the work over without any
// claim at all. An unscoped task (empty ProjectID) and a node with no project
// authorizer wired are unchanged — there is no matrix to consult. Everything
// else is ProjectAuthz's decision, and a project it cannot read is a refusal
// there, never a pass.
func (s *agentServiceImpl) authorizeAgentForProject(ctx context.Context, tenantID, projectID, agentID string, requiredRole ProjectRole) error {
	if s.projectAuthz == nil || projectID == "" {
		return nil
	}
	return s.projectAuthz.AuthorizeAgentForTenant(ctx, tenantID, projectID, agentID, requiredRole)
}

// BlockTask parks a task until a person readmits it.
func (s *agentServiceImpl) BlockTask(ctx context.Context, tenantID, taskID string, kind TaskBlockKind, reason string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}

	if err := s.taskStore.BlockTask(ctx, tenantID, taskID, kind, reason); err != nil {
		return err
	}

	s.logger.Debugf("blocked task: id=%s tenant=%s kind=%s", taskID, tenantID, string(kind))

	s.publishEvent(ctx, tenantID, "task_blocked", map[string]string{
		"task_id":    taskID,
		"block_kind": string(kind),
		"reason":     reason,
	})

	s.surfaceTaskVerb(ctx, tenantID, taskID, blockKindConclusions[kind])

	return nil
}

// surfaceTaskVerb announces a task the block or readmit verb just wrote.
//
// THE RECORD IS RE-READ rather than composed here: an observer renders the
// task's own accounting — its attempts, its block reason, its result — and the
// store is what holds them. A record that cannot be read is logged and the
// announcement dropped; the verb itself has already succeeded and must not be
// undone by a failure to talk about it.
func (s *agentServiceImpl) surfaceTaskVerb(ctx context.Context, tenantID, taskID string, reason TaskConclusionReason) {
	if reason == "" {
		return
	}
	task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
	if err != nil {
		s.logger.Warnf("orchestration: task %s concluded %s but could not be read back to surface it: %s",
			taskID, reason, err.Error())
		return
	}
	s.surfaceConclusion(ctx, tenantID, taskConclusionRecord{reason: reason, task: task}, "")
}

// ReadmitTask returns a blocked task to the pending queue.
func (s *agentServiceImpl) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}

	if err := s.taskStore.ReadmitTask(ctx, tenantID, taskID); err != nil {
		return err
	}

	s.logger.Debugf("readmitted task: id=%s tenant=%s", taskID, tenantID)

	s.publishEvent(ctx, tenantID, "task_readmitted", map[string]string{
		"task_id": taskID,
	})

	s.surfaceTaskVerb(ctx, tenantID, taskID, TaskConcludedReadmitted)

	return nil
}

// CompleteTask marks a task as completed with a result.
func (s *agentServiceImpl) CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}

	if err := s.taskStore.CompleteTask(ctx, tenantID, taskID, result); err != nil {
		return err
	}

	s.logger.Debugf("completed task: id=%s tenant=%s", taskID, tenantID)

	s.publishEvent(ctx, tenantID, "task_completed", map[string]string{
		"task_id": taskID,
	})

	return nil
}

// =============================================================================
// Event Publishing
// =============================================================================

// publishEvent publishes an orchestration event to both the messaging service
// and the channel server when configured. Errors are logged but do not
// propagate to the caller since event publishing is best-effort.
func (s *agentServiceImpl) publishEvent(ctx context.Context, tenantID, eventType string, metadata map[string]string) {
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["type"] = eventType

	// Publish to messaging service
	if s.messaging != nil {
		event := struct {
			Type     string            `json:"type"`
			TenantID string            `json:"tenant_id"`
			Metadata map[string]string `json:"metadata"`
		}{
			Type:     eventType,
			TenantID: tenantID,
			Metadata: metadata,
		}
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			s.logger.Warnf("failed to marshal event: %s", marshalErr.Error())
		} else {
			channel := "orchestrator:events"
			if tenantID != "" {
				channel = "orchestrator:events:" + tenantID
			}
			if pubErr := s.messaging.Publish(ctx, channel, payload); pubErr != nil {
				s.logger.Warnf("failed to publish event: %s", pubErr.Error())
			}
		}
	}

	// Push to channel server
	if s.channelServer != nil {
		content := eventType
		if subject, ok := metadata["subject"]; ok {
			content = eventType + ": " + subject
		}
		if taskID, ok := metadata["task_id"]; ok && content == eventType {
			content = eventType + ": " + taskID
		}
		pushErr := s.channelServer.PushEvent(ctx, &ChannelEvent{
			Content:  content,
			Metadata: metadata,
		})
		if pushErr != nil {
			s.logger.Warnf("failed to push channel event: %s", pushErr.Error())
		}
	}

	// Fan-out to per-job event subscribers.
	jobID, hasJobID := metadata["job_id"]
	if hasJobID && jobID != "" {
		jobKey := tenantID + "/" + jobID
		if val, ok := s.eventSubscribers.Load(jobKey); ok {
			sl, ok := val.(*subscriberList)
			if !ok {
				return
			}
			evt := &JobEvent{
				JobID:     jobID,
				Type:      eventType,
				State:     JobState(metadata["state"]),
				Message:   metadata["details"],
				Iteration: 0,
				Metadata:  metadata,
				Timestamp: time.Now().UnixNano(),
			}
			for _, sub := range sl.subs {
				select {
				case sub.ch <- evt:
				default:
					// Drop if subscriber is slow; non-blocking.
				}
			}
		}
	}
}

// =============================================================================
// Job Persistence
// =============================================================================

// jobRecordTimeout bounds ONE durable job-record operation — a single write
// or a single read by id. It is NOT a bound on a scan: see jobRecordScope.
const jobRecordTimeout = 5 * time.Second

// =============================================================================
// The scope every durable job-record operation runs in
// =============================================================================
//
// A JOB RECORD IS ADDRESSED BY ITS KEY, NOT BY THE CALLER'S TENANT. The store
// underneath this service routes by the tenant on the CONTEXT (see
// pkg/server/tenant_kvstore.go): a barrier-isolated tenant's writes land in
// its own backend, an untenanted write lands in the shared one. The job
// record's tenant is in its KEY instead — "job:{tenant}:{id}", and a listing
// is bounded by "job:{tenant}:" so no tenant can observe another's — and it
// has to be, because the two readers that MUST work carry no tenant to route
// by: the restart scan, which enumerates every node's interrupted jobs before
// any request exists, and the reaper's death record, which is written for a
// tenant whose barrier may by then be sealed. A subsystem whose entire
// purpose is to record what happened cannot fail closed on a seal.
//
// So all four paths — the write, the read by id, the tenant listing and the
// restart scan — are detached from the caller's tenant. Before
// go-quicbaas#822 only three of them were: ListJobs scanned on the REQUEST
// context, so every record a barrier tenant's jobs wrote was filed in one
// store and looked for in another. The measured effect was a task page whose
// attempt history was empty for every job that had left memory, while the
// very same record answered a GET by id.
//
// WHAT THEY DO NOT SHARE IS A TIMEOUT, and that distinction is the whole
// reason there are two helpers rather than one. The single-record operations
// have always carried their own 5 s; the two SCANS have always been bounded
// by their caller and nothing else. Putting the 5 s on the scans as well —
// which the first cut of #822 did — recreates this PR's own symptom through
// a different door: a deadline-less REST listing would inherit 5 s as the
// only deadline in the chain, and a tenant with thousands of concluded
// records, or a store whose prefix scan is simply slower than that, would
// have its attempt history silently truncated. The scan error is logged and
// not returned, so the caller would see a short list and no reason for it.

// jobRecordScope detaches ctx's VALUES — its tenant above all — while leaving
// the bound on the work exactly where the caller put it.
//
// It adds NO deadline of its own: a caller with one keeps it, and a caller
// without one is still unbounded, which is what both scans need. Cancellation
// is forwarded with context.AfterFunc rather than a goroutine of our own, so
// a listing still stops when the request it serves is abandoned, and the
// returned cancel detaches the hook as well as releasing the scope.
func jobRecordScope(ctx context.Context) (context.Context, context.CancelFunc) {
	scope, cancel := context.WithCancel(context.Background())
	if deadline, ok := ctx.Deadline(); ok {
		cancel()
		scope, cancel = context.WithDeadline(context.Background(), deadline)
	}
	stop := context.AfterFunc(ctx, cancel)
	return scope, func() {
		stop()
		cancel()
	}
}

// jobRecordOp is the scope for ONE record: untenanted for the same reason
// jobRecordScope is, and bounded by jobRecordTimeout because a single Get or
// Put that has not answered in five seconds is a stuck store, not a large
// one.
//
// It takes no context because no caller has one to give: the write happens on
// the job goroutine after the request that started it returned, and the read
// by id serves a lookup whose own deadline the caller never threads down.
func jobRecordOp() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), jobRecordTimeout)
}

// persistJobStatus writes a job status to the KV store. This is best-effort:
// if the KV write fails, the error is logged but not propagated. The in-memory
// state remains authoritative while the control loop is running.
func (s *agentServiceImpl) persistJobStatus(tenantID string, status *JobStatus) {
	if err := s.storeJobStatus(tenantID, status); err != nil {
		s.logger.Warnf("orchestration: failed to persist job status: job=%s: %s",
			status.JobID, err.Error())
	}
}

// storeJobStatus is persistJobStatus with the failure RETURNED rather than
// logged, for the one caller that cannot step over it: a conclusion owes the
// job its tidy-up before the session record may be written closed, so a job
// store that will not answer has to leave the session retryable instead of
// silently finishing without it. See recordSessionConclusionOnJob.
func (s *agentServiceImpl) storeJobStatus(tenantID string, status *JobStatus) error {
	if s.kvStore == nil {
		return nil
	}

	key := jobPersistenceKey(tenantID, status.JobID)
	data, err := json.Marshal(status)
	if err != nil {
		return &JobPersistenceError{
			JobID:   status.JobID,
			Message: "failed to marshal job status for persistence",
			Cause:   err,
		}
	}

	ctx, cancel := jobRecordOp()
	defer cancel()

	if putErr := s.kvStore.Put(ctx, key, data); putErr != nil {
		return &JobPersistenceError{
			JobID:   status.JobID,
			Message: "failed to write job status to the KV store",
			Cause:   putErr,
		}
	}
	return nil
}

// loadJobStatus reads a job status from the KV store by tenant and job ID.
func (s *agentServiceImpl) loadJobStatus(tenantID, jobID string) (*JobStatus, error) {
	if s.kvStore == nil {
		return nil, &JobPersistenceError{
			JobID:   jobID,
			Message: "KV store not configured",
		}
	}

	key := jobPersistenceKey(tenantID, jobID)

	ctx, cancel := jobRecordOp()
	defer cancel()

	data, err := s.kvStore.Get(ctx, key)
	if err != nil {
		return nil, &JobPersistenceError{
			JobID:   jobID,
			Message: "failed to read from KV store",
			Cause:   err,
		}
	}

	var status JobStatus
	if unmarshalErr := json.Unmarshal(data, &status); unmarshalErr != nil {
		return nil, &JobPersistenceError{
			JobID:   jobID,
			Message: "failed to unmarshal job status",
			Cause:   unmarshalErr,
		}
	}

	return &status, nil
}

// jobStatusAbsent reports whether a loadJobStatus failure means the job record
// is genuinely NOT THERE, as opposed to not readable.
//
// THE TWO ARE OPPOSITE ANSWERS AND loadJobStatus RETURNS THEM THE SAME SHAPE.
// A job that was never persisted, or whose record has been reaped, has nothing
// to tell and nothing to retry. A read that timed out, a store that is
// unavailable, a body that will not unmarshal — those are answers the store
// FAILED TO GIVE, and treating them as absence is how a caller walks past work
// it still owes. The distinction lives here, beside the function that produces
// the error, so a caller cannot get it wrong by unwrapping differently.
func jobStatusAbsent(err error) bool {
	if err == nil {
		return false
	}
	var persistence *JobPersistenceError
	if !errors.As(err, &persistence) {
		return false
	}
	// Only the KV's own not-found means absent. An unmarshal failure has a
	// non-nil Cause that is not a not-found, and a store that is unavailable
	// reports its own error; both are read failures, not empty keyspaces.
	return persistence.Cause != nil && qerrors.IsNotFound(persistence.Cause)
}

// recoverJobs scans the KV store for persisted jobs with non-terminal states.
// For each found, it attempts to resume the job from a safe checkpoint state
// using the persisted config, work items, and memory. If resumption fails,
// the job is marked as failed.
func (s *agentServiceImpl) recoverJobs(ctx context.Context) error {
	if s.kvStore == nil {
		return nil
	}

	prefix := "job:"
	var recoveredCount int

	// The restart scan runs in the job-record scope for the same reason
	// every other job-record read does: it must see every tenant's
	// interrupted jobs, and it has no tenant to route by.
	//
	// THE BOOT BUDGET IS THE CALLER'S, unchanged: recoverOrchestrationJobs
	// passes a context bounded by Server.ShutdownTimeout
	// (pkg/server/lifecycle_orchestration.go), and the scope neither
	// shortens nor extends it. Recovery enumerates every record a node
	// left behind, and the honest bound on that is how long the operator
	// is willing to wait for the daemon to come up — not a constant this
	// package picked.
	scanCtx, cancelScan := jobRecordScope(ctx)
	defer cancelScan()

	scanErr := s.kvStore.Scan(scanCtx, prefix, func(key string, value []byte) error {
		var status JobStatus
		if unmarshalErr := json.Unmarshal(value, &status); unmarshalErr != nil {
			s.logger.Warnf("orchestration: skipping corrupted job record: key=%s: %s",
				key, unmarshalErr.Error())
			return nil
		}

		// Skip jobs that already reached a terminal state.
		if IsTerminalJobState(status.State) {
			return nil
		}

		// Only this node's jobs are resumed. A job another node owns is that
		// node's to resume, and if that node is gone the reaper — not a
		// restart here — concludes it from its stale lease.
		if status.Node != s.node {
			s.logger.Debugf("orchestration: job %s is owned by node %q, not resumed on %q",
				status.JobID, status.Node, s.node)
			return nil
		}

		// Attempt to resume the job from persisted state.
		if resumeErr := s.resumeJob(ctx, &status); resumeErr != nil {
			s.logger.Warnf("orchestration: failed to resume job %s, marking as failed: %s",
				status.JobID, resumeErr.Error())

			// Mark as failed since resumption failed.
			status.State = JobStateFailed
			status.Error = "failed to resume after restart: " + resumeErr.Error()
			status.UpdatedAt = time.Now().UnixNano()

			data, marshalErr := json.Marshal(&status)
			if marshalErr != nil {
				s.logger.Warnf("orchestration: failed to marshal failed job status: job=%s: %s",
					status.JobID, marshalErr.Error())
				return nil
			}

			if putErr := s.kvStore.Put(scanCtx, key, data); putErr != nil {
				s.logger.Warnf("orchestration: failed to persist failed job status: job=%s: %s",
					status.JobID, putErr.Error())
			}
			return nil
		}

		recoveredCount++
		return nil
	})

	if scanErr != nil {
		return &JobRecoveryError{
			Message: "failed to scan KV store for recoverable jobs",
			Cause:   scanErr,
		}
	}

	if recoveredCount > 0 {
		s.logger.Infof("orchestration: resumed %d interrupted jobs", recoveredCount)
	}

	return nil
}

// RecoverJobs is the exported entry point for job recovery on startup.
// It delegates to the internal recoverJobs method.
func (s *agentServiceImpl) RecoverJobs(ctx context.Context) error {
	return s.recoverJobs(ctx)
}

// resumeJob relaunches an interrupted job from a safe checkpoint state. The
// checkpoint is the runner's own answer for the state the job was
// interrupted in, or the runner's first state when it has none. Persisted
// recoverable state — work items, memory, iteration — is restored by the
// envelope and the runner from the status.
func (s *agentServiceImpl) resumeJob(ctx context.Context, status *JobStatus) error {
	// Validate that we have enough persisted state to resume.
	if status.Config == nil {
		return &JobResumeError{
			JobID:   status.JobID,
			Message: "no config in persisted state",
		}
	}

	if status.JobID == "" {
		return &JobResumeError{
			Message: "no job ID in persisted state",
		}
	}

	config := status.Config
	if config.Runner == "" {
		config.Runner = ResolveJobRunnerName(status.Runner)
	}
	if config.WorkflowName == "" {
		config.WorkflowName = status.WorkflowName
	}
	runner, ok := LookupJobRunner(config.Runner)
	if !ok {
		return &UnknownJobRunnerError{Name: config.Runner}
	}

	resumeState := resumeStateFor(runner, status.State)

	// If resolve returned a terminal state (e.g., escalating -> failed),
	// persist the terminal state and return without launching a goroutine.
	if IsTerminalJobState(resumeState) {
		status.State = resumeState
		status.UpdatedAt = time.Now().UnixNano()
		s.persistJobStatus(config.TenantID, status)
		s.logger.Infof("orchestration: job %s resolved to terminal state %s, not resuming",
			status.JobID, resumeState)
		return nil
	}

	if _, err := s.launchJob(ctx, config, status.JobID, status); err != nil {
		return err
	}

	s.logger.Infof("orchestration: resumed job %s from state %s (original: %s)",
		status.JobID, resumeState, status.State)

	return nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// lookupBackend retrieves a backend by name from the locally registered set.
// Callers that have request context should prefer lookupBackendCtx so a
// remote-kind registration can preempt the local lookup.
func (s *agentServiceImpl) lookupBackend(name string) (AgentBackend, error) {
	val, ok := s.backends.Load(name)
	if !ok {
		return nil, &BackendNotFoundError{Backend: name}
	}
	backend, ok := val.(AgentBackend)
	if !ok {
		return nil, &BackendNotFoundError{Backend: name}
	}
	return backend, nil
}

// lookupBackendCtx resolves a backend for the given request context. It first
// consults any registered RemoteBackendResolver; if the resolver returns a
// backend, it is used. Otherwise the locally registered backend table is
// consulted. This is the routing primitive used by the BackendDispatcher.
//
// When the context carries a local-backend pin (set by pinSessionBackend for a
// session whose Spawn landed on the local backend) the resolver is bypassed
// entirely so the follow-up call deterministically reaches the same local
// instance that holds the live session.
func (s *agentServiceImpl) lookupBackendCtx(ctx context.Context, name, projectID string) (AgentBackend, error) {
	if localBackendPinned(ctx) {
		return s.lookupBackend(name)
	}
	if s.remoteResolver != nil {
		if backend, ok := s.remoteResolver(ctx, name, projectID); ok && backend != nil {
			return backend, nil
		}
	}
	return s.lookupBackend(name)
}

// resolveBackendForSpawn resolves a backend at Spawn time and reports whether
// the resolution came from the RemoteBackendResolver (fromRemote=true) or fell
// through to the local backend table (fromRemote=false). Spawn records this
// origin on the session so the whole lifecycle is pinned to the same backend
// instance the session was spawned on. See sessionRecord.BackendLocal.
func (s *agentServiceImpl) resolveBackendForSpawn(ctx context.Context, name, projectID string) (backend AgentBackend, fromRemote bool, err error) {
	if s.remoteResolver != nil {
		if b, ok := s.remoteResolver(ctx, name, projectID); ok && b != nil {
			return b, true, nil
		}
	}
	b, lookupErr := s.lookupBackend(name)
	if lookupErr != nil {
		return nil, false, lookupErr
	}
	return b, false, nil
}

// defaultBackendName returns the name of the default backend to use when none
// is specified. Prefers the lead agent backend if configured, then "ollama",
// then falls back to the first registered backend. Returns empty if no
// backends are registered.
func (s *agentServiceImpl) defaultBackendName() string {
	if s.leadAgent != nil && s.leadAgent.Backend != "" {
		return s.leadAgent.Backend
	}
	if _, ok := s.backends.Load("ollama"); ok {
		return "ollama"
	}
	var first string
	s.backends.Range(func(key, _ interface{}) bool {
		first, _ = key.(string)
		return false // stop after first
	})
	return first
}

// managedWorkspaceDir resolves the work directory for a spawn scoped to a
// server-managed project. It returns:
//
//   - ("", nil) when no durable project store is wired, the project is not a
//     durable record (e.g. it names an in-memory RegisteredProject), or the
//     record is not server-managed — the caller falls through to the legacy
//     resolution chain;
//   - (path, nil) when the managed workspace is ready;
//   - ("", *ProjectWorkspaceNotReadyError) when the workspace exists but is
//     still pending/cloning or has failed;
//   - ("", err) when the store itself failed, so the readiness gate can never
//     be skipped by an outage.
func (s *agentServiceImpl) managedWorkspaceDir(ctx context.Context, projectID string) (string, error) {
	if s.projectStore == nil {
		return "", nil
	}
	tenantID := StorageTenant(ctx)
	project, err := s.projectStore.Get(ctx, tenantID, projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return "", nil
		}
		return "", err
	}
	if !project.Provisioning.Managed {
		return "", nil
	}
	if project.Provisioning.State != ProvisioningStateReady {
		return "", &ProjectWorkspaceNotReadyError{
			TenantID:  tenantID,
			ProjectID: projectID,
			State:     project.Provisioning.State,
			Reason:    project.Provisioning.Error,
		}
	}
	return project.Workspace.Path, nil
}

// resolveWorkspaceStorage resolves the storage tier a spawn's container
// mounts its checkout on, from the durable project record.
//
// THE RECORD IS THE ONLY SOURCE. A tier decides where a tenant's work lives
// and whether it survives a restart, so it is a property of the project an
// operator configured, never of the request that spawned the session: a caller
// able to ask for an ephemeral workspace could discard another tenant's
// checkout by asking.
//
// AN UNRESOLVABLE PROJECT RESOLVES TO NOTHING, which is the inert plan: no
// project store wired, no ProjectID on the request, or a store that holds no
// such record all leave the spawn launching exactly the container it always
// did. That is the state of every caller that predates this dimension, and a
// tier invented for them would move their work without being asked.
//
// A record that names a tier the daemon cannot honour is REFUSED here rather
// than rounded to the zero value: rounding down means writing to the disk the
// project was configured off, silently.
func (s *agentServiceImpl) resolveWorkspaceStorage(ctx context.Context, projectID string) (WorkspaceStoragePlan, error) {
	if s.projectStore == nil || projectID == "" {
		return WorkspaceStoragePlan{}, nil
	}
	project, err := s.projectStore.Get(ctx, StorageTenant(ctx), projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return WorkspaceStoragePlan{}, nil
		}
		return WorkspaceStoragePlan{}, err
	}
	if err := project.Storage.Validate(); err != nil {
		return WorkspaceStoragePlan{}, err
	}
	return project.Storage.Plan(), nil
}

// lookupProject retrieves a project by ID.
func (s *agentServiceImpl) lookupProject(projectID string) (*RegisteredProject, bool) {
	val, ok := s.projects.Load(projectID)
	if !ok {
		return nil, false
	}
	proj, ok := val.(*RegisteredProject)
	if !ok {
		return nil, false
	}
	return proj, true
}

// findSessionBackend finds which backend owns a session.
func (s *agentServiceImpl) findSessionBackend(sessionID string) (string, error) {
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		return "", &SessionNotFoundError{SessionID: sessionID}
	}
	rec, ok := val.(*sessionRecord)
	if !ok {
		return "", &SessionNotFoundError{SessionID: sessionID}
	}
	return rec.Backend, nil
}

// pinSessionBackend pins a follow-up Prompt / Status / Close to the SAME
// backend instance the session was spawned on, covering both resolution
// outcomes Spawn can produce:
//
//   - Spawn resolved to a remote daemon: the captured host is replayed onto the
//     context so lookupBackendCtx re-resolves to that same daemon.
//   - Spawn resolved to the local in-process backend: a local-backend pin is set
//     so lookupBackendCtx bypasses the resolver and uses the local backend
//     directly. This is the fix for the spawn→local / prompt→remote divergence:
//     without it a remote daemon registering AFTER Spawn would hijack the
//     backend name on the follow-up and route to an empty instance.
//
// Without this, a follow-up that omits the host hint (the common SPA case) can
// re-resolve to a different daemon or the local fallback backend, where the
// in-memory session does not exist — surfacing as "acp: session not found".
// Returns the context unchanged when the session is unknown.
func (s *agentServiceImpl) pinSessionBackend(ctx context.Context, sessionID string) context.Context {
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		return ctx
	}
	rec, ok := val.(*sessionRecord)
	if !ok {
		return ctx
	}
	// Local-spawned sessions: force resolution to the local backend, never the
	// resolver. This wins regardless of any per-request affinity because the
	// live session physically lives only in the local instance.
	if rec.BackendLocal {
		return withLocalBackendPin(ctx)
	}
	if rec.HostAffinity == "" {
		return ctx
	}
	// Do not override an explicit per-request affinity — an operator may be
	// deliberately re-routing. Only fill in when the caller left it empty.
	if HostAffinityFromContext(ctx) != "" {
		return ctx
	}
	return WithHostAffinity(ctx, rec.HostAffinity)
}

// generateID produces a random hex-encoded ID.
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// buildDecompositionPrompt creates a structured prompt for task decomposition.
// When backends are provided, the prompt includes available backends and their
// capabilities so the AI can assign work items to appropriate backends.
func buildDecompositionPrompt(task string, projectIDs []string, backends ...*AgentBackendInfo) string {
	projects := ""
	if len(projectIDs) > 0 {
		projects = workflowPrompts.MustRender("decomposition_projects", prompts.Vars{
			"project_ids": strings.Join(projectIDs, ", "),
		}) + "\n\n"
	}
	available := ""
	if len(backends) > 0 {
		var lines strings.Builder
		for _, b := range backends {
			lines.WriteString(workflowPrompts.MustRender("decomposition_backend", prompts.Vars{
				"name":         b.Name,
				"type":         b.Type,
				"capabilities": describeBackendCapabilities(b.Capabilities),
			}))
			lines.WriteString("\n")
		}
		available = workflowPrompts.MustRender("decomposition_backends", prompts.Vars{
			"backend_lines": lines.String(),
		}) + "\n\n"
	}
	return workflowPrompts.MustRender("decomposition_prompt", prompts.Vars{
		"task":     task,
		"projects": projects,
		"backends": available,
	})
}

// backendCapabilityNames is what each capability is called in a prompt, in
// the order a backend's line lists them. It is the vocabulary a planner
// assigns work with rather than prose, so it stays in Go.
var backendCapabilityNames = []struct {
	name string
	has  func(BackendCapabilities) bool
}{
	{"persistent_sessions", func(c BackendCapabilities) bool { return c.PersistentSessions }},
	{"tool_use", func(c BackendCapabilities) bool { return c.ToolUse }},
	{"file_access", func(c BackendCapabilities) bool { return c.FileAccess }},
	{"streaming", func(c BackendCapabilities) bool { return c.Streaming }},
	{"acp", func(c BackendCapabilities) bool { return c.ACP }},
}

// describeBackendCapabilities lists what a backend can do, or says none.
func describeBackendCapabilities(capabilities BackendCapabilities) string {
	caps := make([]string, 0, len(backendCapabilityNames))
	for _, candidate := range backendCapabilityNames {
		if candidate.has(capabilities) {
			caps = append(caps, candidate.name)
		}
	}
	if len(caps) == 0 {
		return "none"
	}
	return strings.Join(caps, ", ")
}

// plannerSystemPrompt is the planning session's standing instructions.
func plannerSystemPrompt() string {
	return workflowPrompts.MustRender("planner_system", nil)
}

// parseWorkItems extracts work items from a prompt response.
func parseWorkItems(resp *PromptResponse) ([]*WorkItem, error) {
	text := extractTextContent(resp)
	if text == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "empty response from backend",
		}
	}

	// Try to extract JSON from the response
	jsonStr := extractJSON(text)
	if jsonStr == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "no JSON found in response",
		}
	}

	var items []*WorkItem
	if err := json.Unmarshal([]byte(jsonStr), &items); err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to parse work items JSON: " + err.Error(),
		}
	}

	return items, nil
}

// extractTextContent concatenates all text blocks from a prompt response.
func extractTextContent(resp *PromptResponse) string {
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

// extractJSON finds the first JSON array in a string. Brackets inside string
// literals are content, not structure — see extractBalanced.
func extractJSON(text string) string {
	return extractBalanced(text, '[', ']')
}

// RegisterAgent registers an agent with the fleet registry.
func (s *agentServiceImpl) RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*RegisterAgentResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if s.agentRegistry == nil {
		return nil, &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	if req.Name == "" {
		return nil, &AgentServiceError{Sentinel: ErrAgentNameRequired}
	}
	if req.Host == "" {
		return nil, &AgentServiceError{Sentinel: ErrAgentHostRequired}
	}
	if req.AgentType == "" {
		return nil, &AgentServiceError{Sentinel: ErrAgentTypeRequired}
	}

	id, err := generateID()
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "failed to generate agent ID: " + err.Error(),
		}
	}

	agent := &AgentRegistration{
		ID:           id,
		TenantID:     req.TenantID,
		Name:         req.Name,
		Host:         req.Host,
		Backend:      req.Backend,
		Model:        req.Model,
		AgentType:    req.AgentType,
		Capabilities: req.Capabilities,
		WorkDir:      req.WorkDir,
		Status:       AgentStatusRegistered,
		Metadata:     req.Metadata,
	}

	if err := s.agentRegistry.RegisterAgent(ctx, agent); err != nil {
		return nil, err
	}

	s.logger.Infof("registered agent: id=%s tenant=%s name=%s host=%s type=%s",
		agent.ID, agent.TenantID, agent.Name, agent.Host, agent.AgentType)

	s.publishEvent(ctx, agent.TenantID, "agent_registered", map[string]string{
		"agent_id":   agent.ID,
		"agent_name": agent.Name,
		"host":       agent.Host,
		"agent_type": agent.AgentType,
	})

	return &RegisterAgentResponse{Agent: agent}, nil
}

// GetAgent retrieves a registered agent by ID.
func (s *agentServiceImpl) GetAgent(ctx context.Context, tenantID, agentID string) (*AgentRegistration, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if s.agentRegistry == nil {
		return nil, &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	return s.agentRegistry.GetAgent(ctx, tenantID, agentID)
}

// ListAgents lists all registered agents for a tenant.
func (s *agentServiceImpl) ListAgents(ctx context.Context, tenantID string, filter *AgentFilter) ([]*AgentRegistration, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if s.agentRegistry == nil {
		return nil, &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	return s.agentRegistry.ListAgents(ctx, tenantID, filter)
}

// RemoveAgent removes an agent from the fleet registry.
func (s *agentServiceImpl) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}
	if s.agentRegistry == nil {
		return &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	// The registration is read BEFORE it is removed: the sessions pinned to an
	// agent are found by the host names the registration carries, and once it
	// is gone there is nothing left to match them against.
	agent, lookupErr := s.agentRegistry.GetAgent(ctx, tenantID, agentID)
	if lookupErr != nil {
		agent = nil
	}

	if err := s.agentRegistry.RemoveAgent(ctx, tenantID, agentID); err != nil {
		return err
	}

	// AN UNREGISTERED DAEMON TAKES ITS SESSIONS WITH IT. Their processes are
	// on a host that is no longer in the fleet, so nothing here can reach,
	// prompt or close them again; leaving the records ACTIVE would show an
	// operator sessions on a daemon they have just removed.
	if agent != nil {
		s.concludeAgentSessions(ctx, tenantID, agent,
			"agent "+agentID+" was removed from the fleet, so the daemon holding the session is gone")
	}

	s.logger.Infof("removed agent: tenant=%s agent=%s", tenantID, agentID)

	s.publishEvent(ctx, tenantID, "agent_removed", map[string]string{
		"agent_id": agentID,
	})

	return nil
}

// AgentHeartbeat updates an agent's last seen timestamp and reconciles its
// record against what the daemon reports it is serving.
//
// observed is what the daemon said about itself on this heartbeat; nil when
// the transport carries no served list, which reconciles nothing. See
// AgentObservation for why an observation that reports NO served backend is
// refused rather than applied.
func (s *agentServiceImpl) AgentHeartbeat(ctx context.Context, tenantID, agentID string, observed *AgentObservation) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}
	if s.agentRegistry == nil {
		return &AgentServiceError{Sentinel: ErrAgentRegistryRequired, Message: "agent registry not configured"}
	}
	return s.agentRegistry.Heartbeat(ctx, tenantID, agentID, observed)
}

// =============================================================================
// Job Orchestration (Phase 3 - stubs for interface compliance)
// =============================================================================

// StartJob starts an autonomous orchestration job. It validates the request,
// gives the job's runner the chance to refuse the configuration, and hands
// the rest to the envelope (job_envelope.go), which mints the job ID, takes
// the lease and drives the run.
func (s *agentServiceImpl) StartJob(ctx context.Context, req *StartJobRequest) (*StartJobResponse, error) {
	return s.startJob(ctx, req, "")
}

// startJob is StartJob with the job's identity settled by the caller. An
// empty jobID mints one in the envelope, which is what every request-driven
// start does; a non-empty one starts the job the caller has ALREADY recorded
// somewhere it cannot take back — the spawn claim on a task
// (ClaimTaskSpawn), which names the job before the job exists so that two
// spawners cannot each create one. Nothing outside this package supplies it:
// StartJobRequest has no job-ID field, so a transport cannot dictate a job's
// identity.
func (s *agentServiceImpl) startJob(ctx context.Context, req *StartJobRequest, jobID string) (*StartJobResponse, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if req == nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "start job request must not be nil",
		}
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = TenantFromContext(ctx)
	}
	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to start a job",
		}
	}

	// A project's NAME is not its id, and past this point nothing would say
	// so: the job's lease pins the project segment of its key at acquisition
	// from whatever the job carried (job_run_lease.go), and a quota counting
	// that subtree then counts a project nobody registered. This is the same
	// check CreateTask makes, on the other way in. See ValidateProjectID,
	// #613 and #667.
	if err := ValidateProjectID("project_id", req.ProjectID); err != nil {
		return nil, err
	}

	// Parse poll interval, defaulting to 5s.
	var pollInterval time.Duration
	if req.PollInterval != "" {
		var parseErr error
		pollInterval, parseErr = time.ParseDuration(req.PollInterval)
		if parseErr != nil {
			return nil, &AgentServiceError{
				Sentinel: ErrInvalidInput,
				Message:  "invalid poll interval: " + parseErr.Error(),
			}
		}
	}

	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}

	// A workflow file is a request-level convenience: it registers the
	// definition with the loader so the envelope's name-based resolution
	// finds it.
	if req.WorkflowFile != "" && s.workflowLoader != nil {
		wf, wfErr := s.workflowLoader.LoadFromFile(tenantID, req.WorkflowFile)
		if wfErr == nil {
			if req.WorkflowName == "" {
				req.WorkflowName = wf.Name
			}
			s.logger.Infof("orchestration: loaded workflow from file %q (name=%s)", req.WorkflowFile, wf.Name)
		} else {
			s.logger.Warnf("orchestration: failed to load workflow file %q: %s", req.WorkflowFile, wfErr.Error())
		}
	}

	config := &JobConfig{
		TenantID:      tenantID,
		Description:   req.Description,
		LeadBackend:   req.LeadBackend,
		LeadModel:     req.LeadModel,
		LeadAgentID:   req.LeadAgentID,
		ProjectID:     req.ProjectID,
		PollInterval:  pollInterval,
		MaxIterations: maxIter,
		WorkflowName:  req.WorkflowName,
		Runner:        ResolveJobRunnerName(req.Runner),
		TaskID:        req.TaskID,
		Attempt:       req.Attempt,
		Params:        copyJobParams(req.Params),
	}

	// The runner refuses a configuration it cannot drive before anything is
	// created: a workflow job with no description has no lead prompt, and a
	// module runner has its own requirements.
	runner, ok := LookupJobRunner(config.Runner)
	if !ok {
		return nil, &UnknownJobRunnerError{Name: config.Runner}
	}
	if err := runner.Validate(ctx, config); err != nil {
		return nil, err
	}
	// A per-subject singleton whose subject is already held is refused
	// here, before a record exists; see refuseHeldLease.
	if err := s.refuseHeldLease(ctx, runner, config); err != nil {
		return nil, err
	}

	startedID, err := s.launchJob(ctx, config, jobID, nil)
	if err != nil {
		return nil, err
	}

	return &StartJobResponse{
		JobID:     startedID,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// copyJobParams takes the runner's parameters by value, so a caller that
// keeps and later mutates the map it passed cannot change the configuration a
// running job was validated against.
func copyJobParams(params map[string]string) map[string]string {
	if len(params) == 0 {
		return nil
	}
	copied := make(map[string]string, len(params))
	for key, value := range params {
		copied[key] = value
	}
	return copied
}

// GetJobStatus returns the current status of a running orchestration job.
func (s *agentServiceImpl) GetJobStatus(ctx context.Context, tenantID, jobID string) (*JobStatus, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to get job status",
		}
	}
	if jobID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTaskIDRequired,
			Message:  "job ID is required to get job status",
		}
	}

	jobKey := tenantID + "/" + jobID
	val, ok := s.jobs.Load(jobKey)
	if ok {
		job, castOk := val.(*runningJob)
		if !castOk {
			return nil, &AgentServiceError{
				Sentinel: ErrControlLoopFailed,
				Message:  "invalid job record type",
			}
		}

		status := job.run.Status()
		if status == nil {
			return nil, &AgentServiceError{
				Sentinel: ErrControlLoopFailed,
				Message:  "job status not yet available",
			}
		}

		return s.withTaskProvenance(ctx, tenantID, status)
	}

	// Fallback: job not in memory (finished and cleaned up), try KV store.
	if s.kvStore != nil {
		status, kvErr := s.loadJobStatus(tenantID, jobID)
		if kvErr != nil {
			s.logger.Warnf("orchestration: KV fallback for job %s failed: %s", jobID, kvErr.Error())
			return nil, &JobNotFoundError{JobID: jobID}
		}
		return s.withTaskProvenance(ctx, tenantID, status)
	}

	return nil, &JobNotFoundError{JobID: jobID}
}

// ListJobs returns the tenant's jobs matching filter, sorted by start time
// descending (most recent first). It merges the loops running here with the
// records every concluded job left behind.
//
// The merge is what makes a task's attempt history readable: a job that has
// returned is no longer in the registry, and its record is the only thing
// left that says it ran at all.
func (s *agentServiceImpl) ListJobs(ctx context.Context, tenantID string, filter *JobFilter) ([]*JobStatus, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}

	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to list jobs",
		}
	}

	// Collect jobs from in-memory running jobs map.
	seen := make(map[string]struct{})
	var jobs []*JobStatus

	prefix := tenantID + "/"
	s.jobs.Range(func(key, val interface{}) bool {
		keyStr, ok := key.(string)
		if !ok || !strings.HasPrefix(keyStr, prefix) {
			return true
		}

		job, ok := val.(*runningJob)
		if !ok {
			return true
		}

		status := job.run.Status()
		if status == nil {
			return true
		}

		// Seen regardless of the filter: a running job excluded by the
		// filter must still suppress its own stale persisted record, or a
		// wider listing would show the job twice.
		seen[status.JobID] = struct{}{}
		if filter.Matches(status) {
			jobs = append(jobs, status)
		}
		return true
	})

	// Merge persisted jobs from KV store.
	if s.kvStore != nil {
		scanCtx, cancel := jobRecordScope(ctx)
		defer cancel()
		kvPrefix := jobPersistencePrefix(tenantID)
		scanErr := s.kvStore.Scan(scanCtx, kvPrefix, func(key string, value []byte) error {
			var status JobStatus
			if unmarshalErr := json.Unmarshal(value, &status); unmarshalErr != nil {
				s.logger.Warnf("orchestration: failed to unmarshal persisted job %s: %s", key, unmarshalErr.Error())
				return nil
			}
			if _, exists := seen[status.JobID]; exists {
				return nil
			}
			if filter.Matches(&status) {
				jobs = append(jobs, &status)
			}
			return nil
		})
		if scanErr != nil {
			s.logger.Warnf("orchestration: KV scan for tenant %s jobs failed: %s", tenantID, scanErr.Error())
		}
	}

	// Sort by start time descending (most recent first).
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].StartedAt > jobs[j].StartedAt
	})

	return jobs, nil
}

// CancelJob cancels a running orchestration job and removes it from the jobs map.
func (s *agentServiceImpl) CancelJob(ctx context.Context, tenantID, jobID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if tenantID == "" {
		return &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to cancel a job",
		}
	}
	if jobID == "" {
		return &AgentServiceError{
			Sentinel: ErrTaskIDRequired,
			Message:  "job ID is required to cancel a job",
		}
	}

	jobKey := tenantID + "/" + jobID
	val, ok := s.jobs.Load(jobKey)
	if !ok {
		return &JobNotFoundError{JobID: jobID}
	}

	job, ok := val.(*runningJob)
	if !ok {
		return &AgentServiceError{
			Sentinel: ErrControlLoopFailed,
			Message:  "invalid job record type",
		}
	}

	job.cancel()
	s.jobs.Delete(jobKey)

	// The lease is released here, synchronously, rather than left to the
	// loop's exit: a cancelled job must not hold its work against a
	// re-dispatch for the rest of a TTL.
	job.run.releaseLease()

	// Clean up event subscribers for the cancelled job.
	s.cleanupJobSubscribers(tenantID, jobID)

	// Persist final cancelled status (best-effort).
	if s.kvStore != nil {
		cancelledStatus := &JobStatus{
			JobID:     jobID,
			TenantID:  tenantID,
			State:     JobStateFailed,
			Error:     "cancelled by user",
			UpdatedAt: time.Now().UnixNano(),
		}
		// Preserve existing status fields if available.
		if existing := job.run.Status(); existing != nil {
			cancelledStatus.Iteration = existing.Iteration
			cancelledStatus.TaskCount = existing.TaskCount
			cancelledStatus.Completed = existing.Completed
			cancelledStatus.Failed = existing.Failed
			cancelledStatus.Description = existing.Description
			cancelledStatus.StartedAt = existing.StartedAt
		}
		s.persistJobStatus(tenantID, cancelledStatus)
	}

	return nil
}

// =============================================================================
// Job Event Subscriptions
// =============================================================================

// SubscribeJobEvents returns a channel of real-time events for a running job.
// The channel is closed when the context is cancelled or the job completes.
func (s *agentServiceImpl) SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *JobEvent, error) {
	if err := s.validateTenant(ctx); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrTenantIDRequired,
			Message:  "tenant ID is required to subscribe to job events",
		}
	}
	if jobID == "" {
		return nil, &AgentServiceError{
			Sentinel: ErrJobNotFound,
			Message:  "job ID is required",
		}
	}

	// Verify the job exists in memory or in the KV store.
	jobKey := tenantID + "/" + jobID
	if _, ok := s.jobs.Load(jobKey); !ok {
		if s.kvStore != nil {
			if _, kvErr := s.loadJobStatus(tenantID, jobID); kvErr != nil {
				return nil, &JobNotFoundError{JobID: jobID}
			}
		} else {
			return nil, &JobNotFoundError{JobID: jobID}
		}
	}

	sub := &eventSubscriber{
		ch:   make(chan *JobEvent, 64),
		done: make(chan struct{}),
	}

	// Add subscriber to the map using a CAS loop for lock-free append.
	for {
		val, loaded := s.eventSubscribers.Load(jobKey)
		var existing *subscriberList
		if loaded {
			existing, _ = val.(*subscriberList)
		}
		var oldSubs []*eventSubscriber
		if existing != nil {
			oldSubs = existing.subs
		}
		newList := &subscriberList{
			subs: make([]*eventSubscriber, len(oldSubs)+1),
		}
		copy(newList.subs, oldSubs)
		newList.subs[len(oldSubs)] = sub
		if loaded {
			if s.eventSubscribers.CompareAndSwap(jobKey, val, newList) {
				break
			}
		} else {
			if _, swapped := s.eventSubscribers.LoadOrStore(jobKey, newList); !swapped {
				break
			}
		}
	}

	// Clean up subscriber when context is cancelled or job completes.
	go func() {
		select {
		case <-ctx.Done():
		case <-sub.done:
		}
		s.removeSubscriber(jobKey, sub)
		close(sub.ch)
	}()

	return sub.ch, nil
}

// removeSubscriber removes a subscriber from the per-job subscriber list.
func (s *agentServiceImpl) removeSubscriber(jobKey string, target *eventSubscriber) {
	for {
		val, ok := s.eventSubscribers.Load(jobKey)
		if !ok {
			return
		}
		sl, ok := val.(*subscriberList)
		if !ok {
			return
		}
		var filtered []*eventSubscriber
		for _, sub := range sl.subs {
			if sub != target {
				filtered = append(filtered, sub)
			}
		}
		if len(filtered) == 0 {
			if s.eventSubscribers.CompareAndDelete(jobKey, val) {
				return
			}
		} else {
			newList := &subscriberList{subs: filtered}
			if s.eventSubscribers.CompareAndSwap(jobKey, val, newList) {
				return
			}
		}
	}
}

// cleanupJobSubscribers closes all subscriber done channels for a completed job
// and removes the subscriber list from the map.
func (s *agentServiceImpl) cleanupJobSubscribers(tenantID, jobID string) {
	jobKey := tenantID + "/" + jobID
	val, ok := s.eventSubscribers.LoadAndDelete(jobKey)
	if !ok {
		return
	}
	sl, ok := val.(*subscriberList)
	if !ok {
		return
	}
	for _, sub := range sl.subs {
		select {
		case <-sub.done:
		default:
			close(sub.done)
		}
	}
}
