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
	"encoding/pem"
	stderrors "errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// holderCSRPEM returns a PEM CSR for a share holder named commonName.
func holderCSRPEM(t *testing.T, commonName string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: commonName}}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

// holderSpec writes a CSR for username and returns its username=path value.
func holderSpec(t *testing.T, username string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), username+".csr")
	require.NoError(t, os.WriteFile(path, []byte(holderCSRPEM(t, username)), 0o600))
	return username + "=" + path
}

// shamirInit sets the flags of `tenant barrier init --strategy shamir` for a
// 2-of-shares quorum on tenantID.
func shamirInit(tenantID string, shares int) {
	resetTenantBarrierFlags()
	tenantBarrierTenantID = tenantID
	tenantBarrierStrategy = "shamir"
	tenantBarrierThreshold = 2
	tenantBarrierShares = shares
}

// TestTenantBarrierInit_ForwardsTheRoster proves the holder roster and policy
// reach go-qrdb: each --officers value becomes a Security Officer and each
// --custodians value a key custodian, carrying the CSR read from its file.
func TestTenantBarrierInit_ForwardsTheRoster(t *testing.T) {
	shamirInit("acme", 3)
	fc := &fakeTenantBarrierClient{}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierOfficers = []string{holderSpec(t, "alice"), holderSpec(t, "bob")}
	tenantBarrierCustodians = []string{holderSpec(t, "carol")}
	tenantBarrierPolicy = qrdbtransport.PolicyAnyEnrolled

	require.NoError(t, runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil))

	req := fc.lastInitReq
	require.NotNil(t, req)
	require.Len(t, req.Officers, 3)
	for i, want := range []struct{ username, role string }{
		{"alice", qrdbtransport.RoleSecurityOfficer},
		{"bob", qrdbtransport.RoleSecurityOfficer},
		{"carol", qrdbtransport.RoleCustodian},
	} {
		assert.Equal(t, want.username, req.Officers[i].Username)
		assert.Equal(t, want.role, req.Officers[i].Role)
		assert.Contains(t, req.Officers[i].CSRPEM, "CERTIFICATE REQUEST")
	}
	assert.Equal(t, qrdbtransport.PolicyAnyEnrolled, req.Policy)
	assert.Empty(t, fc.lastOfficerID, "a named roster needs no enrolled-holder lookup")
}

// TestTenantBarrierInit_RosterErrors proves each unusable roster is refused
// with its typed error before any init is sent.
func TestTenantBarrierInit_RosterErrors(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T)
		target any
	}{
		{"spec without a path", func(t *testing.T) {
			shamirInit("acme", 2)
			tenantBarrierOfficers = []string{"alice"}
		}, new(*TenantBarrierHolderSpecError)},
		{"spec without a username", func(t *testing.T) {
			shamirInit("acme", 2)
			tenantBarrierCustodians = []string{"=/x.csr"}
		}, new(*TenantBarrierHolderSpecError)},
		{"unreadable CSR", func(t *testing.T) {
			shamirInit("acme", 2)
			tenantBarrierOfficers = []string{"alice=" + filepath.Join(t.TempDir(), "missing.csr")}
		}, new(*InitReadCSRError)},
		{"roster smaller than shares", func(t *testing.T) {
			shamirInit("acme", 3)
			tenantBarrierOfficers = []string{holderSpec(t, "alice"), holderSpec(t, "bob")}
		}, new(*TenantBarrierRosterSizeError)},
		{"roster on a non-shamir strategy", func(t *testing.T) {
			resetTenantBarrierFlags()
			tenantBarrierTenantID = "acme"
			tenantBarrierStrategy = "none"
			tenantBarrierOfficers = []string{holderSpec(t, "alice")}
		}, new(*TenantBarrierRosterFlagsError)},
		{"policy on a non-shamir strategy", func(t *testing.T) {
			resetTenantBarrierFlags()
			tenantBarrierTenantID = "acme"
			tenantBarrierStrategy = "none"
			tenantBarrierPolicy = qrdbtransport.PolicyDesignated
		}, new(*TenantBarrierRosterFlagsError)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			fc := &fakeTenantBarrierClient{}
			restore := withFakeTenantBarrier(fc)
			defer restore()

			err := runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil)
			require.Error(t, err)
			assert.True(t, stderrors.As(err, tc.target), "got %T: %v", err, err)
			assert.Nil(t, fc.lastInitReq, "an unusable roster must not be sent")
		})
	}
}

// TestTenantBarrierInit_RosterlessNeedsEnrolledHolders proves a Shamir init
// without a roster is refused, typed, when the tenant enrolled fewer holders
// than the threshold, and that a lookup failure is returned as is.
func TestTenantBarrierInit_RosterlessNeedsEnrolledHolders(t *testing.T) {
	shamirInit("acme", 3)
	fc := enrolledTenantBarrierClient(1)
	restore := withFakeTenantBarrier(fc)
	defer restore()

	err := runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil)
	var required *TenantBarrierRosterRequiredError
	require.True(t, stderrors.As(err, &required), "got %T: %v", err, err)
	assert.Equal(t, 1, required.Enrolled)
	assert.Equal(t, 2, required.Threshold)
	assert.Equal(t, "acme", fc.lastOfficerID)
	assert.Nil(t, fc.lastInitReq)

	lookupErr := &TenantBarrierHolderSpecError{Flag: "lookup", Got: "failed"}
	fc.officerFunc = func(context.Context, string) ([]*qrdbtransport.OfficerStatus, error) {
		return nil, lookupErr
	}
	err = runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil)
	assert.ErrorIs(t, err, lookupErr)
	assert.Nil(t, fc.lastInitReq)
}

// nodeTenantBarrierClient runs the CLI's tenant barrier calls on a real
// in-process qrdb node as the tenant admin, whose identity a network
// transport would take from the mTLS connection.
type nodeTenantBarrierClient struct {
	TenantBarrierClient
	admin context.Context
}

func (c nodeTenantBarrierClient) TenantBarrierInit(_ context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	return c.TenantBarrierClient.TenantBarrierInit(c.admin, req)
}

func (c nodeTenantBarrierClient) TenantBarrierOfficerList(_ context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	return c.TenantBarrierClient.TenantBarrierOfficerList(c.admin, tenantID)
}

// freshTenantAdmin creates tenantID on n with a first admin and returns the
// admin's authenticated context.
func freshTenantAdmin(t *testing.T, n *qrdbnode.Node, tenantID string) context.Context {
	t.Helper()
	svcCtx, err := n.Node.ServiceContext(context.Background())
	require.NoError(t, err)
	adminName := tenantID + "-admin"
	resp, err := n.Node.Client().Tenants().CreateTenant(svcCtx, &qrdbtransport.CreateTenantRequest{
		ID:            tenantID,
		Name:          tenantID,
		IsolationType: qrdbtransport.TenantIsolationPrefix,
		AdminUsername: adminName,
		AdminCSRPEM:   holderCSRPEM(t, adminName),
	})
	require.NoError(t, err, "CreateTenant")
	require.NotNil(t, resp.Admin, "CreateTenant returned no admin certificate")

	block, _ := pem.Decode([]byte(resp.Admin.CertPEM))
	require.NotNil(t, block, "admin certificate is not PEM")
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	admin, _, err := n.Node.AuthMiddleware().Authenticate(qrdbsdk.WithTLSState(context.Background(),
		&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}))
	require.NoError(t, err, "Authenticate")
	return admin
}

// TestTenantBarrierInit_FreshTenantOnRealNode bootstraps a fresh tenant's
// Shamir barrier through the CLI on a real qrdb node: without a roster the
// tenant has no holders and the init is refused, typed; with one holder per
// share go-qrdb initializes the barrier and certifies every holder.
func TestTenantBarrierInit_FreshTenantOnRealNode(t *testing.T) {
	n := qrdbnode.TenantBarrierHost(t)
	const tenantID = "rosterco"
	client := nodeTenantBarrierClient{
		TenantBarrierClient: n.Node.Client().Tenants(),
		admin:               freshTenantAdmin(t, n, tenantID),
	}
	orig := tenantBarrierClientFactory
	tenantBarrierClientFactory = func() (TenantBarrierClient, error) { return client, nil }
	defer func() { tenantBarrierClientFactory = orig }()

	shamirInit(tenantID, 3)
	err := runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil)
	var required *TenantBarrierRosterRequiredError
	require.True(t, stderrors.As(err, &required), "got %T: %v", err, err)
	assert.Zero(t, required.Enrolled)

	shamirInit(tenantID, 3)
	tenantBarrierOfficers = []string{holderSpec(t, "alice"), holderSpec(t, "bob")}
	tenantBarrierCustodians = []string{holderSpec(t, "carol")}
	var out bytes.Buffer
	cmd := &cobra.Command{Use: "init"}
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierInitCommand(cmd, nil))
	assert.Contains(t, out.String(), "2-of-3")
	assert.Contains(t, out.String(), "alice (so)")
	assert.Contains(t, out.String(), "carol (custodian)")

	enrolled, err := client.TenantBarrierOfficerList(context.Background(), tenantID)
	require.NoError(t, err)
	assert.Len(t, enrolled, 3, "go-qrdb enrolls the whole roster")
}
