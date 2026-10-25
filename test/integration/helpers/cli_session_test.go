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

//go:build integration

package helpers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// The session WriteCLISession saves must be the one the CLI's own token
// store loads and accepts, at the path the CLI reads with HOME=home.
func TestWriteCLISession_CLITokenStoreLoadsIt(t *testing.T) {
	home := t.TempDir()
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	path := CLISessionPath(home)
	require.Equal(t, filepath.Join(home, ".quicbaas", "token"), path)

	require.NoError(t, WriteCLISession(path, "bearer-1", expiresAt))

	token, err := cli.LoadToken(path)
	require.NoError(t, err)
	require.Equal(t, "bearer-1", token.AccessToken)
	require.Equal(t, "Bearer", token.TokenType)
	require.True(t, token.ExpiresAt.Equal(expiresAt))
	require.True(t, cli.IsTokenValid(token))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a session holds a bearer: owner-only")
}

func TestWriteCLISession_UnwritablePathIsTyped(t *testing.T) {
	// A regular file where the session directory must go makes the
	// directory impossible to create.
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".quicbaas"), nil, 0o600))
	path := CLISessionPath(home)

	err := WriteCLISession(path, "bearer-1", time.Now().Add(time.Hour))

	var sessionErr *CLISessionError
	require.ErrorAs(t, err, &sessionErr)
	require.Equal(t, "create directory", sessionErr.Step)
	require.Equal(t, path, sessionErr.Path)
	require.Error(t, sessionErr.Unwrap())
}
