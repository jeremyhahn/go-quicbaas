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

// Package iam provides the IAM client for the QuicBaaS SDK.
//
// This package implements ABAC (Attribute-Based Access Control) policy
// management and policy enforcement point (PEP) operations.
package iam

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	iamcore "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// PolicyVersionUnsupportedError is returned by Update when the request
// carries a non-zero Version. Policies are not versioned, so the value is
// refused before it is sent, on every transport. It is the same type the
// server returns for a version sent over REST or MCP.
type PolicyVersionUnsupportedError = iamcore.PolicyVersionUnsupportedError

const (
	policyServiceName = "quicbaas.IAM.Policy"
)

// PolicyClient provides ABAC policy management operations.
//
// The client supports creating, updating, deleting, and testing
// ABAC policies for fine-grained access control.
type PolicyClient interface {
	// Create creates a new ABAC policy.
	Create(ctx context.Context, tenantID string, req *CreatePolicyRequest) (*Policy, error)

	// Get retrieves a policy by name.
	Get(ctx context.Context, tenantID, policyName string) (*Policy, error)

	// Update updates an existing policy.
	Update(ctx context.Context, tenantID, policyName string, req *UpdatePolicyRequest) (*Policy, error)

	// Delete removes a policy.
	Delete(ctx context.Context, tenantID, policyName string) error

	// List lists policies matching the filter criteria.
	List(ctx context.Context, tenantID string, req *ListPoliciesRequest) (*ListPoliciesResponse, error)

	// Validate validates a policy without persisting it.
	Validate(ctx context.Context, tenantID string, policy *Policy) (*ValidationResult, error)

	// Test tests a policy against sample requests.
	Test(ctx context.Context, tenantID string, req *TestPolicyRequest) (*TestPolicyResponse, error)

	// Simulate simulates an authorization decision without side effects.
	Simulate(ctx context.Context, tenantID string, req *SimulateRequest) (*SimulateResponse, error)

	// GetEffectivePermissions retrieves the effective permissions for a subject.
	GetEffectivePermissions(ctx context.Context, tenantID string, req *EffectivePermissionsRequest) (*EffectivePermissionsResponse, error)
}

// Policy represents an ABAC policy definition.
type Policy struct {
	// Name is the unique policy identifier within a tenant.
	Name string `json:"name"`

	// TenantID is the tenant this policy belongs to.
	TenantID string `json:"tenant_id"`

	// Description is a human-readable description of the policy.
	Description string `json:"description,omitempty"`

	// Version is always zero: policies are not versioned, and a non-zero
	// version sent to Update or to the server is refused with
	// PolicyVersionUnsupportedError.
	Version int64 `json:"version,omitempty"`

	// Enabled indicates if the policy is active.
	Enabled bool `json:"enabled,omitempty"`

	// Priority determines evaluation order (higher = evaluated first).
	Priority int `json:"priority,omitempty"`

	// Rules are the policy rules that define access control logic.
	Rules []Rule `json:"rules,omitempty"`

	// Targets define the scope of resources this policy applies to.
	Targets []Target `json:"targets,omitempty"`

	// Conditions are global conditions that must be met for the policy to apply.
	Conditions []Condition `json:"conditions,omitempty"`

	// Effect is the default effect when rules match (allow or deny).
	Effect Effect `json:"effect,omitempty"`

	// CreatedAt is when the policy was created.
	CreatedAt time.Time `json:"created_at,omitempty"`

	// UpdatedAt is when the policy was last updated.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// CreatedBy is the user who created the policy.
	CreatedBy string `json:"created_by,omitempty"`

	// UpdatedBy is the user who last updated the policy.
	UpdatedBy string `json:"updated_by,omitempty"`

	// Labels are arbitrary key-value pairs for organization.
	Labels map[string]string `json:"labels,omitempty"`

	// SpecYAML is the raw policy specification as YAML (optional).
	// Used for validation to pass the complete policy definition.
	SpecYAML []byte `json:"spec_yaml,omitempty"`

	// LoadError is set when the server holds this policy's stored record
	// but the record does not decode strictly, so the server never applies
	// it. Repair the record with Update (a corrected document) or Delete.
	LoadError *PolicyLoadError `json:"load_error,omitempty"`
}

// PolicyLoadError describes a stored policy record the server flagged and
// does not apply. It carries the reason and the unknown key paths, never
// the record's values.
type PolicyLoadError struct {
	// Reason is why the stored record did not decode.
	Reason string `json:"reason"`

	// UnknownKeys are the key paths the record carries that the policy
	// document does not define, e.g. "spec.rules[0].bogus".
	UnknownKeys []string `json:"unknown_keys,omitempty"`
}

// Rule defines an individual access control rule within a policy.
type Rule struct {
	// Name is the rule identifier.
	Name string `json:"name"`

	// Description is a human-readable description.
	Description string `json:"description,omitempty"`

	// Effect is the rule outcome (allow or deny).
	Effect Effect `json:"effect"`

	// Subjects define who the rule applies to.
	Subjects []SubjectMatcher `json:"subjects,omitempty"`

	// Actions define what operations the rule covers.
	Actions []ActionMatcher `json:"actions,omitempty"`

	// Resources define which resources the rule applies to.
	Resources []ResourceMatcher `json:"resources,omitempty"`

	// Conditions are conditions that must be met for the rule to apply.
	Conditions []Condition `json:"conditions,omitempty"`

	// Priority determines rule evaluation order within the policy.
	Priority int `json:"priority,omitempty"`
}

// Effect represents the outcome of a policy or rule evaluation.
type Effect int

const (
	// EffectDeny denies access.
	EffectDeny Effect = iota
	// EffectAllow allows access.
	EffectAllow
)

// String returns the string representation of the effect.
func (e Effect) String() string {
	switch e {
	case EffectAllow:
		return "ALLOW"
	case EffectDeny:
		return "DENY"
	default:
		return "UNKNOWN"
	}
}

// SubjectMatcher defines criteria for matching subjects (users/services).
type SubjectMatcher struct {
	// Type is the subject type (user, service, group, role).
	Type SubjectType `json:"type"`

	// ID is the specific subject identifier (optional).
	ID string `json:"id,omitempty"`

	// Attributes are attribute conditions for matching.
	Attributes map[string]AttributeMatcher `json:"attributes,omitempty"`
}

// SubjectType represents the type of subject.
type SubjectType int

const (
	// SubjectTypeUser represents a user subject.
	SubjectTypeUser SubjectType = iota
	// SubjectTypeService represents a service subject.
	SubjectTypeService
	// SubjectTypeGroup represents a group subject.
	SubjectTypeGroup
	// SubjectTypeRole represents a role subject.
	SubjectTypeRole
	// SubjectTypeAny matches any subject type.
	SubjectTypeAny
)

// String returns the string representation of the subject type.
func (t SubjectType) String() string {
	switch t {
	case SubjectTypeUser:
		return "USER"
	case SubjectTypeService:
		return "SERVICE"
	case SubjectTypeGroup:
		return "GROUP"
	case SubjectTypeRole:
		return "ROLE"
	case SubjectTypeAny:
		return "ANY"
	default:
		return "UNKNOWN"
	}
}

// ActionMatcher defines criteria for matching actions.
type ActionMatcher struct {
	// Pattern is the action pattern (supports wildcards).
	// Examples: "data:read", "user:*", "*:delete"
	Pattern string `json:"pattern"`

	// Negate inverts the match (matches if pattern does NOT match).
	Negate bool `json:"negate,omitempty"`
}

// ResourceMatcher defines criteria for matching resources.
type ResourceMatcher struct {
	// Pattern is the resource pattern (supports wildcards).
	// Examples: "orders://*", "users://123", "kv://config/*"
	Pattern string `json:"pattern"`

	// Type is the resource type filter.
	Type string `json:"type,omitempty"`

	// Attributes are attribute conditions for matching.
	Attributes map[string]AttributeMatcher `json:"attributes,omitempty"`

	// Negate inverts the match.
	Negate bool `json:"negate,omitempty"`
}

// AttributeMatcher defines criteria for matching an attribute value.
type AttributeMatcher struct {
	// Operator is the comparison operator.
	Operator ComparisonOperator `json:"operator"`

	// Value is the value to compare against.
	Value interface{} `json:"value,omitempty"`

	// Values is a list of values for IN/NOT_IN operators.
	Values []interface{} `json:"values,omitempty"`
}

// ComparisonOperator represents a comparison operation.
type ComparisonOperator int

const (
	// ComparisonEquals checks for equality.
	ComparisonEquals ComparisonOperator = iota
	// ComparisonNotEquals checks for inequality.
	ComparisonNotEquals
	// ComparisonGreaterThan checks if value is greater.
	ComparisonGreaterThan
	// ComparisonGreaterThanOrEqual checks if value is greater or equal.
	ComparisonGreaterThanOrEqual
	// ComparisonLessThan checks if value is less.
	ComparisonLessThan
	// ComparisonLessThanOrEqual checks if value is less or equal.
	ComparisonLessThanOrEqual
	// ComparisonIn checks if value is in a set.
	ComparisonIn
	// ComparisonNotIn checks if value is not in a set.
	ComparisonNotIn
	// ComparisonContains checks if value contains substring.
	ComparisonContains
	// ComparisonStartsWith checks if value starts with prefix.
	ComparisonStartsWith
	// ComparisonEndsWith checks if value ends with suffix.
	ComparisonEndsWith
	// ComparisonMatches checks if value matches regex pattern.
	ComparisonMatches
	// ComparisonExists checks if attribute exists.
	ComparisonExists
	// ComparisonNotExists checks if attribute does not exist.
	ComparisonNotExists
)

// String returns the string representation of the operator.
func (o ComparisonOperator) String() string {
	switch o {
	case ComparisonEquals:
		return "EQUALS"
	case ComparisonNotEquals:
		return "NOT_EQUALS"
	case ComparisonGreaterThan:
		return "GREATER_THAN"
	case ComparisonGreaterThanOrEqual:
		return "GREATER_THAN_OR_EQUAL"
	case ComparisonLessThan:
		return "LESS_THAN"
	case ComparisonLessThanOrEqual:
		return "LESS_THAN_OR_EQUAL"
	case ComparisonIn:
		return "IN"
	case ComparisonNotIn:
		return "NOT_IN"
	case ComparisonContains:
		return "CONTAINS"
	case ComparisonStartsWith:
		return "STARTS_WITH"
	case ComparisonEndsWith:
		return "ENDS_WITH"
	case ComparisonMatches:
		return "MATCHES"
	case ComparisonExists:
		return "EXISTS"
	case ComparisonNotExists:
		return "NOT_EXISTS"
	default:
		return "UNKNOWN"
	}
}

// Target defines the scope a policy applies to.
type Target struct {
	// ResourceTypes are the resource types this policy targets.
	ResourceTypes []string `json:"resource_types,omitempty"`

	// Actions are the actions this policy targets.
	Actions []string `json:"actions,omitempty"`

	// SubjectTypes are the subject types this policy targets.
	SubjectTypes []SubjectType `json:"subject_types,omitempty"`
}

// Condition represents a condition that must be satisfied.
type Condition struct {
	// Type is the condition type.
	Type ConditionType `json:"type"`

	// Key is the attribute or context key to evaluate.
	Key string `json:"key,omitempty"`

	// Operator is the comparison operator.
	Operator ComparisonOperator `json:"operator"`

	// Value is the value to compare against.
	Value interface{} `json:"value,omitempty"`

	// Values is a list of values for IN/NOT_IN operators.
	Values []interface{} `json:"values,omitempty"`

	// SubConditions are nested conditions for AND/OR logic.
	SubConditions []Condition `json:"sub_conditions,omitempty"`

	// Description is a human-readable description.
	Description string `json:"description,omitempty"`
}

// ConditionType represents the type of condition.
type ConditionType int

const (
	// ConditionTypeAttribute evaluates an attribute value.
	ConditionTypeAttribute ConditionType = iota
	// ConditionTypeTime evaluates time-based conditions.
	ConditionTypeTime
	// ConditionTypeIP evaluates IP-based conditions.
	ConditionTypeIP
	// ConditionTypeContext evaluates request context values.
	ConditionTypeContext
	// ConditionTypeAnd requires all sub-conditions to match.
	ConditionTypeAnd
	// ConditionTypeOr requires any sub-condition to match.
	ConditionTypeOr
	// ConditionTypeNot inverts the sub-condition result.
	ConditionTypeNot
)

// String returns the string representation of the condition type.
func (t ConditionType) String() string {
	switch t {
	case ConditionTypeAttribute:
		return "ATTRIBUTE"
	case ConditionTypeTime:
		return "TIME"
	case ConditionTypeIP:
		return "IP"
	case ConditionTypeContext:
		return "CONTEXT"
	case ConditionTypeAnd:
		return "AND"
	case ConditionTypeOr:
		return "OR"
	case ConditionTypeNot:
		return "NOT"
	default:
		return "UNKNOWN"
	}
}

// CreatePolicyRequest is the request to create a new policy.
type CreatePolicyRequest struct {
	// Name is the unique policy name.
	Name string `json:"name"`

	// Description is a human-readable description.
	Description string `json:"description,omitempty"`

	// Enabled indicates if the policy should be active.
	Enabled bool `json:"enabled,omitempty"`

	// Priority determines evaluation order.
	Priority int `json:"priority,omitempty"`

	// Rules are the policy rules.
	Rules []Rule `json:"rules,omitempty"`

	// Targets define the policy scope.
	Targets []Target `json:"targets,omitempty"`

	// Conditions are global conditions.
	Conditions []Condition `json:"conditions,omitempty"`

	// Effect is the default effect.
	Effect Effect `json:"effect,omitempty"`

	// Labels are arbitrary metadata.
	Labels map[string]string `json:"labels,omitempty"`

	// SpecYAML is the raw policy specification as YAML.
	// If provided, it takes precedence over individual fields.
	SpecYAML []byte `json:"spec_yaml,omitempty"`
}

// UpdatePolicyRequest is the request to update an existing policy.
type UpdatePolicyRequest struct {
	// Description is the new description (optional).
	Description *string `json:"description,omitempty"`

	// Enabled updates the enabled state (optional).
	Enabled *bool `json:"enabled,omitempty"`

	// Priority updates the priority (optional).
	Priority *int `json:"priority,omitempty"`

	// Rules replaces the policy rules (optional).
	Rules []Rule `json:"rules,omitempty"`

	// Targets replaces the policy targets (optional).
	Targets []Target `json:"targets,omitempty"`

	// Conditions replaces the global conditions (optional).
	Conditions []Condition `json:"conditions,omitempty"`

	// Effect updates the default effect (optional).
	Effect *Effect `json:"effect,omitempty"`

	// Labels replaces the labels (optional).
	Labels map[string]string `json:"labels,omitempty"`

	// Version must be zero: policies are not versioned, and a non-zero
	// value is refused with PolicyVersionUnsupportedError before the
	// request is sent.
	Version int64 `json:"version,omitempty"`

	// SpecYAML is the raw replacement policy specification as YAML
	// (optional). If provided, it takes precedence over individual fields.
	SpecYAML []byte `json:"spec_yaml,omitempty"`
}

// ListPoliciesRequest is the request to list policies.
type ListPoliciesRequest struct {
	// Limit is the maximum number of policies to return.
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor.
	Cursor string `json:"cursor,omitempty"`

	// Filter filters policies by name pattern.
	Filter string `json:"filter,omitempty"`

	// EnabledOnly returns only enabled policies.
	EnabledOnly bool `json:"enabled_only,omitempty"`

	// Labels filters by labels.
	Labels map[string]string `json:"labels,omitempty"`

	// OrderBy specifies the ordering field.
	OrderBy string `json:"order_by,omitempty"`

	// Descending reverses the sort order.
	Descending bool `json:"descending,omitempty"`
}

// ListPoliciesResponse is the response from listing policies.
type ListPoliciesResponse struct {
	// Policies is the list of policies.
	Policies []Policy `json:"policies"`

	// NextCursor is the cursor for the next page.
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if more results are available.
	HasMore bool `json:"has_more"`

	// TotalCount is the total number of matching policies.
	TotalCount int64 `json:"total_count"`
}

// ValidationResult contains the results of policy validation.
type ValidationResult struct {
	// Valid indicates if the policy is valid.
	Valid bool `json:"valid"`

	// Errors are validation errors.
	Errors []ValidationError `json:"errors,omitempty"`

	// Warnings are non-fatal validation warnings.
	Warnings []ValidationWarning `json:"warnings,omitempty"`
}

// ValidationError describes a validation error.
type ValidationError struct {
	// Field is the field path that has the error.
	Field string `json:"field,omitempty"`

	// Code is the error code.
	Code string `json:"code,omitempty"`

	// Message is the error description.
	Message string `json:"message"`
}

// ValidationWarning describes a validation warning.
type ValidationWarning struct {
	// Field is the field path with the warning.
	Field string `json:"field,omitempty"`

	// Code is the warning code.
	Code string `json:"code,omitempty"`

	// Message is the warning description.
	Message string `json:"message"`
}

// TestPolicyRequest is the request to test a policy.
type TestPolicyRequest struct {
	// PolicyName is the name of the policy to test.
	PolicyName string `json:"policy_name"`

	// TestCases are the test cases to run.
	TestCases []TestCase `json:"test_cases"`
}

// TestCase defines a test case for policy testing.
type TestCase struct {
	// Name is the test case name.
	Name string `json:"name"`

	// Description is the test case description.
	Description string `json:"description,omitempty"`

	// Subject is the subject making the request.
	Subject Subject `json:"subject"`

	// Action is the action being performed.
	Action string `json:"action"`

	// Resource is the resource being accessed.
	Resource string `json:"resource"`

	// Context is additional request context.
	Context map[string]interface{} `json:"context,omitempty"`

	// ExpectedResult is the expected authorization result.
	ExpectedResult Effect `json:"expected_result"`
}

// Subject represents the entity making an authorization request.
type Subject struct {
	// ID is the unique subject identifier.
	ID string `json:"id"`

	// Type is the subject type.
	Type SubjectType `json:"type,omitempty"`

	// TenantID is the subject's tenant.
	TenantID string `json:"tenant_id,omitempty"`

	// Roles are the roles assigned to the subject.
	Roles []string `json:"roles,omitempty"`

	// Groups are the groups the subject belongs to.
	Groups []string `json:"groups,omitempty"`

	// Attributes are additional subject attributes.
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// TestPolicyResponse is the response from testing a policy.
type TestPolicyResponse struct {
	// Passed is the number of test cases that passed.
	Passed int `json:"passed"`

	// Failed is the number of test cases that failed.
	Failed int `json:"failed"`

	// Results are the individual test case results.
	Results []TestCaseResult `json:"results"`
}

// TestCaseResult is the result of a single test case.
type TestCaseResult struct {
	// Name is the test case name.
	Name string `json:"name"`

	// Passed indicates if the test case passed.
	Passed bool `json:"passed"`

	// ExpectedResult is the expected result.
	ExpectedResult Effect `json:"expected_result"`

	// ActualResult is the actual result.
	ActualResult Effect `json:"actual_result"`

	// MatchedRule is the rule that matched (if any).
	MatchedRule string `json:"matched_rule,omitempty"`

	// EvaluationTimeNs is the evaluation time in nanoseconds.
	EvaluationTimeNs int64 `json:"evaluation_time_ns,omitempty"`

	// Error is any error that occurred during evaluation.
	Error string `json:"error,omitempty"`
}

// SimulateRequest is the request to simulate an authorization decision.
type SimulateRequest struct {
	// Subject is the subject making the request.
	Subject Subject `json:"subject"`

	// Action is the action being performed.
	Action string `json:"action"`

	// Resource is the resource being accessed.
	Resource string `json:"resource"`

	// Context is additional request context.
	Context map[string]interface{} `json:"context,omitempty"`

	// IncludeTrace includes detailed evaluation trace.
	IncludeTrace bool `json:"include_trace,omitempty"`

	// PolicyFilter limits simulation to specific policies.
	PolicyFilter []string `json:"policy_filter,omitempty"`
}

// SimulateResponse is the response from simulating an authorization.
type SimulateResponse struct {
	// Allowed indicates if the request would be allowed.
	Allowed bool `json:"allowed"`

	// Effect is the resulting effect.
	Effect Effect `json:"effect"`

	// MatchedPolicies are the policies that matched.
	MatchedPolicies []string `json:"matched_policies,omitempty"`

	// MatchedRules are the rules that matched.
	MatchedRules []string `json:"matched_rules,omitempty"`

	// DenyingPolicies are policies that would deny the request.
	DenyingPolicies []string `json:"denying_policies,omitempty"`

	// AllowingPolicies are policies that would allow the request.
	AllowingPolicies []string `json:"allowing_policies,omitempty"`

	// EvaluationTimeNs is the evaluation time in nanoseconds.
	EvaluationTimeNs int64 `json:"evaluation_time_ns,omitempty"`

	// Trace is the detailed evaluation trace (if requested).
	Trace []EvaluationTraceEntry `json:"trace,omitempty"`
}

// EvaluationTraceEntry represents a step in the policy evaluation.
type EvaluationTraceEntry struct {
	// PolicyName is the policy being evaluated.
	PolicyName string `json:"policy_name"`

	// RuleName is the rule being evaluated.
	RuleName string `json:"rule_name,omitempty"`

	// Result is the evaluation result.
	Result Effect `json:"result"`

	// Matched indicates if the policy/rule matched.
	Matched bool `json:"matched"`

	// ConditionsEvaluated are the conditions that were evaluated.
	ConditionsEvaluated []ConditionEvaluation `json:"conditions_evaluated,omitempty"`

	// TimeNs is the evaluation time in nanoseconds.
	TimeNs int64 `json:"time_ns,omitempty"`
}

// ConditionEvaluation represents the evaluation of a condition.
type ConditionEvaluation struct {
	// Description is the condition description.
	Description string `json:"description,omitempty"`

	// Result is the evaluation result.
	Result bool `json:"result"`

	// ActualValue is the actual value found.
	ActualValue interface{} `json:"actual_value,omitempty"`

	// ExpectedValue is the expected value.
	ExpectedValue interface{} `json:"expected_value,omitempty"`
}

// EffectivePermissionsRequest is the request to get effective permissions.
type EffectivePermissionsRequest struct {
	// Subject is the subject to get permissions for.
	Subject Subject `json:"subject"`

	// ResourceType filters by resource type (optional).
	ResourceType string `json:"resource_type,omitempty"`

	// ActionPattern filters by action pattern (optional).
	ActionPattern string `json:"action_pattern,omitempty"`

	// IncludeDenied includes explicitly denied permissions.
	IncludeDenied bool `json:"include_denied,omitempty"`
}

// EffectivePermissionsResponse is the response with effective permissions.
type EffectivePermissionsResponse struct {
	// Permissions are the effective permissions.
	Permissions []EffectivePermission `json:"permissions"`

	// EvaluatedPolicies are the policies that were evaluated.
	EvaluatedPolicies []string `json:"evaluated_policies,omitempty"`

	// EvaluationTimeNs is the evaluation time in nanoseconds.
	EvaluationTimeNs int64 `json:"evaluation_time_ns,omitempty"`
}

// EffectivePermission represents an effective permission.
type EffectivePermission struct {
	// Action is the permitted action.
	Action string `json:"action"`

	// ResourcePattern is the resource pattern this applies to.
	ResourcePattern string `json:"resource_pattern"`

	// Effect is the permission effect.
	Effect Effect `json:"effect"`

	// SourcePolicies are the policies granting this permission.
	SourcePolicies []string `json:"source_policies,omitempty"`

	// Conditions are any conditions on this permission.
	Conditions []string `json:"conditions,omitempty"`
}

// convertToPolicy converts a response to a Policy type.
// It handles both *Policy pointers and map[string]interface{} (from REST/gRPC transports).
func convertToPolicy(resp interface{}) (*Policy, bool) {
	if resp == nil {
		return nil, false
	}

	// If already a *Policy, return it directly.
	if policy, ok := resp.(*Policy); ok {
		return policy, true
	}

	// If it's a map, try to convert it.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, false
	}

	// Check for nested policy field (server response format: {policy: {...}, timestamp: "..."})
	if policyData, ok := respMap["policy"].(map[string]interface{}); ok {
		respMap = policyData
	}

	policy := &Policy{}

	// Handle server format with nested metadata/spec fields.
	if metadata, ok := respMap["metadata"].(map[string]interface{}); ok {
		if v, ok := metadata["name"].(string); ok {
			policy.Name = v
		}
		if v, ok := metadata["tenant"].(string); ok {
			policy.TenantID = v
		}
		if v, ok := metadata["description"].(string); ok {
			policy.Description = v
		}
		if v, ok := metadata["priority"].(float64); ok {
			policy.Priority = int(v)
		}
		if labels, ok := metadata["labels"].(map[string]interface{}); ok {
			policy.Labels = make(map[string]string, len(labels))
			for k, v := range labels {
				if s, ok := v.(string); ok {
					policy.Labels[k] = s
				}
			}
		}
		if v, ok := metadata["created"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				policy.CreatedAt = t
			}
		}
		if v, ok := metadata["updated"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				policy.UpdatedAt = t
			}
		}
	}
	if spec, ok := respMap["spec"].(map[string]interface{}); ok {
		if rulesRaw, ok := spec["rules"].([]interface{}); ok {
			policy.Rules = convertServerRulesToSDK(rulesRaw)
		}
		if effect, ok := spec["effect"].(string); ok {
			if effect == "allow" {
				policy.Effect = EffectAllow
			} else {
				policy.Effect = EffectDeny
			}
		}
	}

	// Handle flat SDK format fields (fallback).
	if v, ok := respMap["name"].(string); ok && policy.Name == "" {
		policy.Name = v
	}
	if v, ok := respMap["tenant_id"].(string); ok && policy.TenantID == "" {
		policy.TenantID = v
	}
	if v, ok := respMap["description"].(string); ok {
		policy.Description = v
	}
	if v, ok := respMap["version"].(float64); ok {
		policy.Version = int64(v)
	}
	if v, ok := respMap["enabled"].(bool); ok {
		policy.Enabled = v
	}
	if v, ok := respMap["priority"].(float64); ok {
		policy.Priority = int(v)
	}
	if v, ok := respMap["created_by"].(string); ok {
		policy.CreatedBy = v
	}
	if v, ok := respMap["updated_by"].(string); ok {
		policy.UpdatedBy = v
	}

	// Handle effect
	if effectRaw, ok := respMap["effect"]; ok {
		switch e := effectRaw.(type) {
		case float64:
			policy.Effect = Effect(int(e))
		case string:
			if e == "ALLOW" {
				policy.Effect = EffectAllow
			} else {
				policy.Effect = EffectDeny
			}
		}
	}

	// Handle labels map.
	if labelsRaw, ok := respMap["labels"]; ok {
		switch labels := labelsRaw.(type) {
		case map[string]string:
			policy.Labels = labels
		case map[string]interface{}:
			policy.Labels = make(map[string]string, len(labels))
			for k, v := range labels {
				if s, ok := v.(string); ok {
					policy.Labels[k] = s
				}
			}
		}
	}

	// Handle timestamps.
	if v, ok := respMap["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			policy.CreatedAt = t
		}
	} else if v, ok := respMap["created"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			policy.CreatedAt = t
		}
	}
	if v, ok := respMap["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			policy.UpdatedAt = t
		}
	} else if v, ok := respMap["updated"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			policy.UpdatedAt = t
		}
	}

	// Handle rules via JSON round-trip for complex nested types.
	if rulesRaw, ok := respMap["rules"]; ok {
		rulesData, marshalErr := json.Marshal(rulesRaw)
		if marshalErr == nil {
			var rules []Rule
			if unmarshalErr := json.Unmarshal(rulesData, &rules); unmarshalErr == nil {
				policy.Rules = rules
			}
		}
	}

	// Handle targets via JSON round-trip.
	if targetsRaw, ok := respMap["targets"]; ok {
		targetsData, marshalErr := json.Marshal(targetsRaw)
		if marshalErr == nil {
			var targets []Target
			if unmarshalErr := json.Unmarshal(targetsData, &targets); unmarshalErr == nil {
				policy.Targets = targets
			}
		}
	}

	// Handle conditions via JSON round-trip.
	if conditionsRaw, ok := respMap["conditions"]; ok {
		conditionsData, marshalErr := json.Marshal(conditionsRaw)
		if marshalErr == nil {
			var conditions []Condition
			if unmarshalErr := json.Unmarshal(conditionsData, &conditions); unmarshalErr == nil {
				policy.Conditions = conditions
			}
		}
	}

	return policy, true
}

// convertServerRulesToSDK converts server-format rules (string actions/resources, string effect)
// to SDK-format rules (ActionMatcher/ResourceMatcher arrays, int Effect).
func convertServerRulesToSDK(rulesRaw []interface{}) []Rule {
	rules := make([]Rule, 0, len(rulesRaw))
	for _, ruleRaw := range rulesRaw {
		ruleMap, ok := ruleRaw.(map[string]interface{})
		if !ok {
			continue
		}
		rule := Rule{}
		if v, ok := ruleMap["name"].(string); ok {
			rule.Name = v
		}
		if v, ok := ruleMap["effect"].(string); ok {
			if v == "allow" {
				rule.Effect = EffectAllow
			} else {
				rule.Effect = EffectDeny
			}
		}
		if actions, ok := ruleMap["actions"].([]interface{}); ok {
			for _, a := range actions {
				if s, ok := a.(string); ok {
					rule.Actions = append(rule.Actions, ActionMatcher{Pattern: s})
				}
			}
		}
		if resources, ok := ruleMap["resources"].([]interface{}); ok {
			for _, r := range resources {
				if s, ok := r.(string); ok {
					rule.Resources = append(rule.Resources, ResourceMatcher{Pattern: s})
				}
			}
		}
		rules = append(rules, rule)
	}
	return rules
}

// Internal request/response types for transport

type createPolicyRequest struct {
	TenantID string               `json:"tenant_id"`
	Request  *CreatePolicyRequest `json:"request"`
}

type getPolicyRequest struct {
	TenantID   string `json:"tenant_id"`
	PolicyName string `json:"policy_name"`
}

type updatePolicyRequest struct {
	TenantID   string               `json:"tenant_id"`
	PolicyName string               `json:"policy_name"`
	Request    *UpdatePolicyRequest `json:"request"`
}

type deletePolicyRequest struct {
	TenantID   string `json:"tenant_id"`
	PolicyName string `json:"policy_name"`
}

type deletePolicyResponse struct{}

type listPoliciesRequestInternal struct {
	TenantID string               `json:"tenant_id"`
	Request  *ListPoliciesRequest `json:"request"`
}

type validatePolicyRequest struct {
	TenantID string  `json:"tenant_id"`
	Policy   *Policy `json:"policy"`
	// Name is the policy name the server checks against the document's
	// metadata.name. Empty means unchecked, so it is omitted on the wire.
	Name string `json:"name,omitempty"`
}

type testPolicyRequestInternal struct {
	TenantID string             `json:"tenant_id"`
	Request  *TestPolicyRequest `json:"request"`
}

type simulateRequestInternal struct {
	TenantID string           `json:"tenant_id"`
	Request  *SimulateRequest `json:"request"`
}

type effectivePermissionsRequestInternal struct {
	TenantID string                       `json:"tenant_id"`
	Request  *EffectivePermissionsRequest `json:"request"`
}

// policyClient implements the PolicyClient interface.
type policyClient struct {
	transport transport.Transport
}

// NewPolicyClient creates a new PolicyClient.
//
// Returns nil if the transport is nil.
func NewPolicyClient(t transport.Transport) PolicyClient {
	if t == nil {
		return nil
	}
	return &policyClient{
		transport: t,
	}
}

// Create creates a new ABAC policy.
func (c *policyClient) Create(ctx context.Context, tenantID string, req *CreatePolicyRequest) (*Policy, error) {
	if err := validateCreatePolicyRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Create", "context is required")
	}

	internalReq := &createPolicyRequest{
		TenantID: tenantID,
		Request:  req,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "Create", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.Policy.Create", "nil response from server")
	}

	policy, ok := convertToPolicy(resp)
	if !ok {
		return nil, errors.NewInternal("iam.Policy.Create", "invalid response type")
	}

	return policy, nil
}

// Get retrieves a policy by name.
func (c *policyClient) Get(ctx context.Context, tenantID, policyName string) (*Policy, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.Policy.Get", "tenant ID is required")
	}
	if policyName == "" {
		return nil, errors.NewInvalidInput("iam.Policy.Get", "policy name is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Get", "context is required")
	}

	internalReq := &getPolicyRequest{
		TenantID:   tenantID,
		PolicyName: policyName,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "Get", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.Policy.Get", "policy not found")
	}

	policy, ok := convertToPolicy(resp)
	if !ok {
		return nil, errors.NewInternal("iam.Policy.Get", "invalid response type")
	}

	return policy, nil
}

// Update updates an existing policy.
func (c *policyClient) Update(ctx context.Context, tenantID, policyName string, req *UpdatePolicyRequest) (*Policy, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.Policy.Update", "tenant ID is required")
	}
	if policyName == "" {
		return nil, errors.NewInvalidInput("iam.Policy.Update", "policy name is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Update", "request is required")
	}
	if req.Version != 0 {
		return nil, &PolicyVersionUnsupportedError{Version: strconv.FormatInt(req.Version, 10)}
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Update", "context is required")
	}

	internalReq := &updatePolicyRequest{
		TenantID:   tenantID,
		PolicyName: policyName,
		Request:    req,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "Update", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.Policy.Update", "nil response from server")
	}

	policy, ok := convertToPolicy(resp)
	if !ok {
		return nil, errors.NewInternal("iam.Policy.Update", "invalid response type")
	}

	return policy, nil
}

// Delete removes a policy.
func (c *policyClient) Delete(ctx context.Context, tenantID, policyName string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.Policy.Delete", "tenant ID is required")
	}
	if policyName == "" {
		return errors.NewInvalidInput("iam.Policy.Delete", "policy name is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.Policy.Delete", "context is required")
	}

	internalReq := &deletePolicyRequest{
		TenantID:   tenantID,
		PolicyName: policyName,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "Delete", internalReq)
	if err != nil {
		return err
	}

	// Accept nil response, *deletePolicyResponse, or map response.
	if resp == nil {
		return nil
	}

	// If already the expected type, return.
	if _, ok := resp.(*deletePolicyResponse); ok {
		return nil
	}

	// Accept map responses from REST/gRPC transports.
	if _, ok := resp.(map[string]interface{}); ok {
		return nil
	}

	return errors.NewInternal("iam.Policy.Delete", "invalid response type")
}

// List lists policies matching the filter criteria.
func (c *policyClient) List(ctx context.Context, tenantID string, req *ListPoliciesRequest) (*ListPoliciesResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.Policy.List", "tenant ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.List", "context is required")
	}

	if req == nil {
		req = &ListPoliciesRequest{}
	}

	internalReq := &listPoliciesRequestInternal{
		TenantID: tenantID,
		Request:  req,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "List", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListPoliciesResponse{Policies: []Policy{}}, nil
	}

	// If already a *ListPoliciesResponse, return it directly.
	if listResp, ok := resp.(*ListPoliciesResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/gRPC transports.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.Policy.List", "invalid response type")
	}

	result := &ListPoliciesResponse{Policies: []Policy{}}

	// Parse policies array.
	if policiesRaw, ok := respMap["policies"]; ok {
		switch policies := policiesRaw.(type) {
		case []interface{}:
			for _, p := range policies {
				if policy, ok := convertToPolicy(p); ok {
					result.Policies = append(result.Policies, *policy)
				}
			}
		case []*Policy:
			for _, p := range policies {
				if p != nil {
					result.Policies = append(result.Policies, *p)
				}
			}
		case []Policy:
			result.Policies = policies
		}
	}

	// Parse pagination info.
	if v, ok := respMap["next_cursor"].(string); ok {
		result.NextCursor = v
	}
	if v, ok := respMap["has_more"].(bool); ok {
		result.HasMore = v
	}
	if v, ok := respMap["total_count"].(float64); ok {
		result.TotalCount = int64(v)
	}

	// Also handle direct limit/offset from REST API.
	if v, ok := respMap["limit"].(float64); ok {
		result.TotalCount = int64(v)
	}

	return result, nil
}

// Validate validates a policy without persisting it.
func (c *policyClient) Validate(ctx context.Context, tenantID string, policy *Policy) (*ValidationResult, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.Policy.Validate", "tenant ID is required")
	}
	if policy == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Validate", "policy is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Validate", "context is required")
	}

	internalReq := &validatePolicyRequest{
		TenantID: tenantID,
		Policy:   policy,
		Name:     policy.Name,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "Validate", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.Policy.Validate", "nil response from server")
	}

	// Handle direct ValidationResult pointer.
	if result, ok := resp.(*ValidationResult); ok {
		return result, nil
	}

	// Handle map response from gRPC/REST transports.
	if respMap, ok := resp.(map[string]interface{}); ok {
		result := &ValidationResult{}
		if v, ok := respMap["valid"].(bool); ok {
			result.Valid = v
		}

		// Handle errors array.
		if errorsRaw, ok := respMap["errors"]; ok {
			switch errors := errorsRaw.(type) {
			case []interface{}:
				for _, e := range errors {
					if errMap, ok := e.(map[string]interface{}); ok {
						ve := ValidationError{}
						if v, ok := errMap["field"].(string); ok {
							ve.Field = v
						}
						if v, ok := errMap["code"].(string); ok {
							ve.Code = v
						}
						if v, ok := errMap["message"].(string); ok {
							ve.Message = v
						}
						result.Errors = append(result.Errors, ve)
					}
				}
			case []string:
				for _, msg := range errors {
					result.Errors = append(result.Errors, ValidationError{Message: msg})
				}
			}
		}

		return result, nil
	}

	return nil, errors.NewInternal("iam.Policy.Validate", "invalid response type")
}

// Test tests a policy against sample requests.
func (c *policyClient) Test(ctx context.Context, tenantID string, req *TestPolicyRequest) (*TestPolicyResponse, error) {
	if err := validateTestPolicyRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Test", "context is required")
	}

	internalReq := &testPolicyRequestInternal{
		TenantID: tenantID,
		Request:  req,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "Test", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.Policy.Test", "nil response from server")
	}

	if testResp, ok := resp.(*TestPolicyResponse); ok {
		return testResp, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[TestPolicyResponse](resp, "iam.Policy.Test")
}

// Simulate simulates an authorization decision without side effects.
func (c *policyClient) Simulate(ctx context.Context, tenantID string, req *SimulateRequest) (*SimulateResponse, error) {
	if err := validateSimulateRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.Simulate", "context is required")
	}

	internalReq := &simulateRequestInternal{
		TenantID: tenantID,
		Request:  req,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "Simulate", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.Policy.Simulate", "nil response from server")
	}

	// Handle direct SimulateResponse pointer.
	if simResp, ok := resp.(*SimulateResponse); ok {
		return simResp, nil
	}

	// Handle map response from gRPC/REST transports.
	if respMap, ok := resp.(map[string]interface{}); ok {
		simResp := &SimulateResponse{}
		if v, ok := respMap["allowed"].(bool); ok {
			simResp.Allowed = v
		}
		if v, ok := respMap["effect"].(float64); ok {
			simResp.Effect = Effect(int(v))
		}
		if v, ok := respMap["evaluation_time_ns"].(float64); ok {
			simResp.EvaluationTimeNs = int64(v)
		}

		// Handle matched_policies array.
		if arr, ok := respMap["matched_policies"].([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					simResp.MatchedPolicies = append(simResp.MatchedPolicies, s)
				}
			}
		} else if arr, ok := respMap["matched_policies"].([]string); ok {
			simResp.MatchedPolicies = arr
		}

		// Handle matched_rules array.
		if arr, ok := respMap["matched_rules"].([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					simResp.MatchedRules = append(simResp.MatchedRules, s)
				}
			}
		} else if arr, ok := respMap["matched_rules"].([]string); ok {
			simResp.MatchedRules = arr
		}

		return simResp, nil
	}

	return nil, errors.NewInternal("iam.Policy.Simulate", "invalid response type")
}

// GetEffectivePermissions retrieves the effective permissions for a subject.
func (c *policyClient) GetEffectivePermissions(ctx context.Context, tenantID string, req *EffectivePermissionsRequest) (*EffectivePermissionsResponse, error) {
	if err := validateEffectivePermissionsRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.Policy.GetEffectivePermissions", "context is required")
	}

	internalReq := &effectivePermissionsRequestInternal{
		TenantID: tenantID,
		Request:  req,
	}

	resp, err := c.transport.Request(ctx, policyServiceName, "GetEffectivePermissions", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.Policy.GetEffectivePermissions", "nil response from server")
	}

	if permResp, ok := resp.(*EffectivePermissionsResponse); ok {
		return permResp, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[EffectivePermissionsResponse](resp, "iam.Policy.GetEffectivePermissions")
}

// Validation functions

func validateCreatePolicyRequest(tenantID string, req *CreatePolicyRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.Policy.Create", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.Policy.Create", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("iam.Policy.Create", "policy name is required")
	}
	// gRPC and MCP carry the rules inside SpecYAML and refuse structured
	// rules on create, so a create needs rules in either place.
	if len(req.Rules) == 0 && len(req.SpecYAML) == 0 {
		return errors.NewInvalidInput("iam.Policy.Create", "at least one rule or a spec YAML is required")
	}
	return nil
}

func validateTestPolicyRequest(tenantID string, req *TestPolicyRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.Policy.Test", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.Policy.Test", "request is required")
	}
	if req.PolicyName == "" {
		return errors.NewInvalidInput("iam.Policy.Test", "policy name is required")
	}
	if len(req.TestCases) == 0 {
		return errors.NewInvalidInput("iam.Policy.Test", "at least one test case is required")
	}
	return nil
}

func validateSimulateRequest(tenantID string, req *SimulateRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.Policy.Simulate", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.Policy.Simulate", "request is required")
	}
	if req.Subject.ID == "" {
		return errors.NewInvalidInput("iam.Policy.Simulate", "subject ID is required")
	}
	if req.Action == "" {
		return errors.NewInvalidInput("iam.Policy.Simulate", "action is required")
	}
	if req.Resource == "" {
		return errors.NewInvalidInput("iam.Policy.Simulate", "resource is required")
	}
	return nil
}

func validateEffectivePermissionsRequest(tenantID string, req *EffectivePermissionsRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.Policy.GetEffectivePermissions", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.Policy.GetEffectivePermissions", "request is required")
	}
	if req.Subject.ID == "" {
		return errors.NewInvalidInput("iam.Policy.GetEffectivePermissions", "subject ID is required")
	}
	return nil
}
