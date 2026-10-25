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
	"path/filepath"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	sdknode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
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
		// The node runs go-quicbaas's production service principal and
		// grants (pkg/qrdbservice), so a test that passes here passes under
		// the grant set the daemon actually holds.
		principal, err := qrdbservice.NodeOption()
		if err != nil {
			shared.err = err
			return
		}
		shared.node, shared.err = boot(dir, nil, principal)
	})
	if shared.err != nil {
		t.Fatalf("booting the qrdb test node: %v", shared.err)
	}
	return shared.node
}

// CloseShared shuts down the node Shared, TenantHost or TenantBarrierHost
// booted, if any, and
// removes its data directory. It is safe to call when none was booted, so
// TestMain can call it unconditionally.
func CloseShared() {
	for _, f := range []struct {
		node *Node
		dir  string
	}{{shared.node, sharedDir}, {tenantHost.node, tenantHostDir}, {tenantBarrierHost.node, tenantBarrierHostDir}} {
		if f.node != nil && f.node.Node != nil {
			_ = f.node.Node.Close()
		}
		if f.dir != "" {
			_ = os.RemoveAll(f.dir)
		}
	}
}

// TenantHostPrincipal is the service principal a TenantHost node runs as. It
// exists only in tests: it holds go-quicbaas's production grant set
// (qrdbservice.Grants) plus tenants:create and volume-pool:create, which the
// production principal does not hold, so a test can onboard the tenants it
// needs. The production
// principal and its grants are left untouched.
const TenantHostPrincipal = "quicbaas-test-tenant-host"

// tenantHost is the package's tenant-hosting node, booted once by TenantHost.
var tenantHost fixture

// tenantHostDir is tenantHost's data directory, removed by CloseShared.
var tenantHostDir string

// TenantHost returns the package's qrdb test node configured to host
// tenants, booting it on first use. It differs from Shared in two ways: the
// barrier is enabled with go-qrdb's "none" seal strategy, because tenant
// onboarding is wired only on a node with a seal, and the node runs as
// TenantHostPrincipal. Its ServiceContext may therefore call
// Tenants().CreateTenant; everything inside a tenant is then done by the
// tenant's own certificate identities. A package uses Shared or TenantHost,
// not both, so it still boots one node.
func TenantHost(t *testing.T) *Node {
	t.Helper()

	tenantHost.once.Do(func() {
		tenantHost.node, tenantHostDir, tenantHost.err = bootTenantHost("quicbaas-qrdbnode-tenants-", nil)
	})
	if tenantHost.err != nil {
		t.Fatalf("booting the tenant-hosting qrdb test node: %v", tenantHost.err)
	}
	return tenantHost.node
}

// tenantBarrierHost is the package's barrier-hosting node, booted once by
// TenantBarrierHost.
var tenantBarrierHost fixture

// tenantBarrierHostDir is tenantBarrierHost's data directory, removed by
// CloseShared.
var tenantBarrierHostDir string

// TenantBarrierHost returns a TenantHost node that can also initialize a
// tenant's own barrier, booting it on first use. A tenant barrier keeps its
// root key in a volume pool, so the node maps a "default" pool to a
// directory under its data dir and creates it in the replicated catalog,
// as go-qrdb's own tenant onboarding conformance cell does. A package uses
// one of Shared, TenantHost and TenantBarrierHost, so it still boots one
// node.
func TenantBarrierHost(t *testing.T) *Node {
	t.Helper()

	tenantBarrierHost.once.Do(func() {
		var poolErr error
		tenantBarrierHost.node, tenantBarrierHostDir, tenantBarrierHost.err = bootTenantHost(
			"quicbaas-qrdbnode-barriers-", func(dir string, cfg *qrdbsdk.DaemonConfig) {
				// The dir provider opens the mount at boot, so it must exist.
				poolDir := filepath.Join(dir, "pool")
				poolErr = os.Mkdir(poolDir, 0o700)
				cfg.Storage.VolumePools = []qrdbsdk.DaemonVolumePoolConfig{{Name: "default", Path: poolDir}}
			})
		if tenantBarrierHost.err == nil {
			tenantBarrierHost.err = poolErr
		}
		if tenantBarrierHost.err != nil {
			return
		}
		tenantBarrierHost.err = createDefaultPool(tenantBarrierHost.node)
	})
	if tenantBarrierHost.err != nil {
		t.Fatalf("booting the barrier-hosting qrdb test node: %v", tenantBarrierHost.err)
	}
	return tenantBarrierHost.node
}

// registerTenantHostGrants grants TenantHostPrincipal go-quicbaas's
// production grant set plus the grants that onboarding test tenants takes,
// once per process.
var registerTenantHostGrants = sync.OnceValue(func() error {
	perms, err := qrdbservice.Grants()
	if err != nil {
		return err
	}
	qrdbsdk.RegisterPermissionRoleGrant(TenantHostPrincipal,
		append(perms, qrdbsdk.PermissionTenantsCreate, qrdbsdk.PermissionVolumePoolCreate)...)
	return nil
})

// bootTenantHost boots a seal-enabled node running as TenantHostPrincipal
// in a new temp dir named by pattern, and returns the node and its dir.
func bootTenantHost(pattern string, configure func(dir string, cfg *qrdbsdk.DaemonConfig)) (*Node, string, error) {
	if err := registerTenantHostGrants(); err != nil {
		return nil, "", err
	}
	// os.MkdirTemp rather than t.TempDir, for the reason Shared gives.
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		return nil, "", err
	}
	node, err := boot(dir, func(cfg *qrdbsdk.DaemonConfig) {
		cfg.Seal.Enabled = true
		cfg.Seal.Strategy = "none"
		if configure != nil {
			configure(dir, cfg)
		}
	}, sdknode.WithServicePrincipal(TenantHostPrincipal))
	return node, dir, err
}

// createDefaultPool creates the "default" volume pool the node maps in the
// replicated catalog, as the node's service principal.
func createDefaultPool(n *Node) error {
	ctx, err := n.Node.ServiceContext(context.Background())
	if err != nil {
		return err
	}
	_, err = n.Node.Client().VolumePools().CreatePool(ctx, &transport.CreateVolumePoolRequest{Name: "default"})
	return err
}

// boot starts a one-member cluster in dir.
func boot(dir string, configure func(*qrdbsdk.DaemonConfig), principal sdknode.NodeOption) (*Node, error) {
	addr, err := reservedAddr()
	if err != nil {
		return nil, err
	}

	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = dir
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Backend: "memory"},
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
	if configure != nil {
		configure(cfg)
	}

	// WithNoSyncWrites: the node's data directory is deleted when the
	// package ends, so fsync buys these tests nothing, while on a runner
	// whose disk is shared with other jobs one fsync can outlast a
	// caller's whole per-operation deadline (go-quicbaas#1340). Without
	// fsync the order Raft commits proposals in — the only thing an
	// arbitration test asserts — is unchanged.
	node, err := sdknode.NewClusteredNode(context.Background(), cfg, map[uint64]string{1: addr}, nil,
		sdknode.WithNoSyncWrites(), principal)
	if err != nil {
		return nil, err
	}
	txn := qrdbservice.Transaction(node, node.Client().Transaction())
	if err := awaitLeadership(txn); err != nil {
		_ = node.Close()
		return nil, err
	}
	return &Node{
		Node:        node,
		Transaction: txn,
		KV:          qrdbservice.KV(node, node.Client().KV()),
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
func awaitLeadership(txn transport.TransactionClient) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	probe := []byte("__sys:testutil/leadership_probe")
	var lastErr error
	for {
		_, lastErr = txn.ExecuteTransaction(ctx, &transport.TransactionRequest{
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
