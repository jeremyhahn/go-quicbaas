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

// Package main: coverage for serveChannelStdio's early-exit branches.
package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestServeChannelStdio_EOFOnEmptyStdin closes stdin immediately so the
// decoder returns EOF and serveChannelStdio returns nil.
func TestServeChannelStdio_EOFOnEmptyStdin(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})

	require.NoError(t, serveChannelStdio(context.Background()))
}
