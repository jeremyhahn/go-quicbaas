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
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// streamJSONScannerBufferSize is the maximum buffer size for the bufio.Scanner
// used to read stream-json output from Claude Code. Claude responses can be
// very large, so we use a 4MB buffer to avoid truncation.
const streamJSONScannerBufferSize = 4 * 1024 * 1024

// stderrTailLimit bounds the number of trailing stderr bytes retained for a
// persistent subprocess. The tail is surfaced in StreamJSONProcessExitedError
// so an operator sees the agent's own failure message instead of a bare
// "broken pipe". A few KB is enough to carry the relevant error lines without
// letting a chatty process consume unbounded memory.
const stderrTailLimit = 8 * 1024

// stderrTail is an io.Writer that retains only the last stderrTailLimit bytes
// written to it. It is wired as the persistent subprocess's Stderr so the
// captured tail is available when the process exits unexpectedly. It is
// safe for concurrent use: the os/exec copier goroutine writes while a prompt
// goroutine reads via String() after the process exits.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

// Write appends p to the tail, discarding the oldest bytes beyond the limit.
func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTailLimit {
		t.buf = t.buf[len(t.buf)-stderrTailLimit:]
	}
	return len(p), nil
}

// String returns the retained stderr tail, trimmed of surrounding whitespace.
func (t *stderrTail) String() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// ACPBackendConfig holds configuration for the ACP backend.
type ACPBackendConfig struct {
	Command      string   `json:"command"`
	Args         []string `json:"args"`
	DefaultModel string   `json:"default_model"`
	TeamMode     bool     `json:"team_mode"`

	// CLIMode indicates the backend uses a persistent CLI subprocess
	// communicating via Claude Code's stream-json protocol rather than
	// the ACP JSON-RPC protocol. When true, Spawn starts a long-running
	// "claude --print --input-format stream-json --output-format stream-json
	// --verbose" subprocess. Multiple prompts reuse the same process, and
	// the agent maintains full conversation context across turns.
	CLIMode bool `json:"cli_mode"`

	// NativeSessionResume selects the agent CLI's own session store as the
	// resume mechanism for persistent CLI-mode sessions.
	//
	// When false (the default) the CLI is started with
	// --no-session-persistence: it keeps no transcript on the host, and a
	// session interrupted by a daemon restart is reconstituted by replaying
	// the durable, barrier-encrypted transcript from the SessionEventLog into
	// a fresh process (ResumeModeReplay).
	//
	// When true the flag is withheld, so the CLI writes its own transcript to
	// the daemon's home directory and a resume is performed with
	// "--resume <session-id>" (ResumeModeNative). Fidelity is complete — every
	// tool call and result is restored, and nothing is re-sent by us — at the
	// cost of a plaintext copy of the conversation living on the host
	// filesystem, outside the storage barrier. Enable it deliberately, for
	// hosts whose filesystem is already in the trust boundary.
	NativeSessionResume bool `json:"native_session_resume"`

	// SandboxNetwork is the container network sandboxed sessions run in. The
	// empty value resolves to SandboxNetworkDefault (an isolated bridge
	// namespace with egress); SandboxNetworkHost — which removes network
	// isolation and exposes every loopback service on the host — is reachable
	// only by setting it here, never by a spawn request. See
	// sandbox_network.go.
	SandboxNetwork SandboxNetwork `json:"sandbox_network,omitempty"`

	// MCPPresetResolver, when non-nil, resolves the empty-URL QuicBaaS MCP
	// preset (MCPServerConfig with Name=="quicbaas" and URL=="") at spawn
	// time into a concrete endpoint URL plus a fresh per-spawn mTLS agent
	// certificate. It is implemented at the server layer; leaving it nil
	// disables preset resolution and the preset is forwarded to the agent
	// unmodified (harmless — it carries no URL). Not serialized: it is a
	// runtime wiring dependency, not configuration state.
	MCPPresetResolver MCPPresetResolver `json:"-"`
}

// TransportFactory creates a StdioTransport. This is used for dependency
// injection in tests.
type TransportFactory func(command string, args []string, workDir string, env []string) (*StdioTransport, error)

// ProcessFactory creates an *exec.Cmd for the stream-json CLI mode. This is
// used for dependency injection in tests to avoid spawning real subprocesses.
type ProcessFactory func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error)

// ACPBackend implements AgentBackend for ACP-compatible agents such as
// Claude Code, Codex, and OpenCode. It supports two modes:
//
//   - JSON-RPC mode: Persistent subprocess communicating via JSON-RPC over stdio
//     (used by Codex and OpenCode which implement the ACP protocol).
//   - CLI mode: Persistent subprocess communicating via Claude Code's stream-json
//     protocol. A single long-running process handles multiple prompts with full
//     conversation context.
type ACPBackend struct {
	config           ACPBackendConfig
	sessions         sync.Map // map[string]*acpSession
	transportFactory TransportFactory
	processFactory   ProcessFactory
	containerRuntime ContainerRuntime  // optional: for sandboxed agent execution
	logger           interfaces.Logger // optional: session-registry diagnostics
	accountResolver  AccountResolver   // optional: pooled provider account selection (see account_resolver.go)

	// sandboxNetworkWarning makes the host-network / invalid-network advisory
	// fire once per backend instead of once per spawn. See sandboxNetwork.
	sandboxNetworkWarning sync.Once
}

// SetContainerRuntime configures the container runtime for sandboxed sessions.
func (b *ACPBackend) SetContainerRuntime(rt ContainerRuntime) {
	b.containerRuntime = rt
}

// SetAccountResolver wires the pooled provider account resolver used when a
// SpawnRequest sets AccountID or AccountAuto. Leaving this unset (the
// default) preserves the pre-account-pool behavior exactly: any spawn that
// does not name AccountID/AccountAuto never consults it, and one that does
// fails closed with AccountResolverNotConfiguredError rather than silently
// falling back to the host's shared identity.
func (b *ACPBackend) SetAccountResolver(r AccountResolver) {
	b.accountResolver = r
}

// SetLogger installs an optional logger used to emit session-registry
// diagnostics: the key a session is stored under at Spawn time and the key a
// follow-up Prompt / Status / Close looks up. These logs are the canonical
// evidence for diagnosing "acp: session not found" — they make the
// create-key vs lookup-key comparison observable without a debugger. Passing
// nil disables the diagnostics. Safe to call once during wiring.
func (b *ACPBackend) SetLogger(l interfaces.Logger) {
	b.logger = l
}

// storeSession records a session under its id and emits a create-key
// diagnostic so operators can correlate it with later lookups. Centralising
// the b.sessions.Store call guarantees every spawn path logs the same way.
func (b *ACPBackend) storeSession(sess *acpSession) {
	b.sessions.Store(sess.id, sess)
	if b.logger != nil {
		b.logger.Debugf("acp_backend: session stored: create_key=%s mode=%s",
			sess.id, b.sessionMode(sess))
	}
}

// sessionMode returns a short label describing how a session was spawned, used
// only in diagnostic logs.
func (b *ACPBackend) sessionMode(sess *acpSession) string {
	switch {
	case sess.cmd != nil:
		return "cli-persistent"
	case sess.transport != nil:
		return "jsonrpc"
	default:
		return "cli-oneshot"
	}
}

// logLookupMiss emits a lookup-key diagnostic listing the keys actually present
// in the registry, so a "session not found" can be diagnosed as a key mismatch
// (lookup_key absent but other keys present) vs an empty registry.
func (b *ACPBackend) logLookupMiss(op, lookupKey string) {
	if b.logger == nil {
		return
	}
	present := make([]string, 0)
	b.sessions.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok {
			present = append(present, k)
		}
		return true
	})
	b.logger.Warnf("acp_backend: %s session miss: lookup_key=%s present_keys=%v",
		op, lookupKey, present)
}

// acpSession tracks an active ACP session and its configuration.
type acpSession struct {
	id string

	// agentID is the identifier the AGENT knows this session by, which is not
	// always the identifier QuicBaaS registers it under. A resumed JSON-RPC
	// session keeps the operator's original ID in the registry while the fresh
	// container hands back a brand new ACP session id; sending the registry's
	// ID on session/prompt would then address a session the agent has never
	// heard of. Empty means the two are the same, which is every non-resumed
	// session. Read through promptSessionID.
	agentID string

	transport    *StdioTransport    // non-nil for JSON-RPC mode sessions
	cmd          *exec.Cmd          // non-nil for CLI stream-json mode sessions
	procCancel   context.CancelFunc // cancels the persistent subprocess on Close
	stderr       *stderrTail        // bounded capture of the subprocess stderr
	stdin        io.WriteCloser     // stream-json stdin pipe
	stdout       *bufio.Scanner     // stream-json stdout reader
	mu           sync.Mutex         // protects stdin/stdout for serial access
	projectID    string
	model        string
	workDir      string
	env          map[string]string
	systemPrompt string
	extraArgs    []string // additional CLI args for the agent command
	createdAt    int64
	lastActivity atomic.Int64
	closed       atomic.Bool

	// cleanup, when non-nil, releases per-session resources provisioned at
	// spawn (currently the ephemeral QuicBaaS MCP agent-cert directory; the
	// pooled-account credential directory and the container env file have
	// their own slots below). It runs exactly
	// once on Close. Guarded by mu so the Spawn-time attach and the Close-time
	// read/clear do not race.
	cleanup func()

	// accountID and accountEnvKeys record the pooled provider account this
	// session's credential material came from, and precisely which environment
	// variables that material was projected into. Both are guarded by mu.
	//
	// The key list is what makes an in-place rotation safe on the one-shot CLI
	// path: RotateAccount retracts exactly these variables before merging the
	// replacement account's, so the next process cannot inherit a stale
	// credential the new account happens not to overwrite.
	accountID      string
	accountEnvKeys []string

	// accountCleanup releases the materialized credential directory of the
	// account currently in force. It is a SEPARATE slot from cleanup because a
	// rotation replaces the account's material while the session's ephemeral
	// MCP certificate material stays exactly where it is; fusing the two would
	// force a rotation to either leak the cert directory or delete a
	// certificate the running agent is still presenting.
	accountCleanup func()

	// envFileCleanup removes the private 0600 --env-file the container runtime
	// reads this session's environment from (InteractiveProcess.Cleanup). It is
	// a THIRD, independent slot for the same reason accountCleanup is separate
	// from cleanup: the file belongs to the running `docker run` process, so it
	// must survive an account rotation (which rewrites credential material) and
	// must not be released by a cert-directory teardown. Guarded by mu.
	envFileCleanup func()
}

// promptSessionID returns the identifier to address the agent with on the ACP
// wire: the agent's own session id when a resume made it differ from the one
// QuicBaaS registered, otherwise the registered id.
func (s *acpSession) promptSessionID() string {
	if s.agentID != "" {
		return s.agentID
	}
	return s.id
}

// streamJSONMessage represents a message in Claude Code's stream-json protocol.
// The stream-json protocol emits newline-delimited JSON messages on stdout:
//   - Init:      {"type":"system","subtype":"init","session_id":"...","model":"..."}
//   - Assistant:  {"type":"assistant","message":{"role":"assistant","content":[...],"stop_reason":"end_turn"}}
//   - Result:     {"type":"result","subtype":"success","result":"...","session_id":"...","is_error":false}
type streamJSONMessage struct {
	Type      string               `json:"type"`
	Subtype   string               `json:"subtype,omitempty"`
	SessionID string               `json:"session_id,omitempty"`
	Model     string               `json:"model,omitempty"`
	IsError   bool                 `json:"is_error,omitempty"`
	Result    string               `json:"result,omitempty"`
	Message   *streamJSONAssistant `json:"message,omitempty"`

	// Usage is the turn-level token accounting carried on the terminal
	// "result" message. It is the authoritative figure for the turn: the CLI
	// aggregates every assistant message it emitted into it, so when it is
	// present the per-assistant-message counts must NOT also be added or the
	// turn would be billed twice. See collectStreamJSONUsage.
	Usage *TokenUsage `json:"usage,omitempty"`
}

// streamJSONAssistant represents the assistant message body in stream-json output.
type streamJSONAssistant struct {
	Role       string              `json:"role"`
	Content    []streamJSONContent `json:"content,omitempty"`
	StopReason string              `json:"stop_reason,omitempty"`
	Model      string              `json:"model,omitempty"`

	// Usage is the per-message token accounting the CLI publishes alongside
	// each assistant message. It is only used when the terminal result message
	// omits its aggregate (see streamJSONMessage.Usage).
	Usage *TokenUsage `json:"usage,omitempty"`
}

// streamJSONContent represents one content block within a stream-json
// assistant or user message. Type discriminates which of the optional field
// groups is populated: "text" (Text), "thinking" (Thinking), "tool_use"
// (ID/Name/Input), or "tool_result" (ToolUseID/Content/IsError — carried on
// user messages echoing a tool's output back to the model).
type streamJSONContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	// Thinking carries the chain-of-thought text of a "thinking" block.
	Thinking string `json:"thinking,omitempty"`

	// ID, Name, and Input describe a "tool_use" block.
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// ToolUseID, Content, and IsError describe a "tool_result" block. Content
	// is raw because the dialect emits either a JSON string or an array of
	// {type,text} blocks; see flattenToolResultContent.
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// streamJSONUserMessage is the input format for sending prompts to Claude Code
// via the stream-json protocol on stdin.
type streamJSONUserMessage struct {
	Type    string                `json:"type"`
	Message streamJSONUserContent `json:"message"`
}

// streamJSONUserContent is the message body of a stream-json user input.
type streamJSONUserContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Compile-time interface check.
var _ AgentBackend = (*ACPBackend)(nil)

// NewACPBackend creates a new ACP backend with the given configuration.
func NewACPBackend(config ACPBackendConfig) *ACPBackend {
	return &ACPBackend{
		config:           config,
		transportFactory: NewStdioTransport,
		processFactory:   defaultProcessFactory,
	}
}

// defaultProcessFactory creates an *exec.Cmd for the stream-json CLI mode.
func defaultProcessFactory(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stderr = io.Discard
	// Ensure the process and its children are interrupted on context cancellation.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	return cmd, nil
}

// processExitedError builds a StreamJSONProcessExitedError describing why a
// persistent stream-json subprocess is gone. It reads the exit code from the
// (already-reaped) cmd.ProcessState and attaches the captured stderr tail so
// the surfaced error reads like "claude exited code 1: <stderr>" instead of a
// bare "broken pipe". Callers must have reaped the process (cmd.Wait) before
// calling so ProcessState is populated.
func (b *ACPBackend) processExitedError(cmd *exec.Cmd, stderr *stderrTail) *StreamJSONProcessExitedError {
	exitCode := 0
	if cmd != nil && cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	return &StreamJSONProcessExitedError{
		Command:  b.config.Command,
		ExitCode: exitCode,
		Stderr:   stderr.String(),
	}
}

// subprocessGoneError handles a persistent stream-json session whose subprocess
// has died mid-flight (write broken pipe or scanner EOF). It reaps the process
// so the exit code is known, marks the session closed so its state reflects the
// failure, and returns an actionable StreamJSONProcessExitedError. It is invoked
// while holding sess.mu, so the reap/close transition is serialised.
func (b *ACPBackend) subprocessGoneError(sess *acpSession) *StreamJSONProcessExitedError {
	if sess.procCancel != nil {
		sess.procCancel()
	}
	if sess.cmd != nil {
		_ = sess.cmd.Wait()
	}
	sess.closed.Store(true)
	return b.processExitedError(sess.cmd, sess.stderr)
}

// Spawn creates a new agent session. In CLI mode, this starts a persistent
// stream-json subprocess. In JSON-RPC mode, it starts a persistent subprocess
// and performs the ACP initialization handshake.
func (b *ACPBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.WorkDir == "" {
		return nil, &InputValidationError{Field: "work_dir", Message: "cannot be empty"}
	}

	// Resolve any empty-URL QuicBaaS MCP preset into a concrete endpoint URL
	// plus a fresh per-spawn mTLS agent certificate written to an ephemeral
	// directory. This MUST run before injectMCPServersEnv so the populated URL
	// and cert paths are carried in the QUICBAAS_MCP_SERVERS payload. The
	// returned cleanup deletes the ephemeral cert directory; it is attached to
	// the session below so it fires on Close, scoping the certificate's
	// lifetime to the agent it was minted for.
	return b.provisionAndSpawn(ctx, req, func() (*Session, error) {
		return b.dispatchSpawn(ctx, req)
	})
}

// provisionAndSpawn runs the per-spawn provisioning both Spawn and Resume
// need, then invokes start. Provisioning mints ephemeral MCP material whose
// lifetime is scoped to the session it was minted for, so the resulting
// cleanup is attached to the started session — or run immediately when the
// start fails and there is no session to attach it to.
func (b *ACPBackend) provisionAndSpawn(
	ctx context.Context,
	req *SpawnRequest,
	start func() (*Session, error),
) (*Session, error) {
	mcpCleanup := b.resolveMCPPresets(ctx, req)

	// Resolve the pooled provider account (if the caller asked for one) BEFORE
	// dispatch, exactly like the MCP preset resolution above: it mutates
	// req.Env / req.AccountHomeDir in place so every dispatch path (host or
	// container) picks up the credential the same way it already picks up
	// everything else in req.Env. See resolveAccount for the isolation
	// guarantees this provides.
	account, err := b.resolveAccount(ctx, req)
	if err != nil {
		if mcpCleanup != nil {
			mcpCleanup()
		}
		return nil, err
	}

	// Materialize any preconfigured MCP servers into the agent's environment
	// BEFORE dispatch, so the resulting env reaches the subprocess on the
	// initial spawn AND is stored on the session for the prompt-time relaunch.
	// MCP access is delivered through the environment (not the ACP session/new
	// mcpServers param, whose shape the agents do not share); see
	// injectMCPServersEnv for the contract.
	injectMCPServersEnv(req)

	sess, err := start()
	if err != nil {
		// Resolution succeeded but the spawn failed: there is no session to
		// attach the cleanup to, so release the ephemeral cert / account
		// material now.
		if mcpCleanup != nil {
			mcpCleanup()
		}
		if account != nil && account.cleanup != nil {
			account.cleanup()
		}
		return nil, err
	}
	b.attachCleanup(sess.ID, mcpCleanup)
	if account != nil {
		sess.AccountID = account.AccountID
		sess.AccountStrategy = account.Strategy
		b.attachAccount(sess.ID, account)
	}
	return sess, nil
}

// attachAccount records the resolved account on the live session so a later
// rotation knows exactly which environment variables to retract. Best effort:
// a session that has already been closed simply has nothing to record against,
// and the credential material it held is released by its own cleanup.
func (b *ACPBackend) attachAccount(sessionID string, account *resolvedSpawnAccount) {
	if account == nil {
		return
	}
	val, ok := b.sessions.Load(sessionID)
	if !ok {
		return
	}
	sess, ok := val.(*acpSession)
	if !ok {
		return
	}
	sess.mu.Lock()
	sess.accountID = account.AccountID
	sess.accountEnvKeys = account.EnvKeys
	previous := sess.accountCleanup
	sess.accountCleanup = account.cleanup
	sess.mu.Unlock()

	// A rotation replaces the credential directory of the account it is moving
	// off. Releasing the previous one here — after the replacement is in place
	// — is what keeps a rotating session from accumulating one materialized
	// credential tree per rotation.
	if previous != nil {
		previous()
	}
}

// dispatchSpawn routes a validated, MCP-resolved spawn request to the correct
// spawn path based on the request and backend config.
func (b *ACPBackend) dispatchSpawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	// CLI mode (stream-json) is used when the backend supports it AND the
	// request explicitly asks for a persistent session.
	if b.config.CLIMode && req.Persistent {
		return b.spawnCLI(ctx, req, spawnPlan{})
	}

	// One-shot CLI mode: store session config without starting a subprocess.
	// Each prompt spawns a fresh process with no conversation context.
	if b.config.CLIMode && !req.Persistent {
		return b.spawnOneShotCLI(req)
	}

	// Sandbox-first policy routing. The orchestrator layer resolves
	// SandboxModeDefault before dispatching, so at this point the mode is
	// either Required (container) or None (raw exec). Legacy Sandboxed=true
	// callers are mapped via EffectiveSandboxMode().
	switch req.EffectiveSandboxMode() {
	case SandboxModeRequired:
		if b.containerRuntime == nil {
			return nil, &SandboxModeViolationError{
				Requested: SandboxModeRequired,
				Policy:    SandboxPolicyRequired,
				Reason:    "backend has no container runtime configured",
			}
		}
		return b.spawnContainerJSONRPC(ctx, req, spawnPlan{})
	case SandboxModeNone, SandboxModeDefault:
		return b.spawnJSONRPC(ctx, req)
	default:
		return nil, &InputValidationError{
			Field:   "sandbox_mode",
			Message: "unknown mode value",
		}
	}
}

// spawnContainerJSONRPC starts an agent in a container using RunInteractive
// and establishes an ACP JSON-RPC session over the container's stdio pipes.
func (b *ACPBackend) spawnContainerJSONRPC(ctx context.Context, req *SpawnRequest, plan spawnPlan) (*Session, error) {
	image := req.ContainerImage
	if image == "" {
		return nil, &InputValidationError{Field: "container_image", Message: "required for sandboxed sessions"}
	}

	// Build command for the container. Use the binary name (not the host path)
	// since the container has the agent installed at a different location.
	binaryName := filepath.Base(b.config.Command)
	command := []string{binaryName}
	command = append(command, b.config.Args...)
	command = append(command, req.ExtraArgs...)

	// Build container spec. Run as the host user so files written to the
	// bind-mounted workspace have the correct ownership. Set HOME to /tmp
	// so OpenCode can write its internal state without permission issues.
	currentUser := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	if req.Env == nil {
		req.Env = make(map[string]string)
	}
	req.Env["HOME"] = "/tmp"

	// Build container spec
	spec := &ContainerSpec{
		Image:      image,
		Command:    command,
		Entrypoint: []string{}, // Clear entrypoint, use command directly
		Env:        req.Env,
		WorkDir:    "/workspace",
		AutoRemove: true,
		Network:    b.sandboxNetwork().String(),
		User:       currentUser,
		Labels: map[string]string{
			"quicbaas.agent":   b.Type(),
			"quicbaas.session": "pending",
		},

		// Trust dimension (container_trust.go / trust_policy.go). Every
		// field is inert at its zero value, so a spawn that does not ask
		// for trusted computing builds the same spec it always did.
		ImageDigest:  req.ImageDigest,
		TrustLevel:   req.TrustLevel,
		TrustLabel:   req.TrustLabel,
		ExpectedPCRs: req.ExpectedPCRs,
	}

	// Mount workspace if specified
	if req.WorkDir != "" && req.WorkDir != WorkDirCurrent {
		spec.Mounts = []ContainerMount{{
			Source: req.WorkDir,
			Target: "/workspace",
		}}
	}

	// Extra mounts were authorized against the orchestrator's mount policy
	// before the backend ever saw this request; append them verbatim.
	spec.Mounts = append(spec.Mounts, req.ExtraMounts...)

	// Mount the OpenCode config directory so the container has provider
	// configuration and npm packages for custom providers like Ollama.
	//
	// When the spawn resolved a pooled account with file-based credentials
	// (see resolveAccount / account_materialize.go), req.AccountHomeDir names
	// a FRESH, per-session directory holding only that account's files —
	// mount THAT instead of the host's shared config, so two concurrent
	// sandboxed sessions using different accounts can never observe each
	// other's credentials (or the operator's own, unrelated, real config).
	// Absent an account selection, fall back to the pre-existing behavior
	// unchanged.
	if req.AccountHomeDir != "" {
		configDir := req.AccountHomeDir + "/.config/opencode"
		if _, statErr := os.Stat(configDir); statErr == nil {
			spec.Mounts = append(spec.Mounts, ContainerMount{
				Source:   configDir,
				Target:   "/home/agent/.config/opencode",
				ReadOnly: true,
			})
		}
	} else if home, _ := os.UserHomeDir(); home != "" {
		configDir := home + "/.config/opencode"
		if _, statErr := os.Stat(configDir); statErr == nil {
			spec.Mounts = append(spec.Mounts, ContainerMount{
				Source:   configDir,
				Target:   "/home/agent/.config/opencode",
				ReadOnly: true,
			})
		}
	}

	// Decouple the container lifetime from the spawning request, exactly as the
	// persistent CLI path does (see spawnCLI). RunInteractive binds the
	// `docker run -i --rm` process to its ctx via exec.CommandContext, and the
	// Spawn RPC ctx is cancelled by gRPC the moment the Spawn handler returns —
	// so passing the request ctx kills the container immediately after Spawn and
	// the follow-up Prompt finds a closed stdio transport ("session closed").
	// context.WithoutCancel preserves request-scoped values (tenant, trace) while
	// detaching cancellation; procCancel is stored on the session and fired only
	// by Close. The handshake Sends below still honour the caller's ctx for
	// spawn-time timeout/cancel.
	procCtx, procCancel := context.WithCancel(context.WithoutCancel(ctx))

	proc, err := b.containerRuntime.RunInteractive(procCtx, spec)
	if err != nil {
		procCancel()
		return nil, &SpawnError{Command: image, Cause: err}
	}

	// Create transport from container pipes
	transport := newStdioTransportFromPipes(proc.Stdin, proc.Stdout)

	// teardown aborts a partially-spawned container: it closes the stdio
	// transport, cancels the container process context (killing `docker run`
	// and triggering --rm cleanup), and removes the private env file that
	// process was reading its environment from. Centralising it keeps every
	// handshake failure path leak-free.
	teardown := func() {
		_ = transport.Close()
		procCancel()
		if proc.Cleanup != nil {
			proc.Cleanup()
		}
	}

	// ACP handshake: initialize
	initParams := &InitializeParams{
		ProtocolVersion: 1,
		ClientInfo: ClientInfo{
			Name:    "go-quicbaas",
			Version: "1.0.0",
		},
	}

	resp, initErr := transport.Send(ctx, "initialize", initParams)
	if initErr != nil {
		teardown()
		return nil, &TransportError{Operation: "initialize", Cause: initErr}
	}
	if resp.Error != nil {
		teardown()
		return nil, &ProtocolError{Message: resp.Error.Message}
	}

	// Resolve model
	model := req.Model
	if model == "" {
		model = b.config.DefaultModel
	}

	// ACP handshake: session/new
	// Always pass empty mcpServers — QuicBaaS MCP scope configs are not
	// compatible with ACP agent MCP server configs. The agent container
	// gets its MCP access via the container environment, not via session params.
	sessionParams := &SessionNewParams{
		CWD:          "/workspace",
		MCPServers:   []MCPServerConfig{},
		Model:        model,
		SystemPrompt: req.SystemPrompt,
	}

	resp, sessionErr := transport.Send(ctx, "session/new", sessionParams)
	if sessionErr != nil {
		teardown()
		return nil, &TransportError{Operation: "session/new", Cause: sessionErr}
	}
	if resp.Error != nil {
		teardown()
		return nil, &ProtocolError{Message: resp.Error.Message}
	}

	// Parse session result
	resultBytes, _ := json.Marshal(resp.Result)
	var sessionResult SessionNewResult
	if jsonErr := json.Unmarshal(resultBytes, &sessionResult); jsonErr != nil {
		teardown()
		return nil, &ProtocolError{Message: "failed to parse session result", Cause: jsonErr}
	}

	agentSessionID := sessionResult.SessionID
	if agentSessionID == "" {
		var genErr error
		agentSessionID, genErr = generateSessionID()
		if genErr != nil {
			teardown()
			return nil, &SpawnError{Command: image, Cause: genErr}
		}
	}

	// A resume keeps the identifier the operator already holds. The container
	// is brand new and reported its own ACP session id, so the two diverge
	// here and stay divergent for the life of the session — see
	// acpSession.agentID.
	sessionID := agentSessionID
	if plan.registerID != "" {
		sessionID = plan.registerID
	}

	now := time.Now().UnixNano()
	sess := &acpSession{
		id:           sessionID,
		agentID:      agentSessionID,
		transport:    transport,
		procCancel:   procCancel,
		model:        model,
		workDir:      req.WorkDir,
		projectID:    req.ProjectID,
		env:          req.Env,
		systemPrompt: req.SystemPrompt,
		createdAt:    now,

		// The container process outlives this call, so the env file it reads
		// from is released by Close — not here.
		envFileCleanup: proc.Cleanup,
	}
	sess.lastActivity.Store(now)
	b.storeSession(sess)

	// Replay priming. Unlike the CLI path — where the transcript replaces a
	// handshake message the spawn was going to pay for anyway — the ACP
	// handshake carries no user turn, so restoring a sandboxed conversation
	// costs one real prompt. That is stated plainly rather than hidden: a
	// container resume is more expensive than a CLI resume, and the alternative
	// (a fresh agent wearing the old session's ID) is not an option.
	if plan.priming != "" {
		if _, primeErr := b.promptJSONRPC(ctx, sess, &PromptRequest{
			SessionID: sessionID,
			Prompt:    plan.priming,
		}); primeErr != nil {
			b.sessions.Delete(sessionID)
			teardown()
			return nil, &SpawnError{Command: image, Cause: primeErr}
		}
	}

	return &Session{
		ID:        sessionID,
		Backend:   b.Type(),
		Model:     model,
		CreatedAt: now,
	}, nil
}

// spawnOneShotCLI creates a session that runs a fresh CLI process per prompt.
// No subprocess is started; the session stores config only. Each call to Prompt
// spawns a new "claude --print --output-format json" process.
func (b *ACPBackend) spawnOneShotCLI(req *SpawnRequest) (*Session, error) {
	model := req.Model
	if model == "" {
		model = b.config.DefaultModel
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	now := time.Now()
	sess := &acpSession{
		id:           sessionID,
		model:        model,
		workDir:      req.WorkDir,
		projectID:    req.ProjectID,
		env:          req.Env,
		systemPrompt: req.SystemPrompt,
		extraArgs:    req.ExtraArgs,
		createdAt:    now.UnixNano(),
	}
	sess.lastActivity.Store(now.UnixNano())
	b.storeSession(sess)

	return &Session{
		ID:        sessionID,
		Backend:   b.Type(),
		Model:     model,
		CreatedAt: now.UnixNano(),
	}, nil
}

// spawnPlan describes a variation on the standard persistent spawn. Its zero
// value is a plain spawn; a resume populates it to reconstitute an existing
// session instead of creating a new one.
type spawnPlan struct {
	// registerID overrides the identifier the session is registered under.
	// A resume sets it to the original session ID so the reconstituted
	// process answers to the identifier the operator already holds, whatever
	// the agent reports in its init message.
	registerID string

	// resumeID asks the agent CLI to reconstitute this session from its own
	// on-disk session store ("--resume <id>"). Only set when the backend is
	// configured for native resume.
	resumeID string

	// priming replaces the spawn handshake message. A replay resume puts the
	// rendered transcript here, so restoring the conversation costs the one
	// round trip the handshake already spends rather than an extra turn.
	priming string
}

// defaultHandshakeContent is the throwaway first message that makes the agent
// CLI emit its stream-json init record. The protocol only produces it after
// receiving input, so a spawn always pays for one round trip.
const defaultHandshakeContent = "Initialize session. Respond with OK."

// handshakeContent returns the message that opens the subprocess: the replay
// priming text for a replay resume, otherwise the throwaway handshake. A
// replay therefore restores the conversation inside the round trip the spawn
// already spends, instead of burning an extra turn.
func (p spawnPlan) handshakeContent() string {
	if p.priming != "" {
		return p.priming
	}
	return defaultHandshakeContent
}

// encodeStreamJSONUserMessage renders one newline-terminated stream-json user
// message, the input format the persistent CLI reads on stdin.
func encodeStreamJSONUserMessage(content string) ([]byte, error) {
	payload, err := json.Marshal(streamJSONUserMessage{
		Type: "user",
		Message: streamJSONUserContent{
			Role:    "user",
			Content: content,
		},
	})
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

// sessionPersistenceFlag is the agent CLI switch that disables its on-disk
// session store. The backend owns it: it is a resume-policy decision
// (NativeSessionResume), not part of the caller's command line, so any
// caller-supplied copy is dropped and the flag is re-applied from policy.
const sessionPersistenceFlag = "--no-session-persistence"

// applySessionPersistenceFlag returns args with the session-persistence policy
// applied: the flag is stripped wherever a caller placed it, then appended
// again unless the backend is configured to resume natively.
func applySessionPersistenceFlag(args []string, nativeResume bool) []string {
	out := args[:0:0]
	for _, arg := range args {
		if arg == sessionPersistenceFlag {
			continue
		}
		out = append(out, arg)
	}
	if !nativeResume {
		out = append(out, sessionPersistenceFlag)
	}
	return out
}

// spawnCLI starts a persistent Claude Code subprocess using the stream-json
// protocol. It reads the init message from stdout to obtain the session ID,
// then stores the process and its pipes in the session for subsequent prompts.
//
// The plan selects between a plain spawn and a resume; see spawnPlan.
func (b *ACPBackend) spawnCLI(ctx context.Context, req *SpawnRequest, plan spawnPlan) (*Session, error) {
	args := make([]string, len(b.config.Args))
	copy(args, b.config.Args)

	// Append extra args from the spawn request (e.g., --dangerously-skip-permissions).
	args = append(args, req.ExtraArgs...)

	args = applySessionPersistenceFlag(args, b.config.NativeSessionResume)

	// A native resume hands the conversation back to the agent's own session
	// store; nothing is re-sent by us.
	if plan.resumeID != "" {
		args = append(args, "--resume", plan.resumeID)
	}

	if b.config.TeamMode {
		args = append(args, "--team")
	}
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	model := req.Model
	if model == "" {
		model = b.config.DefaultModel
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	var env []string
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}

	// Decouple the subprocess lifetime from the spawning request. The Spawn RPC
	// context is cancelled by gRPC the moment the Spawn handler returns; binding
	// the persistent process to it via exec.CommandContext would deliver SIGINT
	// (through the factory's cmd.Cancel hook) and kill claude immediately after
	// Spawn — the follow-up Prompt would then write to a dead stdin and get a
	// "broken pipe". context.WithoutCancel preserves request-scoped values
	// (tenant, trace, etc.) while detaching cancellation; procCancel is stored on
	// the session and fired only by Close. The init read below still honours the
	// caller's ctx for spawn-time timeout/cancel.
	procCtx, procCancel := context.WithCancel(context.WithoutCancel(ctx))

	cmd, err := b.processFactory(procCtx, b.config.Command, args, req.WorkDir, env)
	if err != nil {
		procCancel()
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	// Capture a bounded tail of the subprocess stderr so an unexpected exit
	// surfaces the agent's own error message instead of a bare broken pipe.
	stderr := &stderrTail{}
	cmd.Stderr = stderr

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		procCancel()
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		procCancel()
		_ = stdinPipe.Close()
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	if err := cmd.Start(); err != nil {
		procCancel()
		_ = stdinPipe.Close()
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, streamJSONScannerBufferSize), streamJSONScannerBufferSize)

	// Claude Code's stream-json mode emits the init message only after
	// receiving the first user input. Send a handshake message to trigger it.

	// reap waits for the subprocess exactly once, no matter how many goroutines
	// race to it (the init/drain reader on EOF, or teardown on a spawn failure).
	// cmd.Wait is not safe to call concurrently, so it is guarded by a Once.
	var waitOnce sync.Once
	reap := func() {
		waitOnce.Do(func() { _ = cmd.Wait() })
	}

	// teardown aborts a partially-spawned persistent subprocess: it closes
	// stdin, cancels the process context (which SIGINTs the process through the
	// factory's Cancel hook), and reaps it. Centralising it keeps every spawn
	// failure path consistent and leak-free.
	teardown := func() {
		_ = stdinPipe.Close()
		procCancel()
		reap()
	}

	handshake, handshakeErr := encodeStreamJSONUserMessage(plan.handshakeContent())
	if handshakeErr != nil {
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: handshakeErr}
	}
	if _, writeErr := stdinPipe.Write(handshake); writeErr != nil {
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: writeErr}
	}

	// Read the init message to obtain session_id and model.
	// Claude Code can take 30+ seconds to initialize (gopls, plugins, etc.),
	// so we read with a timeout to avoid blocking indefinitely.
	type initResult struct {
		msg streamJSONMessage
		err error
	}
	initCh := make(chan initResult, 1)
	go func() {
		for scanner.Scan() {
			var msg streamJSONMessage
			if jsonErr := json.Unmarshal(scanner.Bytes(), &msg); jsonErr != nil {
				continue
			}
			if msg.Type == "system" && msg.Subtype == "init" {
				initCh <- initResult{msg: msg}
				return
			}
		}
		// Scanner exhausted before the init message: the subprocess exited
		// during startup. Reap it so its exit code is known, then build an
		// actionable error carrying the exit code and stderr tail.
		scanErr := scanner.Err()
		if scanErr == nil {
			reap()
			scanErr = b.processExitedError(cmd, stderr)
		}
		initCh <- initResult{err: scanErr}
	}()

	// Wait for init message with 5-minute timeout (Claude Code startup can be slow).
	initTimeout := 5 * time.Minute
	var initMsg streamJSONMessage
	select {
	case <-ctx.Done():
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
	case <-time.After(initTimeout):
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: &StreamJSONError{
			Command: b.config.Command,
			Result:  "timed out waiting for init message after " + initTimeout.String(),
		}}
	case result := <-initCh:
		if result.err != nil {
			teardown()
			return nil, &SpawnError{Command: b.config.Command, Cause: result.err}
		}
		initMsg = result.msg
	}

	sessionID := initMsg.SessionID
	if sessionID == "" {
		var genErr error
		sessionID, genErr = generateSessionID()
		if genErr != nil {
			teardown()
			return nil, &SpawnError{Command: b.config.Command, Cause: genErr}
		}
	}
	// A resumed session keeps the identifier the operator already holds, so
	// the agent's own reported session ID never leaks into the registry.
	if plan.registerID != "" {
		sessionID = plan.registerID
	}

	if initMsg.Model != "" {
		model = initMsg.Model
	}

	// Drain the handshake response (assistant + result messages) so the
	// scanner is positioned for the first real prompt's response.
	drainCh := make(chan struct{}, 1)
	go func() {
		for scanner.Scan() {
			var msg streamJSONMessage
			if jsonErr := json.Unmarshal(scanner.Bytes(), &msg); jsonErr != nil {
				continue
			}
			if msg.Type == "result" {
				break
			}
		}
		drainCh <- struct{}{}
	}()
	select {
	case <-drainCh:
		// Handshake response consumed
	case <-time.After(2 * time.Minute):
		// Handshake timed out, but session is still usable
	case <-ctx.Done():
		teardown()
		return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
	}

	now := time.Now().Unix()
	sess := &acpSession{
		id:           sessionID,
		cmd:          cmd,
		procCancel:   procCancel,
		stderr:       stderr,
		stdin:        stdinPipe,
		stdout:       scanner,
		projectID:    req.ProjectID,
		model:        model,
		workDir:      req.WorkDir,
		env:          req.Env,
		systemPrompt: req.SystemPrompt,
		extraArgs:    req.ExtraArgs,
		createdAt:    now,
	}
	sess.lastActivity.Store(now)

	b.storeSession(sess)

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	return &Session{
		ID:        sessionID,
		ProjectID: req.ProjectID,
		Backend:   b.Type(),
		Model:     model,
		Status:    SessionStateActive,
		PID:       pid,
		WorkDir:   req.WorkDir,
		CreatedAt: now,
	}, nil
}

// spawnJSONRPC creates a persistent session by starting a subprocess,
// initializing the ACP connection, and creating a session via JSON-RPC.
func (b *ACPBackend) spawnJSONRPC(ctx context.Context, req *SpawnRequest) (*Session, error) {
	var env []string
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}

	// Isolate each ACP session with its own HOME to prevent lock file and
	// database conflicts between concurrent instances (e.g., OpenCode uses
	// ~/.local/state/opencode/locks and ~/.local/share/opencode/opencode.db).
	//
	// When the spawn resolved a pooled account with file-based credentials
	// (see resolveAccount / account_materialize.go), req.AccountHomeDir
	// already names a fresh, per-session directory seeded with that
	// account's files (e.g. ".config/opencode/auth.json") — use it directly
	// instead of a bare isolation directory, so the subprocess reads that
	// account's credential rather than the operator's shared config. Its
	// lifetime is owned by resolveAccount's cleanup, attached in
	// provisionAndSpawn.
	sessionHome := req.AccountHomeDir
	if sessionHome == "" {
		var tmpErr error
		sessionHome, tmpErr = os.MkdirTemp("", "acp-session-*")
		if tmpErr != nil {
			sessionHome = ""
		}
	}
	if sessionHome != "" {
		env = append(env, "HOME="+sessionHome)
	}

	args := make([]string, len(b.config.Args))
	copy(args, b.config.Args)
	args = append(args, req.ExtraArgs...)
	if b.config.TeamMode {
		args = append(args, "--team")
	}
	// Pass the working directory explicitly as --cwd so agents that resolve
	// config relative to their own CWD flag (not process CWD) find it.
	if req.WorkDir != "" {
		args = append(args, "--cwd", req.WorkDir)
	}

	transport, err := b.transportFactory(b.config.Command, args, req.WorkDir, env)
	if err != nil {
		return nil, err
	}

	// Initialize ACP connection
	initParams := &InitializeParams{
		ProtocolVersion: 1,
		ClientInfo: ClientInfo{
			Name:    "go-quicbaas",
			Version: "1.0.0",
		},
	}

	resp, err := transport.Send(ctx, "initialize", initParams)
	if err != nil {
		_ = transport.Close()
		return nil, &TransportError{Operation: "initialize", Cause: err}
	}

	if resp.Error != nil {
		_ = transport.Close()
		return nil, &ProtocolError{Message: resp.Error.Message}
	}

	model := req.Model
	if model == "" {
		model = b.config.DefaultModel
	}

	// Create ACP session. Pass empty mcpServers — QuicBaaS scope configs
	// are not compatible with ACP agent MCP server schemas.
	sessionParams := &SessionNewParams{
		CWD:          req.WorkDir,
		MCPServers:   []MCPServerConfig{},
		Model:        model,
		SystemPrompt: req.SystemPrompt,
	}

	resp, err = transport.Send(ctx, "session/new", sessionParams)
	if err != nil {
		_ = transport.Close()
		return nil, &TransportError{Operation: "session/new", Cause: err}
	}

	if resp.Error != nil {
		_ = transport.Close()
		return nil, &ProtocolError{Message: resp.Error.Message}
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		_ = transport.Close()
		return nil, &ProtocolError{Message: "failed to marshal session result", Cause: err}
	}

	var sessionResult SessionNewResult
	if err := json.Unmarshal(resultBytes, &sessionResult); err != nil {
		_ = transport.Close()
		return nil, &ProtocolError{Message: "failed to parse session result", Cause: err}
	}

	now := time.Now().Unix()
	sess := &acpSession{
		id:        sessionResult.SessionID,
		transport: transport,
		projectID: req.ProjectID,
		model:     model,
		workDir:   req.WorkDir,
		createdAt: now,
	}
	sess.lastActivity.Store(now)

	b.storeSession(sess)

	return &Session{
		ID:        sessionResult.SessionID,
		ProjectID: req.ProjectID,
		Backend:   b.Type(),
		Model:     model,
		Status:    SessionStateActive,
		PID:       transport.PID(),
		WorkDir:   req.WorkDir,
		CreatedAt: now,
	}, nil
}

// Prompt sends a prompt to an ACP session. If SessionID is provided, uses the
// existing session. If empty, creates an ephemeral session, prompts, and closes it.
func (b *ACPBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.Prompt == "" {
		return nil, &InputValidationError{Field: "prompt", Message: "cannot be empty"}
	}

	// If no session ID, create an ephemeral session
	if req.SessionID == "" {
		return b.promptEphemeral(ctx, req)
	}

	val, ok := b.sessions.Load(req.SessionID)
	if !ok {
		b.logLookupMiss("prompt", req.SessionID)
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}

	sess, ok := val.(*acpSession)
	if !ok {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}
	sess.lastActivity.Store(time.Now().Unix())

	// CLI stream-json mode: send via persistent subprocess
	if sess.cmd != nil {
		return b.promptStreamJSON(ctx, sess, req)
	}

	// JSON-RPC mode: send via persistent transport
	if sess.transport != nil {
		return b.promptJSONRPC(ctx, sess, req)
	}

	// One-shot CLI mode: no persistent subprocess, spawn fresh process per prompt
	if b.config.CLIMode {
		return b.promptOneShotCLI(ctx, sess, req)
	}

	return nil, &SessionNotFoundError{SessionID: req.SessionID}
}

// promptStreamJSON sends a prompt through the persistent Claude Code subprocess
// using the stream-json protocol. It writes a user message to stdin and reads
// response lines from stdout until a result message is received.
func (b *ACPBackend) promptStreamJSON(ctx context.Context, sess *acpSession, req *PromptRequest) (*PromptResponse, error) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.closed.Load() {
		return nil, &PromptError{SessionID: sess.id, Cause: &SessionClosedError{SessionID: sess.id}}
	}

	// Build the user message
	userMsg := streamJSONUserMessage{
		Type: "user",
		Message: streamJSONUserContent{
			Role:    "user",
			Content: req.Prompt,
		},
	}

	msgBytes, err := json.Marshal(userMsg)
	if err != nil {
		return nil, &PromptError{SessionID: sess.id, Cause: &TransportError{Operation: "marshal", Cause: err}}
	}
	msgBytes = append(msgBytes, '\n')

	if _, writeErr := sess.stdin.Write(msgBytes); writeErr != nil {
		// A write failure (typically "broken pipe") means the persistent
		// subprocess is gone. Surface why it exited — its code and stderr tail —
		// instead of the opaque pipe error, and mark the session closed so the
		// UI shows a real failure reason and follow-ups fail fast.
		return nil, &PromptError{SessionID: sess.id, Cause: b.subprocessGoneError(sess)}
	}

	// Read response lines until we get a "result" type message.
	var contentBlocks []ContentBlock
	var model string
	var stopReason string

	// assistantUsage accumulates the per-message token counts the CLI publishes
	// on each assistant message. It is the fallback for turns whose terminal
	// result message carries no aggregate; see collectStreamJSONUsage.
	assistantUsage := &TokenUsage{}

	// Use a channel to allow context cancellation during reads.
	type scanResult struct {
		msg *streamJSONMessage
		err error
	}
	resultCh := make(chan scanResult, 1)

	for {
		// Read the next line in a goroutine so we can select on ctx.Done().
		go func() {
			if sess.stdout.Scan() {
				var msg streamJSONMessage
				if jsonErr := json.Unmarshal(sess.stdout.Bytes(), &msg); jsonErr != nil {
					resultCh <- scanResult{msg: nil, err: nil} // skip unparseable lines
					return
				}
				resultCh <- scanResult{msg: &msg, err: nil}
				return
			}
			// Scanner exhausted: process exited or pipe closed. Surface the
			// subprocess exit code and stderr tail rather than a bare scan error.
			scanErr := sess.stdout.Err()
			if scanErr == nil {
				scanErr = b.subprocessGoneError(sess)
			}
			resultCh <- scanResult{msg: nil, err: scanErr}
		}()

		select {
		case <-ctx.Done():
			return nil, &PromptError{SessionID: sess.id, Cause: &TransportError{Operation: "prompt", Cause: ctx.Err()}}

		case sr := <-resultCh:
			if sr.err != nil {
				return nil, &PromptError{SessionID: sess.id, Cause: sr.err}
			}
			if sr.msg == nil {
				// Unparseable line, continue reading.
				continue
			}

			// Translate the message into structured transcript events —
			// assistant chunks, chain-of-thought, tool calls/results — so
			// attachers see what the agent is doing as it happens.
			emitStreamJSONEvents(req.EventCallback, sess.id, sr.msg)

			switch sr.msg.Type {
			case "assistant":
				if sr.msg.Message != nil {
					if sr.msg.Message.Model != "" {
						model = sr.msg.Message.Model
					}
					for _, c := range sr.msg.Message.Content {
						if c.Type == "text" {
							contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: c.Text})
						}
					}
					if sr.msg.Message.StopReason != "" {
						stopReason = sr.msg.Message.StopReason
					}
					assistantUsage.Add(sr.msg.Message.Usage)
				}

			case "result":
				if sr.msg.IsError {
					return nil, &PromptError{SessionID: sess.id, Cause: &StreamJSONError{
						Command: b.config.Command,
						Result:  sr.msg.Result,
					}}
				}
				// Use accumulated content blocks, or fall back to the result text.
				if len(contentBlocks) == 0 && sr.msg.Result != "" {
					contentBlocks = []ContentBlock{{Type: "text", Text: sr.msg.Result}}
				}
				if model == "" {
					model = sess.model
				}
				// The stream-json dialect reports stop_reason as null on the
				// assistant message and signals completion with this result
				// message instead, so a finished turn would otherwise carry an
				// empty stop reason. Normalise it to the same value the
				// JSON-RPC and one-shot paths report, so every backend
				// describes a completed turn identically.
				if stopReason == "" {
					stopReason = StopReasonEndTurn
				}
				return &PromptResponse{
					SessionID:  sess.id,
					Content:    contentBlocks,
					Model:      model,
					StopReason: stopReason,
					Usage:      collectStreamJSONUsage(sr.msg.Usage, assistantUsage),
				}, nil
			}
			// Other message types (e.g., tool_use) are skipped.
		}
	}
}

// promptOneShotCLI runs a single prompt by spawning a fresh CLI process.
// No conversation context is maintained between calls.
func (b *ACPBackend) promptOneShotCLI(ctx context.Context, sess *acpSession, req *PromptRequest) (*PromptResponse, error) {
	args := []string{"--print", "--output-format", "json", "--no-session-persistence"}

	if b.config.TeamMode {
		args = append(args, "--team")
	}

	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = sess.systemPrompt
	}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	model := req.Model
	if model == "" {
		model = sess.model
	}
	if model == "" {
		model = b.config.DefaultModel
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// Append extra args from session config (e.g., --dangerously-skip-permissions).
	args = append(args, sess.extraArgs...)

	var env []string
	for k, v := range sess.env {
		env = append(env, k+"="+v)
	}

	cmd, cmdErr := b.processFactory(ctx, b.config.Command, args, sess.workDir, env)
	if cmdErr != nil {
		return nil, &PromptError{SessionID: sess.id, Cause: cmdErr}
	}

	cmd.Stdin = strings.NewReader(req.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = runErr.Error()
		}
		return nil, &PromptError{SessionID: sess.id, Cause: &OneShotExecutionError{
			Command: b.config.Command,
			Stderr:  errMsg,
		}}
	}

	// Parse Claude --output-format json response
	var result struct {
		Type    string      `json:"type"`
		Result  string      `json:"result"`
		IsError bool        `json:"is_error"`
		Usage   *TokenUsage `json:"usage"`
	}
	if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr != nil {
		// Not JSON — treat raw stdout as text response
		resp := &PromptResponse{
			SessionID:  sess.id,
			Content:    []ContentBlock{{Type: "text", Text: stdout.String()}},
			Model:      model,
			StopReason: StopReasonEndTurn,
		}
		emitPromptResponseChunks(req.EventCallback, resp)
		return resp, nil
	}

	if result.IsError {
		return nil, &PromptError{SessionID: sess.id, Cause: &OneShotExecutionError{
			Command: b.config.Command,
			Stderr:  result.Result,
		}}
	}

	resp := &PromptResponse{
		SessionID:  sess.id,
		Content:    []ContentBlock{{Type: "text", Text: result.Result}},
		Model:      model,
		StopReason: StopReasonEndTurn,
		Usage:      collectStreamJSONUsage(result.Usage, nil),
	}
	emitPromptResponseChunks(req.EventCallback, resp)
	return resp, nil
}

// collectStreamJSONUsage reduces a turn's stream-json token accounting to the
// single figure the cost ledger records.
//
// The terminal "result" message carries the CLI's own aggregate for the turn
// and is authoritative when present; the per-assistant-message counts are its
// components, so adding both would bill the turn twice. The accumulated
// per-message total is used only when the result message omits the aggregate,
// which is how the dialect behaves for turns cut short.
//
// A turn that reported no counts at all yields nil, never a zero-valued
// TokenUsage: "not measured" and "measured as zero" must stay distinguishable
// so recordTurnUsage does not write a meaningless ledger entry.
func collectStreamJSONUsage(result, accumulated *TokenUsage) *TokenUsage {
	if !result.Empty() {
		return result
	}
	if !accumulated.Empty() {
		return accumulated
	}
	return nil
}

// promptJSONRPC sends a prompt via the persistent JSON-RPC transport.
// OpenCode ACP sends the response content via session/update notifications
// (agent_thought_chunk, agent_message_chunk) before the final result.
// We accumulate content from notifications and combine with the result.
func (b *ACPBackend) promptJSONRPC(ctx context.Context, sess *acpSession, req *PromptRequest) (*PromptResponse, error) {
	// Set up notification handler to capture streamed content.
	var contentMu sync.Mutex
	var contentParts []string

	sess.transport.SetNotificationHandler(func(msg *JSONRPCMessage) {
		if msg.Method != "session/update" || msg.Params == nil {
			// Non-update notifications are rare; surface them as system
			// messages rather than silently dropping them.
			if req.EventCallback != nil && msg.Params != nil {
				req.EventCallback(&SessionEvent{
					SessionID: sess.id,
					Kind:      KindSystemMessage,
					System: &SystemMessagePayload{
						Level:   "info",
						Code:    msg.Method,
						Message: compactJSON(msg.Params),
					},
				})
			}
			return
		}

		paramsBytes, marshalErr := json.Marshal(msg.Params)
		if marshalErr != nil {
			return
		}
		var params acpSessionUpdateParams
		if unmarshalErr := json.Unmarshal(paramsBytes, &params); unmarshalErr != nil {
			return
		}

		// Accumulate streamed text for the final PromptResponse.
		if text := params.Update.contentText(); text != "" {
			contentMu.Lock()
			contentParts = append(contentParts, text)
			contentMu.Unlock()
		}

		// Translate the update into its structured transcript event.
		emitACPSessionUpdate(req.EventCallback, sess.id, &params.Update)
	})
	defer sess.transport.SetNotificationHandler(nil)

	promptParams := &SessionPromptParams{
		SessionID: sess.promptSessionID(),
		Prompt:    []PromptContent{{Type: "text", Text: req.Prompt}},
	}

	resp, err := sess.transport.Send(ctx, "session/prompt", promptParams)
	if err != nil {
		return nil, &PromptError{SessionID: req.SessionID, Cause: err}
	}

	if resp.Error != nil {
		return nil, &PromptError{SessionID: req.SessionID, Cause: &ProtocolError{Message: resp.Error.Message}}
	}

	// Build response from accumulated notification content.
	contentMu.Lock()
	fullContent := strings.Join(contentParts, "")
	contentMu.Unlock()

	if fullContent != "" {
		return &PromptResponse{
			SessionID:  sess.id,
			Content:    []ContentBlock{{Type: "text", Text: fullContent}},
			Model:      sess.model,
			StopReason: StopReasonEndTurn,
		}, nil
	}

	// Fallback to parsing the result directly (for agents that include content
	// in the result). Nothing was streamed for such a turn, so emit the parsed
	// content as assistant chunks to keep the transcript complete.
	parsed, parseErr := b.parsePromptResult(resp, req.SessionID)
	if parseErr != nil {
		return nil, parseErr
	}
	emitPromptResponseChunks(req.EventCallback, parsed)
	return parsed, nil
}

// promptEphemeral creates a temporary session, sends the prompt, and closes it.
func (b *ACPBackend) promptEphemeral(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	spawnReq := &SpawnRequest{
		ProjectID:    req.ProjectID,
		WorkDir:      "/tmp",
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
	}

	sess, err := b.Spawn(ctx, spawnReq)
	if err != nil {
		return nil, err
	}
	defer b.Close(ctx, sess.ID)

	promptReq := &PromptRequest{
		SessionID: sess.ID,
		Prompt:    req.Prompt,
	}

	return b.Prompt(ctx, promptReq)
}

// parsePromptResult extracts a PromptResponse from a JSON-RPC response.
func (b *ACPBackend) parsePromptResult(resp *JSONRPCMessage, sessionID string) (*PromptResponse, error) {
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, &ProtocolError{Message: "failed to marshal prompt result", Cause: err}
	}

	var promptResult SessionPromptResult
	if err := json.Unmarshal(resultBytes, &promptResult); err != nil {
		return nil, &ProtocolError{Message: "failed to parse prompt result", Cause: err}
	}

	return &PromptResponse{
		SessionID:  sessionID,
		Content:    promptResult.Content,
		Model:      promptResult.Model,
		StopReason: promptResult.StopReason,
	}, nil
}

// Status returns health information for an active ACP session.
func (b *ACPBackend) Status(ctx context.Context, sessionID string) (*SessionStatus, error) {
	val, ok := b.sessions.Load(sessionID)
	if !ok {
		b.logLookupMiss("status", sessionID)
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}

	sess, ok := val.(*acpSession)
	if !ok {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	now := time.Now().Unix()

	// Determine session state based on mode.
	state := SessionStateIdle
	if sess.closed.Load() {
		state = SessionStateClosed
	} else if sess.cmd != nil {
		// CLI stream-json mode: check if the subprocess is still running.
		if sess.cmd.ProcessState != nil && sess.cmd.ProcessState.Exited() {
			state = SessionStateClosed
		} else {
			state = SessionStateActive
		}
	} else if sess.transport != nil && sess.transport.closed.Load() {
		state = SessionStateClosed
	}

	return &SessionStatus{
		SessionID:    sessionID,
		State:        state,
		LastActivity: sess.lastActivity.Load(),
		Uptime:       now - sess.createdAt,
	}, nil
}

// Close terminates an ACP session. For CLI stream-json sessions, it closes stdin
// and signals the subprocess. For JSON-RPC sessions, it sends a session/close
// notification and shuts down the transport.
func (b *ACPBackend) Close(ctx context.Context, sessionID string) error {
	val, ok := b.sessions.LoadAndDelete(sessionID)
	if !ok {
		b.logLookupMiss("close", sessionID)
		return &SessionNotFoundError{SessionID: sessionID}
	}

	sess, ok := val.(*acpSession)
	if !ok {
		return &SessionNotFoundError{SessionID: sessionID}
	}

	sess.closed.Store(true)

	// Release any per-session resources provisioned at spawn (the ephemeral
	// QuicBaaS MCP agent-cert directory). Read-and-clear under mu so the
	// cleanup runs exactly once even if Close races with a late attach, then
	// defer it so it fires after the transport/subprocess teardown below
	// regardless of which branch returns.
	sess.mu.Lock()
	cleanup := sess.cleanup
	accountCleanup := sess.accountCleanup
	envFileCleanup := sess.envFileCleanup
	sess.cleanup = nil
	sess.accountCleanup = nil
	sess.envFileCleanup = nil
	sess.mu.Unlock()
	if cleanup != nil {
		defer cleanup()
	}
	if accountCleanup != nil {
		defer accountCleanup()
	}
	if envFileCleanup != nil {
		defer envFileCleanup()
	}

	// CLI stream-json mode: close stdin and terminate subprocess. Cancelling the
	// process context fires the factory's Cancel hook (SIGINT) on the detached
	// process-lifetime context — the only place this persistent process is ever
	// torn down. cmd.WaitDelay (set at creation) bounds the reap.
	if sess.cmd != nil {
		_ = sess.stdin.Close()
		if sess.procCancel != nil {
			sess.procCancel()
		} else if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Signal(os.Interrupt)
		}
		_ = sess.cmd.Wait()
		return nil
	}

	// No transport: nothing to close.
	if sess.transport == nil {
		return nil
	}

	// JSON-RPC mode: best-effort session/close notification, then close transport.
	closeParams := &SessionCloseParams{SessionID: sessionID}
	_ = sess.transport.Notify("session/close", closeParams)

	closeErr := sess.transport.Close()

	// Container-backed JSON-RPC sessions (spawnContainerJSONRPC) carry a
	// procCancel that owns the detached `docker run` lifetime. Fire it so Close
	// tears the container down (and triggers --rm) instead of leaking it until
	// the agent process happens to exit on its own.
	if sess.procCancel != nil {
		sess.procCancel()
	}

	return closeErr
}

// Capabilities returns the capabilities of the ACP backend.
func (b *ACPBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Kind:               BackendKindAgent,
		PersistentSessions: true,
		ToolUse:            true,
		FileAccess:         true,
		Streaming:          false,
		ACP:                true,
		TeamManagement:     b.config.TeamMode,
	}
}

// Type returns the backend type identifier.
func (b *ACPBackend) Type() string {
	return "acp"
}

// ToolGuidance returns backend-specific tool usage instructions.
// OpenCode's tools have strict schema requirements that smaller models
// struggle with (e.g., bash requires a 'description' field, read uses
// 'filePath' not 'path'). Other backends return empty string.
func (b *ACPBackend) ToolGuidance() string {
	base := filepath.Base(b.config.Command)
	if base == "opencode" {
		return "TOOL USAGE RULES (required by this agent):\n" +
			"- The Bash tool requires BOTH 'command' (string) and 'description' (string) fields.\n" +
			"- The Write tool requires 'filePath' (string) and 'content' (string) fields.\n" +
			"- The Read tool requires 'filePath' (string), NOT 'path'.\n"
	}
	return ""
}

// acpModelCatalog maps the command basename to the curated list of supported
// model identifiers. The catalog is authoritative for in-process ACP backends;
// remote daemon registrations may extend it via their own Capabilities reply.
var acpModelCatalog = map[string][]string{
	"claude": {
		"claude-fable-5",
		"claude-opus-5",
		"claude-sonnet-5",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-haiku-4-5",
	},
	"claude-code": {
		"claude-fable-5",
		"claude-opus-5",
		"claude-sonnet-5",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-haiku-4-5",
	},
	"codex": {
		"codex-mini-latest",
		"o4-mini",
		"o3",
	},
	"opencode": {
		"anthropic/claude-opus-4-5",
		"anthropic/claude-sonnet-4-5",
		"anthropic/claude-haiku-4-5",
		"openai/o4-mini",
		"openai/o3",
		"google/gemini-2.5-pro",
		"google/gemini-2.5-flash",
	},
}

// ListModels returns the curated model catalog for this ACP backend based on
// its command basename. Returns an empty slice for unknown commands.
func (b *ACPBackend) ListModels(_ context.Context) ([]string, error) {
	base := filepath.Base(b.config.Command)
	if models, ok := acpModelCatalog[base]; ok {
		return models, nil
	}
	return []string{}, nil
}

// generateSessionID creates a cryptographically random session identifier.
func generateSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ses-" + hex.EncodeToString(buf), nil
}
