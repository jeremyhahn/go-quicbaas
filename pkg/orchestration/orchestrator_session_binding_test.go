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
	"encoding/json"
	"testing"
)

// These tests pin the return leg of task → job → session (go-quicbaas#860):
// a session row names the job that spawned it and the container its agent
// runs in, so the session page can link back to the job and show the
// container. A session nobody's job spawned carries neither.

// TestListSessions_StoredRowCarriesItsJobAndContainer covers the durable
// branch: a session that is no longer live is listed from its record alone.
func TestListSessions_StoredRowCarriesItsJobAndContainer(t *testing.T) {
	t.Parallel()

	svc := sessionFilterService(t,
		[]*SessionRecord{
			{SessionID: "sess-job", TenantID: "tenant-1", JobID: "job-1", ContainerID: "ctr-0123456789abcdef", Backend: "claude", Status: SessionStateClosed, CreatedAt: 1},
			{SessionID: "sess-direct", TenantID: "tenant-1", Backend: "claude", Status: SessionStateClosed, CreatedAt: 2},
		},
		nil,
	)

	byID := sessionsByID(listSessionsForTest(t, svc, &ListSessionsRequest{}))
	if got := byID["sess-job"]; got.JobID != "job-1" || got.ContainerID != "ctr-0123456789abcdef" {
		t.Errorf("sess-job = job %q container %q, want job-1/ctr-0123456789abcdef", got.JobID, got.ContainerID)
	}
	if got := byID["sess-direct"]; got.JobID != "" || got.ContainerID != "" {
		t.Errorf("sess-direct = job %q container %q, want neither: no job spawned it", got.JobID, got.ContainerID)
	}
}

// TestListSessions_LiveRowCarriesItsJobAndContainer covers the live branch,
// including the reconstituted session whose live entry carries no container
// and whose durable record answers for it.
func TestListSessions_LiveRowCarriesItsJobAndContainer(t *testing.T) {
	t.Parallel()

	svc := sessionFilterService(t,
		[]*SessionRecord{
			{SessionID: "sess-live", TenantID: "tenant-1", JobID: "job-1", ContainerID: "ctr-live", Backend: "claude", Status: SessionStateActive, CreatedAt: 1},
			{SessionID: "sess-rebuilt", TenantID: "tenant-1", JobID: "job-2", ContainerID: "ctr-durable", Backend: "claude", Status: SessionStateActive, CreatedAt: 2},
			{SessionID: "sess-direct", TenantID: "tenant-1", Backend: "claude", Status: SessionStateActive, CreatedAt: 3},
		},
		nil,
	)
	svc.sessions.Store("sess-live", &sessionRecord{Backend: "claude", TenantID: "tenant-1", JobID: "job-1", ContainerID: "ctr-live", CreatedAt: 1})
	svc.sessions.Store("sess-rebuilt", &sessionRecord{Backend: "claude", TenantID: "tenant-1", CreatedAt: 2})
	svc.sessions.Store("sess-direct", &sessionRecord{Backend: "claude", TenantID: "tenant-1", CreatedAt: 3})

	byID := sessionsByID(listSessionsForTest(t, svc, &ListSessionsRequest{}))
	if got := byID["sess-live"]; got.JobID != "job-1" || got.ContainerID != "ctr-live" {
		t.Errorf("sess-live = job %q container %q, want job-1/ctr-live", got.JobID, got.ContainerID)
	}
	if got := byID["sess-rebuilt"]; got.JobID != "job-2" || got.ContainerID != "ctr-durable" {
		t.Errorf("sess-rebuilt = job %q container %q, want job-2/ctr-durable from the durable record", got.JobID, got.ContainerID)
	}
	if got := byID["sess-direct"]; got.JobID != "" || got.ContainerID != "" {
		t.Errorf("sess-direct = job %q container %q, want neither", got.JobID, got.ContainerID)
	}
}

// TestSessionInfo_JobAndContainerAreOmittedWhenAbsent pins the JSON the REST,
// QUIC and MCP transports send: a bound session names its job and container,
// and an unbound one leaves both keys out rather than sending "" — the page
// tests for presence, and an empty string is not "no job".
func TestSessionInfo_JobAndContainerAreOmittedWhenAbsent(t *testing.T) {
	t.Parallel()

	bound, err := json.Marshal(&SessionInfo{SessionID: "s-job", JobID: "job-1", ContainerID: "ctr-1"})
	if err != nil {
		t.Fatalf("Marshal(bound) error = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(bound, &fields); err != nil {
		t.Fatalf("Unmarshal(bound) error = %v", err)
	}
	if fields["job_id"] != "job-1" || fields["container_id"] != "ctr-1" {
		t.Errorf("bound session JSON = %s, want job_id job-1 and container_id ctr-1", bound)
	}

	direct, err := json.Marshal(&SessionInfo{SessionID: "s-direct"})
	if err != nil {
		t.Fatalf("Marshal(direct) error = %v", err)
	}
	fields = nil
	if err := json.Unmarshal(direct, &fields); err != nil {
		t.Fatalf("Unmarshal(direct) error = %v", err)
	}
	for _, key := range []string{"job_id", "container_id"} {
		if _, present := fields[key]; present {
			t.Errorf("unbound session JSON = %s, want no %q key", direct, key)
		}
	}
}

func sessionsByID(resp *ListSessionsResponse) map[string]*SessionInfo {
	byID := make(map[string]*SessionInfo, len(resp.Sessions))
	for _, session := range resp.Sessions {
		byID[session.SessionID] = session
	}
	return byID
}
