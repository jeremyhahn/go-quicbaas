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

//go:build desktop

package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// installTestEmbeddedFleetCA publishes an in-memory CA into the desktop hook,
// as EmbeddedServerBinding.Start does, and clears both seams when the test ends.
func installTestEmbeddedFleetCA(t *testing.T, name string) {
	t.Helper()
	// The issuer needs both seams the server publishes at start: the CA and
	// the issued-certificate record store. The store is in memory and owned
	// by this test alone.
	records, err := service.NewKVFleetNodeCertRecords(qrdbsdk.NewMemoryKVStore())
	require.NoError(t, err)
	installEmbeddedFleetCA(xkmsca.New(t, name))
	installEmbeddedFleetCertRecords(records)
	t.Cleanup(func() {
		uninstallEmbeddedFleetCA()
		uninstallEmbeddedFleetCertRecords()
		uninstallEmbeddedSPIFFETrustDomain()
	})
}

// issueThroughDesktopHook resolves the issuer and the trust domain through the
// desktop seams and issues a node certificate, as createFleetDeployService and
// the deploy service do.
func issueThroughDesktopHook(t *testing.T) (*service.IssuedFleetNodeCertificate, error) {
	t.Helper()
	issuer, err := resolveFleetNodeCertIssuer()
	require.NoError(t, err)
	return issuer.IssueFleetNodeCertificate(context.Background(), &service.FleetNodeCertificateRequest{
		TenantID:          "automatethethings",
		Hostname:          "agent01.example.com",
		ValidityDays:      1,
		SPIFFETrustDomain: resolveDeploySPIFFETrustDomain(),
	})
}

// The trust domain the embedded server publishes reaches the in-process CA,
// which then mints the agent SPIFFE SAN spiffe://<tenant>/svc/<cn> enrolment
// requires. Without the trust domain it mints nothing (the test below).
func TestDesktopDeployHook_PassesThePublishedTrustDomain(t *testing.T) {
	const trustDomain = "quicbaas.example.test"
	installTestEmbeddedFleetCA(t, "QuicBaaS desktop hook Root CA")
	installEmbeddedSPIFFETrustDomain(trustDomain)
	require.Equal(t, trustDomain, resolveDeploySPIFFETrustDomain())

	issued, err := issueThroughDesktopHook(t)
	require.NoError(t, err)
	defer issued.PrivateKey.Destroy()

	block, _ := pem.Decode(issued.CertPEM)
	require.NotNil(t, block, "issued certificate is not PEM")
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	require.NotEmpty(t, leaf.URIs, "certificate carries no SPIFFE SAN; enrolment would refuse it")
	assert.Equal(t, "spiffe://automatethethings/svc/agent-agent01.example.com", leaf.URIs[0].String())
}

// With no trust domain published, the hook's in-process CA refuses with the
// typed error and issues nothing.
func TestDesktopDeployHook_RefusesWithoutATrustDomain(t *testing.T) {
	installTestEmbeddedFleetCA(t, "QuicBaaS desktop hook no-domain Root CA")
	installEmbeddedSPIFFETrustDomain("")
	require.Empty(t, resolveDeploySPIFFETrustDomain())

	issued, err := issueThroughDesktopHook(t)
	var noTD *service.FleetNodeCertNoTrustDomainError
	require.ErrorAs(t, err, &noTD)
	assert.Equal(t, "automatethethings", noTD.TenantID)
	assert.Nil(t, issued)
}
