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
	"errors"
	"maps"
	"slices"
	"strings"
)

// =============================================================================
// Spawning by Agent reference, with failover
// =============================================================================
//
// A SPAWN NAMING AN AGENT OR A POOL IS RESOLVED HERE, IN THE ONE PLACE EVERY
// SESSION STARTS. The job envelope, the REST verb and the lead agent all reach
// SpawnSession; resolving the reference anywhere else would give each of them
// its own failover policy, and they would drift.
//
// FAILOVER IS DECIDED BY THE REFUSAL, NOT BY A PROBE. Whether an Agent's
// accounts are spent, its credential expired or its provider rate-limiting is
// only known when the spawn asks the account resolver for a credential, and
// whether its backend is served is only known when the spawn is dispatched. So
// each Agent is spawned in turn and the refusal is classified
// (AgentFailoverReason): one the next Agent might not share moves on, anything
// else — a refused workspace, a malformed request, a policy denial — would be
// refused identically and stops the walk.

// SetAgentDirectory installs the tenant agent catalog spawns resolve against.
func (s *agentServiceImpl) SetAgentDirectory(directory *AgentDirectory) {
	s.agentDirectory = directory
}

// AgentDirectory returns the installed agent catalog, or nil.
func (s *agentServiceImpl) AgentDirectory() *AgentDirectory {
	return s.agentDirectory
}

// AgentDirectoryAccessor is implemented by an agent service that holds the
// tenant agent catalog.
type AgentDirectoryAccessor interface {
	AgentDirectory() *AgentDirectory
}

// spawnByAgentRef resolves the request's Agent reference and spawns the first
// Agent that takes the work.
func (s *agentServiceImpl) spawnByAgentRef(ctx context.Context, req *SpawnSessionRequest) (*SpawnSessionResponse, error) {
	ref := req.AgentRef()
	if err := ref.Validate("agent"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Backend) != "" || strings.TrimSpace(req.Model) != "" {
		return nil, &InputValidationError{
			Field:   "backend",
			Message: "an agent reference supplies the backend and the model; name the agent or the backend, not both",
		}
	}
	if s.agentDirectory == nil {
		return nil, &AgentCatalogUnavailableError{Ref: ref}
	}
	// The spec the caller's workspace carries is wiped however the walk ends;
	// each attempt spawns with its own copy, because a spawn wipes the spec it
	// was handed and the next Agent would otherwise find it empty.
	if req.Workspace != nil {
		defer req.Workspace.Spec.Wipe()
	}
	tenantID := normalizeStoreTenant(TenantFromContext(ctx))
	admitted, attempts, err := s.agentDirectory.Resolve(ctx, tenantID, ref, req.Role)
	if err != nil {
		return nil, err
	}
	var last error
	for _, agent := range admitted {
		resp, err := s.spawnAsAgent(ctx, tenantID, req, agent)
		if err == nil {
			s.agentDirectory.auditFailovers(ctx, tenantID, ref, attempts, agent.ID)
			resp.ResolvedAgentID = agent.ID
			resp.Failovers = attempts
			return resp, nil
		}
		reason, failover := AgentFailoverReason(err)
		if !failover {
			return nil, err
		}
		attempts = append(attempts, AgentAttempt{AgentID: agent.ID, Reason: reason, Detail: err.Error()})
		last = err
	}
	s.agentDirectory.auditFailovers(ctx, tenantID, ref, attempts, "")
	// ONE AGENT, ONE REFUSAL: THE REFUSAL ITSELF. A caller naming a single
	// Agent classifies the spawn's failure exactly as it would a backend's —
	// an account limit parks the attempt, a workspace fault goes to a person —
	// and wrapping it would only add a layer to read through.
	if !ref.IsPool() && len(attempts) == 1 && last != nil {
		return nil, last
	}
	return nil, &AgentPoolExhaustedError{Ref: ref, Attempts: attempts, Last: last}
}

// spawnAsAgent spawns one Agent of a reference: its own spend limits first,
// then the request it resolves to. Either refusal is classified by the caller.
func (s *agentServiceImpl) spawnAsAgent(ctx context.Context, tenantID string, req *SpawnSessionRequest, agent Agent) (*SpawnSessionResponse, error) {
	if err := s.checkAgentSpend(ctx, tenantID, agent); err != nil {
		return nil, err
	}
	candidate, err := req.forAgent(agent)
	if err != nil {
		return nil, err
	}
	return s.SpawnSession(ctx, candidate)
}

// forAgent returns the request one Agent of a reference spawns with: a copy
// carrying the Agent's backend, model, image, arguments, account policy and —
// when the request names none — its system prompt (see role_charter.go for
// the order), with the reference cleared so the copy spawns directly.
func (r *SpawnSessionRequest) forAgent(agent Agent) (*SpawnSessionRequest, error) {
	candidate := *r
	candidate.AgentID, candidate.PoolID = "", ""
	candidate.resolvedAgentID = agent.ID
	candidate.Backend = agent.Backend
	candidate.Model = agent.Model
	candidate.ExtraArgs = append(slices.Clone(r.ExtraArgs), agent.ExtraArgs()...)
	candidate.Env = maps.Clone(r.Env)
	if image := strings.TrimSpace(agent.Image); image != "" && strings.TrimSpace(candidate.ContainerImage) == "" {
		candidate.ContainerImage = image
		candidate.SandboxMode = SandboxModeRequired
	}
	if strings.TrimSpace(candidate.SystemPrompt) == "" {
		prompt, err := agent.resolvePrompt(r.Role)
		if err != nil {
			return nil, err
		}
		candidate.SystemPrompt = prompt
	}
	if strategy := strings.TrimSpace(agent.Account.Strategy); strategy != "" && candidate.AccountID == "" {
		candidate.AccountAuto = true
		candidate.AccountStrategy = strategy
	}
	if len(agent.Account.Accounts) > 0 && candidate.AccountID == "" {
		candidate.AccountAuto = true
		candidate.AccountAllow = slices.Clone(agent.Account.Accounts)
	}
	if r.Workspace != nil {
		candidate.Workspace = r.Workspace.clone()
	}
	if r.prepareCandidate != nil {
		r.prepareCandidate(&candidate)
	}
	candidate.prepareCandidate = nil
	return &candidate, nil
}

// clone copies a workspace, spec bytes included, so a spawn that wipes its
// copy leaves the original for the next attempt.
func (w *SessionWorkspace) clone() *SessionWorkspace {
	if w == nil {
		return nil
	}
	copied := *w
	if w.Spec != nil {
		spec := *w.Spec
		spec.Files = make(map[string][]byte, len(w.Spec.Files))
		for name, content := range w.Spec.Files {
			spec.Files[name] = slices.Clone(content)
		}
		copied.Spec = &spec
	}
	return &copied
}

// auditFailovers records each Agent a spawn passed over, naming the one it
// moved to next ("none" when no Agent took the work). Every switch is on the
// audit trail, whether or not the walk ended in a session.
func (d *AgentDirectory) auditFailovers(ctx context.Context, tenantID string, ref AgentRef, attempts []AgentAttempt, chosen string) {
	for i, attempt := range attempts {
		next := chosen
		if i+1 < len(attempts) {
			next = attempts[i+1].AgentID
		}
		if next == "" {
			next = "none"
		}
		emitStoreAudit(ctx, d.auditor, StoreAuditEvent{
			TenantID:   tenantID,
			Resource:   AuditResourceAgentDefinition,
			ResourceID: agentFailoverResourceID(ref, attempt, next),
			Action:     AuditActionAgentFailover,
			Success:    chosen != "",
		})
	}
}

// =============================================================================
// A job's lead
// =============================================================================

// resolvedLead is the lead a job starts with: the reference it names and what
// that reference resolved to when the job started.
type resolvedLead struct {
	Ref     AgentRef
	Backend string
	Model   string
}

// jobLead decides a job's lead: the request's reference, else the project's
// default lead. A job with neither leads with whatever its task's routing or
// its workflow's lead role names, exactly as a job always has.
//
// THE RESOLUTION IS THE JOB'S RECORD, NOT ITS SPAWN. The backend recorded here
// is the one the job's lease is counted against, so it is the first Agent of
// the reference with capacity — or, when every one is full, the first Agent,
// on whose backend the job then waits. The lead session itself is spawned by
// reference and fails over at that moment.
func (s *agentServiceImpl) jobLead(ctx context.Context, tenantID string, req *StartJobRequest) (resolvedLead, error) {
	ref := req.Lead
	if err := ref.Validate("lead"); err != nil {
		return resolvedLead{}, err
	}
	if ref.IsZero() {
		projectLead, err := s.projectDefaultLead(ctx, tenantID, req.ProjectID)
		if err != nil {
			return resolvedLead{}, err
		}
		ref = projectLead
	}
	if ref.IsZero() {
		return resolvedLead{Backend: req.routedBackend}, nil
	}
	if s.agentDirectory == nil {
		return resolvedLead{}, &AgentCatalogUnavailableError{Ref: ref}
	}
	catalogue, err := s.agentDirectory.Catalogue(ctx, tenantID)
	if err != nil {
		return resolvedLead{}, err
	}
	candidates, err := catalogue.Candidates(ref)
	if err != nil {
		return resolvedLead{}, err
	}
	admitted, _, err := s.agentDirectory.resolver.Admit(ctx, tenantID, candidates, AgentRoleLead)
	if err != nil {
		return resolvedLead{}, err
	}
	chosen := candidates[0]
	if len(admitted) > 0 {
		chosen = admitted[0]
	}
	return resolvedLead{Ref: ref, Backend: chosen.Backend, Model: chosen.Model}, nil
}

// projectDefaultLead reads a project's default lead; the zero reference when
// the project names none or no project store is wired.
func (s *agentServiceImpl) projectDefaultLead(ctx context.Context, tenantID, projectID string) (AgentRef, error) {
	if s.projectStore == nil || projectID == "" {
		return AgentRef{}, nil
	}
	project, err := s.projectStore.Get(ctx, tenantID, projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return AgentRef{}, nil
		}
		return AgentRef{}, err
	}
	return project.DefaultLeadAgent, nil
}
