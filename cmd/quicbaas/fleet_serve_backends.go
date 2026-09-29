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

package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// agentBackendDeps is what every backend constructor shares: the daemon's
// configuration, its sandbox policy, the MCP preset resolver, and the one
// ollama backend the daemon serves, if it serves one.
type agentBackendDeps struct {
	cfg             *fleet.Config
	sandboxNetwork  orchestration.SandboxNetwork
	sandboxSecurity orchestration.SandboxSecurity
	mcpResolver     orchestration.MCPPresetResolver

	// ollama is the "ollama" backend when this daemon serves it, and nil
	// otherwise. opencode lists its models as ollama/<name>.
	ollama *orchestration.OllamaBackend
}

// agentBackendBuilder constructs one backend.
type agentBackendBuilder func(d *agentBackendDeps) orchestration.AgentBackend

// agentBackendBuilders maps every individual backend kind to its constructor.
// fleet.Config.EffectiveBackends has already expanded the acp group and
// dropped the endpoint backends it had no server for.
var agentBackendBuilders = map[fleet.BackendKind]agentBackendBuilder{
	fleet.BackendClaude:   buildClaudeBackend,
	fleet.BackendCodex:    buildCodexBackend,
	fleet.BackendOpencode: buildOpencodeBackend,
	fleet.BackendOpenVINO: func(d *agentBackendDeps) orchestration.AgentBackend {
		// OVMS serves /v3, alone among these. It is also the one that cannot
		// be driven by a vendor CLI instead: OVMS answers "Only function tools
		// are supported" to codex, which advertises its shell and patch tools
		// as custom types with no switch to change them.
		return d.openAIAgent(openAIAgentEndpoint{
			base: d.cfg.OpenVINOBaseURL, chatPath: "",
			model: d.cfg.OpenVINOModel, context: d.cfg.OpenVINOContextTokens,
		})
	},
	fleet.BackendOllamaAgent: func(d *agentBackendDeps) orchestration.AgentBackend {
		return d.openAIAgent(openAIAgentEndpoint{
			base: d.cfg.EffectiveOllamaBaseURL(), chatPath: openAIChatPath,
			model: d.cfg.OllamaAgentModel, context: d.cfg.OllamaAgentContextTokens,
		})
	},
	fleet.BackendVLLM: func(d *agentBackendDeps) orchestration.AgentBackend {
		// vLLM: continuous batching and a cross-request prefix cache, which
		// ollama has neither of. Measured on one fleet at 268.7 triage
		// turns/min against ollama's 21.0 at the same concurrency.
		return d.openAIAgent(openAIAgentEndpoint{
			base: d.cfg.VLLMBaseURL, chatPath: openAIChatPath,
			model: d.cfg.VLLMModel, context: d.cfg.VLLMContextTokens,
		})
	},
	fleet.BackendOllama: func(d *agentBackendDeps) orchestration.AgentBackend { return d.ollama },
}

// buildFleetBackends constructs the backends this daemon serves: exactly
// cfg.EffectiveBackends(), each by its own constructor. The ACP backends run
// the claude / codex / opencode CLIs on PATH — operators override via
// --image-map when running sandboxed. Ollama points at the configured ollama
// API (default 127.0.0.1:11434).
//
// WHAT IS BUILT IS WHAT IS ADVERTISED. The runtime registers the names of
// these backends with the fleet, and placement routes by them, so a node that
// names opencode, openvino and ollama is never sent claude or codex work.
func buildFleetBackends(cfg *fleet.Config, logger interfaces.Logger) (map[string]orchestration.AgentBackend, error) {
	sandboxSecurity, securityErr := cfg.EffectiveSandboxSecurity()
	if securityErr != nil {
		return nil, securityErr
	}
	deps := &agentBackendDeps{
		cfg:             cfg,
		sandboxNetwork:  cfg.EffectiveSandboxNetwork(),
		sandboxSecurity: sandboxSecurity,
		// Resolve the QuicBaaS MCP preset for spawned ACP agents. The
		// resolver points them at the long-lived on-disk mTLS identity the
		// deploy flow uploaded (no per-spawn minting). It is wired only when
		// the control plane supplied an --mcp-endpoint; otherwise it stays
		// nil and the preset is forwarded unmodified.
		mcpResolver: buildFleetServeMCPResolver(cfg, logger),
	}
	if cfg.Serves(fleet.BackendOllama) {
		deps.ollama = newAgentOllamaBackend(cfg)
	}

	backends := map[string]orchestration.AgentBackend{}
	for _, kind := range cfg.EffectiveBackends() {
		build, ok := agentBackendBuilders[kind]
		if !ok {
			continue
		}
		backends[string(kind)] = build(deps)
	}
	// EVERY BACKEND THAT LOGS GETS THE DAEMON'S LOGGER. ACPBackend.SetLogger
	// is how the backend reports what a CLI did when a turn fails — most
	// importantly the evidence withheld from the error string, which is
	// otherwise a byte count an operator cannot act on. Nothing called it
	// here once, so those diagnostics were dead on every fleet node daemon: a
	// reviewer whose CLI produced no turn logged "(9882 bytes of stderr
	// withheld)" and nothing else, on every attempt, for as long as the fault
	// lasted.
	for _, backend := range backends {
		switch logged := backend.(type) {
		case *orchestration.ACPBackend:
			logged.SetLogger(logger)
		case *orchestration.OpenVINOBackend:
			logged.SetLogger(logger)
		}
	}
	if len(backends) == 0 {
		return nil, &MessageError{Message: fmt.Sprintf("no backends constructed from %v", cfg.EffectiveBackends())}
	}
	logger.Debugf("fleet node daemon backends constructed: %d", len(backends))
	return backends, nil
}

// newAgentOllamaBackend is the plain ollama model server backend.
func newAgentOllamaBackend(cfg *fleet.Config) *orchestration.OllamaBackend {
	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    cfg.EffectiveOllamaBaseURL(),
		MaxRetries: 2,
		Timeout:    10 * time.Minute,
	})
	return orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
		ChatService:  ollama.NewChatService(client, ""),
		ModelService: ollama.NewModelService(client),
		DefaultModel: "",
	})
}

// buildClaudeBackend is the Claude Code CLI over stream-json.
func buildClaudeBackend(d *agentBackendDeps) orchestration.AgentBackend {
	return orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:           "claude",
		Args:              []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--disable-slash-commands"},
		DefaultModel:      orchestration.DefaultClaudeModel,
		CLIMode:           true,
		SandboxNetwork:    d.sandboxNetwork,
		SandboxSecurity:   d.sandboxSecurity,
		MCPPresetResolver: d.mcpResolver,
	})
}

// buildCodexBackend is the codex CLI in exec mode.
//
// CODEX SPEAKS NO ACP. `codex --help` lists exec, review, login, mcp,
// app-server — nothing that speaks agent-client-protocol. This was once
// constructed with no subcommand and no transport, so the daemon launched the
// INTERACTIVE CLI and sent it JSON-RPC; codex read nothing it understood on a
// piped stdin and exited, which arrived as `acp: transport failed: initialize`
// and read as a broken protocol. Every independent review failed this way
// until 2026-09-13.
//
// `codex exec --json` is the non-interactive mode: one turn per invocation,
// prompt on stdin, JSONL events on stdout. --sandbox danger-full-access
// because the CONTAINER is the sandbox and codex's own would otherwise block
// the commands a review runs. --skip-git-repo-check because a workspace root
// is not always a repository root.
func buildCodexBackend(d *agentBackendDeps) orchestration.AgentBackend {
	return orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:   "codex",
		Transport: orchestration.ACPTransportExec,
		// SerenaCodexConfigArgs registers serena, the semantic code-retrieval
		// server the reviewer charter tells this agent to search with. codex
		// has no --mcp-config flag — only `-c <dotted.key>=<TOML>` — so its
		// registration is stated on argv. It is built in pkg/orchestration
		// beside Claude Code's so both CLIs get the same server argv and the
		// same checkout root.
		//
		// REASONING EFFORT IS STATED, NOT LEFT TO THE CLI'S DEFAULT. Code
		// review is the one role whose output is a gate: a shallower pass does
		// not fail visibly, it approves things it should have caught. Naming
		// the effort here means the depth a review runs at is a deployment
		// decision an operator can read and change, rather than whatever the
		// installed codex build happens to default to that week.
		ExecArgs: append([]string{
			"exec", "--json",
			"--sandbox", "danger-full-access",
			"--skip-git-repo-check",
			"-c", "model_reasoning_effort=" + strconv.Quote(d.cfg.EffectiveCodexReasoningEffort()),
		}, orchestration.SerenaCodexConfigArgs()...),
		ExecModelFlag: "--model",
		ExecStdinArg:  "-",
		// EVERY TURN AFTER THE FIRST RESUMES THE FIRST ONE'S THREAD. A second
		// `codex exec` in the same container is a brand new conversation;
		// `codex exec resume <thread> -` is the one that still holds the
		// charter, the change and the reviewer's own analysis
		// (go-quicbaas#987). The thread lives in CODEX_HOME, which the
		// session's container keeps between turns.
		ExecResumeArgs: []string{"resume"},
		// codex finds its credential through CODEX_HOME, not $HOME.
		CredentialDir: orchestration.CredentialDirSpec{
			Rel:     ".codex",
			Target:  "/home/agent/.codex",
			HomeEnv: "CODEX_HOME",
			// codex treats CODEX_HOME as its whole state directory and
			// refuses to start without write access to it:
			//   failed to initialize in-process app-server client:
			//   Read-only file system (os error 30)
			// The directory is a fresh per-spawn materialization of one
			// account's files, so the write cannot reach another session's
			// credential or the operator's own.
			Writable: true,
		},
		DefaultModel:      "",
		SandboxNetwork:    d.sandboxNetwork,
		SandboxSecurity:   d.sandboxSecurity,
		MCPPresetResolver: d.mcpResolver,
	})
}

// buildOpencodeBackend is the opencode CLI over ACP.
//
// ITS CATALOG INCLUDES WHAT THIS NODE'S OLLAMA SERVES. opencode routes a turn
// by its model's provider prefix, so a node serving ollama can run any model
// that ollama holds as ollama/<name> — the local review model among them. The
// node reports those through the backend models endpoint, read from ollama at
// the time of asking, rather than from a list written down in code.
func buildOpencodeBackend(d *agentBackendDeps) orchestration.AgentBackend {
	var providers []orchestration.ACPModelProvider
	if d.ollama != nil {
		providers = append(providers, orchestration.ACPModelProvider{ID: orchestration.ModelProviderOllama, Models: d.ollama})
	}
	return orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:           "opencode",
		Args:              []string{"acp"},
		DefaultModel:      "",
		SandboxNetwork:    d.sandboxNetwork,
		SandboxSecurity:   d.sandboxSecurity,
		MCPPresetResolver: d.mcpResolver,
		// Was hardcoded in the backend; stated here now so a second
		// file-credential CLI needs no edit to the backend. Same paths, same
		// read-only mount, same behaviour as before.
		CredentialDir: orchestration.CredentialDirSpec{
			Rel:    ".config/opencode",
			Target: "/home/agent/.config/opencode",
		},
		ModelProviders: providers,
	})
}

// openAIAgent is the container-and-tools agent loop against one
// OpenAI-compatible endpoint.
//
// What this backend is: an OpenAI-compatible HTTP client that runs its tool
// calls INSIDE a container. That shape is what a role needing a workspace
// requires — triage judges an issue against the code, so it must read the
// repository. The plain "ollama" backend cannot: it is a model server with no
// container and file_access false, and pointing triage at it was measured as
// "PermissionDenied: mount not allowed (extra_mounts)" — the mount policy
// correctly refusing a workspace to a backend with nowhere to put it.
//
// The endpoints differ only in route and address, so they differ only in data.
// openvino and vllm are served only when the deployment names a server
// (fleet.Config.EffectiveBackends): there is no default for them, and a
// backend registered against a guess fails on first use rather than at startup
// where an operator would see it.
func (d *agentBackendDeps) openAIAgent(endpoint openAIAgentEndpoint) orchestration.AgentBackend {
	return orchestration.NewOpenVINOBackend(orchestration.OpenVINOBackendConfig{
		BaseURL:         endpoint.base,
		ChatPath:        endpoint.chatPath,
		DefaultModel:    endpoint.model,
		ContextTokens:   endpoint.context,
		SandboxSecurity: d.sandboxSecurity,
	})
}

// openAIChatPath is the completions route every OpenAI-compatible server
// serves except OVMS, which uses /v3 (DefaultOVMSChatPath, the backend's own
// default when this is empty).
const openAIChatPath = "/v1/chat/completions"

// openAIAgentEndpoint is one served model this daemon can run an agent loop
// against: where it is, which route it speaks, what it serves, and how much
// prompt it takes.
type openAIAgentEndpoint struct {
	base     string
	chatPath string
	model    string
	context  int
}
