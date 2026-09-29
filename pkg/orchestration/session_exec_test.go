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
	"reflect"
	"strings"
	"testing"
)

// =============================================================================
// Harness
// =============================================================================

const (
	// execSessionTenant owns every session in this file. The container a
	// command runs in is resolved through a tenant-scoped store read, so the
	// tenant is part of the behaviour rather than decoration.
	execSessionTenant = "tenant-exec"

	// liveContainerSession runs in a container the daemon still holds a live
	// record for; resumedContainerSession's live record is gone and only the
	// durable one names its container, which is the state a restarted daemon
	// finds every session in.
	liveContainerSession    = "sess-live"
	resumedContainerSession = "sess-resumed"

	// hostSession's agent is a host process, so there is nowhere in it for a
	// container command to run.
	hostSession = "sess-host"

	liveContainerID    = "ctr-live-0123456789abcdef"
	resumedContainerID = "ctr-resumed-0123456789ab"
)

// execSessionRig is a service holding the three session shapes ExecSession has
// to tell apart, plus the runtime that records what it was asked to run.
type execSessionRig struct {
	service *agentServiceImpl
	runtime *mockContainerRuntime
	store   AgentStore
	ctx     context.Context
}

// newExecSessionRig builds the service and the sessions. Passing a nil runtime
// builds the service the way a daemon on a host with no container runtime
// builds it: with none wired at all.
func newExecSessionRig(t *testing.T, runtime *mockContainerRuntime) *execSessionRig {
	t.Helper()

	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": &mockAgentBackend{}}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	if runtime != nil {
		impl.SetContainerRuntime(runtime)
	}

	ctx := ContextWithTenant(context.Background(), execSessionTenant)
	rig := &execSessionRig{service: impl, runtime: runtime, store: store, ctx: ctx}

	// Live records, as SpawnSession installs them: one containerized, one a
	// host process.
	impl.sessions.Store(liveContainerSession, &sessionRecord{
		Backend:     "mock",
		TenantID:    execSessionTenant,
		ContainerID: liveContainerID,
	})
	impl.sessions.Store(hostSession, &sessionRecord{
		Backend:  "mock",
		TenantID: execSessionTenant,
	})

	// A durable record with no live twin: the session survived a restart, its
	// agent process did not, and the container id has to come off the record.
	if putErr := store.PutSession(ctx, &SessionRecord{
		SessionID:   resumedContainerSession,
		TenantID:    execSessionTenant,
		Backend:     "mock",
		ContainerID: resumedContainerID,
	}); putErr != nil {
		t.Fatalf("PutSession: %v", putErr)
	}

	return rig
}

// execCalls returns the runtime's recorded executions.
func (r *execSessionRig) execCalls(t *testing.T) []mockExecCall {
	t.Helper()

	r.runtime.mu.Lock()
	defer r.runtime.mu.Unlock()
	return append([]mockExecCall(nil), r.runtime.execCalls...)
}

// assertNothingRan is the assertion every refusal shares: a request the
// service declined must not have reached a container, or the refusal was
// cosmetic.
func (r *execSessionRig) assertNothingRan(t *testing.T) {
	t.Helper()

	if calls := r.execCalls(t); len(calls) != 0 {
		t.Fatalf("a refused request still reached the runtime: %+v", calls)
	}
}

// commitSpec is the shape of command this door exists for: the delivery's
// commit, with the identity the project configured and the message on stdin.
func commitSpec() *ExecSpec {
	return &ExecSpec{
		Cmd:     []string{"git", "commit", "-F", "-"},
		Env:     map[string]string{"GIT_AUTHOR_NAME": "agent-go-quicbaas"},
		WorkDir: "/workspace",
		Stdin:   []byte("subject\n"),
	}
}

// =============================================================================
// ExecSession: resolving the container
// =============================================================================

// TestAgentService_ExecSession_RunsInTheSessionsOwnContainer is the core
// contract: the caller names a SESSION, and the command runs in the container
// THAT session's record names — never one the caller could have guessed, and
// never a different session's.
func TestAgentService_ExecSession_RunsInTheSessionsOwnContainer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		sessionID string
		wantID    string
	}{
		{
			name:      "FromTheLiveRecord",
			sessionID: liveContainerSession,
			wantID:    liveContainerID,
		},
		{
			// The live registry is process-local; after a restart only the
			// durable record remembers the container, and a resumed session
			// whose delivery cannot be published is a session that silently
			// stops finishing work.
			name:      "FromTheDurableRecordAfterARestart",
			sessionID: resumedContainerSession,
			wantID:    resumedContainerID,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := &ExecResult{ExitCode: 0, Stdout: "6f2c1ab\n"}
			rig := newExecSessionRig(t, &mockContainerRuntime{available: true, execResult: want})

			spec := commitSpec()
			got, err := rig.service.ExecSession(rig.ctx, &ExecSessionRequest{
				SessionID: tc.sessionID,
				Spec:      spec,
			})
			if err != nil {
				t.Fatalf("ExecSession() error = %v", err)
			}
			if got != want {
				t.Errorf("ExecSession() result = %+v, want the runtime's own answer %+v", got, want)
			}

			calls := rig.execCalls(t)
			if len(calls) != 1 {
				t.Fatalf("runtime saw %d executions, want exactly 1", len(calls))
			}
			if calls[0].ContainerID != tc.wantID {
				t.Errorf("ran in container %q, want %q", calls[0].ContainerID, tc.wantID)
			}
			// The spec is handed through untouched: a service that rewrote the
			// command, dropped the environment or lost the stdin would produce
			// a commit nobody asked for.
			if !reflect.DeepEqual(calls[0].Spec, spec) {
				t.Errorf("spec = %+v, want %+v", calls[0].Spec, spec)
			}
		})
	}
}

// =============================================================================
// ExecSession: refusals
// =============================================================================

// TestAgentService_ExecSession_RefusesASessionWithNoContainer proves the
// refusal that the whole sandbox depends on. Running the command on the daemon
// instead would put a tenant's git identity and credentials on the host, which
// is the entire thing the container exists to prevent.
func TestAgentService_ExecSession_RefusesASessionWithNoContainer(t *testing.T) {
	t.Parallel()

	t.Run("LiveHostProcess", func(t *testing.T) {
		t.Parallel()

		rig := newExecSessionRig(t, &mockContainerRuntime{available: true})

		result, err := rig.service.ExecSession(rig.ctx, &ExecSessionRequest{
			SessionID: hostSession,
			Spec:      commitSpec(),
		})
		if result != nil {
			t.Errorf("ExecSession() result = %+v, want nil", result)
		}
		var notContainerized *SessionNotContainerizedError
		if !errors.As(err, &notContainerized) {
			t.Fatalf("ExecSession() error = %T (%v), want *SessionNotContainerizedError", err, err)
		}
		if notContainerized.SessionID != hostSession {
			t.Errorf("SessionID = %q, want %q", notContainerized.SessionID, hostSession)
		}
		if !errors.Is(err, ErrSessionNotContainerized) {
			t.Error("expected errors.Is(err, ErrSessionNotContainerized)")
		}
		rig.assertNothingRan(t)
	})

	t.Run("DurableRecordWithNoContainer", func(t *testing.T) {
		t.Parallel()

		rig := newExecSessionRig(t, &mockContainerRuntime{available: true})
		if err := rig.store.PutSession(rig.ctx, &SessionRecord{
			SessionID: "sess-durable-host",
			TenantID:  execSessionTenant,
			Backend:   "mock",
		}); err != nil {
			t.Fatalf("PutSession: %v", err)
		}

		_, err := rig.service.ExecSession(rig.ctx, &ExecSessionRequest{
			SessionID: "sess-durable-host",
			Spec:      commitSpec(),
		})
		var notContainerized *SessionNotContainerizedError
		if !errors.As(err, &notContainerized) {
			t.Fatalf("ExecSession() error = %T (%v), want *SessionNotContainerizedError", err, err)
		}
		rig.assertNothingRan(t)
	})
}

// TestAgentService_ExecSession_UnknownSession proves an unnamed session is a
// failure rather than a no-op. Answering a clean ExecResult here would report
// a commit that never happened.
func TestAgentService_ExecSession_UnknownSession(t *testing.T) {
	t.Parallel()

	rig := newExecSessionRig(t, &mockContainerRuntime{available: true})

	result, err := rig.service.ExecSession(rig.ctx, &ExecSessionRequest{
		SessionID: "sess-never-existed",
		Spec:      commitSpec(),
	})
	if result != nil {
		t.Errorf("ExecSession() result = %+v, want nil", result)
	}
	if err == nil {
		t.Fatal("ExecSession() error = nil for a session that does not exist")
	}
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Fatalf("ExecSession() error = %T (%v), want the store's not-found", err, err)
	}
	rig.assertNothingRan(t)
}

// TestAgentService_ExecSession_TenantIsolation proves the store read that
// resolves the container is scoped: another tenant's session is as unreachable
// as one that does not exist, so no caller can reach into a container it does
// not own by naming its session.
func TestAgentService_ExecSession_TenantIsolation(t *testing.T) {
	t.Parallel()

	rig := newExecSessionRig(t, &mockContainerRuntime{available: true})
	other := ContextWithTenant(context.Background(), "tenant-other")

	_, err := rig.service.ExecSession(other, &ExecSessionRequest{
		SessionID: resumedContainerSession,
		Spec:      commitSpec(),
	})
	if !errors.Is(err, ErrAgentStoreNotFound) {
		t.Fatalf("ExecSession() error = %T (%v), want the store's not-found", err, err)
	}
	rig.assertNothingRan(t)
}

// TestAgentService_ExecSession_RejectsUnrunnableRequests covers the requests
// that name no session or no command. Each has to be refused before the
// container is resolved: an unvalidated spec would otherwise reach the runtime
// and be rejected there, one process launch later.
func TestAgentService_ExecSession_RejectsUnrunnableRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		req         *ExecSessionRequest
		wantInvalid bool
	}{
		{name: "NilRequest", req: nil},
		{name: "BlankSessionID", req: &ExecSessionRequest{Spec: commitSpec()}},
		{
			name:        "NilSpec",
			req:         &ExecSessionRequest{SessionID: liveContainerSession},
			wantInvalid: true,
		},
		{
			name:        "EmptyCmd",
			req:         &ExecSessionRequest{SessionID: liveContainerSession, Spec: &ExecSpec{}},
			wantInvalid: true,
		},
		{
			name: "UnrepresentableEnv",
			req: &ExecSessionRequest{
				SessionID: liveContainerSession,
				Spec:      &ExecSpec{Cmd: []string{"git"}, Env: map[string]string{"T": "a\nSMUGGLED=1"}},
			},
			wantInvalid: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rig := newExecSessionRig(t, &mockContainerRuntime{available: true})

			result, err := rig.service.ExecSession(rig.ctx, tc.req)
			if result != nil {
				t.Errorf("ExecSession() result = %+v, want nil", result)
			}
			if tc.wantInvalid {
				var validErr *InputValidationError
				if !errors.As(err, &validErr) {
					t.Fatalf("ExecSession() error = %T (%v), want *InputValidationError", err, err)
				}
			} else if !errors.Is(err, ErrSessionIDRequired) {
				t.Fatalf("ExecSession() error = %T (%v), want ErrSessionIDRequired", err, err)
			}
			rig.assertNothingRan(t)
		})
	}
}

// TestAgentService_ExecSession_NoContainerRuntime proves the capability is
// absent rather than broken on a daemon with no runtime wired: the caller is
// told this service owns none, not that its session lost its container.
func TestAgentService_ExecSession_NoContainerRuntime(t *testing.T) {
	t.Parallel()

	rig := newExecSessionRig(t, nil)

	result, err := rig.service.ExecSession(rig.ctx, &ExecSessionRequest{
		SessionID: liveContainerSession,
		Spec:      commitSpec(),
	})
	if result != nil {
		t.Errorf("ExecSession() result = %+v, want nil", result)
	}
	var unsupported *SessionExecUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("ExecSession() error = %T (%v), want *SessionExecUnsupportedError", err, err)
	}
	if unsupported.SessionID != liveContainerSession {
		t.Errorf("SessionID = %q, want %q", unsupported.SessionID, liveContainerSession)
	}
	if !errors.Is(err, ErrSessionExecUnsupported) {
		t.Error("expected errors.Is(err, ErrSessionExecUnsupported)")
	}
}

// TestAgentService_ExecSession_RuntimeFailurePropagates proves the runtime's
// own refusal reaches the caller intact. A container that vanished between the
// record being written and the command being run is a real state, and the
// caller has to be able to tell it from a command that ran and failed.
func TestAgentService_ExecSession_RuntimeFailurePropagates(t *testing.T) {
	t.Parallel()

	want := &ContainerNotFoundError{ContainerID: liveContainerID}
	rig := newExecSessionRig(t, &mockContainerRuntime{available: true, execErr: want})

	_, err := rig.service.ExecSession(rig.ctx, &ExecSessionRequest{
		SessionID: liveContainerSession,
		Spec:      commitSpec(),
	})
	var notFound *ContainerNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ExecSession() error = %T (%v), want *ContainerNotFoundError", err, err)
	}
	if notFound.ContainerID != liveContainerID {
		t.Errorf("ContainerID = %q, want %q", notFound.ContainerID, liveContainerID)
	}
}

// =============================================================================
// JobRun.Exec
// =============================================================================

// TestJobRun_Exec_DelegatesToTheEngine proves the run's own verb reaches the
// engine's service: the job holds an AgentService, and the one implementation
// that owns a container runtime is the one that can run the delivery's
// commands.
func TestJobRun_Exec_DelegatesToTheEngine(t *testing.T) {
	t.Parallel()

	want := &ExecResult{ExitCode: 1, Stderr: "nothing to commit"}
	rig := newExecSessionRig(t, &mockContainerRuntime{available: true, execResult: want})

	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: execSessionTenant},
		Agents: rig.service,
		Logger: &agentTestLogger{},
	})

	spec := commitSpec()
	got, err := run.Exec(rig.ctx, &ExecSessionRequest{SessionID: liveContainerSession, Spec: spec})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	// A non-zero status is the command's answer, not an error: a job asking
	// "is there anything to commit" reads it and moves on.
	if got != want {
		t.Errorf("Exec() result = %+v, want %+v", got, want)
	}

	calls := rig.execCalls(t)
	if len(calls) != 1 || calls[0].ContainerID != liveContainerID {
		t.Fatalf("runtime saw %+v, want one execution in %s", calls, liveContainerID)
	}
	if !reflect.DeepEqual(calls[0].Spec, spec) {
		t.Errorf("spec = %+v, want %+v", calls[0].Spec, spec)
	}
}

// TestJobRun_Exec_RefusesAServiceThatCannotExec covers the agent services that
// are NOT the engine's — the remote fleet node client, the SDK's orchestrator
// client, a test's own fake. They carry no container runtime, and the run has
// to say so rather than fail somewhere less legible.
func TestJobRun_Exec_RefusesAServiceThatCannotExec(t *testing.T) {
	t.Parallel()

	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: execSessionTenant},
		Agents: &controlLoopMockService{},
		Logger: &agentTestLogger{},
	})

	result, err := run.Exec(context.Background(), &ExecSessionRequest{
		SessionID: liveContainerSession,
		Spec:      commitSpec(),
	})
	if result != nil {
		t.Errorf("Exec() result = %+v, want nil", result)
	}
	var unsupported *SessionExecUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Exec() error = %T (%v), want *SessionExecUnsupportedError", err, err)
	}
	if unsupported.SessionID != liveContainerSession {
		t.Errorf("SessionID = %q, want %q", unsupported.SessionID, liveContainerSession)
	}
}

// TestJobRun_Exec_RejectsUnnamedSessions proves the run refuses a request that
// names no session before it asks whether its service could run one, so the
// caller gets the same answer whichever service the job holds.
func TestJobRun_Exec_RejectsUnnamedSessions(t *testing.T) {
	t.Parallel()

	rig := newExecSessionRig(t, &mockContainerRuntime{available: true})
	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: execSessionTenant},
		Agents: rig.service,
		Logger: &agentTestLogger{},
	})

	for _, req := range []*ExecSessionRequest{nil, {Spec: commitSpec()}} {
		if _, err := run.Exec(rig.ctx, req); !errors.Is(err, ErrSessionIDRequired) {
			t.Fatalf("Exec(%+v) error = %T (%v), want ErrSessionIDRequired", req, err, err)
		}
	}
	rig.assertNothingRan(t)
}

// TestAgentService_ExecSession_TenantIsolationOnTheLiveRecord is the other
// half of the isolation. The live session map is process-wide, not a
// tenant-scoped read, so a caller naming another tenant's LIVE session would
// otherwise reach straight into its container — the store path being scoped
// buys nothing while this one is not.
func TestAgentService_ExecSession_TenantIsolationOnTheLiveRecord(t *testing.T) {
	t.Parallel()

	rig := newExecSessionRig(t, &mockContainerRuntime{available: true})
	other := ContextWithTenant(context.Background(), "tenant-other")

	_, err := rig.service.ExecSession(other, &ExecSessionRequest{
		SessionID: liveContainerSession,
		Spec:      commitSpec(),
	})
	var notFound *SessionNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ExecSession() error = %T (%v), want *SessionNotFoundError", err, err)
	}
	rig.assertNothingRan(t)
}

// TestSessionExecErrors_SayWhichSessionAndWhy exercises the two refusals'
// message text.
//
// An Error() nothing calls is an Error() nobody has read, and these two reach
// an operator through the job's failure — and, through the SDLC module's
// surfacing path, a comment on a forge issue. Each has to name the session it
// is about and say which of the two conditions it is, because "exec failed"
// alone leaves an operator unable to tell a host-process session from a
// service with no runtime wired.
func TestSessionExecErrors_SayWhichSessionAndWhy(t *testing.T) {
	t.Parallel()

	notContainerized := &SessionNotContainerizedError{SessionID: "sess-host"}
	if !strings.Contains(notContainerized.Error(), "sess-host") {
		t.Errorf("SessionNotContainerizedError does not name the session: %q", notContainerized.Error())
	}
	if !errors.Is(notContainerized, ErrSessionNotContainerized) {
		t.Error("SessionNotContainerizedError does not unwrap to its sentinel")
	}

	unsupported := &SessionExecUnsupportedError{SessionID: "sess-remote"}
	if !strings.Contains(unsupported.Error(), "sess-remote") {
		t.Errorf("SessionExecUnsupportedError does not name the session: %q", unsupported.Error())
	}
	if !errors.Is(unsupported, ErrSessionExecUnsupported) {
		t.Error("SessionExecUnsupportedError does not unwrap to its sentinel")
	}

	// The two must not read alike: one says the session has no container, the
	// other says this service cannot reach one.
	if notContainerized.Error() == unsupported.Error() {
		t.Fatal("the two refusals are indistinguishable to a reader")
	}
}
