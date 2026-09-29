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
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// A turn the agent cut off is the fleet's fault — go-quicbaas#1064
// =============================================================================
//
// The burn-in node's log, during a deploy that stopped quicbaas-agent:
//
//	remote_acp_backend: rpc Prompt: rpc error: code = Internal desc = acp:
//	prompt failed: session …: container: agent stream closed
//
// followed by "charged as a failure of the work". These tests pin both sides
// of the line that report drew: an agent that stops serving a turn is
// refunded, a worker that crashes under a healthy agent is not.

// agentShutdownStreamClose is what a draining agent answers a turn whose
// worker's stream the shutdown closed: pkg/fleet's mapPromptError, Unavailable.
func agentShutdownStreamClose() error {
	return status.Error(codes.Unavailable, "agent: draining for shutdown: "+
		(&ContainerStreamClosedError{ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt"}).Error())
}

// containerTransportLoss is what a HEALTHY agent answers a turn whose worker's
// container stream closed with no exit status of the worker's own — the
// container was killed or its pipe cut: pkg/fleet's mapPromptError,
// Unavailable, drain or no drain.
func containerTransportLoss() error {
	return status.Error(codes.Unavailable, "agent: the worker's container stream closed without a worker exit: "+
		(&ContainerStreamClosedError{ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt"}).Error())
}

// crashedWorkerStream is a container stream that closed because the worker
// process inside it exited non-zero: the runtime reported the status.
func crashedWorkerStream() *ContainerStreamClosedError {
	return &ContainerStreamClosedError{
		ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt",
		Exit: ContainerExit{Known: true, Code: 1},
	}
}

// workerCrash is what a healthy agent answers a turn whose worker process
// exited non-zero: the ordinary mapping, Internal.
func workerCrash() error {
	return status.Error(codes.Internal, "acp: prompt failed: session sess-1: "+crashedWorkerStream().Error())
}

// promptThroughDaemon runs one Prompt through a real RemoteACPBackend against
// a daemon that answers with promptErr, and returns what the backend reported.
func promptThroughDaemon(t *testing.T, promptErr error) error {
	t.Helper()
	daemon := startBufconnDaemon(t, &fakeDaemonServer{promptErr: promptErr})
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Backend:      "acp",
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	_, got := backend.Prompt(context.Background(), &PromptRequest{SessionID: "sess-1", Prompt: "go"})
	if got == nil {
		t.Fatal("a failed remote turn was reported as a successful prompt")
	}
	return got
}

// concludeFailedJob classifies loopErr the way the job's end does and applies
// the result to a task holding one charged attempt.
func concludeFailedJob(t *testing.T, loopErr error) (*Task, string) {
	t.Helper()
	task := parkedTask()
	if charged := task.ChargedAttempts(); charged != 1 {
		t.Fatalf("the fixture starts at %d charged attempts; this test proves nothing", charged)
	}
	class, fault := classifyJobFailure(loopErr)
	applyConclusion(task, attemptConclusion{
		status: &JobStatus{JobID: "job-1", State: JobStateFailed, Error: loopErr.Error(), FailureClass: class},
		fault:  fault,
	}, time.Now().UTC())
	return task, class
}

// TestAgentShutdownStreamClose_IsRefundedAndRequeued is the regression guard
// for the deploy that charged job 27094ec7: the turn's stream closed because
// the AGENT stopped, and the task must get its attempt back.
func TestAgentShutdownStreamClose_IsRefundedAndRequeued(t *testing.T) {
	t.Parallel()

	promptErr := promptThroughDaemon(t, agentShutdownStreamClose())
	if !errors.Is(promptErr, ErrAgentInterrupted) {
		t.Fatalf("err = %v, want it to carry ErrAgentInterrupted", promptErr)
	}
	var interrupted *AgentInterruptedError
	if !errors.As(promptErr, &interrupted) {
		t.Fatalf("err = %T, want an AgentInterruptedError in the chain", promptErr)
	}
	// AUDITED WITH THE HOST: the job's error names the agent that went away.
	if interrupted.Host != "daemon.test:9091" || interrupted.AgentID != "agent-remote" {
		t.Errorf("interruption names host %q agent %q, want the bound registration", interrupted.Host, interrupted.AgentID)
	}
	if !strings.Contains(promptErr.Error(), "daemon.test:9091") {
		t.Errorf("error %q does not name the host", promptErr.Error())
	}

	task, class := concludeFailedJob(t, promptErr)
	if class != FaultAgentInterrupted {
		t.Fatalf("class = %q, want %q", class, FaultAgentInterrupted)
	}
	if got := task.ChargedAttempts(); got != 0 {
		t.Fatalf("charged attempts = %d, want 0 — an agent restart was charged to the work", got)
	}
	if task.EnvironmentFaults != 1 {
		t.Errorf("environment faults = %d, want the refund recorded", task.EnvironmentFaults)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("status = %s, want pending — the task is requeued, not parked", task.Status)
	}
	if task.ConsecutiveEnvironmentFaults != 1 {
		t.Errorf("consecutive faults = %d, want the requeue counted against the bound", task.ConsecutiveEnvironmentFaults)
	}
}

// TestWorkerCrash_IsStillCharged is the direction that must not regress: the
// same stream-closed text, reported Internal by an agent that is NOT shutting
// down, is a worker that died, and that is the work's.
func TestWorkerCrash_IsStillCharged(t *testing.T) {
	t.Parallel()

	promptErr := promptThroughDaemon(t, workerCrash())
	if errors.Is(promptErr, ErrAgentInterrupted) {
		t.Fatalf("err = %v: a worker crash under a healthy agent was read as the agent's interruption", promptErr)
	}

	task, class := concludeFailedJob(t, promptErr)
	if class != "" {
		t.Fatalf("class = %q, want none — a worker crash is a failure of the work", class)
	}
	if got := task.ChargedAttempts(); got != 1 {
		t.Fatalf("charged attempts = %d, want 1 — the crash must be charged", got)
	}
	if task.EnvironmentFaults != 0 {
		t.Errorf("environment faults = %d, want no refund", task.EnvironmentFaults)
	}
}

// TestContainerTransportLoss_OutsideShutdownIsRefunded: a worker's container
// stream that closed with no worker exit, under an agent that is NOT shutting
// down, is transport loss — refunded and requeued like the shutdown case.
func TestContainerTransportLoss_OutsideShutdownIsRefunded(t *testing.T) {
	t.Parallel()

	promptErr := promptThroughDaemon(t, containerTransportLoss())
	if !errors.Is(promptErr, ErrAgentInterrupted) {
		t.Fatalf("err = %v, want it to carry ErrAgentInterrupted", promptErr)
	}
	task, class := concludeFailedJob(t, promptErr)
	if class != FaultAgentInterrupted {
		t.Fatalf("class = %q, want %q", class, FaultAgentInterrupted)
	}
	if got := task.ChargedAttempts(); got != 0 {
		t.Fatalf("charged attempts = %d, want 0 — a lost container stream was charged to the work", got)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("status = %s, want requeued", task.Status)
	}
}

// TestLocalContainerStream_CrashIsChargedTransportLossIsNot: the same line,
// drawn on a turn whose backend ran in this process and whose error is the
// backend's own. Only the worker's non-zero exit is charged.
func TestLocalContainerStream_CrashIsChargedTransportLossIsNot(t *testing.T) {
	t.Parallel()

	lost := &ContainerStreamClosedError{ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt"}
	task, class := concludeFailedJob(t, lost)
	if class != FaultAgentInterrupted || task.ChargedAttempts() != 0 {
		t.Errorf("transport loss: class %q charged %d, want %q and 0", class, task.ChargedAttempts(), FaultAgentInterrupted)
	}

	runtimeFailed := &ContainerStreamClosedError{ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt",
		Exit: ContainerExit{Known: true, Code: 125}}
	task, class = concludeFailedJob(t, runtimeFailed)
	if class != FaultAgentInterrupted || task.ChargedAttempts() != 0 {
		t.Errorf("runtime failure: class %q charged %d, want %q and 0", class, task.ChargedAttempts(), FaultAgentInterrupted)
	}

	task, class = concludeFailedJob(t, crashedWorkerStream())
	if class != "" || task.ChargedAttempts() != 1 {
		t.Errorf("worker crash: class %q charged %d, want none and 1", class, task.ChargedAttempts())
	}
}

// blockingPromptServer holds every Prompt until the server is stopped, the way
// a live worker's turn is held when its agent goes away.
type blockingPromptServer struct {
	proto.UnimplementedFleetRuntimeServer
	entered chan struct{}
}

func (s *blockingPromptServer) Prompt(_ *proto.PromptSessionRequest, stream grpc.ServerStreamingServer[proto.PromptSessionResponse]) error {
	close(s.entered)
	<-stream.Context().Done()
	return stream.Context().Err()
}

// TestAgentTransportLoss_MidPromptIsAnInterruption: an agent that exits
// outright cannot say it is draining. The transport says it instead, and the
// turn must be classified the same way.
func TestAgentTransportLoss_MidPromptIsAnInterruption(t *testing.T) {
	t.Parallel()

	svc := &blockingPromptServer{entered: make(chan struct{})}
	lis := bufconn.Listen(remoteBackendBufSize)
	server := grpc.NewServer()
	proto.RegisterFleetRuntimeServer(server, svc)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Backend:      "acp",
		Logger:       &agentTestLogger{},
		ConnDialer: func(_ context.Context, target string) (*grpc.ClientConn, error) {
			return grpc.NewClient("passthrough:///"+target,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }))
		},
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	result := make(chan error, 1)
	go func() {
		_, promptErr := backend.Prompt(context.Background(), &PromptRequest{SessionID: "sess-1", Prompt: "go"})
		result <- promptErr
	}()
	<-svc.entered
	server.Stop() // the agent goes away with the turn in flight

	promptErr := <-result
	if !errors.Is(promptErr, ErrAgentInterrupted) {
		t.Fatalf("err = %v, want a lost agent to be read as an interruption", promptErr)
	}
	if class, _ := classifyJobFailure(promptErr); class != FaultAgentInterrupted {
		t.Errorf("class = %q, want %q", class, FaultAgentInterrupted)
	}
}

// TestAgentInterruption_OnlyTheAgentsCodesCount pins the narrow reading: a
// cancellation THIS side made, a deadline and every other code keep the
// pre-existing answer.
func TestAgentInterruption_OnlyTheAgentsCodesCount(t *testing.T) {
	t.Parallel()

	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"unavailable", live, status.Error(codes.Unavailable, "transport is closing"), true},
		{"canceled by the agent", live, status.Error(codes.Canceled, "grpc: the server has been stopped"), true},
		{"canceled by this caller", cancelled, status.Error(codes.Canceled, "context canceled"), false},
		{"internal", live, status.Error(codes.Internal, "worker exited"), false},
		{"deadline", live, status.Error(codes.DeadlineExceeded, "slow"), false},
		{"not a status", live, errors.New("local failure"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := wrapAgentInterruption(tc.ctx, "Prompt", "h:1", "a", tc.err, tc.err)
			if got := errors.Is(wrapped, ErrAgentInterrupted); got != tc.want {
				t.Fatalf("interrupted = %v, want %v", got, tc.want)
			}
			if !errors.Is(wrapped, tc.err) {
				t.Error("the transport error must stay reachable through the wrap")
			}
		})
	}
}

// TestAgentInterrupted_IsNeverCharged: the class requeues and refunds like
// every other requeueing class, and it goes on refunding at the consecutive
// bound. The bound still parks the task — a task whose own turns keep taking
// the agent down reaches a person — but no interruption, however many in a
// row, spends an attempt of the work.
func TestAgentInterrupted_IsNeverCharged(t *testing.T) {
	t.Parallel()

	fault, ok := classifyEnvironmentFault(&AgentInterruptedError{Op: "Prompt", Host: "h:1", Code: codes.Unavailable})
	if !ok || fault.Class != FaultAgentInterrupted {
		t.Fatalf("fault = %+v, want %q", fault, FaultAgentInterrupted)
	}
	if !strings.Contains(fault.Reason, "h:1") || !strings.Contains(fault.Reason, "not charged") {
		t.Errorf("reason %q must name the host and say the attempt was not charged", fault.Reason)
	}
	if !KnownFaultClass(FaultAgentInterrupted) {
		t.Error("the class is not known")
	}
	if _, explained := FaultClassMeaning(FaultAgentInterrupted); !explained {
		t.Error("the class has no meaning for the forge comment to quote")
	}

	// Repeated interruptions, each dispatched and concluded exactly as the
	// engine does it: the dispatch counts the attempt, the conclusion decides
	// the charge.
	// The fixture's one attempt IS the first interrupted job, so every
	// conclusion below must leave the task with nothing charged.
	task := parkedTask()
	const startCharged = 0
	loopErr := &AgentInterruptedError{Op: "Prompt", Host: "h:1", AgentID: "a", Code: codes.Unavailable}
	for i := 1; i <= MaxConsecutiveEnvironmentFaults; i++ {
		if i > 1 {
			task.Status = TaskStatusInProgress
			task.Attempts++
			task.SpawnedJobID = "job-" + strconv.Itoa(i)
		}
		class, f := classifyJobFailure(loopErr)
		reason := applyConclusion(task, attemptConclusion{
			status: &JobStatus{JobID: task.SpawnedJobID, State: JobStateFailed, Error: loopErr.Error(), FailureClass: class},
			fault:  f,
		}, time.Now().UTC())
		if got := task.ChargedAttempts(); got != startCharged {
			t.Fatalf("interruption %d: charged attempts = %d, want %d — an agent interruption spent an attempt of the work",
				i, got, startCharged)
		}
		if i < MaxConsecutiveEnvironmentFaults {
			if task.Status != TaskStatusPending {
				t.Fatalf("interruption %d: status = %s, want requeued", i, task.Status)
			}
			continue
		}
		// THE BOUND: parked for a person, still refunded, and saying so.
		if reason != TaskConcludedFaultBounded || task.Status != TaskStatusBlocked {
			t.Fatalf("interruption %d: conclusion %q status %s, want the bounded park", i, reason, task.Status)
		}
		if task.BlockFaultClass != FaultAgentInterrupted {
			t.Errorf("block class = %q, want %q", task.BlockFaultClass, FaultAgentInterrupted)
		}
		if strings.Contains(task.BlockedReason, "IS charged") || !strings.Contains(task.BlockedReason, "not charged either") {
			t.Errorf("block reason %q misstates the charge", task.BlockedReason)
		}
		if !taskSourceReleasable(task.Status, task.BlockFaultClass) {
			t.Error("the park cannot be readmitted by a re-label")
		}
	}
	if task.EnvironmentFaults != MaxConsecutiveEnvironmentFaults {
		t.Errorf("environment faults = %d, want every interruption refunded", task.EnvironmentFaults)
	}
}

// TestFaultOutcome_OtherRequeueingClassesAreStillChargedAtTheBound: the
// never-charged rule is this class's alone.
func TestFaultOutcome_OtherRequeueingClassesAreStillChargedAtTheBound(t *testing.T) {
	t.Parallel()

	third := faultOutcomeFor(&Task{ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults - 1},
		EnvironmentFault{Class: FaultForgeUnreachable})
	if !third.bounded || third.refund {
		t.Errorf("third forge fault = %+v, want bounded and charged", third)
	}
}

// TestLogAttribution_AnInterruptionNamesItsHost: the log line an operator greps
// says the job was the infrastructure's and carries the host.
func TestLogAttribution_AnInterruptionNamesItsHost(t *testing.T) {
	t.Parallel()

	logger := newRecordingLogger()
	svc := &agentServiceImpl{logger: logger}
	loopErr := &AgentInterruptedError{Op: "Prompt", Host: "daemon.test:9091", AgentID: "agent-remote", Code: codes.Unavailable}
	svc.logAttribution(&JobStatus{JobID: "job-1", State: JobStateFailed, FailureClass: FaultAgentInterrupted}, loopErr, nil)

	lines := logger.all()
	if !strings.Contains(lines, "daemon.test:9091") || !strings.Contains(lines, "not the work") {
		t.Fatalf("log = %q, want the host and the attribution", lines)
	}
	if strings.Contains(lines, "charged as a failure of the work") {
		t.Fatalf("log = %q contradicts the refund", lines)
	}
}
