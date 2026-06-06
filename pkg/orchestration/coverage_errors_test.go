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
	"context"
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// agent_registry_types.go — disabled-agent typed errors
// =============================================================================

func TestAgentDisabledErrors(t *testing.T) {
	disabled := &AgentDisabledError{TenantID: "t1", AgentID: "a1"}
	if !strings.Contains(disabled.Error(), "tenant=t1") || !strings.Contains(disabled.Error(), "agent=a1") {
		t.Fatalf("AgentDisabledError message missing context: %q", disabled.Error())
	}
	if !errors.Is(disabled, ErrAgentDisabled) {
		t.Fatal("AgentDisabledError must unwrap to ErrAgentDisabled")
	}

	already := &AgentAlreadyDisabledError{TenantID: "t1", AgentID: "a1"}
	if !errors.Is(already, ErrAgentAlreadyDisabled) {
		t.Fatal("AgentAlreadyDisabledError must unwrap to ErrAgentAlreadyDisabled")
	}
	if !strings.Contains(already.Error(), "agent=a1") {
		t.Fatalf("AgentAlreadyDisabledError missing agent: %q", already.Error())
	}

	notDisabled := &AgentNotDisabledError{TenantID: "t1", AgentID: "a1"}
	if !errors.Is(notDisabled, ErrAgentNotDisabled) {
		t.Fatal("AgentNotDisabledError must unwrap to ErrAgentNotDisabled")
	}
	if !strings.Contains(notDisabled.Error(), "tenant=t1") {
		t.Fatalf("AgentNotDisabledError missing tenant: %q", notDisabled.Error())
	}
}

// =============================================================================
// artifact.go — artifact typed errors
// =============================================================================

func TestArtifactTypedErrors(t *testing.T) {
	tooLarge := &ArtifactTooLargeError{Name: "blob", Size: 100, Limit: 10}
	if !errors.Is(tooLarge, ErrArtifactTooLarge) {
		t.Fatal("ArtifactTooLargeError must unwrap to ErrArtifactTooLarge")
	}
	if !strings.Contains(tooLarge.Error(), "size=100") || !strings.Contains(tooLarge.Error(), "limit=10") {
		t.Fatalf("ArtifactTooLargeError missing size/limit: %q", tooLarge.Error())
	}

	kindInvalid := &ArtifactKindInvalidError{Kind: ArtifactKind("bogus")}
	if !errors.Is(kindInvalid, ErrArtifactKindInvalid) {
		t.Fatal("ArtifactKindInvalidError must unwrap to ErrArtifactKindInvalid")
	}
	if !strings.Contains(kindInvalid.Error(), "bogus") {
		t.Fatalf("ArtifactKindInvalidError missing kind: %q", kindInvalid.Error())
	}

	refInvalid := &ArtifactRefInvalidError{Kind: ArtifactKindFile, Missing: "path"}
	if !errors.Is(refInvalid, ErrArtifactRefInvalid) {
		t.Fatal("ArtifactRefInvalidError must unwrap to ErrArtifactRefInvalid")
	}
	if !strings.Contains(refInvalid.Error(), "missing=path") {
		t.Fatalf("ArtifactRefInvalidError missing field: %q", refInvalid.Error())
	}

	notFound := &ArtifactNotFoundError{TaskID: "task-1", Name: "out"}
	if !errors.Is(notFound, ErrArtifactNotFound) {
		t.Fatal("ArtifactNotFoundError must unwrap to ErrArtifactNotFound")
	}
	if !strings.Contains(notFound.Error(), "task=task-1") {
		t.Fatalf("ArtifactNotFoundError missing task: %q", notFound.Error())
	}
}

// =============================================================================
// errors.go — NoMatchingAgentError, NotLeadOfParentError, ParentJobNotFoundError
// =============================================================================

func TestNoMatchingAgentError(t *testing.T) {
	e := &NoMatchingAgentError{Constraint: "gpu", Detail: "no gpu daemons online"}
	if !errors.Is(e, ErrNoMatchingAgent) {
		t.Fatal("NoMatchingAgentError must unwrap to ErrNoMatchingAgent")
	}
	if !strings.Contains(e.Error(), "no gpu daemons online") {
		t.Fatalf("NoMatchingAgentError missing detail: %q", e.Error())
	}
}

func TestNotLeadOfParentError(t *testing.T) {
	e := &NotLeadOfParentError{ParentJobID: "p1", CallerAgentID: "c1", LeadAgentID: "l1"}
	if !errors.Is(e, ErrNotLeadOfParent) {
		t.Fatal("NotLeadOfParentError must unwrap to ErrNotLeadOfParent")
	}
	msg := e.Error()
	if !strings.Contains(msg, "parent=p1") || !strings.Contains(msg, "caller=c1") || !strings.Contains(msg, "lead=l1") {
		t.Fatalf("NotLeadOfParentError missing context: %q", msg)
	}
}

func TestParentJobNotFoundError(t *testing.T) {
	e := &ParentJobNotFoundError{ParentJobID: "p9"}
	if !errors.Is(e, ErrParentJobNotFound) {
		t.Fatal("ParentJobNotFoundError must unwrap to ErrParentJobNotFound")
	}
	if !strings.Contains(e.Error(), "p9") {
		t.Fatalf("ParentJobNotFoundError missing parent ID: %q", e.Error())
	}
}

// =============================================================================
// infer_types.go — inference typed errors
// =============================================================================

func TestInferTypedErrors(t *testing.T) {
	noChunks := &InferNoChunksError{Backend: "ollama"}
	if !errors.Is(noChunks, ErrInferNoChunks) {
		t.Fatal("InferNoChunksError must unwrap to ErrInferNoChunks")
	}
	if !strings.Contains(noChunks.Error(), "backend=ollama") {
		t.Fatalf("InferNoChunksError missing backend: %q", noChunks.Error())
	}

	cause := errors.New("rpc dial failed")
	backendErr := &InferBackendError{Op: "Infer", Err: cause}
	if !errors.Is(backendErr, ErrInferBackend) {
		t.Fatal("InferBackendError must unwrap to ErrInferBackend")
	}
	if !errors.Is(backendErr, cause) {
		t.Fatal("InferBackendError must unwrap to its cause")
	}
	if !strings.Contains(backendErr.Error(), "op=Infer") {
		t.Fatalf("InferBackendError missing op: %q", backendErr.Error())
	}

	unsupported := &InferUnsupportedError{Backend: "claude", Operation: "infer"}
	if !errors.Is(unsupported, ErrInferUnsupported) {
		t.Fatal("InferUnsupportedError must unwrap to ErrInferUnsupported")
	}
	if !strings.Contains(unsupported.Error(), "backend=claude") {
		t.Fatalf("InferUnsupportedError missing backend: %q", unsupported.Error())
	}
}

// =============================================================================
// project_errors.go & project_authz.go — project typed errors
// =============================================================================

func TestProjectStoreTypedErrors(t *testing.T) {
	byName := &ProjectStoreNotFoundError{TenantID: "t1", Name: "alpha"}
	if !errors.Is(byName, ErrProjectNotFound) {
		t.Fatal("ProjectStoreNotFoundError must unwrap to ErrProjectNotFound")
	}
	if !strings.Contains(byName.Error(), "name=alpha") {
		t.Fatalf("by-name error missing name: %q", byName.Error())
	}
	byID := &ProjectStoreNotFoundError{TenantID: "t1", ProjectID: "p1"}
	if !strings.Contains(byID.Error(), "id=p1") {
		t.Fatalf("by-id error missing id: %q", byID.Error())
	}

	exists := &ProjectAlreadyExistsError{TenantID: "t1", Name: "alpha"}
	if !errors.Is(exists, ErrProjectAlreadyExists) {
		t.Fatal("ProjectAlreadyExistsError must unwrap to ErrProjectAlreadyExists")
	}
	if !strings.Contains(exists.Error(), "name=alpha") {
		t.Fatalf("ProjectAlreadyExistsError missing name: %q", exists.Error())
	}

	invalidWithField := &ProjectInvalidError{Field: "name", Message: "required"}
	if !errors.Is(invalidWithField, ErrProjectInvalid) {
		t.Fatal("ProjectInvalidError must unwrap to ErrProjectInvalid")
	}
	if !strings.Contains(invalidWithField.Error(), "name: required") {
		t.Fatalf("ProjectInvalidError with field missing detail: %q", invalidWithField.Error())
	}
	invalidNoField := &ProjectInvalidError{Message: "bad"}
	if !strings.Contains(invalidNoField.Error(), "bad") || strings.Contains(invalidNoField.Error(), ": : ") {
		t.Fatalf("ProjectInvalidError without field formatted wrong: %q", invalidNoField.Error())
	}

	storeErr := &ProjectStoreError{Message: "marshal failed"}
	if !errors.Is(storeErr, ErrProjectStore) {
		t.Fatal("ProjectStoreError must unwrap to ErrProjectStore")
	}
	if !strings.Contains(storeErr.Error(), "marshal failed") {
		t.Fatalf("ProjectStoreError missing message: %q", storeErr.Error())
	}
}

func TestProjectAuthzTypedErrors(t *testing.T) {
	denied := &ProjectAccessDeniedError{
		ProjectID:    "p1",
		AgentID:      "a1",
		RequiredRole: ProjectRoleLead,
		ActualRole:   ProjectRoleWorker,
	}
	if !errors.Is(denied, ErrProjectAccessDenied) {
		t.Fatal("ProjectAccessDeniedError must unwrap to ErrProjectAccessDenied")
	}
	msg := denied.Error()
	if !strings.Contains(msg, "required=lead") || !strings.Contains(msg, "actual=worker") {
		t.Fatalf("ProjectAccessDeniedError missing roles: %q", msg)
	}

	roleInvalid := &ProjectRoleInvalidError{Role: "root"}
	if !errors.Is(roleInvalid, ErrProjectRoleInvalid) {
		t.Fatal("ProjectRoleInvalidError must unwrap to ErrProjectRoleInvalid")
	}
	if !strings.Contains(roleInvalid.Error(), "root") {
		t.Fatalf("ProjectRoleInvalidError missing role: %q", roleInvalid.Error())
	}
}

// =============================================================================
// codex_exec_backend.go — full lifecycle with a fake codex command
// =============================================================================

func TestCodexExecBackend_ConstructorAndMetadata(t *testing.T) {
	// Empty command defaults to "codex".
	def := NewCodexExecBackend(CodexExecBackendConfig{})
	if def.command != "codex" {
		t.Fatalf("expected default command 'codex', got %q", def.command)
	}
	if def.Type() != "codex" {
		t.Fatalf("Type() = %q, want codex", def.Type())
	}
	if def.ToolGuidance() != "" {
		t.Fatal("codex backend ToolGuidance must be empty")
	}
	caps := def.Capabilities()
	if caps.Kind != BackendKindModelServer || caps.PersistentSessions {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}

	// Explicit command is honored.
	custom := NewCodexExecBackend(CodexExecBackendConfig{Command: "/bin/echo", Args: []string{"-n"}})
	if custom.command != "/bin/echo" {
		t.Fatalf("expected custom command, got %q", custom.command)
	}
}

func TestCodexExecBackend_SpawnValidation(t *testing.T) {
	b := NewCodexExecBackend(CodexExecBackendConfig{})
	if _, err := b.Spawn(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil request")
	}
	if _, err := b.Spawn(context.Background(), &SpawnRequest{}); err == nil {
		t.Fatal("expected error for empty work dir")
	}
}

func TestCodexExecBackend_SpawnStatusClose(t *testing.T) {
	ctx := context.Background()
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: "/bin/echo", DefaultModel: "gpt-x"})

	sess, err := b.Spawn(ctx, &SpawnRequest{
		WorkDir:   t.TempDir(),
		ProjectID: "proj-1",
		Env:       map[string]string{"FOO": "bar"},
		ExtraArgs: []string{"--flag"},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !strings.HasPrefix(sess.ID, "codex-") {
		t.Fatalf("expected codex- prefixed session ID, got %q", sess.ID)
	}
	if sess.Model != "gpt-x" {
		t.Fatalf("expected default model gpt-x, got %q", sess.Model)
	}

	status, err := b.Status(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != SessionStateIdle {
		t.Fatalf("expected idle state, got %q", status.State)
	}

	// Status / Close on an unknown session yields SessionNotFoundError.
	if _, err := b.Status(ctx, "nope"); !isSessionNotFound(err) {
		t.Fatalf("expected SessionNotFoundError, got %v", err)
	}
	if err := b.Close(ctx, "nope"); !isSessionNotFound(err) {
		t.Fatalf("expected SessionNotFoundError, got %v", err)
	}

	if err := b.Close(ctx, sess.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Session is gone after Close.
	if _, err := b.Status(ctx, sess.ID); !isSessionNotFound(err) {
		t.Fatalf("expected SessionNotFoundError after close, got %v", err)
	}
}

func isSessionNotFound(err error) bool {
	var nf *SessionNotFoundError
	return errors.As(err, &nf)
}

func TestCodexExecBackend_PromptValidation(t *testing.T) {
	ctx := context.Background()
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: "/bin/echo"})

	if _, err := b.Prompt(ctx, nil); err == nil {
		t.Fatal("expected error for nil request")
	}
	if _, err := b.Prompt(ctx, &PromptRequest{}); err == nil {
		t.Fatal("expected error for empty prompt")
	}
	// Unknown session ID.
	if _, err := b.Prompt(ctx, &PromptRequest{SessionID: "ghost", Prompt: "hi"}); !isSessionNotFound(err) {
		t.Fatalf("expected SessionNotFoundError, got %v", err)
	}
}

func TestCodexExecBackend_PromptSuccess(t *testing.T) {
	ctx := context.Background()
	// /bin/echo prints its arguments; CombinedOutput returns no error so the
	// success branch (including the event-callback path) executes. The echoed
	// stdout flows through extractCodexAssistantText.
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: "/bin/echo"})

	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: t.TempDir(), Model: "m1"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var events []string
	resp, err := b.Prompt(ctx, &PromptRequest{
		SessionID:    sess.ID,
		Prompt:       "do the thing",
		SystemPrompt: "be terse",
		EventCallback: func(_, eventType, _ string) {
			events = append(events, eventType)
		},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("expected end_turn, got %q", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %+v", resp.Content)
	}
	if len(events) == 0 {
		t.Fatal("expected at least the start event callback")
	}
}

func TestCodexExecBackend_PromptCommandFailure(t *testing.T) {
	ctx := context.Background()
	// /bin/false exits non-zero, exercising the runErr branch → PromptError.
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: "/bin/false"})

	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	_, err = b.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "x"})
	var promptErr *PromptError
	if !errors.As(err, &promptErr) {
		t.Fatalf("expected PromptError, got %T: %v", err, err)
	}
	var oneShot *OneShotExecutionError
	if !errors.As(err, &oneShot) {
		t.Fatalf("expected wrapped OneShotExecutionError, got %v", err)
	}
}

func TestCodexExecBackend_PromptEphemeralSession(t *testing.T) {
	ctx := context.Background()
	// No SessionID: the backend creates an ephemeral session inline.
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: "/bin/echo", DefaultModel: "edm"})

	resp, err := b.Prompt(ctx, &PromptRequest{Prompt: "ephemeral"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Model != "edm" {
		t.Fatalf("expected ephemeral default model edm, got %q", resp.Model)
	}
}

// =============================================================================
// codex_exec_backend.go — extractCodexAssistantText parsing
// =============================================================================

func TestExtractCodexAssistantText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no markers falls back to raw", "just some text", "just some text"},
		{
			name: "last codex block wins",
			in:   "header\n--------\nuser\nplease review\ncodex\nfirst draft\ntokens used\ncodex\nfinal answer\n",
			want: "final answer",
		},
		{
			name: "trailing codex marker with no body falls back to raw",
			in:   "user\nhi\ncodex\ntokens used",
			want: "user\nhi\ncodex\ntokens used",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractCodexAssistantText(tt.in); got != tt.want {
				t.Fatalf("extractCodexAssistantText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewCodexExecSessionID(t *testing.T) {
	id1, err := newCodexExecSessionID()
	if err != nil {
		t.Fatalf("newCodexExecSessionID: %v", err)
	}
	id2, _ := newCodexExecSessionID()
	if !strings.HasPrefix(id1, "codex-") {
		t.Fatalf("expected codex- prefix, got %q", id1)
	}
	if id1 == id2 {
		t.Fatal("session IDs must be unique")
	}
	// "codex-" (6) + 32 hex chars.
	if len(id1) != 38 {
		t.Fatalf("expected 38-char id, got %d (%q)", len(id1), id1)
	}
}
