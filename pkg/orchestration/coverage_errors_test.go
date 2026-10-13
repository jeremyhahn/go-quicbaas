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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// fleet_registry_types.go — disabled-agent typed errors
// =============================================================================

func TestAgentDisabledErrors(t *testing.T) {
	disabled := &FleetNodeDisabledError{TenantID: "t1", NodeID: "a1"}
	if !strings.Contains(disabled.Error(), "tenant=t1") || !strings.Contains(disabled.Error(), "agent=a1") {
		t.Fatalf("FleetNodeDisabledError message missing context: %q", disabled.Error())
	}
	if !errors.Is(disabled, ErrFleetNodeDisabled) {
		t.Fatal("FleetNodeDisabledError must unwrap to ErrFleetNodeDisabled")
	}

	already := &FleetNodeAlreadyDisabledError{TenantID: "t1", NodeID: "a1"}
	if !errors.Is(already, ErrFleetNodeAlreadyDisabled) {
		t.Fatal("FleetNodeAlreadyDisabledError must unwrap to ErrFleetNodeAlreadyDisabled")
	}
	if !strings.Contains(already.Error(), "agent=a1") {
		t.Fatalf("FleetNodeAlreadyDisabledError missing agent: %q", already.Error())
	}

	notDisabled := &FleetNodeNotDisabledError{TenantID: "t1", NodeID: "a1"}
	if !errors.Is(notDisabled, ErrFleetNodeNotDisabled) {
		t.Fatal("FleetNodeNotDisabledError must unwrap to ErrFleetNodeNotDisabled")
	}
	if !strings.Contains(notDisabled.Error(), "tenant=t1") {
		t.Fatalf("FleetNodeNotDisabledError missing tenant: %q", notDisabled.Error())
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
// errors.go — NoMatchingFleetNodeError, NotLeadOfParentError, ParentJobNotFoundError
// =============================================================================

func TestNoMatchingAgentError(t *testing.T) {
	e := &NoMatchingFleetNodeError{Constraint: "gpu", Detail: "no gpu daemons online"}
	if !errors.Is(e, ErrNoMatchingFleetNode) {
		t.Fatal("NoMatchingFleetNodeError must unwrap to ErrNoMatchingFleetNode")
	}
	if !strings.Contains(e.Error(), "no gpu daemons online") {
		t.Fatalf("NoMatchingFleetNodeError missing detail: %q", e.Error())
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
		FleetNodeID:  "a1",
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

func TestCodexExecBackend_ForeignSessionValueIsNotFound(t *testing.T) {
	ctx := context.Background()
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: "/bin/echo"})
	b.sessions.Store("foreign", "not a session")

	if _, err := b.Status(ctx, "foreign"); !isSessionNotFound(err) {
		t.Fatalf("Status: expected SessionNotFoundError, got %v", err)
	}
	if _, err := b.Prompt(ctx, &PromptRequest{SessionID: "foreign", Prompt: "hi"}); !isSessionNotFound(err) {
		t.Fatalf("Prompt: expected SessionNotFoundError, got %v", err)
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
	codex := fakeCodex(t, codexTurnJSONL)
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: codex.path})

	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: t.TempDir(), Model: "m1"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var events []SessionEventKind
	resp, err := b.Prompt(ctx, &PromptRequest{
		SessionID:    sess.ID,
		Prompt:       "do the thing",
		SystemPrompt: "be terse",
		EventCallback: func(ev *SessionEvent) {
			events = append(events, ev.Kind)
		},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.StopReason != "end_turn" || resp.Model != "m1" {
		t.Fatalf("stop/model = %q/%q, want end_turn/m1", resp.StopReason, resp.Model)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "OK" {
		t.Fatalf("content = %+v, want the agent message OK", resp.Content)
	}
	// codex's input_tokens includes the cached part; the fresh prompt is the
	// difference (codexExecUsage).
	if resp.Usage == nil || resp.Usage.InputTokens != 3073 || resp.Usage.CacheReadInputTokens != 12160 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v, want fresh 3073 / cached 12160 / out 5", resp.Usage)
	}
	if args := codex.args(t); !strings.Contains(args, "exec --json -c model=\"m1\"") {
		t.Fatalf("argv = %q, want exec --json with the session's model", args)
	}
	if len(events) != 2 || events[0] != KindSystemMessage || events[1] != KindAssistantChunk {
		t.Fatalf("unexpected event kinds: %v", events)
	}
}

// TestCodexExecBackend_OnlyACompletedAnsweredTurnSucceeds asserts the event
// stream is the verdict (go-quicbaas#1058 review): an assistant message
// followed by turn.failed is a failure, as is a completed turn with no answer
// and a stream that never completes. Each failure carries the usage codex
// reported and the model the turn ran on.
func TestCodexExecBackend_OnlyACompletedAnsweredTurnSucceeds(t *testing.T) {
	answer := `{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"partial"}}`
	usage := `"usage":{"input_tokens":120,"cached_input_tokens":20,"output_tokens":7}`
	tests := []struct {
		name      string
		stream    string
		wantUsage bool
	}{
		// codex's turn.failed carries an error, not usage (codex-cli 0.154.0).
		{"failed after an assistant message", answer + "\n" + `{"type":"turn.failed","error":{"message":"stream disconnected"}}`, false},
		{"completed with no answer", `{"type":"turn.completed",` + usage + `}`, true},
		{"an answer that never completed", answer, false},
		{"an error event after the answer", answer + "\n" + `{"type":"error","error":"stream closed"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codex := fakeCodex(t, tc.stream)
			b := NewCodexExecBackend(CodexExecBackendConfig{Command: codex.path})
			sess, err := b.Spawn(context.Background(), &SpawnRequest{
				WorkDir: t.TempDir(), Env: map[string]string{"HOME": t.TempDir(), "CODEX_HOME": ""},
			})
			if err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			resp, err := b.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "x"})
			if err == nil {
				t.Fatalf("Prompt = %+v, want a failure", resp)
			}
			evidence, carried := EvidenceOf(err)
			if !carried {
				t.Fatalf("err = %v, want failure evidence", err)
			}
			if evidence.Model != CodexCLIDefaultModel {
				t.Errorf("evidence model = %q, want %q", evidence.Model, CodexCLIDefaultModel)
			}
			if tc.wantUsage {
				if evidence.Usage == nil || evidence.Usage.InputTokens != 100 || evidence.Usage.CacheReadInputTokens != 20 || evidence.Usage.OutputTokens != 7 {
					t.Errorf("evidence usage = %+v, want fresh 100 / cached 20 / out 7", evidence.Usage)
				}
			} else if evidence.Usage != nil {
				t.Errorf("evidence usage = %+v, want nil: the stream reported none", evidence.Usage)
			}
		})
	}
}

// TestCodexExecBackend_DefaultModelIsResolved covers a session that names no
// model: codex runs the one its config.toml pins, found through the session's
// CODEX_HOME or HOME, and with none pinned the turn is labelled
// CodexCLIDefaultModel. No model is put on argv in any of these cases.
func TestCodexExecBackend_DefaultModelIsResolved(t *testing.T) {
	writeConfig := func(t *testing.T, dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	tests := []struct {
		name  string
		setup func(t *testing.T) map[string]string
		want  string
	}{
		{"HOME config pins a model", func(t *testing.T) map[string]string {
			home := t.TempDir()
			writeConfig(t, filepath.Join(home, ".codex"), "model = \"gpt-5.5\"\n[profiles.x]\nmodel = \"other\"\n")
			return map[string]string{"HOME": home, "CODEX_HOME": ""}
		}, "gpt-5.5"},
		{"CODEX_HOME wins over HOME", func(t *testing.T) map[string]string {
			home, codexHome := t.TempDir(), t.TempDir()
			writeConfig(t, filepath.Join(home, ".codex"), "model = \"from-home\"\n")
			writeConfig(t, codexHome, "model = \"from-codex-home\"\n")
			return map[string]string{"HOME": home, "CODEX_HOME": codexHome}
		}, "from-codex-home"},
		{"no config is the CLI default", func(t *testing.T) map[string]string {
			return map[string]string{"HOME": t.TempDir(), "CODEX_HOME": ""}
		}, CodexCLIDefaultModel},
		{"a config that pins nothing is the CLI default", func(t *testing.T) map[string]string {
			home := t.TempDir()
			writeConfig(t, filepath.Join(home, ".codex"), "approval_policy = \"never\"\n")
			return map[string]string{"HOME": home, "CODEX_HOME": ""}
		}, CodexCLIDefaultModel},
		{"an unreadable config is the CLI default", func(t *testing.T) map[string]string {
			home := t.TempDir()
			writeConfig(t, filepath.Join(home, ".codex"), "model = = broken")
			return map[string]string{"HOME": home, "CODEX_HOME": ""}
		}, CodexCLIDefaultModel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codex := fakeCodex(t, codexTurnJSONL)
			b := NewCodexExecBackend(CodexExecBackendConfig{Command: codex.path})
			sess, err := b.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir(), Env: tc.setup(t)})
			if err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			resp, err := b.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "x"})
			if err != nil {
				t.Fatalf("Prompt: %v", err)
			}
			if resp.Model != tc.want {
				t.Fatalf("model = %q, want %q", resp.Model, tc.want)
			}
			if args := codex.args(t); strings.Contains(args, "model=") {
				t.Fatalf("argv = %q; a resolved default must not be sent to codex", args)
			}
		})
	}
}

// TestCodexExecTurnReachesTheLedgerOnTheDefaultModel is the go-quicbaas#1058
// review regression, end to end: a control-plane codex turn on the CLI's
// default model used to carry no usage and an empty model, which the ledger
// refused. It must now be ledgered with its tokens under a model name.
func TestCodexExecTurnReachesTheLedgerOnTheDefaultModel(t *testing.T) {
	codex := fakeCodex(t, codexTurnJSONL)
	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{
		"codex": NewCodexExecBackend(CodexExecBackendConfig{Command: codex.path}),
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl) //nolint:errcheck // NewAgentService returns this type
	ledger := NewConsumptionLedger(NewMemoryTaskKV())
	impl.SetConsumptionLedger(ledger)

	ctx := ContextWithTenant(context.Background(), "t1")
	spawn, err := svc.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "codex", WorkDir: t.TempDir(), ProjectID: "proj-1", Persistent: true,
		Env: map[string]string{"HOME": t.TempDir(), "CODEX_HOME": ""},
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: spawn.SessionID, ProjectID: "proj-1", Prompt: "hi"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 || rollup.TotalTokens != 15238 {
		t.Fatalf("ledger = %d entries / %d tokens, want the codex turn with 15238 tokens", rollup.Entries, rollup.TotalTokens)
	}
	if len(rollup.ByModel) != 1 {
		t.Fatalf("by model = %+v, want one model", rollup.ByModel)
	}
	for key := range rollup.ByModel {
		if key != "codex/"+CodexCLIDefaultModel {
			t.Fatalf("model key = %q, want codex/%s", key, CodexCLIDefaultModel)
		}
	}
}

// TestFailedCodexExecTurnReachesTheLedgerOnTheDefaultModel is the failure half
// of the ledger regression: a failed turn on codex's default model used to be
// recorded with an empty model, which the ledger refused, and with no usage.
// A turn codex completed without an answer is a failure that consumed tokens:
// it must be one failed entry, under a model name, with the tokens codex
// reported, priced when the table lists the model.
func TestFailedCodexExecTurnReachesTheLedgerOnTheDefaultModel(t *testing.T) {
	codex := fakeCodex(t, `{"type":"turn.started"}
{"type":"turn.completed","usage":{"input_tokens":1000000,"cached_input_tokens":0,"output_tokens":0}}`)
	kv := kvstore.NewMemory()
	svc, err := NewAgentService(NewAgentStore(kv), map[string]AgentBackend{
		"codex": NewCodexExecBackend(CodexExecBackendConfig{Command: codex.path}),
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl) //nolint:errcheck // NewAgentService returns this type
	ledger := NewConsumptionLedger(NewMemoryTaskKV())
	impl.SetConsumptionLedger(ledger)
	book, err := NewPriceBook(config.PricingConfig{Version: "v1", Models: []config.ModelPriceConfig{
		{Model: CodexCLIDefaultModel, Engine: "codex", Provider: "openai", InputPerMTok: 2},
	}})
	if err != nil {
		t.Fatalf("NewPriceBook: %v", err)
	}
	impl.SetPriceBook(book)

	ctx := ContextWithTenant(context.Background(), "t1")
	spawn, err := svc.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "codex", WorkDir: t.TempDir(), ProjectID: "proj-1", Persistent: true,
		Env: map[string]string{"HOME": t.TempDir(), "CODEX_HOME": ""},
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: spawn.SessionID, ProjectID: "proj-1", Prompt: "hi"}); err == nil {
		t.Fatal("PromptSession succeeded; a turn with no answer must fail")
	}

	rollup, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if rollup.Entries != 1 || rollup.Failures != 1 || rollup.InputTokens != 1_000_000 {
		t.Fatalf("ledger = %+v, want one failed entry with 1,000,000 input tokens", rollup.ConsumptionTotals)
	}
	if rollup.PricedEntries != 1 || rollup.EquivalentMicros != 2_000_000 {
		t.Errorf("priced/equivalent = %d/%d, want 1/2000000: the failed turn's tokens cost money", rollup.PricedEntries, rollup.EquivalentMicros)
	}
	if _, ok := rollup.ByModel["codex/"+CodexCLIDefaultModel]; !ok || len(rollup.ByModel) != 1 {
		t.Fatalf("by model = %+v, want codex/%s", rollup.ByModel, CodexCLIDefaultModel)
	}
}

// codexTurnJSONL is one turn as `codex exec --json` streams it (codex-cli
// 0.154.0; see execEvent).
const codexTurnJSONL = `{"type":"thread.started","thread_id":"th-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}
{"type":"turn.completed","usage":{"input_tokens":15233,"cached_input_tokens":12160,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}`

// fakeCodexCLI is a stand-in codex executable that prints a fixed event
// stream and records its argv.
type fakeCodexCLI struct {
	path    string
	argFile string
}

func fakeCodex(t *testing.T, stream string) fakeCodexCLI {
	t.Helper()
	dir := t.TempDir()
	cli := fakeCodexCLI{path: filepath.Join(dir, "codex"), argFile: filepath.Join(dir, "argv")}
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(events, []byte(stream+"\n"), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	script := "#!/bin/sh\ncat >/dev/null\necho \"$*\" > '" + cli.argFile + "'\ncat '" + events + "'\n"
	if err := os.WriteFile(cli.path, []byte(script), 0o700); err != nil { //nolint:gosec // test executable
		t.Fatalf("write fake codex: %v", err)
	}
	return cli
}

func (c fakeCodexCLI) args(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(c.argFile)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	return strings.TrimSpace(string(data))
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
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: fakeCodex(t, codexTurnJSONL).path, DefaultModel: "edm"})

	resp, err := b.Prompt(ctx, &PromptRequest{Prompt: "ephemeral"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Model != "edm" {
		t.Fatalf("expected ephemeral default model edm, got %q", resp.Model)
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
