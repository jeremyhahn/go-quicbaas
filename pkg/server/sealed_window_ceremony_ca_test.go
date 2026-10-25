//go:build !integration

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

package server

import (
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// sealedWindowInit runs `qrdb init` through the sealed-window client of a
// sealed node composed with ceremonyCA, and returns the ceremony's result.
func sealedWindowInit(t *testing.T, ceremonyCA *lateBoundCeremonyCA) (*qrdbtransport.CeremonyInitializeResponse, error) {
	t.Helper()
	node := buildSealedMemoryNode(t)
	svcs := node.Services()
	pinCfg := &config.Config{}
	pinCfg.Storage.DataDir = t.TempDir()
	pinBackend, err := buildPINBackend(pinCfg, slog.Default(), nil)
	require.NoError(t, err)

	s := &Server{logger: buildTestLogger(t), qrdbNode: node, pinBackend: pinBackend, ceremonyCA: ceremonyCA}
	client, err := newSealedWindowClient(s, &CeremonyServices{
		Barrier:    svcs.Barrier,
		Credential: svcs.Credential,
		Ceremony:   svcs.Ceremony,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	adminCert := string(newBYOAdminCertPEM(t))
	return client.Ceremony().CeremonyInitialize(t.Context(), &qrdbtransport.CeremonyInitializeRequest{
		SOPin:            "sealed-window-so-pin",
		UserPin:          "sealed-window-user-pin",
		Hostname:         "node.example.test",
		AdminCertPEM:     adminCert,
		AdminCertPoolPEM: adminCert,
		Secret:           []byte("sealed-window-passphrase"),
	})
}

// The sealed-window client hands `qrdb init` the server's late-bound
// platform CA, so the ceremony issues the node's TLS certificate from it.
func TestNewSealedWindowClient_InitializeIssuesFromThePlatformCA(t *testing.T) {
	caSrv := withTempServer(t)
	caSrv.config.IAM.SPIFFETrustDomain = "example.test"
	caSrv.xkmsCA = newFakeTrustedCA(t)
	ceremonyCA := &lateBoundCeremonyCA{}
	ceremonyCA.Set(caSrv.ceremonyCAAdapter())
	platformCA, err := ceremonyCA.CACertificate(t.Context())
	require.NoError(t, err)

	resp, err := sealedWindowInit(t, ceremonyCA)
	require.NoError(t, err)
	block, _ := pem.Decode([]byte(resp.TLSCertPEM))
	require.NotNil(t, block, "the ceremony issued no TLS certificate")
	tlsCert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	require.NoError(t, tlsCert.CheckSignatureFrom(platformCA), "the TLS certificate must be issued by the platform CA")
}

// Before the platform CA is bound, the late-bound CA fails the ceremony
// closed with the typed error instead of initializing without a CA.
func TestNewSealedWindowClient_InitializeFailsClosedWithoutTheCA(t *testing.T) {
	_, err := sealedWindowInit(t, &lateBoundCeremonyCA{})
	var unavailable *CeremonyCAUnavailableError
	require.ErrorAs(t, err, &unavailable)
}
