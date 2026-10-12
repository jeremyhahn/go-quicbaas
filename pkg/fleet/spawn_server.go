// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package fleet

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
)

// SpawnServer sentinel errors.
var (
	// ErrSpawnServerNoRuntime is returned by NewGRPCSpawnServer when the
	// runtime dependency is missing.
	ErrSpawnServerNoRuntime = errors.New("agent: spawn server requires a runtime")

	// ErrSpawnServerNoLogger is returned by NewGRPCSpawnServer when the
	// logger dependency is missing.
	ErrSpawnServerNoLogger = errors.New("agent: spawn server requires a logger")

	// ErrSpawnServerNoBindAddr is returned by NewGRPCSpawnServer when the
	// bind address is empty.
	ErrSpawnServerNoBindAddr = errors.New("agent: spawn server requires a bind address")

	// ErrSpawnServerNotStarted is returned by Addr when the server has not
	// yet bound a listener.
	ErrSpawnServerNotStarted = errors.New("agent: spawn server not started")

	// ErrSpawnServerStopped is returned by Bind and Start once Stop has run:
	// a stopped server does not bind again.
	ErrSpawnServerStopped = errors.New("agent: spawn server stopped")
)

// GRPCSpawnServerDeps injects production dependencies into NewGRPCSpawnServer.
// Identity is populated via SetIdentity after enrollment completes; the server
// rejects RPCs with codes.Unauthenticated until then.
type GRPCSpawnServerDeps struct {
	// Runtime is the pkg/fleet Runtime that handles the actual backend calls.
	Runtime *Runtime

	// BindAddr is the TCP address to listen on (e.g., "127.0.0.1:9091" or
	// ":0" for an ephemeral port). Required.
	BindAddr string

	// Logger is used for lifecycle and handler-level diagnostics. Required.
	Logger interfaces.Logger

	// ServerTLSConfig configures mTLS for the daemon-side gRPC server. When
	// nil the server runs plaintext — only safe for tests.
	ServerTLSConfig *tls.Config

	// ListenerOverride, when non-nil, short-circuits the net.Listen call.
	// Exposed for bufconn-backed tests; production callers leave it nil.
	ListenerOverride net.Listener

	// Audit is an optional sink for per-RPC security audit events. When nil
	// the server still serves RPCs but emits no audit trail — acceptable for
	// unit tests, not for production. Any type that implements AuditSink is
	// accepted; *security.AuditLogger satisfies the contract natively.
	Audit AuditSink
}

// GRPCSpawnServer implements the gRPC FleetRuntime service backed by a
// pkg/fleet Runtime. It enforces tenant isolation and scope checks against
// the enrolled AgentIdentity before dispatching to the runtime.
type GRPCSpawnServer struct {
	proto.UnimplementedFleetRuntimeServer

	runtime          *Runtime
	bindAddr         string
	tlsConfig        *tls.Config
	listenerOverride net.Listener
	logger           interfaces.Logger

	audit AuditSink

	mu       sync.Mutex
	listener net.Listener
	grpcSrv  *grpc.Server
	serveErr chan error
	identity *orchestration.AgentIdentity
	stopped  bool

	// draining is set the moment Stop begins. From then on a new spawn is
	// refused and a turn that fails is reported as the agent's interruption
	// rather than the worker's failure (go-quicbaas#1064).
	draining atomic.Bool

	// drainStarted is closed, once, at the same moment draining is set, so a
	// caller can wait for the drain to begin rather than poll for it.
	drainStarted chan struct{}
	drainOnce    sync.Once
}

// Compile-time interface check.
var _ SpawnServer = (*GRPCSpawnServer)(nil)
var _ proto.FleetRuntimeServer = (*GRPCSpawnServer)(nil)

// NewGRPCSpawnServer constructs a daemon-side gRPC server wired to the given
// Runtime. The server does not bind until Start is called.
func NewGRPCSpawnServer(deps GRPCSpawnServerDeps) (*GRPCSpawnServer, error) {
	if deps.Runtime == nil {
		return nil, ErrSpawnServerNoRuntime
	}
	if deps.Logger == nil {
		return nil, ErrSpawnServerNoLogger
	}
	if deps.ListenerOverride == nil && deps.BindAddr == "" {
		return nil, ErrSpawnServerNoBindAddr
	}
	return &GRPCSpawnServer{
		runtime:          deps.Runtime,
		bindAddr:         deps.BindAddr,
		tlsConfig:        deps.ServerTLSConfig,
		listenerOverride: deps.ListenerOverride,
		logger:           deps.Logger,
		audit:            deps.Audit,
		drainStarted:     make(chan struct{}),
	}, nil
}

// SetIdentity installs the enrolled AgentIdentity. Must be called after
// enrollment completes and before the first RPC is accepted. Safe for
// concurrent callers.
func (s *GRPCSpawnServer) SetIdentity(id *orchestration.AgentIdentity) {
	s.mu.Lock()
	s.identity = id
	s.mu.Unlock()
}

// Identity returns the currently-installed AgentIdentity, or nil before
// enrollment. Exposed for diagnostics.
func (s *GRPCSpawnServer) Identity() *orchestration.AgentIdentity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identity
}

// Bind binds the listener without serving it, so the control plane's probe of
// this node's address at enrollment reaches a listening socket; Start then
// serves the same listener. Idempotent, and a no-op once bound.
func (s *GRPCSpawnServer) Bind(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bindLocked(ctx)
}

// bindLocked binds the listener unless one is already bound. Caller holds mu.
func (s *GRPCSpawnServer) bindLocked(ctx context.Context) error {
	if s.stopped {
		return ErrSpawnServerStopped
	}
	if s.listener != nil {
		return nil
	}
	listener := s.listenerOverride
	if listener == nil {
		lc := net.ListenConfig{}
		l, err := lc.Listen(ctx, "tcp", s.bindAddr)
		if err != nil {
			return &SpawnServerBindError{Addr: s.bindAddr, Err: err}
		}
		listener = l
	}
	s.listener = listener
	return nil
}

// Start binds the listener, registers the FleetRuntime service, and serves
// in a goroutine. Returns once the listener is bound (a serve error after
// Start is exposed via a close-on-error channel observable by Stop).
func (s *GRPCSpawnServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.grpcSrv != nil {
		return nil // already started; Start is idempotent by design
	}

	if err := s.bindLocked(ctx); err != nil {
		return err
	}
	listener := s.listener

	var opts []grpc.ServerOption
	if s.tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(s.tlsConfig)))
	} else {
		// Explicit plaintext for test harnesses. Production daemons always
		// configure TLS via the DaemonDeps wiring.
		opts = append(opts, grpc.Creds(insecure.NewCredentials()))
	}
	// The same bound the daemon dials with (orchestration.RemoteACPMaxMessageBytes):
	// a worker's reply is text plus quoted tool output and has measured 16.9 MB.
	opts = append(opts,
		grpc.MaxRecvMsgSize(orchestration.RemoteACPMaxMessageBytes),
		grpc.MaxSendMsgSize(orchestration.RemoteACPMaxMessageBytes),
	)

	s.grpcSrv = grpc.NewServer(opts...)
	proto.RegisterFleetRuntimeServer(s.grpcSrv, s)

	s.serveErr = make(chan error, 1)
	go func() {
		err := s.grpcSrv.Serve(listener)
		s.serveErr <- err
		close(s.serveErr)
	}()

	s.logger.Infof("agent spawn server listening at %s", listener.Addr().String())
	return nil
}

// Stop performs a graceful shutdown bounded by the context deadline, then
// forcefully terminates if the grace period elapses. Idempotent.
//
// IT DRAINS BEFORE IT CLOSES ANYTHING, mirroring the control plane's own
// drain. New spawns are refused from the first moment; the prompts already in
// flight are left to finish until ctx's deadline, and only then are their
// streams cut. A turn cut off that way — or one whose worker was taken down
// by the same shutdown — is reported Unavailable, which the control plane
// refunds as the fleet's fault rather than charging to the work.
func (s *GRPCSpawnServer) Stop(ctx context.Context) error {
	s.draining.Store(true)
	s.drainOnce.Do(func() { close(s.drainStarted) })
	s.mu.Lock()
	if s.stopped || s.grpcSrv == nil {
		// A listener bound ahead of an enrollment that never completed has
		// no server to close it.
		var closeErr error
		if !s.stopped && s.listener != nil {
			if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				closeErr = &SpawnServerServeError{Err: err}
			}
		}
		s.stopped = true
		s.mu.Unlock()
		return closeErr
	}
	srv := s.grpcSrv
	serveErr := s.serveErr
	s.stopped = true
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		srv.Stop()
		<-done
	}

	// Drain the serve goroutine's exit status. grpc.Server returns nil from
	// Serve on a graceful stop; anything else gets surfaced to the caller.
	select {
	case err := <-serveErr:
		if err != nil && !isGracefulServerClosed(err) {
			return &SpawnServerServeError{Err: err}
		}
	case <-time.After(100 * time.Millisecond):
		// Serve goroutine already consumed; fall through.
	}
	return nil
}

// Draining reports whether Stop has begun. Exposed for diagnostics.
func (s *GRPCSpawnServer) Draining() bool {
	return s.draining.Load()
}

// DrainStarted returns a channel closed the moment Stop begins draining.
func (s *GRPCSpawnServer) DrainStarted() <-chan struct{} {
	return s.drainStarted
}

// Addr returns the concretely bound address (handles ":0" ephemeral ports).
// Returns empty string before Start.
func (s *GRPCSpawnServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// -----------------------------------------------------------------------------
// RPC handlers
// -----------------------------------------------------------------------------

// Enroll is currently served by the control plane, not the daemon:
// daemons call it, they do not implement it. Returning Unimplemented here
// prevents misconfiguration where a daemon accidentally advertises itself
// as the enrollment authority.
func (s *GRPCSpawnServer) Enroll(ctx context.Context, req *proto.EnrollRequest) (*proto.EnrollResponse, error) {
	begin := time.Now()
	err := status.Error(codes.Unimplemented, "daemons do not issue enrollments")
	s.auditRPC(ctx, "enroll", "", "", "", err)
	observeRPC("enroll", err, begin)
	return nil, err
}

// Spawn dispatches to Runtime.Spawn after authenticating the caller and
// validating the required scope.
func (s *GRPCSpawnServer) Spawn(ctx context.Context, req *proto.SpawnSessionRequest) (*proto.SpawnSessionResponse, error) {
	begin := time.Now()
	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimeSpawn)
	if authErr != nil {
		s.auditRPC(ctx, "spawn", req.GetTenantId(), "", "", authErr)
		observeRPC("spawn", authErr, begin)
		return nil, authErr
	}

	if req.GetBackend() == "" {
		err := status.Error(codes.InvalidArgument, "backend is required")
		s.auditRPC(ctx, "spawn", identity.TenantID, identity.AgentID, "", err)
		observeRPC("spawn", err, begin)
		return nil, err
	}
	// A DRAINING AGENT STARTS NOTHING NEW. The session would be cut off by the
	// very shutdown that is under way. Unavailable is what the control plane
	// reads as an agent that did not take the spawn, which is refunded.
	if s.draining.Load() {
		err := status.Error(codes.Unavailable, errNodeDraining.Error())
		s.auditRPC(ctx, "spawn", identity.TenantID, identity.AgentID, "", err)
		observeRPC("spawn", err, begin)
		return nil, err
	}

	spawnReq := &orchestration.SpawnRequest{
		ProjectID:      req.GetProjectId(),
		WorkDir:        req.GetWorkDir(),
		Model:          req.GetModel(),
		SystemPrompt:   req.GetSystemPrompt(),
		Env:            req.GetEnv(),
		MCPServers:     protoMCPServersToRuntime(req.GetMcpServers()),
		SandboxMode:    orchestration.SandboxMode(req.GetSandboxMode()),
		Sandboxed:      req.GetSandboxed(),
		ContainerImage: req.GetContainerImage(),
		Persistent:     req.GetPersistent(),
		// The control plane's own argv for the agent program — today the
		// permission grant an SDLC worker is spawned with (go-quicbaas#218).
		// Honoured verbatim: this daemon runs the process, and a grant it
		// dropped would be a grant nobody could observe was missing.
		ExtraArgs: req.GetExtraArgs(),

		// THE POOLED ACCOUNT SELECTION, AND IT MUST BE FORWARDED FOR THE SAME
		// REASON THE ARGV IS (go-quicbaas#235). The caller states which
		// credential the agent runs on; this daemon is the only party that can
		// resolve it, because the credential lives in this daemon's own secret
		// store and never crosses the wire. Dropping the request here — which
		// is what happened until #235 — does not fail: the backend's
		// resolveAccount is only consulted when AccountID or AccountAuto is
		// set, so the process starts with whatever ambient identity the daemon
		// happens to have, and nothing anywhere says the caller's choice was
		// discarded. That is the exact silent shape #218 and #235 were both
		// measured as, with the local path fixed and the distributed one not.
		AccountID:       req.GetAccountId(),
		AccountAuto:     req.GetAccountAuto(),
		AccountStrategy: req.GetAccountStrategy(),
		AccountAllow:    req.GetAccountAllow(),
		// The exclusion travels with the allow-list, and honouring only one of
		// them would put a rotating session back on the account it was rotated
		// off. See the client-side note in remote_acp_backend.go.
		AccountExclude: req.GetAccountExclude(),
		// The job the spawn works for. It is not a dispatch input on this side
		// either: it scopes the credential delivery grant this daemon's account
		// resolution reports, so a remote session's grant records the job it
		// served.
		JobID: req.GetJobId(),

		// THE CONTAINER AND TRUST DIMENSION (#248). This daemon is the party
		// that creates the container, so it is the only one that can apply any
		// of these — and it was already built to: ACPBackend reads all four
		// trust fields when it assembles the container spec, and
		// Runtime.authorizeMounts re-authorizes every extra mount against THIS
		// host's MountPolicy before a path is bound. Until the wire carried
		// them, that authorization ran on a slice that was always empty and the
		// resolved trust tier never arrived, so a remote session ran untrusted
		// no matter what the control plane's policy had decided.
		ExtraMounts:  protoMountsToRuntime(req.GetExtraMounts()),
		ImageDigest:  req.GetImageDigest(),
		TrustLevel:   orchestration.TrustLevel(req.GetTrustLevel()),
		TrustLabel:   req.GetTrustLabel(),
		ExpectedPCRs: req.GetExpectedPcrs(),

		// THE STORAGE TIER THE CONTROL PLANE RESOLVED FROM THE PROJECT RECORD,
		// which this daemon does not hold. It is applied where every other
		// sandbox decision is applied — ACPBackend.sandboxSpec, through
		// ApplyWorkspaceStorage — and an under-provisioned memory budget is
		// refused there rather than becoming an OOM kill mid-attempt.
		Storage: protoStorageToRuntime(req.GetStorage()),

		// THE WORKSPACE BY NAME (go-quicbaas#981). This daemon provisions it
		// under its own projects root — Runtime.provisionWorkspace — and the
		// response says where.
		Workspace: protoWorkspaceToRuntime(req.GetWorkspace()),

		// THE SIGNING RELAY (go-quicbaas#1013): this daemon cannot mount the
		// control plane's signing socket, so it binds one of its own that
		// forwards over the SigningRelay stream the control plane opens next.
		SigningRelay: protoSigningRelayToRuntime(req.GetSigningRelay()),
	}

	session, err := s.runtime.Spawn(ctx, identity.TenantID, nil, req.GetBackend(), spawnReq)
	if err != nil {
		// A CONFIGURATION FAULT TRAVELS AS SUCH (#235). The pooled account is
		// resolved before the process is started, so "no account for this
		// tenant" means nothing was created and the SDLC module must park the item
		// rather than charge it. That decision is made from the resolver's
		// TYPED error, which does not survive a gRPC status — so the class is
		// attached here and mapped back to the same sentinel on the other
		// side. A failure that is not one of those classes is untouched.
		mapped := orchestration.AttachSpawnFaultEvidence(mapRuntimeError(err), err)
		s.auditRPC(ctx, "spawn", identity.TenantID, identity.AgentID, "", mapped)
		observeRPC("spawn", mapped, begin)
		return nil, mapped
	}

	s.auditRPC(ctx, "spawn", identity.TenantID, identity.AgentID, session.ID, nil)
	observeRPC("spawn", nil, begin)
	return &proto.SpawnSessionResponse{
		SessionId: session.ID,
		Backend:   req.GetBackend(),
		Model:     session.Model,
		Timestamp: timestamppb.New(time.Unix(0, session.CreatedAt)),

		// THE CONTAINER THIS DAEMON PUT THE AGENT IN (#740). The container is
		// created here — by this daemon's runtime, from this daemon's image map
		// — so this response is the only place its identity can come from. The
		// control plane records it on the session and refuses to publish a
		// delivery from a session that has none (pkg/sdlc/runner.go: the commit,
		// the signature and the push all run INSIDE the worker's container),
		// which is what a spawn that reported no container was measured as: a
		// remote worker refused for "runs no container" while its container was
		// running on this host.
		//
		// It is the id as THIS host's container runtime knows it, and it means
		// nothing to a runtime that is not this one. A caller that acts on it
		// must be acting through this daemon — see the note on Session
		// ContainerID in RemoteACPBackend.Spawn.
		//
		// Empty is a real answer, not a missing one: a session the daemon runs
		// as a host process has no container, and inventing an id for it would
		// send a tenant's work into a sandbox nobody chose.
		ContainerId: session.ContainerID,

		// WHICH ACCOUNT THIS SESSION IS BILLING TO. Resolution happened here,
		// against a pool this daemon owns; the control plane cannot recompute
		// it and has no other way to learn it. Reporting it is what lets the
		// durable session record — and every turn's usage row — name an account
		// for a session that ran remotely. The credential itself does not
		// travel and never has; only the identifier and the strategy that chose
		// it do.
		AccountId:       session.AccountID,
		AccountStrategy: session.AccountStrategy,

		// AND WHICH COPY OF THAT ACCOUNT'S CREDENTIAL IT MATERIALIZED. The
		// generation was read out of the envelope this daemon resolved, before
		// the agent process started; the control plane compares it against the
		// account's current generation before every prompt and rolls a session
		// holding a superseded copy rather than spending a turn discovering it
		// (go-quicbaas#997). Without it a remote session falls back to a
		// timestamp comparison that cannot see a rotation landing mid-spawn.
		AccountGeneration: session.AccountGeneration,

		// AND THE GRANT THE DELIVERY WAS MADE UNDER. The control plane issued
		// it when it released the credential to this daemon, but only this side
		// knows which SESSION that release served, so this response is the only
		// place the pairing can come from (account_grant.go). The control plane
		// records it and its session conclusion withdraws it — which is what
		// makes a remote session's provider credential revocable at all.
		CredentialLeaseId: session.CredentialLeaseID,

		// AND THE SESSION-SLOT RESERVATION it was resolved under, which the
		// control plane's record must name to satisfy.
		ReservationId: session.ReservationID,

		// WHERE THIS DAEMON PROVISIONED THE NAMED WORKSPACE, which only it
		// can say: the root is this host's. The control plane records it on
		// the session and pins the job's later sessions to it. Empty when the
		// spawn named none.
		WorkDir: spawnedWorkDir(spawnReq, session),
	}, nil
}

// ExecSession runs one command in the container of a session this daemon
// spawned, after authenticating the caller for the session's tenant under the
// spawn scope — the authority that created the container is the authority
// that may act in it. See Runtime.ExecSession for the scoping.
func (s *GRPCSpawnServer) ExecSession(ctx context.Context, req *proto.ExecSessionRequest) (*proto.ExecSessionResponse, error) {
	begin := time.Now()
	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimeSpawn)
	if authErr != nil {
		s.auditRPC(ctx, "exec", req.GetTenantId(), "", req.GetSessionId(), authErr)
		observeRPC("exec", authErr, begin)
		return nil, authErr
	}
	result, truncated, err := s.runtime.ExecSession(ctx, identity.TenantID, req.GetSessionId(), &orchestration.ExecSpec{
		Cmd:     req.GetCmd(),
		Env:     req.GetEnv(),
		WorkDir: req.GetWorkDir(),
		Stdin:   req.GetStdin(),
	})
	if err != nil {
		mapped := mapRuntimeError(err)
		s.auditRPC(ctx, "exec", identity.TenantID, identity.AgentID, req.GetSessionId(), mapped)
		observeRPC("exec", mapped, begin)
		return nil, mapped
	}
	s.auditRPC(ctx, "exec", identity.TenantID, identity.AgentID, req.GetSessionId(), nil)
	observeRPC("exec", nil, begin)
	return &proto.ExecSessionResponse{
		ExitCode:  int32(result.ExitCode), //nolint:gosec // G115: an exit status fits in 32 bits
		Stdout:    []byte(result.Stdout),
		Stderr:    []byte(result.Stderr),
		Truncated: truncated,
	}, nil
}

// RemoveWorkspace removes a session workspace this daemon provisioned, after
// authenticating the caller for the workspace's tenant under the spawn scope —
// the authority that had it made is the authority that has it removed. See
// Runtime.RemoveWorkspace for the scoping.
func (s *GRPCSpawnServer) RemoveWorkspace(ctx context.Context, req *proto.RemoveWorkspaceRequest) (*proto.RemoveWorkspaceResponse, error) {
	begin := time.Now()
	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimeSpawn)
	if authErr != nil {
		s.auditRPC(ctx, "remove_workspace", req.GetTenantId(), "", "", authErr)
		observeRPC("remove_workspace", authErr, begin)
		return nil, authErr
	}
	removed, err := s.runtime.RemoveWorkspace(identity.TenantID, &orchestration.SessionWorkspace{
		TenantID:  req.GetTenantId(),
		ProjectID: req.GetProjectId(),
		Key:       req.GetKey(),
	})
	if err != nil {
		mapped := mapRuntimeError(err)
		s.auditRPC(ctx, "remove_workspace", identity.TenantID, identity.AgentID, "", mapped)
		observeRPC("remove_workspace", mapped, begin)
		return nil, mapped
	}
	s.auditRPC(ctx, "remove_workspace", identity.TenantID, identity.AgentID, "", nil)
	observeRPC("remove_workspace", nil, begin)
	return &proto.RemoveWorkspaceResponse{Removed: removed}, nil
}

// protoSigningRelayToRuntime maps the wire's relay request; nil stays nil.
func protoSigningRelayToRuntime(in *proto.SigningRelaySpec) *orchestration.SigningRelaySpec {
	if in == nil {
		return nil
	}
	return &orchestration.SigningRelaySpec{PublicKeys: in.GetPublicKeys()}
}

// SigningRelay serves the control plane's relay for one session this daemon
// spawned (go-quicbaas#1013). The first frame must name the session; the
// caller is authorized for its tenant under the spawn scope — the authority
// that created the session is the authority that signs for it — and the runtime
// refuses an unknown session, another tenant's, one with no relay, and a
// second attach. Result frames are delivered to the signs waiting for them
// until the stream ends; a stream that ends loses the relay for good.
func (s *GRPCSpawnServer) SigningRelay(stream grpc.BidiStreamingServer[proto.SigningRelayControlFrame, proto.SigningRelayNodeFrame]) error {
	begin := time.Now()
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		observeRPC("signing_relay", err, begin)
		return err
	}
	open := first.GetOpen()
	if open == nil || open.GetSessionId() == "" {
		err := status.Error(codes.InvalidArgument, "the first signing relay frame must open a session")
		s.auditRPC(ctx, "signing_relay", "", "", "", err)
		observeRPC("signing_relay", err, begin)
		return err
	}
	identity, authErr := s.authorize(open.GetTenantId(), orchestration.AgentScopeRuntimeSpawn)
	if authErr != nil {
		s.auditRPC(ctx, "signing_relay", open.GetTenantId(), "", open.GetSessionId(), authErr)
		observeRPC("signing_relay", authErr, begin)
		return authErr
	}
	relay, attachErr := s.runtime.AttachSigningRelay(identity.TenantID, open.GetSessionId(), stream)
	if attachErr != nil {
		mapped := mapRuntimeError(attachErr)
		s.auditRPC(ctx, "signing_relay", identity.TenantID, identity.AgentID, open.GetSessionId(), mapped)
		observeRPC("signing_relay", mapped, begin)
		return mapped
	}
	s.auditRPC(ctx, "signing_relay", identity.TenantID, identity.AgentID, open.GetSessionId(), nil)

	frames := make(chan *proto.SigningRelayControlFrame)
	ended := make(chan error, 1)
	go func() {
		for {
			frame, recvErr := stream.Recv()
			if recvErr != nil {
				ended <- recvErr
				return
			}
			select {
			case frames <- frame:
			case <-ctx.Done():
				ended <- ctx.Err()
				return
			}
		}
	}()
	for {
		select {
		case frame := <-frames:
			if result := frame.GetResult(); result != nil {
				relay.deliver(result)
			}
		case endErr := <-ended:
			// THE LINK IS GONE FOR GOOD: the relay does not wait for another.
			relay.lose(endErr)
			observeRPC("signing_relay", nil, begin)
			return nil
		case <-relay.done():
			// The session was closed on this side; the stream goes with it.
			observeRPC("signing_relay", nil, begin)
			return nil
		}
	}
}

// spawnedWorkDir is the work directory a spawn response reports: the
// provisioned workspace for a spawn that named one, nothing otherwise — a
// work_dir the control plane sent is its own path and needs no echo.
func spawnedWorkDir(req *orchestration.SpawnRequest, session *orchestration.Session) string {
	if req.Workspace == nil || session == nil {
		return ""
	}
	return session.WorkDir
}

// protoWorkspaceToRuntime is the runtime form of a named workspace, nil for a
// spawn that named none. A pinned directory becomes a pin on this very key.
func protoWorkspaceToRuntime(in *proto.SessionWorkspace) *orchestration.SessionWorkspace {
	if in == nil {
		return nil
	}
	ws := &orchestration.SessionWorkspace{
		TenantID:  in.GetTenantId(),
		ProjectID: in.GetProjectId(),
		Key:       in.GetKey(),
	}
	if dir := in.GetPinnedDir(); dir != "" {
		ws.Pin = &orchestration.WorkspacePlacement{Key: ws.Key, Dir: dir}
	}
	if files := in.GetSpecFiles(); len(files) > 0 {
		ws.Spec = &orchestration.WorkspaceSpecFiles{Files: make(map[string][]byte, len(files))}
		for name, content := range files {
			ws.Spec.Files[name] = content
		}
	}
	return ws
}

// Prompt dispatches to Runtime.Prompt and streams a single response. The
// streaming contract exists so Phase 7 realtime-UI work can replace the
// single-response path with per-block streaming without a proto break.
func (s *GRPCSpawnServer) Prompt(req *proto.PromptSessionRequest, stream grpc.ServerStreamingServer[proto.PromptSessionResponse]) error {
	begin := time.Now()
	ctx := stream.Context()
	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimePrompt)
	if authErr != nil {
		s.auditRPC(ctx, "prompt", req.GetTenantId(), "", req.GetSessionId(), authErr)
		observeRPC("prompt", authErr, begin)
		return authErr
	}

	if req.GetPrompt() == "" {
		err := status.Error(codes.InvalidArgument, "prompt is required")
		s.auditRPC(ctx, "prompt", identity.TenantID, identity.AgentID, req.GetSessionId(), err)
		observeRPC("prompt", err, begin)
		return err
	}
	if req.GetBackend() == "" {
		err := status.Error(codes.InvalidArgument, "backend is required")
		s.auditRPC(ctx, "prompt", identity.TenantID, identity.AgentID, req.GetSessionId(), err)
		observeRPC("prompt", err, begin)
		return err
	}

	promptReq := &orchestration.PromptRequest{
		SessionID:    req.GetSessionId(),
		ProjectID:    req.GetProjectId(),
		Prompt:       req.GetPrompt(),
		Model:        req.GetModel(),
		SystemPrompt: req.GetSystemPrompt(),
	}

	resp, err := s.runtime.Prompt(ctx, req.GetBackend(), promptReq)
	if err != nil {
		// THE CLI'S OWN ACCOUNT OF THE FAILURE TRAVELS WITH IT (#235). This
		// daemon ran the process and is the only party that saw its result
		// object; mapping the failure to a bare status here is what left a
		// remote worker burning an attempt on a pre-model failure that a local
		// one has refunded since #235. Attached as a status detail, best
		// effort — a detail that cannot be carried leaves the error untouched.
		// See orchestration/turn_failure_wire.go for what is deliberately NOT
		// put on the wire.
		mapped := orchestration.AttachTurnFailureEvidence(s.mapPromptError(err), err)
		s.auditRPC(ctx, "prompt", identity.TenantID, identity.AgentID, req.GetSessionId(), mapped)
		observeRPC("prompt", mapped, begin)
		return mapped
	}

	// THE TURN'S COST TRAVELS WITH ITS ANSWER. This daemon has no ledger; the
	// control plane charges the turn from this figure, and without it every
	// remote turn was recorded as costing nothing (go-quicbaas#1046). A figure
	// that cannot be encoded is reported and left off, never allowed to fail a
	// turn whose answer is already in hand.
	usageJSON, usageErr := orchestration.PromptUsageToWire(resp.Usage)
	if usageErr != nil {
		s.logger.Warnf("agent: turn usage for session %s could not be encoded; the turn will not be charged: %s",
			req.GetSessionId(), usageErr.Error())
	}
	if sendErr := stream.Send(&proto.PromptSessionResponse{
		SessionId:  resp.SessionID,
		Content:    runtimeContentBlocksToProto(resp.Content),
		Model:      resp.Model,
		StopReason: resp.StopReason,
		Timestamp:  timestamppb.Now(),
		UsageJson:  usageJSON,
	}); sendErr != nil {
		s.auditRPC(ctx, "prompt", identity.TenantID, identity.AgentID, req.GetSessionId(), sendErr)
		observeRPC("prompt", sendErr, begin)
		return sendErr
	}
	s.auditRPC(ctx, "prompt", identity.TenantID, identity.AgentID, req.GetSessionId(), nil)
	observeRPC("prompt", nil, begin)
	return nil
}

// Status dispatches to Runtime.Status.
func (s *GRPCSpawnServer) Status(ctx context.Context, req *proto.SessionStatusRequest) (*proto.SessionStatusResponse, error) {
	begin := time.Now()
	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimeStatus)
	if authErr != nil {
		s.auditRPC(ctx, "status", req.GetTenantId(), "", req.GetSessionId(), authErr)
		observeRPC("status", authErr, begin)
		return nil, authErr
	}

	if req.GetSessionId() == "" {
		err := status.Error(codes.InvalidArgument, "session_id is required")
		s.auditRPC(ctx, "status", identity.TenantID, identity.AgentID, "", err)
		observeRPC("status", err, begin)
		return nil, err
	}

	var (
		last    *orchestration.SessionStatus
		lastErr error
	)
	for _, backendName := range s.runtime.Backends() {
		st, err := s.runtime.Status(ctx, backendName, req.GetSessionId())
		if err == nil && st != nil {
			last = st
			break
		}
		lastErr = err
	}
	if last == nil {
		var err error
		if lastErr != nil {
			err = mapRuntimeError(lastErr)
		} else {
			err = status.Errorf(codes.NotFound, "session %q not found", req.GetSessionId())
		}
		s.auditRPC(ctx, "status", identity.TenantID, identity.AgentID, req.GetSessionId(), err)
		observeRPC("status", err, begin)
		return nil, err
	}

	s.auditRPC(ctx, "status", identity.TenantID, identity.AgentID, req.GetSessionId(), nil)
	observeRPC("status", nil, begin)
	return &proto.SessionStatusResponse{
		SessionId:     last.SessionID,
		State:         runtimeSessionStateToProto(last.State),
		LastActivity:  timestamppb.New(time.Unix(0, last.LastActivity)),
		UptimeSeconds: last.Uptime,
		Timestamp:     timestamppb.Now(),
	}, nil
}

// Close dispatches to Runtime.Close.
func (s *GRPCSpawnServer) Close(ctx context.Context, req *proto.CloseSessionRequest) (*proto.CloseSessionResponse, error) {
	begin := time.Now()
	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimeStatus)
	if authErr != nil {
		s.auditRPC(ctx, "close", req.GetTenantId(), "", req.GetSessionId(), authErr)
		observeRPC("close", authErr, begin)
		return nil, authErr
	}

	if req.GetSessionId() == "" {
		err := status.Error(codes.InvalidArgument, "session_id is required")
		s.auditRPC(ctx, "close", identity.TenantID, identity.AgentID, "", err)
		observeRPC("close", err, begin)
		return nil, err
	}

	var closeErr error
	closed := false
	for _, backendName := range s.runtime.Backends() {
		if err := s.runtime.Close(ctx, backendName, req.GetSessionId()); err == nil {
			closed = true
			break
		} else {
			closeErr = err
		}
	}
	if !closed {
		var err error
		if closeErr != nil {
			err = mapRuntimeError(closeErr)
		} else {
			err = status.Errorf(codes.NotFound, "session %q not found", req.GetSessionId())
		}
		s.auditRPC(ctx, "close", identity.TenantID, identity.AgentID, req.GetSessionId(), err)
		observeRPC("close", err, begin)
		return nil, err
	}

	s.auditRPC(ctx, "close", identity.TenantID, identity.AgentID, req.GetSessionId(), nil)
	observeRPC("close", nil, begin)
	return &proto.CloseSessionResponse{
		SessionId: req.GetSessionId(),
		Timestamp: timestamppb.Now(),
	}, nil
}

// Infer dispatches a stateless inference call to Runtime.Infer and streams
// InferResponse frames back to the caller. The backend must not require a
// prior Spawn — session-based backends are rejected with Unimplemented so
// callers know to use the Spawn+Prompt path instead.
//
// Auth: caller must hold AgentScopeRuntimeInfer or AgentScopeRuntimePrompt
// (both are accepted so operators that already hold the broader prompt scope
// do not need a separate token re-issuance).
func (s *GRPCSpawnServer) Infer(req *proto.InferRequest, stream grpc.ServerStreamingServer[proto.InferResponse]) error {
	begin := time.Now()
	ctx := stream.Context()

	// Accept either the dedicated infer scope or the prompt scope so existing
	// daemon tokens continue to work without re-enrollment.
	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimePrompt)
	if authErr != nil {
		// Try the infer-specific scope as a fallback.
		identity, authErr = s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimeInfer)
		if authErr != nil {
			s.auditRPC(ctx, "infer", req.GetTenantId(), "", "", authErr)
			observeRPC("infer", authErr, begin)
			return authErr
		}
	}

	if req.GetPrompt() == "" {
		err := status.Error(codes.InvalidArgument, "prompt is required")
		s.auditRPC(ctx, "infer", identity.TenantID, identity.AgentID, "", err)
		observeRPC("infer", err, begin)
		return err
	}
	if req.GetBackend() == "" {
		err := status.Error(codes.InvalidArgument, "backend is required")
		s.auditRPC(ctx, "infer", identity.TenantID, identity.AgentID, "", err)
		observeRPC("infer", err, begin)
		return err
	}

	inferReq := &orchestration.InferRequest{
		Backend:      req.GetBackend(),
		Model:        req.GetModel(),
		Prompt:       req.GetPrompt(),
		SystemPrompt: req.GetSystemPrompt(),
		TenantID:     identity.TenantID,
		Metadata:     req.GetMetadata(),
	}

	chunks, err := s.runtime.Infer(ctx, req.GetBackend(), inferReq)
	if err != nil {
		var unsup *orchestration.InferUnsupportedError
		if errors.As(err, &unsup) {
			mapped := status.Errorf(codes.Unimplemented, "infer not supported on backend %q: use Spawn+Prompt", req.GetBackend())
			s.auditRPC(ctx, "infer", identity.TenantID, identity.AgentID, "", mapped)
			observeRPC("infer", mapped, begin)
			return mapped
		}
		// Attached here too, though no backend Infer can reach today produces
		// this evidence: Runtime.Infer refuses every PersistentSessions
		// backend and ACPBackend is the only producer. That is TRUE BY TWO
		// FACTS A PAGE APART, and this boundary has already dropped three
		// things that were each fine until they were not. The call is a no-op
		// when there is no evidence, so the insurance is free.
		mapped := orchestration.AttachTurnFailureEvidence(mapRuntimeError(err), err)
		s.auditRPC(ctx, "infer", identity.TenantID, identity.AgentID, "", mapped)
		observeRPC("infer", mapped, begin)
		return mapped
	}

	for chunk := range chunks {
		if chunk == nil {
			continue
		}
		frame := &proto.InferResponse{
			Delta:      runtimeContentBlocksToProto(chunk.Delta),
			Model:      chunk.Model,
			StopReason: chunk.StopReason,
			Final:      chunk.Final,
		}
		if sendErr := stream.Send(frame); sendErr != nil {
			s.auditRPC(ctx, "infer", identity.TenantID, identity.AgentID, "", sendErr)
			observeRPC("infer", sendErr, begin)
			return sendErr
		}
	}

	s.auditRPC(ctx, "infer", identity.TenantID, identity.AgentID, "", nil)
	observeRPC("infer", nil, begin)
	return nil
}

// ListModels returns the model names available on the named backend. The
// backend must implement orchestration.ModelLister; backends that do not
// expose a model catalog return an empty slice without an error.
//
// Auth: caller must hold AgentScopeRuntimeInfer or AgentScopeRuntimePrompt.
func (s *GRPCSpawnServer) ListModels(ctx context.Context, req *proto.ListModelsRequest) (*proto.ListModelsResponse, error) {
	begin := time.Now()

	if req.GetBackend() == "" {
		err := status.Error(codes.InvalidArgument, "backend is required")
		observeRPC("list_models", err, begin)
		return nil, err
	}

	identity, authErr := s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimePrompt)
	if authErr != nil {
		identity, authErr = s.authorize(req.GetTenantId(), orchestration.AgentScopeRuntimeInfer)
		if authErr != nil {
			s.auditRPC(ctx, "list_models", req.GetTenantId(), "", "", authErr)
			observeRPC("list_models", authErr, begin)
			return nil, authErr
		}
	}

	models, err := s.runtime.ListModels(ctx, req.GetBackend())
	if err != nil {
		mapped := mapRuntimeError(err)
		s.auditRPC(ctx, "list_models", identity.TenantID, identity.AgentID, "", mapped)
		observeRPC("list_models", mapped, begin)
		return nil, mapped
	}

	s.auditRPC(ctx, "list_models", identity.TenantID, identity.AgentID, "", nil)
	observeRPC("list_models", nil, begin)
	return &proto.ListModelsResponse{
		Backend: req.GetBackend(),
		Models:  models,
	}, nil
}

// Heartbeat accepts a bidirectional stream of beats. Daemon-side the server
// acks each beat with the same sequence; policy-refresh payloads are driven
// by the enrollment/control plane and echoed here when present.
func (s *GRPCSpawnServer) Heartbeat(stream grpc.BidiStreamingServer[proto.HeartbeatBeat, proto.HeartbeatAck]) error {
	begin := time.Now()
	ctx := stream.Context()
	identity, authErr := s.authorize("", orchestration.AgentScopeRuntimeStatus)
	if authErr != nil {
		s.auditRPC(ctx, "heartbeat", "", "", "", authErr)
		observeRPC("heartbeat", authErr, begin)
		return authErr
	}

	for {
		beat, err := stream.Recv()
		if err != nil {
			observeRPC("heartbeat", err, begin)
			if errors.Is(err, io.EOF) {
				s.auditRPC(ctx, "heartbeat", identity.TenantID, identity.AgentID, "", nil)
			} else {
				s.auditRPC(ctx, "heartbeat", identity.TenantID, identity.AgentID, "", err)
			}
			return err
		}
		if beat.GetNodeId() != "" && beat.GetNodeId() != identity.AgentID {
			err := status.Errorf(codes.PermissionDenied,
				"heartbeat agent_id %q does not match enrolled identity %q",
				beat.GetNodeId(), identity.AgentID)
			s.auditRPC(ctx, "heartbeat", identity.TenantID, identity.AgentID, "", err)
			observeRPC("heartbeat", err, begin)
			return err
		}
		if sendErr := stream.Send(&proto.HeartbeatAck{
			Sequence:   beat.GetSequence(),
			ReceivedAt: timestamppb.Now(),
		}); sendErr != nil {
			s.auditRPC(ctx, "heartbeat", identity.TenantID, identity.AgentID, "", sendErr)
			observeRPC("heartbeat", sendErr, begin)
			return sendErr
		}
	}
}

// -----------------------------------------------------------------------------
// Auth + translation helpers
// -----------------------------------------------------------------------------

// authorize loads the current identity and validates it against the required
// tenant and scope. Returns a gRPC status error suitable for returning
// directly from an RPC handler.
func (s *GRPCSpawnServer) authorize(tenantID, requiredScope string) (*orchestration.AgentIdentity, error) {
	identity := s.Identity()
	if identity == nil {
		return nil, status.Error(codes.Unauthenticated, "fleet node daemon has not completed enrollment")
	}
	if tenantID != "" && tenantID != identity.TenantID {
		return nil, status.Errorf(codes.PermissionDenied,
			"tenant %q does not match enrolled identity tenant %q", tenantID, identity.TenantID)
	}
	if requiredScope != "" && !identity.HasScope(requiredScope) {
		return nil, status.Errorf(codes.PermissionDenied,
			"missing required scope %q", requiredScope)
	}
	return identity, nil
}

// errNodeDraining is the status message of a call refused, or a turn cut off,
// because this agent is shutting down.
var errNodeDraining = errors.New("agent: draining for shutdown")

// mapPromptError converts a failed turn into its gRPC status.
//
// A TURN THAT FAILED WHILE THIS AGENT DRAINS IS THE AGENT'S DOING. Its worker's
// stream was closed by the shutdown, or its context was cancelled when the
// drain's bound ran out; either way the worker did not fail. It is reported
// Unavailable, which the control plane refunds (orchestration,
// agent_interruption.go).
//
// A CONTAINER STREAM THAT CLOSED WITHOUT THE WORKER'S OWN EXIT IS TRANSPORT
// LOSS, drain or no drain. The container was killed, its runtime failed, or
// its pipe was cut — none of which is the worker failing — so it is reported
// Unavailable too. Only a stream that closed with the worker's non-zero exit
// status (ContainerStreamClosedError.WorkerCrashed) is a crash; it takes the
// ordinary mapping, Internal, and is still charged.
func (s *GRPCSpawnServer) mapPromptError(err error) error {
	if s.draining.Load() {
		return status.Error(codes.Unavailable, errNodeDraining.Error()+": "+err.Error())
	}
	var closed *orchestration.ContainerStreamClosedError
	if errors.As(err, &closed) && !closed.WorkerCrashed() {
		return status.Error(codes.Unavailable, errContainerTransportLost.Error()+": "+err.Error())
	}
	return mapRuntimeError(err)
}

// errContainerTransportLost is the status message of a turn whose worker's
// container stream closed without the worker exiting on its own.
var errContainerTransportLost = errors.New("agent: the worker's container stream closed without a worker exit")

// mapRuntimeError converts runtime errors into gRPC status errors. Known
// domain errors become their canonical codes; unknown errors fall through
// as codes.Internal.
func mapRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	var violation *orchestration.SandboxModeViolationError
	if errors.As(err, &violation) {
		return status.Errorf(codes.FailedPrecondition, "sandbox policy violation: %s", violation.Error())
	}
	// A refused bind mount is an authorization decision about the request the
	// caller sent, not a fault on this host: PermissionDenied, carrying the
	// message that names the offending field and root. The control plane
	// re-surfaces that code as 403 rather than a bare 500.
	var mountDenied *orchestration.MountNotAllowedError
	if errors.As(err, &mountDenied) {
		return status.Error(codes.PermissionDenied, mountDenied.Error())
	}
	var input *orchestration.InputValidationError
	if errors.As(err, &input) {
		return status.Errorf(codes.InvalidArgument, "invalid input: %s", input.Error())
	}
	// A prompt / status / close against a session the backend doesn't hold is
	// a not-found condition, not an internal error. Mapping it to codes.NotFound
	// lets the central orchestrator and REST layer surface a clean 404 instead
	// of a generic 500 for a session that was just spawned on a different
	// instance.
	if errors.Is(err, orchestration.ErrSessionNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, ErrRuntimeUnknownBackend) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, ErrRuntimeNoImage) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	// A relay attach this daemon refuses: a session it does not hold is not
	// found, and one of another tenant is answered the same way — never as
	// somebody else's; a session with no relay, or one already attached, is a
	// precondition of the request.
	if errors.Is(err, ErrSigningRelayUnknownSession) || errors.Is(err, ErrSigningRelayForeignSession) {
		return status.Error(codes.NotFound, err.Error())
	}
	if errors.Is(err, ErrSigningRelayNotRequested) || errors.Is(err, ErrSigningRelayAlreadyAttached) ||
		errors.Is(err, ErrSigningRelayUnsupported) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	// A named workspace this host could not provision or find is a
	// precondition of the request — a stale pin, a foreign tenant, no projects
	// root — never an internal fault. Its class travels as SpawnFaultEvidence
	// so the control plane rebuilds ErrSessionWorkspace.
	if errors.Is(err, orchestration.ErrSessionWorkspace) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	// A workspace removal refused because a session here still holds it is a
	// precondition too: the sweep retries it once the session is gone.
	var held *orchestration.WorkspaceHeldError
	if errors.As(err, &held) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	// A session that runs no container has nowhere for a command to run.
	if errors.Is(err, orchestration.ErrSessionNotContainerized) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// runtimeSessionStateMap dispatches SessionState -> proto SessionState in
// O(1) without a switch statement.
var runtimeSessionStateMap = map[orchestration.SessionState]proto.SessionState{
	orchestration.SessionStateActive:    proto.SessionState_SESSION_STATE_ACTIVE,
	orchestration.SessionStateIdle:      proto.SessionState_SESSION_STATE_IDLE,
	orchestration.SessionStateClosed:    proto.SessionState_SESSION_STATE_CLOSED,
	orchestration.SessionStateError:     proto.SessionState_SESSION_STATE_ERROR,
	orchestration.SessionStateResumable: proto.SessionState_SESSION_STATE_RESUMABLE,
}

func runtimeSessionStateToProto(s orchestration.SessionState) proto.SessionState {
	if v, ok := runtimeSessionStateMap[s]; ok {
		return v
	}
	return proto.SessionState_SESSION_STATE_UNSPECIFIED
}

func runtimeContentBlocksToProto(blocks []orchestration.ContentBlock) []*proto.ContentBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]*proto.ContentBlock, len(blocks))
	for i, b := range blocks {
		out[i] = &proto.ContentBlock{Type: b.Type, Text: b.Text}
	}
	return out
}

func protoMCPServersToRuntime(in []*proto.MCPServerConfig) []orchestration.MCPServerConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]orchestration.MCPServerConfig, len(in))
	for i, s := range in {
		command := s.GetCommand()
		if command == "" {
			// Command mirrors URL for senders that predate the command field
			// and encoded the endpoint there. Only when the sender said
			// nothing: a sender that set command meant that command.
			command = s.GetUrl()
		}
		out[i] = orchestration.MCPServerConfig{
			Name:    s.GetName(),
			URL:     s.GetUrl(),
			Env:     s.GetEnv(),
			Command: command,
			Args:    s.GetArgs(),
		}
	}
	return out
}

// protoMountsToRuntime converts proto container mounts to the orchestration
// shape. Nil for an empty repeated field, so "no extra mounts" stays
// indistinguishable from a caller that predates the field.
func protoMountsToRuntime(in []*proto.ContainerMount) []orchestration.ContainerMount {
	if len(in) == 0 {
		return nil
	}
	out := make([]orchestration.ContainerMount, len(in))
	for i, m := range in {
		out[i] = orchestration.ContainerMount{
			Source:         m.GetSource(),
			Target:         m.GetTarget(),
			ReadOnly:       m.GetReadOnly(),
			Type:           m.GetType(),
			TmpfsSizeBytes: m.GetTmpfsSizeBytes(),
		}
	}
	return out
}

// protoStorageToRuntime converts the wire storage plan to the orchestration
// shape. A message the control plane left off the wire is the inert plan, which
// is what every project with no storage configuration resolves to.
func protoStorageToRuntime(in *proto.WorkspaceStoragePlan) orchestration.WorkspaceStoragePlan {
	if in == nil {
		return orchestration.WorkspaceStoragePlan{}
	}
	return orchestration.WorkspaceStoragePlan{
		Tier:           orchestration.WorkspaceTier(in.GetTier()),
		TmpfsSizeBytes: in.GetTmpfsSizeBytes(),
		MemoryBytes:    in.GetMemoryBytes(),
	}
}

// isGracefulServerClosed returns true if err came from a graceful server
// shutdown and should be treated as success. gRPC's Serve returns nil on
// graceful stop, but net.Listener errors (e.g., "use of closed network
// connection") may surface on a forced Stop.
func isGracefulServerClosed(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, grpc.ErrServerStopped) {
		return true
	}
	return false
}

// -----------------------------------------------------------------------------
// Audit + metrics
// -----------------------------------------------------------------------------

// AuditSink is the narrow interface the spawn server uses to record per-RPC
// audit events. *security.AuditLogger satisfies this via its existing Log
// method, keeping production wiring trivial while letting tests inject a
// recording fake without constructing the full audit pipeline.
type AuditSink interface {
	Log(ctx context.Context, event *security.AuditEvent) error
}

// auditRPC records a single per-RPC audit event. A nil rpcErr means success;
// a non-nil error (including gRPC status errors) is recorded with Result
// derived from the gRPC code: PermissionDenied/Unauthenticated/FailedPrecondition
// become "denied"; everything else becomes "failure".
func (s *GRPCSpawnServer) auditRPC(ctx context.Context, rpc, tenantID, nodeID, sessionID string, rpcErr error) {
	if s.audit == nil {
		return
	}
	event := &security.AuditEvent{
		Timestamp: time.Now(),
		Action:    "agent.runtime." + rpc,
		Resource:  "agent_runtime",
		UserID:    nodeID,
		Username:  tenantID,
		Details: map[string]string{
			"tenant_id": tenantID,
			"agent_id":  nodeID,
		},
	}
	if sessionID != "" {
		event.Details["session_id"] = sessionID
	}
	if rpcErr == nil {
		event.Result = security.ResultSuccess
	} else {
		event.Result = auditResultForError(rpcErr)
		event.ErrorMsg = rpcErr.Error()
	}
	if logErr := s.audit.Log(ctx, event); logErr != nil {
		s.logger.Warnf("agent spawn server audit %s: %v", rpc, logErr)
	}
}

// auditResultForError maps a handler error to an audit Result value. The
// gRPC authorization codes signal denial; everything else is a failure.
func auditResultForError(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return security.ResultFailure
	}
	switch st.Code() {
	case codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition:
		return security.ResultDenied
	default:
		return security.ResultFailure
	}
}

// observeRPC records a single duration sample into the RPCDuration histogram.
// The "code" label is the gRPC code string; unknown errors fall under
// codes.Internal.
func observeRPC(rpc string, err error, begin time.Time) {
	RPCDuration.WithLabelValues(rpc, grpcCodeString(err)).Observe(time.Since(begin).Seconds())
}

// grpcCodeString returns the canonical code label for metrics. io.EOF is
// the normal stream-close and is recorded as OK.
func grpcCodeString(err error) string {
	if err == nil || errors.Is(err, io.EOF) {
		return codes.OK.String()
	}
	if st, ok := status.FromError(err); ok {
		return st.Code().String()
	}
	return codes.Unknown.String()
}

// -----------------------------------------------------------------------------
// Spawn server typed errors
// -----------------------------------------------------------------------------

// SpawnServerBindError wraps a net.Listen failure during Start.
type SpawnServerBindError struct {
	Addr string
	Err  error
}

func (e *SpawnServerBindError) Error() string {
	return "agent: spawn server bind " + e.Addr + ": " + e.Err.Error()
}

func (e *SpawnServerBindError) Unwrap() error { return e.Err }

// SpawnServerServeError wraps a non-graceful Serve failure.
type SpawnServerServeError struct {
	Err error
}

func (e *SpawnServerServeError) Error() string {
	return "agent: spawn server serve: " + e.Err.Error()
}

func (e *SpawnServerServeError) Unwrap() error { return e.Err }
