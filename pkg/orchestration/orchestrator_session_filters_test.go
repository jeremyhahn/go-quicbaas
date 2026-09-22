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
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// sessionFilterService builds a service holding durable session records and
// the persisted jobs they name, which is what a listing has to read to answer
// "which role was this" and "which daemon ran it".
func sessionFilterService(t *testing.T, sessions []*SessionRecord, jobs []*JobStatus) *agentServiceImpl {
	t.Helper()

	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService() error = %v", err)
	}
	impl := svc.(*agentServiceImpl)

	kv := kvstore.NewMemory()
	impl.SetKVStore(kv)
	for _, job := range jobs {
		encoded, marshalErr := json.Marshal(job)
		if marshalErr != nil {
			t.Fatalf("Marshal(job %s) error = %v", job.JobID, marshalErr)
		}
		if putErr := kv.Put(context.Background(), jobPersistenceKey("tenant-1", job.JobID), encoded); putErr != nil {
			t.Fatalf("Put(job %s) error = %v", job.JobID, putErr)
		}
	}
	for _, record := range sessions {
		if putErr := store.PutSession(context.Background(), record); putErr != nil {
			t.Fatalf("PutSession(%s) error = %v", record.SessionID, putErr)
		}
	}
	return impl
}

// sessionFilterFixture is three sessions across two jobs, two roles, two
// daemons, two backends and two projects — enough that every narrowing under
// test can be wrong in a visible way.
func sessionFilterFixture(t *testing.T) *agentServiceImpl {
	t.Helper()

	return sessionFilterService(t,
		[]*SessionRecord{
			{SessionID: "sess-1", TenantID: "tenant-1", ProjectID: "proj-a", JobID: "job-1", Backend: "claude", Status: SessionStateActive, CreatedAt: 1},
			{SessionID: "sess-2", TenantID: "tenant-1", ProjectID: "proj-a", JobID: "job-2", Backend: "codex", Status: SessionStateClosed, CreatedAt: 2},
			{SessionID: "sess-3", TenantID: "tenant-1", ProjectID: "proj-b", Backend: "claude", Status: SessionStateActive, CreatedAt: 3},
		},
		[]*JobStatus{
			{JobID: "job-1", Runner: "sdlc", AgentID: "agent-1"},
			{JobID: "job-2", Runner: "review", AgentID: "agent-2"},
		},
	)
}

// listSessionsForTest runs the listing as a tenant-scoped caller.
func listSessionsForTest(t *testing.T, svc *agentServiceImpl, req *ListSessionsRequest) *ListSessionsResponse {
	t.Helper()

	ctx := ContextWithTenant(context.Background(), "tenant-1")
	resp, err := svc.ListSessions(ctx, req)
	if err != nil {
		t.Fatalf("ListSessions() error = %v, want nil", err)
	}
	return resp
}

// TestListSessions_ResolvesTheRoleAndTheAgentOntoEveryRow proves both derived
// fields are filled from the session's job, and that a session belonging to
// no job carries neither rather than a guess.
func TestListSessions_ResolvesTheRoleAndTheAgentOntoEveryRow(t *testing.T) {
	t.Parallel()

	resp := listSessionsForTest(t, sessionFilterFixture(t), &ListSessionsRequest{Page: &Page{}})
	if len(resp.Sessions) != 3 {
		t.Fatalf("listed %d sessions, want 3", len(resp.Sessions))
	}
	byID := make(map[string]*SessionInfo, len(resp.Sessions))
	for _, session := range resp.Sessions {
		byID[session.SessionID] = session
	}
	if byID["sess-1"].Runner != "sdlc" || byID["sess-1"].AgentID != "agent-1" {
		t.Errorf("sess-1 = runner %q agent %q, want sdlc/agent-1", byID["sess-1"].Runner, byID["sess-1"].AgentID)
	}
	if byID["sess-2"].Runner != "review" || byID["sess-2"].AgentID != "agent-2" {
		t.Errorf("sess-2 = runner %q agent %q, want review/agent-2", byID["sess-2"].Runner, byID["sess-2"].AgentID)
	}
	if byID["sess-3"].Runner != "" || byID["sess-3"].AgentID != "" {
		t.Errorf("sess-3 = runner %q agent %q, want neither: it belongs to no job", byID["sess-3"].Runner, byID["sess-3"].AgentID)
	}
}

// TestListSessions_NarrowsOnEveryNewFilter walks each narrowing the sessions
// listing gained, one at a time, so a broken one names itself.
func TestListSessions_NarrowsOnEveryNewFilter(t *testing.T) {
	t.Parallel()

	svc := sessionFilterFixture(t)
	cases := []struct {
		name string
		req  *ListSessionsRequest
		want []string
	}{
		{"project", &ListSessionsRequest{ProjectID: "proj-a", Page: &Page{}}, []string{"sess-1", "sess-2"}},
		{"job", &ListSessionsRequest{JobID: "job-2", Page: &Page{}}, []string{"sess-2"}},
		{"runner", &ListSessionsRequest{Runner: "sdlc", Page: &Page{}}, []string{"sess-1"}},
		{"agent", &ListSessionsRequest{AgentID: "agent-2", Page: &Page{}}, []string{"sess-2"}},
		{"backend", &ListSessionsRequest{Backend: "claude", Page: &Page{}}, []string{"sess-1", "sess-3"}},
		{"state", &ListSessionsRequest{Status: SessionStateClosed, Page: &Page{}}, []string{"sess-2"}},
		{"unknown agent matches nothing", &ListSessionsRequest{AgentID: "agent-9", Page: &Page{}}, nil},
		{"compose", &ListSessionsRequest{ProjectID: "proj-a", Backend: "claude", Page: &Page{}}, []string{"sess-1"}},
	}
	for _, tc := range cases {
		resp := listSessionsForTest(t, svc, tc.req)
		got := make([]string, 0, len(resp.Sessions))
		for _, session := range resp.Sessions {
			got = append(got, session.SessionID)
		}
		if len(got) != len(tc.want) || resp.Total != len(tc.want) {
			t.Errorf("%s: listed %v (total %d), want %v", tc.name, got, resp.Total, tc.want)
			continue
		}
		for i, want := range tc.want {
			if got[i] != want {
				t.Errorf("%s: listed %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
}

// TestListSessions_CountsTheFacetsOverTheUnnarrowedSet proves the counts
// beside the unselected choices survive a selection — the property that makes
// the bar usable rather than a one-way door.
func TestListSessions_CountsTheFacetsOverTheUnnarrowedSet(t *testing.T) {
	t.Parallel()

	resp := listSessionsForTest(t, sessionFilterFixture(t),
		&ListSessionsRequest{Backend: "claude", Page: &Page{}})

	if resp.Total != 2 {
		t.Fatalf("Total = %d, want the 2 claude sessions", resp.Total)
	}
	if resp.Facets["backend"]["codex"] != 1 {
		t.Errorf("backend facet = %v, want codex still counted", resp.Facets["backend"])
	}
	if resp.Facets["state"]["closed"] != 1 || resp.Facets["state"]["active"] != 2 {
		t.Errorf("state facet = %v, want both states counted over the unnarrowed set", resp.Facets["state"])
	}
	if resp.Facets["runner"]["review"] != 1 || resp.Facets["runner"]["sdlc"] != 1 {
		t.Errorf("runner facet = %v, want a count per role", resp.Facets["runner"])
	}
	if resp.Facets["agent_id"]["agent-1"] != 1 || resp.Facets["agent_id"]["agent-2"] != 1 {
		t.Errorf("agent facet = %v, want a count per daemon", resp.Facets["agent_id"])
	}
}

// TestListSessions_FiltersComposeWithSearchAndPaging proves the narrowing,
// the search and the cut stack, and that Total counts what matched rather
// than what fit on the page.
func TestListSessions_FiltersComposeWithSearchAndPaging(t *testing.T) {
	t.Parallel()

	resp := listSessionsForTest(t, sessionFilterFixture(t),
		&ListSessionsRequest{Backend: "claude", Page: &Page{Query: "sess", Limit: 1}})

	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2 — the match count, not the page size", resp.Total)
	}
	if len(resp.Sessions) != 1 || resp.NextOffset != 1 {
		t.Errorf("page = %d records with next %d, want 1 and a second page", len(resp.Sessions), resp.NextOffset)
	}

	// A search that matches nothing narrows to nothing rather than falling
	// back to the whole listing.
	empty := listSessionsForTest(t, sessionFilterFixture(t),
		&ListSessionsRequest{Page: &Page{Query: "no-such-session"}})
	if empty.Total != 0 || len(empty.Sessions) != 0 {
		t.Errorf("unmatched search = %d records, want none", empty.Total)
	}
}

// TestListSessions_NilPageAnswersEverythingAndCountsNothing proves the health
// monitor's sweep — the one caller that asks for no page — still sees every
// session and is not charged for the counts.
func TestListSessions_NilPageAnswersEverythingAndCountsNothing(t *testing.T) {
	t.Parallel()

	resp := listSessionsForTest(t, sessionFilterFixture(t), &ListSessionsRequest{})
	if len(resp.Sessions) != 3 {
		t.Errorf("listed %d sessions, want all 3 uncut", len(resp.Sessions))
	}
	if resp.Facets != nil {
		t.Errorf("Facets = %v, want none for a caller that asked for no page", resp.Facets)
	}
}

// TestListSessions_RefusesAnUnknownState proves the state filter still
// refuses rather than answering "there are no such sessions".
func TestListSessions_RefusesAnUnknownState(t *testing.T) {
	t.Parallel()

	ctx := ContextWithTenant(context.Background(), "tenant-1")
	resp, err := sessionFilterFixture(t).ListSessions(ctx, &ListSessionsRequest{Status: "runnning", Page: &Page{}})
	if resp != nil {
		t.Error("ListSessions() answered a request naming a state it does not have")
	}
	if err == nil {
		t.Fatal("error = nil, want a refusal naming the unknown state")
	}
}
