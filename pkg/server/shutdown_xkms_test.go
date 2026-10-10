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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

var errXKMSCloseFailed = errors.New("xkms close failed")

// withRecordingXKMSClose replaces the closeXKMSFn seam with one that counts
// its calls and returns closeErr.
func withRecordingXKMSClose(t *testing.T, closeErr error) *int {
	t.Helper()
	calls := 0
	orig := closeXKMSFn
	closeXKMSFn = func() error {
		calls++
		return closeErr
	}
	t.Cleanup(func() { closeXKMSFn = orig })
	return &calls
}

// TestServer_Shutdown_OwnedXKMS_IsReleased pins go-quicbaas#714: the
// process-global xkms service a server initialized holds key storage in that
// server's data dir, so Shutdown must release it. Left registered, the next
// server in the process reused it and wrote its OIDC signing key into the
// previous server's data dir.
func TestServer_Shutdown_OwnedXKMS_IsReleased(t *testing.T) {
	srv := withInMemoryServer(t)
	calls := withRecordingXKMSClose(t, nil)
	srv.ownsXKMS = true

	require.NoError(t, srv.Shutdown(context.Background()))
	assert.Equal(t, 1, *calls, "Shutdown must close the xkms service this server initialized")
	assert.False(t, srv.ownsXKMS, "the released service must not stay owned")

	require.NoError(t, srv.Shutdown(context.Background()))
	assert.Equal(t, 1, *calls, "a second Shutdown must not close the service again")
}

// TestServer_Shutdown_UnownedXKMS_IsLeftOpen pins that a server which found
// xkms already initialized does not close it: the service belongs to whoever
// initialized it.
func TestServer_Shutdown_UnownedXKMS_IsLeftOpen(t *testing.T) {
	srv := withInMemoryServer(t)
	calls := withRecordingXKMSClose(t, nil)

	require.NoError(t, srv.Shutdown(context.Background()))
	assert.Equal(t, 0, *calls, "Shutdown must not close an xkms service it did not initialize")
}

// TestServer_Shutdown_XKMSCloseError_IsReturned pins that a failed release
// surfaces in Shutdown's error rather than being dropped.
func TestServer_Shutdown_XKMSCloseError_IsReturned(t *testing.T) {
	srv := withInMemoryServer(t)
	withRecordingXKMSClose(t, errXKMSCloseFailed)
	srv.ownsXKMS = true

	err := srv.Shutdown(context.Background())
	var shutdownErr *api.ShutdownError
	require.ErrorAs(t, err, &shutdownErr)
	assert.Contains(t, shutdownErr.Errors, errXKMSCloseFailed)
}
