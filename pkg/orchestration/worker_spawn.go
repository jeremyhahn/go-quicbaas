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

import "encoding/json"

// =============================================================================
// The worker's execution environment
// =============================================================================
//
// WHETHER A WORKER CAN EDIT THE DIRECTORY IT WAS SPAWNED INTO IS A PROPERTY OF
// THE ENGINE, NOT OF WHOEVER STARTED THE DAEMON. A worker spawned with no
// permission grant assesses its work, authors the whole change, and cannot
// write one file — repeatedly, into the same environment, until its budget is
// gone. The same daemon started from a shell whose CLI settings happened to
// carry a permissive default would have succeeded, and one started by systemd
// with a clean environment would have failed for the opposite reason.
//
// Three rules follow. The spawn STATES its own grant; the process is spawned
// into the ENGINE's configuration directory rather than an operator's (the
// scrub that makes this true belongs to the backend that execs the CLI —
// scrubAgentEnv — and what belongs here is the value the engine puts in its
// place); and the spawn NAMES THE CREDENTIAL IT RUNS ON, a pooled provider
// account resolved from the engine's own pool (account_resolver.go), because a
// configuration directory the engine owns is a directory with no credential
// in it.
//
// AND THE GRANT DEPENDS ON WHERE THE SESSION RUNS. There are two, one per
// isolation: a session on the daemon host is rationed, a session in a
// container is not. See WorkerPermissionMode and WorkerFullControlFlag.

// WorkerPermissionMode is the permission grant a worker spawn carries when it
// runs ON THE DAEMON HOST.
//
// acceptEdits, NOT --dangerously-skip-permissions. The two are not degrees of
// the same thing: acceptEdits grants the file-editing tools inside the
// directory the session was spawned into — exactly the directory the engine
// prepared and will verify — while skip-permissions grants everything, Bash
// included. The operator's ExecutionGuard allowlist is the authority for what
// runs on the host (execution_guard.go), and a spawn flag that made every
// command permitted would take that authority away from the operator and give
// it to the agent.
const WorkerPermissionMode = "acceptEdits"

// WorkerFullControlFlag is the grant a worker spawn carries when it runs IN A
// CONTAINER: everything, Bash included.
//
// THE CONTAINER IS THE BOUNDARY, AND A SECOND BOUNDARY INSIDE IT ONLY COSTS
// ATTEMPTS. A sandboxed session has its own root filesystem, its own network
// policy, its own memory cgroup and no credential the engine did not put
// there; the operator's authority over what may run is exercised by deciding
// that the session is containerized at all, not by rationing tools inside it.
// Handing such a session acceptEdits grants the file tools and withholds Bash
// — so the agent cannot build, cannot test, cannot read a diff, and in a
// non-interactive `--print` session there is nobody to approve the command it
// would otherwise have to ask for.
//
// MEASURED, NOT ASSUMED (go-quicbaas#927). Session
// 570cff37-42e7-442d-b079-39d0f4623b50 spent 13m46s on one attempt and ended
// `tool_uses=0 blocks=text:6`: six blocks of prose, not one tool call, one
// attempt gone and the tree untouched. The agent narrated the work it was not
// permitted to do.
//
// IT IS ONLY EVER PAIRED WITH A CONTAINER. WorkerSpawnArgs returns it for a
// spawn whose own request states SandboxModeRequired and for no other, so no
// argument value, policy resolution or configuration order can put it on a
// session that runs as the daemon.
//
// THE FLAG IS ALSO WHY THE CONTAINER DOES NOT RUN AS ROOT. Claude Code
// refuses --dangerously-skip-permissions when its uid is 0, so a container
// given root to make `apk add` work would have taken the tool grant away
// again. Privilege inside the image is reached by passwordless sudo from the
// spawn's own uid instead (deployments/agents/software-factory-worker/
// Dockerfile), which keeps root available, keeps this flag usable, and keeps
// every file the agent writes owned by the daemon on any bind mount.
const WorkerFullControlFlag = "--dangerously-skip-permissions"

// claudeCodeBackends are the backends whose command-line program is Claude
// Code, and therefore the ones that understand --permission-mode and read
// CLAUDE_CONFIG_DIR.
//
// NAMED EXPLICITLY, because the question is which CLI is being exec'd, not
// which vendor serves the model: handing a --permission-mode flag to a program
// that does not take one does not degrade the worker, it kills the process at
// exec time. A backend absent from this set is spawned exactly as before.
var claudeCodeBackends = map[string]bool{
	"claude":      true,
	"claude-code": true,
}

// WorkerSpawnArgs is the extra argv a worker session is spawned with, or nil
// for a backend the engine has no grant to state.
//
// sandboxed is the request's OWN statement that it requires a container, never
// a policy default that may still resolve either way: an unresolved mode takes
// the host grant, because the narrower grant is the safe reading of an answer
// that is not yet known.
func WorkerSpawnArgs(backend string, sandboxed bool) []string {
	if !claudeCodeBackends[backend] {
		return nil
	}
	if sandboxed {
		return []string{WorkerFullControlFlag}
	}
	return []string{"--permission-mode", WorkerPermissionMode}
}

// serenaServerArgs is the argv that starts serena's MCP server for one agent
// CLI. ctx is serena's own per-client context, which decides the tool set and
// prompt it exposes.
//
// `--project` NAMES THE CHECKOUT ROOT, NOT THE REPOSITORY. The obvious spelling
// is `--project-from-cwd`, and it does not work here: serena walks up from the
// working directory looking for a `.git`, and the working directory is
// WorkspaceCheckoutRoot, which HOLDS the clones rather than being one. A worker
// gets the project repository and every context repository cloned side by side
// under it, so there is no `.git` at that level and serena would activate
// nothing at all — starting cleanly and then failing every symbol query.
//
// Naming the root instead is not a workaround, it is the better answer: serena
// indexes a plain directory happily, so the agent can search the company
// conventions checked out beside the project, which the worker charter tells it
// to read.
func serenaServerArgs(ctx string) []string {
	return []string{
		"start-mcp-server",
		"--transport", "stdio",
		"--context", ctx,
		"--project", WorkspaceCheckoutRoot,
	}
}

// SerenaClaudeMCPConfig is the inline MCP configuration that registers serena
// with Claude Code.
//
// INLINE JSON RATHER THAN A FILE IN THE IMAGE. `--mcp-config` takes "JSON files
// or strings", and a string means the configuration is built HERE, from
// WorkspaceCheckoutRoot, instead of being duplicated in a Dockerfile heredoc
// where nothing can check it against the constant it has to agree with.
func SerenaClaudeMCPConfig() string {
	cfg, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"serena": map[string]any{
				"type":    "stdio",
				"command": serenaCommand,
				"args":    serenaServerArgs(serenaContextClaudeCode),
			},
		},
	})
	if err != nil {
		// Unreachable: the value is a literal map of strings and slices.
		return ""
	}
	return string(cfg)
}

// SerenaCodexConfigArgs is the `-c` overrides that register serena with codex,
// which has no --mcp-config flag and parses each value as TOML.
//
// Stated here rather than at the codex backend's construction so both CLIs read
// the same server argv, and a change to the checkout root reaches both.
func SerenaCodexConfigArgs() []string {
	args, err := json.Marshal(serenaServerArgs(serenaContextCodex))
	if err != nil {
		return nil
	}
	// TOML and JSON agree on an array of quoted strings, so the marshalled
	// value is a valid TOML array as it stands.
	return []string{
		"-c", `mcp_servers.serena.command="` + serenaCommand + `"`,
		"-c", "mcp_servers.serena.args=" + string(args),
	}
}

const (
	// serenaCommand is the executable the worker image installs.
	serenaCommand = "serena"

	// serenaContextClaudeCode and serenaContextCodex are serena's own built-in
	// client contexts. Handing a CLI the other's context would advertise tools
	// it cannot call.
	serenaContextClaudeCode = "claude-code"
	serenaContextCodex      = "codex"
)

// WorkerMCPArgs is the extra argv that registers serena with a worker's CLI,
// or nil for a spawn that cannot use it.
//
// SANDBOXED ONLY. serena is installed in the worker IMAGE, and the checkout root
// it is pointed at exists only inside the container; a host session has neither.
// Claude Code treats an unusable --mcp-config as a startup error, which would
// turn "serena is unavailable" — a degradation — into "the worker never
// started", which is an outage.
//
// Only Claude Code takes this shape. codex is configured through
// SerenaCodexConfigArgs where that backend is registered, because it has no
// equivalent flag; a backend absent from claudeCodeBackends is spawned exactly
// as before.
func WorkerMCPArgs(backend string, sandboxed bool) []string {
	if !sandboxed || !claudeCodeBackends[backend] {
		return nil
	}
	return []string{"--mcp-config", SerenaClaudeMCPConfig()}
}

// WorkerSpawnEnv is the environment the engine puts on a worker spawn, or
// nil when there is nothing to say.
//
// One entry today: the configuration directory. It is the positive half of the
// environment contract — the backend scrubs the operator's CLAUDE_* variables
// out of the inherited environment, and this puts the engine's own directory
// in, so a worker's settings, and therefore its behaviour, are the engine's.
func WorkerSpawnEnv(backend, configDir string) map[string]string {
	if configDir == "" || !claudeCodeBackends[backend] {
		return nil
	}
	return map[string]string{"CLAUDE_CONFIG_DIR": configDir}
}

// pooledAccountBackends are the backends whose credential the engine supplies
// from its own account pool, rather than leaving the CLI to authenticate
// however it already does.
//
// IT MUST AGREE WITH THE SERVER'S ATTACHMENT LIST — pkg/server,
// lifecycle_orchestration.go wires the AccountResolver onto exactly "claude",
// "claude-code" and "opencode" — and it is written out here rather than
// derived because the consequence of disagreeing is asymmetric. A backend
// listed here that has no resolver fails every spawn closed
// (AccountResolverNotConfiguredError), which is loud; a backend the server
// DOES pool but this set omits goes on spawning with whatever ambient
// identity it finds, which is silent.
//
// "claude-code" is present for the same reason it is present in
// claudeCodeBackends: it is an alias an operator may configure for the same
// program.
//
// "codex" IS POOLED, AND ITS ABSENCE WAS THE SILENT HALF OF THAT ASYMMETRY.
// ReviewPolicy.RequireIndependentReviewer refuses an approval from the model
// family that wrote the code, so a project with a claude worker reviews with
// codex — and codex's credential is a ChatGPT subscription in a FILE
// (AccountCredential.Files -> ~/.codex/auth.json), not an environment
// variable. With codex missing here, ApplyWorkerSpawnEnvironment never set
// AccountAuto, resolveAccount returned early, no home directory was
// materialized, and the codex process exited before its first turn with
// "acp: transport failed: initialize" — a credential failure wearing a
// transport error's clothes. Measured on the burn-in node 2026-09-13.
//
// The daemon side needs no list change: pkg/fleet's Runtime attaches the
// resolver to EVERY ACP backend it serves, deliberately, to avoid exactly this
// kind of name-list omission. The server-local path is the one that still
// disagrees — its codex is a CodexExecBackend, which has no
// SetAccountResolver — so an in-process codex spawn now fails closed and loud
// rather than silently unauthenticated. That is the correct direction of the
// two, and giving CodexExecBackend resolver support is the follow-on.
//
// Each maps to the provider whose accounts it draws on, which is what an
// Agent naming accounts is checked against when it is saved.
var pooledAccountBackends = map[string]AccountProvider{
	"claude":      AccountProviderClaude,
	"claude-code": AccountProviderClaude,
	"codex":       AccountProviderCodex,
	"opencode":    AccountProviderOpenCode,
}

// PooledAccountProvider returns the provider whose pooled accounts a backend
// draws on, and false for a backend that draws on none.
func PooledAccountProvider(backend string) (AccountProvider, bool) {
	provider, ok := pooledAccountBackends[backend]
	return provider, ok
}

// WorkerSpawnAccountAuto reports whether a worker spawn on this backend and
// model must resolve a pooled provider account.
//
// Auto, never a pinned id: the engine asks for "an account this tenant and
// project may use" and lets the pool's strategy choose, which is what makes
// the pool a pool — accounts can be added, retired, rate-limited and rotated
// without a task naming any of them.
//
// A MODEL ON A LOCAL PROVIDER NEEDS NO ACCOUNT. opencode routes a turn by its
// model's provider, and "ollama/review-q8:latest" is served by an ollama the
// fleet runs, with no credential to present. Demanding a pooled opencode
// account there would refuse the spawn with NoAccountAvailableError on a node
// that has everything it needs (SpawnNeedsNoAccount).
func WorkerSpawnAccountAuto(backend, model string) bool {
	_, pooled := pooledAccountBackends[backend]
	return pooled && !SpawnNeedsNoAccount(backend, model)
}

// ApplyWorkerSpawnEnvironment puts the engine's grant, configuration
// directory and credential onto a spawn request bound for a prepared
// directory. It appends to ExtraArgs and Env rather than replacing them, so a
// workflow's own agent-spec settings survive; an agent spec that already
// states a permission mode keeps it.
//
// THE ALREADY-STATED GRANT IS HOW A ROLE NARROWS ITSELF. A runner whose
// session must not be handed everything — triage, which reads a repository and
// writes nothing — states its own --permission-mode, and this leaves it alone.
// That is the opt-out, and it is deliberately explicit: a role that says
// nothing gets the grant its isolation earns.
func ApplyWorkerSpawnEnvironment(req *SpawnSessionRequest, configDir string) {
	if req == nil {
		return
	}
	sandboxed := req.EffectiveSandboxMode() == SandboxModeRequired
	if !hasPermissionGrant(req.ExtraArgs) {
		req.ExtraArgs = append(req.ExtraArgs, WorkerSpawnArgs(req.Backend, sandboxed)...)
	}
	// The MCP registration is NOT behind the permission-grant check. That check
	// exists so a role which narrows its own grant keeps it — triage states its
	// own --permission-mode and must not be handed everything — but a role that
	// narrows its permissions still wants to read code efficiently, and serena
	// grants nothing: it reads the checkout the session already has.
	if !hasMCPConfig(req.ExtraArgs) {
		req.ExtraArgs = append(req.ExtraArgs, WorkerMCPArgs(req.Backend, sandboxed)...)
	}
	for k, v := range WorkerSpawnEnv(req.Backend, configDir) {
		if req.Env == nil {
			req.Env = make(map[string]string)
		}
		req.Env[k] = v
	}
	if WorkerSpawnAccountAuto(req.Backend, req.Model) && req.AccountID == "" {
		req.AccountAuto = true
	}
}

// hasPermissionGrant reports whether argv already states a permission grant,
// in either of the two forms the CLI accepts.
func hasPermissionGrant(args []string) bool {
	for _, a := range args {
		if a == "--permission-mode" || a == WorkerFullControlFlag {
			return true
		}
	}
	return false
}

// hasMCPConfig reports whether argv already names an MCP configuration.
//
// A caller that states its own is stating the whole set: Claude Code merges
// every --mcp-config it is given, so appending the engine's beside a
// deliberately narrowed one would put back the server that caller left out.
func hasMCPConfig(args []string) bool {
	for _, a := range args {
		if a == "--mcp-config" || a == "--strict-mcp-config" {
			return true
		}
	}
	return false
}
