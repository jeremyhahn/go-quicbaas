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
	"errors"
	"testing"
	"time"
)

// TestAgentService_JobLifecyclePolicy: the service carries the operator's
// policy and a control loop built over it takes the policy from there.
func TestAgentService_JobLifecyclePolicy(t *testing.T) {
	t.Parallel()
	svc := &agentServiceImpl{logger: &controlLoopMockLogger{}}
	if err := svc.JobLifecyclePolicy().Guard.CheckCommand("c", "make test"); !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("an unset policy refuses every command: %v", err)
	}

	svc.SetJobLifecyclePolicy(JobLifecyclePolicy{
		Guard:         guardAllowing(t, "make test"),
		StageTimeouts: StageTimeouts{Reviewable: 42 * time.Second},
	})
	policy := svc.JobLifecyclePolicy()
	if err := policy.Guard.CheckCommand("c", "make test"); err != nil {
		t.Fatalf("the installed guard must be returned: %v", err)
	}

	var provider JobLifecyclePolicyProvider = svc
	loop := NewControlLoop(provider.(AgentService), &controlLoopMockLogger{})
	resolved := loop.lifecyclePolicy()
	if resolved.StageTimeouts.Reviewable != 42*time.Second || resolved.StageTimeouts.Prepare != DefaultStageTimeouts.Prepare {
		t.Fatalf("the loop must take the service's bounds with defaults filled: %+v", resolved.StageTimeouts)
	}
	if err := resolved.Guard.CheckCommand("c", "make lint"); !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("the loop must take the service's guard: %v", err)
	}
}
