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
	"reflect"
	"slices"
	"testing"
)

// TestLiveJobsByHost_CountsJobsPerAgentHost: a job's several sessions on one
// host occupy one slot, a session with no job is one of its own, and a session
// on a local backend — or one with no host recorded — is on no agent.
func TestLiveJobsByHost_CountsJobsPerAgentHost(t *testing.T) {
	svc := newTestService(t)
	records := map[string]*sessionRecord{
		"lead":     {JobID: "job-1", HostAffinity: "vs02:9091"},
		"worker":   {JobID: "job-1", HostAffinity: "vs02:9091"},
		"reviewer": {JobID: "job-2", HostAffinity: "vs02:9091"},
		"ad-hoc":   {HostAffinity: "vs03:9091"},
		"local":    {JobID: "job-3", BackendLocal: true},
		"unplaced": {JobID: "job-4"},
	}
	for id, rec := range records {
		svc.sessions.Store(id, rec)
	}
	want := map[string]int{"vs02:9091": 2, "vs03:9091": 1}
	if got := svc.LiveJobsByHost(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LiveJobsByHost = %v, want %v", got, want)
	}
}

// TestLiveJobsByHost_NothingLiveIsAnEmptyFleet: no sessions, no load, and a
// record of the wrong shape is skipped rather than counted.
func TestLiveJobsByHost_NothingLiveIsAnEmptyFleet(t *testing.T) {
	svc := newTestService(t)
	if got := svc.LiveJobsByHost(); len(got) != 0 {
		t.Fatalf("LiveJobsByHost = %v, want empty", got)
	}
	svc.sessions.Store("odd", "not a record")
	if got := svc.LiveJobsByHost(); len(got) != 0 {
		t.Fatalf("LiveJobsByHost = %v, want a malformed record skipped", got)
	}
}

func TestJobSlotHosts_NamesTheHostsWhereTheJobIsLive(t *testing.T) {
	svc := newTestService(t)
	records := map[string]*sessionRecord{
		"lead":     {JobID: "job-1", HostAffinity: "vs02:9091"},
		"worker":   {JobID: "job-1", HostAffinity: "vs03:9091"},
		"other":    {JobID: "job-2", HostAffinity: "vs04:9091"},
		"local":    {JobID: "job-1", BackendLocal: true},
		"unplaced": {JobID: "job-1"},
	}
	for id, rec := range records {
		svc.sessions.Store(id, rec)
	}
	want := JobSlotHosts{"vs02:9091": {}, "vs03:9091": {}}
	if got := svc.jobSlotHosts("job-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("jobSlotHosts(job-1) = %v, want %v", got, want)
	}
}

func TestJobSlotHosts_NoJobHoldsNoSlot(t *testing.T) {
	svc := newTestService(t)
	svc.sessions.Store("ad-hoc", &sessionRecord{HostAffinity: "vs02:9091"})
	if got := svc.jobSlotHosts(""); got != nil {
		t.Fatalf("jobSlotHosts(\"\") = %v, want nil: an ad-hoc session is no job", got)
	}
	if got := svc.jobSlotHosts("job-9"); len(got) != 0 {
		t.Fatalf("jobSlotHosts(job-9) = %v, want none for a job with nothing live", got)
	}
}

func TestJobSlotHostsContext_RoundTripsAndIgnoresNoHosts(t *testing.T) {
	hosts := JobSlotHosts{"vs02:9091": {}}
	got := JobSlotHostsFromContext(WithJobSlotHosts(context.Background(), hosts))
	if !got.Holds("vs02:9091") || got.Holds("vs03:9091") {
		t.Fatalf("JobSlotHostsFromContext = %v, want only vs02:9091", got)
	}
	ctx := context.Background()
	if WithJobSlotHosts(ctx, nil) != ctx || JobSlotHostsFromContext(ctx) != nil {
		t.Fatal("no hosts must leave the context unmarked")
	}
	var nilCtx context.Context
	if JobSlotHostsFromContext(nilCtx) != nil {
		t.Fatal("a nil context carries no slots")
	}
}

// SpawnSession hands placement the hosts where the spawn's job already holds a
// slot, and none for a job with nothing live.
func TestSpawnSession_ThreadsTheJobSlotHostsToPlacement(t *testing.T) {
	for name, tc := range map[string]struct {
		jobID string
		want  bool
	}{
		"job live on the host": {jobID: "job-1", want: true},
		"another job":          {jobID: "job-2", want: false},
		"no job":               {jobID: "", want: false},
	} {
		svc, _ := newStrictAffinityService(t, nil)
		svc.sessions.Store("lead", &sessionRecord{JobID: "job-1", HostAffinity: "vs02:9091"})
		var seen JobSlotHosts
		svc.SetRemoteBackendResolver(func(ctx context.Context, _, _ string) (AgentBackend, bool) {
			seen = JobSlotHostsFromContext(ctx)
			return nil, false
		})
		ctx := ContextWithTenant(context.Background(), "tenant-1")
		if _, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "opencode", JobID: tc.jobID}); err != nil {
			t.Fatalf("%s: SpawnSession: %v", name, err)
		}
		if got := seen.Holds("vs02:9091"); got != tc.want {
			t.Errorf("%s: placement saw a slot on vs02:9091 = %v, want %v", name, got, tc.want)
		}
	}
}

// TestReconcileAgentBackends_SigningRelayTrait: a heartbeat that reports the
// relay adds the trait, one that reports none removes it, and one that says
// nothing about it — an older build — leaves it as registered.
func TestReconcileAgentBackends_SigningRelayTrait(t *testing.T) {
	on, off := true, false
	for name, tc := range map[string]struct {
		registered []string
		relay      *bool
		want       bool
	}{
		"reported, not registered": {[]string{"codex"}, &on, true},
		"withdrawn":                {[]string{"codex", FleetNodeTraitSigningRelay}, &off, false},
		"silent keeps it":          {[]string{"codex", FleetNodeTraitSigningRelay}, nil, true},
		"silent, never had it":     {[]string{"codex"}, nil, false},
		"reported twice is once":   {[]string{"codex", FleetNodeTraitSigningRelay}, &on, true},
	} {
		agent := &FleetNode{Backend: "codex", Capabilities: tc.registered}
		reconcileFleetNodeBackends(agent, &FleetNodeObservation{Backends: []string{"codex"}, SigningRelay: tc.relay})
		count := 0
		for _, capability := range agent.Capabilities {
			if capability == FleetNodeTraitSigningRelay {
				count++
			}
		}
		if got := slices.Contains(agent.Capabilities, FleetNodeTraitSigningRelay); got != tc.want || count > 1 {
			t.Errorf("%s: capabilities = %v, want relay=%v once", name, agent.Capabilities, tc.want)
		}
	}
}
