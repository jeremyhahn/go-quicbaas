//go:build iam

package iam

import (
	"context"
	"testing"
	"time"
)

func TestPolicyEvaluator_Evaluate(t *testing.T) {
	compiler := NewPolicyCompiler()
	cache := NewPolicyCache(NewCacheConfig(&PEPConfig{
		TenantID: "test-tenant",
		CacheTTL: 5 * time.Minute,
	}))
	evaluator := NewPolicyEvaluator(compiler, cache)

	policy := &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: PolicyMetadata{
			Name:     "test-policy",
			Tenant:   "test-tenant",
			Priority: 100,
		},
		Spec: PolicySpec{
			Subjects: SubjectSelector{
				Authenticated: true,
			},
			Rules: []Rule{
				{
					Name:      "allow-read",
					Effect:    EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"/data/*"},
				},
				{
					Name:      "deny-delete",
					Effect:    EffectDeny,
					Actions:   []string{"delete"},
					Resources: []string{"/data/*"},
				},
			},
		},
	}

	compiled, err := compiler.Compile(policy)
	if err != nil {
		t.Fatalf("Failed to compile policy: %v", err)
	}
	cache.Set("test-tenant", compiled)

	ctx := context.Background()

	tests := []struct {
		name        string
		req         *CheckRequest
		wantAllowed bool
		wantReason  string
	}{
		{
			name: "valid request - allowed",
			req: &CheckRequest{
				Subject: Subject{
					ID:            "user-123",
					TenantID:      "test-tenant",
					Authenticated: true,
				},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			wantAllowed: true,
		},
		{
			name: "valid request - denied by explicit deny",
			req: &CheckRequest{
				Subject: Subject{
					ID:            "user-123",
					TenantID:      "test-tenant",
					Authenticated: true,
				},
				Action:   "delete",
				Resource: "/data/file.txt",
			},
			wantAllowed: false,
		},
		{
			name: "no matching rule - default deny",
			req: &CheckRequest{
				Subject: Subject{
					ID:            "user-123",
					TenantID:      "test-tenant",
					Authenticated: true,
				},
				Action:   "write",
				Resource: "/data/file.txt",
			},
			wantAllowed: false,
			wantReason:  "no matching allow policy",
		},
		{
			name:        "nil request",
			req:         nil,
			wantAllowed: false,
			wantReason:  "invalid request: subject required",
		},
		{
			name: "missing subject ID",
			req: &CheckRequest{
				Subject: Subject{
					TenantID:      "test-tenant",
					Authenticated: true,
				},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			wantAllowed: false,
			wantReason:  "invalid request: subject required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := evaluator.Evaluate(ctx, tt.req)
			if err != nil {
				t.Errorf("Evaluate() error = %v", err)
				return
			}
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("Evaluate() allowed = %v, want %v", resp.Allowed, tt.wantAllowed)
			}
			if tt.wantReason != "" && resp.Reason != tt.wantReason {
				t.Errorf("Evaluate() reason = %v, want %v", resp.Reason, tt.wantReason)
			}
			if resp.EvaluationTimeNs <= 0 {
				t.Error("EvaluationTimeNs should be > 0")
			}
		})
	}
}

func TestPolicyEvaluator_ActionMatching(t *testing.T) {
	evaluator := &PolicyEvaluator{}

	tests := []struct {
		name          string
		ruleActions   []string
		requestAction string
		wantMatch     bool
	}{
		{name: "exact match", ruleActions: []string{"read", "write"}, requestAction: "read", wantMatch: true},
		{name: "wildcard match", ruleActions: []string{"*"}, requestAction: "anything", wantMatch: true},
		{name: "prefix match", ruleActions: []string{"write*"}, requestAction: "write:key", wantMatch: true},
		{name: "prefix match exact", ruleActions: []string{"write*"}, requestAction: "write", wantMatch: true},
		{name: "no match", ruleActions: []string{"read", "list"}, requestAction: "delete", wantMatch: false},
		{name: "empty list", ruleActions: []string{}, requestAction: "read", wantMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := evaluator.matchesAction(tt.ruleActions, tt.requestAction); result != tt.wantMatch {
				t.Errorf("matchesAction() = %v, want %v", result, tt.wantMatch)
			}
		})
	}
}

func TestPolicyEvaluator_SubjectMatchingEdgeCases(t *testing.T) {
	compiler := NewPolicyCompiler()
	cache := NewPolicyCache(NewCacheConfig(&PEPConfig{
		TenantID: "test-tenant",
		CacheTTL: 5 * time.Minute,
	}))
	evaluator := NewPolicyEvaluator(compiler, cache)
	ctx := context.Background()

	tests := []struct {
		name        string
		policy      *Policy
		subject     Subject
		action      string
		resource    string
		wantAllowed bool
	}{
		{
			name: "service matching",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "svc", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{Services: []string{"api-service"}},
					Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"/**"}}},
				},
			},
			subject:     Subject{ID: "api-service", TenantID: "test-tenant", Authenticated: true},
			action:      "write",
			resource:    "/data/file.txt",
			wantAllowed: true,
		},
		{
			name: "empty matcher matches all",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "empty", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{},
					Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"/public/*"}}},
				},
			},
			subject:     Subject{ID: "anyone", TenantID: "test-tenant"},
			action:      "read",
			resource:    "/public/data.txt",
			wantAllowed: true,
		},
		{
			name: "authenticated only",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "auth", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{Authenticated: true},
					Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"/data/*"}}},
				},
			},
			subject:     Subject{ID: "user", TenantID: "test-tenant", Authenticated: true},
			action:      "read",
			resource:    "/data/file.txt",
			wantAllowed: true,
		},
		{
			name: "group match",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "grp", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{Groups: []string{"editors"}},
					Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"/data/*"}}},
				},
			},
			subject:     Subject{ID: "user", TenantID: "test-tenant", Groups: []string{"users", "editors"}, Authenticated: true},
			action:      "read",
			resource:    "/data/file.txt",
			wantAllowed: true,
		},
		{
			name: "role match",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "role", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{Roles: []string{"admin"}},
					Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"delete"}, Resources: []string{"/data/*"}}},
				},
			},
			subject:     Subject{ID: "user", TenantID: "test-tenant", Roles: []string{"admin"}, Authenticated: true},
			action:      "delete",
			resource:    "/data/file.txt",
			wantAllowed: true,
		},
		{
			name: "no match with constraints",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "nomatch", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{Users: []string{"admin"}, Groups: []string{"admins"}},
					Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"/data/*"}}},
				},
			},
			subject:     Subject{ID: "user", TenantID: "test-tenant", Groups: []string{"users"}, Authenticated: true},
			action:      "read",
			resource:    "/data/file.txt",
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := compiler.Compile(tt.policy)
			if err != nil {
				t.Fatalf("Failed to compile: %v", err)
			}
			cache.Set("test-tenant", compiled)
			resp, err := evaluator.Evaluate(ctx, &CheckRequest{Subject: tt.subject, Action: tt.action, Resource: tt.resource})
			if err != nil {
				t.Errorf("Evaluate() error = %v", err)
			}
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("allowed = %v, want %v", resp.Allowed, tt.wantAllowed)
			}
			cache.Clear()
		})
	}
}

func TestPolicyEvaluator_NilCache(t *testing.T) {
	evaluator := NewPolicyEvaluator(NewPolicyCompiler(), nil)
	resp, err := evaluator.Evaluate(context.Background(), &CheckRequest{
		Subject:  Subject{ID: "user", TenantID: "test", Authenticated: true},
		Action:   "read",
		Resource: "/data/file.txt",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.Allowed {
		t.Error("should deny with nil cache")
	}
	if resp.Reason != "no matching allow policy" {
		t.Errorf("reason = %v, want 'no matching allow policy'", resp.Reason)
	}
}

func TestPolicyEvaluator_ResourceTrieNoMatch(t *testing.T) {
	compiler := NewPolicyCompiler()
	cache := NewPolicyCache(NewCacheConfig(&PEPConfig{TenantID: "test-tenant", CacheTTL: 5 * time.Minute}))
	evaluator := NewPolicyEvaluator(compiler, cache)

	policy := &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "p1", Tenant: "test-tenant"},
		Spec: PolicySpec{
			Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"/data/*"}}},
		},
	}

	compiled, _ := compiler.Compile(policy)
	cache.Set("test-tenant", compiled)

	resp, _ := evaluator.Evaluate(context.Background(), &CheckRequest{
		Subject:  Subject{ID: "user", TenantID: "test-tenant", Authenticated: true},
		Action:   "read",
		Resource: "/admin/config.txt",
	})

	if resp.Allowed {
		t.Error("should deny when resource doesn't match")
	}
}

func TestPolicyEvaluator_AttributeMatchers(t *testing.T) {
	compiler := NewPolicyCompiler()
	cache := NewPolicyCache(NewCacheConfig(&PEPConfig{TenantID: "test-tenant", CacheTTL: 5 * time.Minute}))
	evaluator := NewPolicyEvaluator(compiler, cache)

	tests := []struct {
		name        string
		match       map[string]interface{}
		attrs       map[string]interface{}
		wantAllowed bool
	}{
		{
			name:        "exact match passes",
			match:       map[string]interface{}{"department": "engineering"},
			attrs:       map[string]interface{}{"department": "engineering"},
			wantAllowed: true,
		},
		{
			name:        "exact match fails",
			match:       map[string]interface{}{"department": "engineering"},
			attrs:       map[string]interface{}{"department": "sales"},
			wantAllowed: false,
		},
		{
			name:        "numeric gte passes",
			match:       map[string]interface{}{"level": map[string]interface{}{"gte": 3}},
			attrs:       map[string]interface{}{"level": 5},
			wantAllowed: true,
		},
		{
			name:        "numeric gte fails",
			match:       map[string]interface{}{"level": map[string]interface{}{"gte": 5}},
			attrs:       map[string]interface{}{"level": 3},
			wantAllowed: false,
		},
		{
			name: "multiple matchers all pass",
			match: map[string]interface{}{
				"department": "engineering",
				"level":      map[string]interface{}{"gte": 3},
			},
			attrs:       map[string]interface{}{"department": "engineering", "level": 5},
			wantAllowed: true,
		},
		{
			name: "multiple matchers one fails",
			match: map[string]interface{}{
				"department": "engineering",
				"level":      map[string]interface{}{"gte": 10},
			},
			attrs:       map[string]interface{}{"department": "engineering", "level": 5},
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "attr", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{Match: tt.match},
					Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"/data/*"}}},
				},
			}

			compiled, err := compiler.Compile(policy)
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}
			cache.Set("test-tenant", compiled)

			resp, err := evaluator.Evaluate(context.Background(), &CheckRequest{
				Subject:  Subject{ID: "user", TenantID: "test-tenant", Authenticated: true, Attributes: tt.attrs},
				Action:   "read",
				Resource: "/data/file.txt",
			})

			if err != nil {
				t.Errorf("Evaluate error: %v", err)
			}
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("allowed = %v, want %v", resp.Allowed, tt.wantAllowed)
			}

			cache.Clear()
		})
	}
}

func BenchmarkPolicyEvaluator_Evaluate(b *testing.B) {
	compiler := NewPolicyCompiler()
	cache := NewPolicyCache(NewCacheConfig(&PEPConfig{TenantID: "test-tenant", CacheTTL: 5 * time.Minute}))
	evaluator := NewPolicyEvaluator(compiler, cache)

	policy := &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "bench", Tenant: "test-tenant", Priority: 100},
		Spec: PolicySpec{
			Subjects: SubjectSelector{Authenticated: true},
			Rules:    []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"/data/*"}}},
		},
	}

	compiled, _ := compiler.Compile(policy)
	cache.Set("test-tenant", compiled)

	req := &CheckRequest{
		Subject:  Subject{ID: "user-123", TenantID: "test-tenant", Authenticated: true},
		Action:   "read",
		Resource: "/data/file.txt",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = evaluator.Evaluate(context.Background(), req)
	}
}

func TestPolicyEvaluator_ConditionPaths(t *testing.T) {
	compiler := NewPolicyCompiler()
	cache := NewPolicyCache(NewCacheConfig(&PEPConfig{TenantID: "test-tenant", CacheTTL: 5 * time.Minute}))
	evaluator := NewPolicyEvaluator(compiler, cache)
	ctx := context.Background()

	tests := []struct {
		name        string
		policy      *Policy
		subject     Subject
		wantAllowed bool
	}{
		{
			name: "unless condition blocks rule",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "unless", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Rules: []Rule{{
						Name:      "r1",
						Effect:    EffectAllow,
						Actions:   []string{"read"},
						Resources: []string{"/data/*"},
						Unless:    []Condition{{Expression: "subject.attrs.blocked"}},
					}},
				},
			},
			subject:     Subject{ID: "user", TenantID: "test-tenant", Authenticated: true, Attributes: map[string]interface{}{"blocked": true}},
			wantAllowed: false,
		},
		{
			name: "when condition passes",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "when", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Rules: []Rule{{
						Name:      "r1",
						Effect:    EffectAllow,
						Actions:   []string{"read"},
						Resources: []string{"/data/*"},
						When:      []Condition{{Expression: "subject.attrs.active"}},
					}},
				},
			},
			subject:     Subject{ID: "user", TenantID: "test-tenant", Authenticated: true, Attributes: map[string]interface{}{"active": true}},
			wantAllowed: true,
		},
		{
			name: "when condition fails",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "when-fail", Tenant: "test-tenant"},
				Spec: PolicySpec{
					Rules: []Rule{{
						Name:      "r1",
						Effect:    EffectAllow,
						Actions:   []string{"read"},
						Resources: []string{"/data/*"},
						When:      []Condition{{Expression: "subject.attrs.premium"}},
					}},
				},
			},
			subject:     Subject{ID: "user", TenantID: "test-tenant", Authenticated: true, Attributes: map[string]interface{}{"premium": false}},
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := compiler.Compile(tt.policy)
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}
			cache.Set("test-tenant", compiled)

			resp, err := evaluator.Evaluate(ctx, &CheckRequest{
				Subject:  tt.subject,
				Action:   "read",
				Resource: "/data/file.txt",
			})

			if err != nil {
				t.Errorf("Evaluate error: %v", err)
			}
			if resp.Allowed != tt.wantAllowed {
				t.Errorf("allowed = %v, want %v", resp.Allowed, tt.wantAllowed)
			}

			cache.Clear()
		})
	}
}

// TestValidateCapturedVars tests the validateCapturedVars function
// which validates captured variables from resource matching.
func TestValidateCapturedVars(t *testing.T) {
	evaluator := &PolicyEvaluator{}

	tests := []struct {
		name         string
		capturedVars map[string]string
		subject      *Subject
		wantResult   bool
	}{
		{
			name:         "empty captured vars",
			capturedVars: map[string]string{},
			subject:      &Subject{ID: "user1"},
			wantResult:   true,
		},
		{
			name:         "non-subject variable",
			capturedVars: map[string]string{"resource_id": "123"},
			subject:      &Subject{ID: "user1"},
			wantResult:   true,
		},
		{
			name:         "subject.id matches",
			capturedVars: map[string]string{"subject.id": "user1"},
			subject:      &Subject{ID: "user1"},
			wantResult:   true,
		},
		{
			name:         "subject.id does not match",
			capturedVars: map[string]string{"subject.id": "user2"},
			subject:      &Subject{ID: "user1"},
			wantResult:   false,
		},
		{
			name:         "subject.name matches",
			capturedVars: map[string]string{"subject.name": "John Doe"},
			subject:      &Subject{ID: "user1", Name: "John Doe"},
			wantResult:   true,
		},
		{
			name:         "subject.name does not match",
			capturedVars: map[string]string{"subject.name": "Jane Doe"},
			subject:      &Subject{ID: "user1", Name: "John Doe"},
			wantResult:   false,
		},
		{
			name:         "subject.email matches",
			capturedVars: map[string]string{"subject.email": "user@example.com"},
			subject:      &Subject{ID: "user1", Email: "user@example.com"},
			wantResult:   true,
		},
		{
			name:         "subject.email does not match",
			capturedVars: map[string]string{"subject.email": "other@example.com"},
			subject:      &Subject{ID: "user1", Email: "user@example.com"},
			wantResult:   false,
		},
		{
			name:         "subject.tenant matches",
			capturedVars: map[string]string{"subject.tenant": "tenant1"},
			subject:      &Subject{ID: "user1", Tenant: "tenant1"},
			wantResult:   true,
		},
		{
			name:         "subject.tenant does not match",
			capturedVars: map[string]string{"subject.tenant": "tenant2"},
			subject:      &Subject{ID: "user1", Tenant: "tenant1"},
			wantResult:   false,
		},
		{
			name:         "subject.tenant_id matches",
			capturedVars: map[string]string{"subject.tenant_id": "tenant1"},
			subject:      &Subject{ID: "user1", TenantID: "tenant1"},
			wantResult:   true,
		},
		{
			name:         "subject.tenant_id does not match",
			capturedVars: map[string]string{"subject.tenant_id": "tenant2"},
			subject:      &Subject{ID: "user1", TenantID: "tenant1"},
			wantResult:   false,
		},
		{
			name:         "subject attribute from map matches",
			capturedVars: map[string]string{"subject.department": "engineering"},
			subject:      &Subject{ID: "user1", Attributes: map[string]interface{}{"department": "engineering"}},
			wantResult:   true,
		},
		{
			name:         "subject attribute from map does not match",
			capturedVars: map[string]string{"subject.department": "sales"},
			subject:      &Subject{ID: "user1", Attributes: map[string]interface{}{"department": "engineering"}},
			wantResult:   false,
		},
		{
			name:         "subject attribute not in map - passes",
			capturedVars: map[string]string{"subject.department": "engineering"},
			subject:      &Subject{ID: "user1", Attributes: nil},
			wantResult:   true,
		},
		{
			name:         "subject attribute not a string - passes",
			capturedVars: map[string]string{"subject.level": "5"},
			subject:      &Subject{ID: "user1", Attributes: map[string]interface{}{"level": 5}},
			wantResult:   true,
		},
		{
			name: "multiple captured vars all match",
			capturedVars: map[string]string{
				"subject.id":        "user1",
				"subject.tenant_id": "tenant1",
			},
			subject:    &Subject{ID: "user1", TenantID: "tenant1"},
			wantResult: true,
		},
		{
			name: "multiple captured vars one does not match",
			capturedVars: map[string]string{
				"subject.id":        "user1",
				"subject.tenant_id": "tenant2",
			},
			subject:    &Subject{ID: "user1", TenantID: "tenant1"},
			wantResult: false,
		},
		{
			name:         "short subject variable name",
			capturedVars: map[string]string{"subject": "value"},
			subject:      &Subject{ID: "user1"},
			wantResult:   true,
		},
		{
			name:         "unknown subject attribute",
			capturedVars: map[string]string{"subject.unknown_field": "value"},
			subject:      &Subject{ID: "user1"},
			wantResult:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.validateCapturedVars(tt.capturedVars, tt.subject)
			if result != tt.wantResult {
				t.Errorf("validateCapturedVars() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

// TestResourceTrieExtractVariableName tests the extractVariableName function
func TestResourceTrieExtractVariableName(t *testing.T) {
	tests := []struct {
		name     string
		segment  string
		wantName string
	}{
		{
			name:     "dollar-brace format",
			segment:  "${userId}",
			wantName: "userId",
		},
		{
			name:     "colon format",
			segment:  ":userId",
			wantName: "userId",
		},
		{
			name:     "brace format",
			segment:  "{userId}",
			wantName: "userId",
		},
		{
			name:     "plain segment - returned as is",
			segment:  "userId",
			wantName: "userId",
		},
		{
			name:     "empty dollar-brace",
			segment:  "${}",
			wantName: "",
		},
		{
			name:     "empty brace",
			segment:  "{}",
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVariableName(tt.segment)
			if result != tt.wantName {
				t.Errorf("extractVariableName(%q) = %q, want %q", tt.segment, result, tt.wantName)
			}
		})
	}
}

// TestResourceTrieSplitPath tests the splitPath function
func TestResourceTrieSplitPath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantResult []string
	}{
		{
			name:       "empty path",
			path:       "",
			wantResult: []string{},
		},
		{
			name:       "single segment",
			path:       "users",
			wantResult: []string{"users"},
		},
		{
			name:       "multiple segments",
			path:       "users/123/profile",
			wantResult: []string{"users", "123", "profile"},
		},
		{
			name:       "leading slash",
			path:       "/users/123",
			wantResult: []string{"users", "123"},
		},
		{
			name:       "trailing slash",
			path:       "users/123/",
			wantResult: []string{"users", "123"},
		},
		{
			name:       "both leading and trailing slash",
			path:       "/users/123/",
			wantResult: []string{"users", "123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitPath(tt.path)
			if len(result) != len(tt.wantResult) {
				t.Errorf("splitPath(%q) returned %d segments, want %d", tt.path, len(result), len(tt.wantResult))
				return
			}
			for i := range result {
				if result[i] != tt.wantResult[i] {
					t.Errorf("splitPath(%q)[%d] = %q, want %q", tt.path, i, result[i], tt.wantResult[i])
				}
			}
		})
	}
}

// TestPEPCheck tests the PEP Check function
func TestPEPCheck(t *testing.T) {
	tests := []struct {
		name      string
		config    *PEPConfig
		req       *CheckRequest
		setup     func(*PEP)
		wantAllow bool
		wantError bool
	}{
		{
			name:   "valid request - allowed",
			config: DefaultPEPConfig("tenant1"),
			req: &CheckRequest{
				Subject: Subject{
					ID:            "user1",
					TenantID:      "tenant1",
					Authenticated: true,
				},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			setup: func(p *PEP) {
				policy := &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Authenticated: true},
						Rules: []Rule{{
							Name: "allow-read", Effect: EffectAllow,
							Actions: []string{"read"}, Resources: []string{"/data/*"},
						}},
					},
				}
				compiled, _ := p.compiler.Compile(policy)
				p.cache.Set("tenant1", compiled)
			},
			wantAllow: true,
			wantError: false,
		},
		{
			name:      "nil request",
			config:    DefaultPEPConfig("tenant1"),
			req:       nil,
			setup:     func(p *PEP) {},
			wantAllow: false,
			wantError: true,
		},
		{
			name:   "missing subject ID",
			config: DefaultPEPConfig("tenant1"),
			req: &CheckRequest{
				Subject:  Subject{TenantID: "tenant1"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			setup:     func(p *PEP) {},
			wantAllow: false,
			wantError: true,
		},
		{
			name:   "missing tenant ID",
			config: DefaultPEPConfig("tenant1"),
			req: &CheckRequest{
				Subject:  Subject{ID: "user1"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			setup:     func(p *PEP) {},
			wantAllow: false,
			wantError: true,
		},
		{
			name:   "tenant ID mismatch",
			config: DefaultPEPConfig("tenant1"),
			req: &CheckRequest{
				Subject:  Subject{ID: "user1", TenantID: "tenant2"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			setup:     func(p *PEP) {},
			wantAllow: false,
			wantError: true,
		},
		{
			name:   "missing action",
			config: DefaultPEPConfig("tenant1"),
			req: &CheckRequest{
				Subject:  Subject{ID: "user1", TenantID: "tenant1"},
				Action:   "",
				Resource: "/data/file.txt",
			},
			setup:     func(p *PEP) {},
			wantAllow: false,
			wantError: true,
		},
		{
			name:   "missing resource",
			config: DefaultPEPConfig("tenant1"),
			req: &CheckRequest{
				Subject: Subject{ID: "user1", TenantID: "tenant1"},
				Action:  "read",
			},
			setup:     func(p *PEP) {},
			wantAllow: false,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pep := NewPEP(tt.config)
			tt.setup(pep)

			resp, err := pep.Check(context.Background(), tt.req)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Fatal("expected response, got nil")
				}
				if resp.Allowed != tt.wantAllow {
					t.Errorf("allowed = %v, want %v", resp.Allowed, tt.wantAllow)
				}
			}
		})
	}
}

// TestPEPCheckClosed tests Check on a closed PEP
func TestPEPCheckClosed(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))
	pep.Close()

	_, err := pep.Check(context.Background(), &CheckRequest{
		Subject:  Subject{ID: "user1", TenantID: "tenant1"},
		Action:   "read",
		Resource: "/data/file.txt",
	})

	if err != ErrPEPClosed {
		t.Errorf("expected ErrPEPClosed, got %v", err)
	}
}

// TestPEPCheckBatch tests the PEP CheckBatch function
func TestPEPCheckBatch(t *testing.T) {
	tests := []struct {
		name          string
		config        *PEPConfig
		req           *BatchCheckRequest
		setup         func(*PEP)
		wantResponses int
		wantError     bool
	}{
		{
			name:   "valid batch request",
			config: DefaultPEPConfig("tenant1"),
			req: &BatchCheckRequest{
				Checks: []*CheckRequest{
					{
						Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
						Action:   "read",
						Resource: "/data/file1.txt",
					},
					{
						Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
						Action:   "read",
						Resource: "/data/file2.txt",
					},
				},
			},
			setup: func(p *PEP) {
				policy := &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Authenticated: true},
						Rules: []Rule{{
							Name: "allow-read", Effect: EffectAllow,
							Actions: []string{"read"}, Resources: []string{"/data/*"},
						}},
					},
				}
				compiled, _ := p.compiler.Compile(policy)
				p.cache.Set("tenant1", compiled)
			},
			wantResponses: 2,
			wantError:     false,
		},
		{
			name:      "nil request",
			config:    DefaultPEPConfig("tenant1"),
			req:       nil,
			setup:     func(p *PEP) {},
			wantError: true,
		},
		{
			name:   "empty checks",
			config: DefaultPEPConfig("tenant1"),
			req: &BatchCheckRequest{
				Checks: []*CheckRequest{},
			},
			setup:     func(p *PEP) {},
			wantError: true,
		},
		{
			name:   "batch with one invalid request",
			config: DefaultPEPConfig("tenant1"),
			req: &BatchCheckRequest{
				Checks: []*CheckRequest{
					{
						Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
						Action:   "read",
						Resource: "/data/file1.txt",
					},
					{
						Subject:  Subject{ID: "", TenantID: "tenant1"}, // Invalid - missing ID
						Action:   "read",
						Resource: "/data/file2.txt",
					},
				},
			},
			setup:         func(p *PEP) {},
			wantResponses: 2, // Both get responses, one is error
			wantError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pep := NewPEP(tt.config)
			tt.setup(pep)

			resp, err := pep.CheckBatch(context.Background(), tt.req)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Fatal("expected response, got nil")
				}
				if len(resp.Responses) != tt.wantResponses {
					t.Errorf("got %d responses, want %d", len(resp.Responses), tt.wantResponses)
				}
			}
		})
	}
}

// TestPEPCheckBatchClosed tests CheckBatch on a closed PEP
func TestPEPCheckBatchClosed(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))
	pep.Close()

	_, err := pep.CheckBatch(context.Background(), &BatchCheckRequest{
		Checks: []*CheckRequest{
			{
				Subject:  Subject{ID: "user1", TenantID: "tenant1"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
		},
	})

	if err != ErrPEPClosed {
		t.Errorf("expected ErrPEPClosed, got %v", err)
	}
}

// TestPEPLoadPolicy tests the LoadPolicy function
func TestPEPLoadPolicy(t *testing.T) {
	tests := []struct {
		name      string
		policy    *Policy
		wantError bool
	}{
		{
			name: "valid policy",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
				Spec: PolicySpec{
					Subjects: SubjectSelector{Authenticated: true},
					Rules: []Rule{{
						Name: "allow-read", Effect: EffectAllow,
						Actions: []string{"read"}, Resources: []string{"/data/*"},
					}},
				},
			},
			wantError: false,
		},
		{
			name: "invalid policy - no rules",
			policy: &Policy{
				APIVersion: "abac.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
				Spec: PolicySpec{
					Rules: []Rule{},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pep := NewPEP(DefaultPEPConfig("tenant1"))

			err := pep.LoadPolicy(context.Background(), tt.policy)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Verify policy is in cache
				policies := pep.cache.GetAll("tenant1")
				if len(policies) == 0 {
					t.Error("policy was not loaded into cache")
				}
			}
		})
	}
}

// TestPEPLoadPolicyClosed tests LoadPolicy on a closed PEP
func TestPEPLoadPolicyClosed(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))
	pep.Close()

	err := pep.LoadPolicy(context.Background(), &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
		Spec: PolicySpec{
			Rules: []Rule{{
				Name: "allow", Effect: EffectAllow,
				Actions: []string{"*"}, Resources: []string{"/**"},
			}},
		},
	})

	if err != ErrPEPClosed {
		t.Errorf("expected ErrPEPClosed, got %v", err)
	}
}

// TestPEPRemovePolicy tests the RemovePolicy function
func TestPEPRemovePolicy(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))

	// Load a policy first
	policy := &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
		Spec: PolicySpec{
			Subjects: SubjectSelector{Authenticated: true},
			Rules: []Rule{{
				Name: "allow-read", Effect: EffectAllow,
				Actions: []string{"read"}, Resources: []string{"/data/*"},
			}},
		},
	}
	err := pep.LoadPolicy(context.Background(), policy)
	if err != nil {
		t.Fatalf("failed to load policy: %v", err)
	}

	// Verify it's there
	policies := pep.cache.GetAll("tenant1")
	if len(policies) == 0 {
		t.Fatal("policy was not loaded")
	}

	// Remove it
	pep.RemovePolicy("test")

	// Verify it's gone (cache invalidated for tenant)
	policies = pep.cache.GetAll("tenant1")
	found := false
	for _, p := range policies {
		if p.Name == "test" {
			found = true
			break
		}
	}
	if found {
		t.Error("policy was not removed")
	}
}

// TestPEPRemovePolicyClosed tests RemovePolicy on a closed PEP
func TestPEPRemovePolicyClosed(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))
	pep.Close()

	// Should not panic
	pep.RemovePolicy("test")
}

// TestPEPValidateCheckRequest tests the validateCheckRequest function
func TestPEPValidateCheckRequest(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))

	tests := []struct {
		name    string
		req     *CheckRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
			errMsg:  "request is nil",
		},
		{
			name: "missing subject ID",
			req: &CheckRequest{
				Subject:  Subject{TenantID: "tenant1"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			wantErr: true,
			errMsg:  "subject ID is required",
		},
		{
			name: "missing tenant ID",
			req: &CheckRequest{
				Subject:  Subject{ID: "user1"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			wantErr: true,
			errMsg:  "subject tenant ID is required",
		},
		{
			name: "tenant ID mismatch",
			req: &CheckRequest{
				Subject:  Subject{ID: "user1", TenantID: "tenant2"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			wantErr: true,
			errMsg:  "tenant ID does not match",
		},
		{
			name: "missing action",
			req: &CheckRequest{
				Subject:  Subject{ID: "user1", TenantID: "tenant1"},
				Resource: "/data/file.txt",
			},
			wantErr: true,
			errMsg:  "action is required",
		},
		{
			name: "missing resource",
			req: &CheckRequest{
				Subject: Subject{ID: "user1", TenantID: "tenant1"},
				Action:  "read",
			},
			wantErr: true,
			errMsg:  "resource is required",
		},
		{
			name: "valid request",
			req: &CheckRequest{
				Subject:  Subject{ID: "user1", TenantID: "tenant1"},
				Action:   "read",
				Resource: "/data/file.txt",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pep.validateCheckRequest(tt.req)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestPolicyEngineSimulatePolicy tests the SimulatePolicy function
func TestPolicyEngineSimulatePolicy(t *testing.T) {
	storage := newMockPolicyStorage()
	cache := newTestPolicyCache()
	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
		Cache:   cache,
	})
	ctx := context.Background()

	tests := []struct {
		name      string
		req       *PolicySimulationRequest
		wantAllow bool
		wantError bool
	}{
		{
			name:      "nil request",
			req:       nil,
			wantError: true,
		},
		{
			name: "nil policy",
			req: &PolicySimulationRequest{
				Policy:       nil,
				CheckRequest: &CheckRequest{},
			},
			wantError: true,
		},
		{
			name: "nil check request",
			req: &PolicySimulationRequest{
				Policy:       createTestPolicy("tenant1", "test"),
				CheckRequest: nil,
			},
			wantError: true,
		},
		{
			name: "invalid policy",
			req: &PolicySimulationRequest{
				Policy: &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec:       PolicySpec{Rules: []Rule{}},
				},
				CheckRequest: &CheckRequest{
					Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
					Action:   "read",
					Resource: "/test/file.txt",
				},
			},
			wantError: true,
		},
		{
			name: "subject does not match",
			req: &PolicySimulationRequest{
				Policy: &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Users: []string{"admin"}},
						Rules: []Rule{{
							Name: "allow", Effect: EffectAllow,
							Actions: []string{"read"}, Resources: []string{"/test/*"},
						}},
					},
				},
				CheckRequest: &CheckRequest{
					Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
					Action:   "read",
					Resource: "/test/file.txt",
				},
			},
			wantAllow: false,
			wantError: false,
		},
		{
			name: "allow - subject matches, action matches, resource matches",
			req: &PolicySimulationRequest{
				Policy: &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Authenticated: true},
						Rules: []Rule{{
							Name: "allow", Effect: EffectAllow,
							Actions: []string{"read"}, Resources: []string{"/test/*"},
						}},
					},
				},
				CheckRequest: &CheckRequest{
					Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
					Action:   "read",
					Resource: "/test/file.txt",
				},
			},
			wantAllow: true,
			wantError: false,
		},
		{
			name: "deny - explicit deny rule",
			req: &PolicySimulationRequest{
				Policy: &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Authenticated: true},
						Rules: []Rule{{
							Name: "deny", Effect: EffectDeny,
							Actions: []string{"delete"}, Resources: []string{"/test/*"},
						}},
					},
				},
				CheckRequest: &CheckRequest{
					Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
					Action:   "delete",
					Resource: "/test/file.txt",
				},
			},
			wantAllow: false,
			wantError: false,
		},
		{
			name: "with trace enabled",
			req: &PolicySimulationRequest{
				Policy: &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Authenticated: true},
						Rules: []Rule{{
							Name: "allow", Effect: EffectAllow,
							Actions: []string{"read"}, Resources: []string{"/test/*"},
						}},
					},
				},
				CheckRequest: &CheckRequest{
					Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
					Action:   "read",
					Resource: "/test/file.txt",
				},
				IncludeTrace: true,
			},
			wantAllow: true,
			wantError: false,
		},
		{
			name: "action does not match",
			req: &PolicySimulationRequest{
				Policy: &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Authenticated: true},
						Rules: []Rule{{
							Name: "allow", Effect: EffectAllow,
							Actions: []string{"write"}, Resources: []string{"/test/*"},
						}},
					},
				},
				CheckRequest: &CheckRequest{
					Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
					Action:   "read",
					Resource: "/test/file.txt",
				},
			},
			wantAllow: false,
			wantError: false,
		},
		{
			name: "resource does not match",
			req: &PolicySimulationRequest{
				Policy: &Policy{
					APIVersion: "abac.quicbaas.io/v1",
					Kind:       "Policy",
					Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
					Spec: PolicySpec{
						Subjects: SubjectSelector{Authenticated: true},
						Rules: []Rule{{
							Name: "allow", Effect: EffectAllow,
							Actions: []string{"read"}, Resources: []string{"/admin/*"},
						}},
					},
				},
				CheckRequest: &CheckRequest{
					Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
					Action:   "read",
					Resource: "/test/file.txt",
				},
			},
			wantAllow: false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.SimulatePolicy(ctx, tt.req)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Fatal("expected result, got nil")
				}
				if result.Allowed != tt.wantAllow {
					t.Errorf("allowed = %v, want %v", result.Allowed, tt.wantAllow)
				}
			}
		})
	}
}

// TestPolicyEngineGetEffectivePermissions tests the GetEffectivePermissions function
func TestPolicyEngineGetEffectivePermissions(t *testing.T) {
	storage := newMockPolicyStorage()
	cache := newTestPolicyCache()
	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
		Cache:   cache,
	})
	ctx := context.Background()

	// Create test policies
	policy := createTestPolicy("tenant1", "policy1")
	_ = storage.CreatePolicy(ctx, "tenant1", policy)

	tests := []struct {
		name      string
		tenantID  string
		req       *EffectivePermissionsRequest
		wantError bool
	}{
		{
			name:      "nil request",
			tenantID:  "tenant1",
			req:       nil,
			wantError: true,
		},
		{
			name:     "nil subject",
			tenantID: "tenant1",
			req: &EffectivePermissionsRequest{
				Subject:          nil,
				ResourcePatterns: []string{"/test/*"},
			},
			wantError: true,
		},
		{
			name:     "empty resource patterns",
			tenantID: "tenant1",
			req: &EffectivePermissionsRequest{
				Subject:          &Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
				ResourcePatterns: []string{},
			},
			wantError: true,
		},
		{
			name:     "valid request",
			tenantID: "tenant1",
			req: &EffectivePermissionsRequest{
				Subject:          &Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
				ResourcePatterns: []string{"/test/*"},
			},
			wantError: false,
		},
		{
			name:     "valid request with specific actions",
			tenantID: "tenant1",
			req: &EffectivePermissionsRequest{
				Subject:          &Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
				ResourcePatterns: []string{"/test/*"},
				Actions:          []string{"read", "write"},
			},
			wantError: false,
		},
		{
			name:     "valid request with deny details",
			tenantID: "tenant1",
			req: &EffectivePermissionsRequest{
				Subject:            &Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
				ResourcePatterns:   []string{"/test/*"},
				IncludeDenyDetails: true,
			},
			wantError: false,
		},
		{
			name:     "valid request with empty tenant ID in subject",
			tenantID: "tenant1",
			req: &EffectivePermissionsRequest{
				Subject:          &Subject{ID: "user1", Authenticated: true},
				ResourcePatterns: []string{"/test/*"},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.GetEffectivePermissions(ctx, tt.tenantID, tt.req)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Fatal("expected result, got nil")
				}
			}
		})
	}
}

// TestMatchAction tests the matchAction helper function
func TestMatchAction(t *testing.T) {
	tests := []struct {
		name          string
		ruleActions   []string
		requestAction string
		want          bool
	}{
		{
			name:          "exact match",
			ruleActions:   []string{"read"},
			requestAction: "read",
			want:          true,
		},
		{
			name:          "wildcard match",
			ruleActions:   []string{"*"},
			requestAction: "anything",
			want:          true,
		},
		{
			name:          "no match",
			ruleActions:   []string{"read", "write"},
			requestAction: "delete",
			want:          false,
		},
		{
			name:          "empty list",
			ruleActions:   []string{},
			requestAction: "read",
			want:          false,
		},
		{
			name:          "multiple actions - match in middle",
			ruleActions:   []string{"read", "write", "delete"},
			requestAction: "write",
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchAction(tt.ruleActions, tt.requestAction)
			if result != tt.want {
				t.Errorf("matchAction() = %v, want %v", result, tt.want)
			}
		})
	}
}

// TestPEPStartAndClose tests Start and Close functions
func TestPEPStartAndClose(t *testing.T) {
	t.Run("start without syncer", func(t *testing.T) {
		pep := NewPEP(DefaultPEPConfig("tenant1"))
		err := pep.Start(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		err = pep.Close()
		if err != nil {
			t.Errorf("unexpected error on close: %v", err)
		}
	})

	t.Run("start on closed PEP", func(t *testing.T) {
		pep := NewPEP(DefaultPEPConfig("tenant1"))
		pep.Close()
		err := pep.Start(context.Background())
		if err != ErrPEPClosed {
			t.Errorf("expected ErrPEPClosed, got %v", err)
		}
	})

	t.Run("double close", func(t *testing.T) {
		pep := NewPEP(DefaultPEPConfig("tenant1"))
		err := pep.Close()
		if err != nil {
			t.Errorf("first close error: %v", err)
		}
		err = pep.Close()
		if err != nil {
			t.Errorf("second close should not error: %v", err)
		}
	})
}

// TestPEPGetStats tests the GetStats function
func TestPEPGetStats(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))

	// Load a policy
	policy := &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
		Spec: PolicySpec{
			Subjects: SubjectSelector{Authenticated: true},
			Rules: []Rule{{
				Name: "allow", Effect: EffectAllow,
				Actions: []string{"read"}, Resources: []string{"/data/*"},
			}},
		},
	}
	compiled, _ := pep.compiler.Compile(policy)
	pep.cache.Set("tenant1", compiled)

	// Perform some checks
	for i := 0; i < 5; i++ {
		_, _ = pep.Check(context.Background(), &CheckRequest{
			Subject:  Subject{ID: "user1", TenantID: "tenant1", Authenticated: true},
			Action:   "read",
			Resource: "/data/file.txt",
		})
	}

	stats := pep.GetStats()
	if stats == nil {
		t.Fatal("expected stats, got nil")
	}
}

// TestPEPSync tests the Sync function
func TestPEPSync(t *testing.T) {
	t.Run("sync without syncer", func(t *testing.T) {
		pep := NewPEP(DefaultPEPConfig("tenant1"))
		err := pep.Sync(context.Background())
		if err == nil {
			t.Error("expected error when syncing without syncer")
		}
	})

	t.Run("sync on closed PEP", func(t *testing.T) {
		pep := NewPEP(DefaultPEPConfig("tenant1"))
		pep.Close()
		err := pep.Sync(context.Background())
		if err != ErrPEPClosed {
			t.Errorf("expected ErrPEPClosed, got %v", err)
		}
	})
}

// TestPEPSubscribe tests the Subscribe function
func TestPEPSubscribe(t *testing.T) {
	t.Run("subscribe without client", func(t *testing.T) {
		pep := NewPEP(DefaultPEPConfig("tenant1"))
		_, err := pep.Subscribe(context.Background(), []string{"*"})
		if err == nil {
			t.Error("expected error when subscribing without client")
		}
	})

	t.Run("subscribe on closed PEP", func(t *testing.T) {
		pep := NewPEP(DefaultPEPConfig("tenant1"))
		pep.Close()
		_, err := pep.Subscribe(context.Background(), []string{"*"})
		if err != ErrPEPClosed {
			t.Errorf("expected ErrPEPClosed, got %v", err)
		}
	})
}

// TestPEPApplyPolicyUpdate tests the applyPolicyUpdate function
func TestPEPApplyPolicyUpdate(t *testing.T) {
	pep := NewPEP(DefaultPEPConfig("tenant1"))

	policy := &Policy{
		APIVersion: "abac.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test", Tenant: "tenant1"},
		Spec: PolicySpec{
			Subjects: SubjectSelector{Authenticated: true},
			Rules: []Rule{{
				Name: "allow", Effect: EffectAllow,
				Actions: []string{"read"}, Resources: []string{"/data/*"},
			}},
		},
	}
	compiled, _ := pep.compiler.Compile(policy)

	// Test snapshot update
	pep.applyPolicyUpdate(&PolicyUpdate{
		Type:   PolicyUpdateSnapshot,
		Policy: compiled,
	})

	// Test created update
	pep.applyPolicyUpdate(&PolicyUpdate{
		Type:   PolicyUpdateCreated,
		Policy: compiled,
	})

	// Test updated update
	pep.applyPolicyUpdate(&PolicyUpdate{
		Type:   PolicyUpdateUpdated,
		Policy: compiled,
	})

	// Test deleted update
	pep.applyPolicyUpdate(&PolicyUpdate{
		Type:     PolicyUpdateDeleted,
		PolicyID: "test",
	})
}

// TestMemoryStoragePolicyCRUD tests the MemoryStorage policy CRUD operations
func TestMemoryStoragePolicyCRUD(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()
	tenantID := "tenant1"

	// Create tenant first
	tenant := &Tenant{
		ID:      tenantID,
		Name:    "Test Tenant",
		Created: time.Now(),
		Updated: time.Now(),
	}
	err := storage.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	t.Run("CreatePolicy", func(t *testing.T) {
		policy := createTestPolicy(tenantID, "policy1")
		err := storage.CreatePolicy(ctx, tenantID, policy)
		if err != nil {
			t.Errorf("CreatePolicy failed: %v", err)
		}

		// Try to create duplicate
		err = storage.CreatePolicy(ctx, tenantID, policy)
		if err == nil {
			t.Error("expected error for duplicate policy")
		}
	})

	t.Run("GetPolicy", func(t *testing.T) {
		policy, err := storage.GetPolicy(ctx, tenantID, "policy1")
		if err != nil {
			t.Errorf("GetPolicy failed: %v", err)
		}
		if policy == nil {
			t.Error("expected policy, got nil")
		}
		if policy.Metadata.Name != "policy1" {
			t.Errorf("expected policy1, got %s", policy.Metadata.Name)
		}

		// Try to get non-existent
		_, err = storage.GetPolicy(ctx, tenantID, "nonexistent")
		if err == nil {
			t.Error("expected error for non-existent policy")
		}
	})

	t.Run("UpdatePolicy", func(t *testing.T) {
		policy := createTestPolicy(tenantID, "policy1")
		policy.Metadata.Description = "Updated description"
		err := storage.UpdatePolicy(ctx, tenantID, policy)
		if err != nil {
			t.Errorf("UpdatePolicy failed: %v", err)
		}

		// Verify update
		updated, _ := storage.GetPolicy(ctx, tenantID, "policy1")
		if updated.Metadata.Description != "Updated description" {
			t.Error("policy was not updated")
		}

		// Try to update non-existent
		nonexistent := createTestPolicy(tenantID, "nonexistent")
		err = storage.UpdatePolicy(ctx, tenantID, nonexistent)
		if err == nil {
			t.Error("expected error for non-existent policy")
		}
	})

	t.Run("ListPolicies", func(t *testing.T) {
		// Create another policy
		policy2 := createTestPolicy(tenantID, "policy2")
		_ = storage.CreatePolicy(ctx, tenantID, policy2)

		policies, err := storage.ListPolicies(ctx, tenantID, ListOptions{})
		if err != nil {
			t.Errorf("ListPolicies failed: %v", err)
		}
		if len(policies) < 2 {
			t.Errorf("expected at least 2 policies, got %d", len(policies))
		}

		// Test pagination
		policies, err = storage.ListPolicies(ctx, tenantID, ListOptions{Limit: 1})
		if err != nil {
			t.Errorf("ListPolicies with limit failed: %v", err)
		}
		if len(policies) != 1 {
			t.Errorf("expected 1 policy with limit, got %d", len(policies))
		}

		// Test offset beyond results
		policies, err = storage.ListPolicies(ctx, tenantID, ListOptions{Offset: 100})
		if err != nil {
			t.Errorf("ListPolicies with large offset failed: %v", err)
		}
		if len(policies) != 0 {
			t.Errorf("expected 0 policies with large offset, got %d", len(policies))
		}
	})

	t.Run("DeletePolicy", func(t *testing.T) {
		err := storage.DeletePolicy(ctx, tenantID, "policy1")
		if err != nil {
			t.Errorf("DeletePolicy failed: %v", err)
		}

		// Verify deletion
		_, err = storage.GetPolicy(ctx, tenantID, "policy1")
		if err == nil {
			t.Error("expected error for deleted policy")
		}

		// Try to delete non-existent
		err = storage.DeletePolicy(ctx, tenantID, "nonexistent")
		if err == nil {
			t.Error("expected error for non-existent policy")
		}
	})
}

// TestResourceTrieCopyVars tests the copyVars helper function
func TestResourceTrieCopyVars(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := copyVars(nil)
		if result == nil {
			t.Error("expected non-nil map")
		}
		if len(result) != 0 {
			t.Errorf("expected empty map, got %d entries", len(result))
		}
	})

	t.Run("non-nil input", func(t *testing.T) {
		original := map[string]string{"key": "value"}
		result := copyVars(original)
		if result == nil {
			t.Error("expected non-nil map")
		}
		if result["key"] != "value" {
			t.Error("value not copied")
		}
		// Modify original to ensure copy is independent
		original["key"] = "modified"
		if result["key"] == "modified" {
			t.Error("copy is not independent")
		}
	})
}

// TestResourceTrieIsVariable tests the isVariable helper function
func TestResourceTrieIsVariable(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		want    bool
	}{
		{"dollar-brace format", "${userId}", true},
		{"colon format", ":userId", true},
		{"brace format", "{userId}", true},
		{"plain segment", "users", false},
		{"empty segment", "", false},
		{"single colon", ":", false},
		{"incomplete dollar-brace", "${", false},
		{"incomplete brace", "{", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVariable(tt.segment)
			if result != tt.want {
				t.Errorf("isVariable(%q) = %v, want %v", tt.segment, result, tt.want)
			}
		})
	}
}

// TestResourceTrieDeleteSegments tests the deleteSegments path
func TestResourceTrieDeleteSegments(t *testing.T) {
	trie := NewResourceTrie()

	// Insert patterns including variables
	trie.Insert("/users/${userId}/profile", "user-profile")
	trie.Insert("/users/:id/settings", "user-settings")
	trie.Insert("/admin/**", "admin-all")

	t.Run("delete with dollar-brace variable", func(t *testing.T) {
		deleted := trie.Delete("/users/${userId}/profile")
		if !deleted {
			t.Error("expected deletion to succeed")
		}
	})

	t.Run("delete with colon variable", func(t *testing.T) {
		deleted := trie.Delete("/users/:id/settings")
		if !deleted {
			t.Error("expected deletion to succeed")
		}
	})

	t.Run("delete non-existent pattern", func(t *testing.T) {
		deleted := trie.Delete("/nonexistent/path")
		if deleted {
			t.Error("expected deletion to fail for non-existent")
		}
	})
}

// TestPEPWithNilConfig tests PEP creation with nil config
func TestPEPWithNilConfig(t *testing.T) {
	pep := NewPEP(nil)
	if pep == nil {
		t.Fatal("expected PEP, got nil")
	}
	if pep.config == nil {
		t.Error("expected default config, got nil")
	}
}

// TestPolicyEngineEvaluateWithEmptyCache tests Evaluate when cache is empty
func TestPolicyEngineEvaluateWithEmptyCache(t *testing.T) {
	storage := newMockPolicyStorage()
	cache := newTestPolicyCache()
	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
		Cache:   cache,
	})
	ctx := context.Background()

	// Create a policy in storage
	policy := createTestPolicy("tenant1", "policy1")
	_ = storage.CreatePolicy(ctx, "tenant1", policy)

	// Evaluate - should trigger cache population
	req := &CheckRequest{
		Subject: Subject{
			ID:            "user1",
			TenantID:      "tenant1",
			Authenticated: true,
		},
		Action:   "read",
		Resource: "/test/file.txt",
	}

	resp, err := engine.Evaluate(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}
