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

// session_state_matrix_test.go is the WHOLE class, stated once.
//
// The defect this file exists for was found one cell at a time: a record in
// `resumable` was skipped by the job-terminal conclusion because the gate
// admitted only `active` and `idle`, so after a restart — which turns every
// persistent local session resumable — a concluded job left its sessions
// resume-ready for work that was over, and the periodic sweep used the same
// gate and never repaired them. Patching that one cell would have left the
// question "what about the others?" open, and the honest answer is a matrix.
//
// So: every session state, crossed with every path that may end or repair a
// record, with the expected outcome pinned and the deliberate exclusions
// asserted as exclusions rather than left to look like oversights.
//
//	state      | job terminal | sweep: job done | sweep: agent gone | agent gone
//	-----------+--------------+-----------------+-------------------+-----------
//	active     | CLOSED       | CLOSED          | CLOSED            | CLOSED
//	idle       | CLOSED       | CLOSED          | CLOSED            | CLOSED
//	resumable  | CLOSED       | CLOSED          | left              | left
//	closed     | left         | left            | left              | left
//	error      | left         | left            | left              | left
//
// The two "left" cells for `resumable` are the design and are commented at
// their gates: a resumable session has no process BY DESIGN, so neither "the
// daemon holding it is gone" nor "it has been quiet" is a fact about it, and
// ending it on either would destroy a session an operator is deliberately
// keeping. Its job concluding is the one fact that says it has nothing left to
// be resumed for.
//
// `closed` is a tombstone and `error` carries its own explanation; overwriting
// either with a fresh conclusion would erase what a reader needs. An operator
// can still close an errored record explicitly, which is asserted below.

package orchestration

import (
	"context"
	"testing"
	"time"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// matrixPath is one way a record can be ended or repaired.
type matrixPath struct {
	name string
	// run drives the path against a service that already holds the record.
	run func(t *testing.T, svc *agentServiceImpl, tenantID, jobID string)
}

// matrixCase is one cell: a starting state and what each path must leave.
type matrixCase struct {
	state SessionState
	// wantClosed maps path name -> whether the record must end up closed.
	wantClosed map[string]bool
}

// newMatrixHarness builds a service holding one seeded record, with the fleet
// facts every path reads: a terminal job, and a registry naming no agent.
func newMatrixHarness(t *testing.T, state SessionState, jobID string) (*agentServiceImpl, AgentStore) {
	t.Helper()
	svc, store := reconcileHarness(t)
	svc.SetFleetRegistry(NewFleetRegistry(kvstore.NewMemory()))
	// Every idle bound is zero, so the sweep's quiet gate admits the record and
	// the verdict below it is what decides. The record is stamped old anyway.
	seedSession(t, store, &SessionRecord{
		SessionID:    "s-matrix",
		TenantID:     "t1",
		JobID:        jobID,
		Status:       state,
		Persistent:   true,
		BackendLocal: true,
		HostAffinity: "daemon-gone",
		LastActivity: time.Now().Add(-72 * time.Hour).UnixNano(),
	})
	return svc, store
}

func matrixStoredState(t *testing.T, store AgentStore) SessionState {
	t.Helper()
	record, err := store.GetSession(context.Background(), "t1", "s-matrix")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return record.Status
}

// TestSessionStateMatrix_EveryStateAgainstEveryConclusionPath is the audit.
func TestSessionStateMatrix_EveryStateAgainstEveryConclusionPath(t *testing.T) {
	const jobID = "j-matrix"

	paths := []matrixPath{
		{
			name: "job terminal",
			run: func(t *testing.T, svc *agentServiceImpl, tenantID, jobID string) {
				if _, err := svc.ConcludeSessionsForJob(
					ContextWithTenant(context.Background(), tenantID), tenantID, jobID); err != nil {
					t.Fatalf("ConcludeSessionsForJob: %v", err)
				}
			},
		},
		{
			name: "sweep: job done",
			run: func(t *testing.T, svc *agentServiceImpl, tenantID, jobID string) {
				// The job is persisted terminal, so sweepVerdict's first branch
				// is the one that fires.
				seedJob(t, svc, tenantID, jobID, JobStateFailed)
				if _, err := svc.SweepSessions(context.Background()); err != nil {
					t.Fatalf("SweepSessions: %v", err)
				}
			},
		},
		{
			name: "sweep: agent gone",
			run: func(t *testing.T, svc *agentServiceImpl, tenantID, jobID string) {
				// No job record at all, so jobTerminalState declines and the
				// host-absence branch is the only evidence available. The
				// registry is empty, so the pinned daemon is absent.
				if _, err := svc.SweepSessions(context.Background()); err != nil {
					t.Fatalf("SweepSessions: %v", err)
				}
			},
		},
		{
			name: "agent gone",
			run: func(t *testing.T, svc *agentServiceImpl, tenantID, jobID string) {
				if _, err := svc.ConcludeSessionsForFleetNode(
					ContextWithTenant(context.Background(), tenantID), tenantID,
					&FleetNode{ID: "a1", Name: "daemon-gone", Host: "daemon-gone"},
					"the daemon left the fleet"); err != nil {
					t.Fatalf("ConcludeSessionsForFleetNode: %v", err)
				}
			},
		},
	}

	cases := []matrixCase{
		{
			state: SessionStateActive,
			wantClosed: map[string]bool{
				"job terminal": true, "sweep: job done": true,
				"sweep: agent gone": true, "agent gone": true,
			},
		},
		{
			state: SessionStateIdle,
			wantClosed: map[string]bool{
				"job terminal": true, "sweep: job done": true,
				"sweep: agent gone": true, "agent gone": true,
			},
		},
		{
			// THE CELL THAT WAS BROKEN. A restart turns every persistent local
			// session resumable, so this row is the ordinary state of a fleet
			// that has been restarted once — and its job concluding must end it.
			state: SessionStateResumable,
			wantClosed: map[string]bool{
				"job terminal": true, "sweep: job done": true,
				"sweep: agent gone": false, "agent gone": false,
			},
		},
		{
			state: SessionStateClosed,
			wantClosed: map[string]bool{
				"job terminal": false, "sweep: job done": false,
				"sweep: agent gone": false, "agent gone": false,
			},
		},
		{
			state: SessionStateError,
			wantClosed: map[string]bool{
				"job terminal": false, "sweep: job done": false,
				"sweep: agent gone": false, "agent gone": false,
			},
		},
	}

	for _, tc := range cases {
		for _, path := range paths {
			t.Run(string(tc.state)+"/"+path.name, func(t *testing.T) {
				svc, store := newMatrixHarness(t, tc.state, jobID)
				path.run(t, svc, "t1", jobID)

				got := matrixStoredState(t, store)
				wantClosed := tc.wantClosed[path.name]
				switch {
				case wantClosed && got != SessionStateClosed:
					t.Errorf("state after %q = %q, want closed; a record this path is "+
						"responsible for was left claiming it has something behind it",
						path.name, got)
				case !wantClosed && got != tc.state:
					t.Errorf("state after %q = %q, want %q unchanged; this path is not "+
						"entitled to end a record in that state", path.name, got, tc.state)
				}
			})
		}
	}
}

// TestSessionStateMatrix_RestartRow is the restart column, which does not fit
// the table above because it decides per record rather than per path.
func TestSessionStateMatrix_RestartRow(t *testing.T) {
	cases := []struct {
		state SessionState
		// wantStored is the durable state after the boot.
		wantStored SessionState
		// wantLive is whether the record is put back in the live map.
		wantLive bool
	}{
		// A persistent local session comes back addressable and awaiting its
		// first prompt. That is the whole point of restoration.
		{SessionStateActive, SessionStateResumable, true},
		{SessionStateIdle, SessionStateIdle, true},
		{SessionStateResumable, SessionStateResumable, true},
		// A tombstone is not restored and not touched.
		{SessionStateClosed, SessionStateClosed, false},
		// A recorded resume failure is neither restored nor concluded: it keeps
		// saying what went wrong until somebody acts on it.
		{SessionStateError, SessionStateError, false},
	}

	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			svc, store := reconcileHarness(t)
			seedSession(t, store, &SessionRecord{
				SessionID:    "s-matrix",
				TenantID:     "t1",
				Status:       tc.state,
				Persistent:   true,
				BackendLocal: true,
			})

			if err := svc.LoadPersistedSessions(context.Background()); err != nil {
				t.Fatalf("LoadPersistedSessions: %v", err)
			}

			if got := matrixStoredState(t, store); got != tc.wantStored {
				t.Errorf("stored state after boot = %q, want %q", got, tc.wantStored)
			}
			if _, live := svc.liveSessionRecord("s-matrix"); live != tc.wantLive {
				t.Errorf("live after boot = %v, want %v", live, tc.wantLive)
			}
		})
	}
}

// TestSessionStateMatrix_OperatorCloseRow is the operator column. The verb has
// to reach EVERY non-closed state, because it is the one an operator uses on
// exactly the records the automatic paths decline to touch.
func TestSessionStateMatrix_OperatorCloseRow(t *testing.T) {
	for _, state := range []SessionState{
		SessionStateActive, SessionStateIdle, SessionStateResumable, SessionStateError,
	} {
		t.Run(string(state), func(t *testing.T) {
			svc, store := reconcileHarness(t)
			seedSession(t, store, &SessionRecord{
				SessionID:    "s-matrix",
				TenantID:     "t1",
				Status:       state,
				Persistent:   true,
				BackendLocal: true,
			})

			ctx := ContextWithTenant(context.Background(), "t1")
			if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-matrix"}); err != nil {
				t.Fatalf("CloseSession on a %s record: %v; the records that most need "+
					"closing must not be the ones the close verb cannot touch", state, err)
			}
			if got := matrixStoredState(t, store); got != SessionStateClosed {
				t.Errorf("state after operator close = %q, want closed", got)
			}
		})
	}

	// And the tombstone answers idempotently rather than "not found".
	t.Run("closed", func(t *testing.T) {
		svc, store := reconcileHarness(t)
		seedSession(t, store, &SessionRecord{
			SessionID: "s-matrix", TenantID: "t1", Status: SessionStateClosed,
		})
		ctx := ContextWithTenant(context.Background(), "t1")
		if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-matrix"}); err != nil {
			t.Fatalf("closing an already-closed session reported an error: %v", err)
		}
		if got := matrixStoredState(t, store); got != SessionStateClosed {
			t.Errorf("state = %q, want closed", got)
		}
	})
}

// remoteGoneBackend answers Close the way a real remote daemon does for a
// session it no longer holds: a gRPC NotFound wrapped in the transport's own
// error type. The sentinel does NOT survive that wire, which is the point.
type remoteGoneBackend struct {
	*mockAgentBackend
}

func (b *remoteGoneBackend) Close(context.Context, string) error {
	return &RemoteBackendRPCError{
		Op:  "Close",
		Err: grpcstatus.Error(grpccodes.NotFound, "acp: session not found"),
	}
}

// TestCloseSession_ARemoteDaemonsGoneSessionIsClosed is the operator's actual
// complaint.
//
// A session whose remote daemon is long gone still has a live entry, so the
// close takes the LIVE path and asks that backend to tear the process down.
// The daemon answers NotFound, RemoteACPBackend wraps it, and isSessionGoneError
// — which matched only the local sentinel — did not recognise it. The teardown
// was reported as a failure and the close refused with "acp: session not
// found", so the records that most needed closing were the only ones the close
// verb could not touch. The stranded listing, defended by the verb meant to
// clear it.
func TestCloseSession_ARemoteDaemonsGoneSessionIsClosed(t *testing.T) {
	backend := &remoteGoneBackend{mockAgentBackend: &mockAgentBackend{backendType: "mock"}}
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T", svc)
	}
	impl.SetKVStore(kvstore.NewMemory())

	seedSession(t, store, &SessionRecord{SessionID: "s-matrix", TenantID: "t1"})
	// Live, and NOT resumable: this is a remote session restored as live, which
	// is the shape that reaches the backend rather than short-circuiting.
	impl.sessions.Store("s-matrix", &sessionRecord{Backend: "mock", TenantID: "t1"})

	ctx := ContextWithTenant(context.Background(), "t1")
	if _, closeErr := impl.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-matrix"}); closeErr != nil {
		t.Fatalf("CloseSession: %v; a remote daemon reporting the session already gone is "+
			"this conclusion SUCCEEDING, not failing", closeErr)
	}
	if got := matrixStoredState(t, store); got != SessionStateClosed {
		t.Errorf("state = %q, want closed", got)
	}
}

// TestCloseSession_ARemoteTeardownFailureIsStillAFailure is the converse: only
// NotFound means "already gone". Any other remote fault leaves a process that
// may still be running, and reporting the session closed over it would be the
// false-liveness state inverted.
func TestCloseSession_ARemoteTeardownFailureIsStillAFailure(t *testing.T) {
	backend := &failingCloseBackend{mockAgentBackend: &mockAgentBackend{backendType: "mock"}}
	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, _ := svc.(*agentServiceImpl)
	impl.SetKVStore(kvstore.NewMemory())

	seedSession(t, store, &SessionRecord{SessionID: "s-matrix", TenantID: "t1"})
	impl.sessions.Store("s-matrix", &sessionRecord{Backend: "mock", TenantID: "t1"})

	ctx := ContextWithTenant(context.Background(), "t1")
	if _, closeErr := impl.CloseSession(ctx, &CloseSessionRequest{SessionID: "s-matrix"}); closeErr == nil {
		t.Fatal("a teardown that failed for a reason other than 'already gone' was reported as a close")
	}
	if got := matrixStoredState(t, store); got == SessionStateClosed {
		t.Error("the record was closed over a process that may still be running")
	}
}

// failingCloseBackend reports a remote fault that is NOT a missing session.
type failingCloseBackend struct {
	*mockAgentBackend
}

func (b *failingCloseBackend) Close(context.Context, string) error {
	return &RemoteBackendRPCError{
		Op:  "Close",
		Err: grpcstatus.Error(grpccodes.Unavailable, "daemon is not answering"),
	}
}
