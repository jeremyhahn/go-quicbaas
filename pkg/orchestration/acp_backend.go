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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	//
	// SUPERSEDED BY Transport, and still honoured. It can only distinguish
	// stream-json from JSON-RPC, so it cannot describe a CLI that speaks
	// neither — see ACPTransport.
	CLIMode bool `json:"cli_mode"`

	// Transport selects how turns are carried. Empty means JSON-RPC, which is
	// what every config predating this field already got. See ACPTransport for
	// why this is a selector rather than a second backend type.
	Transport ACPTransport `json:"transport,omitempty"`

	// ExecArgs is the argv prefix for one ACPTransportExec turn — the
	// subcommand and the flags that do not vary per turn, e.g.
	// {"exec", "--json", "--skip-git-repo-check"} for codex.
	//
	// IT MUST SELECT A NON-INTERACTIVE MODE. A CLI invoked with no subcommand
	// generally opens its interactive interface, reads nothing it understands
	// on a piped stdin, and exits — which arrives as a transport failure and
	// reads as a broken protocol rather than a missing argument. That was
	// exactly the codex reviewer defect measured 2026-09-13.
	ExecArgs []string `json:"exec_args,omitempty"`

	// ExecModelFlag is the flag that names the model on an exec turn, e.g.
	// "--model". Empty leaves the model to the CLI's own configuration.
	ExecModelFlag string `json:"exec_model_flag,omitempty"`

	// ExecStdinArg is the positional argument telling the CLI to read the
	// prompt from stdin, e.g. "-". Empty appends nothing.
	ExecStdinArg string `json:"exec_stdin_arg,omitempty"`

	// CredentialDir describes where a FILE-based pooled credential lives.
	//
	// This was hardcoded to OpenCode's ".config/opencode" on the JSON-RPC
	// container path. It is data now because it is the second thing a new CLI
	// needs and the first thing a reader assumes is already generic.
	CredentialDir CredentialDirSpec `json:"credential_dir,omitempty"`

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

	// ProcessExitObserver, when non-nil, is told when a supervised persistent
	// CLI subprocess terminates — however it terminated, and whether or not
	// anything was waiting for it at the time. The service layer wires it to
	// mark the persisted session record closed, so a session whose agent died
	// while the item was parked stops reading as addressable. Not serialized:
	// a runtime wiring dependency, like MCPPresetResolver above.
	ProcessExitObserver ProcessExitObserver `json:"-"`
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
	containerRuntime ContainerRuntime        // optional: for sandboxed agent execution
	logger           interfaces.Logger       // optional: session-registry diagnostics
	accountResolver  AccountResolver         // optional: pooled provider account selection (see account_resolver.go)
	accountLeases    AccountCredentialLeaser // optional: withdraws a delivery grant a failed spawn already took out

	// sandboxOwner is the daemon every sandbox container this backend
	// launches is stamped as belonging to. See SetSandboxOwner.
	sandboxOwner string

	// sandboxNetworkWarning makes the host-network / invalid-network advisory
	// fire once per backend instead of once per spawn. See sandboxNetwork.
	sandboxNetworkWarning sync.Once

	// streamJSONInit and streamJSONDrain override the stream-json open's two
	// bounds when positive; zero means the package defaults. Set by tests
	// that need a handshake to time out in milliseconds rather than minutes.
	streamJSONInit  time.Duration
	streamJSONDrain time.Duration
}

// SetContainerRuntime configures the container runtime for sandboxed sessions.
func (b *ACPBackend) SetContainerRuntime(rt ContainerRuntime) {
	b.containerRuntime = rt
}

// SetSandboxOwner names the daemon every sandbox container this backend
// launches belongs to. The name is stamped on the container as
// ContainerOwnerLabel, which is what lets that daemon recognise — and only
// that daemon remove — the containers a previous run of it left behind
// (container_sweep.go, go-quicbaas#750).
//
// Unset leaves the label off, and an unlabelled container is one no sweep will
// ever claim. That is the safe direction: a container nobody sweeps is an
// operator's problem, a container the wrong process sweeps is a running
// worker killed under someone else.
func (b *ACPBackend) SetSandboxOwner(owner string) {
	b.sandboxOwner = strings.TrimSpace(owner)
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

// SetAccountCredentialLeaser wires the authority that withdraws a credential
// delivery grant (see account_lease.go).
//
// The backend needs it for exactly ONE case: a spawn whose account resolution
// SUCCEEDED and whose dispatch then failed. No Session is returned, so nothing
// downstream ever learns the lease ID, and a grant that reached no session
// record is one the reaper can never find. Every other withdrawal — the
// ordinary ending, a rotation superseding a grant, a sweep after a restart — is
// the orchestrator's, driven off the durable record.
func (b *ACPBackend) SetAccountCredentialLeaser(l AccountCredentialLeaser) {
	b.accountLeases = l
}

// releaseAccountLease withdraws a delivery grant this backend took out and
// could not hand on.
//
// Best effort BY CONSTRUCTION, not by choice: the spawn has already failed and
// the caller is returning that failure, so there is nothing to report a
// revocation error to. It is not silently lost either — the grant's TTL bounds
// it, and the reaper turns it into a tombstone.
func (b *ACPBackend) releaseAccountLease(ctx context.Context, leaseID string) {
	if b.accountLeases == nil || leaseID == "" {
		return
	}
	if err := b.accountLeases.Revoke(ctx, leaseID); err != nil && b.logger != nil {
		b.logger.Warnf("acp: failed to revoke credential delivery lease %s after a failed spawn: %s",
			leaseID, err.Error())
	}
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
	case sess.streamJSON() && sess.containerID != "":
		return "cli-container"
	case sess.streamJSON():
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
//
// AT DEBUG, BECAUSE A MISS HERE IS THE ORDINARY CASE. The spawn server resolves
// which backend owns a session by ASKING EACH ONE IN TURN until one answers
// (GRPCSpawnServer.Status and .Close), so every backend that does not hold the
// session reports a miss on every call — six of them on a daemon serving seven
// backends, for a lookup that succeeded. At WARN that buried real faults in
// routine traffic: an operator reading agent.out saw a wall of "session miss"
// for every healthy openvino and vllm turn. The miss is only interesting once
// EVERY backend has missed, and the server says so itself by returning
// NotFound.
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
	b.logger.Debugf("acp_backend: %s session miss: lookup_key=%s present_keys=%v",
		op, lookupKey, present)
}

// acpSession tracks an active ACP session and its configuration.
type acpSession struct {
	id string

	// tenantID owns this session, captured from the spawning context. It is
	// what lets the process supervisor name the tenant whose session record an
	// exit belongs to, on a goroutine that has no request context.
	tenantID string

	// agentID is the identifier the AGENT knows this session by, which is not
	// always the identifier QuicBaaS registers it under. A resumed JSON-RPC
	// session keeps the operator's original ID in the registry while the fresh
	// container hands back a brand new ACP session id; sending the registry's
	// ID on session/prompt would then address a session the agent has never
	// heard of. Empty means the two are the same, which is every non-resumed
	// session. Read through promptSessionID.
	agentID string

	transport  *StdioTransport    // non-nil for JSON-RPC mode sessions
	cmd        *exec.Cmd          // non-nil for CLI stream-json mode sessions
	procCancel context.CancelFunc // cancels the persistent subprocess on Close
	stderr     *stderrTail        // bounded capture of the subprocess stderr
	stdin      io.WriteCloser     // stream-json stdin pipe
	reader     *streamJSONReader  // the one reader of stream-json stdout; see streamJSONReader
	mu         sync.Mutex         // protects stdin/reader/pending for serial access

	// pending counts the prompts written to the agent whose terminal result
	// has not been consumed: the handshake still in flight when the open
	// returned, or a turn whose caller gave up on it. A prompt settles them
	// before it writes, so every result is attributed to the prompt that
	// asked for it. Guarded by mu.
	pending      int
	projectID    string
	model        string
	workDir      string
	env          map[string]string
	systemPrompt string
	extraArgs    []string // additional CLI args for the agent command
	createdAt    int64
	lastActivity atomic.Int64
	closed       atomic.Bool

	// reaper waits for cmd exactly once, however many callers race to it — the
	// supervisor, Close, and the mid-flight subprocess-gone path all reach the
	// process through it, because cmd.Wait is not safe to call concurrently.
	// Nil for sessions that own no persistent subprocess. See reapProcess.
	reaper *procReaper

	// exit is what the supervised subprocess terminated with, stored by the
	// supervisor on the far side of that single Wait and nil while the process
	// is still running. It is what makes "the agent is gone" a fact this
	// daemon OBSERVED rather than one it infers from an unpopulated
	// cmd.ProcessState. See acp_process_supervisor.go.
	exit atomic.Pointer[ProcessExit]

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

	// containerID names the container this session's agent runs in, for a
	// sandboxed session the BACKEND launched. It is what lets the engine run
	// a command in that container afterwards (ExecSession) — the delivery's
	// commit and push happen there, not on the daemon. Empty for every
	// session that runs as a host process. Immutable after Spawn, so it needs
	// no lock.
	containerID string

	// containerImage is what that container was launched from. Carried purely
	// so a session that stops answering names the image an operator has to go
	// look at; nothing decides anything on it. Immutable after Spawn.
	containerImage string

	// envFileCleanup removes the private 0600 --env-file the container runtime
	// reads this session's environment from (InteractiveProcess.Cleanup). It is
	// a THIRD, independent slot for the same reason accountCleanup is separate
	// from cleanup: the file belongs to the running `docker run` process, so it
	// must survive an account rotation (which rewrites credential material) and
	// must not be released by a cert-directory teardown. Guarded by mu.
	envFileCleanup func()
}

// streamJSON reports whether this session speaks the agent CLI's stream-json
// protocol over a persistent stdio pair.
//
// THE PIPES ARE THE DISCRIMINATOR, NOT THE *exec.Cmd. Until spawnContainerCLI
// existed, "persistent CLI session" and "this daemon owns a subprocess" were the
// same thing, and every dispatch site asked `sess.cmd != nil`. A containerized
// CLI session speaks exactly the same protocol with no host process at all — the
// `docker run -i` process belongs to the container runtime — so a cmd-based test
// would route it to the one-shot path and it would answer no prompt at all.
func (s *acpSession) streamJSON() bool {
	return s.stdin != nil && s.reader != nil
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

	// TerminalReason and NumTurns are the CLI's own structured account of why
	// its query loop stopped, carried onto the error when a turn fails so the
	// SDLC module can tell a failed attempt at the work from a CLI that never
	// reached the model. See TurnFailureEvidence.
	//
	// `subtype` is deliberately NOT used for this: it reports "success" on a
	// failing result (measured, Claude Code 2.1.251).
	TerminalReason string `json:"terminal_reason,omitempty"`
	NumTurns       int    `json:"num_turns,omitempty"`

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

// agentEnvScrubbed are the environment variables an agent subprocess must not
// inherit from whoever started this daemon, matched as exact names or as
// prefixes.
//
// THE DAEMON'S ENVIRONMENT IS NOT THE AGENT'S. A quicbaasd started from an
// interactive Claude Code session carries that session's identity — CLAUDECODE,
// CLAUDE_CODE_SESSION_ID, a messaging socket, and a CLAUDE_CONFIG_DIR pointing
// at one operator's personal settings — and every agent it spawns inherits the
// lot, becoming a nested child of a conversation it has nothing to do with.
// That is not a cosmetic leak: go-quicbaas#218 measured a worker whose ability
// to write a file was decided entirely by which shell had started the daemon,
// silently, with the failure attributed to the work.
//
// Removed rather than overridden because there is no value to override with:
// "no session" is the absence of the variable, not a different string.
//
// WHAT IS DELIBERATELY NOT HERE is the other half of the rule, and it is the
// half that decides whether the scrub is safe: the line runs between a variable
// that DESCRIBES A SESSION and one that CONFIGURES THIS DAEMON. The first kind
// is somebody's shell leaking into a worker. The second kind is the operator's
// deployment, and taking it away does not isolate the worker, it misconfigures
// it — a daemon deployed against Bedrock whose worker loses CLAUDE_CODE_USE_BEDROCK
// does not fall back to a safe default, it talks to the wrong provider or fails
// to authenticate, which is a worse failure than the one being fixed and a far
// quieter one.
//
// So HOME and PATH stay (the CLI needs both to find its own installation), and
// so does every name in keep, each of which the CLAUDE_CODE_ prefix would
// otherwise catch:
//
//   - CLAUDE_CODE_OAUTH_TOKEN — the provider CREDENTIAL, named as Claude Code's
//     access-token variable by account_provider_registry.go. A pooled account
//     supplies it per spawn and wins over anything inherited; an operator
//     running without the account pool exported it on the daemon on purpose.
//   - CLAUDE_CODE_USE_BEDROCK / CLAUDE_CODE_USE_VERTEX and their
//     CLAUDE_CODE_SKIP_*_AUTH companions — provider SELECTION and the auth mode
//     that goes with it. These describe where this deployment's models come
//     from, which no worker is entitled to decide for itself.
//
// The prefix is kept rather than replaced by an exhaustive denylist because the
// session-identity variables are the ones that GROW: a new CLAUDE_CODE_<session
// thing> appears with each Claude Code release and would silently start leaking
// through a list nobody remembered to extend. Provider configuration is small,
// documented and stable, so it is the half that can be enumerated.
var agentEnvScrubbed = struct {
	names    []string
	prefixes []string
	keep     []string
}{
	names:    []string{"CLAUDECODE", "CLAUDE_PID", "CLAUDE_EFFORT", "CLAUDE_CONFIG_DIR", "CLAUDE_PLUGIN_DATA"},
	prefixes: []string{"CLAUDE_CODE_", "CODEX_COMPANION_"},
	keep: []string{
		"CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CODE_SKIP_BEDROCK_AUTH",
		"CLAUDE_CODE_SKIP_VERTEX_AUTH",
	},
}

// scrubAgentEnv returns environ with the inherited agent-session variables
// removed, preserving the order of everything else.
//
// The caller appends the spawn request's own variables afterwards, so an SDLC module
// that wants a value for one of these names supplies it explicitly and exec's
// last-wins duplicate handling gives it the child. What no caller can do is
// leave one of them at whatever the operator's shell happened to hold.
func scrubAgentEnv(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			// Not a NAME=VALUE pair at all. Kept verbatim: this function
			// removes what it recognises and is not a validator of the
			// environment it was handed.
			kept = append(kept, entry)
			continue
		}
		if scrubbedAgentEnvName(name) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// scrubbedAgentEnvName reports whether a variable name is one of the inherited
// agent-session variables.
func scrubbedAgentEnvName(name string) bool {
	// The exemption is checked first, so a name on the keep list survives a
	// prefix that would otherwise claim it.
	if slices.Contains(agentEnvScrubbed.keep, name) {
		return false
	}
	if slices.Contains(agentEnvScrubbed.names, name) {
		return true
	}
	for _, prefix := range agentEnvScrubbed.prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// agentProcessEnv is the complete environment an agent subprocess runs in: this
// daemon's, scrubbed of the variables that describe whoever started it, with
// the request's own variables appended.
//
// APPENDED LAST because exec resolves duplicate names to the last occurrence,
// so a caller that supplies a value for a name the scrub removed gets it, and a
// caller that supplies nothing gets its absence rather than an operator's.
//
// One function for every path that execs an agent — the CLI process factory,
// the JSON-RPC stdio transport, the codex one-shot — because the contract is
// about the daemon's environment, not about which transport happens to carry
// the session, and a path that quietly kept its own merge would be the one
// exception nobody remembers to check.
func agentProcessEnv(env []string) []string {
	return append(scrubAgentEnv(os.Environ()), env...)
}

// defaultProcessFactory creates an *exec.Cmd for the stream-json CLI mode.
func defaultProcessFactory(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	// ALWAYS SET, EVEN WITH NOTHING TO ADD, because the request's own
	// variables can only ADD to the inherited environment and the fault this
	// fixes is a variable that must be TAKEN AWAY. Leaving cmd.Env nil hands
	// the child os.Environ() verbatim; see scrubAgentEnv.
	cmd.Env = agentProcessEnv(env)
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

// agentGoneError handles a persistent stream-json session whose agent has died
// mid-flight (write broken pipe or scanner EOF). It ends the process, marks the
// session closed so its state reflects the failure, and returns an actionable
// typed error. It is invoked while holding sess.mu, so the reap/close
// transition is serialised.
//
// The error differs by where the agent was running, because what is KNOWABLE
// differs. A host subprocess was reaped by this daemon, so its exit code and
// stderr tail are reported. A containerized agent's process belongs to the
// container runtime and there is nothing here to Wait on, so the container is
// reported instead of a fabricated exit code — see ContainerStreamClosedError.
func (b *ACPBackend) agentGoneError(sess *acpSession) error {
	if sess.procCancel != nil {
		sess.procCancel()
	}
	// The wait error is deliberately dropped HERE and nowhere else: the exit
	// code it would restate is read from ProcessState by processExitedError
	// below, which is the more useful of the two reports. The supervisor keeps
	// the full answer on the recorded exit. A containerized session owns no
	// reaper at all, so this is a no-op for it.
	_ = sess.reapProcess()
	sess.closed.Store(true)
	if sess.containerID != "" {
		return &ContainerStreamClosedError{
			ContainerID: sess.containerID,
			Image:       sess.containerImage,
			Operation:   "prompt",
		}
	}
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
		if account != nil {
			if account.cleanup != nil {
				account.cleanup()
			}
			// The material is gone; the GRANT over it must go too, or the
			// pool would carry a live delivery lease for a session that never
			// existed.
			b.releaseAccountLease(ctx, account.LeaseID)
		}
		return nil, err
	}
	b.attachCleanup(sess.ID, mcpCleanup)
	if account != nil {
		sess.AccountID = account.AccountID
		sess.AccountStrategy = account.Strategy
		sess.CredentialLeaseID = account.LeaseID
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
// spawn path.
//
// THE SANDBOX MODE IS RESOLVED FIRST, before anything else about the request
// or the backend is consulted. Until 2026-09 it was resolved last: the two
// CLI-mode branches below returned before the switch was ever reached, so a
// CLI-mode backend under `--sandbox-policy required` exec'd the agent straight
// onto the daemon host — with the tenant's provider credential in its
// environment — and handed back a session with no container. The operator
// asked for isolation, the resolver agreed, and the backend silently dropped
// it (#741, live-proven in #742). A sandbox that is not applied must be a
// refusal, never a downgrade, so Required now has exactly one shape: a
// container, or an error.
func (b *ACPBackend) dispatchSpawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	// Legacy Sandboxed=true callers are mapped onto a mode by
	// EffectiveSandboxMode(); the orchestrator layer has already resolved
	// SandboxModeDefault against policy before dispatching.
	switch req.EffectiveSandboxMode() {
	case SandboxModeRequired:
		if b.containerRuntime == nil {
			return nil, &SandboxModeViolationError{
				Requested: SandboxModeRequired,
				Policy:    SandboxPolicyRequired,
				Reason:    sandboxReasonNoRuntime,
			}
		}
		if b.config.isStreamJSON() {
			// The agent's stream-json protocol, spoken to the command of a
			// container instead of to a child of the daemon.
			return b.spawnContainerCLI(ctx, req, spawnPlan{})
		}
		if b.config.isExec() {
			// No process to hold the session open, so the CONTAINER is the
			// session and each turn execs into it. See ACPTransportExec.
			return b.spawnContainerExec(ctx, req)
		}
		return b.spawnContainerJSONRPC(ctx, req, spawnPlan{})

	case SandboxModeNone, SandboxModeDefault:
		// CLI mode (stream-json) is used when the backend supports it AND the
		// request explicitly asks for a persistent session.
		if b.config.isStreamJSON() && req.Persistent {
			return b.spawnCLI(ctx, req, spawnPlan{})
		}
		// One-shot CLI mode: store session config without starting a
		// subprocess. Each prompt spawns a fresh process with no conversation
		// context. The exec transport is one-shot by nature, so unsandboxed it
		// takes the same path — the session is a record, and each turn is its
		// own process.
		if b.config.isStreamJSON() || b.config.isExec() {
			return b.spawnOneShotCLI(req)
		}
		return b.spawnJSONRPC(ctx, req)

	default:
		return nil, &InputValidationError{
			Field:   "sandbox_mode",
			Message: "unknown mode value",
		}
	}
}

// containerWorkDir is where a sandboxed session's workspace is mounted and
// where the agent runs. It is the path the sdlc runner's checkout, verification
// and publish steps all address inside the worker container.
const containerWorkDir = "/workspace"

// containerAgentHome is HOME for a sandboxed agent. It is a path every uid can
// write, which matters because the container runs as the DAEMON's uid — an
// arbitrary number as far as the image is concerned — and both agents need a
// writable home for their own state (OpenCode's lock files and database,
// Claude Code's per-run configuration).
const containerAgentHome = "/tmp"

// sandboxSpec builds the ContainerSpec a sandboxed session runs in, given the
// command the container is to execute.
//
// EVERYTHING THAT MAKES A SANDBOX A SANDBOX IS DECIDED HERE, ONCE: the network,
// the user, the workspace mount, the mount-policy-authorized extra mounts, the
// trust dimension, the labels. Both sandboxed spawn paths — the ACP JSON-RPC
// one and the CLI one — go through it, so a change to the isolation cannot
// reach one protocol and miss the other. That is not hypothetical tidiness:
// the CLI path had NO sandbox at all until #741, and a second hand-built spec
// is exactly how it would silently acquire a weaker one.
//
// It mutates req.Env to carry HOME, because req.Env is what the session records
// as its environment and a rotation reseeds the next spawn from that record.
//
// It returns an error when the project's resolved storage tier cannot be
// applied — an unknown tier, or a memory limit too small to hold the tmpfs
// workspace and the build that runs in it. That is a refusal BEFORE the
// container exists, which is the only place it is cheap: the same
// misconfiguration observed at runtime is an OOM kill partway through an
// attempt.
func (b *ACPBackend) sandboxSpec(req *SpawnRequest, image string, command []string) (*ContainerSpec, error) {
	// THE SESSION RUNS AS THE DAEMON'S OWN UID, AND THAT IS NOT A RESTRICTION
	// ON WHAT IT MAY DO. Inside the container the agent has passwordless sudo
	// (deployments/agents/software-factory-worker/Dockerfile), so it installs
	// packages and runs anything it needs to; the container is the boundary,
	// and its tool grant is WorkerFullControlFlag. What the uid buys is two
	// properties root would cost:
	//
	//   - EVERY FILE THE AGENT WRITES IS OWNED BY THE DAEMON. A workspace on
	//     the persistent tier is a host bind mount, and a root-owned tree left
	//     behind there is a tree the daemon cannot clean up or re-clone into.
	//     The ephemeral tier moves the checkout onto a tmpfs and narrows this,
	//     but the tier is per project and the remaining mounts — the workspace
	//     spec directory and the signing agent's socket — are read-only, so no
	//     bind mount can receive an agent's write at all.
	//   - CLAUDE CODE STILL ACCEPTS ITS GRANT. The CLI refuses
	//     --dangerously-skip-permissions when its uid is 0, so a container run
	//     as root to make package installation work would have taken the tool
	//     grant away again — the exact failure #927 exists to fix.
	//
	// The uid must exist in the image's /etc/passwd, and that is not cosmetic:
	// git invokes ssh-keygen for every gpg.format=ssh signature, ssh-keygen
	// calls getpwuid, and without an entry it exits "No user exists for uid"
	// and EVERY signed commit fails. The image asserts this at build time.
	if req.Env == nil {
		req.Env = make(map[string]string, 1)
	}
	req.Env["HOME"] = containerAgentHome

	// The owner label is part of the SPEC rather than something the runtime
	// adds at launch, because ContainerConfigDigest measures spec.Labels: a
	// label injected below the measurement would make the launched container
	// disagree with the configuration that was attested.
	labels := map[string]string{
		"quicbaas.agent":   b.Type(),
		"quicbaas.session": "pending",
	}
	if b.sandboxOwner != "" {
		labels[ContainerOwnerLabel] = b.sandboxOwner
	}

	spec := &ContainerSpec{
		Image:      image,
		Command:    command,
		Entrypoint: []string{}, // Clear entrypoint, use command directly
		Env:        req.Env,
		WorkDir:    containerWorkDir,
		AutoRemove: true,
		Network:    b.sandboxNetwork().String(),
		User:       fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		Labels:     labels,

		// Trust dimension (container_trust.go / trust_policy.go). Every
		// field is inert at its zero value, so a spawn that does not ask
		// for trusted computing builds the same spec it always did.
		ImageDigest:  req.ImageDigest,
		TrustLevel:   req.TrustLevel,
		TrustLabel:   req.TrustLabel,
		ExpectedPCRs: req.ExpectedPCRs,
	}

	// Mount workspace if specified.
	if req.WorkDir != "" && req.WorkDir != WorkDirCurrent {
		spec.Mounts = []ContainerMount{{
			Source: req.WorkDir,
			Target: containerWorkDir,
		}}
	}

	// Extra mounts were authorized against the orchestrator's mount policy
	// before the backend ever saw this request; append them verbatim.
	spec.Mounts = append(spec.Mounts, req.ExtraMounts...)

	// WHICH DIRECTORY IS DATA, NOT A NAME IN THE BACKEND. It was OpenCode's
	// ".config/opencode" hardcoded here, which read as generic and was not:
	// codex's credential is also a file, in ".codex", found through CODEX_HOME,
	// and a second CLI with a file credential had nowhere to say so. See
	// ACPBackendConfig.CredentialDir. A CLI whose credential is an environment
	// variable (ProviderDescriptor.AccessTokenEnvKey) leaves the spec empty and
	// mounts nothing, exactly as before.
	if cred := b.config.CredentialDir; cred.configured() {
		source := ""
		if req.AccountHomeDir != "" {
			source = filepath.Join(req.AccountHomeDir, cred.Rel)
		} else if home, _ := os.UserHomeDir(); home != "" {
			source = filepath.Join(home, cred.Rel)
		}
		mounted := false
		if source != "" {
			if _, statErr := os.Stat(source); statErr == nil {
				spec.Mounts = append(spec.Mounts, ContainerMount{
					Source:   source,
					Target:   cred.Target,
					ReadOnly: !cred.Writable,
				})
				mounted = true
			}
		}
		// A CONFIGURED CREDENTIAL THAT DID NOT MOUNT IS A REFUSAL, NOT A
		// WARNING. This used to set HomeEnv unconditionally, so a backend whose
		// credential directory was absent — a pooled account whose files were
		// stored under the wrong relative path, a materialization that wrote
		// nothing — still told the CLI where to look, and pointed it at a path
		// nothing had mounted. codex answered
		//
		//	CODEX_HOME points to "/home/agent/.codex", but that path does not
		//	exist
		//
		// on stderr, produced no turn, and the runner reported only
		// "(9882 bytes of stderr withheld)". Every review on the burn-in fleet
		// failed that way for hours, on every attempt, while the account read
		// `active` and the credential resolved: the spawn had everything it
		// needed except the one mount, and said so nowhere.
		//
		// Failing here costs the same attempt and names the cause.
		if !mounted {
			return nil, &CredentialDirMissingError{
				Backend: b.config.Command,
				Source:  source,
				Target:  cred.Target,
				Rel:     cred.Rel,
			}
		}
		// A CLI that locates its credential by variable is told where it went.
		// codex reads CODEX_HOME and would otherwise look in $HOME/.codex,
		// which on this spawn is the container's own empty /tmp.
		if cred.HomeEnv != "" {
			if req.Env == nil {
				req.Env = make(map[string]string, 1)
			}
			req.Env[cred.HomeEnv] = cred.Target
			spec.Env = req.Env
		}
	}

	// The project's storage tier is applied LAST, over the assembled spec.
	// THIS IS THE PATH A SOFTWARE-FACTORY WORKER TAKES: the orchestrator's own
	// spawnSandboxedSession is skipped for an ACP backend that holds a
	// container runtime, so a tier wired only there would never reach one. The
	// two paths share ApplyWorkspaceStorage precisely so they cannot diverge.
	if err := ApplyWorkspaceStorage(spec, containerWorkDir, req.Storage); err != nil {
		return nil, err
	}

	return spec, nil
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
	command := []string{filepath.Base(b.config.Command)}
	command = append(command, b.config.Args...)
	command = append(command, req.ExtraArgs...)

	spec, specErr := b.sandboxSpec(req, image, command)
	if specErr != nil {
		return nil, specErr
	}

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
	//

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
		return nil, &PeerRPCError{Code: resp.Error.Code, Message: resp.Error.Message}
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
		return nil, &PeerRPCError{Code: resp.Error.Code, Message: resp.Error.Message}
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
		id:             sessionID,
		agentID:        agentSessionID,
		transport:      transport,
		procCancel:     procCancel,
		model:          model,
		workDir:        req.WorkDir,
		projectID:      req.ProjectID,
		env:            req.Env,
		systemPrompt:   req.SystemPrompt,
		createdAt:      now,
		containerID:    proc.ContainerID,
		containerImage: image,

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
		ID:          sessionID,
		Backend:     b.Type(),
		Model:       model,
		CreatedAt:   now,
		ContainerID: proc.ContainerID,
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
func defaultHandshakeContent() string {
	return workflowPrompts.MustRender("handshake", nil)
}

// handshakeContent returns the message that opens the subprocess: the replay
// priming text for a replay resume, otherwise the throwaway handshake. A
// replay therefore restores the conversation inside the round trip the spawn
// already spends, instead of burning an extra turn.
func (p spawnPlan) handshakeContent() string {
	if p.priming != "" {
		return p.priming
	}
	return defaultHandshakeContent()
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

// spawnCLI starts a persistent Claude Code subprocess ON THE DAEMON HOST using
// the stream-json protocol. It reads the init message from stdout to obtain the
// session ID, then stores the process and its pipes in the session for
// subsequent prompts.
//
// UNSANDBOXED BY CONSTRUCTION. This path runs the agent as a child of the
// daemon, so dispatchSpawn reaches it only for a request whose effective
// sandbox mode is None or Default; a Required request goes to
// spawnContainerCLI, and one that cannot be containerized is refused (#741).
//
// The plan selects between a plain spawn and a resume; see spawnPlan.
func (b *ACPBackend) spawnCLI(ctx context.Context, req *SpawnRequest, plan spawnPlan) (*Session, error) {
	model := req.Model
	if model == "" {
		model = b.config.DefaultModel
	}
	args := b.cliArgs(req, plan, model)

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

	// The reaper waits for the subprocess exactly once, no matter how many
	// goroutines race to it (the init/drain reader on EOF, teardown on a spawn
	// failure, and — once the session exists — its supervisor, Close, and the
	// mid-flight subprocess-gone path). cmd.Wait is not safe to call
	// concurrently, so every one of them goes through this value; it is carried
	// onto the session below so the spawn's half and the session's half of the
	// process's life share one Once rather than each holding their own.
	reaper := &procReaper{cmd: cmd}

	// teardown aborts a partially-spawned persistent subprocess: it closes
	// stdin, cancels the process context (which SIGINTs the process through the
	// factory's Cancel hook), and reaps it. Centralising it keeps every spawn
	// failure path consistent and leak-free.
	teardown := func() {
		_ = stdinPipe.Close()
		procCancel()
		// A teardown reaps to avoid a zombie; the spawn is already failing and
		// is about to return the reason it failed, which a wait error would
		// only displace.
		_ = reaper.reap()
	}

	open, openErr := b.openStreamJSON(ctx, &streamJSONProc{
		stdin:  stdinPipe,
		stdout: stdoutPipe,
		gone: func() error {
			// Reap so the exit code is known, then build an actionable error
			// carrying that code and the stderr tail. Same as teardown: the
			// report processExitedError builds is the answer, not the wait
			// error it would displace.
			_ = reaper.reap()
			return b.processExitedError(cmd, stderr)
		},
	}, plan, model, teardown)
	if openErr != nil {
		return nil, openErr
	}

	now := time.Now().UnixNano()
	sess := &acpSession{
		id:           open.sessionID,
		tenantID:     StorageTenant(ctx),
		cmd:          cmd,
		reaper:       reaper,
		procCancel:   procCancel,
		stderr:       stderr,
		stdin:        stdinPipe,
		reader:       open.reader,
		pending:      open.pending,
		projectID:    req.ProjectID,
		model:        open.model,
		workDir:      req.WorkDir,
		env:          req.Env,
		systemPrompt: req.SystemPrompt,
		extraArgs:    req.ExtraArgs,
		createdAt:    now,
	}
	sess.lastActivity.Store(now)

	b.storeSession(sess)

	// THE PROCESS GETS AN OWNER THAT OUTLIVES ITS SPAWNER, and it gets one the
	// moment the session is addressable. Before this, the only thing waiting on
	// the agent was whichever caller happened to fail next; a session parked on
	// a question had nobody, and a process that died there stayed a zombie the
	// daemon reported as active (#187). See acp_process_supervisor.go.
	b.superviseProcess(sess)

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	return &Session{
		ID:        open.sessionID,
		ProjectID: req.ProjectID,
		Backend:   b.Type(),
		Model:     open.model,
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
		return nil, &PeerRPCError{Code: resp.Error.Code, Message: resp.Error.Message}
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
		return nil, &PeerRPCError{Code: resp.Error.Code, Message: resp.Error.Message}
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

	now := time.Now().UnixNano()
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
	sess.lastActivity.Store(time.Now().UnixNano())

	// CLI stream-json mode: send over the agent's persistent stdio, whether it
	// is a host subprocess or a container.
	if sess.streamJSON() {
		return b.promptStreamJSON(ctx, sess, req)
	}

	// JSON-RPC mode: send via persistent transport
	if sess.transport != nil {
		return b.promptJSONRPC(ctx, sess, req)
	}

	// One-shot CLI mode: no persistent subprocess, spawn fresh process per prompt
	if b.config.isStreamJSON() {
		return b.promptOneShotCLI(ctx, sess, req)
	}

	// Exec transport: one command per turn, into this session's container when
	// it has one, otherwise a fresh host process. Checked AFTER the persistent
	// transports so a session that somehow holds both is driven by the live
	// one rather than by its configuration.
	if b.config.isExec() {
		return b.promptExec(ctx, sess, req)
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

	// WHAT IS OWED IS SETTLED FIRST. A handshake reply still in flight when
	// the open returned, or a turn whose caller gave up before its result,
	// still has a `result` coming down the stream, and it belongs to THAT
	// prompt. Reading past it here is what keeps this prompt's own result
	// from being handed to the wrong turn — or, as measured, to nobody.
	for sess.pending > 0 {
		if _, err := sess.reader.nextResult(ctx, nil); err != nil {
			return nil, &PromptError{SessionID: sess.id, Cause: b.streamJSONReadError(sess, err)}
		}
		sess.pending--
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
		return nil, &PromptError{SessionID: sess.id, Cause: b.agentGoneError(sess)}
	}
	// The prompt is written: its result is owed until this call consumes it.
	// A return on ctx before then leaves the count for the next prompt.
	sess.pending++

	var contentBlocks []ContentBlock
	var model string
	var stopReason string

	// assistantUsage accumulates the per-message token counts the CLI publishes
	// on each assistant message. It is the fallback for turns whose terminal
	// result message carries no aggregate; see collectStreamJSONUsage.
	assistantUsage := &TokenUsage{}

	result, readErr := sess.reader.nextResult(ctx, func(msg *streamJSONMessage) {
		// Translate the message into structured transcript events —
		// assistant chunks, chain-of-thought, tool calls/results — so
		// attachers see what the agent is doing as it happens.
		emitStreamJSONEvents(req.EventCallback, sess.id, msg)
		if msg.Type != "assistant" || msg.Message == nil {
			return
		}
		if msg.Message.Model != "" {
			model = msg.Message.Model
		}
		for _, c := range msg.Message.Content {
			if c.Type == "text" {
				contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: c.Text})
			}
		}
		if msg.Message.StopReason != "" {
			stopReason = msg.Message.StopReason
		}
		assistantUsage.Add(msg.Message.Usage)
	})
	if readErr != nil {
		return nil, &PromptError{SessionID: sess.id, Cause: b.streamJSONReadError(sess, readErr)}
	}
	sess.pending--

	if result.IsError {
		// The evidence is taken from the RESULT message's own aggregate
		// rather than from assistantUsage: the question it answers is
		// whether the CLI ever reached the model at all, and a nil aggregate
		// must stay distinguishable from a zero one (see
		// TurnFailureEvidence.PreFirstTurn).
		return nil, &PromptError{SessionID: sess.id, Cause: &StreamJSONError{
			Command: b.config.Command,
			Result:  result.Result,
			TurnFailureEvidence: TurnFailureEvidence{
				TerminalReason: result.TerminalReason,
				NumTurns:       result.NumTurns,
				Usage:          result.Usage,
			},
		}}
	}
	// Use accumulated content blocks, or fall back to the result text.
	if len(contentBlocks) == 0 && result.Result != "" {
		contentBlocks = []ContentBlock{{Type: "text", Text: result.Result}}
	}
	if model == "" {
		model = sess.model
	}
	// The stream-json dialect reports stop_reason as null on the assistant
	// message and signals completion with this result message instead, so a
	// finished turn would otherwise carry an empty stop reason. Normalise it
	// to the same value the JSON-RPC and one-shot paths report, so every
	// backend describes a completed turn identically.
	if stopReason == "" {
		stopReason = StopReasonEndTurn
	}
	return &PromptResponse{
		SessionID:  sess.id,
		Content:    contentBlocks,
		Model:      model,
		StopReason: stopReason,
		Usage:      collectStreamJSONUsage(result.Usage, assistantUsage),
	}, nil
}

// streamJSONReadError names why a read from the agent's stream failed: the
// caller's ctx, or the stream's end — for which the subprocess exit code and
// stderr tail (or the container) are reported rather than a bare scan error.
func (b *ACPBackend) streamJSONReadError(sess *acpSession, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &TransportError{Operation: "prompt", Cause: err}
	}
	// The stream ended. The reader already knows why when the opener could
	// say; the session is closed and the process reaped either way.
	gone := b.agentGoneError(sess)
	if err != nil {
		return err
	}
	return gone
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

		// Same structured account the persistent path reads; see
		// streamJSONMessage and TurnFailureEvidence. `--print
		// --output-format json` emits the identical result object.
		TerminalReason string `json:"terminal_reason"`
		NumTurns       int    `json:"num_turns"`
	}
	// A RUN THAT WROTE NOTHING DID NOT TAKE A TURN (#241).
	//
	// This is checked before the parse because an empty buffer fails
	// json.Unmarshal ("unexpected end of JSON input") and used to fall through
	// to the raw-text fallback below, which produced a completed turn whose one
	// content block held the empty string — BYTE-FOR-BYTE what a CLI that
	// reported {"result":""} produces. So "the CLI produced nothing" and "the
	// CLI produced an empty result" were the same PromptResponse, and no caller
	// could tell a dead process from an empty answer.
	//
	// The distinction is the one this package already draws for token counts:
	// an ABSENCE is not a measurement of zero. A result object saying the
	// answer was empty is a claim the CLI made; a silent exit is the absence of
	// any claim, and reporting it as a completed turn is the fail-open.
	//
	// This is NOT the open question in #241. That question is what to do with
	// stdout the SDLC module asked to be JSON and could not parse — text, a
	// truncated object — and all three options it lists concern CONTENT. None
	// of them makes no content at all a successful turn, and there is no
	// backend for which a silent exit is a working answer: ParseFinishReport
	// already rejects such a turn downstream, so this only moves the diagnosis
	// to the CLI that produced nothing instead of naming the finish schema.
	if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return nil, &PromptError{SessionID: sess.id, Cause: &OneShotExecutionError{
			Command: b.config.Command,
			Stderr:  stderr.String(),
			Cause:   ErrOneShotNoOutput,
		}}
	}

	if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr != nil {
		// Not JSON — treat raw stdout as text response.
		//
		// THE FALLBACK IS KEPT AND IS NO LONGER SILENT. Whether the one-shot
		// path may accept unparseable stdout as a turn at all is the behaviour
		// question #241 reserves for the owner — a backend whose CLI does not
		// honour --output-format json works today BECAUSE of this branch, and
		// closing it would fail those spawns. What is fixed here is that taking
		// the branch left no trace: a result truncated by a killed process, or
		// one carrying a number outside int64, arrived as an ordinary
		// end_turn whose "answer" was the broken bytes, and nothing anywhere
		// said the SDLC module had given up parsing.
		//
		// THE BYTES ARE COUNTED, NOT LOGGED. Stdout is model output produced in
		// a process holding credential material in its environment, and this
		// package does not render such buffers into log lines — see
		// StreamJSONProcessExitedError.Stderr, which carries the whole argument
		// on its own field. (OneShotExecutionError.Stderr states the same rule
		// but forwards to that one for the reasoning; citing the forwarder is
		// how a rule gets diluted the next time somebody follows the pointer.)
		if b.logger != nil {
			b.logger.Warnf(
				"acp_backend: one-shot result was not parseable JSON; accepted as raw text: command=%s session=%s stdout_bytes=%d err=%s",
				b.config.Command, sess.id, stdout.Len(), jsonErr.Error())
		}
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
			TurnFailureEvidence: TurnFailureEvidence{
				TerminalReason: result.TerminalReason,
				NumTurns:       result.NumTurns,
				Usage:          result.Usage,
			},
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
//
// THE Empty TESTS HERE ARE SOUND ONLY BECAUSE NEGATIVES CANNOT REACH THEM.
// Both arguments were decoded through TokenUsage.UnmarshalJSON, which clamps
// each dimension, so no dimension can cancel another and sum a measured turn to
// zero — which is how a real turn used to be discarded here as unmeasured
// (#242). Nothing in this function detects that on its own; it relies on the
// decode boundary, and a future caller that builds a TokenUsage by hand from
// untrusted numbers must call normalize first.
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
		return nil, &PromptError{SessionID: req.SessionID, Cause: &PeerRPCError{Code: resp.Error.Code, Message: resp.Error.Message}}
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
	// SECONDS OUT, NANOSECONDS IN. createdAt is Unix nanoseconds like every
	// other session instant; Uptime is a duration in SECONDS, which is what
	// the proto field `uptime_seconds` carries and what the REST handler
	// multiplies by time.Second. Subtracting the two directly — a seconds
	// clock less a nanosecond instant — is a negative number the width of
	// the epoch.
	uptime := time.Duration(time.Now().UnixNano() - sess.createdAt)

	state, err := b.sessionState(ctx, sess)
	if err != nil {
		return nil, err
	}

	return &SessionStatus{
		SessionID:    sessionID,
		State:        state,
		LastActivity: sess.lastActivity.Load(),
		Uptime:       int64(uptime.Seconds()),
	}, nil
}

// sessionState decides what one live session record is worth reporting as.
//
// It answers an ERROR rather than a state when the session's liveness cannot
// be established, because every consumer of this answer — the stall detector
// above all — treats "not live" as grounds to end work, and a guess in that
// direction ends work that is still running.
func (b *ACPBackend) sessionState(ctx context.Context, sess *acpSession) (SessionState, error) {
	// The session's own record outranks anything the host can be asked. A
	// concluded session is closed, and a supervisor that observed the process
	// terminate has the authoritative answer already. This is the branch that
	// makes a killed agent report as closed: cmd.ProcessState is populated
	// only by Wait, so a process that died with nobody waiting on it reported
	// ACTIVE for as long as its zombie survived (#187).
	if sess.closed.Load() || sess.exited() != nil {
		return SessionStateClosed, nil
	}

	// A CONTAINERIZED SESSION IS ALIVE ONLY WHILE ITS CONTAINER IS, and the
	// container is the one thing here nothing else observes. There is no
	// *exec.Cmd for the supervisor to Wait on — `docker run -i --rm` belongs
	// to the container runtime (acp_backend_container_cli.go) — and the death
	// of the container reaches this daemon as EOF on a stdio nobody is reading
	// between prompts. So a worker container that died mid-execute went on
	// being reported ACTIVE indefinitely, holding its job's lease and its
	// project's only slot (go-quicbaas#774). The runtime is asked instead.
	if sess.containerID != "" && b.containerRuntime != nil {
		running, err := b.containerRuntime.IsRunning(ctx, sess.containerID)
		if err != nil {
			// A container the runtime no longer knows is gone: `--rm` removed
			// it when it exited. That is an answer, not a failure to get one.
			var missing *ContainerNotFoundError
			if errors.As(err, &missing) {
				return SessionStateClosed, nil
			}
			return "", err
		}
		if !running {
			return SessionStateClosed, nil
		}
	}

	switch {
	case sess.streamJSON():
		// CLI stream-json mode: the agent is running until the supervisor says
		// otherwise (host subprocess) or until its container stops.
		return SessionStateActive, nil
	case sess.transport != nil && sess.transport.closed.Load():
		return SessionStateClosed, nil
	}
	return SessionStateIdle, nil
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
	if sess.streamJSON() {
		_ = sess.stdin.Close()
		if sess.procCancel != nil {
			sess.procCancel()
		} else if sess.cmd != nil && sess.cmd.Process != nil {
			_ = sess.cmd.Process.Signal(os.Interrupt)
		}
		// Through the shared reaper, never cmd.Wait directly: the session's
		// supervisor is already parked in that Wait, and a second one would be
		// the concurrent call os/exec forbids. This blocks until the supervisor's
		// wait returns, which is what Close wants — the process is reaped before
		// Close reports it closed. The wait error is not Close's answer: Close
		// succeeded if the process is gone, however it went, and how it went is
		// on the recorded exit for whoever asks.
		_ = sess.reapProcess()
		return nil
	}

	// Exec transport: the SESSION IS THE CONTAINER, so closing the session is
	// removing it. There is no transport and no subprocess to fall through to,
	// and spawnContainerExec deliberately turns AutoRemove off so the lifetime
	// is owned here rather than by whatever kills the keep-alive first — a
	// container removed from under Close is a container Close cannot report on.
	// Without this branch an exec session reaches the "nothing to close" return
	// below and leaks a running container per review.
	if sess.containerID != "" && b.containerRuntime != nil && b.config.isExec() {
		// Stop first so the keep-alive gets a chance to exit cleanly, then
		// remove — Remove force-removes a container still running, so a Stop
		// that fails is not fatal to the removal.
		_ = b.containerRuntime.Stop(ctx, sess.containerID)
		if err := b.containerRuntime.Remove(ctx, sess.containerID); err != nil {
			return &ContainerStopError{ContainerID: sess.containerID, Cause: err}
		}
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
//
// THIS CATALOG TRACKS software/conventions/model-usage.md IN THE COMPANY REPO,
// which is the source of truth for which models may be used at all. Two rules
// from it bind every entry below, and both are the reason this list is shorter
// than it was:
//
//   - ONLY THE LATEST VERSION OF EACH TIER. No configuration anywhere may pin
//     an older model, so the 4.5 family is gone rather than kept as a
//     compatibility courtesy. An entry that still resolves is exactly how a
//     downgrade happens quietly.
//   - THE FLOOR IS OPUS 5 for substantive work, with Sonnet 5 reserved for
//     trivial mechanical tasks. Haiku is prohibited outright, so listing it
//     here — advertising it as selectable — is the configuration equivalent of
//     permitting it.
//
// A stale entry here is not cosmetic: this list is what a caller picks from,
// so a name that no longer exists surfaces as a provider error nobody
// attributes to configuration, and a name that DOES still exist silently runs
// work below the floor.
//
// Removed rather than guessed: opencode's google/* entries named Gemini 2.5,
// and nothing in model-usage.md establishes a current Gemini version to
// replace them with. Inventing a version string would recreate precisely the
// staleness this change exists to remove. If those models are wanted, the
// version belongs in model-usage.md first and here second.
var acpModelCatalog = map[string][]string{
	"claude": {
		"claude-fable-5",
		"claude-opus-5",
		"claude-sonnet-5",
	},
	"claude-code": {
		"claude-fable-5",
		"claude-opus-5",
		"claude-sonnet-5",
	},
	// Codex runs the latest GPT model. Verified 2026-08-21 against the live
	// pin point model-usage.md names, ~/.codex/config.toml, rather than taken
	// from the document alone.
	"codex": {
		"gpt-5.5",
	},
	"opencode": {
		"anthropic/claude-fable-5",
		"anthropic/claude-opus-5",
		"anthropic/claude-sonnet-5",
		"openai/gpt-5.5",
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
