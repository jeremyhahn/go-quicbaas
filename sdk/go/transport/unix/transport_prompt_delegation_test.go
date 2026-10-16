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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestTransport_PromptVerbs_Delegate proves the Unix transport hands every
// Prompts verb to the gRPC prompt handler rather than refusing it locally:
// with no server listening each fails on the socket, never with "unknown
// method" or a transport refusal.
func TestTransport_PromptVerbs_Delegate(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()
	if !tr.SupportsPrompts() {
		t.Fatal("the Unix transport must declare the Prompts service")
	}

	addr := prompt.Address{Set: "s", Name: "n"}
	for method, req := range map[string]interface{}{
		"List":      &prompt.ListRequest{Set: "s"},
		"Get":       &prompt.GetRequest{Address: addr},
		"Effective": &addr,
		"Diff":      &addr,
		"History":   &addr,
		"Put":       &prompt.PutRequest{Address: addr, Content: "b", Note: "n"},
		"Rollback":  &prompt.RollbackRequest{Address: addr, ToVersion: 1, ExpectedVersion: 1},
		"Reset":     &prompt.ResetRequest{Address: addr, ExpectedVersion: 1},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := tr.Request(ctx, "quicbaas.Prompts", method, req)
		cancel()
		if err == nil {
			t.Fatalf("%s: expected a connection error, no server is listening", method)
		}
		if refusedAsUnsupported(err) {
			t.Fatalf("%s was refused before the socket, dispatch regression: %v", method, err)
		}
	}
}

// TestTransport_PromptUnknownVerb_IsRefusedByTheHandler proves the refusal
// of an unknown verb comes from the delegated gRPC handler, not the socket.
func TestTransport_PromptUnknownVerb_IsRefusedByTheHandler(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := tr.Request(ctx, "quicbaas.Prompts", "Delete", &prompt.Address{}); !errors.IsInvalidInput(err) {
		t.Fatalf("an unknown verb must be refused by the handler: %v", err)
	}
}
