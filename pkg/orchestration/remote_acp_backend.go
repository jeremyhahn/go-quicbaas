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

package orchestration

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Remote ACP Backend — talks to a pkg/fleet daemon over FleetRuntime gRPC
// =============================================================================

// Remote backend sentinel errors.
var (
	// ErrRemoteBackendNoRegistration is returned when construction is attempted
	// without a non-nil FleetNode.
	ErrRemoteBackendNoRegistration = errors.New("remote_acp_backend: registration is required")

	// ErrRemoteBackendNoLogger is returned when construction is attempted
	// without a logger.
	ErrRemoteBackendNoLogger = errors.New("remote_acp_backend: logger is required")

	// ErrRemoteBackendNoHost is returned when the registration's Host field
	// is empty.
	ErrRemoteBackendNoHost = errors.New("remote_acp_backend: registration host is required")

	// ErrRemoteBackendNotDaemon is returned when the registration does not
	// resolve to FleetNodeKindDaemon. Only daemon-kind registrations are eligible
	// for remote routing.
	ErrRemoteBackendNotDaemon = errors.New("remote_acp_backend: registration is not a daemon")

	// ErrRemoteBackendNoBackend is returned when construction is attempted
	// without naming the backend the instance serves. There is no default:
	// falling back to the registration's primary is precisely the defect this
	// field exists to prevent (go-quicbaas#937), so an unnamed backend is a
	// construction error rather than a silent reroute.
	ErrRemoteBackendNoBackend = errors.New("remote_acp_backend: served backend name is required")

	// ErrRemoteBackendEmptyStream is returned when the daemon closes the
	// prompt stream without sending any response frame.
	ErrRemoteBackendEmptyStream = errors.New("remote_acp_backend: daemon closed prompt stream without response")

	// ErrRemoteBackendSessionsUnsupported is returned when a caller asks for
	// behavior incompatible with the remote session contract (e.g., status
	// lookups on a backend that doesn't support sessions).
	ErrRemoteBackendSessionsUnsupported = errors.New("remote_acp_backend: remote backend does not support this operation")

	// ErrRemoteBackendToken is the sentinel every RemoteBackendTokenError
	// unwraps to: the bearer token a daemon RPC needs could not be had, so the
	// RPC was not sent.
	ErrRemoteBackendToken = errors.New("remote_acp_backend: bearer token unavailable")

	// ErrRemoteBackendEmptyToken is the cause of a RemoteBackendTokenError
	// whose token source answered without an access token.
	ErrRemoteBackendEmptyToken = errors.New("remote_acp_backend: token source returned no access token")
)

// RemoteBackendDialError indicates a gRPC connection failure to the remote
// daemon. The wrapped Err is the underlying grpc.Dial / connection error.
type RemoteBackendDialError struct {
	Host string
	Err  error
}

// Error implements the error interface.
func (e *RemoteBackendDialError) Error() string {
	return "remote_acp_backend: dial " + e.Host + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error for errors.Is / errors.As support.
func (e *RemoteBackendDialError) Unwrap() error { return e.Err }

// RemoteBackendRPCError indicates the remote daemon returned a non-OK status.
// Callers should prefer errors.As to unwrap the underlying status.Status via
// google.golang.org/grpc/status.FromError.
type RemoteBackendRPCError struct {
	Op  string
	Err error
}

// Error implements the error interface.
func (e *RemoteBackendRPCError) Error() string {
	return "remote_acp_backend: rpc " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error for errors.Is / errors.As support.
func (e *RemoteBackendRPCError) Unwrap() error { return e.Err }

// RemoteBackendTokenError reports that the bearer token for a daemon RPC could
// not be had. The RPC is not sent: a daemon call without its token is refused
// there anyway, and sending it would hide the cause behind an Unauthenticated
// status. Err is the cause, which is the caller's own context error when its
// deadline or cancellation came before the token did.
type RemoteBackendTokenError struct {
	Host string
	Err  error
}

// Error implements the error interface.
func (e *RemoteBackendTokenError) Error() string {
	return "remote_acp_backend: bearer token for " + e.Host + ": " + e.Err.Error()
}

// Unwrap returns ErrRemoteBackendToken and the cause, so errors.Is matches both
// the sentinel and, say, context.DeadlineExceeded.
func (e *RemoteBackendTokenError) Unwrap() []error {
	return []error{ErrRemoteBackendToken, e.Err}
}

// RemoteTokenSource supplies the bearer token a daemon RPC carries.
//
// THE CONTEXT IS THE RPC'S, and an implementation must return when it is done.
// oauth2.TokenSource takes no context, so a slow token endpoint held every
// remote RPC — and the REST or gRPC handler waiting on it — past its caller's
// deadline (go-quicbaas#193).
type RemoteTokenSource interface {
	Token(ctx context.Context) (*oauth2.Token, error)
}

// RemoteBackendMismatchError indicates a request named a backend other than
// the one this instance is bound to. It is the routing invariant stated as an
// error: one RemoteACPBackend serves exactly one backend name on exactly one
// daemon, and a request carrying a different name was dispatched to the wrong
// instance. Only requests that carry their own backend name (InferRequest) can
// be checked this way; for the rest the name is a single field read from this
// instance, so no mismatch is representable.
type RemoteBackendMismatchError struct {
	// Op is the RPC that was refused.
	Op string
	// Bound is the backend this instance serves.
	Bound string
	// Requested is the backend the request named.
	Requested string
}

// Error implements the error interface.
func (e *RemoteBackendMismatchError) Error() string {
	return "remote_acp_backend: " + e.Op + ": instance serves backend " + e.Bound +
		", request named " + e.Requested
}

// newUnservedBackendError reports that a registration was asked for a backend
// it does not serve. It reuses the fleet's own NoMatchingFleetNodeError vocabulary
// — the same type pkg/server's resolver raises when no daemon satisfies the
// backend constraint — so the refusal reaches the public API as the
// no_matching_agent it is, rather than as a new parallel class.
func newUnservedBackendError(reg *FleetNode, requested string) *NoMatchingFleetNodeError {
	detail := "agent " + reg.ID + " (" + reg.Host + ") does not serve backend=" + requested +
		"; it serves primary=" + reg.Backend
	if len(reg.Capabilities) > 0 {
		detail += " capabilities=[" + strings.Join(reg.Capabilities, ",") + "]"
	}
	return &NoMatchingFleetNodeError{Constraint: "backend", Detail: detail}
}

// RemoteACPBackendDeps injects production dependencies into
// NewRemoteACPBackend. Fields that support "nil for tests" are annotated as
// such; production callers populate everything.
type RemoteACPBackendDeps struct {
	// Registration identifies the daemon to connect to. Registration.Host is
	// expected to be a dialable "host:port" string.
	//
	// Registration.Backend is the daemon's PRIMARY backend and is NOT what
	// travels on the wire — see Backend below.
	Registration *FleetNode

	// Backend is the backend name this instance serves: the name stamped on
	// every RPC it makes. Required, and it must be one the registration
	// actually serves (FleetNodeServesBackend), which is the daemon's primary or
	// any backend in its capability set.
	//
	// THE FIELD EXISTS BECAUSE THE PRIMARY IS THE WRONG ANSWER. A daemon
	// registers one primary and advertises the whole set it serves; resolution
	// picks it for any of them. Stamping the primary on the wire meant a
	// session resolved for codex was served by claude, silently, with the
	// session record still saying codex (go-quicbaas#937) — and an
	// independent-reviewer gate satisfied in configuration while violated in
	// fact. One instance serves one backend name; the pool constructs one per
	// requested name.
	Backend string

	// TokenSource supplies OAuth2 bearer tokens for daemon RPCs. May be nil
	// in tests that use ConnDialer to establish insecure connections.
	TokenSource RemoteTokenSource

	// ClientTLSConfig is used for mTLS to the daemon. When nil and ConnDialer
	// is also nil, the backend dials plaintext — only safe for tests.
	ClientTLSConfig *tls.Config

	// ConnDialer, when non-nil, replaces the default grpc.NewClient path.
	// Intended for bufconn-backed tests. When set, the backend does not
	// configure credentials itself (the dialer is expected to).
	ConnDialer func(ctx context.Context, target string) (*grpc.ClientConn, error)

	// Logger is used for lifecycle and per-call diagnostics. Required.
	Logger interfaces.Logger

	// Auditor records a remote spawn the daemon refused because the whole
	// account pool was unavailable (see auditAccountPoolExhausted). Nil
	// records nothing.
	Auditor StoreAuditor
	// SigningTrait returns this daemon's signing-agent trait
	// (SigningAgentTrait of its sockets root's identifier), or "" when it has
	// none yet. An agent advertising it mounts a signing socket directly; one
	// that does not, but advertises FleetNodeTraitSigningRelay, is served over a
	// relay (go-quicbaas#1013). Nil answers "".
	SigningTrait func() string

	// SigningAudit records every signature relayed to the agent. Nil records
	// nothing, which only a test may do.
	SigningAudit SigningRelayAuditor
}

// RemoteACPBackend implements AgentBackend by forwarding calls to a remote
// pkg/fleet daemon over the FleetRuntime gRPC service. One backend instance
// is scoped to a single FleetNode; callers route to the right
// registration via the BackendDispatcher's remote resolver.
type RemoteACPBackend struct {
	registration *FleetNode

	// backend is the backend name this instance serves. It is the SINGLE
	// source of the name on every RPC and on the Session that Spawn returns,
	// which is what makes "the session's recorded backend equals the backend
	// named on its RPCs" structurally true rather than merely checked.
	backend string

	pool        *grpcConnPool
	tokenSource RemoteTokenSource
	logger      interfaces.Logger
	auditor     StoreAuditor

	signingTrait func() string
	signingAudit SigningRelayAuditor

	// relays maps session ID -> the cancel of that session's signing relay.
	// Close cancels it; a relay the agent ends removes itself.
	relays sync.Map
}

// Compile-time interface checks.
var _ AgentBackend = (*RemoteACPBackend)(nil)
var _ ModelLister = (*RemoteACPBackend)(nil)

// NewRemoteACPBackend constructs a backend from validated deps.
func NewRemoteACPBackend(deps RemoteACPBackendDeps) (*RemoteACPBackend, error) {
	if deps.Registration == nil {
		return nil, ErrRemoteBackendNoRegistration
	}
	if deps.Logger == nil {
		return nil, ErrRemoteBackendNoLogger
	}
	if strings.TrimSpace(deps.Registration.Host) == "" {
		return nil, ErrRemoteBackendNoHost
	}
	kind := deps.Registration.EffectiveKind()
	if kind != FleetNodeKindDaemon && kind != FleetNodeKindEmbedded {
		return nil, ErrRemoteBackendNotDaemon
	}
	served := strings.TrimSpace(deps.Backend)
	if served == "" {
		return nil, ErrRemoteBackendNoBackend
	}
	// THE REFUSAL THE OLD CODE COULD NOT EXPRESS. It never compared the
	// requested backend against the registration, so a spawn for a backend the
	// selected daemon does not serve produced a working session on some other
	// backend. Comparing them at the one point that builds the routing object
	// means no instance can exist that names a backend its daemon cannot run.
	if !FleetNodeServesBackend(deps.Registration, served) {
		return nil, newUnservedBackendError(deps.Registration, served)
	}

	pool := newGRPCConnPool(grpcConnPoolConfig{
		TLSConfig: deps.ClientTLSConfig,
		Dialer:    deps.ConnDialer,
		Logger:    deps.Logger,
	})

	return &RemoteACPBackend{
		registration: deps.Registration,
		backend:      served,
		pool:         pool,
		tokenSource:  deps.TokenSource,
		logger:       deps.Logger,
		auditor:      deps.Auditor,
		signingTrait: deps.SigningTrait,
		signingAudit: deps.SigningAudit,
	}, nil
}

// Registration returns the backend's bound FleetNode. Exposed for
// diagnostics and for the BackendDispatcher's routing decisions.
func (b *RemoteACPBackend) Registration() *FleetNode {
	return b.registration
}

// Backend returns the backend name this instance serves — the name it stamps
// on every RPC. It is not necessarily the registration's primary backend.
func (b *RemoteACPBackend) Backend() string {
	return b.backend
}

// ResolvedHost returns the daemon host this backend is bound to. The
// orchestrator captures it at Spawn time and replays it on Prompt / Status /
// Close so every call for a session reaches the same daemon instance that
// holds the live in-memory session. Implements resolvedHostBackend.
func (b *RemoteACPBackend) ResolvedHost() string {
	if b.registration == nil {
		return ""
	}
	return b.registration.Host
}

// ResolvedAgentID returns the fleet node this backend is bound to. The
// orchestrator captures it at Spawn time so the session's job can name the
// agent that ran the work. Implements resolvedAgentBackend.
func (b *RemoteACPBackend) ResolvedAgentID() string {
	if b.registration == nil {
		return ""
	}
	return b.registration.ID
}

// Close terminates a remote session by delegating to the daemon's
// FleetRuntime.Close. Pool-level teardown is a separate operation — see
// CloseAll.
func (b *RemoteACPBackend) Close(ctx context.Context, sessionID string) error {
	// The session's signing window closes with it, whatever the daemon says.
	b.stopSigningRelay(sessionID)
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return err
	}
	_, rpcErr := client.Close(rpcCtx, &proto.CloseSessionRequest{
		SessionId: sessionID,
		TenantId:  b.registration.TenantID,
	})
	if rpcErr != nil {
		return &RemoteBackendRPCError{Op: "Close", Err: rpcErr}
	}
	return nil
}

// CloseAll terminates the underlying conn pool. Used by orchestrator teardown
// when the backend is being removed from the dispatcher.
func (b *RemoteACPBackend) CloseAll() error {
	return b.pool.Close()
}

// Spawn dispatches to the daemon's FleetRuntime.Spawn.
func (b *RemoteACPBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	b.logger.Debugf("remote_acp_backend: Spawn backend=%s tenant=%s project=%s",
		b.backend, b.registration.TenantID, req.ProjectID)

	// THE SIGNING SOCKET IS RELAYED, NOT MOUNTED, on an agent that cannot
	// reach this daemon's sockets root (go-quicbaas#1013). The mount is
	// dropped — the agent binds its own socket at the same target — and the
	// keys it will advertise are read off the session's own agent here.
	extraMounts := req.ExtraMounts
	var relaySpec *proto.SigningRelaySpec
	relaySocket := ""
	if mount, signs := signingRelayMount(req); signs && relaySigningFor(b.registration, b.localSigningTrait()) {
		keys, keysErr := listSigningAgentKeys(ctx, mount.Source)
		if keysErr != nil {
			return nil, keysErr
		}
		relaySocket = mount.Source
		extraMounts = WithoutSigningMount(req.ExtraMounts)
		relaySpec = &proto.SigningRelaySpec{PublicKeys: keys}
	}
	pbReq := &proto.SpawnSessionRequest{
		// THE BACKEND THIS INSTANCE SERVES, which is the backend the resolver
		// was asked for — not the daemon's primary (go-quicbaas#937).
		Backend:        b.backend,
		ProjectId:      req.ProjectID,
		WorkDir:        req.WorkDir,
		Model:          req.Model,
		SystemPrompt:   req.SystemPrompt,
		Env:            req.Env,
		McpServers:     runtimeMCPServersToProto(req.MCPServers),
		SandboxMode:    proto.SandboxMode(req.SandboxMode),
		Sandboxed:      req.Sandboxed,
		ContainerImage: req.ContainerImage,
		TenantId:       b.registration.TenantID,
		Persistent:     req.Persistent,
		// FORWARDED, because the caller's grant is the whole point of it. The
		// SDLC module states its worker's permission mode on the spawn
		// (go-quicbaas#218); dropping it here would leave every REMOTE worker
		// spawned exactly the way the fault was measured, with the local path
		// fixed and the distributed one silently not.
		ExtraArgs: req.ExtraArgs,
		// The pooled account is selected and materialized by the daemon that
		// actually runs the agent — the credential lives in that daemon's own
		// secret store, never on the wire. Only the caller's request is
		// forwarded; the remote daemon persists the account it resolved on its
		// own session record and resumes against it. A remote session is not
		// resumable by this control plane (see SessionRecord.Resumable), so no
		// pin is needed on this side.
		AccountId:       req.AccountID,
		AccountAuto:     req.AccountAuto,
		AccountStrategy: req.AccountStrategy,
		// ACCOUNTALLOW IS A RESTRICTION, WHICH IS WHY DROPPING IT IS THE
		// DANGEROUS DIRECTION. It names the accounts one session may use and
		// rotate through; empty means the WHOLE tenant/project/provider pool is
		// eligible. So a request that omitted it here did not fail — it
		// silently broadened from the caller's chosen set to everything the
		// tenant owns, and the session could bill an account its operator had
		// deliberately excluded. Found by review of #235, the fourth thing this
		// boundary has quietly dropped.
		AccountAllow: req.AccountAllow,
		// AccountExclude is AccountAllow's other half, and it is carried for
		// the identical reason: it is a RESTRICTION whose empty value means "no
		// restriction". Rotation sets it to the account that has just hit a
		// limit so the replacement can never be the same one; a boundary that
		// forwarded the allow-list and dropped the exclusion would move a
		// session off an account and straight back onto it.
		AccountExclude: req.AccountExclude,
		// JobID travels because the daemon cannot derive it, and its account
		// resolution puts it in the delivery grant's scope so a remote
		// session's grant records the job it served (account_lease.go).
		JobId: req.JobID,

		// THE CONTAINER AND TRUST DIMENSION (#248). Both ends were already
		// built for these — the orchestrator populates every one of them on the
		// SpawnRequest it dispatches (orchestrator.go, spawnReq), and the
		// ACPBackend the daemon runs consumes every one of them when it builds
		// the container spec — and only the wire between them was missing. So a
		// remote session ran without the mount its operator had authorized and
		// at whatever tier the daemon defaulted to, silently, because each of
		// these is inert at its zero value and nothing fails when a request
		// asks for nothing.
		ExtraMounts:     runtimeMountsToProto(extraMounts),
		ImageDigest:     req.ImageDigest,
		TrustLevel:      proto.TrustLevel(req.TrustLevel),
		TrustLabel:      req.TrustLabel,
		ReferenceValues: req.ReferenceValues,

		// THE STORAGE TIER, AND IT TRAVELS FOR THE TRUST TIER'S OWN REASON.
		// This control plane holds the project record and resolved the tier
		// from it; the daemon that creates the container is the only party that
		// can apply it. Dropped, it would not fail — every field of it is inert
		// at its zero value — it would run the remote worker's checkout on the
		// disk its operator configured the project off, with no memory limit on
		// the tmpfs the tier depends on.
		Storage: runtimeStorageToProto(req.Storage),

		// THE WORKSPACE BY NAME (go-quicbaas#981). WorkDir is a path on THIS
		// host; the daemon provisions the named workspace under its own
		// projects root and answers with where, below.
		Workspace: sessionWorkspaceToProto(req.Workspace),

		// The relay the agent serves the signing socket from, when it serves
		// one; nil otherwise.
		SigningRelay: relaySpec,
	}
	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return nil, err
	}
	resp, rpcErr := client.Spawn(rpcCtx, pbReq)
	if rpcErr != nil {
		// The daemon's configuration-fault class, when it attached one, so a
		// dispatch that started no process is classified the same whether the
		// resolver ran here or on another host (#235).
		spawnErr := wrapRemoteSpawnFault("Spawn", rpcErr)
		b.auditPoolExhaustion(ctx, req, spawnErr)
		return nil, &RemoteBackendRPCError{Op: "Spawn", Err: spawnErr}
	}
	// THE RELAY IS ATTACHED BEFORE SPAWN RETURNS, so nothing the caller does
	// with the session — the checkout exec above all — can ask for a signature
	// the relay is not yet there to carry. A relay that cannot be attached
	// fails the spawn, and the session it was for is closed.
	if relaySpec != nil {
		if relayErr := b.openSigningRelay(ctx, client, resp.GetSessionId(), relaySocket, relaySpec.GetPublicKeys()); relayErr != nil {
			if closeErr := b.Close(context.WithoutCancel(ctx), resp.GetSessionId()); closeErr != nil {
				b.logger.Warnf("remote_acp_backend: close session %s after its relay failed: %v", resp.GetSessionId(), closeErr)
			}
			return nil, relayErr
		}
	}
	createdAt := time.Now().UnixNano()
	if resp.GetTimestamp() != nil {
		createdAt = resp.GetTimestamp().AsTime().UnixNano()
	}
	// A NAMED WORKSPACE'S DIRECTORY IS THE DAEMON'S ANSWER, and only the
	// daemon's: it provisioned it under a root this host knows nothing about.
	// An empty answer is a daemon that did not provision it, which the
	// orchestrator refuses (requireAgentWorkspace).
	workDir := req.WorkDir
	if req.Workspace != nil {
		workDir = resp.GetWorkDir()
	}
	return &Session{
		ID:        resp.GetSessionId(),
		ProjectID: req.ProjectID,
		// THE SESSION'S RECORDED BACKEND AND THE ONE ON ITS RPCs ARE ONE FIELD.
		// Both this and pbReq.Backend above read b.backend, so the invariant
		// "a session's recorded backend equals the backend named on its RPCs"
		// holds by construction and needs no runtime check; there is no second
		// value for them to disagree about. The same field serves Prompt,
		// Infer and ListModels.
		Backend:   b.backend,
		Model:     resp.GetModel(),
		Status:    SessionStateActive,
		WorkDir:   workDir,
		CreatedAt: createdAt,

		// THE ACCOUNT THE DAEMON RESOLVED, WHICH ONLY THE DAEMON KNOWS.
		// Selection runs there, on a pool whose credentials never cross this
		// wire, so the answer can only come back in the response. Without it
		// the orchestrator recorded no AccountID on the durable session record
		// and stamped none onto the session's turn usage, and the cost of every
		// remotely-executed session was attributable to no account at all —
		// #248's class in the response direction.
		AccountID:       resp.GetAccountId(),
		AccountStrategy: resp.GetAccountStrategy(),

		// AND WHICH COPY OF ITS CREDENTIAL THE DAEMON MATERIALIZED, for the
		// same reason and with one more consequence. This side records the
		// generation on the durable session record and asks, before every
		// prompt, whether the account has moved past it. Dropping it would put
		// remote sessions back on the timestamp comparison, which reports a
		// rotation that landed while the container was still starting as fresh
		// — the defect go-quicbaas#997 exists to remove, left in place for the
		// sessions this control plane can least afford to lose.
		AccountGeneration: resp.GetAccountGeneration(),

		// THE DELIVERY GRANT THE RELEASE WAS SERVED UNDER, for exactly the
		// reason the account is carried and one step more urgent: this side
		// RECORDS it on the durable session record, and the session's conclusion
		// withdraws it.
		//
		// This control plane ISSUED that grant — its own materialize verb did,
		// when it released the credential to the daemon — but it could not tell
		// which session the grant belonged to, because the release happens
		// inside the daemon's resolution. The daemon reports it back here
		// (pkg/fleet/account_grant.go). A grant that did not come back would be
		// a credential on a remote host that this control plane leased and could
		// never revoke.
		CredentialLeaseID: resp.GetCredentialLeaseId(),

		// THE RESERVATION THE DAEMON'S RESOLUTION TOOK, against this control
		// plane's table. This side records it on the durable session record,
		// and the record naming it is what satisfies it.
		ReservationID: resp.GetReservationId(),

		// THE CONTAINER THE DAEMON PUT THE AGENT IN (#740), for the same reason
		// the account is carried: the daemon created it and nothing on this side
		// can derive it. Without it the orchestrator recorded an empty
		// ContainerID for every remotely-executed session, and the SDLC runner —
		// which refuses to let a worker start work it could never publish,
		// because the delivery's commit, signature and push all run inside the
		// worker's container — refused every remote worker as "runs no
		// container".
		//
		// THE ID BELONGS TO THE DAEMON'S HOST, and that is a constraint on who
		// may act on it rather than a reason to drop it. It addresses a
		// container under the container runtime the AGENT owns; the control
		// plane's own runtime is a different one whenever the agent is on a
		// different host. The session record names that host (SessionRecord
		// HostAffinity) precisely so a command can be routed to the daemon that
		// can run it. See go-quicbaas#743 for the exec seam that routing needs
		// and does not yet have.
		ContainerID: resp.GetContainerId(),
	}, nil
}

// Prompt dispatches to the daemon's FleetRuntime.Prompt and accumulates the
// single-frame server stream into a non-streaming PromptResponse. Streaming
// through to callers is Phase 7 scope.
func (b *RemoteACPBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}

	b.logger.Debugf("remote_acp_backend: Prompt backend=%s tenant=%s session=%s",
		b.backend, b.registration.TenantID, req.SessionID)
	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return nil, err
	}
	stream, rpcErr := client.Prompt(rpcCtx, &proto.PromptSessionRequest{
		Backend:      b.backend,
		SessionId:    req.SessionID,
		ProjectId:    req.ProjectID,
		Prompt:       req.Prompt,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
		TenantId:     b.registration.TenantID,
	})
	if rpcErr != nil {
		// See promptError: the daemon's turn-failure evidence and whether the
		// agent itself cut the turn off are both read here.
		return nil, b.promptError(ctx, rpcErr)
	}

	out := &PromptResponse{}
	gotFrame := false
	for {
		frame, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			// Same on the receive path: a streaming Prompt reports the turn's
			// failure here rather than at the call, so the evidence has to be
			// read off both or the fix works only for the half that fails
			// early.
			return nil, b.promptError(ctx, recvErr)
		}
		gotFrame = true
		if frame.GetSessionId() != "" {
			out.SessionID = frame.GetSessionId()
		}
		if frame.GetModel() != "" {
			out.Model = frame.GetModel()
		}
		if frame.GetStopReason() != "" {
			out.StopReason = frame.GetStopReason()
		}
		// THE TURN'S COST COMES BACK WITH ITS ANSWER. The daemon has no
		// ledger; this frame is the only way a remote turn is ever charged.
		// A figure that cannot be decoded is reported and dropped rather than
		// failing a turn whose answer is already here: the answer is the
		// work, the figure is bookkeeping about it.
		usage, usageErr := promptUsageFromWire(frame.GetUsageJson())
		if usageErr != nil {
			b.logger.Warnf("remote_acp_backend: turn usage from backend=%s session=%s could not be decoded; the turn is not charged: %s",
				b.backend, req.SessionID, usageErr.Error())
		}
		if usage != nil {
			out.Usage = usage
		}
		// The wire frame carries no tool census, so out.ToolUse stays nil: a
		// remote turn does not report its tool use.
		out.Content = append(out.Content, protoContentBlocksToRuntime(frame.GetContent())...)
		if req.EventCallback != nil {
			for _, block := range frame.GetContent() {
				// Only words are assistant chunks: a tool block carries no
				// payload to stream.
				if block.GetType() != ContentBlockText || block.GetText() == "" {
					continue
				}
				req.EventCallback(&SessionEvent{
					SessionID: out.SessionID,
					Kind:      KindAssistantChunk,
					AssistantChunk: &AssistantChunkPayload{
						Text:       block.GetText(),
						StopReason: out.StopReason,
						Model:      out.Model,
					},
				})
			}
		}
	}
	if !gotFrame {
		return nil, ErrRemoteBackendEmptyStream
	}
	return out, nil
}

// promptError wraps a failed Prompt RPC for the caller.
//
// Two readings are layered on the transport error. The daemon's structured
// account of a failed turn, when it attached one, so classification downstream
// is the same whether the process ran here or on another host (#235). And
// whether the AGENT cut the turn off — it drained, restarted or went away —
// which is the fleet's fault rather than the work's (#1064); see
// agent_interruption.go. An error that is neither is wrapped exactly as before.
func (b *RemoteACPBackend) promptError(ctx context.Context, rpcErr error) error {
	wrapped := wrapRemoteTurnFailure("Prompt", rpcErr)
	wrapped = wrapAgentInterruption(ctx, "Prompt", b.ResolvedHost(), b.ResolvedAgentID(), rpcErr, wrapped)
	return &RemoteBackendRPCError{Op: "Prompt", Err: wrapped}
}

// Infer runs a stateless prompt against a remote daemon's backend. Unlike
// Prompt, Infer does not require a prior Spawn and targets sessionless
// backends (e.g., OllamaBackend). The returned channel delivers InferChunk
// values as the daemon streams them; the channel is closed when the stream
// ends or an error occurs. Callers that want an aggregated response should
// use AccumulateChunks. The error return is non-nil only when the RPC itself
// fails to start; per-stream errors after the channel is opened are
// signalled by the channel closing without a final chunk.
func (b *RemoteACPBackend) Infer(ctx context.Context, req *InferRequest) (<-chan *InferChunk, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.Prompt == "" {
		return nil, &InputValidationError{Field: "prompt", Message: "cannot be empty"}
	}
	// Infer is the one request that carries its own backend name, so it is the
	// one place the routing invariant can be checked rather than only stated:
	// a request dispatched to an instance bound to a different backend is the
	// #937 class recurring, and running it would serve the caller a different
	// model family under the name they asked for. An empty name defers to the
	// instance's binding — the service boundary resolved on it.
	if req.Backend != "" && req.Backend != b.backend {
		return nil, &RemoteBackendMismatchError{Op: "Infer", Bound: b.backend, Requested: req.Backend}
	}

	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}

	b.logger.Debugf("remote_acp_backend: Infer backend=%s tenant=%s model=%s",
		b.backend, b.registration.TenantID, req.Model)

	pbReq := &proto.InferRequest{
		Backend:      b.backend,
		Model:        req.Model,
		Prompt:       req.Prompt,
		SystemPrompt: req.SystemPrompt,
		TenantId:     b.registration.TenantID,
		Metadata:     req.Metadata,
	}

	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return nil, err
	}
	stream, rpcErr := client.Infer(rpcCtx, pbReq)
	if rpcErr != nil {
		return nil, &InferBackendError{Op: "Infer.Start", Err: rpcErr}
	}

	ch := make(chan *InferChunk, 32)
	go func() {
		defer close(ch)
		gotChunk := false
		for {
			frame, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				if !gotChunk {
					b.logger.Warnf("remote_acp_backend: Infer stream closed with no chunks: backend=%s",
						b.backend)
				}
				return
			}
			if recvErr != nil {
				// Context cancellation is a normal exit path; only log real errors.
				if ctx.Err() == nil {
					b.logger.Warnf("remote_acp_backend: Infer stream error: backend=%s err=%v",
						b.backend, recvErr)
				}
				return
			}
			if frame == nil {
				continue
			}
			gotChunk = true
			chunk := &InferChunk{
				Delta:      protoContentBlocksToRuntime(frame.GetDelta()),
				Model:      frame.GetModel(),
				StopReason: frame.GetStopReason(),
				Final:      frame.GetFinal(),
			}
			select {
			case ch <- chunk:
			case <-ctx.Done():
				return
			}
			if frame.GetFinal() {
				return
			}
		}
	}()

	return ch, nil
}

// Status dispatches to the daemon's FleetRuntime.Status.
//
// A DAEMON THAT NO LONGER HOLDS THE SESSION IS AN ANSWER, NOT A FAULT. The
// daemon maps its own SessionNotFoundError onto codes.NotFound
// (pkg/fleet.mapRuntimeError), and wrapping that indiscriminately in
// RemoteBackendRPCError erased the one distinction every liveness check needs:
// the stall detector type-asserts for *SessionNotFoundError to tell "this
// session is gone" from "this session could not be reached", and treated a
// dead remote worker as unreadable forever (go-quicbaas#774). The code is
// translated back into the package's own typed error here, at the seam that
// knows it came off the wire.
func (b *RemoteACPBackend) Status(ctx context.Context, sessionID string) (*SessionStatus, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return nil, err
	}
	resp, rpcErr := client.Status(rpcCtx, &proto.SessionStatusRequest{
		SessionId: sessionID,
		TenantId:  b.registration.TenantID,
	})
	if rpcErr != nil {
		if grpcstatus.Code(rpcErr) == codes.NotFound {
			return nil, &SessionNotFoundError{SessionID: sessionID}
		}
		return nil, &RemoteBackendRPCError{Op: "Status", Err: rpcErr}
	}
	return &SessionStatus{
		SessionID:    resp.GetSessionId(),
		State:        protoSessionStateToRuntime(resp.GetState()),
		LastActivity: timestampAsUnixNano(resp.GetLastActivity()),
		Uptime:       resp.GetUptimeSeconds(),
	}, nil
}

// Compile-time check: a remote backend runs commands in its sessions'
// containers on the daemon that owns them.
var _ SessionContainerExecutor = (*RemoteACPBackend)(nil)

// ExecInSession runs one command in the container of a session the daemon
// holds, for this backend's tenant. The daemon resolves the container from
// the session; a session it does not hold, of another tenant, or with no
// container is refused there.
func (b *RemoteACPBackend) ExecInSession(ctx context.Context, sessionID string, spec *ExecSpec) (*ExecResult, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return nil, err
	}
	resp, rpcErr := client.ExecSession(rpcCtx, &proto.ExecSessionRequest{
		TenantId:  b.registration.TenantID,
		SessionId: sessionID,
		Cmd:       spec.Cmd,
		Env:       spec.Env,
		WorkDir:   spec.WorkDir,
		Stdin:     spec.Stdin,
	})
	if rpcErr != nil {
		switch grpcstatus.Code(rpcErr) {
		case codes.NotFound:
			return nil, &SessionNotFoundError{SessionID: sessionID}
		case codes.FailedPrecondition:
			return nil, &SessionNotContainerizedError{SessionID: sessionID}
		}
		return nil, &RemoteBackendRPCError{Op: "ExecSession", Err: rpcErr}
	}
	// The bytes the command wrote, as the local exec path returns them: no
	// encoding is assumed or repaired.
	return &ExecResult{
		ExitCode: int(resp.GetExitCode()),
		Stdout:   string(resp.GetStdout()),
		Stderr:   string(resp.GetStderr()),
	}, nil
}

// Compile-time check: a remote backend removes the session workspaces its
// daemon holds.
var _ WorkspaceRemover = (*RemoteACPBackend)(nil)

// RemoveWorkspace implements WorkspaceRemover: the daemon removes the
// workspace under its own projects root, with the same confinement and mount
// policy it provisions under. A workspace of another tenant is refused here,
// before the call: this backend speaks for its registration's tenant only.
func (b *RemoteACPBackend) RemoveWorkspace(ctx context.Context, ws *SessionWorkspace) (bool, error) {
	if err := ws.Validate(); err != nil {
		return false, err
	}
	if ws.TenantID != b.registration.TenantID {
		return false, &SessionWorkspaceTenantError{SpawnTenant: b.registration.TenantID, WorkspaceTenant: ws.TenantID}
	}
	client, err := b.client(ctx)
	if err != nil {
		return false, err
	}
	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return false, err
	}
	resp, rpcErr := client.RemoveWorkspace(rpcCtx, &proto.RemoveWorkspaceRequest{
		TenantId:  ws.TenantID,
		ProjectId: ws.ProjectID,
		Key:       ws.Key,
	})
	if rpcErr != nil {
		return false, &RemoteBackendRPCError{Op: "RemoveWorkspace", Err: rpcErr}
	}
	return resp.GetRemoved(), nil
}

// Capabilities reports the superset of capabilities a remote daemon exposes.
// The actual backend on the other side may be ACP or Ollama; we don't
// discover that here and instead advertise the intersection most callers
// rely on.
func (b *RemoteACPBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Kind:               BackendKindAgent,
		PersistentSessions: true,
		ToolUse:            true,
		FileAccess:         true,
		Streaming:          false,
		ACP:                true,
		TeamManagement:     false,
	}
}

// Type returns the backend type identifier.
func (b *RemoteACPBackend) Type() string {
	return "remote_acp"
}

// ToolGuidance returns empty — guidance is injected by the daemon-side
// backend before reaching the model, so the central side has nothing to add.
func (b *RemoteACPBackend) ToolGuidance() string { return "" }

// ListModels proxies the ListModels RPC to the remote daemon and returns the
// model names available on the backend THIS INSTANCE SERVES. A daemon serving
// several backends has a different model catalog per backend, so naming the
// registration's primary here would answer for the wrong one.
//
// Implements orchestration.ModelLister.
func (b *RemoteACPBackend) ListModels(ctx context.Context) ([]string, error) {
	cl, err := b.client(ctx)
	if err != nil {
		return nil, b.routingError("ListModels", &RemoteBackendDialError{Host: b.registration.Host, Err: err})
	}
	rpcCtx, err := b.authCtx(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := cl.ListModels(rpcCtx, &proto.ListModelsRequest{
		Backend:  b.backend,
		TenantId: b.registration.TenantID,
	})
	if err != nil {
		return nil, b.routingError("ListModels", &RemoteBackendRPCError{Op: "ListModels", Err: err})
	}
	if resp.GetModels() == nil {
		return []string{}, nil
	}
	return resp.GetModels(), nil
}

// routingError names the daemon address and backend a failed op was routed
// to, so a gateway can report which node did not answer (go-quicbaas#795).
func (b *RemoteACPBackend) routingError(op string, err error) error {
	return &DaemonRoutingError{Backend: b.backend, Address: b.registration.Host, Op: op, Err: err}
}

// client opens (or reuses) a gRPC connection to the registration's host.
func (b *RemoteACPBackend) client(ctx context.Context) (proto.FleetRuntimeClient, error) {
	conn, err := b.pool.Get(ctx, b.registration.Host)
	if err != nil {
		return nil, err
	}
	return proto.NewFleetRuntimeClient(conn), nil
}

// authCtx decorates the RPC context with tenant metadata and (when
// configured) an OAuth2 bearer token. Both are passed in gRPC metadata
// headers so middleware on either side can validate them. A token that cannot
// be had within ctx is a *RemoteBackendTokenError, and the caller must not send
// the RPC without it.
func (b *RemoteACPBackend) authCtx(ctx context.Context) (context.Context, error) {
	md, err := b.authMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return metadata.NewOutgoingContext(ctx, md), nil
}

// authMetadata builds the metadata every daemon RPC carries, fetching the
// bearer token within ctx. It is separate from authCtx for the signing relay,
// whose stream outlives the context its token is fetched under.
func (b *RemoteACPBackend) authMetadata(ctx context.Context) (metadata.MD, error) {
	md := metadata.New(nil)
	md.Set("x-quicbaas-tenant", b.registration.TenantID)
	md.Set("x-quicbaas-agent-id", b.registration.ID)
	if b.tokenSource == nil {
		return md, nil
	}
	tok, err := b.tokenSource.Token(ctx)
	if err != nil {
		return nil, &RemoteBackendTokenError{Host: b.registration.Host, Err: err}
	}
	if tok == nil || tok.AccessToken == "" {
		return nil, &RemoteBackendTokenError{Host: b.registration.Host, Err: ErrRemoteBackendEmptyToken}
	}
	md.Set("authorization", "Bearer "+tok.AccessToken)
	return md, nil
}

// =============================================================================
// grpcConnPool — connection reuse per host:port
// =============================================================================

type grpcConnPoolConfig struct {
	TLSConfig *tls.Config
	Dialer    func(ctx context.Context, target string) (*grpc.ClientConn, error)
	Logger    interfaces.Logger
}

// grpcConnPool maintains one *grpc.ClientConn per host:port target. Cold
// dials are serialized via a sync.Mutex; hot-path lookups are lock-free
// through sync.Map.
type grpcConnPool struct {
	conns  sync.Map // map[string]*grpc.ClientConn
	mu     sync.Mutex
	cfg    grpcConnPoolConfig
	closed bool
}

func newGRPCConnPool(cfg grpcConnPoolConfig) *grpcConnPool {
	return &grpcConnPool{cfg: cfg}
}

// Get returns a live *grpc.ClientConn for the target, dialing if needed.
func (p *grpcConnPool) Get(ctx context.Context, target string) (*grpc.ClientConn, error) {
	if existing, ok := p.conns.Load(target); ok {
		if conn, ok := existing.(*grpc.ClientConn); ok {
			return conn, nil
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, &RemoteBackendDialError{Host: target, Err: errors.New("pool closed")}
	}
	if existing, ok := p.conns.Load(target); ok {
		if conn, ok := existing.(*grpc.ClientConn); ok {
			return conn, nil
		}
	}
	conn, err := p.dial(ctx, target)
	if err != nil {
		return nil, err
	}
	p.conns.Store(target, conn)
	return conn, nil
}

// Close terminates all pooled connections. Safe to call multiple times.
func (p *grpcConnPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	var firstErr error
	p.conns.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*grpc.ClientConn); ok {
			if err := conn.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		p.conns.Delete(key)
		return true
	})
	return firstErr
}

func (p *grpcConnPool) dial(ctx context.Context, target string) (*grpc.ClientConn, error) {
	if p.cfg.Logger != nil {
		p.cfg.Logger.Debugf("remote_acp_backend: dialing daemon target=%s", target)
	}
	if p.cfg.Dialer != nil {
		conn, err := p.cfg.Dialer(ctx, target)
		if err != nil {
			return nil, &RemoteBackendDialError{Host: target, Err: err}
		}
		return conn, nil
	}
	var opts []grpc.DialOption
	if p.cfg.TLSConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(p.cfg.TLSConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	// A WORKER'S REPLY IS NOT A SMALL MESSAGE. gRPC's default receive limit is
	// 4 MiB; one Prompt reply carrying a 35-minute delivery turn -- the
	// worker's text, its tool calls and the verification output it quoted --
	// measured 16.9 MB on go-quicbaas#62 (2026-09-21) and was refused with
	// ResourceExhausted after the whole turn had been paid for, charged to
	// the work, and re-dispatched into the same wall. The agent side raises
	// its send limit to the same bound (RemoteACPMaxMessageBytes).
	opts = append(opts, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(RemoteACPMaxMessageBytes),
		grpc.MaxCallSendMsgSize(RemoteACPMaxMessageBytes),
	))
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, &RemoteBackendDialError{Host: target, Err: err}
	}
	return conn, nil
}

// RemoteACPMaxMessageBytes bounds one gRPC message between the daemon and an
// fleet node daemon, in both directions. 64 MiB: a turn's reply is text the model
// wrote plus the tool output it quoted, and the largest measured (16.9 MB)
// sits well inside it; a message beyond this is a runaway, not a turn.
const RemoteACPMaxMessageBytes = 64 << 20

// =============================================================================
// Translation helpers
// =============================================================================

// runtimeMCPServersToProto converts an orchestration MCP config slice to its
// proto equivalent.
func runtimeMCPServersToProto(in []MCPServerConfig) []*proto.MCPServerConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]*proto.MCPServerConfig, len(in))
	for i, s := range in {
		// Prefer the explicit URL; fall back to the legacy Command field for
		// callers that encoded the endpoint there before URL existed. Env
		// (e.g. the agent's mTLS cert/key paths) is preserved verbatim.
		url := s.URL
		if url == "" {
			url = s.Command
		}
		out[i] = &proto.MCPServerConfig{
			Name: s.Name,
			Url:  url,
			Env:  s.Env,
			// The launch instruction itself, which a stdio MCP server cannot
			// be started without. Sent verbatim rather than folded into url:
			// the receiving side's url->command fallback exists for senders
			// that predate these fields and must not be what a current sender
			// relies on.
			Command: s.Command,
			Args:    s.Args,
		}
	}
	return out
}

// runtimeMountsToProto converts orchestration container mounts to the proto
// shape. Nil for an empty slice, so a spawn that authorized no extra mount
// sends no repeated field rather than an empty one.
func runtimeMountsToProto(in []ContainerMount) []*proto.ContainerMount {
	if len(in) == 0 {
		return nil
	}
	out := make([]*proto.ContainerMount, len(in))
	for i, m := range in {
		out[i] = &proto.ContainerMount{
			Source:         m.Source,
			Target:         m.Target,
			ReadOnly:       m.ReadOnly,
			Type:           m.Type,
			TmpfsSizeBytes: m.TmpfsSizeBytes,
		}
	}
	return out
}

// runtimeStorageToProto converts a resolved workspace storage plan to the wire
// shape. An inert plan converts to nil, so a spawn for a project with no
// storage configuration puts no field on the wire at all.
func runtimeStorageToProto(plan WorkspaceStoragePlan) *proto.WorkspaceStoragePlan {
	if plan.Inert() {
		return nil
	}
	return &proto.WorkspaceStoragePlan{
		Tier:           string(plan.Tier),
		TmpfsSizeBytes: plan.TmpfsSizeBytes,
		MemoryBytes:    plan.MemoryBytes,
	}
}

// protoContentBlocksToRuntime converts proto content blocks to the
// orchestration shape.
func protoContentBlocksToRuntime(in []*proto.ContentBlock) []ContentBlock {
	if len(in) == 0 {
		return nil
	}
	out := make([]ContentBlock, len(in))
	for i, b := range in {
		out[i] = ContentBlock{
			Type:      b.GetType(),
			Text:      b.GetText(),
			ToolName:  b.GetToolName(),
			ToolUseID: b.GetToolUseId(),
			IsError:   b.GetIsError(),
			Bytes:     int(b.GetBytes()),
		}
	}
	return out
}

// protoSessionStateMap dispatches proto SessionState -> runtime SessionState
// in O(1).
var protoSessionStateMap = map[proto.SessionState]SessionState{
	proto.SessionState_SESSION_STATE_ACTIVE:    SessionStateActive,
	proto.SessionState_SESSION_STATE_IDLE:      SessionStateIdle,
	proto.SessionState_SESSION_STATE_CLOSED:    SessionStateClosed,
	proto.SessionState_SESSION_STATE_ERROR:     SessionStateError,
	proto.SessionState_SESSION_STATE_RESUMABLE: SessionStateResumable,
}

func protoSessionStateToRuntime(s proto.SessionState) SessionState {
	if v, ok := protoSessionStateMap[s]; ok {
		return v
	}
	return ""
}

// timestampAsUnixNano extracts nanoseconds-since-epoch from a proto timestamp,
// returning 0 for nil inputs.
//
// NANOSECONDS, NOT SECONDS. Every session instant in this package — a
// Session's CreatedAt, a SessionStatus's LastActivity, a SessionRecord's
// pair of both — is Unix nanoseconds; see the unit contract on
// orchestration.Session. This helper returned seconds, so a remote daemon's
// Status answer reached a reader that decoded it with time.Unix(0, ...) as
// an instant a second after the epoch, and the session read as idle since
// 1970 (go-quicbaas#855).
func timestampAsUnixNano(ts interface{ AsTime() time.Time }) int64 {
	if ts == nil {
		return 0
	}
	t := ts.AsTime()
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// =============================================================================
// oauth/credentials compile marker
// =============================================================================

// _ is a compile marker to ensure the oauth credentials dependency is
// tracked — it's used by future JWT interceptors the deployment wires in.
var _ = oauth.TokenSource{}

// sessionWorkspaceToProto is the wire form of a named workspace, nil for a
// spawn that named none. The pinned directory travels only when the pin is for
// this very workspace: a pin naming another key of the job places the spawn on
// the right host but says nothing about where this workspace lives on it.
func sessionWorkspaceToProto(ws *SessionWorkspace) *proto.SessionWorkspace {
	if ws == nil {
		return nil
	}
	out := &proto.SessionWorkspace{
		TenantId:  ws.TenantID,
		ProjectId: ws.ProjectID,
		Key:       ws.Key,
	}
	if ws.Pin != nil && ws.Pin.Key == ws.Key {
		out.PinnedDir = ws.Pin.Dir
	}
	if ws.Spec != nil {
		out.SpecFiles = make(map[string][]byte, len(ws.Spec.Files))
		for name, content := range ws.Spec.Files {
			out.SpecFiles[name] = content
		}
	}
	return out
}

// auditPoolExhaustion records a spawn the daemon refused because every pooled
// account of the provider was saturated or cooling down. Admission does not
// cover this: it asks about the whole pool, so another provider's free account
// admits a project whose requested pool is exhausted.
func (b *RemoteACPBackend) auditPoolExhaustion(ctx context.Context, req *SpawnRequest, spawnErr error) {
	if b.auditor == nil {
		return
	}
	exhaustion, ok := exhaustionFromResolveError(AccountResolveRequest{
		TenantID:  b.registration.TenantID,
		ProjectID: req.ProjectID,
		Provider:  AccountProvider(b.backend),
	}, spawnErr)
	if ok {
		auditAccountPoolExhausted(ctx, b.auditor, exhaustion)
	}
}
