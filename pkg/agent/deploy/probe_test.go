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

package deploy

import (
	"context"
	"errors"
	"net"
	"testing"
)

// TestDialAgentRuntime_Listening is the healthy case: a process holds the
// agent's runtime port, so the deploy may report it online.
func TestDialAgentRuntime_Listening(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	if err := DialAgentRuntime(context.Background(), l.Addr().String()); err != nil {
		t.Fatalf("DialAgentRuntime: %v", err)
	}
}

// TestDialAgentRuntime_Refused is the omega case seen from the control
// plane: `connect: connection refused` on the agent's port because the
// daemon died with the deploy's SSH session.
func TestDialAgentRuntime_Refused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err = DialAgentRuntime(context.Background(), addr)
	var unreachable *AgentUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("want AgentUnreachableError, got %T: %v", err, err)
	}
	if unreachable.Addr != addr {
		t.Fatalf("addr=%q want %q", unreachable.Addr, addr)
	}
}

func TestDialAgentRuntime_EmptyAddr(t *testing.T) {
	err := DialAgentRuntime(context.Background(), "")
	var specErr *InvalidSpecError
	if !errors.As(err, &specErr) {
		t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
	}
}

func TestDialAgentRuntime_CancelledContext(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := DialAgentRuntime(ctx, l.Addr().String()); err == nil {
		t.Fatal("expected error on cancelled context")
	}
}
