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
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// =============================================================================
// Sentinel Errors
// =============================================================================

var (
	// ErrValidatorNotFound indicates the requested validator type is not registered.
	ErrValidatorNotFound = errors.New("validator: not found")

	// ErrValidatorExecution indicates a validator failed during execution.
	ErrValidatorExecution = errors.New("validator: execution failed")

	// ErrValidatorPipeline indicates a validation pipeline failure.
	ErrValidatorPipeline = errors.New("validator: pipeline failed")

	// ErrValidatorNilInput indicates a nil input was provided to a validator.
	ErrValidatorNilInput = errors.New("validator: nil input")
)

// =============================================================================
// Typed Errors
// =============================================================================

// ValidatorNotFoundError indicates the requested validator type is not registered.
type ValidatorNotFoundError struct {
	Name string
}

// Error implements the error interface.
func (e *ValidatorNotFoundError) Error() string {
	return ErrValidatorNotFound.Error() + ": " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ValidatorNotFoundError) Unwrap() error {
	return ErrValidatorNotFound
}

// ValidatorExecutionError indicates a validator failed during execution.
type ValidatorExecutionError struct {
	Validator string
	Cause     error
}

// Error implements the error interface.
func (e *ValidatorExecutionError) Error() string {
	return ErrValidatorExecution.Error() + ": " + e.Validator + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *ValidatorExecutionError) Unwrap() []error {
	return []error{ErrValidatorExecution, e.Cause}
}

// ValidatorPipelineError indicates a required validation criterion failed.
type ValidatorPipelineError struct {
	Criterion string
	Message   string
}

// Error implements the error interface.
func (e *ValidatorPipelineError) Error() string {
	return ErrValidatorPipeline.Error() + ": " + e.Criterion + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ValidatorPipelineError) Unwrap() error {
	return ErrValidatorPipeline
}

// =============================================================================
// Validator Interface and Result
// =============================================================================

// ValidationResult represents the outcome of a single validation check.
type ValidationResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
	Details string `json:"details,omitempty"`
}

// Validator checks work outputs against a review criterion.
type Validator interface {
	// Validate evaluates artifacts against the given criterion and returns a result.
	Validate(ctx context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error)
}

// =============================================================================
// Validator Registry
// =============================================================================

// ValidatorRegistry holds registered validators by type name and provides
// pipeline execution for running all criteria validators in sequence.
type ValidatorRegistry struct {
	validators map[string]Validator
}

// NewValidatorRegistry creates a new ValidatorRegistry with an empty validator map.
func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{
		validators: make(map[string]Validator),
	}
}

// Register adds a validator to the registry under the given name.
// If a validator with the same name already exists, it is replaced.
func (r *ValidatorRegistry) Register(name string, v Validator) {
	r.validators[name] = v
}

// Get retrieves a validator by name. Returns a ValidatorNotFoundError if the
// validator type is not registered.
func (r *ValidatorRegistry) Get(name string) (Validator, error) {
	v, ok := r.validators[name]
	if !ok {
		return nil, &ValidatorNotFoundError{Name: name}
	}
	return v, nil
}

// RunPipeline executes all criteria validators in order. It returns the
// individual validation results, a boolean indicating whether all required
// criteria passed, and any execution error encountered. Optional criteria
// that fail do not affect the overall pass/fail status.
func (r *ValidatorRegistry) RunPipeline(ctx context.Context, criteria []ReviewCriterion, artifacts []*TaskResult) ([]ValidationResult, bool, error) {
	if len(criteria) == 0 {
		return nil, true, nil
	}

	results := make([]ValidationResult, 0, len(criteria))
	allRequiredPassed := true

	for _, criterion := range criteria {
		if err := ctx.Err(); err != nil {
			return results, false, err
		}

		v, vErr := r.Get(criterion.Validator)
		if vErr != nil {
			return results, false, vErr
		}

		result, execErr := v.Validate(ctx, criterion, artifacts)
		if execErr != nil {
			return results, false, &ValidatorExecutionError{
				Validator: criterion.Validator,
				Cause:     execErr,
			}
		}

		results = append(results, result)

		if criterion.Required && !result.Passed {
			allRequiredPassed = false
		}
	}

	return results, allRequiredPassed, nil
}

// =============================================================================
// AgentValidator
// =============================================================================

// AgentValidator prompts an agent session to review artifacts against a criterion.
// It sends a structured review prompt to the lead agent and parses the JSON response.
type AgentValidator struct {
	agentService AgentService
	backend      string
	model        string
}

// NewAgentValidator creates an AgentValidator that uses the given AgentService
// to prompt agents for review evaluations.
func NewAgentValidator(agentService AgentService, backend, model string) *AgentValidator {
	return &AgentValidator{
		agentService: agentService,
		backend:      backend,
		model:        model,
	}
}

// agentReviewResponse is the expected JSON response from an agent review prompt.
type agentReviewResponse struct {
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

// buildValidationPrompt constructs the prompt sent to the agent for evaluation.
func buildValidationPrompt(criterion ReviewCriterion, artifacts []*TaskResult) string {
	var sb strings.Builder
	sb.WriteString("Review the following artifacts against criterion: ")
	sb.WriteString(criterion.Name)
	sb.WriteString(". Config: ")
	sb.WriteString(criterion.Config)
	sb.WriteString(". Artifacts:\n")
	for i, a := range artifacts {
		if a == nil {
			continue
		}
		sb.WriteString("--- Artifact ")
		sb.WriteString(itoa(i + 1))
		sb.WriteString(" ---\n")
		sb.WriteString(a.Output)
		sb.WriteString("\n")
	}
	sb.WriteString("\nRespond with ONLY valid JSON: {\"passed\": bool, \"message\": \"string\"}")
	return sb.String()
}

// Validate prompts the configured agent to evaluate artifacts against the criterion.
func (v *AgentValidator) Validate(ctx context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error) {
	prompt := buildValidationPrompt(criterion, artifacts)

	resp, err := v.agentService.PromptSession(ctx, &PromptSessionRequest{
		Backend: v.backend,
		Model:   v.model,
		Prompt:  prompt,
	})
	if err != nil {
		return ValidationResult{
			Name:   criterion.Name,
			Passed: false,
		}, err
	}

	// Extract text content from the response.
	var responseText strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			responseText.WriteString(block.Text)
		}
	}

	var parsed agentReviewResponse
	if jsonErr := json.Unmarshal([]byte(responseText.String()), &parsed); jsonErr != nil {
		return ValidationResult{
				Name:    criterion.Name,
				Passed:  false,
				Message: "failed to parse agent response",
				Details: responseText.String(),
			}, &ValidatorExecutionError{
				Validator: "agent",
				Cause:     jsonErr,
			}
	}

	return ValidationResult{
		Name:    criterion.Name,
		Passed:  parsed.Passed,
		Message: parsed.Message,
	}, nil
}

// =============================================================================
// CommandValidator
// =============================================================================

// CommandValidator executes a shell command from the criterion config and
// checks the exit code. Exit code 0 means passed; non-zero means failed
// with stderr captured as the failure message.
type CommandValidator struct{}

// NewCommandValidator creates a new CommandValidator.
func NewCommandValidator() *CommandValidator {
	return &CommandValidator{}
}

// Validate runs the criterion config as a shell command using exec.CommandContext
// for timeout support. Exit 0 indicates pass; non-zero indicates failure.
func (v *CommandValidator) Validate(ctx context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error) {
	if criterion.Config == "" {
		return ValidationResult{
				Name:    criterion.Name,
				Passed:  false,
				Message: "no command configured",
			}, &ValidatorExecutionError{
				Validator: "command",
				Cause:     &InputValidationError{Field: "config", Message: "command is required"},
			}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "sh", "-c", criterion.Config)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return ValidationResult{
				Name:    criterion.Name,
				Passed:  false,
				Message: strings.TrimSpace(stderr.String()),
				Details: "exit code: " + itoa(exitErr.ExitCode()),
			}, nil
		}
		// Non-exit errors (command not found, context deadline, etc.)
		return ValidationResult{
				Name:   criterion.Name,
				Passed: false,
			}, &ValidatorExecutionError{
				Validator: "command",
				Cause:     err,
			}
	}

	return ValidationResult{
		Name:    criterion.Name,
		Passed:  true,
		Message: strings.TrimSpace(stdout.String()),
	}, nil
}

// =============================================================================
// PolicyValidator
// =============================================================================

// secretPatterns contains compiled regular expressions for detecting common
// secret patterns in output text. These cover API keys, tokens, private keys,
// passwords, and AWS credentials.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:api[_-]?key|apikey)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)(?:secret[_-]?key|secretkey)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)(?:access[_-]?token|accesstoken)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)(?:private[_-]?key|privatekey)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)password\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)aws[_-]?secret[_-]?access[_-]?key\s*[:=]\s*\S+`),
	regexp.MustCompile(`-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----`),
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9._\-]+`),
}

// policyHandlers maps policy names to their handler functions for O(1) dispatch.
var policyHandlers = map[string]func(artifacts []*TaskResult) ValidationResult{
	"no secrets in output": checkNoSecrets,
}

// PolicyValidator evaluates output artifacts against named security policies
// using regex-based pattern scanning.
type PolicyValidator struct{}

// NewPolicyValidator creates a new PolicyValidator.
func NewPolicyValidator() *PolicyValidator {
	return &PolicyValidator{}
}

// Validate checks the artifacts against the policy specified in the criterion config.
func (v *PolicyValidator) Validate(ctx context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error) {
	handler, ok := policyHandlers[criterion.Config]
	if !ok {
		// Fall back to generic secret scan for unknown policies.
		result := checkNoSecrets(artifacts)
		result.Name = criterion.Name
		return result, nil
	}

	result := handler(artifacts)
	result.Name = criterion.Name
	return result, nil
}

// checkNoSecrets scans all artifact outputs for secret patterns.
func checkNoSecrets(artifacts []*TaskResult) ValidationResult {
	var matches []string
	for _, a := range artifacts {
		if a == nil {
			continue
		}
		for _, pattern := range secretPatterns {
			found := pattern.FindAllString(a.Output, -1)
			matches = append(matches, found...)
		}
	}

	if len(matches) > 0 {
		return ValidationResult{
			Passed:  false,
			Message: "secret patterns detected in output",
			Details: "found " + itoa(len(matches)) + " potential secret(s)",
		}
	}

	return ValidationResult{
		Passed:  true,
		Message: "no secret patterns detected",
	}
}
