//go:build !integration

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

package server

import (
	"context"
	"errors"
	"testing"

	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// bindingReaderRecorder is a facade that records the binding reader the
// server hands it. Every other facade method is unused by wireProjectBindings.
type bindingReaderRecorder struct {
	service.QuicBaaS
	reader sdlc.BindingReader
	calls  int
}

func (r *bindingReaderRecorder) SetProjectBindingReader(bindings sdlc.BindingReader) {
	r.reader = bindings
	r.calls++
}

func closeBindingTestNode(t *testing.T, node *qrdbnode.Node) {
	t.Helper()
	if err := node.Close(); err != nil {
		t.Errorf("close qrdb node: %v", err)
	}
}

// TestWireProjectBindings_BindsWithSDLCDisabled is go-quicbaas#891: a node
// whose qrdb node has a tenant store resolver binds the binding store onto
// the project service even with the SDLC module off, because bindings
// persist in the tenant keyspace either way.
func TestWireProjectBindings_BindsWithSDLCDisabled(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { closeBindingTestNode(t, node) })
	cfg := &config.Config{}
	cfg.SDLC.Enabled = false
	s := &Server{logger: buildTestLogger(t), config: cfg, qrdbNode: node}
	facade := &bindingReaderRecorder{}

	if err := s.wireProjectBindings(facade); err != nil {
		t.Fatalf("wireProjectBindings: %v", err)
	}

	if facade.calls != 1 {
		t.Fatalf("SetProjectBindingReader called %d times, want 1", facade.calls)
	}
	if _, ok := facade.reader.(*sdlc.Store); !ok {
		t.Fatalf("binding reader = %T, want *sdlc.Store", facade.reader)
	}
	_, err := facade.reader.GetBinding(context.Background(), "tenant-a", "project-a")
	if !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("GetBinding on an empty tenant keyspace = %v, want sdlc.ErrNotFound", err)
	}
}

// TestWireProjectBindings_NoResolverLeavesReaderUnwired proves a node with no
// tenant store resolver, or no node at all, wires nothing: no binding store,
// so no binding can exist.
func TestWireProjectBindings_NoResolverLeavesReaderUnwired(t *testing.T) {
	t.Run("no node", func(t *testing.T) {
		s := &Server{logger: buildTestLogger(t)}
		facade := &bindingReaderRecorder{}
		if err := s.wireProjectBindings(facade); err != nil {
			t.Fatalf("wireProjectBindings: %v", err)
		}
		if facade.calls != 0 {
			t.Fatalf("SetProjectBindingReader called %d times with no node", facade.calls)
		}
	})
	t.Run("no resolver", func(t *testing.T) {
		node := buildMemoryStandaloneNode(t)
		t.Cleanup(func() { closeBindingTestNode(t, node) })
		node.Services().StoreResolver = nil
		s := &Server{logger: buildTestLogger(t), qrdbNode: node}
		facade := &bindingReaderRecorder{}
		if err := s.wireProjectBindings(facade); err != nil {
			t.Fatalf("wireProjectBindings: %v", err)
		}
		if facade.calls != 0 {
			t.Fatalf("SetProjectBindingReader called %d times with no resolver", facade.calls)
		}
	})
}
