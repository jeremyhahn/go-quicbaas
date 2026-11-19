package server

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestBuildQRDBConfig_NodeServesUnixSocket pins that a qrdb.v1 call over the
// Unix socket is authenticated by the embedded node exactly as over TCP: when
// the node serves the network (NetworkServing), the configured
// server.unix.socket is the node's own Unix listener, which carries the
// node's services and quicbaas's capability modules behind the same
// authentication middleware as its gRPC TCP listener. quicbaas's own Unix
// grpcServer, whose bearer-only authorizer refuses an mTLS-only
// administrator, must not bind it.
func TestBuildQRDBConfig_NodeServesUnixSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "q.sock")
	cfg := config.DefaultConfig()
	cfg.Server.Unix.Socket = socket
	require.True(t, cfg.NetworkServing(), "the default config serves the network through the node")

	qcfg, err := buildQRDBConfig(cfg, false)
	require.NoError(t, err)
	require.True(t, qcfg.Protocols.Unix, "the node must serve the Unix socket")
	require.Equal(t, socket, qcfg.Unix.SocketPath, "the node must listen on server.unix.socket")
}

// TestBuildQRDBConfig_NoUnixSocketLeavesNodeUnixOff pins that an empty
// server.unix.socket starts no Unix listener on the node.
func TestBuildQRDBConfig_NoUnixSocketLeavesNodeUnixOff(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Unix.Socket = ""

	qcfg, err := buildQRDBConfig(cfg, false)
	require.NoError(t, err)
	require.False(t, qcfg.Protocols.Unix)
	require.Empty(t, qcfg.Unix.SocketPath)
}
