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

// Package qrdbnode boots a real single-member go-qrdb cluster for tests whose
// property is that the STORE decides a race.
//
// It is a leaf package on purpose. The obvious home was pkg/testutil, but that
// package imports pkg/service, which imports pkg/scheduler/storage/quicbaas —
// so the scheduler's own claim tests could not have used it without an import
// cycle. The only quicbaas package imported here is pkg/testutil/netport,
// which is itself a leaf over the standard library.
package qrdbnode

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	sdknode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/netport"
)

// =============================================================================
// Real qrdb node, for tests whose property is that the STORE decides a race
// =============================================================================
//
// Permitted in the unit tier by the Unit Test Contract's consensus-arbitration
// carve-out: a double that agrees with the implementation proves nothing about
// arbitration, and arbitration tests must run per push rather than nightly.
// The bounds come with it — memory backend, an isolated temp dir, an EPHEMERAL
// port, one node per package, and a budget of roughly twenty seconds.
//
// Data-shaped tests do not belong here. Use a double or sdk.NewMemoryKVStore
// when the question is what a record holds rather than who won it.

// ErrNeverLed reports a test node that never accepted a proposal.
var ErrNeverLed = errors.New("qrdbnode: test node never became leader")

// QRDBNode is a booted single-member qrdb cluster and the handles tests reach
// it through.
type Node struct {
	// Node is the running node. Tests needing the raw client use Node.Client().
	Node *sdknode.Node

	// Transaction decides guarded writes; KV serves reads.
	Transaction transport.TransactionClient
	KV          transport.KVClient
}

// fixture holds the one node a package boots.
type fixture struct {
	once sync.Once
	node *Node
	err  error
}

// shared is per-process, and a test binary is one package, so "one node
// per package" is what this yields without any coordination between packages.
var shared fixture

// sharedDir is the node's data directory, removed by
// CloseSharedQRDBNode.
var sharedDir string

// Shared boots a single-member in-memory qrdb cluster on first use and
// returns it to every caller after that.
//
// Booting is the expensive part of these tests — several seconds — and the
// transaction plane is keyed per record, so tests using distinct identifiers do
// not interfere. Callers that must not collide should key off their own unique
// identifiers rather than asking for a second node.
//
// The node needs no seal and no barrier: the transaction plane is independent
// of both, which is why arbitration records live on it.
//
// Registers no cleanup. The node lives for the test binary's lifetime; a
// package that wants it closed does so from TestMain via Close.
func Shared(t *testing.T) *Node {
	t.Helper()

	shared.once.Do(func() {
		// os.MkdirTemp rather than t.TempDir: the node is shared by the whole
		// package and outlives whichever test happened to boot it, while
		// t.TempDir is removed when THAT test ends — which would delete the
		// running node's data directory partway through the suite.
		// CloseShared removes it instead.
		dir, err := os.MkdirTemp("", "quicbaas-qrdbnode-")
		if err != nil {
			shared.err = err
			return
		}
		sharedDir = dir
		shared.node, shared.err = boot(dir)
	})
	if shared.err != nil {
		t.Fatalf("booting the qrdb test node: %v", shared.err)
	}
	return shared.node
}

// CloseShared shuts the shared node down if one was booted. It is safe
// to call when none was, so TestMain can call it unconditionally.
func CloseShared() {
	if shared.node != nil && shared.node.Node != nil {
		_ = shared.node.Node.Close()
	}
	if sharedDir != "" {
		_ = os.RemoveAll(sharedDir)
	}
}

// boot starts a one-member cluster in dir.
func boot(dir string) (*Node, error) {
	addr, err := reservedAddr()
	if err != nil {
		return nil, err
	}

	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = dir
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	// Every listener off: only the embedded transport is needed.
	cfg.Protocols.REST = false
	cfg.Protocols.GRPC = false
	cfg.Protocols.QUIC = false
	cfg.Protocols.Unix = false
	cfg.Protocols.MCP = false

	cfg.Raft.ReplicaID = 1
	cfg.Raft.ShardID = 1
	cfg.Raft.RaftAddress = addr

	// WithNoSyncWrites: the node's data directory is deleted when the
	// package ends, so fsync buys these tests nothing, while on a runner
	// whose disk is shared with other jobs one fsync can outlast a
	// caller's whole per-operation deadline (go-quicbaas#1340). Without
	// fsync the order Raft commits proposals in — the only thing an
	// arbitration test asserts — is unchanged.
	node, err := sdknode.NewClusteredNode(context.Background(), cfg, map[uint64]string{1: addr}, nil,
		sdknode.WithNoSyncWrites())
	if err != nil {
		return nil, err
	}
	if err := awaitLeadership(node); err != nil {
		_ = node.Close()
		return nil, err
	}
	return &Node{
		Node:        node,
		Transaction: node.Client().Transaction(),
		KV:          node.Client().KV(),
	}, nil
}

// reservedAddr returns a loopback address whose port is reserved for this test
// binary.
//
// The port is RESERVED, never chosen. Hand-picked constants only avoid each
// other by luck: two packages' fixtures once sat on 63991 and 63992 purely
// because someone remembered to check, and `go test` runs packages in
// parallel, so the first forgotten increment produces a bind failure that reads
// as a Raft problem rather than a port problem.
//
// This used to ask the kernel for a port by listening on ":0" and closing the
// listener, which left the port free from that close until the node bound it —
// a small race, but one crossed by every fixture on the host at once. netport
// removes the interval instead of shrinking it: the port is locked host-wide
// and lies outside the range the kernel draws ":0" binds from
// (go-quicbaas#706).
func reservedAddr() (string, error) {
	ports, err := netport.Reserve(1)
	if err != nil {
		return "", err
	}
	return netport.LoopbackAddr(ports[0]), nil
}

// awaitLeadership blocks until the single-member group has elected itself and
// accepts a proposal.
//
// A freshly started replica is not yet leader, so the first transaction returns
// "not leader for shard 1". Readiness is polled with a ticker rather than slept
// on: the convention forbids time.Sleep, and a ticker with a deadline bounds
// the failure instead of hanging the suite.
func awaitLeadership(node *sdknode.Node) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	probe := []byte("__sys:testutil/leadership_probe")
	var lastErr error
	for {
		_, lastErr = node.Client().Transaction().ExecuteTransaction(ctx, &transport.TransactionRequest{
			Compares: []transport.Compare{{
				Key:            probe,
				Target:         transport.CompareCreateRevision,
				Result:         transport.CompareEqual,
				CreateRevision: 0,
			}},
			SuccessOps: []transport.Op{{Type: transport.OpPut, Key: probe, Value: []byte("ready")}},
		})
		if lastErr == nil {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return errors.Join(ErrNeverLed, lastErr)
		}
	}
}
