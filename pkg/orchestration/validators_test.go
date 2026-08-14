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
// Mock AgentService for AgentValidator tests
// =============================================================================

// mockAgentServiceForValidator implements the AgentService interface with
// configurable PromptSession behavior for testing the AgentValidator.
type mockAgentServiceForValidator struct {
	AgentService
	promptResponse *PromptSessionResponse
	promptErr      error
	lastRequest    *PromptSessionRequest
}

func (m *mockAgentServiceForValidator) PromptSession(ctx context.Context, req *PromptSessionRequest) (*PromptSessionResponse, error) {
	m.lastRequest = req
	return m.promptResponse, m.promptErr
}

// =============================================================================
// Mock Validator for pipeline tests
// =============================================================================

type mockValidator struct {
	result ValidationResult
	err    error
}

func (m *mockValidator) Validate(_ context.Context, criterion ReviewCriterion, _ []*TaskResult) (ValidationResult, error) {
	result := m.result
	result.Name = criterion.Name
	return result, m.err
}

// =============================================================================
// Typed Error Tests
// =============================================================================

func TestValidatorNotFoundError(t *testing.T) {
	t.Parallel()

	err := &ValidatorNotFoundError{Name: "missing"}
	msg := err.Error()
	if !strings.Contains(msg, "missing") {
		t.Fatalf("expected error to contain validator name, got: %s", msg)
	}
	if !errors.Is(err, ErrValidatorNotFound) {
		t.Fatal("expected Unwrap to return ErrValidatorNotFound")
	}
}

func TestValidatorExecutionError(t *testing.T) {
	t.Parallel()

	cause := errors.New("timeout")
	err := &ValidatorExecutionError{Validator: "command", Cause: cause}
	msg := err.Error()
	if !strings.Contains(msg, "command") {
		t.Fatalf("expected error to contain validator name, got: %s", msg)
	}
	if !strings.Contains(msg, "timeout") {
		t.Fatalf("expected error to contain cause, got: %s", msg)
	}
	if !errors.Is(err, ErrValidatorExecution) {
		t.Fatal("expected Unwrap to return ErrValidatorExecution")
	}
	if !errors.Is(err, cause) {
		t.Fatal("expected Unwrap to return cause")
	}
}

func TestValidatorPipelineError(t *testing.T) {
	t.Parallel()

	err := &ValidatorPipelineError{Criterion: "code-quality", Message: "failed checks"}
	msg := err.Error()
	if !strings.Contains(msg, "code-quality") {
		t.Fatalf("expected error to contain criterion name, got: %s", msg)
	}
	if !strings.Contains(msg, "failed checks") {
		t.Fatalf("expected error to contain message, got: %s", msg)
	}
	if !errors.Is(err, ErrValidatorPipeline) {
		t.Fatal("expected Unwrap to return ErrValidatorPipeline")
	}
}

// =============================================================================
// ValidatorRegistry Tests
// =============================================================================

func TestValidatorRegistry_Register_Get(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	v := &mockValidator{result: ValidationResult{Passed: true}}
	reg.Register("test", v)

	got, err := reg.Get("test")
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if got != v {
		t.Fatal("Get returned different validator instance")
	}
}

func TestValidatorRegistry_Register_Overwrite(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	v1 := &mockValidator{result: ValidationResult{Passed: true}}
	v2 := &mockValidator{result: ValidationResult{Passed: false}}

	reg.Register("test", v1)
	reg.Register("test", v2)

	got, err := reg.Get("test")
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if got != v2 {
		t.Fatal("expected overwritten validator to be returned")
	}
}

func TestValidatorRegistry_Get_NotFound(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent validator")
	}
	if !errors.Is(err, ErrValidatorNotFound) {
		t.Fatalf("expected ErrValidatorNotFound, got: %v", err)
	}
	var nfe *ValidatorNotFoundError
	if !errors.As(err, &nfe) {
		t.Fatal("expected ValidatorNotFoundError type")
	}
	if nfe.Name != "nonexistent" {
		t.Fatalf("expected name 'nonexistent', got: %s", nfe.Name)
	}
}

func TestValidatorRegistry_RunPipeline_AllPass(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	reg.Register("check", &mockValidator{result: ValidationResult{Passed: true, Message: "ok"}})

	criteria := []ReviewCriterion{
		{Name: "check-1", Validator: "check", Required: true},
		{Name: "check-2", Validator: "check", Required: true},
	}

	results, allPassed, err := reg.RunPipeline(context.Background(), criteria, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allPassed {
		t.Fatal("expected all to pass")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Fatalf("expected result %q to pass", r.Name)
		}
	}
}

func TestValidatorRegistry_RunPipeline_RequiredFails(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	reg.Register("pass", &mockValidator{result: ValidationResult{Passed: true}})
	reg.Register("fail", &mockValidator{result: ValidationResult{Passed: false, Message: "not good"}})

	criteria := []ReviewCriterion{
		{Name: "first", Validator: "pass", Required: true},
		{Name: "second", Validator: "fail", Required: true},
	}

	results, allPassed, err := reg.RunPipeline(context.Background(), criteria, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allPassed {
		t.Fatal("expected pipeline to report failure when required criterion fails")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[1].Passed {
		t.Fatal("expected second result to fail")
	}
}

func TestValidatorRegistry_RunPipeline_OptionalFails(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	reg.Register("pass", &mockValidator{result: ValidationResult{Passed: true}})
	reg.Register("fail", &mockValidator{result: ValidationResult{Passed: false, Message: "optional issue"}})

	criteria := []ReviewCriterion{
		{Name: "required-check", Validator: "pass", Required: true},
		{Name: "optional-check", Validator: "fail", Required: false},
	}

	results, allPassed, err := reg.RunPipeline(context.Background(), criteria, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allPassed {
		t.Fatal("expected pipeline to pass when only optional criteria fail")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestValidatorRegistry_RunPipeline_Empty(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	results, allPassed, err := reg.RunPipeline(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allPassed {
		t.Fatal("expected empty pipeline to pass")
	}
	if results != nil {
		t.Fatal("expected nil results for empty pipeline")
	}
}

func TestValidatorRegistry_RunPipeline_UnknownValidator(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	criteria := []ReviewCriterion{
		{Name: "check", Validator: "unknown", Required: true},
	}

	_, _, err := reg.RunPipeline(context.Background(), criteria, nil)
	if err == nil {
		t.Fatal("expected error for unknown validator")
	}
	if !errors.Is(err, ErrValidatorNotFound) {
		t.Fatalf("expected ErrValidatorNotFound, got: %v", err)
	}
}

func TestValidatorRegistry_RunPipeline_ExecutionError(t *testing.T) {
	t.Parallel()

	execErr := errors.New("agent unavailable")
	reg := NewValidatorRegistry()
	reg.Register("broken", &mockValidator{err: execErr})

	criteria := []ReviewCriterion{
		{Name: "check", Validator: "broken", Required: true},
	}

	_, _, err := reg.RunPipeline(context.Background(), criteria, nil)
	if err == nil {
		t.Fatal("expected error for execution failure")
	}
	if !errors.Is(err, ErrValidatorExecution) {
		t.Fatalf("expected ErrValidatorExecution, got: %v", err)
	}
}

func TestValidatorRegistry_RunPipeline_ContextCancelled(t *testing.T) {
	t.Parallel()

	reg := NewValidatorRegistry()
	reg.Register("check", &mockValidator{result: ValidationResult{Passed: true}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	criteria := []ReviewCriterion{
		{Name: "check", Validator: "check", Required: true},
	}

	_, allPassed, err := reg.RunPipeline(ctx, criteria, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if allPassed {
		t.Fatal("expected allPassed to be false on cancellation")
	}
}

// =============================================================================
// CommandValidator Tests
// =============================================================================

func TestCommandValidator_Success(t *testing.T) {
	t.Parallel()

	v := NewCommandValidator()
	criterion := ReviewCriterion{
		Name:      "true-check",
		Validator: "command",
		Config:    "true",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected true command to pass")
	}
	if result.Name != "true-check" {
		t.Fatalf("expected name 'true-check', got: %s", result.Name)
	}
}

func TestCommandValidator_Failure(t *testing.T) {
	t.Parallel()

	v := NewCommandValidator()
	criterion := ReviewCriterion{
		Name:      "false-check",
		Validator: "command",
		Config:    "echo 'something went wrong' >&2 && false",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected false command to fail")
	}
	if result.Message != "something went wrong" {
		t.Fatalf("expected stderr message, got: %q", result.Message)
	}
	if result.Details != "exit code: 1" {
		t.Fatalf("expected exit code detail, got: %q", result.Details)
	}
}

func TestCommandValidator_EmptyConfig(t *testing.T) {
	t.Parallel()

	v := NewCommandValidator()
	criterion := ReviewCriterion{
		Name:      "empty-cmd",
		Validator: "command",
		Config:    "",
		Required:  true,
	}

	_, err := v.Validate(context.Background(), criterion, nil)
	if err == nil {
		t.Fatal("expected error for empty config")
	}
	if !errors.Is(err, ErrValidatorExecution) {
		t.Fatalf("expected ErrValidatorExecution, got: %v", err)
	}
}

func TestCommandValidator_WithStdout(t *testing.T) {
	t.Parallel()

	v := NewCommandValidator()
	criterion := ReviewCriterion{
		Name:      "echo-check",
		Validator: "command",
		Config:    "echo 'all good'",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected echo command to pass")
	}
	if result.Message != "all good" {
		t.Fatalf("expected stdout message 'all good', got: %q", result.Message)
	}
}

func TestCommandValidator_ContextTimeout(t *testing.T) {
	t.Parallel()

	v := NewCommandValidator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	criterion := ReviewCriterion{
		Name:      "timeout-check",
		Validator: "command",
		Config:    "echo hello",
		Required:  true,
	}

	_, err := v.Validate(ctx, criterion, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !errors.Is(err, ErrValidatorExecution) {
		t.Fatalf("expected ErrValidatorExecution, got: %v", err)
	}
}

// =============================================================================
// PolicyValidator Tests
// =============================================================================

func TestPolicyValidator_NoSecrets(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "This is perfectly clean output with no secrets."},
		{Output: "Just some normal code: func main() { fmt.Println(\"hello\") }"},
	}

	criterion := ReviewCriterion{
		Name:      "secret-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected clean output to pass, message: %s", result.Message)
	}
	if result.Name != "secret-scan" {
		t.Fatalf("expected name 'secret-scan', got: %s", result.Name)
	}
}

func TestPolicyValidator_SecretsFound_APIKey(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "api_key=sk_live_abc123def456"},
	}

	criterion := ReviewCriterion{
		Name:      "secret-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected API key to be detected")
	}
	if result.Message != "secret patterns detected in output" {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

func TestPolicyValidator_SecretsFound_PrivateKey(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "-----BEGIN PRIVATE KEY-----\nMIIEvgIBADANBg...\n-----END PRIVATE KEY-----"},
	}

	criterion := ReviewCriterion{
		Name:      "pkey-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected private key to be detected")
	}
}

func TestPolicyValidator_SecretsFound_RSAPrivateKey(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "-----BEGIN RSA PRIVATE KEY-----\nbase64data\n-----END RSA PRIVATE KEY-----"},
	}

	criterion := ReviewCriterion{
		Name:      "rsa-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected RSA private key to be detected")
	}
}

func TestPolicyValidator_SecretsFound_BearerToken(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0"},
	}

	criterion := ReviewCriterion{
		Name:      "bearer-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected bearer token to be detected")
	}
}

func TestPolicyValidator_SecretsFound_Password(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "password=supersecret123"},
	}

	criterion := ReviewCriterion{
		Name:      "password-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected password to be detected")
	}
}

func TestPolicyValidator_SecretsFound_AWSKey(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"},
	}

	criterion := ReviewCriterion{
		Name:      "aws-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected AWS secret key to be detected")
	}
}

func TestPolicyValidator_NilArtifacts(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	criterion := ReviewCriterion{
		Name:      "nil-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected nil artifacts to pass")
	}
}

func TestPolicyValidator_NilArtifactEntries(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{nil, nil}

	criterion := ReviewCriterion{
		Name:      "nil-entries-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected nil artifact entries to pass")
	}
}

func TestPolicyValidator_UnknownPolicyFallback(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "clean output"},
	}

	criterion := ReviewCriterion{
		Name:      "unknown-policy",
		Validator: "policy",
		Config:    "some unknown policy",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Falls back to secret scan which should pass on clean output.
	if !result.Passed {
		t.Fatal("expected unknown policy fallback to pass on clean output")
	}
}

func TestPolicyValidator_MultipleArtifactsSecretInSecond(t *testing.T) {
	t.Parallel()

	v := NewPolicyValidator()
	artifacts := []*TaskResult{
		{Output: "clean output"},
		{Output: "secret_key=abc123"},
	}

	criterion := ReviewCriterion{
		Name:      "multi-scan",
		Validator: "policy",
		Config:    "no secrets in output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected secret in second artifact to be detected")
	}
}

// =============================================================================
// AgentValidator Tests
// =============================================================================

func TestAgentValidator_PromptConstruction(t *testing.T) {
	t.Parallel()

	mock := &mockAgentServiceForValidator{
		promptResponse: &PromptSessionResponse{
			Content: []ContentBlock{
				{Type: "text", Text: `{"passed": true, "message": "looks good"}`},
			},
		},
	}

	v := NewAgentValidator(mock, "ollama", "llama3")
	criterion := ReviewCriterion{
		Name:      "code-quality",
		Validator: "agent",
		Config:    "check for proper error handling",
		Required:  true,
	}
	artifacts := []*TaskResult{
		{Output: "func main() { err := doSomething(); if err != nil { log.Fatal(err) } }"},
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected agent validation to pass")
	}
	if result.Message != "looks good" {
		t.Fatalf("expected message 'looks good', got: %q", result.Message)
	}

	// Verify the prompt was constructed correctly.
	req := mock.lastRequest
	if req.Backend != "ollama" {
		t.Fatalf("expected backend 'ollama', got: %s", req.Backend)
	}
	if req.Model != "llama3" {
		t.Fatalf("expected model 'llama3', got: %s", req.Model)
	}
	if req.Prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
}

func TestAgentValidator_PromptError(t *testing.T) {
	t.Parallel()

	promptErr := &PromptError{SessionID: "test", Cause: errors.New("connection refused")}
	mock := &mockAgentServiceForValidator{
		promptErr: promptErr,
	}

	v := NewAgentValidator(mock, "ollama", "llama3")
	criterion := ReviewCriterion{
		Name:      "review",
		Validator: "agent",
		Config:    "check output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, nil)
	if err == nil {
		t.Fatal("expected error from agent prompt failure")
	}
	if !errors.Is(err, ErrPromptFailed) {
		t.Fatalf("expected ErrPromptFailed, got: %v", err)
	}
	if result.Passed {
		t.Fatal("expected failed result")
	}
}

func TestAgentValidator_InvalidJSON(t *testing.T) {
	t.Parallel()

	mock := &mockAgentServiceForValidator{
		promptResponse: &PromptSessionResponse{
			Content: []ContentBlock{
				{Type: "text", Text: "this is not valid json"},
			},
		},
	}

	v := NewAgentValidator(mock, "ollama", "llama3")
	criterion := ReviewCriterion{
		Name:      "review",
		Validator: "agent",
		Config:    "check output",
		Required:  true,
	}

	result, err := v.Validate(context.Background(), criterion, nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !errors.Is(err, ErrValidatorExecution) {
		t.Fatalf("expected ErrValidatorExecution, got: %v", err)
	}
	if result.Passed {
		t.Fatal("expected failed result for invalid JSON")
	}
	if result.Details != "this is not valid json" {
		t.Fatalf("expected raw response in details, got: %q", result.Details)
	}
}

func TestAgentValidator_AgentRejectsCriterion(t *testing.T) {
	t.Parallel()

	mock := &mockAgentServiceForValidator{
		promptResponse: &PromptSessionResponse{
			Content: []ContentBlock{
				{Type: "text", Text: `{"passed": false, "message": "missing error handling in 3 functions"}`},
			},
		},
	}

	v := NewAgentValidator(mock, "ollama", "llama3")
	criterion := ReviewCriterion{
		Name:      "error-handling",
		Validator: "agent",
		Config:    "all errors must be handled",
		Required:  true,
	}
	artifacts := []*TaskResult{
		{Output: "func bad() { doThing() }"},
	}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Fatal("expected agent to reject criterion")
	}
	if result.Message != "missing error handling in 3 functions" {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

func TestAgentValidator_NilArtifactsInPrompt(t *testing.T) {
	t.Parallel()

	mock := &mockAgentServiceForValidator{
		promptResponse: &PromptSessionResponse{
			Content: []ContentBlock{
				{Type: "text", Text: `{"passed": true, "message": "ok"}`},
			},
		},
	}

	v := NewAgentValidator(mock, "ollama", "llama3")
	criterion := ReviewCriterion{
		Name:      "nil-check",
		Validator: "agent",
		Config:    "check",
		Required:  true,
	}
	artifacts := []*TaskResult{nil, {Output: "valid"}, nil}

	result, err := v.Validate(context.Background(), criterion, artifacts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Fatal("expected pass")
	}
}

// =============================================================================
// buildValidationPrompt Tests
// =============================================================================

func TestBuildValidationPrompt(t *testing.T) {
	t.Parallel()

	criterion := ReviewCriterion{
		Name:   "quality",
		Config: "check quality",
	}
	artifacts := []*TaskResult{
		{Output: "artifact one"},
		{Output: "artifact two"},
	}

	prompt := buildValidationPrompt(criterion, artifacts)

	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}

	// Verify key components are present.
	checks := []string{
		"criterion: quality",
		"Config: check quality",
		"Artifact 1",
		"artifact one",
		"Artifact 2",
		"artifact two",
		"Respond with ONLY valid JSON",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Fatalf("expected prompt to contain %q", check)
		}
	}
}

func TestBuildValidationPrompt_NilArtifactSkipped(t *testing.T) {
	t.Parallel()

	criterion := ReviewCriterion{
		Name:   "test",
		Config: "cfg",
	}
	artifacts := []*TaskResult{nil, {Output: "data"}}

	prompt := buildValidationPrompt(criterion, artifacts)

	// Should contain Artifact 2 but not crash on nil.
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "Artifact 2") {
		t.Fatal("expected prompt to contain Artifact 2")
	}
	if strings.Contains(prompt, "Artifact 1") {
		t.Fatal("expected nil artifact 1 to be skipped")
	}
}

// PromptSessionAsync mirrors the production contract: the turn is acknowledged
// immediately and its output would flow to the session event log.
func (m *mockAgentServiceForValidator) PromptSessionAsync(ctx context.Context, req *PromptSessionRequest) (*PromptTurnAck, error) {
	return &PromptTurnAck{
		SessionID: req.SessionID,
		TurnID:    "turn-test",
		Status:    TurnStatusAccepted,
		Sequence:  1,
	}, nil
}
