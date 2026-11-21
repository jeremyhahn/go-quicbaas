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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearPINEnv(t *testing.T) {
	t.Helper()
	for _, e := range []string{soPINEnv, userPINEnv, barrierSOPINEnv, barrierPassphraseEnv} {
		t.Setenv(e, "")
	}
}

func writePINFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func runBarrierInit(t *testing.T, stdin string, args ...string) (*fakeBarrierClient, error) {
	t.Helper()
	fc := &fakeBarrierClient{}
	t.Cleanup(withFakeBarrier(fc))
	t.Cleanup(resetBarrierFlags)
	cmd := newBarrierInitCommand()
	cmd.SetArgs(append([]string{"--barrier-strategy", "passphrase"}, args...))
	cmd.SetIn(strings.NewReader(stdin))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return fc, cmd.Execute()
}

// TestBarrierInit_PINsFromFiles is #226: both PINs and the passphrase reach
// the request with no secret on argv — one PIN from a file, one from stdin.
func TestBarrierInit_PINsFromFiles(t *testing.T) {
	clearPINEnv(t)
	t.Setenv(barrierPassphraseEnv, "topsecret")
	so := writePINFile(t, "so.pin", "SO123\n")
	fc, err := runBarrierInit(t, "USR123\n", "--so-pin-file", so, "--user-pin-file", "-")
	require.NoError(t, err)
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "SO123", fc.lastInitReq.SOPin)
	assert.Equal(t, "USR123", fc.lastInitReq.UserPin)
}

// TestBarrierInit_PINsFromEnv adopts the variables quicbaasd init reads.
func TestBarrierInit_PINsFromEnv(t *testing.T) {
	clearPINEnv(t)
	t.Setenv(soPINEnv, "envso")
	t.Setenv(userPINEnv, "envuser")
	fc, err := runBarrierInit(t, "", "--passphrase", "x", "--so-pin", "argv")
	require.NoError(t, err)
	assert.Equal(t, "envso", fc.lastInitReq.SOPin, "env wins over argv")
	assert.Equal(t, "envuser", fc.lastInitReq.UserPin)
}

// TestBarrierInit_OneStdinClaim refuses two secrets on the one stdin.
func TestBarrierInit_OneStdinClaim(t *testing.T) {
	clearPINEnv(t)
	_, err := runBarrierInit(t, "pp\n", "--passphrase-stdin",
		"--so-pin-file", "-", "--user-pin", "u")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both read standard input")

	_, err = runBarrierInit(t, "pp\n", "--passphrase", "x",
		"--so-pin-file", "-", "--user-pin-file", "-")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both read standard input")
}

func TestBarrierInit_EmptyPINFile(t *testing.T) {
	clearPINEnv(t)
	_, err := runBarrierInit(t, "", "--passphrase", "x",
		"--so-pin-file", writePINFile(t, "e", ""), "--user-pin", "u")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--so-pin-file is empty")
}

// TestBarrierInit_NoneIgnoresLeftoverPINEnv: an exported PIN must not fail
// --barrier-strategy=none, while a typed file flag still does.
func TestBarrierInit_NoneIgnoresLeftoverPINEnv(t *testing.T) {
	clearPINEnv(t)
	t.Setenv(soPINEnv, "left")
	defer resetBarrierFlags()
	fc2 := &fakeBarrierClient{}
	defer withFakeBarrier(fc2)()
	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{"--barrier-strategy", "none"})
	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc2.lastInitReq)
	assert.Empty(t, fc2.lastInitReq.SOPin)

	resetBarrierFlags()
	cmd = newBarrierInitCommand()
	cmd.SetArgs([]string{"--barrier-strategy", "none", "--so-pin-file", "x"})
	require.Error(t, cmd.Execute())
}

// TestPINVerbs_HaveFileFlags holds every PIN-bearing verb to the non-argv
// surface (#226): each PIN flag has its -file sibling.
func TestPINVerbs_HaveFileFlags(t *testing.T) {
	for name, c := range map[string]*cobra.Command{
		"barrier init":              newBarrierInitCommand(),
		"barrier init-shamir":       newBarrierInitShamirCommand(),
		"barrier rekey":             newBarrierRekeyCommand(),
		"barrier recovery generate": newBarrierRecoveryGenerateCommand(),
		"init initialize":           newInitInitializeCommand(),
		"init sign-csr":             newInitSignCSRCommand(),
	} {
		for _, pin := range []string{soPINFlag, userPINFlag} {
			if c.Flags().Lookup(pin) == nil {
				continue
			}
			assert.NotNil(t, c.Flags().Lookup(pin+"-file"), "%s: --%s has no -file path", name, pin)
		}
		assert.NotContains(t, c.Long, "--so-pin \"", name)
		assert.NotContains(t, c.Long, "--so-pin SO", name)
	}
}

func TestResolvePINs_RekeyFromFile(t *testing.T) {
	clearPINEnv(t)
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()
	cmd := newBarrierRekeyCommand()
	cmd.SetArgs([]string{"--so-pin-file", writePINFile(t, "so", "S\r\n")})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "S", fc.lastRekeyReq.SOPin)
}
