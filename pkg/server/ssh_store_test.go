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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newSSHStoreTestServer(t *testing.T, dataDir string, xkms config.XKMSConfig) *Server {
	t.Helper()
	return &Server{
		config: &config.Config{Storage: config.StorageConfig{DataDir: dataDir}, XKMS: xkms},
		logger: buildTestLogger(t),
	}
}

func TestBuildSSHClient_RemoteXKMSD(t *testing.T) {
	s := newSSHStoreTestServer(t, t.TempDir(), config.XKMSConfig{URL: " https://xkmsd.example.internal:8443 ", Token: "t"})

	client, err := s.buildSSHClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NoError(t, client.Close())
}

// TestBuildSSHClient_UnsealedNodeFailsClosed proves a node with no barrier
// and no remote xkmsd gets no SSH store at all.
func TestBuildSSHClient_UnsealedNodeFailsClosed(t *testing.T) {
	s := newSSHStoreTestServer(t, t.TempDir(), config.XKMSConfig{})

	client, err := s.buildSSHClient()
	require.NoError(t, err)
	assert.Nil(t, client, "an unsealed node must have no SSH key store")
}

// newSSHStoreLogServer returns an unsealed test server whose logger writes
// JSON lines into the returned buffer.
func newSSHStoreLogServer(t *testing.T, dataDir string) (*Server, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	log, err := qrdbsdk.NewLoggerWithHandler(handler, logger.LogConfig{Level: "warn"})
	require.NoError(t, err)
	s := newSSHStoreTestServer(t, dataDir, config.XKMSConfig{})
	s.logger = log
	return s, &buf
}

// legacySSHStoreWarnings returns the decoded warning records whose message
// is about the legacy plaintext SSH key directory.
func legacySSHStoreWarnings(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var warnings []map[string]any
	dec := json.NewDecoder(buf)
	for {
		var record map[string]any
		err := dec.Decode(&record)
		if errors.Is(err, io.EOF) {
			return warnings
		}
		require.NoError(t, err)
		msg, ok := record["msg"].(string)
		require.True(t, ok, "log record without a message: %v", record)
		if record["level"] == "WARN" && strings.Contains(msg, "legacy") {
			warnings = append(warnings, record)
		}
	}
}

func TestBuildSSHClient_WarnsOnLegacyPlaintextDir(t *testing.T) {
	dataDir := t.TempDir()
	legacyDir := filepath.Join(dataDir, "xkms-ssh")
	require.NoError(t, os.Mkdir(legacyDir, 0o700))
	s, buf := newSSHStoreLogServer(t, dataDir)

	_, err := s.buildSSHClient()
	require.NoError(t, err)

	warnings := legacySSHStoreWarnings(t, buf)
	require.Len(t, warnings, 1, "exactly one legacy-directory warning")
	assert.Equal(t, legacyDir, warnings[0]["legacy_dir"])
	assert.Contains(t, warnings[0]["msg"], "plaintext private keys")
	assert.Contains(t, warnings[0]["msg"], "docs/ssh/architecture.md")
	assert.DirExists(t, legacyDir, "the check must delete nothing")
}

func TestBuildSSHClient_WarnsOnInterruptedWipeDir(t *testing.T) {
	dataDir := t.TempDir()
	wipingDir := filepath.Join(dataDir, "xkms-ssh.wiping")
	require.NoError(t, os.Mkdir(wipingDir, 0o700))
	s, buf := newSSHStoreLogServer(t, dataDir)

	_, err := s.buildSSHClient()
	require.NoError(t, err)

	warnings := legacySSHStoreWarnings(t, buf)
	require.Len(t, warnings, 1)
	assert.Equal(t, wipingDir, warnings[0]["legacy_dir"])
}

func TestBuildSSHClient_NoLegacyDirNoWarning(t *testing.T) {
	s, buf := newSSHStoreLogServer(t, t.TempDir())

	_, err := s.buildSSHClient()
	require.NoError(t, err)
	assert.Empty(t, legacySSHStoreWarnings(t, buf))
}

func TestBuildSSHClient_LegacyPathIsAFile(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "xkms-ssh"), []byte("not a store"), 0o600))
	s, buf := newSSHStoreLogServer(t, dataDir)

	client, err := s.buildSSHClient()
	require.NoError(t, err, "a file at the legacy path must not fail the boot")
	assert.Nil(t, client)
	assert.Empty(t, legacySSHStoreWarnings(t, buf), "a file is not a key directory")
}

func TestBuildSSHClient_LegacyPathUninspectable(t *testing.T) {
	// The data dir is a file, so stat of the legacy path fails with ENOTDIR:
	// an error other than not-exist, which is logged and the boot goes on.
	dataDir := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.WriteFile(dataDir, []byte("x"), 0o600))
	s, buf := newSSHStoreLogServer(t, dataDir)

	_, err := s.buildSSHClient()
	require.NoError(t, err, "a stat error must not fail the boot")

	warnings := legacySSHStoreWarnings(t, buf)
	require.Len(t, warnings, 2, "one cannot-inspect warning per legacy path")
	assert.Equal(t, filepath.Join(dataDir, "xkms-ssh"), warnings[0]["legacy_dir"])
	assert.Contains(t, warnings[0]["msg"], "cannot inspect")
}

func TestSSHSealedBackend_NoBarrier(t *testing.T) {
	s := newSSHStoreTestServer(t, "", config.XKMSConfig{})

	backend, err := s.sshSealedBackend()
	assert.Nil(t, backend)
	var notSealed *SSHStoreNotSealedError
	require.ErrorAs(t, err, &notSealed)
}

func TestResolveSSHKeyProvider_UnknownBackend(t *testing.T) {
	provider, err := resolveSSHKeyProvider(context.Background(), "no-such-backend")
	assert.Nil(t, provider)
	require.Error(t, err)
}

func TestSSHStoreErrorMessages(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		name  string
		err   error
		want  string
		cause error
	}{
		{name: "not sealed", err: &SSHStoreNotSealedError{}, want: "not barrier-sealed"},
		{name: "build", err: &SSHStoreBuildError{Op: "sealed storage", Err: cause}, want: "sealed storage: boom", cause: cause},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, tt.err.Error(), tt.want)
			if tt.cause != nil {
				assert.ErrorIs(t, tt.err, tt.cause)
			}
		})
	}
}
