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

// Package main provides flag-parsing tests for the canonical
// 'quicbaas bootstrap' command tree.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock BootstrapClient
// =============================================================================

type fakeBootstrapClient struct {
	autoFunc func(ctx context.Context, req *BootstrapAutoRequest) (*BootstrapAutoResponse, error)
	lastReq  *BootstrapAutoRequest
}

func (f *fakeBootstrapClient) BootstrapAuto(ctx context.Context, req *BootstrapAutoRequest) (*BootstrapAutoResponse, error) {
	f.lastReq = req
	if f.autoFunc != nil {
		return f.autoFunc(ctx, req)
	}
	return &BootstrapAutoResponse{
		Method:      req.Method,
		CABundlePEM: "-----BEGIN CERTIFICATE-----\nstub\n-----END CERTIFICATE-----\n",
		SPKIPin:     "sha256/abcdef",
	}, nil
}

// withFakeBootstrap installs fc as the active bootstrap factory and
// returns a restore func.
func withFakeBootstrap(fc *fakeBootstrapClient) func() {
	orig := bootstrapClientFactory
	bootstrapClientFactory = func() (BootstrapClient, error) { return fc, nil }
	return func() { bootstrapClientFactory = orig }
}

// resetBootstrapFlags zeroes the package-level flag variables.
func resetBootstrapFlags() {
	bootstrapMethod = "auto"
	bootstrapServerURL = ""
	bootstrapBundleOut = ""
	bootstrapOutputFmt = "table"
}

// =============================================================================
// Command tree shape
// =============================================================================

// TestNewBootstrapCommand_Shape verifies the canonical surface.
func TestNewBootstrapCommand_Shape(t *testing.T) {
	cmd := newBootstrapCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "bootstrap", cmd.Use)

	expected := map[string]bool{
		"auto": false,
	}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for verb, present := range expected {
		assert.True(t, present, "expected 'bootstrap %s' subcommand", verb)
	}
}

// =============================================================================
// auto
// =============================================================================

// TestBootstrapAuto_RejectsInvalidMethod returns a typed error.
func TestBootstrapAuto_RejectsInvalidMethod(t *testing.T) {
	defer resetBootstrapFlags()
	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "telepathy"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BootstrapInvalidMethodError
	assert.ErrorAs(t, err, &typed)
}

// TestBootstrapAuto_AcceptsCanonicalMethods verifies all canonical methods.
func TestBootstrapAuto_AcceptsCanonicalMethods(t *testing.T) {
	for _, method := range []string{"auto", "dane", "noise", "spki", "direct"} {
		t.Run(method, func(t *testing.T) {
			defer resetBootstrapFlags()
			fc := &fakeBootstrapClient{}
			defer withFakeBootstrap(fc)()

			cmd := newBootstrapAutoCommand()
			cmd.SetArgs([]string{"--method", method})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)

			require.NoError(t, cmd.Execute())
			require.NotNil(t, fc.lastReq)
			assert.Equal(t, method, fc.lastReq.Method)
		})
	}
}

// TestBootstrapAuto_WritesBundleFile verifies --bundle-output.
func TestBootstrapAuto_WritesBundleFile(t *testing.T) {
	defer resetBootstrapFlags()
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "ca.pem")

	fc := &fakeBootstrapClient{}
	defer withFakeBootstrap(fc)()

	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "auto", "--bundle-output", bundlePath})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	got, err := os.ReadFile(bundlePath)
	require.NoError(t, err)
	assert.Contains(t, string(got), "BEGIN CERTIFICATE")
}

// TestBootstrapAuto_PrintsBundleToStdout when no --bundle-output is given.
func TestBootstrapAuto_PrintsBundleToStdout(t *testing.T) {
	defer resetBootstrapFlags()
	fc := &fakeBootstrapClient{}
	defer withFakeBootstrap(fc)()

	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "auto"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "BOOTSTRAP RESULT")
	assert.Contains(t, out.String(), "BEGIN CERTIFICATE")
}

// TestBootstrapAuto_FactoryError surfaces the typed pending-SDK error
// from the default factory when the SDK has no bootstrap surface.
func TestBootstrapAuto_FactoryError(t *testing.T) {
	defer resetBootstrapFlags()
	orig := bootstrapClientFactory
	defer func() { bootstrapClientFactory = orig }()

	wantErr := errors.New("factory failure")
	bootstrapClientFactory = func() (BootstrapClient, error) { return nil, wantErr }

	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "auto"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory failure")
}

// TestBootstrapAuto_PendingSDK exercises the default factory's stub.
func TestBootstrapAuto_PendingSDK(t *testing.T) {
	defer resetBootstrapFlags()
	// Use the production factory (default placeholder).
	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "auto"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BootstrapPendingSDKError
	assert.ErrorAs(t, err, &typed)
}

// =============================================================================
// Errors
// =============================================================================

// TestBootstrapInvalidMethodError_Message verifies the error string.
func TestBootstrapInvalidMethodError_Message(t *testing.T) {
	err := &BootstrapInvalidMethodError{Got: "telepathy"}
	assert.Contains(t, err.Error(), "telepathy")
	assert.Contains(t, err.Error(), "auto, dane, noise, spki, direct")
}

// TestBootstrapPendingSDKError_Message verifies the error string.
func TestBootstrapPendingSDKError_Message(t *testing.T) {
	err := &BootstrapPendingSDKError{Op: "auto"}
	assert.Contains(t, err.Error(), "auto")
	assert.Contains(t, err.Error(), "SDK BootstrapClient not yet wired")
}
