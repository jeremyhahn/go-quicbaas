//go:build mcp

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

package mcp

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const ceremonyInitializeTool = "qrdb_ceremony_initialize"

// TestCeremonyInitializeRoundTrip_CustodianOfficerAndCredentials sends a
// custodian ceremony through the SDK MCP adapter into the server tool and
// back. Every officer field and the policy must reach the service unchanged,
// and the credentials qrdb issued must reach the caller.
func TestCeremonyInitializeRoundTrip_CustodianOfficerAndCredentials(t *testing.T) {
	holders := []qrdbtransport.CeremonyHolderCert{
		{Username: "carol", Role: "custodian", CertPEM: "carol-cert"},
	}
	fake := &fakeCeremonyService{initInfo: &service.CeremonyInitInfo{
		State:                 "READY",
		ShareCount:            2,
		Holders:               holders,
		AdminCertPEM:          []byte("admin-cert"),
		AdminKeyPEM:           []byte("admin-key"),
		AdminCredentialSource: "generated",
	}}
	tr := registryMCPTransport(t, newCeremonyTestRegistry(t, fake), nil, ceremonyInitializeTool)

	officers := []qrdbtransport.CeremonyOfficerRequest{
		{
			Username:      "carol",
			Role:          "custodian",
			EncryptionKEM: "ML-KEM-768",
			EncryptionKey: []byte{0x01, 0x02, 0xfe},
			Factors: []qrdbtransport.HolderFactorEnrolment{
				{Name: "totp", Params: map[string]any{"digits": float64(6)}},
			},
			Designated: "totp",
		},
		{Username: "dave", CSRPEM: "csr-d"},
	}
	resp, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{
		SOPin:     "so",
		UserPin:   "user",
		Threshold: 2,
		Policy:    "custodian",
		Officers:  officers,
	})
	require.NoError(t, err)

	require.NotNil(t, fake.gotInitReq)
	assert.Equal(t, "custodian", fake.gotInitReq.Policy)
	assert.Equal(t, officers, fake.gotInitReq.Officers)

	assert.Equal(t, holders, resp.Holders)
	assert.Equal(t, "admin-cert", resp.AdminCertPEM)
	assert.Equal(t, []byte("admin-key"), resp.AdminKeyPEM)
	assert.Equal(t, "generated", resp.AdminCredentialSource)
}

// TestCeremonyInitializeRoundTrip_OmitsAbsentCredentials pins that a ceremony
// which issued no credentials returns none, rather than empty values a caller
// could mistake for a delivered admin key.
func TestCeremonyInitializeRoundTrip_OmitsAbsentCredentials(t *testing.T) {
	fake := &fakeCeremonyService{initInfo: &service.CeremonyInitInfo{State: "READY"}}
	r := newCeremonyTestRegistry(t, fake)

	out, err := r.ExecuteTool(context.Background(), ceremonyInitializeTool, map[string]interface{}{"threshold": 1})
	require.NoError(t, err)
	m, ok := out.(map[string]interface{})
	require.True(t, ok)
	for _, key := range []string{"holders", "admin_cert_pem", "admin_key_pem", "admin_credential_source"} {
		assert.NotContains(t, m, key)
	}
}

// TestCeremonyInitializeTool_RefusesUndecodableOfficers pins that officers
// which do not decode into go-qrdb's officer shape are refused with a typed
// error before the service runs.
func TestCeremonyInitializeTool_RefusesUndecodableOfficers(t *testing.T) {
	tests := []struct {
		name     string
		officers interface{}
	}{
		{"encryption key not base64", []interface{}{
			map[string]interface{}{"username": "carol", "encryption_key": "!!not-base64!!"},
		}},
		{"officers not an array", "carol"},
		{"factor params not an object", []interface{}{
			map[string]interface{}{"username": "carol", "factors": []interface{}{
				map[string]interface{}{"name": "totp", "params": []interface{}{1}},
			}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeCeremonyService{initInfo: &service.CeremonyInitInfo{}}
			r := newCeremonyTestRegistry(t, fake)

			_, err := r.ExecuteTool(context.Background(), ceremonyInitializeTool, map[string]interface{}{
				"threshold": 1,
				"officers":  tc.officers,
			})
			var argErr *CeremonyOfficersArgError
			require.True(t, stderrors.As(err, &argErr), "got %v", err)
			assert.Nil(t, fake.gotInitReq, "the service must not run")
		})
	}
}
