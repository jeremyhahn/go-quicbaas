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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// fakeTenantBarrierClient records requests so tests can assert what would be
// sent, and returns injected responses.
type fakeTenantBarrierClient struct {
	statusFunc  func(ctx context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error)
	initFunc    func(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error)
	unsealFunc  func(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error
	beginFunc   func(ctx context.Context, req *qrdbtransport.TenantQuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error)
	submitFunc  func(ctx context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error)
	sealFunc    func(ctx context.Context, req *qrdbtransport.TenantBarrierSealRequest) error
	officerFunc func(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error)

	lastStatusReq *qrdbtransport.TenantBarrierStatusRequest
	lastInitReq   *qrdbtransport.TenantBarrierInitRequest
	lastUnsealReq *qrdbtransport.TenantBarrierUnsealRequest
	lastBeginReq  *qrdbtransport.TenantQuorumBeginRequest
	lastSubmitReq *qrdbtransport.TenantQuorumSubmitRequest
	lastSealReq   *qrdbtransport.TenantBarrierSealRequest
	lastOfficerID string
}

func (f *fakeTenantBarrierClient) TenantBarrierStatus(ctx context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	f.lastStatusReq = req
	if f.statusFunc != nil {
		return f.statusFunc(ctx, req)
	}
	return &qrdbtransport.TenantBarrierStatusResponse{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	f.lastInitReq = cloneTenantBarrierInitReq(req)
	if f.initFunc != nil {
		return f.initFunc(ctx, req)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierUnseal(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
	f.lastUnsealReq = req
	if f.unsealFunc != nil {
		return f.unsealFunc(ctx, req)
	}
	return nil
}

func (f *fakeTenantBarrierClient) TenantBarrierSeal(ctx context.Context, req *qrdbtransport.TenantBarrierSealRequest) error {
	f.lastSealReq = req
	if f.sealFunc != nil {
		return f.sealFunc(ctx, req)
	}
	return nil
}

func (f *fakeTenantBarrierClient) TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	f.lastOfficerID = tenantID
	if f.officerFunc != nil {
		return f.officerFunc(ctx, tenantID)
	}
	return nil, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierQuorumBegin(ctx context.Context, req *qrdbtransport.TenantQuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	f.lastBeginReq = req
	if f.beginFunc != nil {
		return f.beginFunc(ctx, req)
	}
	return &qrdbtransport.QuorumChallenge{Op: req.Op}, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	f.lastSubmitReq = req
	if f.submitFunc != nil {
		return f.submitFunc(ctx, req)
	}
	return &qrdbtransport.QuorumOutcome{Op: req.Op}, nil
}

// withFakeTenantBarrier installs fc as the active factory and returns a
// restore func.
func withFakeTenantBarrier(fc *fakeTenantBarrierClient) func() {
	orig := tenantBarrierClientFactory
	tenantBarrierClientFactory = func() (TenantBarrierClient, error) { return fc, nil }
	return func() { tenantBarrierClientFactory = orig }
}

// enrolledTenantBarrierClient returns a fake whose tenant already enrolled n
// share holders, so a roster-less Shamir init passes the local holder check.
func enrolledTenantBarrierClient(n int) *fakeTenantBarrierClient {
	holders := make([]*qrdbtransport.OfficerStatus, n)
	for i := range holders {
		holders[i] = &qrdbtransport.OfficerStatus{Username: "holder-" + strconv.Itoa(i)}
	}
	return &fakeTenantBarrierClient{
		officerFunc: func(context.Context, string) ([]*qrdbtransport.OfficerStatus, error) {
			return holders, nil
		},
	}
}

// resetTenantBarrierFlags clears package-level flag state between cases.
func resetTenantBarrierFlags() {
	tenantBarrierTenantID = ""
	tenantBarrierStrategy = ""
	tenantBarrierSecret = ""
	tenantBarrierThreshold = 0
	tenantBarrierShares = 0
	tenantBarrierOutputFmt = "table"
	tenantBarrierOfficers = nil
	tenantBarrierCustodians = nil
	tenantBarrierPolicy = ""
	barrierAssumeYes = false
	resetBarrierFlags()
}

// TestTenantBarrier_EveryVerbRequiresATenant is the safety property for this
// whole surface. Defaulting to the system tenant would let a destructive verb
// silently hit the wrong barrier — sealing the control plane instead of one
// tenant. Every verb must refuse without --tenant, and must send nothing.
func TestTenantBarrier_EveryVerbRequiresATenant(t *testing.T) {
	verbs := map[string]func(*cobra.Command, []string) error{
		"status":        runTenantBarrierStatusCommand,
		"init":          runTenantBarrierInitCommand,
		"unseal":        runTenantBarrierUnsealCommand,
		"quorum begin":  runTenantBarrierQuorumBeginCommand,
		"quorum submit": runTenantBarrierQuorumSubmitCommand,
		"seal":          runTenantBarrierSealCommand,
		"officer list":  runTenantBarrierOfficerListCommand,
	}
	for name, run := range verbs {
		t.Run(name, func(t *testing.T) {
			resetTenantBarrierFlags()
			fc := &fakeTenantBarrierClient{}
			restore := withFakeTenantBarrier(fc)
			defer restore()

			err := run(&cobra.Command{Use: name}, nil)
			require.Error(t, err, "%s must refuse without --tenant", name)

			assert.Nil(t, fc.lastStatusReq)
			assert.Nil(t, fc.lastInitReq)
			assert.Nil(t, fc.lastUnsealReq)
			assert.Nil(t, fc.lastBeginReq)
			assert.Nil(t, fc.lastSubmitReq)
			assert.Nil(t, fc.lastSealReq)
			assert.Empty(t, fc.lastOfficerID)
		})
	}
}

// TestTenantBarrierStatus_ReportsStrategyAndQuorum verifies the operator sees
// which strategy protects the tenant and whether it is hardware-backed —
// the facts that decide who is able to unseal it.
func TestTenantBarrierStatus_ReportsStrategyAndQuorum(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{
		statusFunc: func(_ context.Context, _ *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error) {
			return &qrdbtransport.TenantBarrierStatusResponse{
				TenantID:        "acme",
				Initialized:     true,
				Sealed:          true,
				Strategy:        "shamir",
				HardwareBacked:  false,
				ShamirThreshold: 2,
				ShamirTotal:     3,
			}, nil
		},
	}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"
	cmd := &cobra.Command{Use: "status"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierStatusCommand(cmd, nil))

	require.NotNil(t, fc.lastStatusReq)
	assert.Equal(t, "acme", fc.lastStatusReq.TenantID)

	got := out.String()
	assert.Contains(t, got, "acme")
	assert.Contains(t, got, "shamir")
	assert.Contains(t, got, "2-of-3")
}

// TestTenantBarrierInit_SendsTheStrategy verifies the tenant's chosen strategy
// and its parameters reach the server unchanged.
func TestTenantBarrierInit_SendsTheStrategy(t *testing.T) {
	resetTenantBarrierFlags()
	// The tenant already enrolled its holders, so the init names no roster.
	fc := enrolledTenantBarrierClient(3)
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"
	tenantBarrierStrategy = "shamir"
	tenantBarrierThreshold = 2
	tenantBarrierShares = 3

	require.NoError(t, runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil))

	req := fc.lastInitReq
	require.NotNil(t, req)
	assert.Equal(t, "acme", req.TenantID)
	assert.Equal(t, "shamir", req.Strategy)
	assert.Equal(t, 2, req.Threshold)
	assert.Equal(t, 3, req.Shares)
}

// TestTenantBarrierInit_SignsWithTheMTLSKey proves the init request carries
// the admin's mTLS key as its Signer, and that the Signer answers a node's
// init nonce over a real TLS 1.3 connection with the certificate's own key.
// A network node refuses init/complete without it (InitSignerRequiredError).
// No SDK test helper serves tenant barrier init/begin over a listener without
// a full qrdb node, so the check stands at the request boundary.
func TestTenantBarrierInit_SignsWithTheMTLSKey(t *testing.T) {
	withClientTLSGlobals(t)
	resetTenantBarrierFlags()
	fc := enrolledTenantBarrierClient(3)
	restore := withFakeTenantBarrier(fc)
	defer restore()

	dir := t.TempDir()
	tlsCert = filepath.Join(dir, "admin.crt")
	tlsKey = filepath.Join(dir, "admin.key")
	writeSelfSignedKeyPair(t, tlsCert, tlsKey)
	tenantBarrierTenantID = "acme"
	tenantBarrierStrategy = "shamir"
	tenantBarrierThreshold = 2
	tenantBarrierShares = 3

	require.NoError(t, runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil))

	req := fc.lastInitReq
	require.NotNil(t, req)

	pair, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
	require.NoError(t, err)
	adminCert, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)
	wantSPKI, err := x509.MarshalPKIXPublicKey(adminCert.PublicKey)
	require.NoError(t, err)

	nonce := []byte("tenant-barrier-init-nonce")
	cs := clientTLS13ConnectionState(t)
	// A nil Signer fails here with InitSignerRequiredError, exactly as the
	// node's init/complete would.
	proof, err := qrdbtransport.ProveTenantBarrierInit(req.Signer, req.TenantID, nonce, cs)
	require.NoError(t, err, "init must carry the admin's mTLS key")
	assert.Equal(t, nonce, proof.Nonce)
	assert.Equal(t, wantSPKI, proof.PublicKey, "the proof must name the mTLS certificate's key")
	assert.NotEmpty(t, proof.Signature)
}

// TestFormatTenantBarrierInit_TablePrintsEveryHolderCertificate pins Codex
// 6985: init issues each holder's certificate once, so the default table
// output must print every one or the holders cannot unseal.
func TestFormatTenantBarrierInit_TablePrintsEveryHolderCertificate(t *testing.T) {
	resp := &qrdbtransport.TenantBarrierInitResponse{
		TenantID:    "acme",
		Strategy:    "shamir",
		Threshold:   2,
		TotalShares: 2,
		Holders: []qrdbtransport.TenantBarrierHolderCert{
			{Username: "alice", Role: "officer", CertPEM: "-----BEGIN CERTIFICATE-----\nYWxpY2U=\n-----END CERTIFICATE-----\n"},
			{Username: "bob", Role: "custodian", CertPEM: "-----BEGIN CERTIFICATE-----\nYm9i\n-----END CERTIFICATE-----\n"},
		},
	}
	var out bytes.Buffer
	require.NoError(t, formatTenantBarrierInit(resp, "table", &out))
	for _, h := range resp.Holders {
		assert.Contains(t, out.String(), h.CertPEM, "holder %s certificate missing", h.Username)
	}
}

// TestFormatTenantBarrierInit_RejectsEmptyResponse confirms a nil response is
// an error rather than an empty table.
func TestFormatTenantBarrierInit_RejectsEmptyResponse(t *testing.T) {
	var out bytes.Buffer
	require.Error(t, formatTenantBarrierInit(nil, "table", &out))
	assert.Empty(t, out.String())
}

// TestTenantBarrierInit_SignerErrors covers the two non-proving cases: no
// client certificate (the embedded transport, trusted in-process) sends no
// Signer, and an unloadable key refuses init before anything is sent.
func TestTenantBarrierInit_SignerErrors(t *testing.T) {
	t.Run("no client certificate sends no signer", func(t *testing.T) {
		withClientTLSGlobals(t)
		resetTenantBarrierFlags()
		fc := enrolledTenantBarrierClient(3)
		restore := withFakeTenantBarrier(fc)
		defer restore()
		tenantBarrierTenantID = "acme"
		tenantBarrierStrategy = "shamir"
		tenantBarrierThreshold = 2
		tenantBarrierShares = 3

		require.NoError(t, runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil))
		require.NotNil(t, fc.lastInitReq)
		assert.Nil(t, fc.lastInitReq.Signer)
	})

	t.Run("unloadable key refuses before sending", func(t *testing.T) {
		withClientTLSGlobals(t)
		resetTenantBarrierFlags()
		fc := enrolledTenantBarrierClient(3)
		restore := withFakeTenantBarrier(fc)
		defer restore()
		dir := t.TempDir()
		tlsCert = filepath.Join(dir, "missing.crt")
		tlsKey = filepath.Join(dir, "missing.key")
		tenantBarrierTenantID = "acme"
		tenantBarrierStrategy = "shamir"
		tenantBarrierThreshold = 2
		tenantBarrierShares = 3

		require.Error(t, runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil))
		assert.Nil(t, fc.lastInitReq, "no init may be sent without the admin's key")
	})
}

// clientTLS13ConnectionState completes an in-process TLS 1.3 handshake over
// net.Pipe and returns the client side's state, whose keying-material
// exporter the init proof binds to.
func clientTLS13ConnectionState(t *testing.T) *tls.ConnectionState {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "node.tenant-barrier.test"},
		DNSNames:     []string{"node.tenant-barrier.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		require.NoError(t, clientConn.Close())
		require.NoError(t, serverConn.Close())
	})
	server := tls.Server(serverConn, &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS13,
	})
	client := tls.Client(clientConn, &tls.Config{
		RootCAs:    roots,
		ServerName: "node.tenant-barrier.test",
		MinVersion: tls.VersionTLS13,
	})

	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Handshake() }()
	require.NoError(t, client.Handshake())
	require.NoError(t, <-serverDone)

	cs := client.ConnectionState()
	return &cs
}

// TestTenantBarrierInit_ValidatesLocally verifies unusable combinations are
// refused before a barrier is created. Initializing is not something a tenant
// admin can simply retry differently, so failing early matters.
func TestTenantBarrierInit_ValidatesLocally(t *testing.T) {
	cases := []struct {
		name      string
		strategy  string
		secret    string
		threshold int
		shares    int
	}{
		{"no strategy", "", "", 0, 0},
		{"passphrase without passphrase", "passphrase", "", 0, 0},
		{"shamir threshold below 2", "shamir", "", 1, 3},
		{"shamir shares below threshold", "shamir", "", 3, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetTenantBarrierFlags()
			fc := &fakeTenantBarrierClient{}
			restore := withFakeTenantBarrier(fc)
			defer restore()

			tenantBarrierTenantID = "acme"
			tenantBarrierStrategy = tc.strategy
			tenantBarrierSecret = tc.secret
			tenantBarrierThreshold = tc.threshold
			tenantBarrierShares = tc.shares

			err := runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil)
			require.Error(t, err)
			assert.Nil(t, fc.lastInitReq, "an unusable barrier config must not be created")
		})
	}
}

// TestTenantBarrierSeal_RequiresConfirmation is the guard on the one verb here
// that destroys access to data. Sealing is irreversible without the tenant's
// own credentials, so it must not happen on a bare invocation.
func TestTenantBarrierSeal_RequiresConfirmation(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"

	err := runTenantBarrierSealCommand(&cobra.Command{Use: "seal"}, nil)
	require.Error(t, err)
	assert.Nil(t, fc.lastSealReq, "seal must not proceed without confirmation")

	// With confirmation it goes through.
	barrierAssumeYes = true
	cmd := &cobra.Command{Use: "seal"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierSealCommand(cmd, nil))
	require.NotNil(t, fc.lastSealReq)
	assert.Equal(t, "acme", fc.lastSealReq.TenantID)
}

// TestTenantBarrierUnseal_RequiresAPassphrase verifies the passphrase path
// fails closed and points at the Shamir verb instead of sending an empty key.
func TestTenantBarrierUnseal_RequiresAPassphrase(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"

	err := runTenantBarrierUnsealCommand(&cobra.Command{Use: "unseal"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'quorum begin' / 'quorum submit'")
	assert.Nil(t, fc.lastUnsealReq)
}

// TestCanonicalSurface_TenantBarrier pins the per-tenant barrier verb set, so
// the tenant-admin surface cannot quietly narrow.
func TestCanonicalSurface_TenantBarrier(t *testing.T) {
	got := subcommandNames(newTenantBarrierCommand())
	for _, verb := range []string{
		"status", "init", "unseal", "quorum", "seal", "officer",
	} {
		assert.True(t, got[verb], "`tenant barrier %s` is missing", verb)
	}

	officer := findSubcommand(newTenantBarrierCommand(), "officer")
	require.NotNil(t, officer)
	officerVerbs := subcommandNames(officer)
	for _, verb := range []string{"list"} {
		assert.True(t, officerVerbs[verb], "`tenant barrier officer %s` is missing", verb)
	}
}

// TestTenantBarrierExamples_ResolveThroughTheRealRoot pins #228: the tenant
// barrier group hangs under `iam tenant`, and every example once said
// `quicbaas tenant barrier ...`, a path that does not exist. Each example's
// command words must resolve, through the real cobra root, to a command whose
// path is exactly those words; cobra's Find stops at the deepest match and
// hands the rest back as args, so a wrong path resolves short rather than
// failing, and only the path comparison catches it.
func TestTenantBarrierExamples_ResolveThroughTheRealRoot(t *testing.T) {
	group, _, err := rootCmd.Find([]string{"iam", "tenant", "barrier"})
	require.NoError(t, err)
	require.Equal(t, "quicbaas iam tenant barrier", group.CommandPath())

	var examples int
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, words := range exampleCommandWords(cmd.Long + "\n" + cmd.Example) {
			examples++
			want := "quicbaas " + strings.Join(words, " ")
			found, _, err := rootCmd.Find(words)
			require.NoError(t, err, "%s example %q does not resolve", cmd.CommandPath(), want)
			assert.Equal(t, want, found.CommandPath(),
				"%s example %q names a command that does not exist", cmd.CommandPath(), want)
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(group)

	assert.Positive(t, examples, "no examples were found; the scan no longer reads them")
}

// exampleCommandWords returns, for each example invocation in a command's help
// text, the command words after `quicbaas` up to the first flag. An invocation
// is a line that starts with `quicbaas` or pipes into it; prose that mentions
// a command in passing is not one.
func exampleCommandWords(help string) [][]string {
	var out [][]string
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		invocation, ok := strings.CutPrefix(trimmed, "quicbaas ")
		if !ok {
			_, invocation, ok = strings.Cut(trimmed, "| quicbaas ")
		}
		if !ok {
			continue
		}
		var words []string
		for _, field := range strings.Fields(invocation) {
			if strings.HasPrefix(field, "-") {
				break
			}
			words = append(words, field)
		}
		out = append(out, words)
	}
	return out
}

// cloneTenantBarrierInitReq copies the borrowed passphrase out of the request.
// See the note on cloneBarrierInitReq: the CLI's buffer is destroyed when the
// command returns, so a retained header faults rather than reading zeros.
func cloneTenantBarrierInitReq(req *qrdbtransport.TenantBarrierInitRequest) *qrdbtransport.TenantBarrierInitRequest {
	if req == nil {
		return nil
	}
	out := *req
	out.Passphrase = bytes.Clone(req.Passphrase)
	return &out
}

// TestTenantBarrierQuorum_BeginAndSubmitCarryTheTenant proves both quorum
// steps address the named tenant's barrier, never the system one.
func TestTenantBarrierQuorum_BeginAndSubmitCarryTheTenant(t *testing.T) {
	withTransport(t, "rest")
	defer resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{
		submitFunc: func(_ context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
			return &qrdbtransport.QuorumOutcome{Op: req.Op, Required: 2, Submitted: 1}, nil
		},
	}
	defer withFakeTenantBarrier(fc)()

	resetTenantBarrierFlags()
	tenantBarrierTenantID = "acme"
	barrierQuorumOp = qrdbtransport.OpUnseal
	barrierUsername = "alice"
	barrierFactor = "mtls"
	require.NoError(t, runTenantBarrierQuorumBeginCommand(&cobra.Command{}, nil))
	require.NotNil(t, fc.lastBeginReq)
	assert.Equal(t, qrdbtransport.TenantQuorumBeginRequest{
		TenantID: "acme", Op: qrdbtransport.OpUnseal, Username: "alice", Factor: "mtls",
	}, *fc.lastBeginReq)

	barrierChallengeID = "c-1"
	barrierResponseFile = writeQuorumFile(t, "response", []byte("signed"))
	barrierShareFile = writeQuorumFile(t, "share", []byte("share"))
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierQuorumSubmitCommand(cmd, nil))
	require.NotNil(t, fc.lastSubmitReq)
	assert.Equal(t, "acme", fc.lastSubmitReq.TenantID)
	assert.Equal(t, "c-1", fc.lastSubmitReq.ChallengeID)
	assert.Equal(t, []byte("signed"), fc.lastSubmitReq.Response)
	assert.Contains(t, out.String(), "1 of 2")
}

// withTransport selects --transport for one test and restores it after.
func withTransport(t *testing.T, name string) {
	t.Helper()
	orig := transport
	transport = name
	t.Cleanup(func() { transport = orig })
}

// TestTenantBarrierQuorum_UnixRefusesBeforeDialling proves begin and submit
// over the Unix transport refuse with the typed go-qrdb#1156 error before the
// client is built.
func TestTenantBarrierQuorum_UnixRefusesBeforeDialling(t *testing.T) {
	commands := map[string]func(*cobra.Command, []string) error{
		"begin":  runTenantBarrierQuorumBeginCommand,
		"submit": runTenantBarrierQuorumSubmitCommand,
	}
	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			defer resetTenantBarrierFlags()
			withTransport(t, "unix")
			dialled := false
			orig := tenantBarrierClientFactory
			tenantBarrierClientFactory = func() (TenantBarrierClient, error) {
				dialled = true
				return &fakeTenantBarrierClient{}, nil
			}
			defer func() { tenantBarrierClientFactory = orig }()

			resetTenantBarrierFlags()
			tenantBarrierTenantID = "acme"
			barrierQuorumOp = qrdbtransport.OpUnseal
			barrierUsername = "alice"
			barrierFactor = "mtls"
			barrierChallengeID = "c-1"
			barrierResponseFile = writeQuorumFile(t, "response", []byte("signed"))
			barrierShareFile = writeQuorumFile(t, "share", []byte("share"))

			err := run(&cobra.Command{}, nil)
			var refusal *TenantQuorumUnixUnsupportedError
			require.ErrorAs(t, err, &refusal)
			assert.True(t, sdkerrors.IsUnimplemented(err), "the refusal carries CodeUnimplemented")
			assert.Contains(t, err.Error(), "go-qrdb#1156")
			assert.False(t, dialled, "the refusal comes before any dial")
		})
	}
}

// TestTenantBarrierQuorum_OtherTransportsAreNotRefused proves the Unix
// refusal does not reach the other transports.
func TestTenantBarrierQuorum_OtherTransportsAreNotRefused(t *testing.T) {
	defer resetTenantBarrierFlags()
	withTransport(t, "rest")
	fc := &fakeTenantBarrierClient{}
	defer withFakeTenantBarrier(fc)()

	resetTenantBarrierFlags()
	tenantBarrierTenantID = "acme"
	barrierQuorumOp = qrdbtransport.OpUnseal
	barrierUsername = "alice"
	barrierFactor = "mtls"
	require.NoError(t, runTenantBarrierQuorumBeginCommand(&cobra.Command{}, nil))
	assert.NotNil(t, fc.lastBeginReq)
}

// TestTenantBarrierQuorumSubmit_RefusesWithoutAShareFile stops before dialling.
func TestTenantBarrierQuorumSubmit_RefusesWithoutAShareFile(t *testing.T) {
	withTransport(t, "rest")
	defer resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{}
	defer withFakeTenantBarrier(fc)()

	resetTenantBarrierFlags()
	tenantBarrierTenantID = "acme"
	barrierQuorumOp = qrdbtransport.OpUnseal
	barrierUsername = "alice"
	barrierFactor = "mtls"
	barrierChallengeID = "c-1"
	barrierResponseFile = writeQuorumFile(t, "response", []byte("signed"))

	err := runTenantBarrierQuorumSubmitCommand(&cobra.Command{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--share-file is required")
	assert.Nil(t, fc.lastSubmitReq)
}
