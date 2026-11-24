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

// ca_service_adapter_fleet_profile_test.go runs the remote fleet node issuer
// against the server's CA adapter over a REAL go-xkms CA, so the certificate
// a remote fleet deploy receives is the one the enrollment check sees.

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

const fleetProfileTestTenant = "automatethethings"

// newFleetProfileAdapter wraps a real software CA in the adapter with a
// SPIFFE trust domain, so issued certificates carry the tenant SAN.
func newFleetProfileAdapter(t *testing.T) *caServiceAdapter {
	t.Helper()
	adapter, err := newCAServiceAdapter(newRealSoftwareCA(t))
	require.NoError(t, err)
	adapter.SetSPIFFETrustDomain("quicbaas.test")
	return adapter
}

func TestCAServiceAdapter_FleetNodeIssuerGetsSvcClientCert(t *testing.T) {
	adapter := newFleetProfileAdapter(t)

	issuer, err := service.NewSDKFleetNodeCertIssuer(adapter)
	require.NoError(t, err)

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &service.FleetNodeCertificateRequest{
		TenantID: fleetProfileTestTenant,
		Hostname: "agent-1.example",
	})
	require.NoError(t, err)
	require.NotNil(t, issued)
	defer issued.PrivateKey.Destroy()

	leaf, err := x509.ParseCertificate(decodePEM(t, issued.CertPEM))
	require.NoError(t, err)

	assert.NoError(t, service.CheckFleetNodeCertIdentity(leaf, fleetProfileTestTenant))
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leaf.ExtKeyUsage)
}

func TestCAServiceAdapter_ClientProfileStaysUserKind(t *testing.T) {
	adapter := newFleetProfileAdapter(t)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{
			CommonName:         "agent-agent-1.example",
			Organization:       fleetProfileTestTenant,
			OrganizationalUnit: fleetProfileTestTenant,
		},
		SANS: &xkmssdk.TrustedCASubjectAlternativeNames{
			DNS: []string{"agent-1.example"},
		},
	}, key)
	require.NoError(t, err)

	resp, err := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
		CSRPEM:       csrPEM,
		Profile:      "client",
		ValidityDays: 30,
		TenantID:     fleetProfileTestTenant,
	})
	require.NoError(t, err)

	leaf, err := x509.ParseCertificate(decodePEM(t, resp.CertificatePEM))
	require.NoError(t, err)

	err = service.CheckFleetNodeCertIdentity(leaf, fleetProfileTestTenant)
	var kindErr *service.FleetNodeCertKindUnavailableError
	require.True(t, errors.As(err, &kindErr), "want *FleetNodeCertKindUnavailableError, got %v", err)
	assert.Equal(t, qrdbsdk.SpiffeKindUser, kindErr.Kind)
}
