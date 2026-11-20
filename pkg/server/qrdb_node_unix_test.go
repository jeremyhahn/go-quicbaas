package server

import (
	"context"
	"io/fs"
	"net"
	"os"
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

	srv := &Server{config: cfg}
	require.False(t, srv.quicbaasServesUnixSocket(),
		"quicbaas's own grpcServer must not bind the socket the node serves")
}

// TestQuicbaasServesUnixSocket_WithoutNetworkServing pins that quicbaas's own
// Unix listener still runs when the node serves no network protocol.
func TestQuicbaasServesUnixSocket_WithoutNetworkServing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Unix.Socket = filepath.Join(t.TempDir(), "q.sock")
	cfg.Server.REST.Enabled = false
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false

	qcfg, err := buildQRDBConfig(cfg, false)
	require.NoError(t, err)
	require.False(t, qcfg.Protocols.Unix)

	srv := &Server{config: cfg}
	require.True(t, srv.quicbaasServesUnixSocket())
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

// TestUnixSocketModeModule_StartChmodsTheSocket pins that the module's Start
// hook puts the node's socket back at the legacy listener's 0660.
func TestUnixSocketModeModule_StartChmodsTheSocket(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "m.sock")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	require.NoError(t, os.Chmod(socket, 0o700))

	m := &unixSocketModeModule{socket: socket}
	require.NoError(t, m.Start(context.Background()))

	info, err := os.Stat(socket)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o660), info.Mode().Perm())
}

// TestUnixSocketModeModule_StartWithoutSocketFails pins that a missing socket
// fails Start with an error that keeps fs.ErrNotExist.
func TestUnixSocketModeModule_StartWithoutSocketFails(t *testing.T) {
	m := &unixSocketModeModule{socket: filepath.Join(shortSocketDir(t), "missing.sock")}
	err := m.Start(context.Background())
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// TestPrepareNodeUnixSocketDir_Creates0750 pins that the socket's missing
// directory is created at the legacy listener's 0750 and that an existing
// directory keeps its mode.
func TestPrepareNodeUnixSocketDir_Creates0750(t *testing.T) {
	base := shortSocketDir(t)
	dir := filepath.Join(base, "sub")
	require.NoError(t, prepareNodeUnixSocketDir(filepath.Join(dir, "g.sock")))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	perm := info.Mode().Perm()
	require.Zero(t, perm&^0o750, "no mode bits beyond 0750: %v", perm)
	require.Equal(t, os.FileMode(0o750), perm&0o750, "umask 022 or 027 leaves 0750: %v", perm)

	existing := filepath.Join(base, "existing")
	require.NoError(t, os.Mkdir(existing, 0o700))
	require.NoError(t, os.Chmod(existing, 0o700))
	require.NoError(t, prepareNodeUnixSocketDir(filepath.Join(existing, "g.sock")))
	info, err = os.Stat(existing)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "an existing directory keeps its mode")
}
