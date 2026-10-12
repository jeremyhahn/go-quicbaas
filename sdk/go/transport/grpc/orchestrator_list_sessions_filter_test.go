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

package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// capturingListSessionsServer records the ListSessions request that actually
// arrived on the wire. The request travels to the test over a buffered
// channel rather than a shared field so the handler goroutine and the test
// goroutine are synchronized by the channel itself.
type capturingListSessionsServer struct {
	pb.UnimplementedOrchestratorServiceServer
	got chan *pb.ListSessionsRequest
}

func (s *capturingListSessionsServer) ListSessions(_ context.Context, req *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	s.got <- req
	return &pb.ListSessionsResponse{}, nil
}

// newCapturingListSessionsConn starts an in-memory gRPC server that captures
// the inbound ListSessions proto and returns a live connection to it.
func newCapturingListSessionsConn(t *testing.T) (*grpc.ClientConn, *capturingListSessionsServer) {
	t.Helper()
	fake := &capturingListSessionsServer{got: make(chan *pb.ListSessionsRequest, 1)}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	return srv.dial(t), fake
}

// TestGRPCListSessions_ForwardsStatusFilter proves the SDK gRPC transport puts
// the SDK request's Status on the outbound proto. It used to forward only
// Backend and TenantId, so a caller asking for active sessions received the
// unfiltered list -- closed, error and resumable records mixed back in with
// the live ones (go-quicbaas#792). The Unix transport delegates to this same
// handler, so it inherited the same silent drop.
func TestGRPCListSessions_ForwardsStatusFilter(t *testing.T) {
	conn, fake := newCapturingListSessionsConn(t)
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")

	if _, err := handleOrchestratorListSessions(ctx, conn, &orchestrator.ListSessionsRequest{
		Backend:   "claude",
		Status:    "active",
		ProjectID: "project-1",
		JobID:     "job-1",
		Runner:    "review",
		AgentID:   "agent-1",
	}); err != nil {
		t.Fatalf("handleOrchestratorListSessions: %v", err)
	}

	got := <-fake.got
	if got.GetStatus() != "active" {
		t.Errorf("outbound Status = %q, want %q", got.GetStatus(), "active")
	}
	if got.GetBackend() != "claude" {
		t.Errorf("outbound Backend = %q, want %q", got.GetBackend(), "claude")
	}
	if got.GetTenantId() != "tenant-a" {
		t.Errorf("outbound TenantId = %q, want %q", got.GetTenantId(), "tenant-a")
	}
	if got.GetProjectId() != "project-1" {
		t.Errorf("outbound ProjectId = %q, want %q", got.GetProjectId(), "project-1")
	}
	if got.GetJobId() != "job-1" {
		t.Errorf("outbound JobId = %q, want %q", got.GetJobId(), "job-1")
	}
	if got.GetRunner() != "review" {
		t.Errorf("outbound Runner = %q, want %q", got.GetRunner(), "review")
	}
	if got.GetAgentId() != "agent-1" {
		t.Errorf("outbound AgentId = %q, want %q", got.GetAgentId(), "agent-1")
	}
}

// TestGRPCListSessions_EmptyStatusStaysEmpty holds the other half of the
// contract: an unset filter must not invent one. The server reads an empty
// status as "every state", which is what a caller that predates the filter
// means, so a transport that defaulted it would start hiding records.
func TestGRPCListSessions_EmptyStatusStaysEmpty(t *testing.T) {
	conn, fake := newCapturingListSessionsConn(t)

	if _, err := handleOrchestratorListSessions(context.Background(), conn,
		&orchestrator.ListSessionsRequest{}); err != nil {
		t.Fatalf("handleOrchestratorListSessions: %v", err)
	}

	got := <-fake.got
	if got.GetStatus() != "" {
		t.Errorf("outbound Status = %q, want empty", got.GetStatus())
	}
	if got.GetBackend() != "" {
		t.Errorf("outbound Backend = %q, want empty", got.GetBackend())
	}
}

type capturingListTasksServer struct {
	pb.UnimplementedOrchestratorServiceServer
	got chan *pb.ListTasksRequest
}

func (s *capturingListTasksServer) ListTasks(_ context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	s.got <- req
	return &pb.ListTasksResponse{}, nil
}

func newCapturingListTasksConn(t *testing.T) (*grpc.ClientConn, *capturingListTasksServer) {
	t.Helper()
	fake := &capturingListTasksServer{got: make(chan *pb.ListTasksRequest, 1)}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	return srv.dial(t), fake
}

func TestGRPCListTasks_ForwardsAttemptFilters(t *testing.T) {
	conn, fake := newCapturingListTasksConn(t)
	autoSpawn := false

	if _, err := handleOrchestratorListTasks(context.Background(), conn, &orchestrator.ListTasksRequest{
		TenantID:  "tenant-a",
		ProjectID: "project-1",
		Statuses:  []string{"pending", "blocked"},
		AutoSpawn: &autoSpawn,
		AgentID:   "agent-1",
		JobID:     "job-1",
	}); err != nil {
		t.Fatalf("handleOrchestratorListTasks: %v", err)
	}

	got := <-fake.got
	filter := got.GetFilter()
	if filter == nil {
		t.Fatal("outbound Filter is nil")
	}
	if got.GetTenantId() != "tenant-a" {
		t.Errorf("outbound TenantId = %q, want tenant-a", got.GetTenantId())
	}
	if filter.GetProjectId() != "project-1" {
		t.Errorf("outbound ProjectId = %q, want project-1", filter.GetProjectId())
	}
	if filter.AutoSpawn == nil || filter.GetAutoSpawn() {
		t.Errorf("outbound AutoSpawn = %v, want explicit false", filter.AutoSpawn)
	}
	if filter.GetAgentId() != "agent-1" {
		t.Errorf("outbound AgentId = %q, want agent-1", filter.GetAgentId())
	}
	if filter.GetJobId() != "job-1" {
		t.Errorf("outbound JobId = %q, want job-1", filter.GetJobId())
	}
	if len(filter.GetStatuses()) != 2 {
		t.Fatalf("outbound Statuses = %v, want two statuses", filter.GetStatuses())
	}
}

// TestGRPCListTasks_ForwardsTheSourceFilter proves the source predicate
// (go-quicbaas#730) builds a filter on its own: a listing narrowed only by
// source must not go out unfiltered and list the whole tenant.
func TestGRPCListTasks_ForwardsTheSourceFilter(t *testing.T) {
	conn, fake := newCapturingListTasksConn(t)

	if _, err := handleOrchestratorListTasks(context.Background(), conn, &orchestrator.ListTasksRequest{
		TenantID:   "tenant-a",
		SourceKind: "operator",
		SourceRef:  "owner/repo#12",
	}); err != nil {
		t.Fatalf("handleOrchestratorListTasks: %v", err)
	}

	filter := (<-fake.got).GetFilter()
	if filter == nil {
		t.Fatal("outbound Filter is nil: the source predicate was dropped")
	}
	if filter.GetSourceKind() != "operator" || filter.GetSourceRef() != "owner/repo#12" {
		t.Errorf("outbound source = %q/%q, want operator/owner/repo#12", filter.GetSourceKind(), filter.GetSourceRef())
	}
}

// statefulListSessionsServer answers with one session in a fixed proto state,
// so the client's rendering of that state can be asserted.
type statefulListSessionsServer struct {
	pb.UnimplementedOrchestratorServiceServer
	state pb.SessionState
}

func (s *statefulListSessionsServer) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	return &pb.ListSessionsResponse{Sessions: []*pb.SessionInfo{{
		SessionId: "s1", Backend: "claude", State: s.state,
	}}}, nil
}

func (s *statefulListSessionsServer) SessionStatus(context.Context, *pb.SessionStatusRequest) (*pb.SessionStatusResponse, error) {
	return &pb.SessionStatusResponse{SessionId: "s1", State: s.state}, nil
}

func newStatefulConn(t *testing.T, state pb.SessionState) *grpc.ClientConn {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &statefulListSessionsServer{state: state})
	})
	return srv.dial(t)
}

// TestGRPCListSessions_ReportsTheSDKStateVocabulary is the other half of the
// filter contract. Sending Status "active" is useless if the rows that come
// back say "SESSION_STATE_ACTIVE": the caller cannot compare the value it gets
// to the value it sent, cannot show it beside a REST caller's rows, and cannot
// feed it back into the same filter. The generated enum .String() produced
// exactly that, so the states are mapped explicitly.
func TestGRPCListSessions_ReportsTheSDKStateVocabulary(t *testing.T) {
	cases := []struct {
		name  string
		state pb.SessionState
		want  string
	}{
		{"active", pb.SessionState_SESSION_STATE_ACTIVE, "active"},
		{"idle", pb.SessionState_SESSION_STATE_IDLE, "idle"},
		{"closed", pb.SessionState_SESSION_STATE_CLOSED, "closed"},
		{"error", pb.SessionState_SESSION_STATE_ERROR, "error"},
		{"resumable", pb.SessionState_SESSION_STATE_RESUMABLE, "resumable"},
		// An unset state is the absence of one, which is what every other
		// transport sends. The enum name would be a value no filter accepts.
		{"unspecified is empty", pb.SessionState_SESSION_STATE_UNSPECIFIED, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := newStatefulConn(t, tc.state)

			out, err := handleOrchestratorListSessions(context.Background(), conn,
				&orchestrator.ListSessionsRequest{})
			if err != nil {
				t.Fatalf("handleOrchestratorListSessions: %v", err)
			}
			list, ok := out.(*orchestrator.ListSessionsResponse)
			if !ok {
				t.Fatalf("ListSessions returned %T, want *orchestrator.ListSessionsResponse", out)
			}
			if len(list.Sessions) != 1 {
				t.Fatalf("sessions = %d, want 1", len(list.Sessions))
			}
			if got := list.Sessions[0].Status; got != tc.want {
				t.Errorf("ListSessions Status = %q, want %q", got, tc.want)
			}

			out, err = handleOrchestratorSessionStatus(context.Background(), conn,
				&orchestrator.SessionStatusRequest{SessionID: "s1"})
			if err != nil {
				t.Fatalf("handleOrchestratorSessionStatus: %v", err)
			}
			single, ok := out.(*orchestrator.SessionStatusResponse)
			if !ok {
				t.Fatalf("SessionStatus returned %T, want *orchestrator.SessionStatusResponse", out)
			}
			if single.Status != tc.want {
				t.Errorf("SessionStatus Status = %q, want %q", single.Status, tc.want)
			}
		})
	}
}

// TestGRPCListSessions_StateVocabularyRoundTripsTheFilter is the property
// stated end to end: whatever state comes back must itself be a value the
// filter would accept, or the two halves of the surface disagree.
func TestGRPCListSessions_StateVocabularyRoundTripsTheFilter(t *testing.T) {
	conn := newStatefulConn(t, pb.SessionState_SESSION_STATE_RESUMABLE)

	out, err := handleOrchestratorListSessions(context.Background(), conn,
		&orchestrator.ListSessionsRequest{Status: "resumable"})
	if err != nil {
		t.Fatalf("handleOrchestratorListSessions: %v", err)
	}
	list, ok := out.(*orchestrator.ListSessionsResponse)
	if !ok {
		t.Fatalf("returned %T, want *orchestrator.ListSessionsResponse", out)
	}
	if got := list.Sessions[0].Status; got != "resumable" {
		t.Fatalf("a row returned by the resumable filter reports Status %q; a caller "+
			"cannot match the rows it asked for against the filter it sent", got)
	}
}

// timedListSessionsServer answers with fixed session instants so the client's
// decoding of them can be asserted.
type timedListSessionsServer struct {
	pb.UnimplementedOrchestratorServiceServer
	at time.Time
}

func (s *timedListSessionsServer) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	return &pb.ListSessionsResponse{Sessions: []*pb.SessionInfo{{
		SessionId:    "s1",
		Backend:      "claude",
		State:        pb.SessionState_SESSION_STATE_ACTIVE,
		CreatedAt:    timestamppb.New(s.at),
		LastActivity: timestamppb.New(s.at),
	}}}, nil
}

func (s *timedListSessionsServer) SessionStatus(context.Context, *pb.SessionStatusRequest) (*pb.SessionStatusResponse, error) {
	return &pb.SessionStatusResponse{
		SessionId:    "s1",
		State:        pb.SessionState_SESSION_STATE_ACTIVE,
		LastActivity: timestamppb.New(s.at),
		Timestamp:    timestamppb.New(s.at.Add(time.Hour)),
	}, nil
}

// TestGRPCListSessions_SessionInstantsAreNanoseconds pins the unit.
//
// The server builds SessionInfo's times with unixNanoTimestamp from
// nanosecond-domain values, and every SDK and CLI consumer reads them back
// with time.Unix(0, v). Decoding them with the seconds converter handed back
// ~1.8e9 where ~1.8e18 was meant, so a 2026 session rendered as a moment in
// January 1970 — the same unit error this PR already fixed on the server side.
func TestGRPCListSessions_SessionInstantsAreNanoseconds(t *testing.T) {
	at := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &timedListSessionsServer{at: at})
	})
	conn := srv.dial(t)

	out, err := handleOrchestratorListSessions(context.Background(), conn,
		&orchestrator.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("handleOrchestratorListSessions: %v", err)
	}
	list, ok := out.(*orchestrator.ListSessionsResponse)
	if !ok {
		t.Fatalf("returned %T, want *orchestrator.ListSessionsResponse", out)
	}
	got := list.Sessions[0]
	if got.CreatedAt != at.UnixNano() {
		t.Errorf("CreatedAt = %d, want %d (%s); rendered as nanoseconds that is %s",
			got.CreatedAt, at.UnixNano(), at, time.Unix(0, got.CreatedAt).UTC())
	}
	if got.LastActive != at.UnixNano() {
		t.Errorf("LastActive = %d, want %d; rendered as nanoseconds that is %s",
			got.LastActive, at.UnixNano(), time.Unix(0, got.LastActive).UTC())
	}
}

// TestGRPCSessionStatus_LastActiveIsNanosecondsAndCreatedIsNotInvented covers
// the single-session surface.
//
// SessionStatusResponse carries NO created_at on the wire — only
// last_activity, uptime_seconds and a `timestamp` that says when the status
// was produced. Filling the SDK's CreatedAt from that timestamp reported "the
// session was created just now" on every call, and the CLI prints it verbatim
// as the creation time. An absent value renders as absent; a confident wrong
// one does not.
func TestGRPCSessionStatus_LastActiveIsNanosecondsAndCreatedIsNotInvented(t *testing.T) {
	at := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &timedListSessionsServer{at: at})
	})
	conn := srv.dial(t)

	out, err := handleOrchestratorSessionStatus(context.Background(), conn,
		&orchestrator.SessionStatusRequest{SessionID: "s1"})
	if err != nil {
		t.Fatalf("handleOrchestratorSessionStatus: %v", err)
	}
	got, ok := out.(*orchestrator.SessionStatusResponse)
	if !ok {
		t.Fatalf("returned %T, want *orchestrator.SessionStatusResponse", out)
	}
	if got.LastActive != at.UnixNano() {
		t.Errorf("LastActive = %d, want %d; rendered as nanoseconds that is %s",
			got.LastActive, at.UnixNano(), time.Unix(0, got.LastActive).UTC())
	}
	// The fake deliberately sets `timestamp` an hour after last_activity, so a
	// CreatedAt taken from it would be both non-zero and demonstrably wrong.
	if got.CreatedAt != 0 {
		t.Errorf("CreatedAt = %d, want 0; the wire carries no created_at for a status, "+
			"so any value here is invented", got.CreatedAt)
	}
}

// capturingSpawnServer records the SpawnSessionRequest that reached the wire.
type capturingSpawnServer struct {
	pb.UnimplementedOrchestratorServiceServer
	got chan *pb.SpawnSessionRequest
}

func (s *capturingSpawnServer) SpawnSession(_ context.Context, req *pb.SpawnSessionRequest) (*pb.SpawnSessionResponse, error) {
	s.got <- req
	return &pb.SpawnSessionResponse{SessionId: "s1", Backend: req.GetBackend()}, nil
}

// TestGRPCSpawnSession_PersistentCrossesTheWire is transport parity on the verb
// that decides whether a session is eligible for any of this PR's
// reconciliation at all.
//
// A non-persistent session is never resumable and is CONCLUDED rather than
// restored at boot. The SDK request carried no persistent field and this
// transport never set one, so gRPC and Unix callers spawned one-shot sessions
// while REST and MCP callers got durable ones — the same request, two
// behaviours, and a follow-up prompt on the gRPC side reaching a process that
// was never registered.
func TestGRPCSpawnSession_PersistentCrossesTheWire(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		req  *orchestrator.SpawnSessionRequest
		want bool
	}{
		// Unset is the deployment's default, and that default is durable —
		// the answer REST has always given an omitted field.
		{"unset defaults to persistent", &orchestrator.SpawnSessionRequest{Backend: "claude"}, true},
		{"explicit true", &orchestrator.SpawnSessionRequest{Backend: "claude", Persistent: &yes}, true},
		// Only an explicit false asks for one-shot-per-prompt.
		{"explicit false", &orchestrator.SpawnSessionRequest{Backend: "claude", Persistent: &no}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &capturingSpawnServer{got: make(chan *pb.SpawnSessionRequest, 1)}
			srv := newHandlerServer(t, func(s *grpc.Server) {
				pb.RegisterOrchestratorServiceServer(s, fake)
			})
			if _, err := handleOrchestratorSpawnSession(context.Background(), srv.dial(t), tc.req); err != nil {
				t.Fatalf("handleOrchestratorSpawnSession: %v", err)
			}
			if got := (<-fake.got).GetPersistent(); got != tc.want {
				t.Errorf("outbound Persistent = %v, want %v", got, tc.want)
			}
		})
	}
}

// jobBoundListSessionsServer answers one session a job spawned into a
// container and one an operator opened directly.
type jobBoundListSessionsServer struct {
	pb.UnimplementedOrchestratorServiceServer
}

func (jobBoundListSessionsServer) ListSessions(context.Context, *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	return &pb.ListSessionsResponse{Sessions: []*pb.SessionInfo{
		{SessionId: "s-job", Model: "gpt-test", JobId: "job-1", ContainerId: "ctr-1", Runner: "review", AgentId: "agent-1"},
		{SessionId: "s-direct"},
	}}, nil
}

// TestGRPCListSessions_CarriesTheJobAndTheContainer proves the gRPC (and so
// the Unix) transport keeps what the session page links back to the job with
// and shows as the container (go-quicbaas#860), and leaves both empty for a
// session no job spawned.
func TestGRPCListSessions_CarriesTheJobAndTheContainer(t *testing.T) {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, jobBoundListSessionsServer{})
	})
	conn := srv.dial(t)

	out, err := handleOrchestratorListSessions(context.Background(), conn,
		&orchestrator.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("handleOrchestratorListSessions: %v", err)
	}
	list, ok := out.(*orchestrator.ListSessionsResponse)
	if !ok {
		t.Fatalf("returned %T, want *orchestrator.ListSessionsResponse", out)
	}
	if len(list.Sessions) != 2 {
		t.Fatalf("listed %d sessions, want 2", len(list.Sessions))
	}
	if got := list.Sessions[0]; got.JobID != "job-1" || got.ContainerID != "ctr-1" || got.Model != "gpt-test" || got.Runner != "review" || got.AgentID != "agent-1" {
		t.Errorf("s-job = job %q container %q model %q runner %q agent %q, want job-1/ctr-1/gpt-test/review/agent-1", got.JobID, got.ContainerID, got.Model, got.Runner, got.AgentID)
	}
	if got := list.Sessions[1]; got.JobID != "" || got.ContainerID != "" {
		t.Errorf("s-direct = job %q container %q, want neither", got.JobID, got.ContainerID)
	}
}
