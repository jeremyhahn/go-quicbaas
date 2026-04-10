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

	// Register Codex backend (ACP over stdio).
	backends["codex"] = orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:      "codex",
		Args:         []string{},
		DefaultModel: "o4-mini",
	})
	s.logger.Info("Codex backend registered for orchestration")

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
	})
	if ok {
		wirer.SetTaskStore(orchestration.NewInMemoryTaskStore())
		wirer.SetTenantGuard(orchestration.NewTenantGuard(logAdapter))
		wirer.SetAgentRegistry(orchestration.NewInMemoryAgentRegistry())
		s.logger.Info("Agent fleet registry attached to orchestration service")

		// Wire workflow loader. Workflows are registered per-job via
		// --workflow-file, not auto-seeded from a directory.
		wl := orchestration.NewWorkflowLoader()
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
