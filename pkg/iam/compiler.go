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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// PolicyCompiler compiles policies into an optimized form for fast evaluation.
type PolicyCompiler struct {
	mu    sync.RWMutex
	cache map[string]*CompiledPolicy
}

// NewPolicyCompiler creates a new policy compiler.
func NewPolicyCompiler() *PolicyCompiler {
	return &PolicyCompiler{
		cache: make(map[string]*CompiledPolicy),
	}
}

// Compile compiles a policy into its optimized form.
func (c *PolicyCompiler) Compile(policy *Policy) (*CompiledPolicy, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check cache
	fingerprint := computeFingerprint(policy)
	if cached, ok := c.cache[fingerprint]; ok {
		return cached, nil
	}

	// Validate policy first
	if err := policy.Validate(); err != nil {
		return nil, &CompilationFailedError{
			Policy: policy.Metadata.Name,
			Reason: fmt.Sprintf("validation failed: %v", err),
		}
	}

	// Compile subject matcher
	subjectMatcher, err := c.compileSubjectSelector(&policy.Spec.Subjects)
	if err != nil {
		return nil, &CompilationFailedError{
			Policy: policy.Metadata.Name,
			Reason: fmt.Sprintf("failed to compile subject selector: %v", err),
		}
	}

	// Compile rules
	rules := make([]*CompiledRule, 0, len(policy.Spec.Rules))
	for i, rule := range policy.Spec.Rules {
		compiledRule, err := c.compileRule(&rule, policy.Spec.Variables)
		if err != nil {
			return nil, &CompilationFailedError{
				Policy: policy.Metadata.Name,
				Reason: fmt.Sprintf("failed to compile rule %d (%s): %v", i, rule.Name, err),
			}
		}
		rules = append(rules, compiledRule)
	}

	// Sort rules by priority (higher priority first)
	// Deny rules should be evaluated before allow rules
	sort.Slice(rules, func(i, j int) bool {
		// Deny rules come first
		if rules[i].Effect == EffectDeny && rules[j].Effect != EffectDeny {
			return true
		}
		if rules[i].Effect != EffectDeny && rules[j].Effect == EffectDeny {
			return false
		}
		// Within same effect, sort by priority
		return rules[i].Priority > rules[j].Priority
	})

	// Compile variables
	variables := make(map[string]interface{})
	for k, v := range policy.Spec.Variables {
		variables[k] = v
	}

	compiled := &CompiledPolicy{
		Name:           policy.Metadata.Name,
		Priority:       policy.Metadata.Priority,
		SubjectMatcher: subjectMatcher,
		Rules:          rules,
		Variables:      variables,
	}

	// Cache the compiled policy
	c.cache[fingerprint] = compiled

	return compiled, nil
}

// compileSubjectSelector compiles a subject selector into an efficient matcher.
func (c *PolicyCompiler) compileSubjectSelector(selector *SubjectSelector) (*SubjectMatcher, error) {
	matcher := &SubjectMatcher{
		Users:         make(map[string]bool),
		Groups:        make(map[string]bool),
		Roles:         make(map[string]bool),
		Services:      make(map[string]bool),
		Matchers:      make([]AttributeMatcher, 0),
		Authenticated: selector.Authenticated,
		Any:           selector.Any,
	}

	// Build lookup maps for O(1) matching
	for _, user := range selector.Users {
		matcher.Users[user] = true
	}
	for _, group := range selector.Groups {
		matcher.Groups[group] = true
	}
	for _, role := range selector.Roles {
		matcher.Roles[role] = true
	}
	for _, service := range selector.Services {
		matcher.Services[service] = true
	}

	// Compile attribute matchers
	for key, value := range selector.Match {
		attrMatcher, err := c.compileAttributeMatcher(key, value)
		if err != nil {
			return nil, &AttributeMatcherCompilationError{Key: key, Cause: err}
		}
		matcher.Matchers = append(matcher.Matchers, attrMatcher)
	}

	return matcher, nil
}

// compileAttributeMatcher creates a matcher for subject attributes.
func (c *PolicyCompiler) compileAttributeMatcher(key string, value interface{}) (AttributeMatcher, error) {
	// Handle different types of attribute matching
	switch v := value.(type) {
	case string:
		// Exact match
		return &exactAttributeMatcher{key: key, value: v}, nil
	case map[string]interface{}:
		// Complex matcher with operators
		return c.compileComplexAttributeMatcher(key, v)
	default:
		return &exactAttributeMatcher{key: key, value: fmt.Sprintf("%v", v)}, nil
	}
}

func (c *PolicyCompiler) compileComplexAttributeMatcher(key string, matchers map[string]interface{}) (AttributeMatcher, error) {
	// Extract operator and value
	for op, val := range matchers {
		switch op {
		case "gte", ">=":
			return &numericAttributeMatcher{key: key, value: val, op: ">="}, nil
		case "gt", ">":
			return &numericAttributeMatcher{key: key, value: val, op: ">"}, nil
		case "lte", "<=":
			return &numericAttributeMatcher{key: key, value: val, op: "<="}, nil
		case "lt", "<":
			return &numericAttributeMatcher{key: key, value: val, op: "<"}, nil
		case "in":
			return &inAttributeMatcher{key: key, values: toStringSlice(val)}, nil
		case "endswith":
			return &stringAttributeMatcher{key: key, value: fmt.Sprintf("%v", val), op: "endswith"}, nil
		case "startswith":
			return &stringAttributeMatcher{key: key, value: fmt.Sprintf("%v", val), op: "startswith"}, nil
		case "contains":
			return &stringAttributeMatcher{key: key, value: fmt.Sprintf("%v", val), op: "contains"}, nil
		}
	}
	return nil, &AttributeMatcherError{Key: key}
}

// compileRule compiles a policy rule into an optimized form.
func (c *PolicyCompiler) compileRule(rule *Rule, variables map[string]string) (*CompiledRule, error) {
	// Build resource trie for fast pattern matching
	resourceTrie := NewResourceTrie()
	for _, resource := range rule.Resources {
		// Substitute variables in resource patterns
		resource = substituteVariables(resource, variables)
		resourceTrie.Insert(resource, rule.Name)
	}

	// Compile conditions
	parser := NewConditionParser(nil)
	conditions := make([]CompiledCondition, 0, len(rule.When))
	for _, condExpr := range rule.When {
		cond, err := parser.Parse(condExpr.Expression)
		if err != nil {
			return nil, &ConditionParseError{Expression: condExpr.Expression, Cause: err}
		}
		conditions = append(conditions, cond)
	}

	// Compile unless conditions
	unless := make([]CompiledCondition, 0, len(rule.Unless))
	for _, condExpr := range rule.Unless {
		cond, err := parser.Parse(condExpr.Expression)
		if err != nil {
			return nil, &UnlessConditionParseError{Expression: condExpr.Expression, Cause: err}
		}
		unless = append(unless, cond)
	}

	return &CompiledRule{
		Name:         rule.Name,
		Effect:       rule.Effect,
		Priority:     rule.Priority,
		Actions:      rule.Actions,
		ResourceTrie: resourceTrie,
		Conditions:   conditions,
		Unless:       unless,
	}, nil
}

// CompileCondition compiles a single condition expression.
func (c *PolicyCompiler) CompileCondition(expr string) (CompiledCondition, error) {
	parser := NewConditionParser(nil)
	return parser.Parse(expr)
}

// ClearCache clears the compilation cache.
func (c *PolicyCompiler) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]*CompiledPolicy)
}

// Attribute matcher implementations

type exactAttributeMatcher struct {
	key   string
	value string
}

func (m *exactAttributeMatcher) Match(subject *Subject) bool {
	if subject.Attributes == nil {
		return false
	}
	if val, ok := subject.Attributes[m.key]; ok {
		return fmt.Sprintf("%v", val) == m.value
	}
	return false
}

type numericAttributeMatcher struct {
	key   string
	value interface{}
	op    string
}

func (m *numericAttributeMatcher) Match(subject *Subject) bool {
	if subject.Attributes == nil {
		return false
	}
	attrVal, ok := subject.Attributes[m.key]
	if !ok {
		return false
	}

	left, lok := toFloat64(attrVal)
	right, rok := toFloat64(m.value)
	if !lok || !rok {
		return false
	}

	switch m.op {
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<":
		return left < right
	case "<=":
		return left <= right
	}
	return false
}

type stringAttributeMatcher struct {
	key   string
	value string
	op    string
}

func (m *stringAttributeMatcher) Match(subject *Subject) bool {
	if subject.Attributes == nil {
		return false
	}
	attrVal, ok := subject.Attributes[m.key]
	if !ok {
		return false
	}

	str := fmt.Sprintf("%v", attrVal)

	switch m.op {
	case "endswith":
		return evalEndsWith(str, m.value)
	case "startswith":
		return evalStartsWith(str, m.value)
	case "contains":
		return evalContains(str, m.value)
	}
	return false
}

type inAttributeMatcher struct {
	key    string
	values []string
}

func (m *inAttributeMatcher) Match(subject *Subject) bool {
	if subject.Attributes == nil {
		return false
	}
	attrVal, ok := subject.Attributes[m.key]
	if !ok {
		return false
	}

	str := fmt.Sprintf("%v", attrVal)
	for _, v := range m.values {
		if str == v {
			return true
		}
	}
	return false
}

// Helper functions

func substituteVariables(pattern string, variables map[string]string) string {
	result := pattern
	for k, v := range variables {
		placeholder := fmt.Sprintf("${%s}", k)
		result = replaceAll(result, placeholder, v)
	}
	return result
}

func replaceAll(s, old, new string) string {
	// Simple string replacement
	for {
		newS := replace(s, old, new)
		if newS == s {
			break
		}
		s = newS
	}
	return s
}

func replace(s, old, new string) string {
	if old == "" {
		return s
	}
	idx := indexOf(s, old)
	if idx == -1 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func toStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []interface{}:
		result := make([]string, len(val))
		for i, item := range val {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result
	case string:
		return []string{val}
	default:
		return []string{fmt.Sprintf("%v", v)}
	}
}

func computeFingerprint(policy *Policy) string {
	// Compute SHA256 hash of policy JSON
	data, err := json.Marshal(policy)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
