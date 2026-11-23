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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSharesFile writes body to a temp file and returns its path.
func writeSharesFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shares.txt")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// TestResolveShares_NoGlobalHoldsAShare runs the share verbs end to end and
// checks the request carries the shares while the --share package globals
// are cleared once read.
func TestResolveShares_NoGlobalHoldsAShare(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	run := func(t *testing.T, cmdArgs []string, recovery bool) {
		t.Helper()
		resetBarrierFlags()
		cmd := newBarrierUnsealSharesCommand()
		if recovery {
			cmd = newBarrierRecoveryRecoverCommand()
		}
		cmd.SetArgs(cmdArgs)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		require.NoError(t, cmd.Execute())
		assert.Nil(t, barrierShares, "barrierShares still holds the shares")
		assert.Nil(t, barrierRecoveryShares, "barrierRecoveryShares still holds the keys")
	}

	run(t, []string{"--share", "a", "--share", "b"}, false)
	require.NotNil(t, fc.lastUnsealSharesReq)
	assert.Equal(t, []string{"a", "b"}, fc.lastUnsealSharesReq.Shares)

	run(t, []string{"--share", "k1", "--share", "k2"}, true)
	require.NotNil(t, fc.lastRecoverReq)
	assert.Equal(t, []string{"k1", "k2"}, fc.lastRecoverReq.Keys)

	path := writeSharesFile(t, "f1\n# comment\nf2\n")
	run(t, []string{"--shares-file", path}, false)
	assert.Equal(t, []string{"f1", "f2"}, fc.lastUnsealSharesReq.Shares)
	run(t, []string{"--shares-file", path}, true)
	assert.Equal(t, []string{"f1", "f2"}, fc.lastRecoverReq.Keys)
}

// TestResolveShares_ReleaseWipesTheShares checks every share lands in its
// own protected buffer, release destroys them all, and every path clears
// the argv slice and returns a usable release.
func TestResolveShares_ReleaseWipesTheShares(t *testing.T) {
	const op = "barrier unseal-shares"

	path := writeSharesFile(t, "# header\n  s1  \n\ns2\n# mid\ns3")
	var none []string
	shares, release, err := resolveShares(&none, path, op)
	require.NoError(t, err)
	require.Len(t, shares, 3)
	assert.Equal(t, []string{"s1", "s2", "s3"}, shares.fields())
	release()
	for i, b := range shares {
		assert.Zero(t, b.Size(), "share %d survived release", i)
	}

	v := []string{"x", "y"}
	shares, release, err = resolveShares(&v, "", op)
	require.NoError(t, err)
	assert.Nil(t, v, "the argv slice still holds the shares")
	require.Len(t, shares, 2)
	assert.Equal(t, []string{"x", "y"}, shares.fields())
	release()
	for i, b := range shares {
		assert.Zero(t, b.Size(), "share %d survived release", i)
	}

	empty := writeSharesFile(t, "# only a comment\n\n")
	cases := []struct {
		name   string
		argv   []string
		file   string
		target any
	}{
		{"both inputs", []string{"x"}, path, new(*BarrierShareInputError)},
		{"neither input", nil, "", new(*BarrierShareInputError)},
		{"missing file", nil, filepath.Join(t.TempDir(), "absent"), new(*BarrierSharesFileError)},
		{"empty file", nil, empty, new(*BarrierSharesFileEmptyError)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			argv := c.argv
			got, rel, err := resolveShares(&argv, c.file, op)
			require.Error(t, err)
			require.ErrorAs(t, err, c.target)
			assert.Nil(t, got)
			require.NotNil(t, rel)
			assert.NotPanics(t, rel)
			assert.Nil(t, argv, "the argv slice survived an error")
		})
	}
}

// TestReadSharesFile_OverLimitIsRefused checks the read cap: one byte over
// is refused with an error naming the limit, exactly the limit is accepted.
func TestReadSharesFile_OverLimitIsRefused(t *testing.T) {
	const op = "barrier unseal-shares"
	line := strings.Repeat("s", 1023) + "\n"
	atLimit := strings.Repeat(line, maxSharesFileBytes/len(line))
	require.Len(t, atLimit, maxSharesFileBytes)

	shares, release, err := readSharesFile(writeSharesFile(t, atLimit), op)
	require.NoError(t, err)
	defer release()
	assert.Len(t, shares, maxSharesFileBytes/len(line))

	_, rel, err := readSharesFile(writeSharesFile(t, atLimit+"x"), op)
	require.Error(t, err)
	require.NotNil(t, rel)
	var typed *BarrierSharesFileError
	require.ErrorAs(t, err, &typed)
	assert.Contains(t, err.Error(), "65536-byte limit")
}

// TestReadSharesFile_FromStdin reads "-" from standard input.
func TestReadSharesFile_FromStdin(t *testing.T) {
	withStdin(t, "s1\n# c\ns2\n")
	shares, release, err := readSharesFile("-", "barrier unseal-shares")
	require.NoError(t, err)
	defer release()
	assert.Equal(t, []string{"s1", "s2"}, shares.fields())
}

// TestResolveShares_GoQrdbRequestShareFieldsAreStrings pins the upstream
// seam resolvedShares.fields exists for: every go-qrdb request share field
// the CLI fills is a []string, so one unwipeable copy per share per call is
// unavoidable here. When go-qrdb types these fields as bytes, this test
// fails: delete resolvedShares.fields, hand the protected bytes to the
// request, and delete this test.
func TestResolveShares_GoQrdbRequestShareFieldsAreStrings(t *testing.T) {
	cases := []struct {
		req   any
		field string
	}{
		{qrdbtransport.BarrierUnsealSharesRequest{}, "Shares"},
		{qrdbtransport.BarrierGenerateRootTokenRequest{}, "Shares"},
		{qrdbtransport.BarrierRecoverWithKeysRequest{}, "Keys"},
	}
	for _, c := range cases {
		rt := reflect.TypeOf(c.req)
		f, ok := rt.FieldByName(c.field)
		require.True(t, ok, "%s has no field %s", rt.Name(), c.field)
		ok = f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.String
		assert.True(t, ok,
			"%s.%s is no longer a []string: go-qrdb now carries shares as bytes; delete resolvedShares.fields and pass the protected bytes to the request", rt.Name(), c.field)
	}
}
