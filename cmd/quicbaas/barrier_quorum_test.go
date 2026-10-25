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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeBarrierCmd runs cmd with args and returns its combined output.
func executeBarrierCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

// writeQuorumFile writes data to a file under t.TempDir and returns its path.
func writeQuorumFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestBarrierRekey_StartsARoundOnTheDomain(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	out, err := executeBarrierCmd(t, newBarrierRekeyCommand(),
		"--domain", "recovery", "--threshold", "3", "--policy", "designated", "--so-pin", "SO")
	require.NoError(t, err)
	require.NotNil(t, fc.lastRekeyStart)
	assert.Equal(t, qrdbtransport.RekeyStartRequest{
		Domain: qrdbtransport.DomainRecovery, Threshold: 3, Policy: "designated", SOPin: "SO",
	}, *fc.lastRekeyStart)
	assert.Contains(t, out, "QUORUM ROUND OPENED")
}

func TestBarrierRekey_RejectsBadInputBeforeDialling(t *testing.T) {
	cases := map[string]struct {
		args    []string
		wantErr string
	}{
		"threshold too low": {[]string{"--threshold", "1"}, "--threshold must be >= 2"},
		"unknown domain":    {[]string{"--domain", "root", "--threshold", "3"}, "--domain must be"},
		"removed --total":   {[]string{"--threshold", "3", "--total", "5"}, "unknown flag: --total"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer resetBarrierFlags()
			fc := &fakeBarrierClient{}
			defer withFakeBarrier(fc)()

			_, err := executeBarrierCmd(t, newBarrierRekeyCommand(), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Nil(t, fc.lastRekeyStart)
		})
	}
}

func TestBarrierOfficerRevoke_StartsARoundWithoutTheHolder(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{
		officerRevokeFunc: func(_ context.Context, req *qrdbtransport.OfficerRevokeRequest) (*qrdbtransport.QuorumRound, error) {
			return &qrdbtransport.QuorumRound{Domain: req.Domain, Op: qrdbtransport.OpRekey, Remove: []string{req.Username}}, nil
		},
	}
	defer withFakeBarrier(fc)()

	out, err := executeBarrierCmd(t, newBarrierOfficerRevokeCommand(), "--username", "bob", "--threshold", "2")
	require.NoError(t, err)
	require.NotNil(t, fc.lastOfficerRevoke)
	assert.Equal(t, qrdbtransport.DomainUnseal, fc.lastOfficerRevoke.Domain)
	assert.Equal(t, "bob", fc.lastOfficerRevoke.Username)
	assert.Equal(t, 2, fc.lastOfficerRevoke.Threshold)
	assert.Contains(t, out, "Removing:")
}

func TestBarrierOfficerRevoke_RejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		args    []string
		wantErr string
	}{
		"no username":        {nil, "--username is required"},
		"negative threshold": {[]string{"--username", "bob", "--threshold", "-1"}, "--threshold must be >= 0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer resetBarrierFlags()
			fc := &fakeBarrierClient{}
			defer withFakeBarrier(fc)()

			_, err := executeBarrierCmd(t, newBarrierOfficerRevokeCommand(), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Nil(t, fc.lastOfficerRevoke)
		})
	}
}

func TestBarrierQuorumBegin_SendsTheHolderAndFactor(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	_, err := executeBarrierCmd(t, newBarrierQuorumBeginCommand(),
		"--op", qrdbtransport.OpRootToken, "--username", "alice", "--factor", "mtls")
	require.NoError(t, err)
	require.NotNil(t, fc.lastQuorumBegin)
	assert.Equal(t, qrdbtransport.QuorumBeginRequest{
		Domain: qrdbtransport.DomainUnseal, Op: qrdbtransport.OpRootToken, Username: "alice", Factor: "mtls",
	}, *fc.lastQuorumBegin)
}

func TestBarrierQuorumBegin_RequiresTheHolder(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	_, err := executeBarrierCmd(t, newBarrierQuorumBeginCommand(), "--op", qrdbtransport.OpUnseal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--username is required")
	assert.Nil(t, fc.lastQuorumBegin)
}

func TestBarrierQuorumSubmit_SendsResponseAndShareAndPrintsTheRootToken(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{
		quorumSubmitFunc: func(_ context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
			return &qrdbtransport.QuorumOutcome{Domain: req.Domain, Op: req.Op, Required: 2, Submitted: 2, Complete: true, RootToken: "tok-1"}, nil
		},
	}
	defer withFakeBarrier(fc)()

	response := writeQuorumFile(t, "response", []byte("signed-nonce"))
	share := writeQuorumFile(t, "share", []byte("opened-share"))
	out, err := executeBarrierCmd(t, newBarrierQuorumSubmitCommand(),
		"--op", qrdbtransport.OpRootToken, "--username", "alice", "--challenge-id", "c-1",
		"--factor", "mtls", "--response-file", response, "--share-file", share,
		"--response-metadata", "alg=ed25519")
	require.NoError(t, err)
	require.NotNil(t, fc.lastQuorumSubmit)
	req := fc.lastQuorumSubmit
	assert.Equal(t, "c-1", req.ChallengeID)
	assert.Equal(t, []byte("signed-nonce"), req.Response)
	assert.Equal(t, map[string]string{"alg": "ed25519"}, req.ResponseMetadata)
	assert.Equal(t, make([]byte, len("opened-share")), req.Share, "the CLI clears the share once submitted")
	assert.Contains(t, out, "Root Token:")
	assert.Contains(t, out, "tok-1")
}

func TestBarrierQuorumSubmit_RejectsMissingOrEmptyFiles(t *testing.T) {
	empty := writeQuorumFile(t, "empty", nil)
	response := writeQuorumFile(t, "response", []byte("r"))
	holder := []string{"--op", qrdbtransport.OpUnseal, "--username", "alice", "--challenge-id", "c-1", "--factor", "mtls"}
	cases := map[string]struct {
		args    []string
		wantErr string
	}{
		"no response file": {append([]string{"--share-file", response}, holder...), "--response-file are required"},
		"no share file":    {append([]string{"--response-file", response}, holder...), "--share-file is required"},
		"empty share file": {append([]string{"--response-file", response, "--share-file", empty}, holder...), "is empty"},
		"missing share":    {append([]string{"--response-file", response, "--share-file", empty + ".absent"}, holder...), "failed to read --share-file"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer resetBarrierFlags()
			fc := &fakeBarrierClient{}
			defer withFakeBarrier(fc)()

			_, err := executeBarrierCmd(t, newBarrierQuorumSubmitCommand(), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Nil(t, fc.lastQuorumSubmit)
		})
	}
}
