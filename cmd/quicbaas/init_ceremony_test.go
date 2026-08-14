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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// resetInitCeremonyFlags clears the package-level flag state cobra binds to,
// so table entries cannot leak values into one another.
func resetInitCeremonyFlags() {
	initSOPin = ""
	initUserPin = ""
	initThreshold = 0
	initOfficerSpec = nil
	initHostname = ""
	initSANs = nil
	initCredSeal = ""
	initUsername = ""
	initCSRPath = ""
	initOutputPath = ""
	initAuthFactor = ""
	initAuthRef = ""
	initOutputFmt = "table"
}

// writeCSRFixture writes a placeholder CSR file and returns its path. The CLI
// only reads and forwards the bytes; the server parses them.
func writeCSRFixture(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	pem := "-----BEGIN CERTIFICATE REQUEST-----\n" + name + "\n-----END CERTIFICATE REQUEST-----\n"
	require.NoError(t, os.WriteFile(path, []byte(pem), 0o600))
	return path
}

// =============================================================================
// parseCeremonyOfficers
// =============================================================================

// TestParseCeremonyOfficers_ReadsEachCSR verifies every --so value is resolved
// to its username and the PEM bytes of its CSR.
func TestParseCeremonyOfficers_ReadsEachCSR(t *testing.T) {
	dir := t.TempDir()
	alice := writeCSRFixture(t, dir, "alice.csr")
	bob := writeCSRFixture(t, dir, "bob.csr")

	officers, err := parseCeremonyOfficers([]string{"alice:" + alice, "bob:" + bob})
	require.NoError(t, err)
	require.Len(t, officers, 2)

	assert.Equal(t, "alice", officers[0].Username)
	assert.Contains(t, officers[0].CSRPEM, "CERTIFICATE REQUEST")
	assert.Contains(t, officers[0].CSRPEM, "alice.csr")
	assert.Equal(t, "bob", officers[1].Username)
	assert.Contains(t, officers[1].CSRPEM, "bob.csr")
}

// TestParseCeremonyOfficers_NoOfficersIsSingleAdminMode verifies the empty case
// is not an error: a ceremony with no officers is single-admin mode.
func TestParseCeremonyOfficers_NoOfficersIsSingleAdminMode(t *testing.T) {
	officers, err := parseCeremonyOfficers(nil)
	require.NoError(t, err)
	assert.Empty(t, officers)
}

// TestParseCeremonyOfficers_RejectsMalformedSpec verifies each malformed --so
// value fails with the typed error rather than being silently skipped, which
// would run the ceremony with fewer officers than the operator intended.
func TestParseCeremonyOfficers_RejectsMalformedSpec(t *testing.T) {
	for _, spec := range []string{"aliceonly", ":/path/to.csr", "alice:", ""} {
		t.Run(spec, func(t *testing.T) {
			_, err := parseCeremonyOfficers([]string{spec})
			require.Error(t, err)
			var typed *InitOfficerSpecError
			require.ErrorAs(t, err, &typed)
		})
	}
}

// TestParseCeremonyOfficers_MissingCSRFileFails verifies an unreadable CSR
// aborts before any request is sent.
func TestParseCeremonyOfficers_MissingCSRFileFails(t *testing.T) {
	_, err := parseCeremonyOfficers([]string{"alice:" + filepath.Join(t.TempDir(), "absent.csr")})
	require.Error(t, err)
	var typed *InitReadCSRError
	require.ErrorAs(t, err, &typed)
}

// =============================================================================
// init initialize
// =============================================================================

// TestRunInitInitialize_SendsThePlanToTheServer verifies the ceremony request
// carries the PINs, the threshold, and every officer's CSR.
func TestRunInitInitialize_SendsThePlanToTheServer(t *testing.T) {
	resetInitCeremonyFlags()
	dir := t.TempDir()

	fc := &fakeCeremonyClient{
		initializeFunc: func(_ context.Context, _ *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
			return &qrdbtransport.CeremonyInitializeResponse{
				State:      "enrolling",
				SPKIPin:    "abc123",
				ShareCount: 3,
			}, nil
		},
	}
	restore := withFakeCeremony(fc)
	defer restore()

	initSOPin = "so-pin"
	initUserPin = "user-pin"
	initThreshold = 2
	initOfficerSpec = []string{
		"alice:" + writeCSRFixture(t, dir, "alice.csr"),
		"bob:" + writeCSRFixture(t, dir, "bob.csr"),
	}

	cmd := &cobra.Command{Use: "initialize"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runInitInitializeCommand(cmd, nil))

	req := fc.lastInitializeReq
	require.NotNil(t, req)
	assert.Equal(t, "so-pin", req.SOPin)
	assert.Equal(t, "user-pin", req.UserPin)
	assert.Equal(t, 2, req.Threshold)
	require.Len(t, req.Officers, 2)
	assert.Equal(t, "alice", req.Officers[0].Username)

	// The operator is told what happened and where the shares are — and the
	// output must never contain share material.
	got := out.String()
	assert.Contains(t, got, "enrolling")
	assert.Contains(t, got, "3 generated")
	assert.Contains(t, got, "claim-share")
}

// TestRunInitInitialize_RequiresPINs verifies both PINs are mandatory: the
// ceremony seals credentials with them, so a missing PIN must fail before any
// state changes rather than half-initializing the barrier.
func TestRunInitInitialize_RequiresPINs(t *testing.T) {
	cases := []struct {
		name    string
		soPin   string
		userPin string
	}{
		{"no so-pin", "", "user-pin"},
		{"no user-pin", "so-pin", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetInitCeremonyFlags()
			fc := &fakeCeremonyClient{}
			restore := withFakeCeremony(fc)
			defer restore()

			initSOPin = tc.soPin
			initUserPin = tc.userPin

			err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
			require.Error(t, err)
			assert.Nil(t, fc.lastInitializeReq, "no request may be sent when validation fails")
		})
	}
}

// TestRunInitInitialize_RejectsThresholdAboveOfficerCount verifies an M-of-N
// request that could never reach quorum is refused locally. Sending it would
// initialize a barrier that no set of officers can ever unseal.
func TestRunInitInitialize_RejectsThresholdAboveOfficerCount(t *testing.T) {
	resetInitCeremonyFlags()
	dir := t.TempDir()

	fc := &fakeCeremonyClient{}
	restore := withFakeCeremony(fc)
	defer restore()

	initSOPin = "so-pin"
	initUserPin = "user-pin"
	initThreshold = 3
	initOfficerSpec = []string{"alice:" + writeCSRFixture(t, dir, "alice.csr")}

	err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "threshold")
	assert.Nil(t, fc.lastInitializeReq, "an unreachable quorum must not be sent to the server")
}

// =============================================================================
// init officer register
// =============================================================================

// TestRunInitOfficerRegister_SubmitsTheCSRAndWritesTheCert verifies the CSR is
// forwarded and the issued certificate is written for delivery to the officer.
func TestRunInitOfficerRegister_SubmitsTheCSRAndWritesTheCert(t *testing.T) {
	resetInitCeremonyFlags()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "alice.crt")

	fb := &fakeBarrierClient{
		officerRegisterFunc: func(_ context.Context, _ *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
			return &qrdbtransport.OfficerRegisterResponse{
				CertPEM:    []byte("-----BEGIN CERTIFICATE-----\nalice\n-----END CERTIFICATE-----\n"),
				ShareIndex: 1,
			}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	initUsername = "alice"
	initCSRPath = writeCSRFixture(t, dir, "alice.csr")
	initOutputPath = certPath

	cmd := &cobra.Command{Use: "register"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runInitOfficerRegisterCommand(cmd, nil))

	req := fb.lastOfficerRegisterReq
	require.NotNil(t, req)
	assert.Equal(t, "alice", req.Username)
	assert.Contains(t, string(req.CSRPEM), "CERTIFICATE REQUEST")

	written, err := os.ReadFile(certPath)
	require.NoError(t, err)
	assert.Contains(t, string(written), "BEGIN CERTIFICATE")

	// Registration reports the share INDEX, never share bytes.
	got := out.String()
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "1")
}

// TestRunInitOfficerRegister_RequiresUsernameAndCSR verifies both inputs are
// mandatory.
func TestRunInitOfficerRegister_RequiresUsernameAndCSR(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		username string
		csr      string
	}{
		{"no username", "", writeCSRFixture(t, dir, "x.csr")},
		{"no csr", "alice", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetInitCeremonyFlags()
			fb := &fakeBarrierClient{}
			restore := withFakeBarrier(fb)
			defer restore()

			initUsername = tc.username
			initCSRPath = tc.csr

			err := runInitOfficerRegisterCommand(&cobra.Command{Use: "register"}, nil)
			require.Error(t, err)
			assert.Nil(t, fb.lastOfficerRegisterReq)
		})
	}
}

// TestRunInitOfficerRegister_RejectsUnknownAuthFactor verifies the officer's
// authenticator binding is validated locally against the canonical set.
func TestRunInitOfficerRegister_RejectsUnknownAuthFactor(t *testing.T) {
	resetInitCeremonyFlags()
	dir := t.TempDir()

	fb := &fakeBarrierClient{}
	restore := withFakeBarrier(fb)
	defer restore()

	initUsername = "alice"
	initCSRPath = writeCSRFixture(t, dir, "alice.csr")
	initAuthFactor = "smartwatch"

	err := runInitOfficerRegisterCommand(&cobra.Command{Use: "register"}, nil)
	require.Error(t, err)
	assert.Nil(t, fb.lastOfficerRegisterReq)
}

// TestRunInitOfficerRegister_MissingCSRFileFails verifies an unreadable CSR
// aborts before any request is sent.
func TestRunInitOfficerRegister_MissingCSRFileFails(t *testing.T) {
	resetInitCeremonyFlags()
	fb := &fakeBarrierClient{}
	restore := withFakeBarrier(fb)
	defer restore()

	initUsername = "alice"
	initCSRPath = filepath.Join(t.TempDir(), "absent.csr")

	err := runInitOfficerRegisterCommand(&cobra.Command{Use: "register"}, nil)
	require.Error(t, err)
	assert.Nil(t, fb.lastOfficerRegisterReq)
}

// =============================================================================
// init officer list / status
// =============================================================================

// TestRunInitOfficerList_RendersTheRoster verifies the roster prints public
// metadata for each officer.
func TestRunInitOfficerList_RendersTheRoster(t *testing.T) {
	resetInitCeremonyFlags()
	fb := &fakeBarrierClient{
		officerListFunc: func(_ context.Context) ([]*qrdbtransport.OfficerStatus, error) {
			return []*qrdbtransport.OfficerStatus{
				{Username: "alice", ShareIndex: 1, AuthFactor: "mtls", CertThumbprint: "aa11"},
				{Username: "bob", ShareIndex: 2, AuthFactor: "piv", CertThumbprint: "bb22"},
			}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	cmd := &cobra.Command{Use: "list"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runInitOfficerListCommand(cmd, nil))

	got := out.String()
	assert.Contains(t, got, "alice")
	assert.Contains(t, got, "bob")
	assert.Contains(t, got, "piv")
	assert.Contains(t, got, "aa11")
}

// TestRunInitOfficerList_EmptyRosterIsNotAnError verifies a barrier with no
// officers reports that plainly rather than failing.
func TestRunInitOfficerList_EmptyRosterIsNotAnError(t *testing.T) {
	resetInitCeremonyFlags()
	fb := &fakeBarrierClient{
		officerListFunc: func(_ context.Context) ([]*qrdbtransport.OfficerStatus, error) {
			return nil, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	cmd := &cobra.Command{Use: "list"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runInitOfficerListCommand(cmd, nil))
	assert.Contains(t, out.String(), "No Security Officers")
}

// TestRunInitOfficerStatus_ReportsClaimStateWithoutShares is the security
// assertion for the status verb: it reports WHO has claimed, never WHAT.
func TestRunInitOfficerStatus_ReportsClaimStateWithoutShares(t *testing.T) {
	resetInitCeremonyFlags()
	fb := &fakeBarrierClient{
		officerClaimStatusFunc: func(_ context.Context) ([]*qrdbtransport.OfficerStatus, error) {
			return []*qrdbtransport.OfficerStatus{
				{Username: "alice", ShareIndex: 1, AuthFactor: "mtls", CertThumbprint: "aa11"},
			}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	cmd := &cobra.Command{Use: "status"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runInitOfficerStatusCommand(cmd, nil))

	got := out.String()
	assert.Contains(t, got, "alice")
	assert.NotContains(t, strings.ToLower(got), "share_value")
	assert.NotContains(t, strings.ToLower(got), "secret")
}

// =============================================================================
// Command tree
// =============================================================================

// TestInitCommandTree_ExposesTheFullCeremonyFlow pins the verb set an operator
// needs to drive go-qrdb's Security Officer distribution model end to end.
// Without initialize and officer register the daemon boots sealed with no way
// to run the ceremony at all.
func TestInitCommandTree_ExposesTheFullCeremonyFlow(t *testing.T) {
	cmd := newInitCommand()

	byName := map[string]bool{}
	for _, sub := range cmd.Commands() {
		byName[sub.Name()] = true
	}
	for _, want := range []string{
		"initialize", "status", "officer",
		"claim-cert", "claim-share", "sign-csr", "enroll-external-officer",
	} {
		assert.True(t, byName[want], "init is missing the %q verb", want)
	}

	var officer *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "officer" {
			officer = sub
		}
	}
	require.NotNil(t, officer)

	officerVerbs := map[string]bool{}
	for _, sub := range officer.Commands() {
		officerVerbs[sub.Name()] = true
	}
	for _, want := range []string{"register", "list", "status"} {
		assert.True(t, officerVerbs[want], "init officer is missing the %q verb", want)
	}
}
