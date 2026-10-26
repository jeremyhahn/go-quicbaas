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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
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
	initRole = qrdbtransport.RoleSecurityOfficer
	initDesignated = ""
	initFactors = nil
	initEncKEM = ""
	initEncKeyPath = ""
	initAdminCSRPath = ""
	initAdminCertPath = ""
	initAdminCertPoolPath = ""
	initAdminGenerate = false
	initAdminKeyOut = ""
	initAdminCertOut = ""
	initSecret = ""
	initOutputFmt = "table"
	resetBarrierFlags()
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
	initAdminCSRPath = writeCSRFixture(t, dir, "admin.csr")

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
	assert.NotEmpty(t, req.AdminCSRPEM)

	// The operator is told what happened and where the shares are — and the
	// output must never contain share material.
	got := out.String()
	assert.Contains(t, got, "enrolling")
	assert.Contains(t, got, "3 generated")
	assert.Contains(t, got, "claim-share")
}

// csrInitializeWithRoster selects --admin-csr with an inline two-officer
// --so roster and answers with the issued admin and holder certificates.
func csrInitializeWithRoster(t *testing.T, dir string) *fakeCeremonyClient {
	t.Helper()
	initSOPin = "so-pin"
	initUserPin = "user-pin"
	initThreshold = 2
	initOfficerSpec = []string{
		"alice:" + writeCSRFixture(t, dir, "alice.csr"),
		"bob:" + writeCSRFixture(t, dir, "bob.csr"),
	}
	initAdminCSRPath = writeCSRFixture(t, dir, "admin.csr")
	return generatedCredentialClient(&qrdbtransport.CeremonyInitializeResponse{
		State:        "complete",
		ShareCount:   2,
		AdminCertPEM: testIssuedAdminCertPEM,
		Holders: []qrdbtransport.CeremonyHolderCert{
			{Username: "alice", Role: qrdbtransport.RoleSecurityOfficer, CertPEM: testIssuedAliceCertPEM},
			{Username: "bob", Role: qrdbtransport.RoleSecurityOfficer, CertPEM: testIssuedBobCertPEM},
		},
	})
}

// Certificates the daemon issues for an --admin-csr ceremony with an inline
// roster; the CLI must deliver every one of them.
const (
	testIssuedAdminCertPEM = "-----BEGIN CERTIFICATE-----\nissued-admin-cert\n-----END CERTIFICATE-----\n"
	testIssuedAliceCertPEM = "-----BEGIN CERTIFICATE-----\nissued-alice-cert\n-----END CERTIFICATE-----\n"
	testIssuedBobCertPEM   = "-----BEGIN CERTIFICATE-----\nissued-bob-cert\n-----END CERTIFICATE-----\n"
)

// TestRunInitInitialize_AdminCSRDeliversEveryIssuedCertificate: the
// initialization cannot be repeated, so the default output prints the issued
// admin certificate and each inline holder's certificate, labelled with its
// holder, or the operator is left without the admin mTLS and officer certs.
func TestRunInitInitialize_AdminCSRDeliversEveryIssuedCertificate(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	restore := withFakeCeremony(csrInitializeWithRoster(t, t.TempDir()))
	defer restore()

	cmd := &cobra.Command{Use: "initialize"}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	require.NoError(t, runInitInitializeCommand(cmd, nil))

	got := out.String()
	assert.Contains(t, got, "Admin Certificate:\n"+testIssuedAdminCertPEM)
	assert.Contains(t, got, "Holder alice ("+qrdbtransport.RoleSecurityOfficer+") Certificate:\n"+testIssuedAliceCertPEM)
	assert.Contains(t, got, "Holder bob ("+qrdbtransport.RoleSecurityOfficer+") Certificate:\n"+testIssuedBobCertPEM)
	assert.NotContains(t, got, "PRIVATE KEY")
}

// TestRunInitInitialize_AdminCSRWritesTheCertificateOut: with --admin-cert-out
// the issued admin certificate is saved there, named in the output and not
// printed; the holder certificates are still printed.
func TestRunInitInitialize_AdminCSRWritesTheCertificateOut(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	dir := t.TempDir()
	restore := withFakeCeremony(csrInitializeWithRoster(t, dir))
	defer restore()
	initAdminCertOut = filepath.Join(dir, "admin.crt")

	cmd := &cobra.Command{Use: "initialize"}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	require.NoError(t, runInitInitializeCommand(cmd, nil))

	cert, err := os.ReadFile(initAdminCertOut)
	require.NoError(t, err)
	assert.Equal(t, testIssuedAdminCertPEM, string(cert))
	got := out.String()
	assert.Contains(t, got, initAdminCertOut)
	assert.NotContains(t, got, "issued-admin-cert")
	assert.Contains(t, got, testIssuedAliceCertPEM)
	assert.Contains(t, got, testIssuedBobCertPEM)
}

// TestRunInitInitialize_AdminCSRCertificateOutMissingInResponse: a response
// without the admin certificate is an error and leaves no empty file.
func TestRunInitInitialize_AdminCSRCertificateOutMissingInResponse(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	dir := t.TempDir()
	csrInitializeWithRoster(t, dir)
	restore := withFakeCeremony(generatedCredentialClient(&qrdbtransport.CeremonyInitializeResponse{State: "complete"}))
	defer restore()
	initAdminCertOut = filepath.Join(dir, "admin.crt")

	err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
	var missing *InitAdminCredentialMissingError
	require.ErrorAs(t, err, &missing)
	assert.NoFileExists(t, initAdminCertOut)
}

// TestRunInitInitialize_AdminCSRJSONCarriesTheCertificates: the JSON output
// stays the daemon's response verbatim, holders and admin certificate included.
func TestRunInitInitialize_AdminCSRJSONCarriesTheCertificates(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	restore := withFakeCeremony(csrInitializeWithRoster(t, t.TempDir()))
	defer restore()
	initOutputFmt = "json"

	cmd := &cobra.Command{Use: "initialize"}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	require.NoError(t, runInitInitializeCommand(cmd, nil))

	var resp qrdbtransport.CeremonyInitializeResponse
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	assert.Equal(t, testIssuedAdminCertPEM, resp.AdminCertPEM)
	require.Len(t, resp.Holders, 2)
	assert.Equal(t, testIssuedBobCertPEM, resp.Holders[1].CertPEM)
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
				CertPEM: "-----BEGIN CERTIFICATE-----\nalice\n-----END CERTIFICATE-----\n",
				Role:    qrdbtransport.RoleSecurityOfficer,
			}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	initUsername = "alice"
	initCSRPath = writeCSRFixture(t, dir, "alice.csr")
	initOutputPath = certPath
	initDesignated = "piv"
	initFactors = []string{"piv", `totp={"digits":6}`}

	cmd := &cobra.Command{Use: "register"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runInitOfficerRegisterCommand(cmd, nil))

	req := fb.lastOfficerRegisterReq
	require.NotNil(t, req)
	assert.Equal(t, "alice", req.Username)
	assert.Equal(t, qrdbtransport.RoleSecurityOfficer, req.Role)
	assert.Equal(t, "piv", req.Designated)
	assert.Contains(t, req.CSRPEM, "CERTIFICATE REQUEST")
	require.Len(t, req.Factors, 2)
	assert.Equal(t, "piv", req.Factors[0].Name)
	assert.Nil(t, req.Factors[0].Params)
	assert.Equal(t, "totp", req.Factors[1].Name)
	assert.Equal(t, float64(6), req.Factors[1].Params["digits"])
	assert.Zero(t, fb.recoveryRegisters)

	written, err := os.ReadFile(certPath)
	require.NoError(t, err)
	assert.Contains(t, string(written), "BEGIN CERTIFICATE")

	assert.Contains(t, out.String(), "alice")
}

// TestRunInitOfficerRegister_RecoveryDomainUsesTheRecoveryVerb verifies
// --domain recovery enrols the holder on the recovery roster, not the
// unseal roster.
func TestRunInitOfficerRegister_RecoveryDomainUsesTheRecoveryVerb(t *testing.T) {
	resetInitCeremonyFlags()
	fb := &fakeBarrierClient{
		officerRegisterFunc: func(_ context.Context, _ *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
			return &qrdbtransport.OfficerRegisterResponse{}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	initUsername = "alice"
	initCSRPath = writeCSRFixture(t, t.TempDir(), "alice.csr")
	barrierDomain = qrdbtransport.DomainRecovery

	require.NoError(t, runInitOfficerRegisterCommand(&cobra.Command{Use: "register"}, nil))
	assert.Equal(t, 1, fb.recoveryRegisters)
	require.NotNil(t, fb.lastOfficerRegisterReq)
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

// TestRunInitOfficerRegister_RejectsBadInputBeforeDialling verifies malformed
// factors, an unpaired encryption key and an unknown domain are refused
// locally, with no request sent.
func TestRunInitOfficerRegister_RejectsBadInputBeforeDialling(t *testing.T) {
	dir := t.TempDir()
	csr := writeCSRFixture(t, dir, "alice.csr")
	cases := map[string]func(){
		"factor without a name":  func() { initFactors = []string{"=x"} },
		"factor params not json": func() { initFactors = []string{"totp={bad"} },
		"kem without key file":   func() { initEncKEM = "ml-kem-768" },
		"key file without kem":   func() { initEncKeyPath = filepath.Join(dir, "k.pub") },
		"unknown domain":         func() { barrierDomain = "audit" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			resetInitCeremonyFlags()
			fb := &fakeBarrierClient{}
			restore := withFakeBarrier(fb)
			defer restore()

			initUsername = "alice"
			initCSRPath = csr
			mutate()

			err := runInitOfficerRegisterCommand(&cobra.Command{Use: "register"}, nil)
			require.Error(t, err)
			assert.Nil(t, fb.lastOfficerRegisterReq)
		})
	}
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
				{Username: "alice", ShareIndex: 1, Designated: "mtls", CertThumbprint: "aa11"},
				{Username: "bob", ShareIndex: 2, Designated: "piv", CertThumbprint: "bb22"},
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
	for _, want := range canonicalInitVerbs {
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
	for _, want := range canonicalInitOfficerVerbs {
		assert.True(t, officerVerbs[want], "init officer is missing the %q verb", want)
	}
}

// TestRunInitInitialize_SendsTheAdminSelector proves each admin credential
// flag reaches the server request, with the file flags read as PEM.
func TestRunInitInitialize_SendsTheAdminSelector(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	cases := []struct {
		name  string
		set   func(dir string)
		check func(t *testing.T, req *qrdbtransport.CeremonyInitializeRequest)
	}{
		{
			name: "admin-csr",
			set:  func(dir string) { initAdminCSRPath = writeCSRFixture(t, dir, "admin.csr") },
			check: func(t *testing.T, req *qrdbtransport.CeremonyInitializeRequest) {
				assert.NotEmpty(t, req.AdminCSRPEM)
				assert.Empty(t, req.AdminCertPEM)
				assert.False(t, req.AdminGenerate)
			},
		},
		{
			name: "admin-cert with pool",
			set: func(dir string) {
				initAdminCertPath = writeCSRFixture(t, dir, "admin.crt")
				initAdminCertPoolPath = writeCSRFixture(t, dir, "pool.pem")
			},
			check: func(t *testing.T, req *qrdbtransport.CeremonyInitializeRequest) {
				assert.NotEmpty(t, req.AdminCertPEM)
				assert.NotEmpty(t, req.AdminCertPoolPEM)
				assert.Empty(t, req.AdminCSRPEM)
			},
		},
		{
			name: "admin-generate",
			set: func(dir string) {
				initAdminGenerate = true
				initAdminKeyOut = filepath.Join(dir, "admin.key")
				initAdminCertOut = filepath.Join(dir, "admin.crt")
			},
			check: func(t *testing.T, req *qrdbtransport.CeremonyInitializeRequest) {
				assert.True(t, req.AdminGenerate)
				assert.Empty(t, req.AdminCSRPEM)
				assert.Empty(t, req.AdminCertPEM)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetInitCeremonyFlags()
			fc := &fakeCeremonyClient{
				initializeFunc: func(_ context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
					resp := &qrdbtransport.CeremonyInitializeResponse{State: "complete"}
					if req.AdminGenerate {
						resp.AdminKeyPEM = []byte(testGeneratedKeyPEM)
						resp.AdminCertPEM = testGeneratedCertPEM
					}
					return resp, nil
				},
			}
			restore := withFakeCeremony(fc)
			defer restore()

			initSOPin = "so-pin"
			initUserPin = "user-pin"
			tc.set(t.TempDir())

			cmd := &cobra.Command{Use: "initialize"}
			cmd.SetOut(&bytes.Buffer{})
			require.NoError(t, runInitInitializeCommand(cmd, nil))
			require.NotNil(t, fc.lastInitializeReq)
			tc.check(t, fc.lastInitializeReq)
		})
	}
}

// TestRunInitInitialize_RequiresExactlyOneAdminSelector proves no request
// is sent unless exactly one admin credential flag is set.
func TestRunInitInitialize_RequiresExactlyOneAdminSelector(t *testing.T) {
	cases := []struct {
		name  string
		set   func(dir string)
		count int
	}{
		{name: "none", set: func(string) {}, count: 0},
		{
			name: "csr and generate",
			set: func(dir string) {
				initAdminCSRPath = writeCSRFixture(t, dir, "admin.csr")
				initAdminGenerate = true
			},
			count: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetInitCeremonyFlags()
			fc := &fakeCeremonyClient{}
			restore := withFakeCeremony(fc)
			defer restore()

			initSOPin = "so-pin"
			initUserPin = "user-pin"
			tc.set(t.TempDir())

			err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
			require.Error(t, err)
			var selErr *InitAdminSelectorError
			require.ErrorAs(t, err, &selErr)
			assert.Equal(t, tc.count, selErr.Count)
			assert.Nil(t, fc.lastInitializeReq, "no request may be sent without exactly one selector")
		})
	}
}

// TestRunInitInitialize_UnreadableAdminFile proves a missing admin file is
// reported with its flag and never sent.
func TestRunInitInitialize_UnreadableAdminFile(t *testing.T) {
	resetInitCeremonyFlags()
	fc := &fakeCeremonyClient{}
	restore := withFakeCeremony(fc)
	defer restore()

	initSOPin = "so-pin"
	initUserPin = "user-pin"
	initAdminCertPath = filepath.Join(t.TempDir(), "missing.crt")

	err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
	require.Error(t, err)
	var readErr *InitReadAdminFileError
	require.ErrorAs(t, err, &readErr)
	assert.Equal(t, "admin-cert", readErr.Flag)
	assert.Nil(t, fc.lastInitializeReq)
}

// TestInitInitializeCommand_RegistersAdminFlags proves the selector and
// passphrase flags are wired onto the cobra command.
func TestInitInitializeCommand_RegistersAdminFlags(t *testing.T) {
	cmd := newInitInitializeCommand()
	for _, name := range []string{"admin-csr", "admin-cert", "admin-cert-pool", "admin-generate", "admin-key-out", "admin-cert-out", passphraseFlag, passphraseStdinFlag} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "flag --%s must be registered", name)
	}
	require.NoError(t, cmd.Flags().Parse([]string{"--admin-generate"}))
	assert.True(t, initAdminGenerate)
	resetInitCeremonyFlags()
}

// testGeneratedKeyPEM and testGeneratedCertPEM stand in for the credential
// the daemon generates for --admin-generate; the CLI only stores them.
const (
	testGeneratedKeyPEM  = "-----BEGIN PRIVATE KEY-----\ngenerated-admin-key\n-----END PRIVATE KEY-----\n"
	testGeneratedCertPEM = "-----BEGIN CERTIFICATE-----\ngenerated-admin-cert\n-----END CERTIFICATE-----\n"
)

// setAdminGenerate selects --admin-generate with key and certificate files
// in dir and returns their paths.
func setAdminGenerate(dir string) (keyPath, certPath string) {
	initSOPin = "so-pin"
	initUserPin = "user-pin"
	initAdminGenerate = true
	initAdminKeyOut = filepath.Join(dir, "admin.key")
	initAdminCertOut = filepath.Join(dir, "admin.crt")
	return initAdminKeyOut, initAdminCertOut
}

// generatedCredentialClient answers --admin-generate with resp.
func generatedCredentialClient(resp *qrdbtransport.CeremonyInitializeResponse) *fakeCeremonyClient {
	return &fakeCeremonyClient{
		initializeFunc: func(_ context.Context, _ *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
			return resp, nil
		},
	}
}

// TestRunInitInitialize_AdminGenerateWritesTheCredentialFiles: the default
// table output keeps the generated key on disk, mode 0600, and names the
// files without printing the key.
func TestRunInitInitialize_AdminGenerateWritesTheCredentialFiles(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	fc := generatedCredentialClient(&qrdbtransport.CeremonyInitializeResponse{
		State:        "complete",
		AdminKeyPEM:  []byte(testGeneratedKeyPEM),
		AdminCertPEM: testGeneratedCertPEM,
	})
	restore := withFakeCeremony(fc)
	defer restore()
	keyPath, certPath := setAdminGenerate(t.TempDir())

	cmd := &cobra.Command{Use: "initialize"}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	require.NoError(t, runInitInitializeCommand(cmd, nil))

	key, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.Equal(t, testGeneratedKeyPEM, string(key))
	info, err := os.Stat(keyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	cert, err := os.ReadFile(certPath)
	require.NoError(t, err)
	assert.Equal(t, testGeneratedCertPEM, string(cert))

	assert.Contains(t, out.String(), keyPath)
	assert.Contains(t, out.String(), certPath)
	assert.NotContains(t, out.String(), "PRIVATE KEY")
	assert.NotContains(t, out.String(), "generated-admin-key")
}

// TestRunInitInitialize_AdminGenerateJSONOmitsThePrivateKey pins that the
// JSON summary of an --admin-generate run never carries the generated
// private key: it is written to --admin-key-out only, and the response's key
// bytes are wiped once the command returns.
func TestRunInitInitialize_AdminGenerateJSONOmitsThePrivateKey(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	resp := &qrdbtransport.CeremonyInitializeResponse{
		State:        "complete",
		AdminKeyPEM:  []byte(testGeneratedKeyPEM),
		AdminCertPEM: testGeneratedCertPEM,
	}
	restore := withFakeCeremony(generatedCredentialClient(resp))
	defer restore()
	keyPath, _ := setAdminGenerate(t.TempDir())
	initOutputFmt = "json"

	cmd := &cobra.Command{Use: "initialize"}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	require.NoError(t, runInitInitializeCommand(cmd, nil))

	key, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.Equal(t, testGeneratedKeyPEM, string(key))

	var summary map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &summary))
	assert.NotContains(t, summary, "admin_key_pem")
	assert.Equal(t, testGeneratedCertPEM, summary["admin_cert_pem"])
	assert.NotContains(t, out.String(), base64.StdEncoding.EncodeToString([]byte(testGeneratedKeyPEM)))
	assert.Equal(t, make([]byte, len(testGeneratedKeyPEM)), resp.AdminKeyPEM,
		"the response's key bytes must be wiped")
}

// TestRunInitInitialize_AdminGenerateWipesTheKeyOnWriteFailure pins that the
// generated key is wiped even when the command fails after the daemon call.
func TestRunInitInitialize_AdminGenerateWipesTheKeyOnWriteFailure(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	resp := &qrdbtransport.CeremonyInitializeResponse{
		State:       "complete",
		AdminKeyPEM: []byte(testGeneratedKeyPEM),
	}
	restore := withFakeCeremony(generatedCredentialClient(resp))
	defer restore()
	setAdminGenerate(t.TempDir())

	cmd := &cobra.Command{Use: "initialize"}
	cmd.SetOut(&bytes.Buffer{})
	err := runInitInitializeCommand(cmd, nil)
	require.Error(t, err)
	assert.Equal(t, make([]byte, len(testGeneratedKeyPEM)), resp.AdminKeyPEM,
		"the response's key bytes must be wiped")
}

// TestRunInitInitialize_AdminGenerateRequiresOutputFiles: without a
// destination for the key the daemon is never called.
func TestRunInitInitialize_AdminGenerateRequiresOutputFiles(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	cases := []struct {
		name  string
		unset func()
		flag  string
	}{
		{name: "no key file", unset: func() { initAdminKeyOut = "" }, flag: "admin-key-out"},
		{name: "no cert file", unset: func() { initAdminCertOut = "" }, flag: "admin-cert-out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetInitCeremonyFlags()
			defer resetInitCeremonyFlags()
			fc := &fakeCeremonyClient{}
			restore := withFakeCeremony(fc)
			defer restore()
			setAdminGenerate(t.TempDir())
			tc.unset()

			err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
			var reqErr *InitAdminOutputRequiredError
			require.ErrorAs(t, err, &reqErr)
			assert.Equal(t, tc.flag, reqErr.Flag)
			assert.Nil(t, fc.lastInitializeReq, "the daemon must not be called without a key destination")
		})
	}
}

// TestRunInitInitialize_AdminGenerateRefusesExistingOutputFile: an existing
// file is never overwritten, the daemon is never called, and a key file
// created before the refusal is removed again.
func TestRunInitInitialize_AdminGenerateRefusesExistingOutputFile(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	cases := []struct {
		name     string
		existing func(keyPath, certPath string) string
		flag     string
	}{
		{name: "key exists", existing: func(k, _ string) string { return k }, flag: "admin-key-out"},
		{name: "cert exists", existing: func(_, c string) string { return c }, flag: "admin-cert-out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetInitCeremonyFlags()
			defer resetInitCeremonyFlags()
			fc := &fakeCeremonyClient{}
			restore := withFakeCeremony(fc)
			defer restore()
			keyPath, certPath := setAdminGenerate(t.TempDir())
			existing := tc.existing(keyPath, certPath)
			require.NoError(t, os.WriteFile(existing, []byte("keep"), 0o600))

			err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
			var fileErr *InitAdminOutputFileError
			require.ErrorAs(t, err, &fileErr)
			assert.Equal(t, tc.flag, fileErr.Flag)
			assert.Equal(t, "create", fileErr.Op)
			assert.Nil(t, fc.lastInitializeReq, "the daemon must not be called when a file cannot be created")

			kept, err := os.ReadFile(existing)
			require.NoError(t, err)
			assert.Equal(t, "keep", string(kept))
			if existing == certPath {
				assert.NoFileExists(t, keyPath, "the key file created before the refusal must be removed")
			}
		})
	}
}

// TestRunInitInitialize_AdminGenerateCallFailureRemovesTheFiles: a failed
// ceremony leaves no empty files, so the operator can retry with the same
// paths.
func TestRunInitInitialize_AdminGenerateCallFailureRemovesTheFiles(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	callErr := &InitAdminSelectorError{Count: 0}
	fc := &fakeCeremonyClient{
		initializeFunc: func(_ context.Context, _ *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
			return nil, callErr
		},
	}
	restore := withFakeCeremony(fc)
	defer restore()
	keyPath, certPath := setAdminGenerate(t.TempDir())

	err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
	require.ErrorIs(t, err, callErr)
	assert.NoFileExists(t, keyPath)
	assert.NoFileExists(t, certPath)
}

// TestRunInitInitialize_AdminGenerateMissingCredentialInResponse: a response
// without the certificate is an error, and the key it did carry is still
// written.
func TestRunInitInitialize_AdminGenerateMissingCredentialInResponse(t *testing.T) {
	t.Setenv(barrierPassphraseEnv, "")
	resetInitCeremonyFlags()
	defer resetInitCeremonyFlags()
	fc := generatedCredentialClient(&qrdbtransport.CeremonyInitializeResponse{
		State:       "complete",
		AdminKeyPEM: []byte(testGeneratedKeyPEM),
	})
	restore := withFakeCeremony(fc)
	defer restore()
	keyPath, certPath := setAdminGenerate(t.TempDir())

	err := runInitInitializeCommand(&cobra.Command{Use: "initialize"}, nil)
	var missing *InitAdminCredentialMissingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "administrator certificate", missing.Field)

	key, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	assert.Equal(t, testGeneratedKeyPEM, string(key))
	assert.NoFileExists(t, certPath)
}
