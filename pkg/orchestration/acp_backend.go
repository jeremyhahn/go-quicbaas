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
)

// streamJSONScannerBufferSize is the maximum buffer size for the bufio.Scanner
// used to read stream-json output from Claude Code. Claude responses can be
// very large, so we use a 4MB buffer to avoid truncation.
const streamJSONScannerBufferSize = 4 * 1024 * 1024

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
	containerRuntime ContainerRuntime // optional: for sandboxed agent execution
}

// SetContainerRuntime configures the container runtime for sandboxed sessions.
func (b *ACPBackend) SetContainerRuntime(rt ContainerRuntime) {
	b.containerRuntime = rt
}

// acpSession tracks an active ACP session and its configuration.
type acpSession struct {
	id           string
	transport    *StdioTransport // non-nil for JSON-RPC mode sessions
	cmd          *exec.Cmd       // non-nil for CLI stream-json mode sessions
	stdin        io.WriteCloser  // stream-json stdin pipe
	stdout       *bufio.Scanner  // stream-json stdout reader
	mu           sync.Mutex      // protects stdin/stdout for serial access
	projectID    string
	model        string
	workDir      string
	env          map[string]string
	systemPrompt string
	extraArgs    []string // additional CLI args for the agent command
	createdAt    int64
	lastActivity atomic.Int64
	closed       atomic.Bool
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
}

// streamJSONAssistant represents the assistant message body in stream-json output.
type streamJSONAssistant struct {
	Role       string              `json:"role"`
	Content    []streamJSONContent `json:"content,omitempty"`
	StopReason string              `json:"stop_reason,omitempty"`
	Model      string              `json:"model,omitempty"`
}

// streamJSONContent represents a content block within an assistant message.
type streamJSONContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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

	// Route to the correct spawn path based on the request and backend config.
	// CLI mode (stream-json) is used when the backend supports it AND the
	// request explicitly asks for a persistent session.
	if b.config.CLIMode && req.Persistent {
		return b.spawnCLI(ctx, req)
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
		return b.spawnContainerJSONRPC(ctx, req)
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
func (b *ACPBackend) spawnContainerJSONRPC(ctx context.Context, req *SpawnRequest) (*Session, error) {
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
		Network:    "host",
		User:       currentUser,
		Labels: map[string]string{
			"quicbaas.agent":   b.Type(),
			"quicbaas.session": "pending",
		},
	}

	// Mount workspace if specified
	if req.WorkDir != "" && req.WorkDir != "." {
		spec.Mounts = []ContainerMount{{
			Source: req.WorkDir,
			Target: "/workspace",
		}}
	}

	// Mount the host's OpenCode config directory so the container has provider
	// configuration and npm packages for custom providers like Ollama.
	if home, _ := os.UserHomeDir(); home != "" {
		configDir := home + "/.config/opencode"
		if _, statErr := os.Stat(configDir); statErr == nil {
			spec.Mounts = append(spec.Mounts, ContainerMount{
				Source:   configDir,
				Target:   "/home/agent/.config/opencode",
				ReadOnly: true,
			})
		}
	}

	proc, err := b.containerRuntime.RunInteractive(ctx, spec)
	if err != nil {
		return nil, &SpawnError{Command: image, Cause: err}
	}

	// Create transport from container pipes
	transport := newStdioTransportFromPipes(proc.Stdin, proc.Stdout)

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
		_ = transport.Close()
		return nil, &TransportError{Operation: "initialize", Cause: initErr}
	}
	if resp.Error != nil {
		_ = transport.Close()
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
		_ = transport.Close()
		return nil, &TransportError{Operation: "session/new", Cause: sessionErr}
	}
	if resp.Error != nil {
		_ = transport.Close()
		return nil, &ProtocolError{Message: resp.Error.Message}
	}

	// Parse session result
	resultBytes, _ := json.Marshal(resp.Result)
	var sessionResult SessionNewResult
	if jsonErr := json.Unmarshal(resultBytes, &sessionResult); jsonErr != nil {
		_ = transport.Close()
		return nil, &ProtocolError{Message: "failed to parse session result", Cause: jsonErr}
	}

	sessionID := sessionResult.SessionID
	if sessionID == "" {
		var genErr error
		sessionID, genErr = generateSessionID()
		if genErr != nil {
			_ = transport.Close()
			return nil, &SpawnError{Command: image, Cause: genErr}
		}
	}

	now := time.Now().UnixNano()
	sess := &acpSession{
		id:           sessionID,
		transport:    transport,
		model:        model,
		workDir:      req.WorkDir,
		projectID:    req.ProjectID,
		env:          req.Env,
		systemPrompt: req.SystemPrompt,
		createdAt:    now,
	}
	sess.lastActivity.Store(now)
	b.sessions.Store(sessionID, sess)

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
	b.sessions.Store(sessionID, sess)

	return &Session{
		ID:        sessionID,
		Backend:   b.Type(),
		Model:     model,
		CreatedAt: now.UnixNano(),
	}, nil
}

// spawnCLI starts a persistent Claude Code subprocess using the stream-json
// protocol. It reads the init message from stdout to obtain the session ID,
// then stores the process and its pipes in the session for subsequent prompts.
func (b *ACPBackend) spawnCLI(ctx context.Context, req *SpawnRequest) (*Session, error) {
	args := make([]string, len(b.config.Args))
	copy(args, b.config.Args)

	// Append extra args from the spawn request (e.g., --dangerously-skip-permissions).
	args = append(args, req.ExtraArgs...)

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

	cmd, err := b.processFactory(ctx, b.config.Command, args, req.WorkDir, env)
	if err != nil {
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		return nil, &SpawnError{Command: b.config.Command, Cause: err}
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, streamJSONScannerBufferSize), streamJSONScannerBufferSize)

	// Claude Code's stream-json mode emits the init message only after
	// receiving the first user input. Send a handshake message to trigger it.
	handshake := `{"type":"user","message":{"role":"user","content":"Initialize session. Respond with OK."}}` + "\n"
	if _, writeErr := stdinPipe.Write([]byte(handshake)); writeErr != nil {
		_ = stdinPipe.Close()
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
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
		scanErr := scanner.Err()
		if scanErr == nil {
			scanErr = &StreamJSONProcessExitedError{Command: b.config.Command}
		}
		initCh <- initResult{err: scanErr}
	}()

	// Wait for init message with 5-minute timeout (Claude Code startup can be slow).
	initTimeout := 5 * time.Minute
	var initMsg streamJSONMessage
	select {
	case <-ctx.Done():
		_ = stdinPipe.Close()
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
		return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
	case <-time.After(initTimeout):
		_ = stdinPipe.Close()
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
		return nil, &SpawnError{Command: b.config.Command, Cause: &StreamJSONError{
			Command: b.config.Command,
			Result:  "timed out waiting for init message after " + initTimeout.String(),
		}}
	case result := <-initCh:
		if result.err != nil {
			_ = stdinPipe.Close()
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
			return nil, &SpawnError{Command: b.config.Command, Cause: result.err}
		}
		initMsg = result.msg
	}

	sessionID := initMsg.SessionID
	if sessionID == "" {
		var genErr error
		sessionID, genErr = generateSessionID()
		if genErr != nil {
			_ = stdinPipe.Close()
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
			return nil, &SpawnError{Command: b.config.Command, Cause: genErr}
		}
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
		_ = stdinPipe.Close()
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
		return nil, &SpawnError{Command: b.config.Command, Cause: ctx.Err()}
	}

	now := time.Now().Unix()
	sess := &acpSession{
		id:           sessionID,
		cmd:          cmd,
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

	b.sessions.Store(sessionID, sess)

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
	sessionHome, tmpErr := os.MkdirTemp("", "acp-session-*")
	if tmpErr == nil {
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

	b.sessions.Store(sessionResult.SessionID, sess)

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
		return nil, &PromptError{SessionID: sess.id, Cause: &TransportError{Operation: "write", Cause: writeErr}}
	}

	// Read response lines until we get a "result" type message.
	var contentBlocks []ContentBlock
	var model string
	var stopReason string

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
			// Scanner exhausted: process exited or pipe closed.
			scanErr := sess.stdout.Err()
			if scanErr == nil {
				scanErr = &StreamJSONProcessExitedError{Command: b.config.Command}
			}
			resultCh <- scanResult{msg: nil, err: scanErr}
		}()

		select {
		case <-ctx.Done():
			return nil, &PromptError{SessionID: sess.id, Cause: &TransportError{Operation: "prompt", Cause: ctx.Err()}}

		case sr := <-resultCh:
			if sr.err != nil {
				return nil, &PromptError{SessionID: sess.id, Cause: &TransportError{Operation: "prompt", Cause: sr.err}}
			}
			if sr.msg == nil {
				// Unparseable line, continue reading.
				continue
			}

			// Forward every event through the callback for the complete audit trail.
			if req.EventCallback != nil {
				raw, _ := json.Marshal(sr.msg)
				req.EventCallback(sess.id, "agent:"+sr.msg.Type, string(raw))
			}

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
				return &PromptResponse{
					SessionID:  sess.id,
					Content:    contentBlocks,
					Model:      model,
					StopReason: stopReason,
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

	if req.EventCallback != nil {
		req.EventCallback(sess.id, "agent:oneshot_response", stdout.String())
	}

	// Parse Claude --output-format json response
	var result struct {
		Type    string `json:"type"`
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr != nil {
		// Not JSON — treat raw stdout as text response
		return &PromptResponse{
			SessionID:  sess.id,
			Content:    []ContentBlock{{Type: "text", Text: stdout.String()}},
			Model:      model,
			StopReason: "end_turn",
		}, nil
	}

	if result.IsError {
		return nil, &PromptError{SessionID: sess.id, Cause: &OneShotExecutionError{
			Command: b.config.Command,
			Stderr:  result.Result,
		}}
	}

	return &PromptResponse{
		SessionID:  sess.id,
		Content:    []ContentBlock{{Type: "text", Text: result.Result}},
		Model:      model,
		StopReason: "end_turn",
	}, nil
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
		// Forward every notification through the event callback.
		if req.EventCallback != nil {
			raw, _ := json.Marshal(msg)
			req.EventCallback(sess.id, "agent:notification:"+msg.Method, string(raw))
		}

		// Accumulate text content from session/update notifications.
		if msg.Method == "session/update" && msg.Params != nil {
			paramsBytes, _ := json.Marshal(msg.Params)
			var update struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					Content       struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"update"`
			}
			if json.Unmarshal(paramsBytes, &update) == nil {
				if update.Update.Content.Text != "" {
					contentMu.Lock()
					contentParts = append(contentParts, update.Update.Content.Text)
					contentMu.Unlock()
				}
			}
		}
	})
	defer sess.transport.SetNotificationHandler(nil)

	promptParams := &SessionPromptParams{
		SessionID: sess.id,
		Prompt:    []PromptContent{{Type: "text", Text: req.Prompt}},
	}

	resp, err := sess.transport.Send(ctx, "session/prompt", promptParams)
	if err != nil {
		return nil, &PromptError{SessionID: req.SessionID, Cause: err}
	}

	if resp.Error != nil {
		return nil, &PromptError{SessionID: req.SessionID, Cause: &ProtocolError{Message: resp.Error.Message}}
	}

	if req.EventCallback != nil {
		raw, _ := json.Marshal(resp.Result)
		req.EventCallback(sess.id, "agent:jsonrpc_result", string(raw))
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
			StopReason: "end_turn",
		}, nil
	}

	// Fallback to parsing the result directly (for agents that include content in the result).
	return b.parsePromptResult(resp, req.SessionID)
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
		return &SessionNotFoundError{SessionID: sessionID}
	}

	sess, ok := val.(*acpSession)
	if !ok {
		return &SessionNotFoundError{SessionID: sessionID}
	}

	sess.closed.Store(true)

	// CLI stream-json mode: close stdin and terminate subprocess.
	if sess.cmd != nil {
		_ = sess.stdin.Close()
		if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Signal(os.Interrupt)
		}
		// Wait with timeout handled by cmd.WaitDelay set at creation.
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

	return sess.transport.Close()
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
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-haiku-4-5",
		"claude-opus-4-7",
		"claude-sonnet-4-7",
		"claude-haiku-4-7",
	},
	"claude-code": {
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-haiku-4-5",
		"claude-opus-4-7",
		"claude-sonnet-4-7",
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
