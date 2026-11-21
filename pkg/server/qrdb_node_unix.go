package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// nodeUnixSocketDirMode and nodeUnixSocketMode are the modes the legacy
// quicbaas Unix listener (startUnixSocketListener) gives the socket's
// directory and the socket, kept when the embedded node serves the socket.
const (
	nodeUnixSocketDirMode os.FileMode = 0o750
	nodeUnixSocketMode    os.FileMode = 0o660
)

// prepareNodeUnixSocketDir creates the socket's directory at 0750, as
// startUnixSocketListener does, before the node is built. The node's Unix
// server creates a missing directory 0700 on every start; with the directory
// already there its MkdirAll is a no-op, so the legacy mode stands. An
// existing directory keeps its mode.
func prepareNodeUnixSocketDir(socket string) error {
	dir := filepath.Dir(socket)
	if err := os.MkdirAll(dir, nodeUnixSocketDirMode); err != nil {
		return fmt.Errorf("create unix socket directory %s: %w", dir, err)
	}
	return nil
}

// unixSocketModeModule puts the node's Unix socket at 0660, the mode
// startUnixSocketListener gives it, so group members keep their access. The
// node's Unix server binds the socket with the process umask and never
// chmods it. The chmod lives in Start because go-qrdb runs every module's
// Start after its transports are serving, on every server start: at
// construction, on a deferred unseal boot and on each encryption-migration
// recycle, each of which binds the socket afresh.
type unixSocketModeModule struct {
	qrdbsdk.BaseModule
	socket string
}

var _ qrdbsdk.ServiceModule = (*unixSocketModeModule)(nil)

// Name identifies the module on the node.
func (m *unixSocketModeModule) Name() string { return "quicbaas-unix-socket-mode" }

// Start sets the socket's mode to 0660.
func (m *unixSocketModeModule) Start(_ context.Context) error {
	if err := os.Chmod(m.socket, nodeUnixSocketMode); err != nil {
		return fmt.Errorf("set unix socket mode on %s: %w", m.socket, err)
	}
	return nil
}
