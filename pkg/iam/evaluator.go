//go:build iam

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

package iam

import (
	"context"
	"sync"
	"time"
)

// PolicyEvaluator evaluates authorization requests against compiled policies.
type PolicyEvaluator struct {
	compiler *PolicyCompiler
	cache    *PolicyCache
	mu       sync.RWMutex
}

// NewPolicyEvaluator creates a new policy evaluator.
func NewPolicyEvaluator(compiler *PolicyCompiler, cache *PolicyCache) *PolicyEvaluator {
	return &PolicyEvaluator{
		compiler: compiler,
		cache:    cache,
	}
}

// Evaluate evaluates an authorization request against applicable policies.
// Target: 2-5us total evaluation time.
func (e *PolicyEvaluator) Evaluate(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	startTime := time.Now()

	// Validate request
	if req == nil || req.Subject.ID == "" {
		return &CheckResponse{
			Allowed:          false,
			Reason:           "invalid request: subject required",
			EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
		}, nil
	}

	// Get applicable policies from cache
	// Use TenantID if set, fallback to Tenant for compatibility
	tenantID := req.Subject.TenantID
	if tenantID == "" {
		tenantID = req.Subject.Tenant
	}
	policies, err := e.getApplicablePolicies(ctx, tenantID)
	if err != nil {
		return &CheckResponse{
			Allowed:          false,
			Reason:           "failed to load policies: " + err.Error(),
			EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
		}, nil
	}

	// Create evaluation context
	evalCtx := &EvalContext{
		Subject:   &req.Subject,
		Action:    req.Action,
		Resource:  req.Resource,
		Request:   req.Context,
		Time:      time.Now(),
		Variables: make(map[string]interface{}),
	}

	// Evaluate policies
	for _, policy := range policies {
		// Check if policy applies to this subject
		if !e.matchesSubject(policy.SubjectMatcher, &req.Subject) {
			continue
		}

		// Merge policy variables into eval context
		for k, v := range policy.Variables {
			evalCtx.Variables[k] = v
		}

		// Evaluate deny rules first (they are pre-sorted)
		for _, rule := range policy.Rules {
			if rule.Effect == EffectDeny {
				if e.evaluateRule(rule, evalCtx) {
					// Explicit deny - return immediately
					return &CheckResponse{
						Allowed:          false,
						Reason:           "denied by policy '" + policy.Name + "', rule '" + rule.Name + "'",
						MatchedPolicy:    policy.Name,
						MatchedRule:      rule.Name,
						EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
					}, nil
				}
			}
		}

		// Evaluate allow rules
		for _, rule := range policy.Rules {
			if rule.Effect == EffectAllow {
				if e.evaluateRule(rule, evalCtx) {
					// Allow found - return immediately
					return &CheckResponse{
						Allowed:          true,
						Reason:           "allowed by policy '" + policy.Name + "', rule '" + rule.Name + "'",
						MatchedPolicy:    policy.Name,
						MatchedRule:      rule.Name,
						EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
					}, nil
				}
			}
		}
	}

	// Default deny if no matching allow rule found
	return &CheckResponse{
		Allowed:          false,
		Reason:           "no matching allow policy",
		EvaluationTimeNs: time.Since(startTime).Nanoseconds(),
	}, nil
}

// evaluateRule checks if a rule matches the current request.
func (e *PolicyEvaluator) evaluateRule(rule *CompiledRule, ctx *EvalContext) bool {
	// Check action match
	if !e.matchesAction(rule.Actions, ctx.Action) {
		return false
	}

	// Check resource match using trie and capture variables
	_, capturedVars, matched := rule.ResourceTrie.Match(ctx.Resource)
	if !matched {
		return false
	}

	// Validate captured variables against subject attributes
	if !e.validateCapturedVars(capturedVars, ctx.Subject) {
		return false
	}

	// Evaluate unless conditions (any true means rule doesn't apply)
	for _, cond := range rule.Unless {
		if cond.Evaluate(ctx) {
			return false
		}
	}

	// Evaluate when conditions (all must be true)
	for _, cond := range rule.Conditions {
		if !cond.Evaluate(ctx) {
			return false
		}
	}

	return true
}

// validateCapturedVars checks if captured variables from resource matching
// match the corresponding subject attributes (e.g., ${subject.id} must match subject.ID).
func (e *PolicyEvaluator) validateCapturedVars(capturedVars map[string]string, subject *Subject) bool {
	for varName, capturedValue := range capturedVars {
		// Check for subject.* variables
		if len(varName) > 8 && varName[:8] == "subject." {
			attrName := varName[8:] // e.g., "id", "email", "tenant_id"
			expectedValue := ""
			switch attrName {
			case "id":
				expectedValue = subject.ID
			case "name":
				expectedValue = subject.Name
			case "email":
				expectedValue = subject.Email
			case "tenant":
				expectedValue = subject.Tenant
			case "tenant_id":
				expectedValue = subject.TenantID
			default:
				// Check in attributes map
				if subject.Attributes != nil {
					if v, ok := subject.Attributes[attrName]; ok {
						if s, ok := v.(string); ok {
							expectedValue = s
						}
					}
				}
			}
			if expectedValue != "" && capturedValue != expectedValue {
				return false
			}
		}
	}
	return true
}

// matchesSubject checks if a subject matches the policy's subject selector.
func (e *PolicyEvaluator) matchesSubject(matcher *SubjectMatcher, subject *Subject) bool {
	// If no matcher is defined, match all subjects (policy applies to everyone)
	if matcher == nil {
		return true
	}

	// Match all
	if matcher.Any {
		return true
	}

	// If no constraints defined (empty matcher), match everyone
	// This handles policies without explicit subject selectors
	if !matcher.Authenticated &&
		len(matcher.Users) == 0 &&
		len(matcher.Groups) == 0 &&
		len(matcher.Roles) == 0 &&
		len(matcher.Services) == 0 &&
		len(matcher.Matchers) == 0 {
		return true
	}

	// Match authenticated requirement
	if matcher.Authenticated && !subject.Authenticated {
		return false
	}

	// Track if we have any specific identity constraints
	hasIdentityConstraints := len(matcher.Users) > 0 || len(matcher.Groups) > 0 ||
		len(matcher.Roles) > 0 || len(matcher.Services) > 0

	// If no identity constraints, check if we just need authentication
	if !hasIdentityConstraints && len(matcher.Matchers) == 0 {
		// Only authenticated requirement - if we got here, authentication passed
		return matcher.Authenticated
	}

	// Match specific users
	if len(matcher.Users) > 0 {
		if _, ok := matcher.Users[subject.ID]; ok {
			return true
		}
	}

	// Match groups
	if len(matcher.Groups) > 0 {
		for _, group := range subject.Groups {
			if _, ok := matcher.Groups[group]; ok {
				return true
			}
		}
	}

	// Match roles
	if len(matcher.Roles) > 0 {
		for _, role := range subject.Roles {
			if _, ok := matcher.Roles[role]; ok {
				return true
			}
		}
	}

	// Match services
	if len(matcher.Services) > 0 {
		if _, ok := matcher.Services[subject.ID]; ok {
			return true
		}
	}

	// If we have identity constraints but none matched, return false
	if hasIdentityConstraints {
		return false
	}

	// Match attribute matchers (all must pass)
	for _, attrMatcher := range matcher.Matchers {
		if !attrMatcher.Match(subject) {
			return false
		}
	}

	// All attribute matchers passed
	return len(matcher.Matchers) > 0
}

// matchesAction checks if the requested action matches any in the rule's action list.
func (e *PolicyEvaluator) matchesAction(ruleActions []string, requestedAction string) bool {
	for _, action := range ruleActions {
		if action == "*" || action == requestedAction {
			return true
		}
		// Support prefix matching (e.g., "write*" matches "write:key")
		if len(action) > 0 && action[len(action)-1] == '*' {
			prefix := action[:len(action)-1]
			if len(requestedAction) >= len(prefix) && requestedAction[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

// getApplicablePolicies retrieves compiled policies for a tenant from cache.
func (e *PolicyEvaluator) getApplicablePolicies(_ context.Context, tenantID string) ([]*CompiledPolicy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.cache != nil {
		return e.cache.GetAll(tenantID), nil
	}

	return nil, nil
}
