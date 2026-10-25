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

package main

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// sealPINProbe records the SO PIN bound to the context BarrierSeal is
// called with, and the context itself so a test can check it was released.
type sealPINProbe struct {
	called bool
	pin    string
	bound  bool
	ctx    context.Context
}

func (p *sealPINProbe) client() *fakeBarrierClient {
	return &fakeBarrierClient{sealFunc: func(ctx context.Context) error {
		p.called = true
		p.ctx = ctx
		pin, ok, err := qrdbtransport.SOPINFromContext(ctx)
		if err != nil {
			return err
		}
		p.pin, p.bound = string(pin), ok
		return nil
	}}
}

// runSeal executes `barrier seal` with args and stdin against the probe.
func runSeal(t *testing.T, probe *sealPINProbe, stdin string, args ...string) error {
	t.Helper()
	t.Cleanup(resetBarrierFlags)
	t.Cleanup(withFakeBarrier(probe.client()))

	cmd := newBarrierSealCommand()
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd.Execute()
}

func TestBarrierSeal_ForwardsTheSOPIN(t *testing.T) {
	cases := []struct {
		name  string
		env   string
		stdin string
		args  []string
		want  string
	}{
		{name: "stdin", stdin: "stdin-pin\n", args: []string{"--so-pin-stdin", "-y"}, want: "stdin-pin"},
		{name: "env", env: "env-pin", args: []string{"-y"}, want: "env-pin"},
		{name: "flag", args: []string{"--so-pin", "flag-pin", "-y"}, want: "flag-pin"},
		{
			name: "stdin wins over env and flag", env: "env-pin", stdin: "stdin-pin",
			args: []string{"--so-pin-stdin", "--so-pin", "flag-pin", "-y"}, want: "stdin-pin",
		},
		{name: "env wins over flag", env: "env-pin", args: []string{"--so-pin", "flag-pin", "-y"}, want: "env-pin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(barrierSOPINEnv, tc.env)
			probe := &sealPINProbe{}

			require.NoError(t, runSeal(t, probe, tc.stdin, tc.args...))
			require.True(t, probe.called)
			assert.True(t, probe.bound, "the SO PIN must be bound to the seal call")
			assert.Equal(t, tc.want, probe.pin)
		})
	}
}

func TestBarrierSeal_NoSOPINBindsNone(t *testing.T) {
	t.Setenv(barrierSOPINEnv, "")
	probe := &sealPINProbe{}

	require.NoError(t, runSeal(t, probe, "", "-y"))
	require.True(t, probe.called, "a seal without a PIN backend needs no PIN")
	assert.False(t, probe.bound)
}

func TestBarrierSeal_SOPINIsReleasedAfterTheCall(t *testing.T) {
	t.Setenv(barrierSOPINEnv, "")
	probe := &sealPINProbe{}

	require.NoError(t, runSeal(t, probe, "pin\n", "--so-pin-stdin", "-y"))
	require.True(t, probe.bound)

	_, stillBound, err := qrdbtransport.SOPINFromContext(probe.ctx)
	require.NoError(t, err)
	assert.False(t, stillBound, "the SDK's copy of the PIN must be destroyed once seal returns")
}

func TestBarrierSeal_SOPINStdinOverLimitDoesNotSeal(t *testing.T) {
	t.Setenv(barrierSOPINEnv, "")
	probe := &sealPINProbe{}

	err := runSeal(t, probe, strings.Repeat("a", maxPassphraseBytes+1), "--so-pin-stdin", "-y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	assert.False(t, probe.called, "an unreadable SO PIN must not reach the server")
}

func TestBarrierSeal_ServerRefusalSurfaces(t *testing.T) {
	t.Setenv(barrierSOPINEnv, "")
	t.Cleanup(resetBarrierFlags)
	t.Cleanup(withFakeBarrier(&fakeBarrierClient{sealFunc: func(context.Context) error {
		return errors.Newf(errors.ErrForbidden, "barrier.Seal", "SO PIN is invalid")
	}}))

	cmd := newBarrierSealCommand()
	cmd.SetArgs([]string{"--so-pin-stdin", "-y"})
	cmd.SetIn(strings.NewReader("wrong\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.NotContains(t, out.String(), "Barrier sealed successfully")
}

func TestReadSOPIN_UnboundableDescriptorIsRefused(t *testing.T) {
	withoutPollSupport(t)
	r, cleanup := stalledDescriptor(t)
	defer cleanup()

	buf, err := readSOPIN(r, "barrier.seal", 10*time.Second)
	require.Error(t, err)
	assert.Nil(t, buf)
	assert.Contains(t, err.Error(), runtime.GOOS)
	assert.Contains(t, err.Error(), barrierSOPINEnv,
		"the error must point at an input path that still works")
	assert.NotContains(t, err.Error(), "argv",
		"the error must not steer the operator to the process list")
}

func TestReadSOPIN_ReadsOneLine(t *testing.T) {
	buf, err := readSOPIN(strings.NewReader("line-pin\nrest"), "barrier.seal", time.Second)
	require.NoError(t, err)
	defer buf.Destroy()
	require.NoError(t, buf.WithBytes(func(pin []byte) error {
		assert.Equal(t, "line-pin", string(pin))
		return nil
	}))
}

func TestBindSOPIN_NoPINLeavesTheContextAlone(t *testing.T) {
	ctx := context.Background()
	got, release, err := bindSOPIN(ctx, "barrier.seal", nil)
	require.NoError(t, err)
	defer release()

	assert.Equal(t, ctx, got)
	_, ok, err := qrdbtransport.SOPINFromContext(got)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestBindSOPIN_BindsAndReleases(t *testing.T) {
	pin, err := protectExposedPassphrase("bound-pin", "barrier.seal")
	require.NoError(t, err)
	defer pin.Destroy()

	ctx, release, err := bindSOPIN(context.Background(), "barrier.seal", pin)
	require.NoError(t, err)

	got, ok, err := qrdbtransport.SOPINFromContext(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "bound-pin", string(got))

	release()
	_, ok, err = qrdbtransport.SOPINFromContext(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "release must destroy the bound PIN")
}

func TestAddSOPINFlags_RegistersBothPaths(t *testing.T) {
	cmd := newBarrierSealCommand()

	stdin := cmd.Flags().Lookup(soPINStdinFlag)
	require.NotNil(t, stdin, "--so-pin-stdin must be registered")
	assert.Contains(t, stdin.Usage, "standard input")

	flag := cmd.Flags().Lookup(soPINFlag)
	require.NotNil(t, flag, "--so-pin stays accepted")
	assert.Contains(t, flag.Usage, soPINExposureWarning)
}
