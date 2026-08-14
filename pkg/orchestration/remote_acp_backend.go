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
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/metadata"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Remote ACP Backend — talks to a pkg/agent daemon over AgentRuntime gRPC
// =============================================================================

// Remote backend sentinel errors.
var (
	// ErrRemoteBackendNoRegistration is returned when construction is attempted
	// without a non-nil AgentRegistration.
	ErrRemoteBackendNoRegistration = errors.New("remote_acp_backend: registration is required")

	// ErrRemoteBackendNoLogger is returned when construction is attempted
	// without a logger.
	ErrRemoteBackendNoLogger = errors.New("remote_acp_backend: logger is required")

	// ErrRemoteBackendNoHost is returned when the registration's Host field
	// is empty.
	ErrRemoteBackendNoHost = errors.New("remote_acp_backend: registration host is required")

	// ErrRemoteBackendNotDaemon is returned when the registration does not
	// resolve to AgentKindDaemon. Only daemon-kind registrations are eligible
	// for remote routing.
	ErrRemoteBackendNotDaemon = errors.New("remote_acp_backend: registration is not a daemon")

	// ErrRemoteBackendEmptyStream is returned when the daemon closes the
	// prompt stream without sending any response frame.
	ErrRemoteBackendEmptyStream = errors.New("remote_acp_backend: daemon closed prompt stream without response")

	// ErrRemoteBackendSessionsUnsupported is returned when a caller asks for
	// behavior incompatible with the remote session contract (e.g., status
	// lookups on a backend that doesn't support sessions).
	ErrRemoteBackendSessionsUnsupported = errors.New("remote_acp_backend: remote backend does not support this operation")
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

// RemoteACPBackendDeps injects production dependencies into
// NewRemoteACPBackend. Fields that support "nil for tests" are annotated as
// such; production callers populate everything.
type RemoteACPBackendDeps struct {
	// Registration identifies the daemon to connect to. Registration.Host is
	// expected to be a dialable "host:port" string; Registration.Backend is
	// forwarded to the daemon in each RPC.
	Registration *AgentRegistration

	// TokenSource supplies OAuth2 bearer tokens for daemon RPCs. May be nil
	// in tests that use ConnDialer to establish insecure connections.
	TokenSource oauth2.TokenSource

	// ClientTLSConfig is used for mTLS to the daemon. When nil and ConnDialer
	// is also nil, the backend dials plaintext — only safe for tests.
	ClientTLSConfig *tls.Config

	// ConnDialer, when non-nil, replaces the default grpc.NewClient path.
	// Intended for bufconn-backed tests. When set, the backend does not
	// configure credentials itself (the dialer is expected to).
	ConnDialer func(ctx context.Context, target string) (*grpc.ClientConn, error)

	// Logger is used for lifecycle and per-call diagnostics. Required.
	Logger interfaces.Logger
}

// RemoteACPBackend implements AgentBackend by forwarding calls to a remote
// pkg/agent daemon over the AgentRuntime gRPC service. One backend instance
// is scoped to a single AgentRegistration; callers route to the right
// registration via the BackendDispatcher's remote resolver.
type RemoteACPBackend struct {
	registration *AgentRegistration
	pool         *grpcConnPool
	tokenSource  oauth2.TokenSource
	logger       interfaces.Logger
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
	if kind != AgentKindDaemon && kind != AgentKindEmbedded {
		return nil, ErrRemoteBackendNotDaemon
	}

	pool := newGRPCConnPool(grpcConnPoolConfig{
		TLSConfig: deps.ClientTLSConfig,
		Dialer:    deps.ConnDialer,
		Logger:    deps.Logger,
	})

	return &RemoteACPBackend{
		registration: deps.Registration,
		pool:         pool,
		tokenSource:  deps.TokenSource,
		logger:       deps.Logger,
	}, nil
}

// Registration returns the backend's bound AgentRegistration. Exposed for
// diagnostics and for the BackendDispatcher's routing decisions.
func (b *RemoteACPBackend) Registration() *AgentRegistration {
	return b.registration
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

// Close terminates a remote session by delegating to the daemon's
// AgentRuntime.Close. Pool-level teardown is a separate operation — see
// CloseAll.
func (b *RemoteACPBackend) Close(ctx context.Context, sessionID string) error {
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	_, rpcErr := client.Close(b.authCtx(ctx), &proto.CloseSessionRequest{
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

// Spawn dispatches to the daemon's AgentRuntime.Spawn.
func (b *RemoteACPBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	b.logger.Debugf("remote_acp_backend: Spawn backend=%s tenant=%s project=%s",
		b.registration.Backend, b.registration.TenantID, req.ProjectID)
	pbReq := &proto.SpawnSessionRequest{
		Backend:        b.registration.Backend,
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
	}
	resp, rpcErr := client.Spawn(b.authCtx(ctx), pbReq)
	if rpcErr != nil {
		return nil, &RemoteBackendRPCError{Op: "Spawn", Err: rpcErr}
	}
	createdAt := time.Now().UnixNano()
	if resp.GetTimestamp() != nil {
		createdAt = resp.GetTimestamp().AsTime().UnixNano()
	}
	return &Session{
		ID:        resp.GetSessionId(),
		ProjectID: req.ProjectID,
		Backend:   b.registration.Backend,
		Model:     resp.GetModel(),
		Status:    SessionStateActive,
		WorkDir:   req.WorkDir,
		CreatedAt: createdAt,
	}, nil
}

// Prompt dispatches to the daemon's AgentRuntime.Prompt and accumulates the
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
		b.registration.Backend, b.registration.TenantID, req.SessionID)
	stream, rpcErr := client.Prompt(b.authCtx(ctx), &proto.PromptSessionRequest{
		Backend:      b.registration.Backend,
		SessionId:    req.SessionID,
		ProjectId:    req.ProjectID,
		Prompt:       req.Prompt,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
		TenantId:     b.registration.TenantID,
	})
	if rpcErr != nil {
		return nil, &RemoteBackendRPCError{Op: "Prompt", Err: rpcErr}
	}

	out := &PromptResponse{}
	gotFrame := false
	for {
		frame, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, &RemoteBackendRPCError{Op: "Prompt", Err: recvErr}
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
		out.Content = append(out.Content, protoContentBlocksToRuntime(frame.GetContent())...)
		if req.EventCallback != nil {
			for _, block := range frame.GetContent() {
				if block.GetText() == "" {
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

	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}

	b.logger.Debugf("remote_acp_backend: Infer backend=%s tenant=%s model=%s",
		b.registration.Backend, b.registration.TenantID, req.Model)

	pbReq := &proto.InferRequest{
		Backend:      b.registration.Backend,
		Model:        req.Model,
		Prompt:       req.Prompt,
		SystemPrompt: req.SystemPrompt,
		TenantId:     b.registration.TenantID,
		Metadata:     req.Metadata,
	}

	stream, rpcErr := client.Infer(b.authCtx(ctx), pbReq)
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
						b.registration.Backend)
				}
				return
			}
			if recvErr != nil {
				// Context cancellation is a normal exit path; only log real errors.
				if ctx.Err() == nil {
					b.logger.Warnf("remote_acp_backend: Infer stream error: backend=%s err=%v",
						b.registration.Backend, recvErr)
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

// Status dispatches to the daemon's AgentRuntime.Status.
func (b *RemoteACPBackend) Status(ctx context.Context, sessionID string) (*SessionStatus, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	resp, rpcErr := client.Status(b.authCtx(ctx), &proto.SessionStatusRequest{
		SessionId: sessionID,
		TenantId:  b.registration.TenantID,
	})
	if rpcErr != nil {
		return nil, &RemoteBackendRPCError{Op: "Status", Err: rpcErr}
	}
	return &SessionStatus{
		SessionID:    resp.GetSessionId(),
		State:        protoSessionStateToRuntime(resp.GetState()),
		LastActivity: timestampAsUnix(resp.GetLastActivity()),
		Uptime:       resp.GetUptimeSeconds(),
	}, nil
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
// model names available on the named backend. The backend name is taken from
// the registration so each RemoteACPBackend instance routes to the correct
// daemon backend.
//
// Implements orchestration.ModelLister.
func (b *RemoteACPBackend) ListModels(ctx context.Context) ([]string, error) {
	cl, err := b.client(ctx)
	if err != nil {
		return nil, &RemoteBackendDialError{Host: b.registration.Host, Err: err}
	}
	resp, err := cl.ListModels(b.authCtx(ctx), &proto.ListModelsRequest{
		Backend:  b.registration.Backend,
		TenantId: b.registration.TenantID,
	})
	if err != nil {
		return nil, &RemoteBackendRPCError{Op: "ListModels", Err: err}
	}
	if resp.GetModels() == nil {
		return []string{}, nil
	}
	return resp.GetModels(), nil
}

// client opens (or reuses) a gRPC connection to the registration's host.
func (b *RemoteACPBackend) client(ctx context.Context) (proto.AgentRuntimeClient, error) {
	conn, err := b.pool.Get(ctx, b.registration.Host)
	if err != nil {
		return nil, err
	}
	return proto.NewAgentRuntimeClient(conn), nil
}

// authCtx decorates the RPC context with tenant metadata and (when
// configured) an OAuth2 bearer token. Both are passed in gRPC metadata
// headers so middleware on either side can validate them.
func (b *RemoteACPBackend) authCtx(ctx context.Context) context.Context {
	md := metadata.New(nil)
	md.Set("x-quicbaas-tenant", b.registration.TenantID)
	md.Set("x-quicbaas-agent-id", b.registration.ID)
	if b.tokenSource != nil {
		tok, err := b.tokenSource.Token()
		if err == nil && tok != nil && tok.AccessToken != "" {
			md.Set("authorization", "Bearer "+tok.AccessToken)
		}
	}
	return metadata.NewOutgoingContext(ctx, md)
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
		conn := existing.(*grpc.ClientConn)
		return conn, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, &RemoteBackendDialError{Host: target, Err: errors.New("pool closed")}
	}
	if existing, ok := p.conns.Load(target); ok {
		return existing.(*grpc.ClientConn), nil
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
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, &RemoteBackendDialError{Host: target, Err: err}
	}
	return conn, nil
}

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
		}
	}
	return out
}

// protoContentBlocksToRuntime converts proto content blocks to the
// orchestration shape.
func protoContentBlocksToRuntime(in []*proto.ContentBlock) []ContentBlock {
	if len(in) == 0 {
		return nil
	}
	out := make([]ContentBlock, len(in))
	for i, b := range in {
		out[i] = ContentBlock{Type: b.GetType(), Text: b.GetText()}
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

// timestampAsUnix extracts seconds-since-epoch from a proto timestamp,
// returning 0 for nil inputs.
func timestampAsUnix(ts interface{ AsTime() time.Time }) int64 {
	if ts == nil {
		return 0
	}
	t := ts.AsTime()
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// =============================================================================
// oauth/credentials compile marker
// =============================================================================

// _ is a compile marker to ensure the oauth credentials dependency is
// tracked — it's used by future JWT interceptors the deployment wires in.
var _ = oauth.TokenSource{}
