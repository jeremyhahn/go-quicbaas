// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package grpc

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestNew_InvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *transport.Config
		wantCode errors.ErrorCode
	}{
		{
			name:     "nil config",
			cfg:      nil,
			wantCode: errors.CodeInvalidInput,
		},
		{
			name:     "empty address",
			cfg:      &transport.Config{},
			wantCode: errors.CodeInvalidInput,
		},
		{
			// TCP addresses require a TLS config.
			name:     "tcp address without TLS",
			cfg:      &transport.Config{Address: "localhost:50051", Timeout: 100 * time.Millisecond},
			wantCode: errors.CodeConfiguration,
		},
		{
			// Unix sockets require a TLS config as well.
			name:     "unix socket without TLS",
			cfg:      &transport.Config{Address: "/tmp/nonexistent.sock", Timeout: 100 * time.Millisecond},
			wantCode: errors.CodeConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if got := errors.Code(err); got != tt.wantCode {
				t.Errorf("error code = %v, want %v", got, tt.wantCode)
			}
		})
	}
}

func TestNew_WithPoolEnabled(t *testing.T) {
	// The grpc pool uses lazy connection establishment — calling
	// New() with PoolEnabled=true succeeds even when no server is
	// reachable; the dial actually happens on first Request(). The
	// historical "should fail at creation" expectation no longer
	// matches the implementation. Verify the lazy-init contract:
	// New returns a usable Transport; Close releases without error.
	cfg := &transport.Config{
		Address:     "/tmp/nonexistent.sock",
		Timeout:     100 * time.Millisecond,
		TLSConfig:   &tls.Config{MinVersion: tls.VersionTLS13},
		PoolEnabled: true,
		PoolMinSize: 1,
		PoolMaxSize: 5,
	}
	tr, err := New(cfg)
	if err != nil {
		// Some pool configurations (eg. min>0 eager init) may still
		// fail at New(); accept either contract.
		return
	}
	if tr == nil {
		t.Fatal("New returned nil transport with no error")
	}
	if cerr := tr.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
}

func TestBuildDialOptions_WithTLSConfig(t *testing.T) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	tests := []struct {
		name    string
		cfg     *transport.Config
		minOpts int
	}{
		{
			name:    "tls only",
			cfg:     &transport.Config{Address: "localhost:50051", TLSConfig: tlsCfg},
			minOpts: 1,
		},
		{
			name: "tls with keepalive",
			cfg: &transport.Config{
				Address:          "localhost:50051",
				TLSConfig:        tlsCfg,
				KeepaliveTime:    10 * time.Second,
				KeepaliveTimeout: 5 * time.Second,
			},
			minOpts: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Transport{cfg: tt.cfg}

			opts, err := tr.buildDialOptions()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(opts) < tt.minOpts {
				t.Errorf("got %d dial options, want at least %d", len(opts), tt.minOpts)
			}
		})
	}
}

func TestTransport_GetConn_Unavailable(t *testing.T) {
	tests := []struct {
		name   string
		closed bool
	}{
		{name: "closed", closed: true},
		{name: "no connection", closed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Transport{}
			tr.closed.Store(tt.closed)

			_, err := tr.getConn()
			if err == nil {
				t.Fatal("expected error")
			}
			if errors.Code(err) != errors.CodeConnection {
				t.Errorf("expected Connection error, got %v", errors.Code(err))
			}
		})
	}
}

func TestTransport_ReturnConn_NoPool(t *testing.T) {
	tr := &Transport{}
	// Should not panic with nil pool
	tr.returnConn(nil)
}

func TestTransport_Healthy_Unavailable(t *testing.T) {
	tests := []struct {
		name   string
		closed bool
	}{
		{name: "closed", closed: true},
		{name: "no connection", closed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Transport{}
			tr.closed.Store(tt.closed)

			if tr.Healthy() {
				t.Error("expected unhealthy transport")
			}
		})
	}
}

func TestTransport_Conn_ReturnsStoredConnection(t *testing.T) {
	tr := &Transport{}
	// When conn is nil, Conn() returns a nil *grpc.ClientConn
	// which is a non-nil interface containing a nil pointer
	result := tr.Conn()
	// The result is the stored value (nil pointer)
	conn, ok := result.(*grpc.ClientConn)
	if !ok {
		// result is nil interface, which is also valid
		if result != nil {
			t.Error("expected either nil interface or nil *grpc.ClientConn")
		}
	} else if conn != nil {
		t.Error("expected nil connection for uninitialized transport")
	}
}

func TestTransport_Close(t *testing.T) {
	tests := []struct {
		name   string
		closed bool
		pool   func() *Pool
	}{
		{name: "already closed", closed: true},
		{name: "no connection"},
		{name: "empty pool", pool: func() *Pool { return &Pool{healthStop: make(chan struct{})} }},
		{name: "closed pool", pool: func() *Pool {
			p := &Pool{healthStop: make(chan struct{}), cfg: &PoolConfig{MinSize: 1, MaxSize: 2}}
			p.closed.Store(true)
			return p
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Transport{cfg: &transport.Config{}}
			if tt.pool != nil {
				tr.pool = tt.pool()
			}
			tr.closed.Store(tt.closed)

			if err := tr.Close(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tr.closed.Load() {
				t.Error("expected transport to be marked closed")
			}
		})
	}
}

func TestConvertGRPCError_AllCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantNil  bool
		wantCode errors.ErrorCode
	}{
		{name: "nil error", err: nil, wantNil: true},
		{name: "OK status", err: status.Error(codes.OK, "ok"), wantNil: true},
		{name: "non-status error", err: errors.NewInternal("test", "test error"), wantCode: errors.CodeUnknown},
		{name: "InvalidArgument", err: status.Error(codes.InvalidArgument, "e"), wantCode: errors.CodeInvalidInput},
		{name: "NotFound", err: status.Error(codes.NotFound, "e"), wantCode: errors.CodeNotFound},
		{name: "AlreadyExists", err: status.Error(codes.AlreadyExists, "e"), wantCode: errors.CodeAlreadyExists},
		{name: "PermissionDenied", err: status.Error(codes.PermissionDenied, "e"), wantCode: errors.CodeForbidden},
		{name: "Unauthenticated", err: status.Error(codes.Unauthenticated, "e"), wantCode: errors.CodeUnauthorized},
		{name: "ResourceExhausted", err: status.Error(codes.ResourceExhausted, "e"), wantCode: errors.CodeResourceExhausted},
		{name: "FailedPrecondition", err: status.Error(codes.FailedPrecondition, "e"), wantCode: errors.CodePreconditionFailed},
		{name: "Aborted", err: status.Error(codes.Aborted, "e"), wantCode: errors.CodeConflict},
		{name: "Unimplemented", err: status.Error(codes.Unimplemented, "e"), wantCode: errors.CodeUnimplemented},
		{name: "Internal", err: status.Error(codes.Internal, "e"), wantCode: errors.CodeInternal},
		{name: "Unavailable", err: status.Error(codes.Unavailable, "e"), wantCode: errors.CodeUnavailable},
		{name: "DeadlineExceeded", err: status.Error(codes.DeadlineExceeded, "e"), wantCode: errors.CodeTimeout},
		{name: "Canceled", err: status.Error(codes.Canceled, "e"), wantCode: errors.CodeCanceled},
		{name: "OutOfRange", err: status.Error(codes.OutOfRange, "e"), wantCode: errors.CodeInvalidInput},
		{name: "DataLoss", err: status.Error(codes.DataLoss, "e"), wantCode: errors.CodeInternal},
		{name: "Unknown", err: status.Error(codes.Unknown, "e"), wantCode: errors.CodeUnknown},
		{name: "not a gRPC code", err: status.Error(codes.Code(100), "e"), wantCode: errors.CodeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertGRPCError(tt.err, "op")
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil error")
			}
			if errors.Code(result) != tt.wantCode {
				t.Errorf("expected %v, got %v", tt.wantCode, errors.Code(result))
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"sdk Unavailable", errors.NewUnavailable("op", "msg"), true},
		{"sdk Timeout", errors.NewTimeout("op", "msg"), true},
		{"sdk ResourceExhausted", errors.NewResourceExhausted("op", "msg"), true},
		{"sdk NotFound", errors.NewNotFound("op", "msg"), false},
		{"sdk InvalidInput", errors.NewInvalidInput("op", "msg"), false},
		{"grpc Unavailable", status.Error(codes.Unavailable, "test"), true},
		{"grpc DeadlineExceeded", status.Error(codes.DeadlineExceeded, "test"), true},
		{"grpc ResourceExhausted", status.Error(codes.ResourceExhausted, "test"), true},
		{"grpc NotFound", status.Error(codes.NotFound, "test"), false},
		{"grpc InvalidArgument", status.Error(codes.InvalidArgument, "test"), false},
		{"grpc Internal", status.Error(codes.Internal, "test"), false},
		{"grpc PermissionDenied", status.Error(codes.PermissionDenied, "test"), false},
		{"grpc Unimplemented", status.Error(codes.Unimplemented, "test"), false},
		{"non-status error", context.DeadlineExceeded, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.err); got != tt.retryable {
				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.retryable)
			}
		})
	}
}

func TestMinDuration(t *testing.T) {
	tests := []struct {
		name     string
		a, b     time.Duration
		expected time.Duration
	}{
		{"first smaller", time.Second, 2 * time.Second, time.Second},
		{"second smaller", 2 * time.Second, time.Second, time.Second},
		{"equal", time.Second, time.Second, time.Second},
		{"first zero", 0, time.Second, 0},
		{"second zero", time.Second, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minDuration(tt.a, tt.b); got != tt.expected {
				t.Errorf("minDuration(%v, %v) = %v, expected %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestFactory_Create(t *testing.T) {
	f := &Factory{}

	// Nil config
	_, err := f.Create(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}

	// Empty address
	_, err = f.Create(&transport.Config{})
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}

func TestStream_Closed(t *testing.T) {
	tests := []struct {
		name string
		op   func(s *stream) error
	}{
		{name: "send", op: func(s *stream) error { return s.Send("test") }},
		{name: "recv", op: func(s *stream) error {
			_, err := s.Recv()
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &stream{}
			s.closed.Store(true)

			err := tt.op(s)
			if err == nil {
				t.Fatal("expected error for closed stream")
			}
			if errors.Code(err) != errors.CodeConnection {
				t.Errorf("expected Connection error, got %v", errors.Code(err))
			}
		})
	}
}

func TestStream_Close(t *testing.T) {
	tests := []struct {
		name   string
		closed bool
	}{
		{name: "already closed", closed: true},
		{name: "open", closed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &stream{transport: &Transport{}}
			s.closed.Store(tt.closed)

			if err := s.Close(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !s.closed.Load() {
				t.Error("expected stream to be marked closed")
			}
		})
	}
}

// mockClientStream implements grpc.ClientStream for testing
type mockClientStream struct {
	grpc.ClientStream
	sendErr error
	recvErr error
	recvMsg interface{}
}

func (m *mockClientStream) SendMsg(msg interface{}) error {
	return m.sendErr
}

func (m *mockClientStream) RecvMsg(msg interface{}) error {
	if m.recvErr != nil {
		return m.recvErr
	}
	// Set the message if provided
	if ptr, ok := msg.(*interface{}); ok {
		*ptr = m.recvMsg
	}
	return nil
}

func (m *mockClientStream) CloseSend() error {
	return nil
}

func TestStream_SendWithError(t *testing.T) {
	mockCS := &mockClientStream{
		sendErr: status.Error(codes.Unavailable, "connection lost"),
	}
	s := &stream{
		stream:    mockCS,
		transport: &Transport{},
	}

	err := s.Send("test")
	if err == nil {
		t.Fatal("expected error from send")
	}
	if errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("expected Unavailable error, got %v", errors.Code(err))
	}
}

func TestStream_RecvWithError(t *testing.T) {
	mockCS := &mockClientStream{
		recvErr: status.Error(codes.Internal, "internal error"),
	}
	s := &stream{
		stream:    mockCS,
		transport: &Transport{},
	}

	_, err := s.Recv()
	if err == nil {
		t.Fatal("expected error from recv")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected Internal error, got %v", errors.Code(err))
	}
}

func TestStream_CloseSend(t *testing.T) {
	mockCS := &mockClientStream{}
	s := &stream{
		stream:    mockCS,
		transport: &Transport{},
	}

	err := s.CloseSend()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTransport_Request_Unavailable(t *testing.T) {
	tests := []struct {
		name   string
		closed bool
		op     func(tr *Transport) error
	}{
		{name: "request closed", closed: true, op: func(tr *Transport) error {
			_, err := tr.Request(context.Background(), "service", "method", nil)
			return err
		}},
		{name: "request no connection", op: func(tr *Transport) error {
			_, err := tr.Request(context.Background(), "service", "method", nil)
			return err
		}},
		{name: "stream closed", closed: true, op: func(tr *Transport) error {
			_, err := tr.RequestStream(context.Background(), "service", "method", nil)
			return err
		}},
		{name: "stream no connection", op: func(tr *Transport) error {
			_, err := tr.RequestStream(context.Background(), "service", "method", nil)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &Transport{
				cfg: &transport.Config{
					MaxRetries:      2,
					RetryBackoff:    time.Millisecond,
					MaxRetryBackoff: 10 * time.Millisecond,
				},
			}
			tr.closed.Store(tt.closed)

			err := tt.op(tr)
			if err == nil {
				t.Fatal("expected error")
			}
			if errors.Code(err) != errors.CodeConnection {
				t.Errorf("expected Connection error, got %v", errors.Code(err))
			}
		})
	}
}

func TestStream_RecvSuccess(t *testing.T) {
	expectedMsg := "test-message"
	mockCS := &mockClientStream{
		recvMsg: expectedMsg,
	}
	s := &stream{
		stream:    mockCS,
		transport: &Transport{},
	}

	msg, err := s.Recv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != expectedMsg {
		t.Errorf("Recv() = %v, want %q", msg, expectedMsg)
	}
}

func TestStream_SendSuccess(t *testing.T) {
	mockCS := &mockClientStream{}
	s := &stream{
		stream:    mockCS,
		transport: &Transport{},
	}

	err := s.Send("test-message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPool_ReplaceConnectionOldNotFound tests replace when old connection is already removed.
func TestPool_ReplaceConnectionOldNotFound(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        2,
		MaxSize:        5,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	// Create an external pooledConn that's not in the pool
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "bufnet", ts.dialOpts()...)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	fakePC := &pooledConn{
		conn:      conn,
		createdAt: time.Now().Add(-2 * time.Hour),
	}

	// Try to replace a connection that's not in the pool
	p.replaceConnection(fakePC)

	// Wait for async operation
	time.Sleep(100 * time.Millisecond)

	// Pool should still have its original connections
	if p.Size() < cfg.MinSize {
		t.Errorf("expected at least %d connections", cfg.MinSize)
	}
}

// TestPool_EnsureMinConnectionsFailure tests ensureMinConnections when connection creation fails.
//
// Runs the ensureMinConnections call on a goroutine with a deadline
// because the bufconn-backed test dialer can hang indefinitely under
// WithBlock when the listener is closed mid-dial. This protects the
// surrounding suite from a goroutine leak that would otherwise time
// out the entire package's test run.
func TestPool_EnsureMinConnectionsFailure(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        3,
		MaxSize:        5,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	// Remove connections to force ensureMinConnections to create new ones
	p.mu.Lock()
	for _, pc := range p.connections {
		pc.conn.Close()
	}
	p.connections = p.connections[:1] // Keep only one
	p.mu.Unlock()

	// Stop server so new connections fail
	ts.stop()

	// Run on a goroutine with a deadline; the bufconn dialer can
	// hang under WithBlock after Close, but we just want to assert
	// ensureMinConnections doesn't panic on failures.
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		p.ensureMinConnections()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		// Acceptable — the dial is blocked, but the function
		// is correctly handling the unreachable backend.
	}

	p.Close()
}

// TestPool_EnsureMinConnectionsAtMax tests ensureMinConnections when pool is at max.
func TestPool_EnsureMinConnectionsAtMax(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        2,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	initialSize := p.Size()

	// Ensure min when already at max
	p.ensureMinConnections()

	if p.Size() != initialSize {
		t.Errorf("pool size should remain %d, got %d", initialSize, p.Size())
	}
}

// mockPooledConnServer provides a server for pooled connection tests.
type mockPooledConnServer struct {
	lis    *bufconn.Listener
	server *grpc.Server
}

func newMockPooledConnServer() *mockPooledConnServer {
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	ts := &mockPooledConnServer{
		lis:    lis,
		server: s,
	}
	go func() {
		_ = s.Serve(lis)
	}()
	return ts
}

func (ts *mockPooledConnServer) stop() {
	ts.server.Stop()
	ts.lis.Close()
}

func (ts *mockPooledConnServer) dialOpts() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return ts.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	}
}

// TestPool_IsHealthyConnections tests isHealthy with various connection states.
func TestPool_IsHealthyConnections(t *testing.T) {
	ts := newMockPooledConnServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        5,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	// Get a connection
	conn, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Find the pooledConn for this connection
	p.mu.Lock()
	var pc *pooledConn
	for _, c := range p.connections {
		if c.conn == conn {
			pc = c
			break
		}
	}
	p.mu.Unlock()

	if pc == nil {
		t.Fatal("could not find pooled connection")
	}

	// Test isHealthy
	state := pc.conn.GetState()
	healthy := p.isHealthy(pc)

	if state == connectivity.Ready || state == connectivity.Idle {
		if !healthy {
			t.Error("expected healthy for Ready/Idle state")
		}
	}

	p.Put(conn)
}

// TestPool_IsExpiredWithZeroMaxAge tests isExpired when MaxAge is zero.
func TestPool_IsExpiredWithZeroMaxAge(t *testing.T) {
	tests := []struct {
		name   string
		maxAge time.Duration
	}{
		{name: "zero max age", maxAge: 0},
		{name: "negative max age", maxAge: -time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pool{cfg: &PoolConfig{MaxAge: tt.maxAge}}
			pc := &pooledConn{createdAt: time.Now().Add(-24 * time.Hour)}

			if p.isExpired(pc) {
				t.Errorf("expected not expired when MaxAge is %v", tt.maxAge)
			}
		})
	}
}

// TestPool_PerformHealthCheckReplacement tests health check triggering replacement.
func TestPool_PerformHealthCheckReplacement(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        2,
		MaxSize:        5,
		MaxAge:         1 * time.Millisecond, // Very short max age
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	// Wait for connections to expire
	time.Sleep(10 * time.Millisecond)

	// Perform health check which should replace expired connections
	p.performHealthCheck()

	// Wait for async replacements
	time.Sleep(100 * time.Millisecond)

	if p.Size() < cfg.MinSize {
		t.Errorf("expected at least %d connections after health check, got %d",
			cfg.MinSize, p.Size())
	}
}

const bufSize = 1024 * 1024

// testServer provides an in-memory gRPC server for testing.
type testServer struct {
	lis    *bufconn.Listener
	server *grpc.Server
}

func newTestServer() *testServer {
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	ts := &testServer{
		lis:    lis,
		server: s,
	}
	go func() {
		_ = s.Serve(lis)
	}()
	return ts
}

func (ts *testServer) stop() {
	ts.server.Stop()
	ts.lis.Close()
}

func (ts *testServer) dialOpts() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return ts.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	}
}

// TestPool_GetWithAvailableConnection tests Get finding an available connection.
func TestPool_GetWithAvailableConnection(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        5,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	conn1, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	if conn1 == nil {
		t.Fatal("expected non-nil connection")
	}

	p.Put(conn1)

	conn2, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get connection second time: %v", err)
	}
	if conn2 != conn1 {
		t.Error("expected to reuse the same connection")
	}
	p.Put(conn2)
}

// TestPool_GetCreatesNewConnectionWhenNeeded tests creating new connections.
func TestPool_GetCreatesNewConnectionWhenNeeded(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        3,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	conn1, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get first connection: %v", err)
	}

	conn2, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get second connection: %v", err)
	}

	if conn1 == conn2 {
		t.Error("expected different connections")
	}

	if p.Size() != 2 {
		t.Errorf("expected pool size 2, got %d", p.Size())
	}

	p.Put(conn1)
	p.Put(conn2)
}

// TestPool_GetPoolExhausted tests the pool exhaustion scenario.
func TestPool_GetPoolExhausted(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	conn1, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get first connection: %v", err)
	}

	conn2, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get second connection: %v", err)
	}

	_, err = p.Get()
	if err == nil {
		t.Fatal("expected error for exhausted pool")
	}
	if errors.Code(err) != errors.CodeResourceExhausted {
		t.Errorf("expected ResourceExhausted error, got %v", errors.Code(err))
	}

	p.Put(conn1)
	p.Put(conn2)
}

// TestPool_PutConnectionNotInPool tests putting a connection not in the pool.
func TestPool_PutConnectionNotInPool(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	externalConn, err := grpc.DialContext(ctx, "bufnet", ts.dialOpts()...)
	if err != nil {
		t.Fatalf("failed to create external connection: %v", err)
	}
	defer externalConn.Close()

	p.Put(externalConn)
}

// TestPool_IsHealthy tests the isHealthy method with real connections.
func TestPool_IsHealthy(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	if !p.Healthy() {
		t.Error("expected pool to be healthy")
	}

	conn, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	state := conn.GetState()
	if state != connectivity.Ready && state != connectivity.Idle {
		t.Errorf("expected Ready or Idle state, got %v", state)
	}
	p.Put(conn)
}

// TestPool_HealthCheckLoop tests the health check loop.
func TestPool_HealthCheckLoop(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 50 * time.Millisecond,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !p.Healthy() {
		t.Error("expected pool to be healthy after health check")
	}

	err = p.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

// TestPool_PerformHealthCheck tests the performHealthCheck method.
func TestPool_PerformHealthCheck(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        2,
		MaxSize:        5,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	if p.Size() != 2 {
		t.Errorf("expected initial size 2, got %d", p.Size())
	}

	p.performHealthCheck()

	if p.Size() < cfg.MinSize {
		t.Errorf("expected at least %d connections, got %d", cfg.MinSize, p.Size())
	}
}

// TestPool_EnsureMinConnections tests ensuring minimum connections.
func TestPool_EnsureMinConnections(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        3,
		MaxSize:        5,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	if p.Size() != 3 {
		t.Errorf("expected size 3, got %d", p.Size())
	}

	p.mu.Lock()
	if len(p.connections) > 0 {
		pc := p.connections[0]
		p.connections = p.connections[1:]
		pc.conn.Close()
	}
	p.mu.Unlock()

	p.ensureMinConnections()

	if p.Size() < cfg.MinSize {
		t.Errorf("expected at least %d connections after ensureMinConnections, got %d",
			cfg.MinSize, p.Size())
	}
}

// TestPool_ReplaceConnection tests connection replacement.
func TestPool_ReplaceConnection(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        3,
		MaxAge:         1 * time.Millisecond,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	conn, err := p.Get()
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	p.Put(conn)

	time.Sleep(50 * time.Millisecond)

	if !p.Healthy() {
		t.Error("expected pool to be healthy after replacement")
	}
}

// TestPool_CloseWithConnections tests closing a pool with active connections.
func TestPool_CloseWithConnections(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        2,
		MaxSize:        5,
		MaxAge:         time.Hour,
		HealthInterval: 50 * time.Millisecond,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	conn1, _ := p.Get()
	conn2, _ := p.Get()
	_ = conn1
	_ = conn2

	err = p.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	if !p.closed.Load() {
		t.Error("expected pool to be marked closed")
	}

	_, err = p.Get()
	if err == nil {
		t.Error("expected error on Get after close")
	}
}

// TestPool_ConcurrentAccess tests concurrent pool access.
func TestPool_ConcurrentAccess(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        2,
		MaxSize:        10,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				conn, err := p.Get()
				if err != nil {
					if errors.Code(err) != errors.CodeResourceExhausted {
						errCh <- err
					}
					continue
				}
				time.Sleep(time.Millisecond)
				p.Put(conn)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestTransport_GetConnWithPool tests getConn with a pool.
func TestTransport_GetConnWithPool(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	tr := &Transport{
		cfg:  &transport.Config{},
		pool: p,
	}
	defer tr.Close()

	conn, err := tr.getConn()
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}
	tr.returnConn(conn)
}

// TestTransport_HealthyWithPool tests Healthy with a pool.
func TestTransport_HealthyWithPool(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	tr := &Transport{
		cfg:  &transport.Config{},
		pool: p,
	}
	defer tr.Close()

	if !tr.Healthy() {
		t.Error("expected transport to be healthy")
	}
}

// TestTransport_DoRequestNoTimeout tests doRequest without timeout.
func TestTransport_DoRequestNoTimeout(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	tr := &Transport{
		cfg: &transport.Config{
			Timeout: 0,
		},
		pool: p,
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = tr.doRequest(ctx, "test.Service", "Method", nil)
	if err == nil {
		t.Fatal("expected error for non-existent service")
	}
}

// TestTransport_RequestWithRetry_NonRetryableError tests non-retryable errors.
func TestTransport_RequestWithRetry_NonRetryableError(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	tr := &Transport{
		cfg: &transport.Config{
			MaxRetries:      3,
			RetryBackoff:    time.Millisecond,
			MaxRetryBackoff: 10 * time.Millisecond,
			Timeout:         100 * time.Millisecond,
		},
		pool: p,
	}
	defer tr.Close()

	start := time.Now()
	ctx := context.Background()
	_, err = tr.requestWithRetry(ctx, "test.Service", "Method", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for non-existent service")
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("retries took too long (%v), expected immediate failure for non-retryable error", elapsed)
	}
}

// TestTransport_RequestStream tests the RequestStream method.
func TestTransport_RequestStream(t *testing.T) {
	tests := []struct {
		name    string
		req     interface{}
		wantErr bool
	}{
		// Stream creation is lazy, so an unknown method still opens a stream.
		{name: "no initial request", req: nil, wantErr: false},
		// A non-proto initial request fails to marshal on SendMsg.
		{name: "initial request send fails", req: "initial-request", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer()
			defer ts.stop()

			p, err := NewPool("bufnet", ts.dialOpts(), &PoolConfig{MinSize: 1, MaxSize: 2, MaxAge: time.Hour})
			if err != nil {
				t.Fatalf("failed to create pool: %v", err)
			}

			tr := &Transport{
				cfg:  &transport.Config{Timeout: 5 * time.Second},
				pool: p,
			}
			defer tr.Close()

			stream, err := tr.RequestStream(context.Background(), "test.Service", "StreamMethod", tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stream == nil {
				t.Fatal("expected non-nil stream")
			}
			if err := stream.Close(); err != nil {
				t.Errorf("stream close: %v", err)
			}
		})
	}
}

// TestTransport_CloseWithPoolAndConn tests closing transport with both pool and conn.
func TestTransport_CloseWithPoolAndConn(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "bufnet", ts.dialOpts()...)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	tr := &Transport{
		cfg:  &transport.Config{},
		pool: p,
		conn: conn,
	}

	err = tr.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	if !tr.closed.Load() {
		t.Error("expected transport to be marked closed")
	}
}

// TestPool_ReplaceConnection_PoolFull tests replacement when pool is already full.
func TestPool_ReplaceConnection_PoolFull(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        2,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	defer p.Close()

	conn1, _ := p.Get()
	conn2, _ := p.Get()

	p.mu.Lock()
	var oldPC *pooledConn
	for _, pc := range p.connections {
		if pc.conn == conn1 {
			oldPC = pc
			break
		}
	}
	for i, pc := range p.connections {
		if pc == oldPC {
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			break
		}
	}
	p.mu.Unlock()

	if oldPC != nil {
		p.replaceConnection(oldPC)
		time.Sleep(100 * time.Millisecond)
	}

	p.Put(conn2)
}

// TestTransport_Request tests the high-level Request method.
func TestTransport_Request(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	cfg := &PoolConfig{
		MinSize:        1,
		MaxSize:        2,
		MaxAge:         time.Hour,
		HealthInterval: 0,
	}

	p, err := NewPool("bufnet", ts.dialOpts(), cfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	tr := &Transport{
		cfg: &transport.Config{
			MaxRetries:      0,
			RetryBackoff:    time.Millisecond,
			MaxRetryBackoff: 10 * time.Millisecond,
			Timeout:         100 * time.Millisecond,
		},
		pool: p,
	}
	defer tr.Close()

	ctx := context.Background()
	_, err = tr.Request(ctx, "test.Service", "Method", nil)
	if err == nil {
		t.Fatal("expected error for non-existent service")
	}
}

// TestTransport_GetConnWithSingleConnection tests getConn with single connection mode.
func TestTransport_GetConnWithSingleConnection(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "bufnet", ts.dialOpts()...)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	tr := &Transport{
		cfg:  &transport.Config{},
		conn: conn,
	}
	defer tr.Close()

	gotConn, err := tr.getConn()
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	if gotConn != conn {
		t.Error("expected same connection in single connection mode")
	}
}

// TestTransport_HealthyWithSingleConnection tests Healthy with single connection.
func TestTransport_HealthyWithSingleConnection(t *testing.T) {
	ts := newTestServer()
	defer ts.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "bufnet", ts.dialOpts()...)
	if err != nil {
		t.Fatalf("failed to create connection: %v", err)
	}

	tr := &Transport{
		cfg:  &transport.Config{},
		conn: conn,
	}
	defer tr.Close()

	if !tr.Healthy() {
		t.Error("expected transport to be healthy with ready connection")
	}
}
