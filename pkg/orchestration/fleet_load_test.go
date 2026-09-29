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
