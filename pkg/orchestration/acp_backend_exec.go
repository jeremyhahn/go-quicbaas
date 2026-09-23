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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// execKeepAlive is the command a sandboxed exec-transport session runs to hold
// its container open between turns.
//
// A ONE-SHOT CLI HAS NO PROCESS TO BE THE SESSION. The stream-json and JSON-RPC
// transports keep a subprocess alive and the session is that process; an exec
// CLI exits after every turn. So the CONTAINER is the session: it is started
// once, idles here, and each turn is a ContainerRuntime.Exec into it. That is
// what makes round three of a review see the same workspace, the same
// materialized credential and the same installed toolchain as round one.
//
// `tail -f /dev/null` rather than `sleep infinity` because busybox's sleep does
// not accept "infinity" on every version in the image line, and a keep-alive
// that exits turns every later turn into "container is not running".
var execKeepAlive = []string{"tail", "-f", "/dev/null"}

// spawnContainerExec starts the container an exec-transport session runs its
// turns in and records it. Nothing is prompted here.
func (b *ACPBackend) spawnContainerExec(ctx context.Context, req *SpawnRequest) (*Session, error) {
	image := req.ContainerImage
	if image == "" {
		return nil, &InputValidationError{Field: "container_image", Message: "required for sandboxed sessions"}
	}

	model := req.Model
	if model == "" {
		model = b.config.DefaultModel
	}

	spec, specErr := b.sandboxSpec(req, image, execKeepAlive)
	if specErr != nil {
		return nil, specErr
	}
	// AutoRemove would delete the container the moment the keep-alive is
	// killed, which is what Close does — but Close also wants to Remove it by
	// id, and a --rm container that has already gone makes that a spurious
	// error. Own the lifetime explicitly instead.
	spec.AutoRemove = false

	sessionID, idErr := generateSessionID()
	if idErr != nil {
		return nil, &SpawnError{Command: b.config.Command, Cause: idErr}
	}

	// The container outlives this call, so it must not be bound to the Spawn
	// RPC's context — the same reason spawnContainerCLI detaches. Run itself is
	// short (it returns once the container is started), so the caller's ctx
	// still bounds the start.
	containerID, runErr := b.containerRuntime.Run(ctx, spec)
	if runErr != nil {
		return nil, &SpawnError{Command: image, Cause: runErr}
	}

	now := time.Now()
	sess := &acpSession{
		id:             sessionID,
		tenantID:       StorageTenant(ctx),
		projectID:      req.ProjectID,
		model:          model,
		workDir:        req.WorkDir,
		env:            req.Env,
		systemPrompt:   req.SystemPrompt,
		extraArgs:      req.ExtraArgs,
		createdAt:      now.UnixNano(),
		containerID:    containerID,
		containerImage: image,
	}
	sess.lastActivity.Store(now.UnixNano())
	b.storeSession(sess)

	return &Session{
		ID:          sessionID,
		ProjectID:   req.ProjectID,
		Backend:     b.Type(),
		Model:       model,
		Status:      SessionStateIdle,
		WorkDir:     req.WorkDir,
		ContainerID: containerID,
		CreatedAt:   now.UnixNano(),
	}, nil
}

// promptExec runs one turn as a single non-interactive command.
//
// The prompt goes on stdin, never in argv: a worker charter is the whole task
// and argv is world-readable in /proc.
func (b *ACPBackend) promptExec(ctx context.Context, sess *acpSession, req *PromptRequest) (*PromptResponse, error) {
	model := req.Model
	if model == "" {
		model = sess.model
	}
	if model == "" {
		model = b.config.DefaultModel
	}

	// A LATER TURN CONTINUES THE FIRST ONE'S CONVERSATION. The session's
	// system prompt is already in that conversation, so it is not sent again;
	// a request that names its own still carries it.
	args := b.config.ExecTurnArgs(model, sess.extraArgs)
	input := execTurnInput(req.SystemPrompt, sess.systemPrompt, req.Prompt)
	if thread := sess.execThreadID.Load(); thread != nil && b.config.canResumeExec() {
		args = b.config.ExecResumeTurnArgs(model, sess.extraArgs, *thread)
		input = execTurnInput(req.SystemPrompt, "", req.Prompt)
	}

	var stdout, stderr string
	var exitCode int

	if sess.containerID != "" {
		result, execErr := b.containerRuntime.Exec(ctx, sess.containerID, &ExecSpec{
			Cmd:     append([]string{filepath.Base(b.config.Command)}, args...),
			Env:     sess.env,
			WorkDir: containerWorkDir,
			Stdin:   []byte(input),
		})
		if execErr != nil {
			return nil, &PromptError{SessionID: sess.id, Cause: execErr}
		}
		stdout, stderr, exitCode = result.Stdout, result.Stderr, result.ExitCode
	} else {
		env := make([]string, 0, len(sess.env))
		for k, v := range sess.env {
			env = append(env, k+"="+v)
		}
		cmd, cmdErr := b.processFactory(ctx, b.config.Command, args, sess.workDir, env)
		if cmdErr != nil {
			return nil, &PromptError{SessionID: sess.id, Cause: cmdErr}
		}
		var out, errBuf bytes.Buffer
		cmd.Stdin = strings.NewReader(input)
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		runErr := cmd.Run()
		stdout, stderr = out.String(), errBuf.String()
		if runErr != nil {
			// A non-zero exit is reported through the same path the container
			// form takes, so the two forms fail identically.
			exitCode = 1
		}
	}

	turn, parseErr := parseExecEvents(stdout)
	if parseErr != nil {
		return nil, &PromptError{SessionID: sess.id, Cause: parseErr}
	}

	// EXIT STATUS IS NOT THE VERDICT ON ITS OWN. An exec CLI can exit non-zero
	// having produced a complete turn (a tool it ran failed), and can exit zero
	// having produced nothing. So a turn with text is a turn; a turn without
	// text is a failure whichever way the process exited, and the exit status
	// and stderr are the evidence attached to it.
	if turn.text == "" {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		// DIAGNOSTIC: the evidence is withheld from Error() on purpose (it can
		// carry credential material), which leaves an operator reading only a
		// byte count. Logged at WARN behind the optional logger so a failing
		// CLI can actually be diagnosed on a running node; truncated, because
		// the point is the first error line, not the transcript.
		if b.logger != nil {
			b.logger.Warnf("acp: %s produced no turn text; exit=%d evidence[:2000]=%s",
				b.config.Command, exitCode, truncateForLog(detail, 2000))
		}
		return nil, &PromptError{SessionID: sess.id, Cause: &OneShotExecutionError{
			Command: b.config.Command,
			Stderr:  detail,
		}}
	}
	_ = exitCode

	if turn.threadID != "" {
		thread := turn.threadID
		sess.execThreadID.Store(&thread)
	}
	sess.lastActivity.Store(time.Now().UnixNano())

	return &PromptResponse{
		SessionID:  sess.id,
		Content:    []ContentBlock{{Type: "text", Text: turn.text}},
		Model:      model,
		StopReason: turn.stopReason,
		Usage:      turn.usage,
	}, nil
}

// execTurnInput composes what the CLI reads on stdin.
//
// An exec CLI has no system-prompt flag — codex exec takes instructions and
// nothing else — so the system prompt is prepended with a boundary the model
// can see. The request's own system prompt wins over the session's, matching
// every other transport.
func execTurnInput(reqSystem, sessSystem, prompt string) string {
	system := reqSystem
	if system == "" {
		system = sessSystem
	}
	if strings.TrimSpace(system) == "" {
		return prompt
	}
	var b strings.Builder
	b.WriteString("=== SYSTEM INSTRUCTIONS ===\n")
	b.WriteString(system)
	b.WriteString("\n=== END SYSTEM INSTRUCTIONS ===\n\n")
	b.WriteString(prompt)
	return b.String()
}

// execTurn is what one exec turn's event stream said.
type execTurn struct {
	text       string
	stopReason string
	usage      *TokenUsage

	// threadID is the conversation the turn ran in, from `thread.started`.
	// Empty when the stream reported none, or reported one that is not a
	// plain identifier (see validExecThreadID).
	threadID string
}

// execEvent is the subset of the JSONL event stream this reads.
//
// MEASURED, NOT ASSUMED. Captured from codex-cli 0.154.0 with
// `codex exec --json`:
//
//	{"type":"thread.started","thread_id":"…"}
//	{"type":"turn.started"}
//	{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}
//	{"type":"turn.completed","usage":{"input_tokens":15233,"cached_input_tokens":12160,
//	                                 "cache_write_input_tokens":0,"output_tokens":5,
//	                                 "reasoning_output_tokens":0}}
//
// Unknown event types are skipped rather than refused: a CLI that adds an event
// must not turn a working turn into a parse failure.
type execEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id,omitempty"`
	Item     *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item,omitempty"`
	Usage *struct {
		InputTokens           int64 `json:"input_tokens"`
		CachedInputTokens     int64 `json:"cached_input_tokens"`
		CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
		OutputTokens          int64 `json:"output_tokens"`
		ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	} `json:"usage,omitempty"`
	Error string `json:"error,omitempty"`
}

// parseExecEvents reduces a JSONL event stream to the turn it describes.
//
// The LAST agent message is the answer: a turn that revises itself emits more
// than one, and the final one is what the agent stood behind.
func parseExecEvents(stdout string) (execTurn, error) {
	var turn execTurn
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	// An event carrying a whole file diff comfortably exceeds the default
	// 64 KiB token.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] != '{' {
			// Not every line is an event — a CLI may print a banner or a
			// warning. Prose is not a parse failure.
			continue
		}
		var ev execEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "thread.started":
			if validExecThreadID(ev.ThreadID) {
				turn.threadID = ev.ThreadID
			}
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" && ev.Item.Text != "" {
				turn.text = ev.Item.Text
			}
		case "turn.completed":
			turn.stopReason = "end_turn"
			if ev.Usage != nil {
				turn.usage = codexExecUsage(ev.Usage.InputTokens, ev.Usage.CachedInputTokens,
					ev.Usage.CacheWriteInputTokens, ev.Usage.OutputTokens)
			}
		case "turn.failed", "error":
			turn.stopReason = "error"
		}
	}
	if err := scanner.Err(); err != nil {
		return turn, &ExecEventStreamError{Cause: err}
	}
	return turn, nil
}

// validExecThreadID reports whether id is safe to put on a later turn's argv.
//
// THE ID COMES FROM THE AGENT'S OWN OUTPUT and goes back on a command line, so
// only a plain identifier is accepted: letters, digits, '-' and '_', not
// starting with '-' so it can never be read as a flag. codex reports a UUID.
func validExecThreadID(id string) bool {
	if id == "" || len(id) > 128 || id[0] == '-' {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// codexExecUsage maps codex's turn accounting onto TokenUsage.
//
// CODEX'S input_tokens INCLUDES ITS CACHED TOKENS. It follows the OpenAI
// accounting model, where cached_input_tokens is the part of input_tokens
// served from the prompt cache (codex itself reports the fresh prompt as
// input − cached). TokenUsage.InputTokens is the FRESH prompt, so the cached
// part is subtracted here; carrying both whole counted every cached token
// twice and priced a cache hit as a full prompt token (go-quicbaas#1058).
// reasoning_output_tokens is likewise a part of output_tokens and is not added.
//
// A cached count larger than the input is a producer defect; the fresh prompt
// is clamped at zero and the record marked repaired, like any other count
// that could not be carried faithfully.
func codexExecUsage(input, cached, cacheWrite, output int64) *TokenUsage {
	repaired := false
	input = nonNegative(input, &repaired)
	cached = nonNegative(cached, &repaired)
	fresh := input - cached
	if fresh < 0 {
		fresh = 0
		repaired = true
	}
	usage := &TokenUsage{
		InputTokens:              fresh,
		OutputTokens:             nonNegative(output, &repaired),
		CacheReadInputTokens:     cached,
		CacheCreationInputTokens: nonNegative(cacheWrite, &repaired),
	}
	usage.malformed = repaired
	return usage
}

// ExecEventStreamError is a JSONL event stream that could not be read to its
// end. It is distinct from a turn that produced no text: this one means the
// stream itself was truncated or unreadable, so nothing can be concluded about
// whether the turn happened.
type ExecEventStreamError struct {
	Cause error
}

func (e *ExecEventStreamError) Error() string {
	return "orchestration: exec event stream could not be read: " + e.Cause.Error()
}

func (e *ExecEventStreamError) Unwrap() error { return e.Cause }

// truncateForLog bounds a diagnostic string so one failing CLI cannot fill the
// log with a whole transcript.
func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(" + strconv.Itoa(len(s)-max) + " more bytes)"
}
