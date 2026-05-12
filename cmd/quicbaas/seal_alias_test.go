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

// Package main provides tests that verify 'quicbaas seal …' continues
// to work as a deprecated alias for 'quicbaas barrier …' for one
// release, and that each alias call prints a deprecation warning to
// stderr while forwarding to the new path.
package main

import (
	"bytes"
	"context"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Alias command tree shape
// =============================================================================

// TestSealAlias_GroupShape verifies the deprecated 'seal' command group
// still exposes the expected verbs.
func TestSealAlias_GroupShape(t *testing.T) {
	cmd := newSealCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "seal", cmd.Use)

	expected := map[string]bool{
		"status": false,
		"init":   false,
		"unseal": false,
		"seal":   false,
		"rekey":  false,
	}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for verb, present := range expected {
		assert.True(t, present, "expected 'seal %s' subcommand on alias", verb)
	}
}

// =============================================================================
// Forwarding + deprecation warning
// =============================================================================

// TestSealAlias_StatusForwardsToBarrier verifies that 'quicbaas seal
// status' (a) prints a deprecation warning to stderr and (b) forwards
// to the new BarrierClient.BarrierStatus.
func TestSealAlias_StatusForwardsToBarrier(t *testing.T) {
	defer resetBarrierFlags()

	called := false
	defer withFakeBarrier(&fakeBarrierClient{
		statusFunc: func(_ context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
			called = true
			return &qrdbtransport.BarrierStatusResponse{Sealed: false, Strategy: "shamir"}, nil
		},
	})()

	cmd := newSealStatusCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.Execute())
	assert.True(t, called, "alias must forward to BarrierClient.BarrierStatus")

	// Deprecation warning lands on stderr.
	assert.Contains(t, stderr.String(), "WARNING: 'quicbaas seal status' is deprecated")
	assert.Contains(t, stderr.String(), "use 'quicbaas barrier status'")
	// stdout is the new barrier output (table form).
	assert.Contains(t, stdout.String(), "BARRIER STATUS")
}

// TestSealAlias_InitForwardsToBarrier verifies that 'quicbaas seal init'
// forwards to BarrierClient.BarrierInitialize and warns.
func TestSealAlias_InitForwardsToBarrier(t *testing.T) {
	defer resetBarrierFlags()

	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newSealInitCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--secret", "topsecret"})

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "topsecret", fc.lastInitReq.Secret)
	assert.Contains(t, stderr.String(), "WARNING: 'quicbaas seal init' is deprecated")
	assert.Contains(t, stderr.String(), "use 'quicbaas barrier init'")
}

// TestSealAlias_UnsealForwardsToBarrier verifies 'quicbaas seal unseal'.
func TestSealAlias_UnsealForwardsToBarrier(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newSealUnsealCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--secret", "x"})

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealReq)
	assert.Equal(t, "x", fc.lastUnsealReq.Secret)
	assert.Contains(t, stderr.String(), "WARNING: 'quicbaas seal unseal' is deprecated")
}

// TestSealAlias_SealForwardsToBarrier verifies 'quicbaas seal seal'.
func TestSealAlias_SealForwardsToBarrier(t *testing.T) {
	defer resetBarrierFlags()
	called := false
	defer withFakeBarrier(&fakeBarrierClient{
		sealFunc: func(_ context.Context) error {
			called = true
			return nil
		},
	})()

	cmd := newSealSealCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--assume-yes"})

	require.NoError(t, cmd.Execute())
	assert.True(t, called, "alias must forward to BarrierClient.BarrierSeal")
	assert.Contains(t, stderr.String(), "WARNING: 'quicbaas seal seal' is deprecated")
}

// TestSealAlias_RekeyForwardsToBarrier verifies 'quicbaas seal rekey'.
func TestSealAlias_RekeyForwardsToBarrier(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newSealRekeyCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--threshold", "2", "--total", "3"})

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRekeyReq)
	assert.Equal(t, 2, fc.lastRekeyReq.Threshold)
	assert.Equal(t, 3, fc.lastRekeyReq.Total)
	assert.Contains(t, stderr.String(), "WARNING: 'quicbaas seal rekey' is deprecated")
}
