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

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// ErrNoClaimArbiter reports a clustered boot whose task-claim arbitration
// could not be wired.
//
// It is fatal rather than degradable, for the reason schedulerClustering gives
// about the scheduler: a clustered node that quietly settled for process-local
// arbitration would decide claims with a lock the other nodes cannot see, and
// every node would award itself the same task. Refusing to start is the only
// safe answer, because the failure is invisible at runtime — every node reports
// success while doing the same work.
var ErrNoClaimArbiter = errors.New("orchestration: clustered mode requires the transaction plane to arbitrate task claims")

// ErrNoJobLeaseStore reports a clustered boot whose job leases would have no
// transaction plane to be decided on. Fatal for the reason ErrNoClaimArbiter
// is: a fleet whose leases were decided in one process would admit one job
// per node.
var ErrNoJobLeaseStore = errors.New("orchestration: clustered mode requires the transaction plane to decide job leases")

// taskClaimArbiter builds the arbiter that decides which node claims a task.
//
// The choice is made from the DEPLOYMENT MODE, not from whichever handles
// happen to be present, and that distinction is the whole point. The mode is
// what says whether a second node can exist; the handles only say what this
// process managed to wire. Choosing from the handles would mean a clustered
// node with a wiring fault silently arbitrating in-process, which is the defect
// this whole change removes.
//
// Both branches are correct for their domain rather than one being a fallback
// for the other:
//
//   - Single-node modes have exactly one process, so the task store's own
//     per-key mutex has already serialized every contender that can exist. The
//     process arbiter is the complete answer there, not a degraded one. It is
//     also the ONLY answer available: go-qrdb gates the transaction plane
//     behind clustered mode under its run-modes split, so there is no
//     transaction to arbitrate with on a standalone boot.
//   - Cluster-like modes must reach the transaction plane, and a failure to do
//     so is fatal.
//
// The predicate is the same config.IsClusterLikeMode the scheduler's clustering
// uses, shared rather than restated so a mode added later cannot be clustered
// for one subsystem and standalone for the other.
func (s *Server) taskClaimArbiter() (orchestration.TaskClaimArbiter, error) {
	if !config.IsClusterLikeMode(s.getEffectiveMode()) {
		return orchestration.NewProcessClaimArbiter(), nil
	}

	if s.qrdbClient == nil {
		return nil, ErrNoClaimArbiter
	}
	client, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrNoClaimArbiter
	}
	return orchestration.NewTransactionClaimArbiter(client.Transaction(), client.KV())
}

// startOrchestration initializes the agent orchestration service and attaches
// it to the QuicBaaS facade. The orchestration service provides multi-agent
// coordination, task management, and session lifecycle management.
func (s *Server) startOrchestration() error {
	s.logger.Info("Initializing agent orchestration service...")

	// Durable orchestration state binds to the tenant-routed KVStore, the same
	// seam the IAM stores use: records are written inside the owning tenant's
	// barrier (own DEK + namespace) rather than a shared table, and they
	// survive a restart. Session records live here too — they are what makes a
	// session addressable again after the daemon that hosted it is gone.
	orchKV := s.durableKVStore()
	if orchKV == nil {
		orchKV = kvstore.NewMemory()
		s.logger.Warn("No qrdb storage attached; orchestration state is in-memory and will not survive a restart")
	}
	// The audit sink every orchestration store — and every credential delivery
	// grant — is recorded on. Named rather than inlined because the delivery
	// leaser takes it directly as well as through the option list.
	storeAuditor := newOrchestrationStoreAuditor(s.auditLoggerForStores)
	orchStoreOpts := []orchestration.StoreOption{
		orchestration.WithStoreAuditor(storeAuditor),
	}

	store := orchestration.NewAgentStore(orchKV, orchStoreOpts...)
	backends := make(map[string]orchestration.AgentBackend)

	// The observer the supervised-process exits are reported through. Bound to
	// the agent service below, once that service exists — see
	// deferredSessionExitRecorder.
	exitRecorder := &deferredSessionExitRecorder{logger: s.logger}

	// Register Ollama backend if the Ollama service is available.
	if s.ollamaService != nil {
		chatModel := s.config.Orchestration.OllamaChatModel
		if chatModel == "" {
			chatModel = "llama3"
		}
		client := ollama.NewClient(ollama.ClientConfig{
			BaseURL:    s.ollamaService.BaseURL(),
			MaxRetries: 2,
			Timeout:    10 * time.Minute, // Large models need time for cold start + inference.
		})
		chatSvc := ollama.NewChatService(client, chatModel)

		backends["ollama"] = orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
			ChatService:  chatSvc,
			DefaultModel: chatModel,
		})
		s.logger.Infof("Ollama backend registered for orchestration (chat model: %s)", chatModel)
	}

	// Register Claude Code backend (persistent stream-json CLI mode).
	// Claude Code uses the stream-json protocol for persistent sessions
	// where a single long-running process handles multiple prompts with
	// full conversation context across turns.
	backends["claude"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command: "claude",
		// --no-session-persistence is NOT listed here: it is a resume-policy
		// decision the backend applies from NativeSessionResume.
		Args:                []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--disable-slash-commands"},
		DefaultModel:        orchestration.DefaultClaudeModel, // The coding tier; the CLI's own default is a Sonnet model (go-quicbaas#1012).
		TeamMode:            s.config.Orchestration.ClaudeTeamMode,
		CLIMode:             true,
		NativeSessionResume: s.config.Orchestration.ClaudeNativeSessionResume,
		ProcessExitObserver: s.recordAgentProcessExit(exitRecorder),
	})
	s.logger.Info("Claude backend registered for orchestration")

	// Register Codex backend. The codex CLI does not implement ACP/JSON-RPC
	// over stdio, so we use the dedicated CodexExecBackend that wraps
	// `codex exec` per Prompt call. Each review/coding turn is a fresh
	// stateless invocation, which fits the reviewer-agent pattern where
	// every review independently evaluates the current workspace state.
	// DefaultModel is left empty so codex uses the model configured in
	// ~/.codex/config.toml. Hardcoding a model here would fail for users
	// whose codex account/plan does not have access to it.
	backends["codex"] = orchestration.NewCodexExecBackend(orchestration.CodexExecBackendConfig{
		Command: "codex",
	})
	s.logger.Info("Codex backend registered for orchestration (codex exec mode)")

	// Register OpenCode backend (ACP JSON-RPC over stdio).
	// OpenCode v1.3+ supports the ACP protocol via the "acp" subcommand.
	opencodePath := "opencode"
	if home, _ := os.UserHomeDir(); home != "" {
		candidate := home + "/.opencode/bin/opencode"
		if _, statErr := os.Stat(candidate); statErr == nil {
			opencodePath = candidate
		}
	}
	backends["opencode"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:      opencodePath,
		Args:         []string{"acp", "--port", "0"},
		DefaultModel: "ollama/llama3",
		// OpenCode's credential is a file. This was hardcoded inside the
		// backend's sandbox spec; it is stated by the construction now so the
		// backend holds no CLI's name. Same paths, same read-only mount.
		CredentialDir: orchestration.CredentialDirSpec{
			Rel:    ".config/opencode",
			Target: "/home/agent/.config/opencode",
		},
	})
	s.logger.Infof("OpenCode backend registered for orchestration (command: %s)", opencodePath)

	for name, backend := range s.opts.agentBackends {
		backends[name] = backend
		s.logger.Infof("Host agent backend registered for orchestration: %s", name)
	}

	logAdapter := &logger.LoggerAdapter{Logger: s.logger}

	agentSvc, err := orchestration.NewAgentService(store, backends, logAdapter)
	if err != nil {
		s.logger.Warnf("Agent orchestration service initialization failed: %v", err)
		return nil // Non-fatal: orchestration is optional
	}

	// Close the loop: an observed process exit now reaches the service that
	// owns every surface the session's state lives on.
	if recorder, ok := agentSvc.(sessionExitRecorder); ok {
		exitRecorder.set(recorder)
	} else {
		s.logger.Warn("Agent service does not record session exits; a crashed agent will be reported but not closed")
	}

	// Wire task store, tenant guard, agent registry, container runtime,
	// KV store for job persistence, and lead agent session.
	wirer, ok := agentSvc.(interface {
		SetTaskStore(store orchestration.TaskStore)
		SetTenantGuard(guard *orchestration.TenantGuard)
		SetContainerRuntime(runtime orchestration.ContainerRuntime)
		SetFleetRegistry(registry orchestration.FleetRegistry)
		SetLeadAgent(config *orchestration.LeadAgentConfig)
		SetLeadSessionID(id string)
		SetKVStore(kv interfaces.KVStore)
		SetWorkflowLoader(wl *orchestration.WorkflowLoader)
		SetPromptResolver(resolver orchestration.PromptResolver)
		SetSessionEventLog(log orchestration.SessionEventLog)
		SetProjectAuthz(authz *orchestration.ProjectAuthz)
		SetProjectStore(store orchestration.ProjectStore)
		SetMountPolicy(policy *orchestration.MountPolicy)
		SetWorkspaceProvisioner(p orchestration.WorkspaceProvisioner)
		SetTrustPolicy(policy orchestration.TrustPolicy)
		SetTranscriptReplayBudget(budget int)
		SetCostTracker(tracker orchestration.CostTracker)
		SetConsumptionLedger(ledger orchestration.ConsumptionLedger)
		SetPriceBook(prices *orchestration.PriceBook)
		SetTenantSpendPolicyStore(store orchestration.TenantSpendPolicyStore)
		SetCostSettingsStore(store orchestration.CostSettingsStore)
		SetFleetTelemetryStore(store *orchestration.FleetTelemetryStore)
		SetAccountStore(store orchestration.AccountStore)
		SetAccountCredentialGenerations(reader orchestration.AccountCredentialGenerationReader)
		SetAccountCredentialLeaser(leases orchestration.AccountCredentialLeaser)
		SetJobLifecyclePolicy(policy orchestration.JobLifecyclePolicy)
		SetNode(node string)
		SetJobLeaseStore(store orchestration.JobLeaseStore, ttl time.Duration)
		SetIdleThresholds(thresholds orchestration.JobIdleThresholds)
		SetFleetHeartbeatTimeout(timeout time.Duration)
		SetTenantLister(tenants orchestration.TenantLister)
		SetTenantResolver(tenants orchestration.TenantResolver)
	})
	if ok {
		// The operator's part of every job's lifecycle: the command allowlist
		// the command validator enforces and the bound on each lifecycle
		// moment. Built from daemon configuration — never tenant-writable —
		// and handed to every control loop through the service. An allowlist
		// the guard refuses is fatal: a node that silently ran with no
		// allowlist would refuse every command, and one that silently ran
		// with a malformed one might not.
		lifecyclePolicy, policyErr := s.jobLifecyclePolicy()
		if policyErr != nil {
			return policyErr
		}
		wirer.SetJobLifecyclePolicy(lifecyclePolicy)
		if err := s.renderWorkerSettings(); err != nil {
			return err
		}

		claimArbiter, arbiterErr := s.taskClaimArbiter()
		if arbiterErr != nil {
			return arbiterErr
		}
		taskStore := orchestration.NewTaskStore(orchKV, claimArbiter, orchStoreOpts...)
		s.orchTaskStore = taskStore
		wirer.SetTaskStore(taskStore)
		wirer.SetTenantGuard(orchestration.NewTenantGuard(logAdapter))

		// Wire project-level authorization so ClaimTask enforces the
		// per-project FleetNodeRoles matrix when a task is scoped to a project.
		// Uses the in-process project store from the QuicBaaS facade when
		// available; falls back to a fresh in-memory store (standalone mode).
		var projectStore orchestration.ProjectStore
		if s.quicBaaSService != nil {
			if psvc, storeOK := s.quicBaaSService.Project().(interface {
				Store() orchestration.ProjectStore
			}); storeOK {
				projectStore = psvc.Store()
			}
		}
		if projectStore == nil {
			projectStore = orchestration.NewProjectStore(orchKV, orchStoreOpts...)
		}
		wirer.SetProjectAuthz(orchestration.NewProjectAuthz(projectStore))
		s.logger.Info("Project authorization attached to orchestration service")

		// Managed project workspaces: spawn resolves a managed project's work
		// directory from the SAME durable store CreateProject provisions into,
		// and refuses to start until provisioning is ready. Extra sandbox
		// mounts are validated against the projects root + operator allowlist
		// (deny-by-default; an unwired policy denies everything).
		projectsRoot := s.config.Orchestration.EffectiveProjectsRoot(s.config.Storage.DataDir)
		socketsRoot, socketsErr := s.socketsRoot()
		if socketsErr != nil {
			return socketsErr
		}
		wirer.SetProjectStore(projectStore)
		// The daemon's own sockets root is admitted BY CONSTRUCTION, not by
		// an allowlist entry: the sdlc runner binds each attempt's signing
		// socket there because a unix socket address does not fit under a
		// managed workspace, and the grant is only the socket itself, read
		// only (see MountPolicy.SocketsRoot).
		mountPolicy := &orchestration.MountPolicy{
			ProjectsRoot: projectsRoot,
			AllowedRoots: s.config.Orchestration.Sandbox.AllowedMountRoots,
			SocketsRoot:  socketsRoot,
		}
		wirer.SetMountPolicy(mountPolicy)

		// Trust axis (orchestration.trust_policy): the integrity floor every
		// spawn resolves TrustLevelDefault against. An unparsable value is
		// FATAL rather than a warning — falling back would install the
		// weakest policy under an operator who asked for the strictest.
		trustPolicy, trustErr := orchestration.ParseTrustPolicy(s.config.Orchestration.TrustPolicy)
		if trustErr != nil {
			return trustErr
		}
		wirer.SetTrustPolicy(trustPolicy)
		s.logger.Infof("Agent trust policy: %s (integrity floor)", trustPolicy.String())
		// One manager over the projects root serves both halves of it: the
		// managed project clones CreateProject provisions, and the named job
		// workspaces a spawn on this daemon's own backend asks for
		// (go-quicbaas#981). A root the manager refuses disables both, and a
		// spawn naming a workspace is then refused rather than placed
		// somewhere the mount policy never admitted.
		workspaces, wsErr := orchestration.NewWorkspaceManager(projectsRoot)
		s.sessionWorkspaces = nil
		if wsErr != nil {
			s.logger.Warnf("Managed project workspaces disabled: %v", wsErr)
		} else {
			s.sessionWorkspaces = workspaces
			// The spec directories an in-process session of a previous run
			// left behind each hold a forge token; no session survived the
			// restart to release them, so they go before anything spawns.
			// The same sweep removes the workspace of every concluded task.
			workspaces.SetConclusions(orchestration.TaskWorkspaceConclusions{Store: taskStore})
			// Removal is checked against the same policy the workspace was
			// admitted under as a bind-mount source (go-quicbaas#777).
			workspaces.SetMountPolicy(mountPolicy)
			specs, sweepErr := workspaces.SweepSessionSpecs()
			if sweepErr != nil {
				return sweepErr
			}
			if len(specs) > 0 {
				s.logger.Infof("Removed %d workspace spec(s) and concluded workspace(s) a previous run left behind", len(specs))
			}
			wirer.SetWorkspaceProvisioner(workspaces)
			// Workspaces pinned to fleet nodes are reconciled on the same
			// tick, through each node's RemoveWorkspace RPC.
			remote, _ := agentSvc.(remoteWorkspaceReconciler)
			s.startWorkspaceSweep(workspaces, remote)
			if wireErr := s.wireProjectWorkspaces(projectStore, workspaces); wireErr != nil {
				s.logger.Warnf("Managed project workspaces disabled: %v", wireErr)
			}
		}
		// Reuse the registry created during IAM init so enrollment-time
		// registrations are immediately visible to dispatch. Builds one over
		// the same durable store only if IAM init never ran.
		if s.fleetRegistry == nil {
			s.fleetRegistry = orchestration.NewFleetRegistry(orchKV, orchStoreOpts...)
		}
		wirer.SetFleetRegistry(s.fleetRegistry)
		s.logger.Info("Fleet registry attached to orchestration service")

		// Rebind the FleetAdminService onto the SAME shared fleet registry the
		// orchestrator just received. The facade builds FleetAdmin with a private
		// default registry at construction time; without this rebind, disable /
		// enable resolve agents in an empty store and every fleet-registered agent
		// surfaces as 404. FleetAdmin() returns the interface, so type-assert to
		// the concrete setter (no-op for alternate implementations).
		if s.quicBaaSService != nil {
			if rebinder, rebindOK := s.quicBaaSService.FleetAdmin().(interface {
				SetFleetRegistry(orchestration.FleetRegistry)
			}); rebindOK {
				rebinder.SetFleetRegistry(s.fleetRegistry)
				s.logger.Info("Agent admin service bound to shared fleet registry")
			}
			// A project's host affinity is verified against the same fleet.
			if fleetWirer, fleetOK := s.quicBaaSService.Project().(interface {
				SetFleetNodes(orchestration.FleetNodeLister)
			}); fleetOK {
				fleetWirer.SetFleetNodes(s.fleetRegistry)
			}
		}

		// Wire workflow loader backed by the QuicBaaS internal KV store. Workflow
		// definitions are persisted under workflowKeyPrefix on shard 1 via the
		// qrdb SDK, exactly like every other orchestration entity, so they
		// replicate across raft peers and survive daemon restarts. The loader
		// rehydrates from KV at boot.
		wl := orchestration.NewWorkflowLoader()
		if s.qrdbClient != nil {
			if kvClient, kvErr := s.qrdbClient.ForShard(1); kvErr != nil {
				s.logger.WithFields(map[string]interface{}{
					"error": kvErr.Error(),
				}).Warn("Failed to obtain qrdb shard client for workflow persistor; workflows will be in-memory only")
			} else {
				persistor := newKVWorkflowPersistor(kvClient, 1)
				wl.SetPersistor(persistor)
				if n, rerr := wl.RehydrateAll(context.Background()); rerr != nil {
					s.logger.WithFields(map[string]interface{}{
						"error": rerr.Error(),
					}).Warn("Failed to rehydrate workflow definitions from KV; starting empty")
				} else if n > 0 {
					s.logger.WithFields(map[string]interface{}{"count": n}).Infof("Rehydrated %d workflow definition(s) from internal KV store", n)
				}
			}
		}
		wirer.SetWorkflowLoader(wl)
		s.logger.Info("Workflow loader attached to orchestration service")

		// Every job resolves its prompt set through the tenant prompt store;
		// without one (no embedded node) jobs render the embedded defaults.
		resolver, err := s.promptResolver()
		if err != nil {
			return err
		}
		wirer.SetPromptResolver(resolver)

		// The project service verifies a Project.DefaultWorkflow against the
		// same tenant-scoped registry, so a project can never name a workflow
		// its tenant does not have.
		if s.quicBaaSService != nil {
			if resolverWirer, resolverOK := s.quicBaaSService.Project().(interface {
				SetWorkflowResolver(service.WorkflowResolver)
			}); resolverOK {
				resolverWirer.SetWorkflowResolver(wl)
				s.logger.Info("Workflow registry attached to project service for default-workflow validation")
			}
		}

		// Wire KV store for durable job persistence. Binds to the same
		// tenant-routed durable store as tasks/projects/agents (see orchKV
		// below) so job records and, in particular, the session event log
		// (conversation transcripts) survive a daemon restart instead of
		// evaporating with the process. Falls back to an in-memory store only
		// when no qrdb storage is attached (storage-less dev boot).
		jobKVStore := orchKV
		wirer.SetKVStore(jobKVStore)
		s.logger.Info("KV store attached to orchestration service for job persistence")

		// Durable job admission: this node's name, a lease store for the
		// deployment's consistency domain wrapped in the project capacity
		// check, and the stall detector's bounds. Wired BEFORE recovery so a
		// resumed job takes its lease under this node's name. An unknown
		// idle-threshold state is FATAL rather than dropped: a bound the
		// operator set and the engine ignored is the failure the detector
		// exists to remove.
		node := s.nodeName()
		leaseTTL := s.config.Orchestration.EffectiveReapLeaseTTL()
		leaseStore, leaseErr := s.jobLeaseStore(leaseTTL)
		if leaseErr != nil {
			return leaseErr
		}
		admitted, admitErr := orchestration.NewProjectCapacityJobLeaseStore(leaseStore, projectStore, leaseTTL)
		if admitErr != nil {
			return admitErr
		}
		// THE FLEET AXIS, ENFORCED AT THE ONE POINT EVERY JOB PASSES THROUGH.
		// The admission plan divides the fleet fairly once per sweep; this
		// catches what no sweep sees — a job started directly, an auto-spawned
		// task, a second node planning from its own reading — and it reads the
		// SAME capacity the plan reads, so the two agree by construction.
		s.orchFleetCapacity = s.orchestrationFleetCapacity()
		admitted, admitErr = orchestration.NewFleetCapacityJobLeaseStore(admitted, s.orchFleetCapacity, leaseTTL)
		if admitErr != nil {
			return admitErr
		}
		// THE BACKEND AXIS, for a model server several projects share. A
		// project quota bounds one project and the fleet quota bounds worker
		// slots; neither can say "this GPU serves two things at once", which
		// is the only limit a locally hosted model has. Reviews make it acute:
		// they are off the project budget by construction, so one project's
		// five green pull requests all get a review in the same pass.
		admitted, admitErr = orchestration.NewBackendCapacityJobLeaseStore(
			admitted, s.config.Orchestration.BackendConcurrency, leaseTTL)
		if admitErr != nil {
			return admitErr
		}
		idle, idleErr := orchestration.JobIdleThresholdsFrom(s.config.Orchestration.IdleThresholds)
		if idleErr != nil {
			return idleErr
		}
		s.orchJobLeaseStore = admitted
		s.orchLeaseTTL = leaseTTL
		wirer.SetNode(node)
		wirer.SetJobLeaseStore(admitted, leaseTTL)
		wirer.SetIdleThresholds(idle)
		// The age past which a registered agent's silence counts as absence.
		// It was configurable and unread until the session sweep needed it:
		// a session pinned to a daemon nothing has heard from is unreachable,
		// and the operator's own timeout is what says how long "nothing" is.
		wirer.SetFleetHeartbeatTimeout(s.config.Orchestration.HeartbeatTimeout)
		s.logger.Infof("Job admission attached to orchestration service (node=%s lease_ttl=%s)", node, leaseTTL)

		// Interrupted jobs are recovered by recoverOrchestrationJobs, once
		// every module has registered its hooks; see Server.Start.

		// The maintenance sweep: the reaper concludes jobs whose node died,
		// the stall detector ends jobs nothing is working. Both run on the
		// heartbeat cadence, a third of the lease TTL, so a dead node is
		// noticed within one TTL of its last heartbeat.
		if sweeper, sweepOK := agentSvc.(jobSweeper); sweepOK {
			s.startOrchestrationSweeps(sweeper, leaseTTL/3)
		}

		// The runtime agent sessions run in is chosen once and wired into the
		// orchestration service and every built-in ACP backend alike.
		containerRuntime, hostRuntime := s.agentContainerRuntime(func() (*orchestration.ExecContainerRuntime, error) {
			return orchestration.NewExecContainerRuntime(logAdapter)
		})
		if containerRuntime != nil {
			s.orchContainerRuntime = hostRuntime
			wirer.SetContainerRuntime(containerRuntime)
			s.logger.Info("Container runtime attached to orchestration service")

			// The shared per-tenant Go cache is the daemon's own mount, so no
			// grant of the request mount policy may reach it.
			goCache := orchestration.GoBuildCache{
				Root: s.config.Orchestration.Sandbox.EffectiveGoCacheDir(s.config.Storage.DataDir),
			}
			if cacheErr := goCache.Validate(mountPolicy); cacheErr != nil {
				return cacheErr
			}
			owner, ownerErr := orchestratorContainerOwner(s.config.Storage.DataDir)
			if ownerErr != nil {
				return ownerErr
			}
			wireSandboxedBackends(backends, containerRuntime, owner, goCache)

			// Nothing has spawned yet — jobs are recovered once every module
			// has registered — so every container carrying this owner label
			// is a previous run's orphan (go-quicbaas#750). An injected
			// runtime is not the host's, so there is nothing of ours to sweep
			// or reap.
			if hostRuntime != nil {
				sweep, sweepErr := hostRuntime.SweepOwned(context.Background(), owner)
				if sweepErr != nil {
					return sweepErr
				}
				if len(sweep.Removed) > 0 {
					s.logger.Infof("Swept %d orphaned worker container(s) left by a previous run", len(sweep.Removed))
				}
				if reapErr := s.startContainerReaper(hostRuntime, owner, backends); reapErr != nil {
					return reapErr
				}
			}
		}

		// Wire the per-tenant token ledger. It binds to the SAME tenant-routed,
		// durable orchKV as tasks/projects/agents/sessions, so usage records
		// are written inside the owning tenant's barrier (own DEK + namespace)
		// and survive a restart. Every ledger key is additionally namespaced by
		// tenant ID (see cost_tracker_store.go), so one tenant's usage can
		// neither be read nor influence account selection for another even when
		// a deployment routes both to one physical store.
		//
		// This is what makes the ledger real: agentServiceImpl.recordTurnUsage
		// charges every completed turn to it, and LeastUsageAccountStrategy
		// reads it back to balance a pool of provider accounts.
		costTracker := orchestration.NewCostTracker(orchKV, logAdapter)
		wirer.SetCostTracker(costTracker)
		s.logger.Info("Token cost counters attached to orchestration service (durable KV-backed, tenant-namespaced)")

		// HOW MANY SESSIONS EACH POOLED ACCOUNT SERVES RIGHT NOW, counted from
		// the session records every spawn writes, whichever host resolved it.
		// One lookup, three readers: the balancing strategy ranks on it and
		// enforces MaxConcurrentSessions with it, admission defers a pool whose
		// accounts are all at their cap, and the usage verb reports it to
		// worker daemons so they balance on the same count (go-quicbaas#1046).
		sessionCounter, sessionErr := orchestration.NewAccountSessionCounter(store)
		if sessionErr != nil {
			return sessionErr
		}

		// The CONSUMPTION LEDGER, on the same store and for a different job.
		// The counters above report; this one DECIDES — it is the append-only
		// record the spend authority reads before any pooled credential is
		// released, and the record an account's circuit breaker is derived
		// from. Both are fed from the single turn-recording site
		// (agentServiceImpl.recordTurnUsage), so a turn is either in both or
		// in neither and the dashboard can never contradict the cap.
		//
		// Guarded writes are what make it an authority rather than a tally:
		// the ledger refuses to record at all on a store that cannot decide a
		// contested create, so an unenforceable cap fails loudly here rather
		// than silently admitting everything at spawn time.
		consumption := orchestration.NewConsumptionLedger(orchKV)
		wirer.SetConsumptionLedger(consumption)

		// The PRICE BOOK every entry is stamped from when its turn is
		// recorded (orchestration.pricing). An absent table prices nothing
		// and every turn is recorded unpriced; a malformed one fails startup
		// rather than stamping figures from a table the operator did not
		// mean.
		prices, pricesErr := orchestration.NewPriceBook(s.config.Orchestration.Pricing)
		if pricesErr != nil {
			return pricesErr
		}
		wirer.SetPriceBook(prices)

		// THE LATE PRICING SWEEP: a scheduler system job that prices every
		// ledger entry recorded without a price once the table names its
		// model — the turns that ran before their model was priced. Without
		// a scheduler the entries stay unpriced and read as such.
		if pricer, pricerOK := agentSvc.(unpricedConsumptionPricer); pricerOK && s.quicBaaSService != nil {
			if engine := s.quicBaaSService.SchedulerEngine(); engine != nil {
				if err := registerLedgerPricing(engine, pricer); err != nil {
					return err
				}
				s.logger.Info("Ledger pricing sweep registered as a scheduler system job")
			} else {
				s.logger.Warn("Scheduler is not enabled; ledger entries recorded without a price stay unpriced")
			}
		}
		// Each tenant's own spend limits, in the tenant-routed orchestration
		// KV beside its accounts and projects (go-quicbaas#1058).
		tenantSpendPolicies := orchestration.NewTenantSpendPolicyStore(orchKV)
		wirer.SetTenantSpendPolicyStore(tenantSpendPolicies)

		// ADMISSION'S FOURTH INPUT: a project's and its tenant's own spend
		// limits, judged over the project and tenant quota planes this same
		// ledger writes (go-quicbaas#1058, ruling F6). A spent project or
		// tenant is deferred with a typed reason, on the audit trail when the
		// deferral starts.
		scopeSpend, scopeSpendErr := orchestration.NewScopeSpendAdmission(
			orchestration.NewQuotaAuthority(consumption), tenantSpendPolicies,
			orchestration.WithScopeSpendReporting(storeAuditor, logAdapter))
		if scopeSpendErr != nil {
			return scopeSpendErr
		}
		s.orchScopeSpend = scopeSpend

		// Each tenant's RUNTIME cost settings — its electricity rate and
		// engine draws — layered over the price table when a local turn is
		// costed from electricity. In the tenant-routed KV, beside the ledger.
		wirer.SetCostSettingsStore(orchestration.NewCostSettingsStore(orchKV))

		// FLEET DEVICE TELEMETRY, filed per node in the qrdb node's
		// time-series engine and integrated over each local turn's window to
		// cost it from what its device measurably drew (turn_energy.go). A
		// node built without time-series storage measures nothing: every
		// local turn is costed from its configured draw and marked estimated.
		if s.timeSeriesStorage != nil {
			series, seriesErr := newFleetTelemetrySeries(s.timeSeriesStorage,
				s.config.Orchestration.Pricing.EffectiveTelemetryRetention())
			if seriesErr != nil {
				return seriesErr
			}
			// Raw telemetry is retained for a bounded window. The policy is
			// set on every series already stored; a failure here is retried
			// on each tenant's next report, so it does not stop the node.
			if retentionErr := series.ApplyRetention(); retentionErr != nil {
				s.logger.Warn("Fleet device telemetry retention could not be applied at startup; it is retried on each tenant's next report: " + retentionErr.Error())
			}
			telemetry, telemetryErr := orchestration.NewFleetTelemetryStore(series)
			if telemetryErr != nil {
				return telemetryErr
			}
			wirer.SetFleetTelemetryStore(telemetry)
			s.logger.Info("Fleet device telemetry attached; local turns are costed from measured energy where a node reports it")
		} else {
			s.logger.Warn("Time-series storage is not configured; fleet device telemetry is dropped and local turn energy is estimated")
		}

		// The gate itself. It names this node as the holder of a breaker's
		// single half-open trial, so two daemons over one store cannot both
		// believe they are the one retrying a failing account.
		spendAuthority, spendErr := orchestration.NewSpendAuthority(consumption, orchKV, node)
		if spendErr != nil {
			return spendErr
		}
		s.logger.Info("Consumption ledger and spend authority attached (per-account quota windows and circuit breaker enforced at credential release)")

		// Bind the account service's usage reporting to the SAME ledger the
		// turn path writes to. Without this, AccountService.GetAccountUsage
		// reports "not configured" over every transport while the ledger it
		// should be reading is sitting right here.
		if s.quicBaaSService != nil {
			if usageWirer, usageOK := s.quicBaaSService.Account().(interface {
				SetUsageTracker(orchestration.CostTracker)
			}); usageOK {
				usageWirer.SetUsageTracker(costTracker)
				s.logger.Info("Account usage reporting bound to the orchestration cost ledger")
			}
			if sessionWirer, sessionOK := s.quicBaaSService.Account().(interface {
				SetSessionReserver(orchestration.AccountSessionReserver)
			}); sessionOK {
				sessionWirer.SetSessionReserver(sessionCounter)
			}
			// The REMOTE half of the cap. MaterializeAccountCredential hands
			// a live paid credential to a host the central does not run, so
			// an unmetered release there is the most expensive one in the
			// system: the worker caches the envelope and spends against it
			// without asking again. It is checked by the SAME authority the
			// in-process spawn is checked by, so the two cannot disagree
			// about whether an account is over its cap.
			if spendWirer, spendOK := s.quicBaaSService.Account().(interface {
				SetSpendAuthority(orchestration.SpendAuthority)
			}); spendOK {
				spendWirer.SetSpendAuthority(spendAuthority)
				s.logger.Info("Pooled credential release bound to the spend authority")
			}
		}

		// Wire the pooled provider account resolver (see
		// pkg/orchestration/account_resolver.go) so a spawn that sets
		// SpawnRequest.AccountID / AccountAuto can select and materialize a
		// pooled Anthropic/Claude, Codex, or OpenCode credential instead of
		// the daemon host's shared identity dirs. Backed by the same
		// tenant-routed, durable orchKV as tasks/projects/agents for
		// metadata, and by the platform's SecretsManager (StoreOptions with
		// a tenant-specific DEK — see account_secret_store.go) for the
		// credential envelope itself, so it is encrypted at rest via the
		// same barrier path as every other secret.
		//
		// The registry is built over the cost ledger above, so both the
		// "static" (use account X) and "least_usage" (use the account with the
		// least usage) strategies the owner asked for are registered and
		// selectable by name from a spawn request.
		if s.secretsManager != nil {
			// Provider registry first: it decides which providers may run the
			// refresh grant and supplies the token endpoint / client id /
			// access-token env key an account did not set for itself. Built-ins
			// (Anthropic, OpenAI, OpenCode) are merged with whatever
			// orchestration.account_providers configures.
			accountProviders, providerErr := orchestration.NewAccountProviderRegistry(
				accountProviderDescriptors(s.config.Orchestration.AccountProviders)...)
			if providerErr != nil {
				return providerErr
			}

			accountStore := orchestration.NewAccountStore(orchKV, orchStoreOpts...)
			// Shared with lifecycle_sdlc.go's attention inbox wiring — see
			// Server.orchAccountStore.
			s.orchAccountStore = accountStore
			accountSecrets := orchestration.NewAccountSecretStore(s.secretsManager)
			accountTokenOpts := []orchestration.AccountTokenManagerOption{
				orchestration.WithAccountProviders(accountProviders),
				orchestration.WithAccountTokenLogger(logAdapter),
			}
			// An account that names a credential_secret_ref is read from that
			// tenant secret, in the ACCOUNT's tenant, on every resolution — both
			// for an in-process spawn and for the materialize verb that hands a
			// worker host its RESOLVED envelope. Workers never see the reference.
			if s.quicBaaSService != nil && s.quicBaaSService.Secrets() != nil {
				accountTokenOpts = append(accountTokenOpts, orchestration.WithAccountSecretResolver(
					service.NewAccountSecretResolver(s.quicBaaSService.Secrets())))
			} else {
				s.logger.Warn("No secrets service available; pooled accounts that name a credential_secret_ref cannot be resolved")
			}
			accountTokens := orchestration.NewAccountTokenManager(accountSecrets, accountStore, 0, accountTokenOpts...)

			// Install the RFC 6749 refresh_token-grant client on every
			// oauth-capable provider. Without this every oauth2 account fails
			// its first renewal with "refresher not configured" at spawn time,
			// which is precisely when a live workload notices.
			refreshedProviders := accountTokens.RegisterOAuthRefreshers(
				orchestration.NewOAuth2TokenRefresher(orchestration.OAuth2RefresherConfig{}))
			s.logger.Infof("OAuth2 token refresher registered for pooled account providers: %v", refreshedProviders)

			// EVERY HAND-OVER OF A POOLED CREDENTIAL IS LEASED, and both
			// hand-over paths lease through the SAME authority: the in-process
			// resolver below, and the remote materialize verb wired a few lines
			// on. That is what makes one revocation cover both.
			//
			// It composes the lease manager startDynamicSecrets built, rather
			// than making one of its own, so an account delivery grant is the
			// same kind of object as every other lease in the platform —
			// durable in the embedded qrdb node, visible to
			// Security().DynamicSecrets().ListLeases, revocable with
			// RevokeLease.
			//
			// A node with no lease manager (no qrdb client, i.e. the sealed
			// window) wires none, and releases are unleased exactly as they
			// were before grants existed. The alternative — refusing every
			// pooled spawn — would take a capability away from a deployment
			// that has no way to get it back.
			var accountLeases orchestration.AccountCredentialLeaser
			if leaseManager := s.LeaseManager(); leaseManager != nil {
				accountLeases = orchestration.NewAccountCredentialLeaser(
					leaseManager,
					s.config.Orchestration.AccountDeliveryTTL,
					storeAuditor,
				)
				s.logger.Info("Pooled provider credential deliveries are leased (revoked on session conclusion)")
			} else {
				s.logger.Warn("No lease manager available; pooled provider credential deliveries are NOT leased")
			}

			// The account write path validates against the SAME registry, so an
			// oauth2 account with no resolvable token endpoint is rejected by
			// CreateAccount/RotateAccountCredential instead of at spawn.
			if s.quicBaaSService != nil {
				if providerWirer, providerOK := s.quicBaaSService.Account().(interface {
					SetProviderRegistry(*orchestration.AccountProviderRegistry)
				}); providerOK {
					providerWirer.SetProviderRegistry(accountProviders)
				}
				// The credential-release verb resolves through the SAME token
				// manager an in-process spawn does, so an oauth_refresh account
				// is renewed on the central that owns its refresh token before
				// the envelope is handed to a worker host. Without this wiring
				// the verb fails closed and a remote pooled spawn is refused —
				// which is the whole of go-quicbaas#734's server half.
				if tokenWirer, tokenOK := s.quicBaaSService.Account().(interface {
					SetTokenManager(*orchestration.AccountTokenManager)
				}); tokenOK {
					tokenWirer.SetTokenManager(accountTokens)
				}
				// The REMOTE half of the delivery grant. This is the
				// enforcement point: a daemon refreshing a copy under a grant
				// the session's conclusion has revoked is refused here, from
				// the lease store, whatever it believes about its own state.
				if leaseWirer, leaseOK := s.quicBaaSService.Account().(interface {
					SetAccountCredentialLeaser(orchestration.AccountCredentialLeaser)
				}); leaseOK {
					leaseWirer.SetAccountCredentialLeaser(accountLeases)
				}
			}

			// BALANCED BY DEFAULT. An unpinned spawn takes the least busy,
			// then least used, account; `default: true` only breaks a tie.
			// Static selection stays registered for a caller that names it.
			accountStrategies, strategyErr := orchestration.NewBalancedAccountStrategyRegistry(costTracker, sessionCounter)
			if strategyErr != nil {
				return strategyErr
			}
			accountResolver := orchestration.NewAccountResolver(
				accountStore, accountTokens, accountStrategies,
				append(orchStoreOpts,
					orchestration.WithStoreLogger(logAdapter),
					// The LOCAL half of the cap: an in-process spawn asks
					// the same authority the remote release does.
					orchestration.WithSpendAuthority(spendAuthority),
					// The LOCAL half of the delivery grant, for the same
					// reason: one authority, both paths.
					orchestration.WithAccountDeliveryGrantor(accountLeases),
					// The session cap, enforced under the reservation table's
					// lock so concurrent spawns cannot all take the last slot.
					orchestration.WithAccountSessionReserver(sessionCounter),
				)...)

			// Give the orchestrator the SAME metadata store the resolver reads
			// from, so a turn that fails on a provider rate/usage limit can put
			// a durable hold on the offending account (AccountStatusLimited +
			// AvailableAt) before selecting a replacement. Without it the
			// rotation still finds a different account for THIS session, but
			// the exhausted one stays nominally active and every other session
			// rediscovers the same limit for itself.
			wirer.SetAccountStore(accountStore)

			// And the authority a live session's credential is checked against
			// before every job-runner turn (go-quicbaas#997): the same token
			// manager that materializes the credential, so a rotation — an
			// envelope replaced here, or a referenced account's secret
			// rewritten by its owner — is seen by the next prompt, not by the
			// provider refusing it.
			wirer.SetAccountCredentialGenerations(accountTokens)

			// ADMISSION'S THIRD INPUT, from the same two authorities the spawn
			// path uses: the account store for lifecycle and cooldown state,
			// and the ledger-backed quota authority for the subscription's
			// sliding usage windows. Asking here means a project whose whole
			// pool is spent DEFERS instead of spawning an agent whose first
			// turn the provider will refuse.
			//
			// Built from the same handles rather than from a copy of the rules:
			// see pkg/orchestration/admission_headroom.go.
			poolHeadroom, headroomErr := orchestration.NewAccountPoolHeadroom(
				accountStore, orchestration.NewQuotaAuthority(consumption),
				orchestration.WithHeadroomSessions(sessionCounter),
				// A pool that goes to zero at admission is reported on the
				// same trail a spawn-time exhaustion is, or the common case,
				// the deferred one, would never be seen.
				orchestration.WithHeadroomReporting(storeAuditor, logAdapter))
			if headroomErr != nil {
				return headroomErr
			}
			s.orchPoolHeadroom = poolHeadroom
			// The same pool answers whether an Agent can draw a usable account
			// at all, which the review dispatcher asks before it starts a job
			// whose only outcome would be a failed spawn.
			if reader, isReader := poolHeadroom.(orchestration.AccountAvailabilityReader); isReader {
				if availability, accepts := agentSvc.(orchestration.AgentAccountAvailabilityReader); accepts {
					availability.SetAccountAvailability(reader)
				}
			}
			s.logger.Info("Admission bound to pooled provider usage headroom (each account's spend windows, cooldowns and session caps)")

			// THE REAPER BECOMES A SECURITY CONTROL. Session conclusion — an
			// operator's close, a job's ending, a node restart's sweep, an
			// agent leaving the fleet — now also withdraws the grants the
			// session's credential was delivered under.
			wirer.SetAccountCredentialLeaser(accountLeases)

			// "claude-code" is an alias for the same program and is absent from
			// the registrations above, so naming it here is a no-op today — the
			// lookup simply misses. It is named anyway because the SDLC module
			// demands a pooled account for it (orchestration.WorkerSpawnAccountAuto,
			// go-quicbaas#235), and the two lists disagreeing in THIS direction
			// is the expensive one: a backend the module pools and the server
			// does not fails every worker spawn closed, with no configuration
			// an operator could change to fix it.
			for _, name := range []string{"claude", "claude-code", "opencode"} {
				if b, ok := backends[name]; ok {
					if acpBackend, ok := b.(*orchestration.ACPBackend); ok {
						acpBackend.SetAccountResolver(accountResolver)
						// Only for the spawn that resolved an account and then
						// failed to dispatch: no Session exists to carry the
						// grant, so the backend withdraws it itself.
						acpBackend.SetAccountCredentialLeaser(accountLeases)
					}
				}
			}
			s.logger.Info("Pooled provider account resolver attached to claude/opencode orchestration backends (default strategy: least_usage, balanced on live sessions then recorded usage; static available)")
		} else {
			s.logger.Warn("No secrets manager attached; pooled provider accounts are disabled (spawns with account_id/account_auto will fail closed)")
		}

		// The tenant AI agent catalog, and the one-time rewrite of any project
		// record still carrying a free-string default lead. Before the session
		// restoration and every sweep below, because a legacy project record
		// no longer decodes and a spawn naming an Agent resolves here.
		agentCtx, agentCancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
		agentErr := s.startAgentCatalog(agentCtx, agentSvc, orchKV, storeAuditor, projectStore, orchStoreOpts)
		agentCancel()
		if agentErr != nil {
			return agentErr
		}

		// Wire the session event log so attachers can replay + tail session
		// events. Backed by the SAME tenant-routed, durable KV store as job
		// persistence (jobKVStore, above) — conversation transcripts
		// (prompts, assistant chunks, tool calls, presence) therefore survive
		// a daemon restart exactly like tasks/projects/agents do, and a
		// barrier-isolated tenant's transcripts land in that tenant's own
		// barrier (own DEK + namespace) rather than a shared table. At-rest
		// confidentiality beyond the tenant boundary is provided by qrdb's
		// StorageBarrier.
		sessionLog, logErr := orchestration.NewKVSessionEventLog(orchestration.KVSessionEventLogConfig{
			KVStore: jobKVStore,
			// A transcript is append-only, so the bound is what stops one
			// long-running session growing a keyspace nobody reads and nothing
			// reclaims. The zero config keeps DefaultSessionRetentionTurns.
			Retention: orchestration.SessionRetention{
				MaxTurns:  s.config.Orchestration.SessionHistory.MaxTurnsPerSession,
				PerTenant: s.config.Orchestration.SessionHistory.PerTenantMaxTurns,
			},
			OnRetentionError: func(err error) {
				s.logger.Warnf("Session transcript prune failed: %v", err)
			},
		})
		if logErr != nil {
			s.logger.Warnf("Session event log disabled: %v", logErr)
		} else {
			wirer.SetSessionEventLog(sessionLog)
			s.logger.Info("Session event log attached (durable KV-backed, tenant-routed, qrdb barrier-encrypted at rest)")
		}

		wirer.SetTranscriptReplayBudget(s.config.Orchestration.TranscriptReplayBudget)

		// Restore the session registry from the durable store. Without this a
		// session spawned before the restart is unaddressable and a prompt
		// against it fails with "either backend or session_id must be
		// provided". Persistent local sessions come back in the resumable
		// state: their agent process is recreated by the next prompt, not at
		// boot, so a restart does not fork a subprocess for every session
		// anyone ever spawned. Must run after the session event log is wired —
		// the replayed transcript is read from it.
		// THE TENANTS A RECOVERY OR MAINTENANCE PASS MUST NAME. orchKV routes
		// every read to the tenant on the context, and both the boot-time
		// restoration below and the periodic session sweep run on contexts
		// that carry none — so without this they see the system tenant's
		// session records and nothing else, and every tenant's session is
		// left neither restored nor reconciled (#792). Wired BEFORE the
		// restoration, which is the first pass that needs it.
		if s.iamComponents != nil && s.iamComponents.GetTenantManager() != nil {
			tenants := &iamTenantLister{tenants: s.iamComponents.GetTenantManager()}
			wirer.SetTenantLister(tenants)
			// A fleet registration's tenant_id is verified against the same
			// tenant directory.
			wirer.SetTenantResolver(tenants)
		} else {
			s.logger.Info("Session recovery limited to the system tenant: no tenant manager to enumerate tenants")
		}

		type sessionLoader interface {
			LoadPersistedSessions(ctx context.Context) error
		}
		if loader, loaderOK := agentSvc.(sessionLoader); loaderOK {
			loadCtx, loadCancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
			if loadErr := loader.LoadPersistedSessions(loadCtx); loadErr != nil {
				s.logger.Warnf("Session restoration from durable store failed: %v", loadErr)
			}
			loadCancel()
		}

		// Wire the remote-backend resolver so daemon-kind registrations are
		// dispatched over gRPC to their reachable host. Non-fatal if prereqs
		// (shared registry + orchestrator OAuth2 client) are missing.
		s.installRemoteBackendResolver(agentSvc)

		// Wire the lead agent if configured: the system tenant's Agent or
		// pool, spawned by reference so it fails over like any other slot.
		s.startLeadAgent(agentSvc, wirer)
	}

	// Attach to the QuicBaaS facade.
	if s.quicBaaSService != nil {
		s.quicBaaSService.SetAgentService(agentSvc)
		s.logger.Info("Agent orchestration service attached to QuicBaaS facade")
	}

	// Sweep every registered task source against every autonomous project,
	// creating auto-spawning tasks for new work, and dispatch the tasks
	// that are waiting. Runs only when there is a project store to read and
	// a task store to write.
	if ok {
		s.startTaskSourcePoller(agentSvc)
		// The dispatcher itself starts in the startupTaskDispatcher step,
		// after job recovery (see orchestrationStartup).
		if spawner, spawnerOK := agentSvc.(orchestration.TaskSpawner); spawnerOK {
			s.taskSpawner = spawner
		}
	}

	s.logger.Info("Agent orchestration service started")
	return nil
}

// nodeName names this daemon on the leases it takes and the jobs it runs.
//
// Diagnostic AND decisive: recoverJobs resumes only the jobs this name owns,
// and the reaper leaves alone every lease this name's loops still hold. Stable
// across restarts by construction — the Raft node ID first, the hostname
// otherwise — because a name that changed on restart would orphan every job
// the previous process was running.
func (s *Server) nodeName() string {
	if s.config != nil && s.config.Raft.NodeID != 0 {
		return "quicbaasd-node-" + strconv.FormatUint(s.config.Raft.NodeID, 10)
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return "quicbaasd-" + host
	}
	return "quicbaasd"
}

// socketsRoot is the one place the daemon resolves its per-session agent
// socket root, for the three things that need the same answer: the sdlc
// runner that binds under it, and the two mount policies that admit exactly
// that root.
//
// It is a method rather than three copies of the derivation because the root
// is PER-DAEMON (go-quicbaas#687) — derived from this daemon's data directory
// so two instances of one user do not share one — and three call sites that
// each spelled the derivation could each spell a different one.
//
// Note this is NOT nodeName's answer: that name is deliberately stable across
// restarts and is shared by every standalone daemon on a host (raft.node_id
// defaults to 1), which is the opposite of what distinguishes two instances.
func (s *Server) socketsRoot() (string, error) {
	if s.config == nil {
		return "", nil
	}
	return s.config.Orchestration.EffectiveSocketsRoot(s.config.Storage.DataDir)
}

// jobRecoverer is the engine's restart path for interrupted jobs.
type jobRecoverer interface {
	RecoverJobs(ctx context.Context) error

	// ResumedJobsSettled closes once every resumed job has settled its
	// lease, with no deadline; the task dispatcher waits on it.
	ResumedJobsSettled() <-chan struct{}
}

// recoverOrchestrationJobs resumes the jobs this node owned when it last
// stopped. It runs after every module has registered into the engine's
// seams, NOT inside startOrchestration: a resumed job runs its workflow's
// lifecycle hooks at once, and a hook whose module has not started yet
// answers "not running on this node" — which failed every job a restarted
// standalone node tried to resume (measured by the daemon e2e
// TestSoftwareFactory_KilledWorkerIsResumedOnRestart).
func (s *Server) recoverOrchestrationJobs() {
	if s.quicBaaSService == nil {
		return
	}
	recoverer, ok := s.quicBaaSService.Agent().(jobRecoverer)
	if !ok {
		return
	}
	recoverCtx, recoverCancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
	defer recoverCancel()
	if err := recoverer.RecoverJobs(recoverCtx); err != nil {
		s.logger.Warnf("Job recovery from KV store failed: %v", err)
	}
	// THE GATE OUTLIVES THE BOOT BUDGET. A resumed job still waiting for its
	// lease when recovery gave up keeps running, so the dispatcher must keep
	// waiting for it; starting on the recovery's deadline instead would
	// release the job's task and dispatch a duplicate (Codex, #1075 round 3).
	s.dispatchGate = recoverer.ResumedJobsSettled()
}

// jobLeaseStore selects the lease store for the deployment's consistency
// domain, by the same predicate taskClaimArbiter uses and for the same reason:
// a mode added later cannot be clustered for claims and standalone for leases.
func (s *Server) jobLeaseStore(ttl time.Duration) (orchestration.JobLeaseStore, error) {
	if !config.IsClusterLikeMode(s.getEffectiveMode()) {
		return orchestration.NewProcessJobLeaseStore(ttl)
	}
	if s.qrdbClient == nil {
		return nil, ErrNoJobLeaseStore
	}
	client, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrNoJobLeaseStore
	}
	return orchestration.NewTransactionJobLeaseStore(client.Transaction(), client.KV(), ttl)
}

// jobSweeper runs one maintenance pass: the reaper over the fleet's leases
// and the stall detector over this node's loops.
type jobSweeper interface {
	Sweep(ctx context.Context) (orchestration.JobSweepReport, error)
}

// orchestrationSweeps is the running sweep goroutine and the means to stop
// it.
type orchestrationSweeps struct {
	stop context.CancelFunc
	done chan struct{}
}

// startOrchestrationSweeps runs the sweeper on a ticker until shutdown.
func (s *Server) startOrchestrationSweeps(sweeper jobSweeper, interval time.Duration) {
	if interval <= 0 {
		interval = orchestration.DefaultJobLeaseTTL / 3
	}
	ctx, stop := context.WithCancel(context.Background())
	sweeps := &orchestrationSweeps{stop: stop, done: make(chan struct{})}
	s.orchSweeps = sweeps
	go func() {
		defer close(sweeps.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report, err := sweeper.Sweep(ctx)
				if err != nil {
					if ctx.Err() == nil {
						s.logger.WithError(err).Warn("orchestration: job sweep failed")
					}
					continue
				}
				s.logJobSweep(report)
			}
		}
	}()
	s.logger.Infof("Job reaper and stall detector started (sweep=%s)", interval)
}

// orchestratorContainerOwner is the owner label the orchestrator's own
// in-process backends stamp on their sandbox containers. It is distinct from
// the embedded agent's (its data directory, unprefixed): the two hold
// different backend sets, and a reaper asking one set about the other's
// sessions would reap live workers.
func orchestratorContainerOwner(dataDir string) (string, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", &ServerCauseError{Message: "orchestration: container owner from data dir", Cause: err}
	}
	return "orchestrator:" + abs, nil
}

// agentContainerRuntime returns the runtime agent sessions run in: the
// WithAgentContainerRuntime override when one is supplied, otherwise the
// host's docker/podman runtime from discover, which is not called when an
// override is supplied. host is the discovered runtime, nil under an
// override: only the host's own runtime is swept and reaped. A nil runtime
// means none was supplied and none is available.
func (s *Server) agentContainerRuntime(discover func() (*orchestration.ExecContainerRuntime, error)) (runtime orchestration.ContainerRuntime, host *orchestration.ExecContainerRuntime) {
	if s.opts.agentContainerRuntime != nil {
		return s.opts.agentContainerRuntime, nil
	}
	host, err := discover()
	if err != nil {
		s.logger.Infof("No host container runtime available: %v", err)
		return nil, nil
	}
	return host, host
}

// sandboxedBackend is the part of an ACP backend that runs its agent in a
// container.
type sandboxedBackend interface {
	SetContainerRuntime(orchestration.ContainerRuntime)
	SetSandboxOwner(string)
	SetGoBuildCache(orchestration.GoBuildCache)
}

// wireSandboxedBackends attaches runtime, owner and goCache to each built-in
// ACP backend present in backends.
func wireSandboxedBackends[B any](backends map[string]B, runtime orchestration.ContainerRuntime, owner string, goCache orchestration.GoBuildCache) {
	for _, name := range []string{"claude", "codex", "opencode"} {
		backend, ok := any(backends[name]).(sandboxedBackend)
		if !ok {
			continue
		}
		backend.SetContainerRuntime(runtime)
		backend.SetSandboxOwner(owner)
		backend.SetGoBuildCache(goCache)
	}
}

// startContainerReaper runs the container reaper over the orchestrator's own
// sandbox containers until shutdown. A container is reaped once none of
// backends holds its session and it is past the grace period.
func (s *Server) startContainerReaper(
	rt *orchestration.ExecContainerRuntime,
	owner string,
	backends map[string]orchestration.AgentBackend,
) error {
	reaper, err := orchestration.NewContainerReaper(orchestration.ContainerReaperConfig{
		Runtime: rt,
		Owner:   owner,
		Live: func(sessionID string) bool {
			for _, b := range backends {
				if holder, ok := b.(interface{ HoldsSession(string) bool }); ok && holder.HoldsSession(sessionID) {
					return true
				}
			}
			return false
		},
		Logger: &logger.LoggerAdapter{Logger: s.logger},
	})
	if err != nil {
		return err
	}
	ctx, stop := context.WithCancel(context.Background())
	loop := &orchestrationSweeps{stop: stop, done: make(chan struct{})}
	s.orchContainerReaper = loop
	go func() {
		defer close(loop.done)
		ticker := time.NewTicker(orchestration.DefaultContainerReapInterval)
		defer ticker.Stop()
		reaper.Run(ctx, ticker.C, func(sweep orchestration.ContainerSweep, err error) {
			if err != nil && ctx.Err() == nil {
				s.logger.WithError(err).Warn("orchestration: container reap failed")
			}
			if len(sweep.Removed) > 0 {
				s.logger.Infof("Reaped %d worker container(s) no live session held", len(sweep.Removed))
			}
		})
	}()
	s.logger.Infof("Container reaper started (owner=%s interval=%s grace=%s)",
		owner, orchestration.DefaultContainerReapInterval, orchestration.DefaultContainerReapGrace)
	return nil
}

// stopContainerReaper stops the container reaper and waits for it.
func (s *Server) stopContainerReaper() {
	if s.orchContainerReaper == nil {
		return
	}
	s.orchContainerReaper.stop()
	<-s.orchContainerReaper.done
	s.orchContainerReaper = nil
}

// stopOrchestrationSweeps stops the sweep and waits for it.
func (s *Server) stopOrchestrationSweeps() {
	if s.orchSweeps == nil {
		return
	}
	s.orchSweeps.stop()
	<-s.orchSweeps.done
	s.orchSweeps = nil
}

// jobDrainer is the orchestration service's shutdown drain: the seam the
// daemon stops running control loops through. The service satisfies it; the
// interface is here so shutdown does not depend on the whole facade.
type jobDrainer interface {
	Shutdown(ctx context.Context) error
}

// drainOrchestrationJobs gives the orchestration service's running control
// loops a bounded window to conclude BEFORE the stores they write to are
// closed.
//
// Without it, shutdown closed storage under running job goroutines, and a
// loop still writing met a closed store — which the engine reports by
// panicking, taking the daemon with it. The window is bounded because an
// agent turn runs for minutes: a loop that does not fit keeps its
// non-terminal record and is resumed by RecoverJobs on the next start,
// exactly as it is when the daemon is killed today.
func (s *Server) drainOrchestrationJobs() {
	if s.quicBaaSService == nil {
		return
	}
	agentSvc := s.quicBaaSService.Agent()
	if agentSvc == nil {
		return
	}
	drainer, ok := agentSvc.(jobDrainer)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), orchestration.DefaultShutdownDrainTimeout)
	defer cancel()

	s.logger.Info("Draining running orchestration jobs...")
	if err := drainer.Shutdown(ctx); err != nil {
		s.logger.WithError(err).Warn("orchestration: running work did not drain before shutdown; it resumes on the next start")
		return
	}
	s.logger.Info("Orchestration jobs drained")
}

// logJobSweep records what a pass concluded. Healthy jobs are silent; a
// reaped or orphaned lease and a stalled job rise to WARN because each is a
// job that ended without its loop saying so.
func (s *Server) logJobSweep(report orchestration.JobSweepReport) {
	for _, f := range report.Reap.Findings {
		switch f.Verdict {
		case orchestration.JobReapReaped, orchestration.JobReapOrphaned:
			s.logger.Warnf("orchestration: reaper %s job=%s tenant=%s task=%s node=%s: %s",
				f.Verdict, f.JobID, f.TenantID, f.TaskID, f.Node, f.Evidence)
		case orchestration.JobReapRunLive:
			s.logger.Infof("orchestration: reaper %s job=%s: %s", f.Verdict, f.JobID, f.Evidence)
		}
	}
	for _, f := range report.Stall.Findings {
		switch f.Verdict {
		case orchestration.JobStallStalled:
			s.logger.Warnf("orchestration: stall detector ended job=%s tenant=%s task=%s state=%s: %s",
				f.JobID, f.TenantID, f.TaskID, f.State, f.Evidence)
		default:
			s.logger.Infof("orchestration: stall detector %s job=%s state=%s: %s",
				f.Verdict, f.JobID, f.State, f.Evidence)
		}
	}
}

// agentExitRecordTimeout bounds the session-record write an observed process
// exit triggers. Short: it is one key write against the local store, and the
// only thing waiting on it is a goroutine whose process is already gone.
const agentExitRecordTimeout = 15 * time.Second

// recordAgentProcessExit builds the observer the ACP backend calls when a
// supervised agent subprocess terminates.
//
// TWO RECORDS, AND BOTH ARE NEEDED. The log line is what an operator watching
// a run sees; the session-record update is what every later reader sees — the
// stall detector asking whether a session is live, the session manager deciding
// whether a parked item can be resumed into its own transcript. Before the
// supervisor existed there was neither: a process that died while its work item
// was parked left a record still claiming to be addressable, and the first
// evidence was a prompt to a dead pipe.
//
// The status write runs on a context of its own, detached from any request,
// because the exit arrives on the supervisor's goroutine long after whichever
// call spawned the session returned — AND BOUNDED, because that goroutine has
// no deadline of its own: an unbounded write against a wedged store would leak
// one supervisor goroutine per dead session, permanently. Its failure is logged
// and not retried: the process is gone either way, and the truth an operator
// needs is already in the line above it.
func (s *Server) recordAgentProcessExit(recorder sessionExitRecorder) orchestration.ProcessExitObserver {
	if recorder == nil {
		return nil
	}
	return func(exit orchestration.ProcessExit) {
		if s.logger != nil {
			s.logger.Warnf("orchestration: agent process exited: %s", exit.String())
		}
		if exit.TenantID == "" || exit.SessionID == "" {
			return
		}
		ctx, cancel := context.WithTimeout(
			orchestration.ContextWithTenant(context.Background(), exit.TenantID),
			agentExitRecordTimeout)
		defer cancel()
		recorder.RecordSessionExit(ctx, exit.TenantID, exit.SessionID, "agent process exited")
	}
}

// sessionExitRecorder is the service-level lifecycle operation an observed
// process exit is routed through.
//
// A narrow interface rather than the concrete service, for the reason every
// other port here is one: this file needs exactly one method, and naming it
// keeps the wiring honest about how much of the service it reaches into.
type sessionExitRecorder interface {
	// RecordSessionExit closes every surface a session's state lives on —
	// the store record, the session-log metadata, and the terminal event.
	RecordSessionExit(ctx context.Context, tenantID, sessionID, reason string)
}

// deferredSessionExitRecorder bridges a construction order that cannot be
// reversed: the backends are built BEFORE the agent service, and the observer
// they carry has to reach the service that does not exist yet.
//
// LATE-BOUND RATHER THAN RE-ORDERED, because the service takes the backends as
// a constructor argument — the cycle is real, not an accident of layout. The
// window in which the target is unset closes microseconds after boot, long
// before any session can be spawned, and an exit arriving inside it is dropped
// with a line saying so rather than panicking on a nil.
type deferredSessionExitRecorder struct {
	mu     sync.Mutex
	target sessionExitRecorder
	logger interface{ Warnf(string, ...any) }
}

// set binds the recorder once the service exists.
func (d *deferredSessionExitRecorder) set(target sessionExitRecorder) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.target = target
}

// RecordSessionExit implements sessionExitRecorder.
func (d *deferredSessionExitRecorder) RecordSessionExit(ctx context.Context, tenantID, sessionID, reason string) {
	d.mu.Lock()
	target := d.target
	d.mu.Unlock()
	if target == nil {
		if d.logger != nil {
			d.logger.Warnf(
				"orchestration: session %s exited before the agent service was wired; its state was not closed",
				sessionID)
		}
		return
	}
	target.RecordSessionExit(ctx, tenantID, sessionID, reason)
}
