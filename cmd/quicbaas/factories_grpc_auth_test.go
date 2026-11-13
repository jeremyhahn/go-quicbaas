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

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGRPCClientOptions_AttachStoredBearer checks that the shared CLI gRPC
// client carries the stored session's platform bearer, and adds nothing when
// there is no session.
func TestGRPCClientOptions_AttachStoredBearer(t *testing.T) {
	origTokenFile := authTokenFile
	origTimeout := globalTimeout
	origDataDir := dataDir
	defer func() {
		authTokenFile = origTokenFile
		globalTimeout = origTimeout
		dataDir = origDataDir
	}()

	globalTimeout = 5 * time.Second
	dataDir = t.TempDir()

	authTokenFile = filepath.Join(t.TempDir(), "missing.json")
	without := grpcClientOptions()

	authTokenFile = writeValidTokenFile(t)
	with := grpcClientOptions()

	require.Len(t, with, len(without)+1,
		"a stored session adds exactly the bearer metadata option")

	// The gRPC connection is lazy, so no server is needed.
	client, err := createGRPCClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	_ = client.Close()
}
