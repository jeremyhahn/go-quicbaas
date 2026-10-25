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

// Package main: extended coverage for barrier.go — interactive seal
// prompt branches and edge cases in readSharesFile not already covered
// by barrier_test.go.
package main

import (
	"bytes"
	"context"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withStdin temporarily replaces os.Stdin for the duration of the test.
// Restores the original on cleanup. The replacement is the read end of
// an *os.File pipe whose write end has the supplied payload written and
// closed; this ensures bufio.NewReader sees EOF after the payload.
func withStdin(t *testing.T, payload string) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	if payload != "" {
		_, werr := w.WriteString(payload)
		require.NoError(t, werr)
	}
	require.NoError(t, w.Close())

	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})
}

// =============================================================================
// runBarrierSealCommand: interactive prompt branches
// =============================================================================

// TestBarrierSeal_InteractiveNo confirms "no" cancels without calling the SDK.
func TestBarrierSeal_InteractiveNo(t *testing.T) {
	defer resetBarrierFlags()
	withStdin(t, "no\n")

	var sealCalled atomic.Bool
	fc := &fakeBarrierClient{
		sealFunc: func(_ context.Context) error {
			sealCalled.Store(true)
			return nil
		},
	}
	defer withFakeBarrier(fc)()

	cmd := newBarrierSealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Seal cancelled")
	assert.False(t, sealCalled.Load(), "SDK BarrierSeal must not be called when user answers 'no'")
}

// TestBarrierSeal_InteractiveAnythingElse exercises the case-insensitive
// path: "MAYBE" is neither "yes" nor empty and is treated as cancellation.
func TestBarrierSeal_InteractiveAnythingElse(t *testing.T) {
	defer resetBarrierFlags()
	withStdin(t, "MAYBE\n")

	var sealCalled atomic.Bool
	fc := &fakeBarrierClient{
		sealFunc: func(_ context.Context) error {
			sealCalled.Store(true)
			return nil
		},
	}
	defer withFakeBarrier(fc)()

	cmd := newBarrierSealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Seal cancelled")
	assert.False(t, sealCalled.Load())
}

// TestBarrierSeal_InteractiveYes confirms "yes" reaches the SDK BarrierSeal.
func TestBarrierSeal_InteractiveYes(t *testing.T) {
	defer resetBarrierFlags()
	withStdin(t, "yes\n")

	var sealCalled atomic.Bool
	fc := &fakeBarrierClient{
		sealFunc: func(_ context.Context) error {
			sealCalled.Store(true)
			return nil
		},
	}
	defer withFakeBarrier(fc)()

	cmd := newBarrierSealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Barrier sealed successfully")
	assert.True(t, sealCalled.Load(), "SDK BarrierSeal must be called when user answers 'yes'")
}

// TestBarrierSeal_InteractiveEOF returns an error when stdin is closed
// without any input — exercises the "failed to read confirmation" path.
func TestBarrierSeal_InteractiveEOF(t *testing.T) {
	defer resetBarrierFlags()
	withStdin(t, "")

	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierSealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read confirmation")
}

// =============================================================================
// readSharesFile: error cases
// =============================================================================
