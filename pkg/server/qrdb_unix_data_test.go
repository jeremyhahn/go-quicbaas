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
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	unixtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"
)

// TestServer_QRDBServiceOnUnixSocket_ServesNodeData proves the qrdb.v1
// service registered on the Unix socket's gRPC server serves the embedded
// node's data services: a go-qrdb client over the socket writes and reads a
// key instead of getting Unimplemented.
func TestServer_QRDBServiceOnUnixSocket_ServesNodeData(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "initStandaloneQRDBNode")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	dir, err := os.MkdirTemp("", "qb1652")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "q.sock")
	lis, err := net.Listen("unix", sock)
	require.NoError(t, err)
	gs := ggrpc.NewServer()
	srv.registerQRDBService(gs)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	tr, err := unixtransport.New(qrdbtransport.WithAddress(sock))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil && !strings.Contains(err.Error(), "health check failed") {
		t.Fatal(err)
	}
	cl, err := qrdbsdk.NewClient(tr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })

	require.NoError(t, cl.KV().Put(ctx, "row1652/unix", []byte("v")))
	got, err := cl.KV().Get(ctx, "row1652/unix")
	require.NoError(t, err)
	require.Equal(t, "v", string(got))
}
