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

package orchestration

import (
	"context"
	"errors"
	"testing"
)

// =============================================================================
// The runner's side of the lease: JobLeasePolicy
// =============================================================================
//
// WHAT THIS FILE NO LONGER COVERS, AND WHERE IT LIVES INSTEAD. Three
// properties of an off-budget lease — admitted at the limit, filling no slot,
// and counted consistently by the third reader — are proven in
// job_lease_test.go beside the capacity store they are about. This file had
// its own copies and they were DELETED rather than renamed when #888 added
// them there: two tests of one property disagree eventually, and the
// canonical versions are better — the count one proves the two READERS AGREE,
// which is the actual invariant, rather than checking a number in isolation.
//
// What is left is the half that is the RUNNER's rather than the store's.

// TestProjectCapacity_OffBudgetStillSerializesPerSubject proves the exemption
// buys unlimited triage of DIFFERENT projects, never two triage jobs over one.
// The lease is what bounds it, so the bound survives a restart and a second
// node rather than living in one process's memory.
func TestProjectCapacity_OffBudgetStillSerializesPerSubject(t *testing.T) {
	ctx := context.Background()
	store, _ := cappedCapacityStore(t, 1)

	first := &JobLease{TenantID: "acme", ProjectID: "proj-1", Workflow: "triage", Ref: "triage:proj-1", JobID: "job-triage-1", Generation: 1, OffBudget: true}
	if err := store.Acquire(ctx, first); err != nil {
		t.Fatalf("acquire first triage: %v", err)
	}
	second := &JobLease{TenantID: "acme", ProjectID: "proj-1", Workflow: "triage", Ref: "triage:proj-1", JobID: "job-triage-2", Generation: 1, OffBudget: true}
	if err := store.Acquire(ctx, second); !errors.Is(err, ErrJobLeaseTaken) {
		t.Fatalf("a second triage job over one project must be refused, got %v", err)
	}
}

// =============================================================================
// The runner's declaration
// =============================================================================

// budgetTestRunner is a runner declaring a JobLeasePolicy.
type budgetTestRunner struct {
	name    string
	subject string
	off     bool
}

func (r budgetTestRunner) Name() string { return r.name }

func (r budgetTestRunner) States() []JobStateSpec {
	return []JobStateSpec{{State: JobStateInitializing, Label: "Initializing"}}
}

func (r budgetTestRunner) Validate(context.Context, *JobConfig) error { return nil }

func (r budgetTestRunner) Run(context.Context, *JobRun) error { return nil }

func (r budgetTestRunner) LeaseSubject(*JobConfig) string { return r.subject }

func (r budgetTestRunner) OffProjectBudget() bool { return r.off }

// TestJobLeasePolicy_ReadFromTheRunner pins the seam: a runner that declares a
// lease policy has it honoured, and one that does not keeps the envelope's own
// answers.
func TestJobLeasePolicy_ReadFromTheRunner(t *testing.T) {
	tests := []struct {
		name        string
		runner      JobRunner
		cfg         *JobConfig
		wantSubject string
		wantOff     bool
	}{
		{
			name:        "a declaring runner names its own subject and exemption",
			runner:      budgetTestRunner{name: "triage", subject: "triage:proj-1", off: true},
			cfg:         &JobConfig{ProjectID: "proj-1"},
			wantSubject: "triage:proj-1",
			wantOff:     true,
		},
		{
			name:        "a declaring runner may be on budget",
			runner:      budgetTestRunner{name: "other", subject: "other:proj-1", off: false},
			cfg:         &JobConfig{ProjectID: "proj-1"},
			wantSubject: "other:proj-1",
			wantOff:     false,
		},
		{
			name:        "an empty subject falls back to the envelope's",
			runner:      budgetTestRunner{name: "quiet", subject: "", off: true},
			cfg:         &JobConfig{ProjectID: "proj-1"},
			wantSubject: "",
			wantOff:     true,
		},
		{
			name:        "a runner declaring nothing keeps the envelope's answers",
			runner:      workflowRunner{},
			cfg:         &JobConfig{ProjectID: "proj-1"},
			wantSubject: "",
			wantOff:     false,
		},
		{
			name:        "a nil runner declares nothing",
			runner:      nil,
			cfg:         &JobConfig{ProjectID: "proj-1"},
			wantSubject: "",
			wantOff:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if subject := runnerLeaseSubject(tc.runner, tc.cfg); subject != tc.wantSubject {
				t.Fatalf("subject: want %q, got %q", tc.wantSubject, subject)
			}
			if off := runnerOffProjectBudget(tc.runner); off != tc.wantOff {
				t.Fatalf("off budget: want %v, got %v", tc.wantOff, off)
			}
		})
	}
}
