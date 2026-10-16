package iam

import (
	"context"
	"errors"
	"testing"
	"time"
)

// nestedConditionCases are the three nested shapes go-qrdb's flat ABAC
// condition cannot hold.
var nestedConditionCases = map[string]Condition{
	"and": {And: []Condition{{Expression: "subject.dept == 'eng'"}}},
	"or":  {Or: []Condition{{Expression: "subject.dept == 'eng'"}}},
	"not": {Not: &Condition{Expression: "subject.dept == 'eng'"}},
}

func conditionPolicy(tenantID, name string, when ...Condition) *Policy {
	p := createTestPolicy(tenantID, name)
	p.Spec.Rules[0].When = when
	return p
}

func requireNestedConditionError(t *testing.T, err error, field string, index int) {
	t.Helper()
	var nested *NestedConditionError
	if !errors.As(err, &nested) {
		t.Fatalf("expected NestedConditionError, got %T: %v", err, err)
	}
	if nested.Field != field || nested.Index != index {
		t.Errorf("got %s[%d], want %s[%d]", nested.Field, nested.Index, field, index)
	}
}

func TestValidateConditions(t *testing.T) {
	t.Run("flat expressions pass", func(t *testing.T) {
		conds := []Condition{{Expression: "subject.dept == 'eng'"}, {Expression: "a and not b"}}
		if err := ValidateConditions("when", conds); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	for name, nested := range nestedConditionCases {
		t.Run(name+" refused", func(t *testing.T) {
			conds := []Condition{{Expression: "x == 1"}, nested}
			requireNestedConditionError(t, ValidateConditions("unless", conds), "unless", 1)
		})
	}
}

func TestRule_Validate_NestedCondition(t *testing.T) {
	rule := Rule{Name: "r", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}

	rule.When = []Condition{{Expression: "x == 1"}}
	if err := rule.Validate(); err != nil {
		t.Fatalf("flat condition: unexpected error: %v", err)
	}

	rule.Unless = []Condition{nestedConditionCases["not"]}
	requireNestedConditionError(t, rule.Validate(), "unless", 0)
}

func TestPolicyEngine_CreatePolicy_NestedCondition(t *testing.T) {
	ctx := context.Background()

	for name, nested := range nestedConditionCases {
		t.Run(name+" refused and not stored", func(t *testing.T) {
			storage := NewMemoryStorage()
			engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})

			err := engine.CreatePolicy(ctx, "tenant1", conditionPolicy("tenant1", "p1", nested))
			requireNestedConditionError(t, err, "when", 0)

			if got, getErr := storage.GetPolicy(ctx, "tenant1", "p1"); getErr == nil && got != nil {
				t.Error("refused policy was stored")
			}
		})
	}

	t.Run("flat condition stored", func(t *testing.T) {
		storage := NewMemoryStorage()
		engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})

		flat := Condition{Expression: "subject.dept == 'eng'"}
		if err := engine.CreatePolicy(ctx, "tenant1", conditionPolicy("tenant1", "p1", flat)); err != nil {
			t.Fatalf("CreatePolicy: %v", err)
		}
		got, err := storage.GetPolicy(ctx, "tenant1", "p1")
		if err != nil {
			t.Fatalf("GetPolicy: %v", err)
		}
		if when := got.Spec.Rules[0].When; len(when) != 1 || when[0].Expression != flat.Expression {
			t.Errorf("stored conditions = %+v", when)
		}
	})
}

func TestPolicyEngine_UpdatePolicy_NestedCondition(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})

	original := Condition{Expression: "subject.dept == 'eng'"}
	if err := engine.CreatePolicy(ctx, "tenant1", conditionPolicy("tenant1", "p1", original)); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	err := engine.UpdatePolicy(ctx, "tenant1", conditionPolicy("tenant1", "p1", nestedConditionCases["or"]))
	requireNestedConditionError(t, err, "when", 0)

	got, err := storage.GetPolicy(ctx, "tenant1", "p1")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if when := got.Spec.Rules[0].When; len(when) != 1 || when[0].Expression != original.Expression || when[0].Or != nil {
		t.Errorf("refused update changed the stored policy: %+v", when)
	}

	flat := Condition{Expression: "subject.dept == 'ops'"}
	if err := engine.UpdatePolicy(ctx, "tenant1", conditionPolicy("tenant1", "p1", flat)); err != nil {
		t.Fatalf("flat UpdatePolicy: %v", err)
	}
	got, err = storage.GetPolicy(ctx, "tenant1", "p1")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if got.Spec.Rules[0].When[0].Expression != flat.Expression {
		t.Errorf("flat update not stored: %+v", got.Spec.Rules[0].When)
	}
}

func TestBatchManager_ImportPolicies_NestedCondition(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{UserStorage: storage, RoleStorage: storage}),
		PolicyEngine:  NewPolicyEngine(PolicyEngineOptions{Storage: storage}),
	})
	tenantID := "import-tenant"
	if err := storage.CreateTenant(ctx, &Tenant{ID: tenantID, Name: "Import", Created: time.Now()}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	rule := func(c Condition) []Rule {
		return []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}, When: []Condition{c}}}
	}
	result, err := bm.ImportPolicies(ctx, tenantID, []*PolicyImport{
		{Name: "nested", Subjects: SubjectSelector{Authenticated: true}, Rules: rule(nestedConditionCases["and"])},
		{Name: "flat", Subjects: SubjectSelector{Authenticated: true}, Rules: rule(Condition{Expression: "x == 1"})},
	}, BatchImportOptions{ContinueOnError: true})
	if err != nil {
		t.Fatalf("ImportPolicies: %v", err)
	}
	if result.SuccessCount != 1 || result.FailureCount != 1 {
		t.Errorf("success=%d failure=%d, want 1/1", result.SuccessCount, result.FailureCount)
	}
	if got, getErr := storage.GetPolicy(ctx, tenantID, "nested"); getErr == nil && got != nil {
		t.Error("nested policy was imported")
	}
	if _, getErr := storage.GetPolicy(ctx, tenantID, "flat"); getErr != nil {
		t.Errorf("flat policy not imported: %v", getErr)
	}
}

func TestPolicyImportItem_Validate_NestedCondition(t *testing.T) {
	flatRules := []Rule{{Name: "r", When: []Condition{{Expression: "x == 1"}}}}
	nestedRules := []Rule{{Name: "r", Unless: []Condition{nestedConditionCases["not"]}}}

	if err := (&PolicyImportItem{Name: "p", Rules: flatRules}).Validate(); err != nil {
		t.Fatalf("flat inline rules: unexpected error: %v", err)
	}
	requireNestedConditionError(t, (&PolicyImportItem{Name: "p", Rules: nestedRules}).Validate(), "unless", 0)

	policy := createTestPolicy("t", "p")
	policy.Spec.Rules = nestedRules
	requireNestedConditionError(t, (&PolicyImportItem{Policy: policy}).Validate(), "unless", 0)
}
