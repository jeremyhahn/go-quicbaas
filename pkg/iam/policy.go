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

// PolicyEngine provides policy management and evaluation.
type PolicyEngine struct {
	storage   PolicyStorage
	cache     *PolicyCache
	compiler  *PolicyCompiler
	evaluator *PolicyEvaluator
	opts      PolicyEngineOptions
	mu        sync.RWMutex
}

// NewPolicyEngine creates a new policy engine.
func NewPolicyEngine(opts PolicyEngineOptions) *PolicyEngine {
	compiler := NewPolicyCompiler()
	evaluator := NewPolicyEvaluator(compiler, opts.Cache)

	return &PolicyEngine{
		storage:   opts.Storage,
		cache:     opts.Cache,
		compiler:  compiler,
		evaluator: evaluator,
		opts:      opts,
	}
}

// CreatePolicy creates a new policy.
func (e *PolicyEngine) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Set tenant if not already set (before validation)
	if policy != nil && policy.Metadata.Tenant == "" {
		policy.Metadata.Tenant = tenantID
	}

	// Validate policy
	if err := e.ValidatePolicy(policy); err != nil {
		return err
	}

	// Verify tenant matches
	if policy.Metadata.Tenant != tenantID {
		return &InvalidPolicyError{
			Reason: "policy tenant does not match request tenant",
		}
	}

	// Check if policy already exists
	existing, err := e.storage.GetPolicy(ctx, tenantID, policy.Metadata.Name)
	if err == nil && existing != nil {
		return &PolicyExistsError{
			TenantID: tenantID,
			Name:     policy.Metadata.Name,
		}
	}

	// Store policy
	if err := e.storage.CreatePolicy(ctx, tenantID, policy); err != nil {
		return err
	}

	// Invalidate cache
	if e.cache != nil {
		e.cache.InvalidateAll(tenantID)
	}

	return nil
}

// GetPolicy retrieves a policy by name.
func (e *PolicyEngine) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	policy, err := e.storage.GetPolicy(ctx, tenantID, name)
	if err != nil {
		return nil, err
	}

	if policy == nil {
		return nil, &PolicyNotFoundTypeError{
			TenantID: tenantID,
			Name:     name,
		}
	}

	return policy, nil
}

// UpdatePolicy updates an existing policy.
func (e *PolicyEngine) UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate policy
	if err := e.ValidatePolicy(policy); err != nil {
		return err
	}

	// Verify tenant matches
	if policy.Metadata.Tenant != tenantID {
		return &InvalidPolicyError{
			Reason: "policy tenant does not match request tenant",
		}
	}

	// Check if policy exists
	existing, err := e.storage.GetPolicy(ctx, tenantID, policy.Metadata.Name)
	if err != nil {
		return err
	}
	if existing == nil {
		return &PolicyNotFoundTypeError{
			TenantID: tenantID,
			Name:     policy.Metadata.Name,
		}
	}

	// Update policy
	if err := e.storage.UpdatePolicy(ctx, tenantID, policy); err != nil {
		return err
	}

	// Invalidate cache
	if e.cache != nil {
		e.cache.InvalidateAll(tenantID)
	}

	return nil
}

// DeletePolicy deletes a policy by name.
func (e *PolicyEngine) DeletePolicy(ctx context.Context, tenantID, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if policy exists
	existing, err := e.storage.GetPolicy(ctx, tenantID, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return &PolicyNotFoundTypeError{
			TenantID: tenantID,
			Name:     name,
		}
	}

	// Delete policy
	if err := e.storage.DeletePolicy(ctx, tenantID, name); err != nil {
		return err
	}

	// Invalidate cache
	if e.cache != nil {
		e.cache.InvalidateAll(tenantID)
	}

	return nil
}

// ListPolicies lists policies for a tenant.
func (e *PolicyEngine) ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.storage.ListPolicies(ctx, tenantID, opts)
}

// ValidatePolicy validates a policy for correctness.
func (e *PolicyEngine) ValidatePolicy(policy *Policy) error {
	if policy == nil {
		return &InvalidPolicyError{Reason: "policy is nil"}
	}

	return policy.Validate()
}

// TestPolicy evaluates a policy against a test request without persisting it.
func (e *PolicyEngine) TestPolicy(ctx context.Context, tenantID string, req *CheckRequest) (*CheckResponse, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.evaluator.Evaluate(ctx, req)
}

// Evaluate evaluates an authorization request.
func (e *PolicyEngine) Evaluate(ctx context.Context, req *CheckRequest) (*CheckResponse, error) {
	// Ensure cache is populated for the tenant
	tenantID := req.Subject.TenantID
	if tenantID == "" {
		tenantID = req.Subject.Tenant
	}
	if tenantID != "" && e.cache != nil {
		// Check if cache has policies for this tenant
		if len(e.cache.GetAll(tenantID)) == 0 {
			// Cache is empty, populate it (ignore errors, continue with empty cache)
			_ = e.CompileAndCache(ctx, tenantID)
		}
	}
	return e.evaluator.Evaluate(ctx, req)
}

// CompileAndCache compiles all policies for a tenant and caches them.
func (e *PolicyEngine) CompileAndCache(ctx context.Context, tenantID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Get all policies for tenant
	policies, err := e.storage.ListPolicies(ctx, tenantID, ListOptions{})
	if err != nil {
		return err
	}

	// Compile each policy
	compiled := make([]*CompiledPolicy, 0, len(policies))
	for _, policy := range policies {
		cp, err := e.compiler.Compile(policy)
		if err != nil {
			// Log error but continue with other policies
			continue
		}
		compiled = append(compiled, cp)
	}

	// Cache compiled policies
	if e.cache != nil {
		e.cache.SetBatch(tenantID, compiled)
	}

	return nil
}

// RefreshCache refreshes the policy cache for a tenant.
func (e *PolicyEngine) RefreshCache(ctx context.Context, tenantID string) error {
	return e.CompileAndCache(ctx, tenantID)
}

// ClearCache clears the policy cache.
func (e *PolicyEngine) ClearCache(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cache != nil {
		e.cache.Clear()
	}

	e.compiler.ClearCache()

	return nil
}

// SimulatePolicy evaluates a draft policy against a check request without persisting.
// This enables dry-run testing of policies before deployment.
func (e *PolicyEngine) SimulatePolicy(ctx context.Context, req *PolicySimulationRequest) (*PolicySimulationResult, error) {
	if req == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.simulate", "request is nil", nil)
	}
	if req.Policy == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.simulate", "policy is nil", nil)
	}
	if req.CheckRequest == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.simulate", "check request is nil", nil)
	}

	startTime := time.Now()

	// Validate the policy first
	if err := req.Policy.Validate(); err != nil {
		return nil, NewIAMError(ErrPolicyInvalid, "iam.policy.simulate", "policy validation failed", err)
	}

	// Compile the policy without caching
	compiled, err := e.compiler.Compile(req.Policy)
	if err != nil {
		return nil, NewIAMError(ErrPolicyCompilationFailed, "iam.policy.simulate", "policy compilation failed", err)
	}

	// Evaluate using a temporary single-policy setup
	result := &PolicySimulationResult{
		Allowed:          false,
		EvaluationTimeNs: 0,
		Reason:           "default deny",
	}

	var steps []EvaluationStep
	stepNum := 0

	// Check subject matching
	stepNum++
	subjectMatches := e.evaluator.matchesSubject(compiled.SubjectMatcher, &req.CheckRequest.Subject)
	if req.IncludeTrace {
		steps = append(steps, EvaluationStep{
			StepNumber: stepNum,
			PolicyName: req.Policy.Metadata.Name,
			Action:     "subject_match",
			Result:     subjectMatches,
			Details:    formatSubjectMatchDetails(&req.CheckRequest.Subject, subjectMatches),
		})
	}

	if !subjectMatches {
		result.Reason = "subject does not match policy"
		result.EvaluationSteps = steps
		result.EvaluationTimeNs = time.Since(startTime).Nanoseconds()
		return result, nil
	}

	// Evaluate rules in priority order
	for _, rule := range compiled.Rules {
		stepNum++

		// Check action match
		actionMatches := matchAction(rule.Actions, req.CheckRequest.Action)
		if req.IncludeTrace {
			steps = append(steps, EvaluationStep{
				StepNumber: stepNum,
				PolicyName: req.Policy.Metadata.Name,
				RuleName:   rule.Name,
				Action:     "action_match",
				Result:     actionMatches,
				Details:    formatActionMatchDetails(rule.Actions, req.CheckRequest.Action),
			})
		}

		if !actionMatches {
			continue
		}

		// Check resource match
		stepNum++
		var resourceMatches bool
		if rule.ResourceTrie != nil {
			_, _, resourceMatches = rule.ResourceTrie.Match(req.CheckRequest.Resource)
		}
		if req.IncludeTrace {
			steps = append(steps, EvaluationStep{
				StepNumber: stepNum,
				PolicyName: req.Policy.Metadata.Name,
				RuleName:   rule.Name,
				Action:     "resource_match",
				Result:     resourceMatches,
				Details:    formatResourceMatchDetails(req.CheckRequest.Resource),
			})
		}

		if !resourceMatches {
			continue
		}

		// Rule matches - apply effect
		stepNum++
		result.MatchedPolicy = req.Policy.Metadata.Name
		result.MatchedRule = rule.Name

		if rule.Effect == EffectDeny {
			result.Allowed = false
			result.Reason = "explicit deny"
			if req.IncludeTrace {
				steps = append(steps, EvaluationStep{
					StepNumber: stepNum,
					PolicyName: req.Policy.Metadata.Name,
					RuleName:   rule.Name,
					Action:     "apply_effect",
					Result:     false,
					Details:    "rule effect is DENY",
				})
			}
		} else {
			result.Allowed = true
			result.Reason = "allowed by rule"
			if req.IncludeTrace {
				steps = append(steps, EvaluationStep{
					StepNumber: stepNum,
					PolicyName: req.Policy.Metadata.Name,
					RuleName:   rule.Name,
					Action:     "apply_effect",
					Result:     true,
					Details:    "rule effect is ALLOW",
				})
			}
		}

		result.EvaluationSteps = steps
		result.EvaluationTimeNs = time.Since(startTime).Nanoseconds()
		return result, nil
	}

	// No rule matched
	result.EvaluationSteps = steps
	result.EvaluationTimeNs = time.Since(startTime).Nanoseconds()
	return result, nil
}

// GetEffectivePermissions calculates the effective permissions for a subject.
// It iterates through all applicable policies and determines allowed/denied actions.
func (e *PolicyEngine) GetEffectivePermissions(ctx context.Context, tenantID string, req *EffectivePermissionsRequest) (*EffectivePermissionsResult, error) {
	if req == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.effectivePermissions", "request is nil", nil)
	}
	if req.Subject == nil {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.effectivePermissions", "subject is nil", nil)
	}
	if len(req.ResourcePatterns) == 0 {
		return nil, NewIAMError(ErrInvalidCheckRequest, "iam.policy.effectivePermissions", "resource patterns are required", nil)
	}

	startTime := time.Now()

	// Ensure tenant ID is set on subject
	if req.Subject.TenantID == "" {
		req.Subject.TenantID = tenantID
	}

	// Get all policies for tenant
	e.mu.RLock()
	policies, err := e.storage.ListPolicies(ctx, tenantID, ListOptions{})
	e.mu.RUnlock()

	if err != nil {
		return nil, err
	}

	// Define common actions to check if not specified
	actionsToCheck := req.Actions
	if len(actionsToCheck) == 0 {
		actionsToCheck = []string{"create", "read", "update", "delete", "list", "admin", "*"}
	}

	result := &EffectivePermissionsResult{
		Subject:                req.Subject,
		Permissions:            make(map[string]*PermissionSet),
		TotalPoliciesEvaluated: len(policies),
	}

	// Initialize permission sets for each resource pattern
	for _, resourcePattern := range req.ResourcePatterns {
		result.Permissions[resourcePattern] = &PermissionSet{
			Allowed: make([]string, 0),
			Denied:  make([]string, 0),
			Sources: make(map[string][]string),
		}
	}

	// Compile policies
	compiledPolicies := make([]*CompiledPolicy, 0, len(policies))
	for _, policy := range policies {
		compiled, err := e.compiler.Compile(policy)
		if err != nil {
			continue
		}
		compiledPolicies = append(compiledPolicies, compiled)
	}

	// For each resource pattern and action, check permissions
	for _, resourcePattern := range req.ResourcePatterns {
		permSet := result.Permissions[resourcePattern]

		for _, action := range actionsToCheck {
			checkReq := &CheckRequest{
				Subject:  *req.Subject,
				Action:   action,
				Resource: resourcePattern,
			}

			// Check against all policies
			isAllowed := false
			isDenied := false
			var allowingSources []string
			var denyingSources []string

			for _, compiled := range compiledPolicies {
				// Check subject match
				if !e.evaluator.matchesSubject(compiled.SubjectMatcher, req.Subject) {
					continue
				}

				// Check rules
				for _, rule := range compiled.Rules {
					if !matchAction(rule.Actions, checkReq.Action) {
						continue
					}

					var resourceMatches bool
					if rule.ResourceTrie != nil {
						_, _, resourceMatches = rule.ResourceTrie.Match(checkReq.Resource)
					}
					if resourceMatches {
						if rule.Effect == EffectDeny {
							isDenied = true
							denyingSources = append(denyingSources, compiled.Name+"/"+rule.Name)
						} else if rule.Effect == EffectAllow && !isDenied {
							isAllowed = true
							allowingSources = append(allowingSources, compiled.Name+"/"+rule.Name)
						}
					}
				}
			}

			// Record the permission
			if isDenied {
				permSet.Denied = append(permSet.Denied, action)
				if req.IncludeDenyDetails {
					permSet.Sources[action] = denyingSources
				}
			} else if isAllowed {
				permSet.Allowed = append(permSet.Allowed, action)
				permSet.Sources[action] = allowingSources
			}
		}
	}

	result.EvaluationTimeNs = time.Since(startTime).Nanoseconds()
	return result, nil
}

// Helper functions for simulation trace formatting.
func formatSubjectMatchDetails(subject *Subject, matched bool) string {
	if matched {
		return "subject matches policy constraints"
	}
	return "subject does not match: " + subject.ID
}

func formatActionMatchDetails(ruleActions []string, requestAction string) string {
	return "checking action '" + requestAction + "' against rule actions"
}

func formatResourceMatchDetails(resource string) string {
	return "checking resource: " + resource
}

// matchAction checks if the request action matches any of the rule's actions.
func matchAction(ruleActions []string, requestAction string) bool {
	for _, action := range ruleActions {
		if action == "*" || action == requestAction {
			return true
		}
	}
	return false
}

// MemoryPolicyCache is an in-memory implementation of PolicyCache.
type MemoryPolicyCache struct {
	mu      sync.RWMutex
	cache   map[string][]*CompiledPolicy
	ttl     time.Duration
	expires map[string]time.Time
}

// NewMemoryPolicyCache creates a new in-memory policy cache.
func NewMemoryPolicyCache(ttl time.Duration) *MemoryPolicyCache {
	return &MemoryPolicyCache{
		cache:   make(map[string][]*CompiledPolicy),
		expires: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// Get retrieves compiled policies for a tenant.
func (c *MemoryPolicyCache) Get(ctx context.Context, tenantID string) ([]*CompiledPolicy, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check expiration
	if expiry, ok := c.expires[tenantID]; ok {
		if time.Now().After(expiry) {
			// Expired, return nil
			return nil, nil
		}
	}

	policies, ok := c.cache[tenantID]
	if !ok {
		return nil, nil
	}

	return policies, nil
}

// Set stores compiled policies for a tenant.
func (c *MemoryPolicyCache) Set(ctx context.Context, tenantID string, policies []*CompiledPolicy) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[tenantID] = policies
	if c.ttl > 0 {
		c.expires[tenantID] = time.Now().Add(c.ttl)
	}

	return nil
}

// Invalidate removes cached policies for a tenant.
func (c *MemoryPolicyCache) Invalidate(ctx context.Context, tenantID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, tenantID)
	delete(c.expires, tenantID)

	return nil
}

// Clear removes all cached policies.
func (c *MemoryPolicyCache) Clear(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string][]*CompiledPolicy)
	c.expires = make(map[string]time.Time)

	return nil
}
