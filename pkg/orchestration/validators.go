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
	"sort"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
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

	// ErrValidatorRegistration indicates a validator could not be registered.
	ErrValidatorRegistration = errors.New("validator: registration refused")

	// ErrValidatorAlreadyRegistered indicates a validator kind is taken.
	ErrValidatorAlreadyRegistered = errors.New("validator: already registered")
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

// ValidatorRegistrationError reports a registration the registry refused for
// a reason other than a duplicate kind.
type ValidatorRegistrationError struct {
	Kind    string
	Message string
}

// Error implements the error interface.
func (e *ValidatorRegistrationError) Error() string {
	if e.Kind == "" {
		return ErrValidatorRegistration.Error() + ": " + e.Message
	}
	return ErrValidatorRegistration.Error() + ": " + e.Kind + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ValidatorRegistrationError) Unwrap() error {
	return ErrValidatorRegistration
}

// ValidatorAlreadyRegisteredError reports a second registration under a kind
// that is already taken.
type ValidatorAlreadyRegisteredError struct {
	Kind string
}

// Error implements the error interface.
func (e *ValidatorAlreadyRegisteredError) Error() string {
	return ErrValidatorAlreadyRegistered.Error() + ": " + e.Kind
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ValidatorAlreadyRegisteredError) Unwrap() error {
	return ErrValidatorAlreadyRegistered
}

// UnknownValidatorError reports a workflow criterion naming a validator kind
// nothing registered. It is a workflow validation failure.
type UnknownValidatorError struct {
	Kind string
}

// Error implements the error interface.
func (e *UnknownValidatorError) Error() string {
	return ErrWorkflowValidation.Error() + ": review_criteria: no registered validator of kind " + e.Kind
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownValidatorError) Unwrap() error {
	return ErrWorkflowValidation
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

// Validator judges work outputs against a review criterion. It is the
// artifact-scoped shape: a pure judgement over what the work produced, with
// no view of the job. Register one through ArtifactValidator.
type Validator interface {
	// Validate evaluates artifacts against the given criterion and returns a result.
	Validate(ctx context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error)
}

// ValidatorFunc is what the registry holds: a judgement over a running job.
// It sees the job through JobContext — its artifacts, its prepared
// directory, its configuration and the operator's command guard — and never
// the loop. A module registers one per kind with RegisterValidator.
type ValidatorFunc func(ctx context.Context, job *JobContext, criterion ReviewCriterion) (ValidationResult, error)

// ArtifactValidator adapts an artifact-scoped Validator to the registry's
// job-aware shape: the job's artifacts are what it judges.
func ArtifactValidator(v Validator) ValidatorFunc {
	return func(ctx context.Context, job *JobContext, criterion ReviewCriterion) (ValidationResult, error) {
		if v == nil {
			return ValidationResult{Name: criterion.Name, Passed: false}, &ValidatorExecutionError{
				Validator: criterion.Validator,
				Cause:     ErrValidatorNilInput,
			}
		}
		var artifacts []*TaskResult
		if job != nil {
			artifacts = job.Artifacts
		}
		return v.Validate(ctx, criterion, artifacts)
	}
}

// =============================================================================
// Validator Registry
// =============================================================================
//
// ReviewCriterion.Validator names a kind; the registry maps kinds to the code
// that judges them. The engine registers its three built-ins at init and a
// module registers its own the same way, so a workflow can name a criterion
// the engine has never heard of and still be validated at registration:
// an unknown kind is refused there, not discovered at review time.

// ValidatorRegistry holds validators by kind and runs a criteria pipeline.
type ValidatorRegistry struct {
	mu         sync.RWMutex
	validators map[string]ValidatorFunc
}

// NewValidatorRegistry creates an empty registry.
func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{validators: make(map[string]ValidatorFunc)}
}

// Register adds a validator under kind. Registration is append-only: a kind
// registers once, and a second registration under the same kind is refused
// so two modules cannot silently shadow each other.
func (r *ValidatorRegistry) Register(kind string, fn ValidatorFunc) error {
	if kind == "" {
		return &ValidatorRegistrationError{Message: "kind must not be empty"}
	}
	if fn == nil {
		return &ValidatorRegistrationError{Kind: kind, Message: "validator must not be nil"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.validators[kind]; exists {
		return &ValidatorAlreadyRegisteredError{Kind: kind}
	}
	r.validators[kind] = fn
	return nil
}

// Get retrieves a validator by kind. Returns a ValidatorNotFoundError if the
// kind is not registered.
func (r *ValidatorRegistry) Get(kind string) (ValidatorFunc, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.validators[kind]
	if !ok {
		return nil, &ValidatorNotFoundError{Name: kind}
	}
	return v, nil
}

// Kinds returns every registered kind, sorted.
func (r *ValidatorRegistry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	kinds := make([]string, 0, len(r.validators))
	for kind := range r.validators {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// RunPipeline executes every criterion in order against the job. It returns
// the individual results, whether all REQUIRED criteria passed, and any
// execution error. Optional criteria that fail do not affect the overall
// verdict.
func (r *ValidatorRegistry) RunPipeline(ctx context.Context, job *JobContext, criteria []ReviewCriterion) ([]ValidationResult, bool, error) {
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

		result, execErr := v(ctx, job, criterion)
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

// Built-in validator kinds.
const (
	// ValidatorKindAgent prompts an agent to judge the artifacts.
	ValidatorKindAgent = "agent"

	// ValidatorKindCommand runs an operator-allowlisted command in the job's
	// prepared directory, ON THE DAEMON. It judges a job whose work is
	// there — a lead-driven workflow's prepared directory. A job whose work
	// happens inside a worker's container is judged where the work is,
	// through JobContext.Container, by the kind the module that owns that
	// job registers.
	ValidatorKindCommand = "command"

	// ValidatorKindPolicy scans the artifacts against a named policy.
	ValidatorKindPolicy = "policy"
)

// validators is the engine's registry: the one a module registers into and
// the one every control loop judges criteria through.
var validators = newBuiltinValidatorRegistry()

// newBuiltinValidatorRegistry builds a registry carrying the three built-in
// kinds.
func newBuiltinValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{validators: map[string]ValidatorFunc{
		ValidatorKindAgent:   validateWithAgent,
		ValidatorKindCommand: validateWithCommand,
		ValidatorKindPolicy:  ArtifactValidator(NewPolicyValidator()),
	}}
}

// RegisterValidator registers fn under kind in the engine's registry so a
// workflow criterion may name it.
func RegisterValidator(kind string, fn ValidatorFunc) error {
	return validators.Register(kind, fn)
}

// ValidateValidatorKind checks that kind names a registered validator. It is
// what workflow validation calls for every ReviewCriterion.
func ValidateValidatorKind(kind string) error {
	if _, err := validators.Get(kind); err != nil {
		return &UnknownValidatorError{Kind: kind}
	}
	return nil
}

// DefaultValidatorRegistry returns the engine's registry.
func DefaultValidatorRegistry() *ValidatorRegistry {
	return validators
}

// =============================================================================
// Agent validator
// =============================================================================

// agentReviewResponse is the expected JSON response from an agent review prompt.
type agentReviewResponse struct {
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

// buildValidationPrompt assembles what an agent validator is asked to judge:
// the criterion, and every artifact the work produced.
func buildValidationPrompt(criterion ReviewCriterion, artifacts []*TaskResult) string {
	var rendered strings.Builder
	for i, a := range artifacts {
		if a == nil {
			continue
		}
		rendered.WriteString(workflowPrompts.MustRender("validation_artifact", prompts.Vars{
			"index":  itoa(i + 1),
			"output": a.Output,
		}))
		rendered.WriteString("\n")
	}
	return workflowPrompts.MustRender("validation_prompt", prompts.Vars{
		"criterion": criterion.Name,
		"config":    criterion.Config,
		"artifacts": rendered.String(),
	})
}

// validateWithAgent prompts the job's lead backend to evaluate the artifacts
// against the criterion and parses the JSON verdict.
func validateWithAgent(ctx context.Context, job *JobContext, criterion ReviewCriterion) (ValidationResult, error) {
	if job == nil || job.Agents == nil || job.Config == nil {
		return ValidationResult{Name: criterion.Name, Passed: false}, &ValidatorExecutionError{
			Validator: ValidatorKindAgent,
			Cause:     ErrValidatorNilInput,
		}
	}
	prompt := buildValidationPrompt(criterion, job.Artifacts)

	resp, err := job.Agents.PromptSession(ctx, &PromptSessionRequest{
		Backend: job.Config.LeadBackend,
		Model:   job.Config.LeadModel,
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
			Validator: ValidatorKindAgent,
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
// Command validator
// =============================================================================

// validateWithCommand runs the criterion config as a shell command in the
// job's prepared directory, on the DAEMON, and checks the exit code.
//
// The command is tenant data bound for the host shell, so it is checked
// against the operator's ExecutionGuard first and a command the allowlist
// does not carry is refused with a typed error rather than run.
func validateWithCommand(ctx context.Context, job *JobContext, criterion ReviewCriterion) (ValidationResult, error) {
	if job == nil {
		return ValidationResult{Name: criterion.Name, Passed: false}, &ValidatorExecutionError{
			Validator: ValidatorKindCommand,
			Cause:     ErrValidatorNilInput,
		}
	}
	if criterion.Config == "" {
		return ValidationResult{
			Name:    criterion.Name,
			Passed:  false,
			Message: "no command configured",
		}, &ValidatorExecutionError{
			Validator: ValidatorKindCommand,
			Cause:     &InputValidationError{Field: "config", Message: "command is required"},
		}
	}
	if err := job.Guard.CheckCommand(criterion.Name, criterion.Config); err != nil {
		return ValidationResult{
			Name:    criterion.Name,
			Passed:  false,
			Message: err.Error(),
		}, err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "sh", "-c", criterion.Config)
	cmd.Dir = job.WorkDir
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
			Validator: ValidatorKindCommand,
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
// Policy validator
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
// using regex-based pattern scanning. It is the artifact-scoped shape; the
// registered "policy" kind is ArtifactValidator(NewPolicyValidator()).
type PolicyValidator struct{}

// NewPolicyValidator creates a new PolicyValidator.
func NewPolicyValidator() *PolicyValidator {
	return &PolicyValidator{}
}

// Validate checks the artifacts against the policy named in the criterion
// config. An unknown policy falls back to the generic secret scan.
func (v *PolicyValidator) Validate(_ context.Context, criterion ReviewCriterion, artifacts []*TaskResult) (ValidationResult, error) {
	handler, ok := policyHandlers[criterion.Config]
	if !ok {
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
