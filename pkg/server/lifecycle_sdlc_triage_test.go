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

package server

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// triageBindingPorts answers one binding for every project, and refuses the
// rest as the stub does.
type triageBindingPorts struct {
	stubSDLCPorts
}

func (triageBindingPorts) GetBinding(_ context.Context, _, projectID string) (*sdlc.ProjectBinding, error) {
	return &sdlc.ProjectBinding{
		ProjectID: projectID,
		Forge:     sdlc.ForgeConfig{Type: sdlc.ForgeGitea, BaseURL: "http://forge.test", Owner: "o", Repo: "r"},
		Worker:    orchestration.AgentRefTo("coder"),
	}, nil
}

// leaseTakenJobs refuses every start as a project already being triaged.
type leaseTakenJobs struct{}

func (leaseTakenJobs) StartJob(context.Context, *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	return nil, &orchestration.JobLeaseTakenError{JobID: "job-2", HeldBy: "job-1"}
}

// unusedTriageTokens and unusedTriageSigning satisfy the runner's required
// ports; the wiring under test never clones.
type unusedTriageTokens struct{}

func (unusedTriageTokens) ForgeToken(context.Context, string, sdlc.ForgeConfig) ([]byte, error) {
	return nil, ErrSDLCUnavailable
}

type unusedTriageSigning struct{}

func (unusedTriageSigning) StartSigningAgent(context.Context, *orchestration.Project, string, sdlc.SigningScope) (*sdlc.SigningSession, error) {
	return nil, ErrSDLCUnavailable
}

// triageWiringConfig is a runner configuration complete but for the ports
// newSDLCTriage fills in itself.
func triageWiringConfig(t *testing.T) sdlc.TriageRunnerConfig {
	t.Helper()
	guard, err := sdlc.NewRepoHostGuard([]string{"forge.test"}, false)
	if err != nil {
		t.Fatalf("NewRepoHostGuard: %v", err)
	}
	return sdlc.TriageRunnerConfig{
		Bindings:    triageBindingPorts{},
		Forges:      triageBindingPorts{},
		Tokens:      unusedTriageTokens{},
		Signing:     unusedTriageSigning{},
		Guard:       guard,
		SocketsRoot: t.TempDir(),
	}
}

func TestNewSDLCTriage_ALostLeaseMarksTheSharedStore(t *testing.T) {
	store, err := sdlc.NewStore(&memoryTenantResolver{stores: map[string]qrdbsdk.KVStore{}})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	runner, scheduler, err := newSDLCTriage(triageWiringConfig(t), leaseTakenJobs{}, store)
	if err != nil || runner == nil || scheduler == nil {
		t.Fatalf("newSDLCTriage = %v, %v, %v; want a runner and a scheduler", runner, scheduler, err)
	}
	ctx := context.Background()

	if err := scheduler.TriageProject(ctx, "t", "p"); err != nil {
		t.Fatalf("a project already being triaged is not an error: %v", err)
	}
	taken, err := store.TakeTriageRerun(ctx, "t", "p")
	if err != nil || !taken {
		t.Fatalf("TakeTriageRerun = %v, %v; the nudge must be marked in the store the runner reads", taken, err)
	}
}

func TestNewSDLCTriage_AnIncompleteRunnerIsAnInitError(t *testing.T) {
	store, err := sdlc.NewStore(&memoryTenantResolver{stores: map[string]qrdbsdk.KVStore{}})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cfg := triageWiringConfig(t)
	cfg.Tokens = nil

	_, _, err = newSDLCTriage(cfg, leaseTakenJobs{}, store)
	var initErr *SDLCInitError
	if !errors.As(err, &initErr) || initErr.Component != "triage runner" {
		t.Fatalf("newSDLCTriage = %v, want an SDLCInitError naming the triage runner", err)
	}
	var invalid *sdlc.InvalidInputError
	if !errors.As(err, &invalid) || invalid.Field != "tokens" {
		t.Fatalf("the cause must name the missing port: %v", err)
	}
}
