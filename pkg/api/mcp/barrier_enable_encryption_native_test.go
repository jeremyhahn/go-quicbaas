//go:build mcp

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

package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	qrdbapimcp "github.com/jeremyhahn/go-qrdb/pkg/api/mcp"
	"github.com/jeremyhahn/go-qrdb/pkg/seal"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	sdkmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// barrier_enable_encryption_native_test.go drives the SDK MCP client's
// BarrierEnableEncryption against go-qrdb's own MCP server, the one that
// answers qrdb_barrier_enable_encryption on a node. The node starts the
// migration in the background and answers after its acceptance window, so a
// tenant migration must outlive the request that started it.

// blockedMigrator is an EncryptionMigrator whose migration runs until the
// test releases it or its context ends.
type blockedMigrator struct {
	started chan qrdbservice.BarrierScope
	release chan struct{}
	done    chan error
}

func newBlockedMigrator() *blockedMigrator {
	return &blockedMigrator{
		started: make(chan qrdbservice.BarrierScope, 1),
		release: make(chan struct{}),
		done:    make(chan error, 1),
	}
}

func (m *blockedMigrator) EnableEncryption(ctx context.Context, scope qrdbservice.BarrierScope, _ seal.MigrationOptions) error {
	m.started <- scope
	select {
	case <-m.release:
		m.done <- ctx.Err()
		return nil
	case <-ctx.Done():
		m.done <- ctx.Err()
		return ctx.Err()
	}
}

func TestBarrierEnableEncryptionNative_TenantMigrationOutlivesRequest(t *testing.T) {
	mig := newBlockedMigrator()
	var releaseOnce sync.Once
	releaseMigration := func() { releaseOnce.Do(func() { close(mig.release) }) }

	native := qrdbapimcp.NewServer(nil, nil, "", qrdbapimcp.WithEncryptionMigrator(mig))
	httpSrv := mcpserver.NewTestStreamableHTTPServer(native.MCPServer())
	tr, err := sdkmcp.New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		httpSrv.Close()
		t.Fatalf("failed to create MCP transport: %v", err)
	}
	t.Cleanup(func() {
		releaseMigration()
		_ = tr.Close()
		httpSrv.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := tr.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{
		TenantID:   "acme",
		Strategy:   "passphrase",
		Passphrase: []byte("correct horse battery staple"),
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Less(t, elapsed, 2*time.Second,
		"the node answers after its acceptance window, not when the migration ends")
	require.Equal(t, "migration_started", resp.Status)
	require.Equal(t, "tenant", resp.Scope)

	select {
	case scope := <-mig.started:
		require.Equal(t, qrdbservice.BarrierScope{Kind: qrdbservice.ScopeTenant, TenantID: "acme"}, scope)
	case <-time.After(2 * time.Second):
		t.Fatal("the tenant migration never started")
	}

	cancel()
	require.NoError(t, tr.Close())

	select {
	case err := <-mig.done:
		t.Fatalf("the migration ended with the request (err=%v); it must outlive it", err)
	case <-time.After(300 * time.Millisecond):
	}

	releaseMigration()
	select {
	case err := <-mig.done:
		require.NoError(t, err, "the migration context must never be cancelled by the request")
	case <-time.After(2 * time.Second):
		t.Fatal("the released migration never finished")
	}
}
