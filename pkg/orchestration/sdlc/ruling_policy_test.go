// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package sdlc

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Configurable rulers: the node policy, the tenant override, the binding
// =============================================================================

func TestValidateForgeLogin_AcceptsForgeLogins(t *testing.T) {
	for _, login := range []string{"acme", "company-agent", "a", "jeremy.hahn", "bot_1", "A1-b2", strings.Repeat("x", maxForgeLoginLength)} {
		if err := ValidateForgeLogin("rulers", login); err != nil {
			t.Errorf("ValidateForgeLogin(%q) = %v, want nil", login, err)
		}
	}
}

func TestValidateForgeLogin_RefusesWhatNamesNobody(t *testing.T) {
	cases := map[string]string{
		"empty":               "",
		"whitespace":          "agent company",
		"padded":              " acme",
		"leading punctuation": "-acme",
		"trailing dot":        "acme.",
		"a path":              "acme/repo",
		"too long":            strings.Repeat("x", maxForgeLoginLength+1),
		"an email":            "owner@example.com",
	}
	for name, login := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateForgeLogin("rulers[0]", login)
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) || invalid.Field != "rulers[0]" {
				t.Fatalf("ValidateForgeLogin(%q) = %v, want InvalidInputError on rulers[0]", login, err)
			}
		})
	}
}

func TestRulerPolicy_ForResolvesTheTenantThenTheDefault(t *testing.T) {
	cases := map[string]struct {
		policy RulerPolicy
		tenant string
		want   []string
	}{
		"nothing configured takes DefaultRulers": {RulerPolicy{}, "t1", DefaultRulers},
		"a node default replaces DefaultRulers":  {RulerPolicy{Default: []string{"ops"}}, "t1", []string{"ops"}},
		"an empty default names nobody":          {RulerPolicy{Default: []string{}}, "t1", []string{}},
		"a tenant entry replaces the default": {
			RulerPolicy{Default: []string{"ops"}, Tenants: map[string][]string{"t1": {"lead"}}}, "t1", []string{"lead"},
		},
		"another tenant keeps the default": {
			RulerPolicy{Default: []string{"ops"}, Tenants: map[string][]string{"t1": {"lead"}}}, "t2", []string{"ops"},
		},
		"a tenant can be given nobody": {
			RulerPolicy{Tenants: map[string][]string{"t1": {}}}, "t1", []string{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.policy.For(tc.tenant)
			if len(got) != len(tc.want) || (len(got) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("For(%q) = %v, want %v", tc.tenant, got, tc.want)
			}
		})
	}
}

func TestRulerPolicy_ForReturnsACopy(t *testing.T) {
	policy := RulerPolicy{Default: []string{"ops"}}
	policy.For("t1")[0] = "mallory"
	if policy.Default[0] != "ops" {
		t.Fatalf("Default = %v: a caller mutated the policy through For", policy.Default)
	}
}

func TestRulerPolicy_ValidateFailsClosed(t *testing.T) {
	cases := map[string]RulerPolicy{
		"a malformed default":        {Default: []string{"not a login"}},
		"a default listed twice":     {Default: []string{"ops", "OPS"}},
		"a blank tenant":             {Tenants: map[string][]string{" ": {"ops"}}},
		"a malformed tenant ruler":   {Tenants: map[string][]string{"t1": {""}}},
		"a tenant ruler twice":       {Tenants: map[string][]string{"t1": {"lead", "lead"}}},
		"an email in a tenant entry": {Tenants: map[string][]string{"t1": {"lead@example.com"}}},
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			if err := policy.Validate(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Validate = %v, want ErrInvalidInput", err)
			}
		})
	}
	if err := (RulerPolicy{Default: []string{"ops"}, Tenants: map[string][]string{"t1": {"lead"}}}).Validate(); err != nil {
		t.Fatalf("a well-formed policy was refused: %v", err)
	}
}

func TestRulerSet_AddsTheBindingsRulers(t *testing.T) {
	binding := testBinding()
	binding.Rulers = []string{"Lead-Agent"}
	set := rulerSet(binding, []string{"company-agent"})
	for _, login := range []string{fixtureOwner, "lead-agent", "company-agent"} {
		if _, ok := set[login]; !ok {
			t.Errorf("ruler set %v lacks %q", set, login)
		}
	}
	if len(set) != 3 {
		t.Fatalf("ruler set = %v, want exactly the owner, the binding's ruler and the configured one", set)
	}
}

func TestProjectBindingValidate_RefusesMalformedRulers(t *testing.T) {
	cases := map[string][]string{
		"a blank ruler":      {""},
		"a ruler twice":      {"lead", "Lead"},
		"not a forge login":  {"lead agent"},
		"a repository path":  {"acme/api"},
		"an overlong ruler":  {strings.Repeat("y", maxForgeLoginLength+1)},
		"one bad among good": {"lead", "-bad"},
	}
	for name, rulers := range cases {
		t.Run(name, func(t *testing.T) {
			binding := testBinding()
			binding.Rulers = rulers
			var invalid *InvalidInputError
			if err := binding.Validate(); !errors.As(err, &invalid) || !strings.HasPrefix(invalid.Field, "rulers[") {
				t.Fatalf("Validate = %v, want InvalidInputError on a rulers entry", err)
			}
		})
	}
	binding := testBinding()
	binding.Rulers = []string{"lead-agent", "ops"}
	if err := binding.Validate(); err != nil {
		t.Fatalf("well-formed rulers were refused: %v", err)
	}
}

func TestNewUnblockSweeper_RefusesAMalformedRulerPolicy(t *testing.T) {
	f := newSweepFixture(t)
	_, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks: f.tasks, Bindings: &fakeBindings{binding: testBinding()}, Forges: &fakeForges{forge: f.forge},
		Rulers: RulerPolicy{Default: []string{"not a login"}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewUnblockSweeper = %v, want ErrInvalidInput", err)
	}
}

func TestNewAttentionService_RefusesAMalformedRulerPolicy(t *testing.T) {
	_, err := NewAttentionService(AttentionServiceConfig{
		Tasks: newFakeAttentionTasks(), Bindings: &fakeBindings{}, Forges: &fakeForges{},
		Projects: &fakeProjects{projects: map[string]*orchestration.Project{}},
		Rulers:   RulerPolicy{Tenants: map[string][]string{"": {"ops"}}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewAttentionService = %v, want ErrInvalidInput", err)
	}
}

func TestSweep_ABindingRulerReadmitsAParkedTask(t *testing.T) {
	f := newSweepFixture(t)
	binding := testBinding()
	binding.Rulers = []string{"lead-agent"}
	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks: f.tasks, Bindings: &fakeBindings{binding: binding}, Forges: &fakeForges{forge: f.forge},
		Rulers: RulerPolicy{Default: []string{}},
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	f.sweeper = sweeper
	task := f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	f.forge.comment(issueNumber(t, task), 903, "lead-agent", "Split it.", parkedAt.Add(time.Minute))

	if finding := onlyFinding(t, f.sweep(t)); finding.Verdict != UnblockReleased {
		t.Fatalf("finding = %+v, want the binding's ruler to readmit the task", finding)
	}
}

func TestSweep_ATenantEntryReplacesTheNodeDefault(t *testing.T) {
	f := newSweepFixture(t)
	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks: f.tasks, Bindings: &fakeBindings{binding: testBinding()}, Forges: &fakeForges{forge: f.forge},
		Rulers: RulerPolicy{Default: []string{"company-agent"}, Tenants: map[string][]string{testTenant: {"tenant-lead"}}},
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	f.sweeper = sweeper
	task := f.parkedForRuling(orchestration.TaskBlockKindEscalated)
	f.forge.comment(issueNumber(t, task), 904, "company-agent", "Yes.", parkedAt.Add(time.Minute))

	if finding := onlyFinding(t, f.sweep(t)); finding.Verdict == UnblockReleased {
		t.Fatalf("finding = %+v: the node default ruled on a tenant that replaced it", finding)
	}
	f.forge.comment(issueNumber(t, task), 905, "tenant-lead", "Yes.", parkedAt.Add(2*time.Minute))
	if finding := onlyFinding(t, f.sweep(t)); finding.Verdict != UnblockReleased {
		t.Fatalf("finding = %+v, want the tenant's own ruler to readmit the task", finding)
	}
}
