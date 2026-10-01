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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
)

// rejectedAsUndispatched reports an error that means the registry request
// never reached the gRPC registry handler, as opposed to a connection failure
// against a socket nobody is listening on.
func rejectedAsUndispatched(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "not supported") ||
		strings.Contains(msg, "not exposed by RegistryService") ||
		strings.Contains(msg, "not a RegistryService streaming method") ||
		strings.Contains(msg, "invalid request type")
}

// TestTransport_Registry_Delegates proves the Unix transport routes every
// registry verb, and the watch stream, through the gRPC registry handler.
// With no server listening each call fails on connection; failing as
// unsupported or as a rejected request type would mean the dispatch table
// never saw it. The gRPC handler tests prove the proto conversion against a
// recording server.
func TestTransport_Registry_Delegates(t *testing.T) {
	unary := map[string]interface{}{
		"Register":   &registryclient.RegisterRequest{Instance: registryclient.ServiceInstance{ID: "i", Service: "api", Address: "a:1"}, TTLSeconds: 5},
		"Renew":      &registryclient.RenewRequest{LeaseID: 1},
		"Deregister": &registryclient.DeregisterRequest{LeaseID: 1},
		"Resolve":    &registryclient.ResolveRequest{Service: "api"},
	}
	for method, req := range unary {
		t.Run(method, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() {
				if err := tr.Close(); err != nil {
					t.Errorf("close: %v", err)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_, err := tr.Request(ctx, registryclient.ServiceName, method, req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			if rejectedAsUndispatched(err) {
				t.Fatalf("registry %s was not dispatched to the gRPC handler: %v", method, err)
			}
		})
	}

	t.Run("Watch", func(t *testing.T) {
		tr := newWiredTransport(t)
		defer func() {
			if err := tr.Close(); err != nil {
				t.Errorf("close: %v", err)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		stream, err := tr.RequestStream(ctx, registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 1})
		if err == nil {
			// gRPC opens a server stream lazily; the failure surfaces on Recv.
			_, err = stream.Recv()
		}
		if err == nil {
			t.Fatal("expected a connection error: no server is listening")
		}
		if rejectedAsUndispatched(err) {
			t.Fatalf("registry watch was not dispatched to the gRPC handler: %v", err)
		}
	})
}

// TestTransport_Registry_ClosedTransport proves a closed Unix transport
// rejects registry calls instead of dialing a dead socket.
func TestTransport_Registry_ClosedTransport(t *testing.T) {
	tr := newWiredTransport(t)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := tr.Request(context.Background(), registryclient.ServiceName, "Resolve", &registryclient.ResolveRequest{Service: "api"}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Request on closed transport = %v", err)
	}
	if _, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api"}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("RequestStream on closed transport = %v", err)
	}
}
