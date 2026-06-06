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
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// startOrchestration initializes the agent orchestration service and attaches
// it to the QuicBaaS facade. The orchestration service provides multi-agent
// coordination, task management, and session lifecycle management.
func (s *Server) startOrchestration() error {
	s.logger.Info("Initializing agent orchestration service...")

	store := orchestration.NewInMemoryAgentStore()
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
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--disable-slash-commands"},
		DefaultModel: "", // Let Claude Code use its configured default model.
		TeamMode:     s.config.Orchestration.ClaudeTeamMode,
		CLIMode:      true,
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
	})
	if ok {
		wirer.SetTaskStore(orchestration.NewInMemoryTaskStore())
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
			projectStore = orchestration.NewInMemoryProjectStore()
		}
		wirer.SetProjectAuthz(orchestration.NewProjectAuthz(projectStore))
		s.logger.Info("Project authorization attached to orchestration service")
		// Reuse the registry created during IAM init so enrollment-time
		// registrations are immediately visible to dispatch. Falls back to
		// a fresh in-memory registry only if IAM init never ran.
		if s.agentRegistry == nil {
			s.agentRegistry = orchestration.NewInMemoryAgentRegistry()
		}
		wirer.SetAgentRegistry(s.agentRegistry)
		s.logger.Info("Agent fleet registry attached to orchestration service")

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

		// Wire KV store for durable job persistence.
		// Uses an in-memory KV store by default; will be replaced with
		// Raft-backed KV store when cluster infrastructure is wired.
		jobKVStore := NewMemoryKVStore()
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

		// Wire the session event log so attachers can replay + tail
		// session events. Backed by the same KV store used for job
		// persistence; at-rest confidentiality is provided by qrdb's
		// StorageBarrier.
		sessionLog, logErr := orchestration.NewKVSessionEventLog(orchestration.KVSessionEventLogConfig{
			KVStore: jobKVStore,
		})
		if logErr != nil {
			s.logger.Warnf("Session event log disabled: %v", logErr)
		} else {
			wirer.SetSessionEventLog(sessionLog)
			s.logger.Info("Session event log attached (KV-backed, qrdb barrier-encrypted at rest)")
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
