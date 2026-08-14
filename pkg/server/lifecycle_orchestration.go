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
	"os"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

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
	orchStoreOpts := []orchestration.StoreOption{
		orchestration.WithStoreAuditor(newOrchestrationStoreAuditor(s.auditLoggerForStores)),
	}

	store := orchestration.NewAgentStore(orchKV, orchStoreOpts...)
	backends := make(map[string]orchestration.AgentBackend)

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
		DefaultModel:        "", // Let Claude Code use its configured default model.
		TeamMode:            s.config.Orchestration.ClaudeTeamMode,
		CLIMode:             true,
		NativeSessionResume: s.config.Orchestration.ClaudeNativeSessionResume,
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
	})
	s.logger.Infof("OpenCode backend registered for orchestration (command: %s)", opencodePath)

	logAdapter := &logger.LoggerAdapter{Logger: s.logger}

	agentSvc, err := orchestration.NewAgentService(store, backends, logAdapter)
	if err != nil {
		s.logger.Warnf("Agent orchestration service initialization failed: %v", err)
		return nil // Non-fatal: orchestration is optional
	}

	// Wire task store, tenant guard, agent registry, container runtime,
	// KV store for job persistence, and lead agent session.
	wirer, ok := agentSvc.(interface {
		SetTaskStore(store orchestration.TaskStore)
		SetTenantGuard(guard *orchestration.TenantGuard)
		SetContainerRuntime(runtime orchestration.ContainerRuntime)
		SetAgentRegistry(registry orchestration.AgentRegistry)
		SetLeadAgent(config *orchestration.LeadAgentConfig)
		SetLeadSessionID(id string)
		SetKVStore(kv interfaces.KVStore)
		SetWorkflowLoader(wl *orchestration.WorkflowLoader)
		SetSessionEventLog(log orchestration.SessionEventLog)
		SetProjectAuthz(authz *orchestration.ProjectAuthz)
		SetProjectStore(store orchestration.ProjectStore)
		SetMountPolicy(policy *orchestration.MountPolicy)
		SetTrustPolicy(policy orchestration.TrustPolicy)
		SetTranscriptReplayBudget(budget int)
		SetCostTracker(tracker orchestration.CostTracker)
		SetAccountStore(store orchestration.AccountStore)
	})
	if ok {
		wirer.SetTaskStore(orchestration.NewTaskStore(orchKV, orchStoreOpts...))
		wirer.SetTenantGuard(orchestration.NewTenantGuard(logAdapter))

		// Wire project-level authorization so ClaimTask enforces the
		// per-project AgentRoles matrix when a task is scoped to a project.
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
		wirer.SetProjectStore(projectStore)
		wirer.SetMountPolicy(&orchestration.MountPolicy{
			ProjectsRoot: projectsRoot,
			AllowedRoots: s.config.Orchestration.Sandbox.AllowedMountRoots,
		})

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
		if wsErr := s.wireProjectWorkspaces(projectStore, projectsRoot); wsErr != nil {
			s.logger.Warnf("Managed project workspaces disabled: %v", wsErr)
		}
		// Reuse the registry created during IAM init so enrollment-time
		// registrations are immediately visible to dispatch. Builds one over
		// the same durable store only if IAM init never ran.
		if s.agentRegistry == nil {
			s.agentRegistry = orchestration.NewAgentRegistry(orchKV, orchStoreOpts...)
		}
		wirer.SetAgentRegistry(s.agentRegistry)
		s.logger.Info("Agent fleet registry attached to orchestration service")

		// Rebind the AgentAdminService onto the SAME shared fleet registry the
		// orchestrator just received. The facade builds AgentAdmin with a private
		// default registry at construction time; without this rebind, disable /
		// enable resolve agents in an empty store and every fleet-registered agent
		// surfaces as 404. AgentAdmin() returns the interface, so type-assert to
		// the concrete setter (no-op for alternate implementations).
		if s.quicBaaSService != nil {
			if rebinder, rebindOK := s.quicBaaSService.AgentAdmin().(interface {
				SetAgentRegistry(orchestration.AgentRegistry)
			}); rebindOK {
				rebinder.SetAgentRegistry(s.agentRegistry)
				s.logger.Info("Agent admin service bound to shared fleet registry")
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
				if n, rerr := wl.Rehydrate(context.Background()); rerr != nil {
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

		// Wire KV store for durable job persistence. Binds to the same
		// tenant-routed durable store as tasks/projects/agents (see orchKV
		// below) so job records and, in particular, the session event log
		// (conversation transcripts) survive a daemon restart instead of
		// evaporating with the process. Falls back to an in-memory store only
		// when no qrdb storage is attached (storage-less dev boot).
		jobKVStore := orchKV
		wirer.SetKVStore(jobKVStore)
		s.logger.Info("KV store attached to orchestration service for job persistence")

		// Recover interrupted jobs from previous server lifecycle.
		type jobRecoverer interface {
			RecoverJobs(ctx context.Context) error
		}
		if recoverer, recoverOK := agentSvc.(jobRecoverer); recoverOK {
			recoverCtx, recoverCancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
			defer recoverCancel()
			if recoverErr := recoverer.RecoverJobs(recoverCtx); recoverErr != nil {
				s.logger.Warnf("Job recovery from KV store failed: %v", recoverErr)
			}
		}

		// Wire container runtime if Docker/Podman is available.
		containerRuntime, rtErr := orchestration.NewExecContainerRuntime(logAdapter)
		if rtErr == nil {
			wirer.SetContainerRuntime(containerRuntime)
			s.logger.Info("Container runtime attached to orchestration service")

			// Give ACP backends access to the container runtime for sandboxed sessions.
			for _, name := range []string{"claude", "codex", "opencode"} {
				if b, ok := backends[name]; ok {
					if acpBackend, ok := b.(*orchestration.ACPBackend); ok {
						acpBackend.SetContainerRuntime(containerRuntime)
					}
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
		s.logger.Info("Token cost ledger attached to orchestration service (durable KV-backed, tenant-namespaced)")

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
			accountSecrets := orchestration.NewAccountSecretStore(s.secretsManager)
			accountTokens := orchestration.NewAccountTokenManager(accountSecrets, accountStore, 0,
				orchestration.WithAccountProviders(accountProviders))

			// Install the RFC 6749 refresh_token-grant client on every
			// oauth-capable provider. Without this every oauth2 account fails
			// its first renewal with "refresher not configured" at spawn time,
			// which is precisely when a live workload notices.
			refreshedProviders := accountTokens.RegisterOAuthRefreshers(
				orchestration.NewOAuth2TokenRefresher(orchestration.OAuth2RefresherConfig{}))
			s.logger.Infof("OAuth2 token refresher registered for pooled account providers: %v", refreshedProviders)

			// The account write path validates against the SAME registry, so an
			// oauth2 account with no resolvable token endpoint is rejected by
			// CreateAccount/RotateAccountCredential instead of at spawn.
			if s.quicBaaSService != nil {
				if providerWirer, providerOK := s.quicBaaSService.Account().(interface {
					SetProviderRegistry(*orchestration.AccountProviderRegistry)
				}); providerOK {
					providerWirer.SetProviderRegistry(accountProviders)
				}
			}

			accountStrategies := orchestration.NewAccountStrategyRegistry(costTracker)
			accountResolver := orchestration.NewAccountResolver(
				accountStore, accountTokens, accountStrategies,
				append(orchStoreOpts, orchestration.WithStoreLogger(logAdapter))...)

			// Give the orchestrator the SAME metadata store the resolver reads
			// from, so a turn that fails on a provider rate/usage limit can put
			// a durable hold on the offending account (AccountStatusLimited +
			// AvailableAt) before selecting a replacement. Without it the
			// rotation still finds a different account for THIS session, but
			// the exhausted one stays nominally active and every other session
			// rediscovers the same limit for itself.
			wirer.SetAccountStore(accountStore)

			for _, name := range []string{"claude", "opencode"} {
				if b, ok := backends[name]; ok {
					if acpBackend, ok := b.(*orchestration.ACPBackend); ok {
						acpBackend.SetAccountResolver(accountResolver)
					}
				}
			}
			s.logger.Info("Pooled provider account resolver attached to claude/opencode orchestration backends (strategies: static, least_usage)")
		} else {
			s.logger.Warn("No secrets manager attached; pooled provider accounts are disabled (spawns with account_id/account_auto will fail closed)")
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

		// Wire lead agent if configured.
		cfg := s.config.Orchestration.LeadAgent
		if cfg.Backend != "" {
			wirer.SetLeadAgent(&orchestration.LeadAgentConfig{
				Backend:      cfg.Backend,
				Model:        cfg.Model,
				SystemPrompt: cfg.SystemPrompt,
			})

			spawnCtx, spawnCancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
			defer spawnCancel()

			resp, spawnErr := agentSvc.SpawnSession(spawnCtx, &orchestration.SpawnSessionRequest{
				Backend:      cfg.Backend,
				Model:        cfg.Model,
				SystemPrompt: cfg.SystemPrompt,
			})
			if spawnErr != nil {
				s.logger.Warnf("Failed to spawn lead agent session: %v", spawnErr)
			} else {
				wirer.SetLeadSessionID(resp.SessionID)
				s.logger.Infof("Lead agent session spawned: session=%s backend=%s model=%s",
					resp.SessionID, cfg.Backend, cfg.Model)
			}
		}
	}

	// Attach to the QuicBaaS facade.
	if s.quicBaaSService != nil {
		s.quicBaaSService.SetAgentService(agentSvc)
		s.logger.Info("Agent orchestration service attached to QuicBaaS facade")
	}

	s.logger.Info("Agent orchestration service started")
	return nil
}
