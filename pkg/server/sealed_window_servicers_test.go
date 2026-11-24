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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
)

// compactionAdminServicer wraps a live node admin servicer and answers the
// compaction pair itself, so the test can see exactly what the proxy hands
// over and hands back.
type compactionAdminServicer struct {
	qrdbsdk.AdminServicer
	policy *qrdbsdk.CompactionPolicy
	set    *qrdbsdk.CompactionPolicy
}

func (f *compactionAdminServicer) NodeCompaction(context.Context) (*qrdbsdk.CompactionPolicy, error) {
	return f.policy, nil
}

func (f *compactionAdminServicer) SetNodeCompaction(_ context.Context, policy *qrdbsdk.CompactionPolicy) error {
	f.set = policy
	return nil
}

// TestSealedAdminServicerProxy_NodeCompaction proves the compaction pair
// added with go-qrdb v0.51.0 fails closed with the typed sealed-window error
// while the embedded node is absent, and forwards to the live admin servicer
// once it is there.
func TestSealedAdminServicerProxy_NodeCompaction(t *testing.T) {
	ctx := context.Background()

	t.Run("sealed", func(t *testing.T) {
		p := newSealedAdminServicerProxy(&Server{})
		var sealed *SealedNodeUnavailableError

		policy, err := p.NodeCompaction(ctx)
		if !errors.As(err, &sealed) {
			t.Fatalf("NodeCompaction: expected SealedNodeUnavailableError, got %v", err)
		}
		if sealed.Service != "admin" {
			t.Fatalf("expected admin service in the sealed error, got %q", sealed.Service)
		}
		if policy != nil {
			t.Fatalf("NodeCompaction returned a policy through a sealed node: %+v", policy)
		}

		sealed = nil
		err = p.SetNodeCompaction(ctx, &qrdbsdk.CompactionPolicy{})
		if !errors.As(err, &sealed) {
			t.Fatalf("SetNodeCompaction: expected SealedNodeUnavailableError, got %v", err)
		}
		if sealed.Service != "admin" {
			t.Fatalf("expected admin service in the sealed error, got %q", sealed.Service)
		}
	})

	t.Run("live", func(t *testing.T) {
		cfg := &qrdbsdk.DaemonConfig{}
		cfg.Raft.DataDir = t.TempDir()
		cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
			{Name: "default", Backend: "memory"},
		}
		node, err := qrdbnode.NewStandaloneNode(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("start node: %v", err)
		}
		t.Cleanup(func() { _ = node.Close() })

		fake := &compactionAdminServicer{
			AdminServicer: node.Services().Admin,
			policy:        &qrdbsdk.CompactionPolicy{},
		}
		node.Services().Admin = fake

		s := &Server{}
		s.setQRDBNode(node)
		t.Cleanup(func() { s.setQRDBNode(nil) })
		p := newSealedAdminServicerProxy(s)

		got, err := p.NodeCompaction(ctx)
		if err != nil {
			t.Fatalf("NodeCompaction: %v", err)
		}
		if got != fake.policy {
			t.Fatalf("NodeCompaction returned %p, want the live servicer's %p", got, fake.policy)
		}

		want := &qrdbsdk.CompactionPolicy{}
		if err := p.SetNodeCompaction(ctx, want); err != nil {
			t.Fatalf("SetNodeCompaction: %v", err)
		}
		if fake.set != want {
			t.Fatalf("SetNodeCompaction passed %p, want %p", fake.set, want)
		}
	})
}

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
