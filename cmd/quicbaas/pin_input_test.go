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
	"reflect"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
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

// TestResolvePINs_NoGlobalHoldsAPIN checks that the PIN is handed only to
// the request literal: no package global keeps a string copy of it after the
// command, whether it came from a file, stdin or argv.
func TestResolvePINs_NoGlobalHoldsAPIN(t *testing.T) {
	clearPINEnv(t)
	t.Setenv(barrierPassphraseEnv, "topsecret")

	fc, err := runBarrierInit(t, "USR123\n",
		"--so-pin-file", writePINFile(t, "so.pin", "SO123\n"), "--user-pin-file", "-")
	require.NoError(t, err)
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "SO123", fc.lastInitReq.SOPin)
	assert.Equal(t, "USR123", fc.lastInitReq.UserPin)
	assert.Empty(t, barrierSOPin, "a package global still holds the SO PIN")
	assert.Empty(t, barrierUserPin, "a package global still holds the user PIN")

	fc, err = runBarrierInit(t, "", "--so-pin", "argvso", "--user-pin", "argvuser")
	require.NoError(t, err)
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "argvso", fc.lastInitReq.SOPin)
	assert.Equal(t, "argvuser", fc.lastInitReq.UserPin)
	assert.Empty(t, barrierSOPin, "the argv SO PIN is still held by its global")
	assert.Empty(t, barrierUserPin, "the argv user PIN is still held by its global")
}

// TestResolvePINs_ReleaseWipesThePINs checks that resolvePINs returns the
// PINs in protected memory, clears the argv targets, and that release()
// destroys the protected buffers; with no source an input has no PIN, and a
// failed resolve still clears the targets.
func TestResolvePINs_ReleaseWipesThePINs(t *testing.T) {
	clearPINEnv(t)
	defer resetBarrierFlags()

	cmd := &cobra.Command{Use: "t"}
	var so, user string
	addPINFlags(cmd, "", soPINInput(&so), userPINInput(&user))
	require.NoError(t, cmd.Flags().Set(soPINFileFlag, writePINFile(t, "so", "S1\n")))
	require.NoError(t, cmd.Flags().Set(userPINFileFlag, "-"))
	cmd.SetIn(strings.NewReader("U1\n"))
	pins, release, err := resolvePINs(cmd, "t", soPINInput(&so), userPINInput(&user))
	require.NoError(t, err)
	assert.True(t, pins.has(0) && pins.has(1))
	assert.Equal(t, "S1", pins.field(0))
	assert.Equal(t, "U1", pins.field(1))
	assert.True(t, so == "" && user == "", "an argv target still holds a PIN")
	release()
	assert.True(t, pins[0].Size() == 0 && pins[1].Size() == 0, "release left a PIN in protected memory")

	// No source: the input has no PIN.
	cmd2 := &cobra.Command{Use: "t"}
	addPINFlags(cmd2, "", soPINInput(&so))
	pins2, release2, err := resolvePINs(cmd2, "t", soPINInput(&so))
	require.NoError(t, err)
	defer release2()
	assert.False(t, pins2.has(0))
	assert.Equal(t, "", pins2.field(0))

	// Failure: the SO PIN resolves, the user PIN file is empty.
	cmd3 := &cobra.Command{Use: "t"}
	addPINFlags(cmd3, "", soPINInput(&so), userPINInput(&user))
	require.NoError(t, cmd3.Flags().Set(soPINFileFlag, writePINFile(t, "so3", "S0\n")))
	require.NoError(t, cmd3.Flags().Set(userPINFileFlag, writePINFile(t, "user3", "")))
	so = "argvso"
	_, release3, err := resolvePINs(cmd3, "t", soPINInput(&so), userPINInput(&user))
	require.Error(t, err)
	assert.NotPanics(t, release3)
	assert.True(t, so == "" && user == "", "a failed resolve left a PIN in an argv target")
}

// TestResolvePINs_GoQrdbRequestPINFieldsAreStrings pins the upstream seam
// resolvedPINs.field exists for: every go-qrdb request PIN field the CLI
// fills is a string, so one unwipeable copy per call is unavoidable here.
// When go-qrdb types these fields as bytes, this test fails: delete
// resolvedPINs.field, hand the protected bytes to the request, and delete
// this test.
func TestResolvePINs_GoQrdbRequestPINFieldsAreStrings(t *testing.T) {
	cases := []struct {
		req    any
		fields []string
	}{
		{qrdbtransport.BarrierInitializeRequest{}, []string{"SOPin", "UserPin"}},
		{qrdbtransport.BarrierInitializeShamirRequest{}, []string{"SOPin", "UserPin"}},
		{qrdbtransport.BarrierRekeyRequest{}, []string{"SOPin"}},
		{qrdbtransport.BarrierGenerateRecoveryKeysRequest{}, []string{"SOPin"}},
		{qrdbtransport.SignCSRInitRequest{}, []string{"SOPin"}},
		{qrdbtransport.CeremonyInitializeRequest{}, []string{"SOPin", "UserPin"}},
	}
	for _, c := range cases {
		rt := reflect.TypeOf(c.req)
		for _, name := range c.fields {
			f, ok := rt.FieldByName(name)
			require.True(t, ok, "%s has no field %s", rt.Name(), name)
			assert.Equal(t, reflect.String, f.Type.Kind(),
				"%s.%s is no longer a string: go-qrdb now carries the PIN as bytes; delete resolvedPINs.field and pass the protected bytes to the request", rt.Name(), name)
		}
	}
}
