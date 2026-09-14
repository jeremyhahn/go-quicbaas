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

package sdlc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Every exit after a successful spawn tears the worker down (go-quicbaas#750)
// =============================================================================
//
// The refusals in this file all happen AFTER JobRun.Spawn returned a live
// session, which is the whole difficulty: a spawn that failed left nothing
// behind, and a run that reached cleanup or escalating tore itself down. What
// leaked was the space between — three `quicbaas-acp-*` containers in one
// minute on the burn-in, each with the daemon session record still beside it,
// because the runner returned its refusal straight out of the state machine.

// daemonAgents is an agent service whose sessions and containers live on a
// REMOTE daemon rather than in this process.
//
// It exists because the local double cannot fail the way #750 did. A close
// that never leaves the control plane looks identical to a close that reaches
// the daemon when both are the same map; here the inventory that must end up
// empty belongs to the daemon, so a runner that forgets to close — or closes
// something other than the session the daemon holds — leaves a container
// behind exactly as the burn-in did.
type daemonAgents struct {
	orchestration.AgentService

	mu sync.Mutex

	// live is what the DAEMON holds: session id -> the container it created.
	live map[string]string

	// closes is every session id the daemon was asked to close, in order.
	closes []string

	seq int

	// container serves the runner's execs, standing in for the daemon's own
	// container runtime.
	container *fakeContainer

	// onSpawn runs once the daemon holds the session, so a test can act in
	// the window between a live worker and a teardown.
	onSpawn func()
}

func newDaemonAgents(container *fakeContainer) *daemonAgents {
	return &daemonAgents{live: map[string]string{}, container: container}
}

func (d *daemonAgents) SpawnSession(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	sessionID := "remote-session-" + itoa(d.seq)
	containerID := "quicbaas-acp-" + itoa(d.seq)
	d.live[sessionID] = containerID
	if d.container != nil {
		d.container.mu.Lock()
		d.container.hostRoot = req.WorkDir
		d.container.mu.Unlock()
	}
	resp := &orchestration.SpawnSessionResponse{
		SessionID:   sessionID,
		Backend:     req.Backend,
		ContainerID: containerID,
	}
	if d.onSpawn != nil {
		d.onSpawn()
	}
	return resp, nil
}

func (d *daemonAgents) PromptSession(_ context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	return &orchestration.PromptSessionResponse{SessionID: req.SessionID}, nil
}

func (d *daemonAgents) ExecSession(_ context.Context, req *orchestration.ExecSessionRequest) (*orchestration.ExecResult, error) {
	d.mu.Lock()
	containerID, ok := d.live[req.SessionID]
	container := d.container
	d.mu.Unlock()
	if !ok {
		return nil, &orchestration.SessionNotFoundError{SessionID: req.SessionID}
	}
	if container == nil {
		return nil, &orchestration.SessionNotContainerizedError{SessionID: req.SessionID}
	}
	return container.exec(containerID, req.Spec)
}

// CloseSession is the daemon reaping the session and the container with it —
// what AgentRuntime.Close does at the other end of the wire.
func (d *daemonAgents) CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	// A close the caller issued on the run's own cancelled context would
	// never reach a real daemon, so it must not be reported as one here.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closes = append(d.closes, req.SessionID)
	delete(d.live, req.SessionID)
	return &orchestration.CloseSessionResponse{SessionID: req.SessionID}, nil
}

// leaked is every container the daemon still holds.
func (d *daemonAgents) leaked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.live))
	for _, containerID := range d.live {
		out = append(out, containerID)
	}
	return out
}

var _ orchestration.SessionExecutor = (*daemonAgents)(nil)

// TestRunner_ARefusalAfterTheSpawnClosesTheWorkerSession is #750's own case: a
// worker the runner refuses for running no container was spawned first, and
// the refusal used to return without closing it.
func TestRunner_ARefusalAfterTheSpawnClosesTheWorkerSession(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.noContainer = true

	err := rig.runner.Run(context.Background(), rig.job(t))

	var refused *WorkerNotContainerizedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want WorkerNotContainerizedError", err)
	}
	if len(rig.agents.spawns) != 1 {
		t.Fatalf("spawns = %d, want the one session the refusal is about", len(rig.agents.spawns))
	}
	if len(rig.agents.closed) != 1 || rig.agents.closed[0] != refused.SessionID {
		t.Fatalf("closed = %v, want the refused session %q closed exactly once",
			rig.agents.closed, refused.SessionID)
	}
}

// TestRunner_ACheckoutFailureClosesTheWorkerSession is the refusal the burn-in
// actually hit: the spawn succeeded, the container was live, and the clone
// inside it exited 128. The runner concluded the job failed and left the
// container running.
func TestRunner_ACheckoutFailureClosesTheWorkerSession(t *testing.T) {
	rig := newRunnerRig(t)
	rig.container.cloneErr = errors.New("sh: ssh: not found")

	err := rig.runner.Run(context.Background(), rig.job(t))

	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("err = %v, want the clone failure to park the attempt", err)
	}
	if len(rig.agents.closed) != 1 {
		t.Fatalf("closed = %v, want the worker session closed once", rig.agents.closed)
	}
}

// TestRunner_ARefusalAfterTheSpawnClosesTheSessionOnTheDaemonThatOwnsIt is the
// same property against a REMOTE worker, which is where the leak was observed.
//
// The container belongs to the daemon, so "no container leaked" is a question
// only the daemon can answer, and the close has to have reached it to change
// the answer. The close crosses the wire today — agentServiceImpl.CloseSession
// pins the backend holding the live record and RemoteACPBackend.Close issues
// AgentRuntime.Close against it — so this is the runner's half of that path:
// the runner must ask.
func TestRunner_ARefusalAfterTheSpawnClosesTheSessionOnTheDaemonThatOwnsIt(t *testing.T) {
	rig := newRunnerRig(t)
	daemon := newDaemonAgents(rig.container)
	rig.agentService = daemon
	rig.container.cloneErr = errors.New("sh: ssh: not found")

	err := rig.runner.Run(context.Background(), rig.job(t))
	if err == nil {
		t.Fatal("the checkout failure was not reported")
	}
	if leaked := daemon.leaked(); len(leaked) != 0 {
		t.Fatalf("the daemon still holds %v; a refused attempt left its container running", leaked)
	}
	if len(daemon.closes) != 1 {
		t.Fatalf("the daemon was asked to close %v, want the one session it spawned", daemon.closes)
	}
}

// TestRunner_ACancelledAttemptStillClosesTheWorkerSession: a cancellation is
// precisely the exit where the container must still go, so the close is issued
// on a context the run's own cancellation cannot reach. The daemon double
// refuses a close carrying a dead context, because a real one would too — so
// this test fails on a teardown that simply passed the run's context along.
func TestRunner_ACancelledAttemptStillClosesTheWorkerSession(t *testing.T) {
	rig := newRunnerRig(t)
	daemon := newDaemonAgents(rig.container)
	rig.agentService = daemon

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancelled once the session EXISTS, which is the window #750 is about:
	// before the spawn there is nothing to leak, and after a teardown there is
	// nothing left to close.
	daemon.onSpawn = cancel

	err := rig.runner.Run(ctx, rig.job(t))
	if err == nil {
		t.Fatal("a cancelled run reported success")
	}
	if leaked := daemon.leaked(); len(leaked) != 0 {
		t.Fatalf("a cancelled attempt left %v running on the daemon", leaked)
	}
	if len(daemon.closes) != 1 {
		t.Fatalf("the daemon was asked to close %v, want the session the cancelled run held", daemon.closes)
	}
}

// TestRunner_ADeliveredAttemptIsTornDownExactlyOnce guards the other side of
// the deferred teardown: a run that reached cleanup already closed its
// session, and the exit path must not close it a second time. A double close
// against a real daemon is a NotFound the runner would log as a failure.
func TestRunner_ADeliveredAttemptIsTornDownExactlyOnce(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.agents.closed) != 1 {
		t.Fatalf("closed = %v, want exactly one close for the one session", rig.agents.closed)
	}
	if rig.signing.stopCount() != 1 {
		t.Fatalf("signing agents stopped = %d, want the attempt's own torn down once", rig.signing.stopCount())
	}
}

// TestRunner_AFailureBeforeTheSpawnClosesNothing is the bad-input twin: the
// exit path must not invent a close for a run that never had a session. A
// close of the empty session id would be a protocol error at the daemon.
func TestRunner_AFailureBeforeTheSpawnClosesNothing(t *testing.T) {
	rig := newRunnerRig(t)
	rig.tokens.err = errors.New("the forge credential is unavailable")

	err := rig.runner.Run(context.Background(), rig.job(t))
	if err == nil {
		t.Fatal("a spawn with no forge token was not refused")
	}
	if len(rig.agents.spawns) != 0 {
		t.Fatalf("spawns = %d, want none", len(rig.agents.spawns))
	}
	if len(rig.agents.closed) != 0 {
		t.Fatalf("closed = %v, want nothing closed for a run that spawned nothing", rig.agents.closed)
	}
}

// =============================================================================
// The withheld line (go-quicbaas#751)
// =============================================================================

// TestCheckoutCommandError_NamesTheCauseRatherThanCountingIt is #751 exactly:
// `git clone … exited 128 (102 bytes of output withheld)` withheld
// `sh: ssh: not found`, which was the entire diagnosis.
func TestCheckoutCommandError_NamesTheCauseRatherThanCountingIt(t *testing.T) {
	err := &CheckoutCommandError{
		TaskID:   "task-1",
		Command:  "clone --origin origin ssh://git@forge/x.git x",
		ExitCode: 128,
		Output:   "Cloning into 'x'...\nsh: ssh: not found\nfatal: could not read from remote repository.",
	}
	got := err.Error()
	if !strings.Contains(got, "sh: ssh: not found") {
		t.Fatalf("the cause is still withheld: %s", got)
	}
	if strings.Contains(got, "bytes of output withheld") {
		t.Fatalf("the error still counts its output instead of saying it: %s", got)
	}
	if !errors.Is(err, ErrRunner) {
		t.Fatal("the error stopped unwrapping to ErrRunner")
	}
}

// TestCheckoutCommandError_RedactsACredentialTheRemoteEchoed is the reason the
// output was withheld in the first place, and the reason it goes through the
// SAME redactSecrets a published reason goes through rather than a second copy
// of that list.
func TestCheckoutCommandError_RedactsACredentialTheRemoteEchoed(t *testing.T) {
	err := &CheckoutCommandError{
		TaskID:   "task-1",
		Command:  "clone",
		ExitCode: 128,
		Output:   "remote: authentication failed for token ghp_0123456789abcdefghijklmnopqrstuvwxyz\nfatal: unable to access",
	}
	got := err.Error()
	if strings.Contains(got, "ghp_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("a token reached the error string: %s", got)
	}
	if !strings.Contains(got, redactedMarker) {
		t.Fatalf("the token was dropped rather than redacted: %s", got)
	}
	if !strings.Contains(got, "fatal: unable to access") {
		t.Fatalf("the diagnosis went with the token: %s", got)
	}
}

// TestPublishCommandError_NamesTheCauseAndRedactsACredential is the same pair
// for the push, which is the command most likely to be handed a credential.
func TestPublishCommandError_NamesTheCauseAndRedactsACredential(t *testing.T) {
	err := &PublishCommandError{
		TaskID:   "task-1",
		Command:  "push origin HEAD",
		ExitCode: 1,
		Output:   "remote: rejected\nAuthorization: Bearer abcdefghijklmnop\n! [remote rejected] main -> main (protected branch hook declined)",
	}
	got := err.Error()
	if strings.Contains(got, "abcdefghijklmnop") {
		t.Fatalf("a bearer token reached the error string: %s", got)
	}
	if !strings.Contains(got, "protected branch hook declined") {
		t.Fatalf("the rejection reason is still withheld: %s", got)
	}
}

// TestDiagnosticTail_KeepsTheEndAndSaysWhatItDropped: a clone states its cause
// last, so the tail is what an operator needs, and a bounded excerpt that did
// not say it was bounded would read as the whole output.
func TestDiagnosticTail_KeepsTheEndAndSaysWhatItDropped(t *testing.T) {
	var b strings.Builder
	for b.Len() <= diagnosticTailLimit*2 {
		b.WriteString("Receiving objects: 41% (1234/3000)\n")
	}
	b.WriteString("fatal: the remote end hung up unexpectedly")

	got := diagnosticTail(b.String())
	if len(got) > diagnosticTailLimit+len("… ") {
		t.Fatalf("the tail is unbounded: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "fatal: the remote end hung up unexpectedly") {
		t.Fatalf("the tail lost the last line: %q", got[max(0, len(got)-120):])
	}
	if !strings.HasPrefix(got, "… ") {
		t.Fatalf("a truncated tail did not say so: %q", got[:40])
	}
}

// TestDiagnosticTail_EmptyOutputRendersNothing is the bad-input case: a command
// that printed nothing must not produce a dangling separator on the error.
func TestDiagnosticTail_EmptyOutputRendersNothing(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t\n"} {
		if got := diagnosticTail(raw); got != "" {
			t.Fatalf("diagnosticTail(%q) = %q, want empty", raw, got)
		}
	}
	err := &CheckoutCommandError{TaskID: "t", Command: "clone", ExitCode: 128}
	if strings.HasSuffix(err.Error(), ": ") {
		t.Fatalf("an error with no output ends in a separator: %q", err.Error())
	}
}

// TestBoundedTail_CutsOnARuneBoundary: the window can open inside a multibyte
// character, and an excerpt that starts with half of one is corrupt.
func TestBoundedTail_CutsOnARuneBoundary(t *testing.T) {
	text := strings.Repeat("é", 64)
	got, truncated := boundedTail(text, 33)
	if !truncated {
		t.Fatal("a 128-byte text bounded to 33 reported no truncation")
	}
	if !strings.HasPrefix(got, "é") {
		t.Fatalf("the excerpt starts mid-rune: %q", got)
	}
}
