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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// testContainerIdentity is the identity the sandboxSpec unit tests launch
// with. The label tests below use their own.
var testContainerIdentity = containerIdentity{SessionID: "ses-test", JobID: "job-test", TenantID: "tenant-test"}

// reaperClock is the fixed "now" every reaper test judges ages against.
var reaperClock = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// fakeOwnedRuntime is an OwnedContainerLister over an in-memory host.
type fakeOwnedRuntime struct {
	mu         sync.Mutex
	containers []OwnedContainer
	listErr    error
	// removeErrs fails Remove for the named containers.
	removeErrs map[string]error
	removed    []string
}

func (f *fakeOwnedRuntime) ListOwned(_ context.Context, _ string) ([]OwnedContainer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]OwnedContainer(nil), f.containers...), nil
}

func (f *fakeOwnedRuntime) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.removeErrs[id]; err != nil {
		return err
	}
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeOwnedRuntime) removedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.removed...)
	sort.Strings(out)
	return out
}

func newTestReaper(t *testing.T, rt OwnedContainerLister, live ...string) (*ContainerReaper, *recordingLogger) {
	t.Helper()
	held := make(map[string]bool, len(live))
	for _, id := range live {
		held[id] = true
	}
	logger := newRecordingLogger()
	reaper, err := NewContainerReaper(ContainerReaperConfig{
		Runtime: rt,
		Owner:   "/var/lib/quicbaas-agent",
		Live:    func(id string) bool { return held[id] },
		Grace:   15 * time.Minute,
		Now:     func() time.Time { return reaperClock },
		Logger:  logger,
	})
	if err != nil {
		t.Fatalf("NewContainerReaper: %v", err)
	}
	return reaper, logger
}

// TestContainerReaper_RemovesOnlyUnheldContainersPastGrace is the reaper's
// whole contract: a container a live session holds is never touched, a young
// container may still be registering, and everything else is an orphan.
func TestContainerReaper_RemovesOnlyUnheldContainersPastGrace(t *testing.T) {
	old := reaperClock.Add(-3 * time.Hour)
	rt := &fakeOwnedRuntime{containers: []OwnedContainer{
		{ID: "live", Name: "quicbaas-acp-live", CreatedAt: old, SessionID: "ses-live", JobID: "job-1"},
		{ID: "closed", Name: "quicbaas-acp-closed", CreatedAt: old, SessionID: "ses-closed", JobID: "job-2", TenantID: "acme"},
		{ID: "young", Name: "quicbaas-acp-young", CreatedAt: reaperClock.Add(-time.Minute), SessionID: "ses-registering"},
		{ID: "unlabelled", Name: "quicbaas-acp-legacy", CreatedAt: old},
		{ID: "undated", Name: "quicbaas-acp-undated", SessionID: "ses-gone"},
	}}
	reaper, logger := newTestReaper(t, rt, "ses-live")

	sweep, err := reaper.Reap(t.Context())
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if got := strings.Join(rt.removedIDs(), ","); got != "closed,unlabelled" {
		t.Fatalf("removed %s, want exactly the two unheld containers past grace", got)
	}
	if len(sweep.Removals) != 2 {
		t.Fatalf("audit records = %d, want 2", len(sweep.Removals))
	}
	for _, removal := range sweep.Removals {
		if removal.Reason == "" {
			t.Errorf("removal of %s carries no reason", removal.Container.ID)
		}
	}
	audit := logger.atLevel("WARN")
	for _, want := range []string{"session=ses-closed", "job=job-2", "tenant=acme", "names no session"} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit log lacks %q:\n%s", want, audit)
		}
	}
}

// TestContainerReaper_KeepsGoingPastARemovalThatFails: one stuck container
// must not shelter every other orphan, and the stuck one is still reported.
func TestContainerReaper_KeepsGoingPastARemovalThatFails(t *testing.T) {
	old := reaperClock.Add(-time.Hour)
	rt := &fakeOwnedRuntime{
		containers: []OwnedContainer{
			{ID: "stuck", CreatedAt: old, SessionID: "a"},
			{ID: "gone", CreatedAt: old, SessionID: "b"},
			{ID: "fine", CreatedAt: old, SessionID: "c"},
		},
		removeErrs: map[string]error{
			"stuck": &ContainerStopError{ContainerID: "stuck", Cause: errors.New("busy")},
			"gone":  &ContainerNotFoundError{ContainerID: "gone"},
		},
	}
	reaper, _ := newTestReaper(t, rt)

	sweep, err := reaper.Reap(t.Context())
	var sweepErr *ContainerSweepError
	if !errors.As(err, &sweepErr) || sweepErr.ContainerID != "stuck" {
		t.Fatalf("err = %v, want a ContainerSweepError naming the stuck container", err)
	}
	// A container already gone is the outcome asked for.
	got := append([]string(nil), sweep.Removed...)
	sort.Strings(got)
	if strings.Join(got, ",") != "fine,gone" {
		t.Fatalf("removed = %v, want the two that went", got)
	}
}

// TestContainerReaper_ReportsAnEnumerationFailure: a runtime that will not
// list is a pass that did not run, not a pass that found nothing.
func TestContainerReaper_ReportsAnEnumerationFailure(t *testing.T) {
	listErr := &ContainerSweepError{Owner: "x", Cause: errors.New("daemon down")}
	reaper, _ := newTestReaper(t, &fakeOwnedRuntime{listErr: listErr})
	if _, err := reaper.Reap(t.Context()); !errors.Is(err, ErrContainerSweep) {
		t.Fatalf("err = %v, want ErrContainerSweep", err)
	}
}

// TestNewContainerReaper_RefusesAnIncompleteConfiguration covers every
// required field: each one missing would make the reaper reap the wrong thing
// or reap silently.
func TestNewContainerReaper_RefusesAnIncompleteConfiguration(t *testing.T) {
	valid := ContainerReaperConfig{
		Runtime: &fakeOwnedRuntime{},
		Owner:   "/var/lib/quicbaas-agent",
		Live:    func(string) bool { return false },
		Logger:  newRecordingLogger(),
	}
	cases := map[string]func(*ContainerReaperConfig){
		"no owner":       func(c *ContainerReaperConfig) { c.Owner = "  " },
		"no runtime":     func(c *ContainerReaperConfig) { c.Runtime = nil },
		"no live check":  func(c *ContainerReaperConfig) { c.Live = nil },
		"no logger":      func(c *ContainerReaperConfig) { c.Logger = nil },
		"negative grace": func(c *ContainerReaperConfig) { c.Grace = -time.Second },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			mutate(&cfg)
			if _, err := NewContainerReaper(cfg); err == nil {
				t.Fatal("NewContainerReaper accepted an incomplete configuration")
			}
		})
	}
	reaper, err := NewContainerReaper(valid)
	if err != nil {
		t.Fatalf("NewContainerReaper(valid): %v", err)
	}
	if reaper.grace != DefaultContainerReapGrace {
		t.Errorf("grace = %s, want the default", reaper.grace)
	}
}

// TestContainerReaper_RunReapsOnEveryTickUntilCancelled drives the loop with a
// tick channel the test owns, so nothing sleeps.
func TestContainerReaper_RunReapsOnEveryTickUntilCancelled(t *testing.T) {
	rt := &fakeOwnedRuntime{containers: []OwnedContainer{
		{ID: "orphan", CreatedAt: reaperClock.Add(-time.Hour), SessionID: "ses-gone"},
	}}
	reaper, _ := newTestReaper(t, rt)

	ctx, cancel := context.WithCancel(t.Context())
	ticks := make(chan time.Time)
	reports := make(chan ContainerSweep)
	done := make(chan struct{})
	go func() {
		defer close(done)
		reaper.Run(ctx, ticks, func(sweep ContainerSweep, err error) {
			if err != nil {
				t.Errorf("pass failed: %v", err)
			}
			reports <- sweep
		})
	}()

	ticks <- reaperClock
	if sweep := <-reports; len(sweep.Removed) != 1 {
		t.Fatalf("first pass removed %v, want the orphan", sweep.Removed)
	}
	cancel()
	<-done
}

// TestContainerIdentity_LabelsNameTheSessionJobTenantAndOwner: the labels are
// what maps a container back to its session; a placeholder maps it to nothing.
func TestContainerIdentity_LabelsNameTheSessionJobTenantAndOwner(t *testing.T) {
	labels := containerIdentity{SessionID: "ses-1", JobID: "job-1", TenantID: "acme"}.labels("claude", "/work")
	want := map[string]string{
		ContainerAgentLabel:   "claude",
		ContainerSessionLabel: "ses-1",
		ContainerJobLabel:     "job-1",
		ContainerTenantLabel:  "acme",
		ContainerOwnerLabel:   "/work",
	}
	for k, v := range want {
		if labels[k] != v {
			t.Errorf("label %s = %q, want %q", k, labels[k], v)
		}
	}
	bare := containerIdentity{SessionID: "ses-2"}.labels("claude", "")
	for _, k := range []string{ContainerJobLabel, ContainerTenantLabel, ContainerOwnerLabel} {
		if _, ok := bare[k]; ok {
			t.Errorf("an unset %s was stamped anyway", k)
		}
	}
}

// TestNewContainerIdentity_DecidesTheIDBeforeTheLaunch: a resume keeps its ID,
// a fresh spawn gets one, and the job falls back to the turn attribution.
func TestNewContainerIdentity_DecidesTheIDBeforeTheLaunch(t *testing.T) {
	ctx := ContextWithTenant(t.Context(), "acme")
	resumed, err := newContainerIdentity(ctx, &SpawnRequest{JobID: "job-req"}, "ses-original")
	if err != nil {
		t.Fatalf("newContainerIdentity: %v", err)
	}
	if resumed.SessionID != "ses-original" || resumed.JobID != "job-req" || resumed.TenantID != "acme" {
		t.Fatalf("resumed identity = %+v", resumed)
	}

	attributed := WithTurnAttribution(ctx, TurnAttribution{JobID: "job-ctx"})
	fresh, err := newContainerIdentity(attributed, &SpawnRequest{}, "")
	if err != nil {
		t.Fatalf("newContainerIdentity: %v", err)
	}
	if !strings.HasPrefix(fresh.SessionID, "ses-") || fresh.JobID != "job-ctx" {
		t.Fatalf("fresh identity = %+v, want a generated ID and the attributed job", fresh)
	}
}

// TestListOwned_DescribesEveryOwnedContainer runs the real runtime adapter
// against the fake CLI: one ps by label, one inspect, parsed into identities.
func TestListOwned_DescribesEveryOwnedContainer(t *testing.T) {
	fake := newFakeRuntimeCLI(t)
	const owner = "/var/lib/quicbaas-agent"
	fake.add("aaaa", owner)
	fake.describe("aaaa", "2026-09-24T09:56:49.924465602Z", "ses-a", "job-a", "acme")
	fake.add("bbbb", owner)
	fake.describe("bbbb", "2026-09-24 03:58:52.671196155 +0000 UTC", "", "", "")
	fake.add("cccc", "/someone/else")
	fake.describe("cccc", "2026-09-24T00:00:00Z", "ses-c", "", "")

	got, err := fake.runtime().ListOwned(t.Context(), owner)
	if err != nil {
		t.Fatalf("ListOwned: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
	if len(got) != 2 || got[0].ID != "aaaa" || got[1].ID != "bbbb" {
		t.Fatalf("ListOwned = %+v, want this owner's two containers", got)
	}
	a := got[0]
	if a.Name != "aaaa" || a.SessionID != "ses-a" || a.JobID != "job-a" || a.TenantID != "acme" {
		t.Errorf("container a = %+v", a)
	}
	if want := time.Date(2026, 9, 24, 9, 56, 49, 924465602, time.UTC); !a.CreatedAt.Equal(want) {
		t.Errorf("docker created = %s, want %s", a.CreatedAt, want)
	}
	if got[1].CreatedAt.IsZero() {
		t.Error("podman's created format did not parse")
	}
}

// TestListOwned_ReportsAnInspectItCouldNotMake is the failure twin.
func TestListOwned_ReportsAnInspectItCouldNotMake(t *testing.T) {
	fake := newFakeRuntimeCLI(t)
	fake.add("aaaa", "/var/lib/quicbaas-agent")
	fake.inspectExit = 1
	fake.write()

	if _, err := fake.runtime().ListOwned(t.Context(), "/var/lib/quicbaas-agent"); !errors.Is(err, ErrContainerSweep) {
		t.Fatalf("err = %v, want ErrContainerSweep", err)
	}
	if _, err := fake.runtime().ListOwned(t.Context(), " "); !errors.Is(err, ErrContainerSweep) {
		t.Fatalf("an empty owner err = %v, want a refusal", err)
	}
}

// TestParseContainerCreated_UnreadableIsZero: a time the reaper cannot read
// must never look old.
func TestParseContainerCreated_UnreadableIsZero(t *testing.T) {
	if !parseContainerCreated("yesterday-ish").IsZero() {
		t.Fatal("garbage parsed as a time")
	}
	if parseContainerCreated("2026-09-24T00:09:28.809990574Z").IsZero() {
		t.Fatal("RFC 3339 did not parse")
	}
}
