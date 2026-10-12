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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// OpenVINO — a model served on an accelerator this fleet owns
// =============================================================================
//
// WHY THIS EXISTS RATHER THAN ANOTHER PROVIDER CONFIG ON THE EXEC BACKEND. The
// exec backend drives a vendor CLI (codex) against an OpenAI-compatible
// endpoint, and that path was tried against OpenVINO Model Server first
// precisely because reusing it would have cost nothing. It fails, for a reason
// no configuration reaches:
//
//	Calculator::Process() for node "LLMExecutor" failed:
//	Only function tools are supported
//
// codex advertises its shell and patch tools as CUSTOM tool types, which the
// Responses API allows and OVMS rejects; codex has no switch to send them as
// `function` instead. So a model served by OVMS cannot be driven by that CLI at
// all, whatever the endpoint.
//
// WHAT THIS BACKEND IS FOR. Reading work — triage above all: deciding whether
// an issue is already satisfied, duplicated or moot. That needs a real agent
// loop (the model must look at the repository before it judges) but not a
// coding CLI, and it is the workload worth moving off a shared GPU. Measured on
// the burn-in fleet 2026-09-15, Qwen3-8B-int4-cw on an Intel NPU: 98% NPU
// utilisation with the GPU at 0%, so triage stops competing with reviews for
// the card.
//
// ONE TOOL, DELIBERATELY. The loop offers `run_command` and nothing else. A
// small model given a wide tool surface picks the wrong tool and argues with
// the schema; a shell in the session's own workspace covers reading, grepping
// and git, which is the whole of what a triage turn does. Anything this
// backend should NOT be able to do is excluded by the container it runs in,
// not by the absence of a tool it could have been handed.

// OpenVINOBackendConfig wires the backend to the server it talks to.
type OpenVINOBackendConfig struct {
	// BaseURL is the server root, WITHOUT the /v1 suffix.
	BaseURL string

	// DefaultModel is served when a request names none.
	DefaultModel string

	// MaxToolRounds bounds how many times one turn may call a tool before
	// the backend gives up. Zero takes DefaultOpenVINOToolRounds.
	//
	// A BOUND IS REQUIRED, NOT ADVISORY: a model that keeps calling a tool
	// without concluding would otherwise hold the session for ever, and a
	// small model is likelier to do it than a large one.
	MaxToolRounds int

	// Thinking lets a reasoning model emit its <think> block.
	//
	// OFF BY DEFAULT, AND THE MEASUREMENTS ARE WHY. Qwen3-8B on an NPU,
	// 2026-09-15, same triage question both ways: thinking off answered
	// correctly in 6-8 seconds; thinking on spent 37-99 seconds and was cut
	// off mid-reasoning having produced no answer at all. A turn that never
	// reaches a verdict is worse than a slow one, and triage runs on a
	// schedule rather than a deadline.
	Thinking bool

	// ChatPath is the completions route on this endpoint. Empty takes
	// DefaultOVMSChatPath.
	//
	// IT IS CONFIGURABLE BECAUSE THIS BACKEND IS NOT ABOUT ONE SERVER. What it
	// actually is: an OpenAI-compatible HTTP client that runs its tool calls
	// inside a container -- which is the shape triage needs, because triage
	// must READ THE REPOSITORY, and a plain model-server backend (no container,
	// no tools) cannot. OVMS serves /v3/chat/completions; ollama, llama-server
	// and vLLM serve /v1/chat/completions. Hardcoding either one ties a
	// capability to a vendor for no reason.
	ChatPath string

	// ContextTokens is what the operator says this endpoint's model accepts.
	// Zero declares nothing, and the limit is then learned from refusals. See
	// context_limit.go for why this is a declaration rather than a probe.
	ContextTokens int

	// SandboxSecurity is the privilege policy the tool container runs under,
	// exactly as for the ACP backends' sandboxes: --cap-drop ALL, the add-back
	// capability set, and the pids limit. The zero value is the secure
	// default. See sandbox_security.go.
	SandboxSecurity SandboxSecurity

	// RequestTimeout bounds a single completion. Zero takes the default.
	RequestTimeout time.Duration

	// HTTPClient is the client used for every call. Nil builds one.
	HTTPClient *http.Client
}

const (
	// DefaultOpenVINOToolRounds bounds one turn's tool calls.
	DefaultOpenVINOToolRounds = 12

	// toolBudgetSpentPrompt is what the loop says when it withdraws the
	// tools for the final round. It states the budget is gone, forbids
	// asking for more, and requires the answer in the format the caller's
	// own charter already asked for — naming no specific format here, because
	// this backend serves every role and only the charter knows which.
	// StopReasonToolBudgetSpent is reported for an answer given on the final,
	// tool-less round: the model was told its budget was spent and concluded
	// from what it had. A caller reading a thin answer under this stop reason
	// is looking at a give-up, not a judgement.
	StopReasonToolBudgetSpent = "tool_budget_spent"

	// contentBlockToolUse is the block type reported once per command the
	// loop ran, before the answer block.
	contentBlockToolUse = "tool_use"

	toolBudgetSpentPrompt = "Your tool-call budget for this turn is spent and no further " +
		"commands can be run. Answer now, in full, using only what you have " +
		"already gathered, in exactly the format your instructions require. " +
		"If the evidence is incomplete, say so inside that format rather than " +
		"asking to investigate further."

	// DefaultOpenVINORequestTimeout bounds one completion. An NPU is slower
	// than a datacentre GPU and a long read is normal, so this is generous.
	DefaultOpenVINORequestTimeout = 10 * time.Minute
)

// OpenVINOBackend runs an agent loop against an OpenVINO Model Server.
type OpenVINOBackend struct {
	config           OpenVINOBackendConfig
	containerRuntime ContainerRuntime

	// contextLimit is what this endpoint will accept, declared and learned.
	contextLimit *ContextLimit
	logger       interfaces0Logger

	mu       sync.Mutex
	sessions map[string]*openVINOSession
}

// interfaces0Logger is the minimal logger this backend needs. It is declared
// here rather than imported so the backend does not depend on the interfaces
// package for one method.
type interfaces0Logger interface {
	Warnf(format string, args ...any)
}

type openVINOSession struct {
	id           string
	model        string
	systemPrompt string
	workDir      string
	containerID  string
	createdAt    time.Time
}

var _ AgentBackend = (*OpenVINOBackend)(nil)

// NewOpenVINOBackend builds the backend.
func NewOpenVINOBackend(config OpenVINOBackendConfig) *OpenVINOBackend {
	if config.MaxToolRounds <= 0 {
		config.MaxToolRounds = DefaultOpenVINOToolRounds
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = DefaultOpenVINORequestTimeout
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: config.RequestTimeout}
	}
	return &OpenVINOBackend{
		config:       config,
		sessions:     map[string]*openVINOSession{},
		contextLimit: NewContextLimit(config.ContextTokens),
	}
}

// DefaultOVMSChatPath is the completions route OpenVINO Model Server serves.
// Every other OpenAI-compatible server in common use serves /v1 instead.
const DefaultOVMSChatPath = "/v3/chat/completions"

// chatURL is the endpoint one completion is posted to.
func (b *OpenVINOBackend) chatURL() string {
	path := strings.TrimSpace(b.config.ChatPath)
	if path == "" {
		path = DefaultOVMSChatPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(b.config.BaseURL, "/") + path
}

// SetContainerRuntime installs the runtime tool calls execute in.
func (b *OpenVINOBackend) SetContainerRuntime(r ContainerRuntime) { b.containerRuntime = r }

// ContextBound reports the tightest prompt ceiling this backend knows, in
// estimated tokens, or zero when nothing has been declared or learned. It
// exists so the daemon's wiring can be proven: a declaration that stops at the
// config teaches the backend nothing, and the first oversized prompt is still
// a wasted round trip.
func (b *OpenVINOBackend) ContextBound() int { return b.contextLimit.Bound() }

// HasContainerRuntime reports whether this backend can run a sandboxed
// session. It exists so the daemon's wiring can be PROVEN rather than assumed:
// a backend missing its runtime is indistinguishable from a working one until
// the first spawn, and on a required-policy daemon that is every spawn.
func (b *OpenVINOBackend) HasContainerRuntime() bool { return b.containerRuntime != nil }

// SetLogger installs an optional logger.
func (b *OpenVINOBackend) SetLogger(l interfaces0Logger) { b.logger = l }

// Type implements AgentBackend.
func (*OpenVINOBackend) Type() string { return "openvino" }

// Capabilities implements AgentBackend.
//
// TOOL USE IS TRUE AND IT IS THE POINT. The Ollama backend reports false and
// says so plainly — it is a model server, not an agent — and that is why it
// cannot triage: a turn that cannot read the repository cannot judge whether
// the work is already there.
func (*OpenVINOBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Kind:               BackendKindAgent,
		PersistentSessions: true,
		ToolUse:            true,
		FileAccess:         true,
	}
}

// ToolGuidance implements AgentBackend.
func (*OpenVINOBackend) ToolGuidance() string {
	return "You have one tool, run_command, which runs a shell command in the " +
		"repository you are working on and returns its combined output. Use it to " +
		"read files, search and inspect history before you answer. When you have " +
		"the evidence you need, answer in plain text and call no further tools."
}

// Spawn implements AgentBackend.
func (b *OpenVINOBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "must not be nil"}
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = b.config.DefaultModel
	}
	if model == "" {
		return nil, &InputValidationError{Field: "model", Message: "an openvino session needs a model, and the backend declares no default"}
	}
	sess := &openVINOSession{
		id:           "ov-" + newOpenVINOSessionID(),
		model:        model,
		systemPrompt: req.SystemPrompt,
		workDir:      req.WorkDir,
		createdAt:    time.Now().UTC(),
	}
	// A TURN THAT CANNOT READ THE REPOSITORY CANNOT JUDGE IT. The tool runs
	// inside a container over the session's workspace, so the session needs one
	// before the first prompt — and a session that silently had none would
	// answer from the issue text alone, confidently and without evidence,
	// which is the failure mode triage exists to prevent.
	if req.SandboxMode == SandboxModeRequired || strings.TrimSpace(req.ContainerImage) != "" {
		if b.containerRuntime == nil {
			return nil, &InputValidationError{
				Field:   "container_runtime",
				Message: "a sandboxed openvino session needs a container runtime and none is installed",
			}
		}
		image := strings.TrimSpace(req.ContainerImage)
		if image == "" {
			return nil, &InputValidationError{Field: "container_image", Message: "required for a sandboxed session"}
		}
		spec := &ContainerSpec{
			Image:   image,
			WorkDir: WorkspaceCheckoutRoot,
			// The container exists to be exec'd into, so it runs a process
			// that does nothing and stays alive; the CLI backends run an agent
			// here instead.
			//
			// THE ARGUMENTS GO IN Command, NOT IN Entrypoint. The runtime maps
			// only Entrypoint[0] onto `--entrypoint` and drops the rest
			// (container_runtime.go), so `{"tail","-f","/dev/null"}` launched
			// a BARE `tail`, which read EOF from a container with no stdin and
			// exited at once -- and AutoRemove then took the container away.
			// The spawn still returned an id, so the session looked healthy
			// for about a second and every later exec failed with
			// "container: not found". Measured on both bound projects,
			// 2026-09-16. This mirrors the ACP backend, which clears the
			// entrypoint and passes the whole command.
			Entrypoint: []string{},
			Command:    []string{"tail", "-f", "/dev/null"},
			// THE SPAWN'S ENVIRONMENT TRAVELS, exactly as it does for the ACP
			// backends. It carries how the checkout authenticates and whom it
			// trusts -- the ssh agent socket and the project's pinned host key
			// among them -- so a container started without it clones nothing:
			// measured as "Host key verification failed" on `git fetch origin
			// main`, on both bound projects, 2026-09-16.
			Env:        req.Env,
			AutoRemove: true,
			Network:    string(SandboxNetworkDefault),
			// The daemon's own uid, for the same reason the ACP backend uses
			// it: the checkout is a bind-mounted host directory owned by that
			// user, and a container running as anybody else cannot write it.
			User:   fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
			Mounts: append([]ContainerMount(nil), req.ExtraMounts...),
		}
		if wd := strings.TrimSpace(req.WorkDir); wd != "" {
			spec.Mounts = append(spec.Mounts, ContainerMount{Source: wd, Target: WorkspaceCheckoutRoot})
		}
		// The same privilege policy as every other sandboxed container:
		// --cap-drop ALL, the add-back set, and a pids bound.
		if secErr := b.config.SandboxSecurity.Apply(spec); secErr != nil {
			return nil, secErr
		}
		containerID, runErr := b.containerRuntime.Run(ctx, spec)
		if runErr != nil {
			return nil, runErr
		}
		sess.containerID = containerID
		sess.workDir = WorkspaceCheckoutRoot
	}

	b.mu.Lock()
	b.sessions[sess.id] = sess
	b.mu.Unlock()
	// THE CONTAINER MUST TRAVEL WITH THE SESSION. It is created here, by this
	// process's runtime, so this response is the only place its identity can
	// come from -- and the control plane refuses a session that reports none:
	// triage with "session ... is not containerized", a delivery with "runs no
	// container". Leaving the field empty is therefore indistinguishable from
	// having run no container at all, while a container sits running on the
	// host. Measured 2026-09-16: every openvino triage job spawned its
	// container, reported no id, and was refused one second later.
	//
	// Empty stays a real answer for an UNSANDBOXED session, which genuinely
	// has none.
	return &Session{
		ID:          sess.id,
		Backend:     b.Type(),
		Model:       model,
		Status:      SessionStateActive,
		ContainerID: sess.containerID,
	}, nil
}

// Status implements AgentBackend.
func (b *OpenVINOBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sess, ok := b.sessions[sessionID]
	if !ok {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	return &SessionStatus{SessionID: sess.id, State: SessionStateActive}, nil
}

// Close implements AgentBackend.
func (b *OpenVINOBackend) Close(ctx context.Context, sessionID string) error {
	b.mu.Lock()
	sess := b.sessions[sessionID]
	delete(b.sessions, sessionID)
	b.mu.Unlock()
	// THE CONTAINER IS THIS BACKEND'S TO STOP. AutoRemove reclaims it once it
	// exits, but nothing exits a `tail -f` on its own, so a session closed
	// without this leaks one container per triage run.
	if sess != nil && sess.containerID != "" && b.containerRuntime != nil {
		if err := b.containerRuntime.Stop(ctx, sess.containerID); err != nil && b.logger != nil {
			b.logger.Warnf("openvino: could not stop container %s for session %s: %s",
				sess.containerID, sessionID, err.Error())
		}
	}
	return nil
}

// newOpenVINOSessionID returns an identifier unique within this process.
func newOpenVINOSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// =============================================================================
// The turn — a chat completion, its tool calls, and the loop between them
// =============================================================================

// ovMessage is one entry in the conversation sent to the server.
type ovMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	ToolCalls  []ovToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type ovToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ovRequest struct {
	Model     string      `json:"model"`
	Messages  []ovMessage `json:"messages"`
	Tools     []ovTool    `json:"tools,omitempty"`
	MaxTokens int         `json:"max_tokens,omitempty"`
	Stream    bool        `json:"stream"`

	// ChatTemplateKwargs reaches the model's chat template. For a reasoning
	// model it carries enable_thinking; see OpenVINOBackendConfig.Thinking.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

// estimateTokens sizes what the MODEL will read, not what the wire carries.
//
// THE ENVELOPE IS NOT PROMPT. Estimating over the marshalled payload counts the
// JSON keys, the model name and the tool schemas, which the tokenizer also sees
// but which do not scale with the conversation — so on a short prompt the
// overhead dominated and the calibrated ratio came out at 0.4 against a real
// count of 10. Measured against a live server 2026-09-16. Summing the message
// content and the tool descriptions tracks the thing that actually grows.
func (r ovRequest) estimateTokens() int {
	total := EstimateTokens(r.Model)
	for _, message := range r.Messages {
		total += EstimateTokens(message.Content)
		for _, call := range message.ToolCalls {
			total += EstimateTokens(call.Function.Arguments)
		}
	}
	for _, tool := range r.Tools {
		total += EstimateTokens(tool.Function.Description)
	}
	return total
}

// ovTool is a FUNCTION tool and nothing else. OVMS answers
// "Only function tools are supported" to anything other than this shape, which
// is exactly why the vendor CLI cannot drive it.
type ovTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type ovResponse struct {
	Choices []struct {
		FinishReason string    `json:"finish_reason"`
		Message      ovMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error any `json:"error,omitempty"`
}

// runCommandTool is the one tool offered. See the file header for why there is
// exactly one.
func runCommandTool() ovTool {
	t := ovTool{Type: "function"}
	t.Function.Name = "run_command"
	t.Function.Description = "Run a shell command in the repository and return its combined output. " +
		"Use it to read files, search, and inspect git history."
	t.Function.Parameters = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to run, e.g. \"ls\", \"git log --oneline -5\", \"grep -rn foo .\"",
			},
		},
		"required": []string{"command"},
	}
	return t
}

// Prompt implements AgentBackend: one turn, including any tool calls it makes.
func (b *OpenVINOBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, &InputValidationError{Field: "prompt", Message: "cannot be empty"}
	}

	b.mu.Lock()
	sess := b.sessions[req.SessionID]
	b.mu.Unlock()

	model := strings.TrimSpace(req.Model)
	system := req.SystemPrompt
	containerID, workDir := "", ""
	if sess != nil {
		if model == "" {
			model = sess.model
		}
		if system == "" {
			system = sess.systemPrompt
		}
		containerID, workDir = sess.containerID, sess.workDir
	}
	if model == "" {
		model = b.config.DefaultModel
	}
	if model == "" {
		return nil, &InputValidationError{Field: "model", Message: "no model on the request, the session or the backend"}
	}

	messages := make([]ovMessage, 0, 4)
	if s := strings.TrimSpace(system); s != "" {
		messages = append(messages, ovMessage{Role: "system", Content: s})
	}
	messages = append(messages, ovMessage{Role: "user", Content: req.Prompt})

	var usage TokenUsage

	// THE LAST ROUND IS ASKED FOR AN ANSWER, NOT FOR MORE EVIDENCE. A model
	// that investigates well will spend every round it is given — one
	// measured triage turn read the CI workflow, go.mod, the upstream module
	// versions and the implementation, all of it on point, and then failed
	// because the budget ended mid-investigation. Withdrawing the tools and
	// saying so converts that into the judgement a human triager makes when
	// their time box expires: conclude from what you have. It is NOT the
	// silent truncation this loop has always refused — no partial text is
	// promoted to an answer, the model is asked for a real one.
	// EVERY TOOL CALL IS REPORTED AS A BLOCK, so the turn a caller audits says
	// how many commands were run and whether the answer came on the final,
	// tool-less round. Without this a triage turn that ran twelve commands
	// and then gave a 27-byte answer looked identical to one that answered
	// from the prompt alone -- which is how the NPU's give-ups on go-luks2
	// (2026-09-21) could not be told from its judgements.
	var toolBlocks []ContentBlock
	for round := 0; round <= b.config.MaxToolRounds; round++ {
		final := round == b.config.MaxToolRounds
		tools := []ovTool{runCommandTool()}
		if final {
			tools = nil
			messages = append(messages, ovMessage{
				Role:    "user",
				Content: toolBudgetSpentPrompt,
			})
		}
		resp, err := b.complete(ctx, ovRequest{
			Model: model, Messages: messages, Tools: tools, Stream: false,
			ChatTemplateKwargs: map[string]any{"enable_thinking": b.config.Thinking},
		})
		if err != nil {
			return nil, err
		}
		if len(resp.Choices) == 0 {
			return nil, &OpenVINOEmptyResponseError{SessionID: req.SessionID, Model: model}
		}
		usage.InputTokens += int64(resp.Usage.PromptTokens)
		usage.OutputTokens += int64(resp.Usage.CompletionTokens)

		choice := resp.Choices[0]
		if len(choice.Message.ToolCalls) == 0 {
			answer := stripThinking(choice.Message.Content)
			if final && strings.TrimSpace(answer) == "" {
				// Asked point blank for a conclusion and produced nothing.
				// That is the failure the bound exists to report.
				return nil, &OpenVINOToolRoundsExceededError{
					SessionID: req.SessionID, Model: model, Rounds: b.config.MaxToolRounds,
				}
			}
			stop := StopReasonEndTurn
			if final {
				stop = StopReasonToolBudgetSpent
			}
			return &PromptResponse{
				SessionID:  req.SessionID,
				Content:    append(toolBlocks, ContentBlock{Type: "text", Text: answer}),
				Model:      model,
				StopReason: stop,
				Usage:      &usage,
				ToolUse:    &ToolUseCensus{Calls: len(toolBlocks)},
			}, nil
		}
		if final {
			// Tools were not offered, so a tool call is the model ignoring
			// the instruction rather than needing more evidence.
			return nil, &OpenVINOToolRoundsExceededError{
				SessionID: req.SessionID, Model: model, Rounds: b.config.MaxToolRounds,
			}
		}

		// The assistant's tool-call message must be replayed before its
		// results, or the server cannot match result to call.
		messages = append(messages, choice.Message)
		for _, call := range choice.Message.ToolCalls {
			toolBlocks = append(toolBlocks, ContentBlock{
				Type: contentBlockToolUse,
				Text: call.Function.Name + " " + truncateForLog(call.Function.Arguments, 200),
			})
			out := b.runTool(ctx, containerID, workDir, call)
			messages = append(messages, ovMessage{
				Role: "tool", ToolCallID: call.ID, Content: out,
			})
		}
	}
	// Unreachable: the final round either answers or returns above. Stated so
	// the bound has one meaning if the loop condition is ever edited.
	return nil, &OpenVINOToolRoundsExceededError{
		SessionID: req.SessionID, Model: model, Rounds: b.config.MaxToolRounds,
	}
}

// complete performs one chat completion.
func (b *OpenVINOBackend) complete(ctx context.Context, body ovRequest) (*ovResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// REFUSED BEFORE THE ROUND TRIP WHEN THE CEILING IS ALREADY KNOWN. Sending
	// a prompt the endpoint has already refused at this size buys nothing but
	// latency, and on a slow local model that is minutes per attempt.
	estimate := body.estimateTokens()
	if !b.contextLimit.Allows(estimate) {
		return nil, &PromptTooLongError{
			Backend:   b.Type(),
			Model:     body.Model,
			Estimated: estimate,
			Bound:     b.contextLimit.Bound(),
			URL:       b.chatURL(),
		}
	}
	url := b.chatURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := b.config.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, &OpenVINOUnreachableError{URL: url, Cause: err}
	}
	defer httpResp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		// A REFUSAL FOR LENGTH TEACHES THE CEILING AND IS REPORTED AS ITSELF.
		// Left as a bare server error it reached the operator as "triage
		// produced no usable verdict" wrapped around a mediapipe stack trace,
		// which named neither the size that was sent nor the size that would
		// fit. Measured on the burn-in fleet: every triage turn failed that way
		// for a day before anybody read the raw body.
		errBody := string(raw)
		if IsLengthRefusal(httpResp.StatusCode, errBody) {
			b.contextLimit.Refused(estimate)
			return nil, &PromptTooLongError{
				Backend:   b.Type(),
				Model:     body.Model,
				Estimated: estimate,
				Bound:     b.contextLimit.Bound(),
				URL:       url,
				Status:    httpResp.StatusCode,
				Detail:    truncateForLog(errBody, 400),
			}
		}
		return nil, &OpenVINOServerError{URL: url, Status: httpResp.StatusCode, Body: truncateForLog(errBody, 600)}
	}
	var parsed ovResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &OpenVINOServerError{URL: url, Status: httpResp.StatusCode, Body: truncateForLog(string(raw), 600), Cause: err}
	}
	// A SUCCESSFUL RESPONSE CARRIES THE SERVER'S OWN TOKEN COUNT, which is the
	// one piece of metadata every OpenAI-compatible server agrees on. It is
	// what relates this package's estimate to real tokens, so an operator's
	// declared bound and a learned one can be compared in the same unit.
	b.contextLimit.Calibrate(estimate, parsed.Usage.PromptTokens)
	return &parsed, nil
}

// runTool executes one tool call and returns what the model should see.
//
// A FAILED COMMAND IS EVIDENCE, NOT AN ERROR. The model asked to run something
// and needs to know it failed and why; returning an error here would end the
// turn on what is often a typo the next round corrects.
func (b *OpenVINOBackend) runTool(ctx context.Context, containerID, workDir string, call ovToolCall) string {
	if call.Function.Name != "run_command" {
		return "error: unknown tool " + call.Function.Name + "; only run_command is available"
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "error: could not parse the arguments as JSON: " + err.Error()
	}
	if strings.TrimSpace(args.Command) == "" {
		return "error: command was empty"
	}
	if b.containerRuntime == nil || containerID == "" {
		return "error: this session has no container to run commands in"
	}
	result, err := b.containerRuntime.Exec(ctx, containerID, &ExecSpec{
		Cmd:     []string{"sh", "-lc", args.Command},
		WorkDir: workDir,
	})
	if err != nil {
		return "error: the command could not be run: " + err.Error()
	}
	out := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	if result.ExitCode != 0 {
		out = "exit status " + fmt.Sprint(result.ExitCode) + "\n" + out
	}
	// A tool result larger than the model's context is worse than a truncated
	// one: it displaces the question being asked.
	return truncateForLog(out, 24000)
}

// stripThinking removes a reasoning block a thinking model emits before its
// answer. Qwen3 wraps it in <think>…</think>; what the caller wants is the
// answer, and the ledger already records the tokens.
func stripThinking(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			return strings.TrimSpace(s)
		}
		end := strings.Index(s[start:], "</think>")
		if end < 0 {
			// An unterminated block means the turn was cut off inside its
			// reasoning; everything before it is all there is.
			return strings.TrimSpace(s[:start])
		}
		s = s[:start] + s[start+end+len("</think>"):]
	}
}

// OpenVINOToolRoundsExceededError reports a turn that kept calling tools
// without answering.
type OpenVINOToolRoundsExceededError struct {
	SessionID string
	Model     string
	Rounds    int
}

// Error implements the error interface.
func (e *OpenVINOToolRoundsExceededError) Error() string {
	return "orchestration: openvino turn called tools " + fmt.Sprint(e.Rounds) +
		" times without answering (model " + e.Model + ", session " + e.SessionID + ")"
}

// OpenVINOEmptyResponseError reports a completion carrying no choices.
type OpenVINOEmptyResponseError struct {
	SessionID string
	Model     string
}

// Error implements the error interface.
func (e *OpenVINOEmptyResponseError) Error() string {
	return "orchestration: openvino returned no choices for model " + e.Model +
		" (session " + e.SessionID + ")"
}

// OpenVINOUnreachableError reports a server that could not be dialled.
type OpenVINOUnreachableError struct {
	URL   string
	Cause error
}

// Error implements the error interface.
func (e *OpenVINOUnreachableError) Error() string {
	return "orchestration: openvino server unreachable at " + e.URL + ": " + e.Cause.Error()
}

// Unwrap exposes the dial failure.
func (e *OpenVINOUnreachableError) Unwrap() error { return e.Cause }

// OpenVINOServerError reports a non-2xx or unparseable answer, with a bounded
// excerpt of the body — OVMS states the reason there ("Only function tools are
// supported" being the one that decided this backend's shape).
type OpenVINOServerError struct {
	URL    string
	Status int
	Body   string
	Cause  error
}

// Error implements the error interface.
func (e *OpenVINOServerError) Error() string {
	msg := "orchestration: openvino " + e.URL + " answered " + fmt.Sprint(e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Unwrap exposes a decode failure when there was one.
func (e *OpenVINOServerError) Unwrap() error { return e.Cause }
