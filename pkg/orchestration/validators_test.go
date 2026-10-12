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
	"time"
)

// =============================================================================
// Fakes
// =============================================================================

// mockAgentServiceForValidator implements AgentService with configurable
// PromptSession behavior for the agent validator.
type mockAgentServiceForValidator struct {
	AgentService
	promptResponse *PromptSessionResponse
	promptErr      error
	lastRequest    *PromptSessionRequest
}

func (m *mockAgentServiceForValidator) PromptSession(_ context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	m.lastRequest = req
	return m.promptResponse, m.promptErr
}

// fixedVerdict is a ValidatorFunc with a fixed verdict.
func fixedVerdict(passed bool, message string) ValidatorFunc {
	return func(_ context.Context, _ *JobContext, c ReviewCriterion) (ValidationResult, error) {
		return ValidationResult{Name: c.Name, Passed: passed, Message: message}, nil
	}
}

// jobWithArtifacts builds a JobContext carrying the artifacts under test.
func jobWithArtifacts(artifacts ...*TaskResult) *JobContext {
	return &JobContext{JobID: "job-1", TenantID: "tenant-a", Artifacts: artifacts}
}

// guardAllowing builds a guard over the given commands or fails the test.
func guardAllowing(t *testing.T, commands ...string) ExecutionGuard {
	t.Helper()
	g, err := NewExecutionGuard(commands)
	if err != nil {
		t.Fatalf("NewExecutionGuard: %v", err)
	}
	return g
}

// =============================================================================
// Typed errors
// =============================================================================

func TestValidatorNotFoundError(t *testing.T) {
	t.Parallel()
	err := &ValidatorNotFoundError{Name: "custom"}
	if !errors.Is(err, ErrValidatorNotFound) {
		t.Fatal("expected errors.Is(ErrValidatorNotFound)")
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Fatalf("expected the kind in the message, got %q", err.Error())
	}
}

func TestValidatorExecutionError(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	err := &ValidatorExecutionError{Validator: "command", Cause: cause}
	if !errors.Is(err, ErrValidatorExecution) || !errors.Is(err, cause) {
		t.Fatal("expected both the sentinel and the cause to unwrap")
	}
	if !strings.Contains(err.Error(), "command") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected message %q", err.Error())
	}
}

func TestValidatorPipelineError(t *testing.T) {
	t.Parallel()
	err := &ValidatorPipelineError{Criterion: "tests", Message: "failed"}
	if !errors.Is(err, ErrValidatorPipeline) {
		t.Fatal("expected errors.Is(ErrValidatorPipeline)")
	}
	if !strings.Contains(err.Error(), "tests") {
		t.Fatalf("unexpected message %q", err.Error())
	}
}

func TestValidatorRegistrationErrors(t *testing.T) {
	t.Parallel()
	regErr := &ValidatorRegistrationError{Message: "kind must not be empty"}
	if !errors.Is(regErr, ErrValidatorRegistration) || !strings.Contains(regErr.Error(), "kind must not be empty") {
		t.Fatalf("registration error: %v", regErr)
	}
	withKind := &ValidatorRegistrationError{Kind: "x", Message: "nil"}
	if !strings.Contains(withKind.Error(), "x: nil") {
		t.Fatalf("registration error with kind: %v", withKind)
	}
	dup := &ValidatorAlreadyRegisteredError{Kind: "x"}
	if !errors.Is(dup, ErrValidatorAlreadyRegistered) || !strings.Contains(dup.Error(), "x") {
		t.Fatalf("duplicate error: %v", dup)
	}
	unknown := &UnknownValidatorError{Kind: "y"}
	if !errors.Is(unknown, ErrWorkflowValidation) || !strings.Contains(unknown.Error(), "y") {
		t.Fatalf("unknown validator error: %v", unknown)
	}
}

// =============================================================================
// Registry
// =============================================================================

func TestValidatorRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	if err := reg.Register("test", fixedVerdict(true, "ok")); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := reg.Get("test")
	if err != nil || got == nil {
		t.Fatalf("expected the registered validator, got %v %v", got, err)
	}
	if kinds := reg.Kinds(); len(kinds) != 1 || kinds[0] != "test" {
		t.Fatalf("Kinds() = %v", kinds)
	}
}

func TestValidatorRegistry_RegisterRefusesDuplicateEmptyAndNil(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	if err := reg.Register("test", fixedVerdict(true, "")); err != nil {
		t.Fatalf("first register: %v", err)
	}
	var dup *ValidatorAlreadyRegisteredError
	if err := reg.Register("test", fixedVerdict(false, "")); !errors.As(err, &dup) || dup.Kind != "test" {
		t.Fatalf("second register: got %v, want ValidatorAlreadyRegisteredError", err)
	}
	var regErr *ValidatorRegistrationError
	if err := reg.Register("", fixedVerdict(true, "")); !errors.As(err, &regErr) {
		t.Fatalf("empty kind: got %v, want ValidatorRegistrationError", err)
	}
	if err := reg.Register("nil", nil); !errors.As(err, &regErr) || regErr.Kind != "nil" {
		t.Fatalf("nil validator: got %v, want ValidatorRegistrationError", err)
	}
}

func TestValidatorRegistry_Get_NotFound(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	_, err := reg.Get("missing")
	var notFound *ValidatorNotFoundError
	if !errors.As(err, &notFound) || notFound.Name != "missing" {
		t.Fatalf("got %v, want ValidatorNotFoundError", err)
	}
}

func TestValidatorRegistry_RunPipeline_AllPass(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	if err := reg.Register("check", fixedVerdict(true, "ok")); err != nil {
		t.Fatal(err)
	}
	criteria := []ReviewCriterion{
		{Name: "a", Validator: "check", Required: true},
		{Name: "b", Validator: "check", Required: false},
	}
	results, allPassed, err := reg.RunPipeline(context.Background(), jobWithArtifacts(), criteria)
	if err != nil || !allPassed || len(results) != 2 {
		t.Fatalf("results=%v allPassed=%v err=%v", results, allPassed, err)
	}
	if results[0].Name != "a" || results[1].Name != "b" {
		t.Fatalf("results carry the criterion names: %v", results)
	}
}

func TestValidatorRegistry_RunPipeline_RequiredFails(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	if err := reg.Register("pass", fixedVerdict(true, "")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("fail", fixedVerdict(false, "not good")); err != nil {
		t.Fatal(err)
	}
	criteria := []ReviewCriterion{
		{Name: "a", Validator: "pass", Required: true},
		{Name: "b", Validator: "fail", Required: true},
	}
	results, allPassed, err := reg.RunPipeline(context.Background(), jobWithArtifacts(), criteria)
	if err != nil || allPassed || len(results) != 2 {
		t.Fatalf("results=%v allPassed=%v err=%v", results, allPassed, err)
	}
}

func TestValidatorRegistry_RunPipeline_OptionalFailureDoesNotFailPipeline(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	if err := reg.Register("fail", fixedVerdict(false, "optional finding")); err != nil {
		t.Fatal(err)
	}
	criteria := []ReviewCriterion{{Name: "b", Validator: "fail", Required: false}}
	_, allPassed, err := reg.RunPipeline(context.Background(), jobWithArtifacts(), criteria)
	if err != nil || !allPassed {
		t.Fatalf("allPassed=%v err=%v", allPassed, err)
	}
}

func TestValidatorRegistry_RunPipeline_Empty(t *testing.T) {
	t.Parallel()
	results, allPassed, err := NewValidatorRegistry().RunPipeline(context.Background(), nil, nil)
	if err != nil || !allPassed || results != nil {
		t.Fatalf("results=%v allPassed=%v err=%v", results, allPassed, err)
	}
}

func TestValidatorRegistry_RunPipeline_UnknownKind(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	_, _, err := reg.RunPipeline(context.Background(), jobWithArtifacts(), []ReviewCriterion{{Name: "x", Validator: "nope"}})
	if !errors.Is(err, ErrValidatorNotFound) {
		t.Fatalf("got %v, want ErrValidatorNotFound", err)
	}
}

func TestValidatorRegistry_RunPipeline_ExecutionErrorStopsPipeline(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	execErr := errors.New("exec failed")
	if err := reg.Register("broken", func(_ context.Context, _ *JobContext, c ReviewCriterion) (ValidationResult, error) {
		return ValidationResult{Name: c.Name}, execErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("never", fixedVerdict(true, "")); err != nil {
		t.Fatal(err)
	}
	results, _, err := reg.RunPipeline(context.Background(), jobWithArtifacts(), []ReviewCriterion{
		{Name: "a", Validator: "broken"},
		{Name: "b", Validator: "never"},
	})
	if !errors.Is(err, ErrValidatorExecution) || !errors.Is(err, execErr) {
		t.Fatalf("got %v, want ValidatorExecutionError wrapping the cause", err)
	}
	if len(results) != 0 {
		t.Fatalf("the failing criterion must not be recorded as a result: %v", results)
	}
}

func TestValidatorRegistry_RunPipeline_ContextCancelled(t *testing.T) {
	t.Parallel()
	reg := NewValidatorRegistry()
	if err := reg.Register("check", fixedVerdict(true, "")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, allPassed, err := reg.RunPipeline(ctx, jobWithArtifacts(), []ReviewCriterion{{Name: "a", Validator: "check"}})
	if !errors.Is(err, context.Canceled) || allPassed {
		t.Fatalf("allPassed=%v err=%v", allPassed, err)
	}
}

// =============================================================================
// ArtifactValidator adapter
// =============================================================================

func TestArtifactValidator_JudgesTheJobsArtifacts(t *testing.T) {
	t.Parallel()
	var seen []*TaskResult
	inner := ValidatorFuncOverArtifacts(func(_ context.Context, c ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error) {
		seen = artifacts
		return ValidationResult{Name: c.Name, Passed: true}, nil
	})
	fn := ArtifactValidator(inner)
	job := jobWithArtifacts(&TaskResult{Output: "one"}, &TaskResult{Output: "two"})
	result, err := fn(context.Background(), job, ReviewCriterion{Name: "c"})
	if err != nil || !result.Passed || result.Name != "c" {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if len(seen) != 2 {
		t.Fatalf("the adapter must hand the job's artifacts through, saw %d", len(seen))
	}
	if _, err := fn(context.Background(), nil, ReviewCriterion{Name: "c"}); err != nil {
		t.Fatalf("a nil job means no artifacts, not an error: %v", err)
	}
}

func TestArtifactValidator_NilInnerIsAnExecutionError(t *testing.T) {
	t.Parallel()
	fn := ArtifactValidator(nil)
	_, err := fn(context.Background(), jobWithArtifacts(), ReviewCriterion{Name: "c", Validator: "x"})
	if !errors.Is(err, ErrValidatorExecution) || !errors.Is(err, ErrValidatorNilInput) {
		t.Fatalf("got %v, want ValidatorExecutionError wrapping ErrValidatorNilInput", err)
	}
}

// ValidatorFuncOverArtifacts adapts a function to the artifact-scoped
// Validator interface for tests.
type ValidatorFuncOverArtifacts func(ctx context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error)

// Validate implements Validator.
func (f ValidatorFuncOverArtifacts) Validate(ctx context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error) {
	return f(ctx, criterion, artifacts)
}

// =============================================================================
// Command validator
// =============================================================================

func TestValidateWithCommand_Success(t *testing.T) {
	t.Parallel()
	job := &JobContext{Guard: guardAllowing(t, "echo hello")}
	result, err := validateWithCommand(context.Background(), job, ReviewCriterion{Name: "echo", Config: "echo hello"})
	if err != nil || !result.Passed || result.Message != "hello" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestValidateWithCommand_Failure(t *testing.T) {
	t.Parallel()
	job := &JobContext{Guard: guardAllowing(t, "echo bad >&2; exit 3")}
	result, err := validateWithCommand(context.Background(), job, ReviewCriterion{Name: "fail", Config: "echo bad >&2; exit 3"})
	if err != nil {
		t.Fatalf("a non-zero exit is a verdict, not an error: %v", err)
	}
	if result.Passed || result.Message != "bad" || result.Details != "exit code: 3" {
		t.Fatalf("result=%v", result)
	}
}

func TestValidateWithCommand_EmptyConfig(t *testing.T) {
	t.Parallel()
	job := &JobContext{Guard: guardAllowing(t, "true")}
	result, err := validateWithCommand(context.Background(), job, ReviewCriterion{Name: "empty"})
	if !errors.Is(err, ErrValidatorExecution) || result.Passed {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestValidateWithCommand_ContextTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	job := &JobContext{Guard: guardAllowing(t, "sleep 5")}
	result, err := validateWithCommand(ctx, job, ReviewCriterion{Name: "slow", Config: "sleep 5"})
	if result.Passed {
		t.Fatal("a killed command must not pass")
	}
	if err == nil && result.Details == "" {
		t.Fatal("a killed command must report either an error or a non-zero exit")
	}
}

func TestValidateWithCommand_RunsInTheJobsWorkDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	job := &JobContext{WorkDir: dir, Guard: guardAllowing(t, "pwd")}
	result, err := validateWithCommand(context.Background(), job, ReviewCriterion{Name: "where", Config: "pwd"})
	if err != nil || !result.Passed {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if !strings.HasSuffix(result.Message, dir) {
		t.Fatalf("the command must run in the prepared directory: got %q, want suffix %q", result.Message, dir)
	}
}

func TestValidateWithCommand_RefusesANonAllowlistedCommand(t *testing.T) {
	t.Parallel()
	job := &JobContext{Guard: guardAllowing(t, "make test")}
	result, err := validateWithCommand(context.Background(), job, ReviewCriterion{Name: "tests", Config: "make test ./..."})
	var refused *CommandRefusedError
	if !errors.As(err, &refused) || refused.Name != "tests" {
		t.Fatalf("got %v, want CommandRefusedError", err)
	}
	if result.Passed {
		t.Fatal("a refused command must not pass")
	}
	if strings.Contains(err.Error(), "./...") {
		t.Fatal("the refused command text must not be rendered")
	}
}

func TestValidateWithCommand_ZeroGuardRefusesEverything(t *testing.T) {
	t.Parallel()
	job := &JobContext{}
	if _, err := validateWithCommand(context.Background(), job, ReviewCriterion{Name: "echo", Config: "echo hi"}); !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("got %v, want ErrCommandRefused", err)
	}
}

func TestValidateWithCommand_NilJobAndEmptyConfig(t *testing.T) {
	t.Parallel()
	if _, err := validateWithCommand(context.Background(), nil, ReviewCriterion{Name: "x", Config: "true"}); !errors.Is(err, ErrValidatorNilInput) {
		t.Fatalf("nil job: got %v, want ErrValidatorNilInput", err)
	}
	job := &JobContext{Guard: guardAllowing(t, "true")}
	if _, err := validateWithCommand(context.Background(), job, ReviewCriterion{Name: "x"}); !errors.Is(err, ErrValidatorExecution) {
		t.Fatalf("empty config: got %v, want ErrValidatorExecution", err)
	}
}

// =============================================================================
// Policy validator
// =============================================================================

func TestPolicyValidator_NoSecrets(t *testing.T) {
	t.Parallel()
	v := NewPolicyValidator()
	result, err := v.Validate(context.Background(), ReviewCriterion{Name: "p", Config: "no secrets in output"},
		[]*TaskResult{{Output: "clean output"}})
	if err != nil || !result.Passed || result.Name != "p" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestPolicyValidator_SecretsFound(t *testing.T) {
	t.Parallel()
	for name, output := range map[string]string{
		"api key":         "api_key=abc123",
		"private key pem": "-----BEGIN RSA PRIVATE KEY-----",
		"bearer token":    "Authorization: Bearer eyJhbGciOi",
		"password":        "password: hunter2",
		"aws secret":      "aws_secret_access_key = wJalrXUtnFEMI",
	} {
		v := NewPolicyValidator()
		result, err := v.Validate(context.Background(), ReviewCriterion{Name: "p", Config: "no secrets in output"},
			[]*TaskResult{{Output: output}})
		if err != nil || result.Passed {
			t.Fatalf("%s: result=%v err=%v", name, result, err)
		}
	}
}

func TestPolicyValidator_UnknownPolicyFallsBackToSecretScan(t *testing.T) {
	t.Parallel()
	v := NewPolicyValidator()
	result, err := v.Validate(context.Background(), ReviewCriterion{Name: "p", Config: "some other policy"},
		[]*TaskResult{nil, {Output: "secret_key=xyz"}})
	if err != nil || result.Passed {
		t.Fatalf("result=%v err=%v", result, err)
	}
	clean, err := v.Validate(context.Background(), ReviewCriterion{Name: "p", Config: "some other policy"}, nil)
	if err != nil || !clean.Passed {
		t.Fatalf("clean=%v err=%v", clean, err)
	}
}

// =============================================================================
// Agent validator
// =============================================================================

func agentJob(svc AgentService, artifacts ...*TaskResult) *JobContext {
	return &JobContext{
		Agents:    svc,
		Config:    &JobConfig{LeadBackend: "claude", LeadModel: "m"},
		Artifacts: artifacts,
	}
}

func TestValidateWithAgent_PromptConstructionAndVerdict(t *testing.T) {
	t.Parallel()
	svc := &mockAgentServiceForValidator{promptResponse: &PromptSessionResponse{
		Content: []ContentBlock{{Type: "text", Text: `{"passed": true, "message": "looks good"}`}},
	}}
	result, err := validateWithAgent(context.Background(), agentJob(svc, nil, &TaskResult{Output: "artifact-one"}),
		ReviewCriterion{Name: "quality", Config: "is it good"})
	if err != nil || !result.Passed || result.Message != "looks good" {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if svc.lastRequest == nil || svc.lastRequest.Backend != "claude" || svc.lastRequest.Model != "m" {
		t.Fatalf("the prompt must go to the job's lead backend and model: %+v", svc.lastRequest)
	}
	if !strings.Contains(svc.lastRequest.Prompt, "quality") || !strings.Contains(svc.lastRequest.Prompt, "artifact-one") {
		t.Fatalf("prompt missing criterion or artifact: %q", svc.lastRequest.Prompt)
	}
	if strings.Contains(svc.lastRequest.Prompt, "Artifact 1 ---\n\n") {
		t.Fatal("a nil artifact must be skipped, not rendered empty")
	}
}

func TestValidateWithAgent_Rejection(t *testing.T) {
	t.Parallel()
	svc := &mockAgentServiceForValidator{promptResponse: &PromptSessionResponse{
		Content: []ContentBlock{{Type: "text", Text: `{"passed": false, "message": "missing tests"}`}},
	}}
	result, err := validateWithAgent(context.Background(), agentJob(svc), ReviewCriterion{Name: "q"})
	if err != nil || result.Passed || result.Message != "missing tests" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestValidateWithAgent_PromptErrorAndInvalidJSON(t *testing.T) {
	t.Parallel()
	promptErr := errors.New("backend down")
	svc := &mockAgentServiceForValidator{promptErr: promptErr}
	if result, err := validateWithAgent(context.Background(), agentJob(svc), ReviewCriterion{Name: "q"}); !errors.Is(err, promptErr) || result.Passed {
		t.Fatalf("prompt error: result=%v err=%v", result, err)
	}
	svc = &mockAgentServiceForValidator{promptResponse: &PromptSessionResponse{
		Content: []ContentBlock{{Type: "text", Text: "not json"}},
	}}
	result, err := validateWithAgent(context.Background(), agentJob(svc), ReviewCriterion{Name: "q"})
	if !errors.Is(err, ErrValidatorExecution) || result.Passed || result.Details != "not json" {
		t.Fatalf("invalid json: result=%v err=%v", result, err)
	}
}

func TestValidateWithAgent_NilJobOrService(t *testing.T) {
	t.Parallel()
	if _, err := validateWithAgent(context.Background(), nil, ReviewCriterion{Name: "q"}); !errors.Is(err, ErrValidatorNilInput) {
		t.Fatalf("nil job: %v", err)
	}
	if _, err := validateWithAgent(context.Background(), &JobContext{Config: &JobConfig{}}, ReviewCriterion{Name: "q"}); !errors.Is(err, ErrValidatorNilInput) {
		t.Fatalf("nil service: %v", err)
	}
}

func TestBuildValidationPrompt(t *testing.T) {
	t.Parallel()
	prompt := buildValidationPrompt(workflowPrompts, ReviewCriterion{Name: "c", Config: "cfg"}, []*TaskResult{{Output: "out-1"}, nil, {Output: "out-2"}})
	for _, want := range []string{"criterion: c", "Config: cfg", "--- Artifact 1 ---", "out-1", "--- Artifact 3 ---", "out-2", `{"passed": bool`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "--- Artifact 2 ---") {
		t.Error("a nil artifact must be skipped")
	}
}
