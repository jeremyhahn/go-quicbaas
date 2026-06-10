// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package unix

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newWiredTransport constructs a Unix transport with a valid TLS config so the
// underlying gRPC transport is successfully created. The gRPC client dials
// lazily, so no server is required: the connection object exists but is never
// actually established. This lets us exercise the delegation paths
// (Request/RequestStream/Conn/Close/Healthy) that forward to the live gRPC
// transport, which the closed/nil guard tests cannot reach.
func newWiredTransport(t *testing.T) *Transport {
	t.Helper()
	cfg := &transport.Config{
		Address:   "/tmp/quicbaas-unix-delegation-test.sock",
		Timeout:   100 * time.Millisecond,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13},
	}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with valid TLS config should succeed (lazy dial), got %v", err)
	}
	if tr.grpc == nil {
		t.Fatal("expected underlying gRPC transport to be wired")
	}
	return tr
}

// TestTransport_New_Success verifies the success branch of New, which returns a
// Transport with a live gRPC delegate and the normalized socket path.
func TestTransport_New_Success(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() {
		if err := tr.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if tr.SocketPath() != "/tmp/quicbaas-unix-delegation-test.sock" {
		t.Errorf("unexpected socket path %q", tr.SocketPath())
	}
}

// TestTransport_Request_Delegates exercises the success branch of Request where
// the call is forwarded to the gRPC transport. With no server listening and a
// short deadline the request fails, but the delegation path is covered.
func TestTransport_Request_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "Service", "Method", struct{}{})
	if err == nil {
		t.Fatal("expected error: no server is listening on the socket")
	}
}

// TestTransport_RequestStream_Delegates exercises the success branch of
// RequestStream forwarding to the gRPC transport.
func TestTransport_RequestStream_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.RequestStream(ctx, "Service", "Method", struct{}{})
	if err == nil {
		t.Fatal("expected error: no server is listening on the socket")
	}
}

// TestTransport_Conn_Delegates verifies Conn returns the live gRPC connection
// object when the transport is wired (non-nil delegate).
func TestTransport_Conn_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	if tr.Conn() == nil {
		t.Fatal("expected non-nil connection from wired transport")
	}
}

// TestTransport_Healthy_Delegates verifies Healthy delegates to the gRPC
// transport. A lazily-dialed connection is not yet Ready, so it reports
// unhealthy without blocking.
func TestTransport_Healthy_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	// The connection is lazy and never reaches Ready, so Healthy is false.
	if tr.Healthy() {
		t.Error("expected unhealthy for a lazily-dialed, unconnected transport")
	}
}

// TestTransport_Close_Delegates verifies Close forwards to the gRPC transport
// and is idempotent.
func TestTransport_Close_Delegates(t *testing.T) {
	tr := newWiredTransport(t)

	if err := tr.Close(); err != nil {
		t.Errorf("first Close should succeed, got %v", err)
	}
	if !tr.closed.Load() {
		t.Error("transport should be marked closed")
	}
	// Second Close is a no-op via the CompareAndSwap guard.
	if err := tr.Close(); err != nil {
		t.Errorf("second Close should be a no-op, got %v", err)
	}
	// After Close, Healthy must report false.
	if tr.Healthy() {
		t.Error("expected unhealthy after Close")
	}
}
