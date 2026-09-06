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

package server

import (
	"context"
	"errors"
	"testing"
)

// TestSealedKVServicerProxy_GuardedWritesFailClosed proves the guarded
// write pair added with go-qrdb v0.16.0 — the release half (CompareAndDelete)
// and the edit half (CompareAndSwap) of PutIfAbsent — fail closed with the
// typed sealed-window error while the embedded node is absent, exactly as
// the unguarded verbs do, instead of panicking on a nil servicer.
func TestSealedKVServicerProxy_GuardedWritesFailClosed(t *testing.T) {
	ctx := context.Background()
	p := newSealedKVServicerProxy(&Server{})

	var sealed *SealedNodeUnavailableError

	deleted, err := p.CompareAndDelete(ctx, "k", []byte("v"))
	if !errors.As(err, &sealed) {
		t.Fatalf("CompareAndDelete: expected SealedNodeUnavailableError, got %v", err)
	}
	if deleted {
		t.Fatal("CompareAndDelete reported a delete through a sealed node")
	}
	if sealed.Service != "kv" {
		t.Fatalf("expected kv service in the sealed error, got %q", sealed.Service)
	}

	swapped, err := p.CompareAndSwap(ctx, "k", []byte("v"), []byte("w"))
	if !errors.As(err, &sealed) {
		t.Fatalf("CompareAndSwap: expected SealedNodeUnavailableError, got %v", err)
	}
	if swapped {
		t.Fatal("CompareAndSwap reported a swap through a sealed node")
	}
}
