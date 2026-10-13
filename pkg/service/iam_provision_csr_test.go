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

package service

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCSR is a well-formed P-256 admin CSR.
var testCSR = mustCSRPEM(mustECDSAKey(elliptic.P256()))

func mustECDSAKey(curve elliptic.Curve) crypto.Signer {
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		panic(err)
	}
	return key
}

func mustCSRPEM(key crypto.Signer) string {
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "admin"}}, key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func TestValidateAdminCSR_AcceptsThePolicyKeys(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	keys := map[string]crypto.Signer{
		"RSA-2048":   rsaKey,
		"ECDSA P256": mustECDSAKey(elliptic.P256()),
		"ECDSA P384": mustECDSAKey(elliptic.P384()),
		"ECDSA P521": mustECDSAKey(elliptic.P521()),
		"Ed25519":    edKey,
	}
	for name, key := range keys {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, validateAdminCSR(mustCSRPEM(key)))
		})
	}
}

func TestValidateAdminCSR_RefusesMalformedRequests(t *testing.T) {
	valid := []byte(testCSR)
	block, _ := pem.Decode(valid)
	require.NotNil(t, block)
	tampered := append([]byte(nil), block.Bytes...)
	// The last byte belongs to the signature, so the request still parses but
	// no longer verifies.
	tampered[len(tampered)-1] ^= 0xff

	tests := map[string]struct {
		csr    string
		reason string
	}{
		"not PEM":          {csr: "not a csr", reason: "not a PEM CERTIFICATE REQUEST block"},
		"wrong block type": {csr: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})), reason: "not a PEM CERTIFICATE REQUEST block"},
		"garbage DER":      {csr: "-----BEGIN CERTIFICATE REQUEST-----\nAA==\n-----END CERTIFICATE REQUEST-----\n", reason: "unparseable request"},
		"bad signature":    {csr: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: tampered})), reason: "signature does not verify"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateAdminCSR(tt.csr)
			var invalid *ProvisionAdminCSRInvalidError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tt.reason, invalid.Reason)
			assert.NotEmpty(t, err.Error())
		})
	}
}

func TestValidateAdminCSR_RefusesKeysOutsideThePolicy(t *testing.T) {
	weakRSA, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)

	tests := map[string]struct {
		key  crypto.Signer
		want string
	}{
		"short RSA": {key: weakRSA, want: "RSA-1024"},
		"P-224":     {key: mustECDSAKey(elliptic.P224()), want: "ECDSA P-224"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateAdminCSR(mustCSRPEM(tt.key))
			assert.Equal(t, &ProvisionAdminCSRKeyError{Key: tt.want}, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	assert.Equal(t, &ProvisionAdminCSRKeyError{Key: "string"}, checkAdminKey("not a key"),
		"an unknown key type is refused, not waved through")
}

func TestIAMProvisionTenantRequest_ValidateRefusesBarrierRoutedIsolation(t *testing.T) {
	for _, isolation := range []qrdbsdk.TenantIsolationType{"", qrdbsdk.TenantIsolationBarrier, qrdbsdk.TenantIsolationVolume, qrdbsdk.TenantIsolationRaft} {
		t.Run("isolation "+string(isolation), func(t *testing.T) {
			for _, req := range []*IAMProvisionTenantRequest{
				{TenantID: "acme", IsolationType: isolation, AdminCSRPEM: testCSR},
				{TenantID: "acme", IsolationType: isolation, AdminEnrollment: IAMTenantAdminEnrollInvite},
			} {
				err := req.Validate()
				var barrierErr *ProvisionBarrierIsolationError
				require.ErrorAs(t, err, &barrierErr, "the default and every barrier-routed type are refused before any write")
				assert.NotEmpty(t, barrierErr.Isolation, "the empty default is reported as the barrier it means")
				assert.Contains(t, err.Error(), "prefix", "the error names the isolation that works")
			}
		})
	}
}

func TestIAMProvisionTenantRequest_ValidateAcceptsPrefixAndTheSystemTenant(t *testing.T) {
	prefix := &IAMProvisionTenantRequest{TenantID: "acme", IsolationType: qrdbsdk.TenantIsolationPrefix, AdminCSRPEM: testCSR}
	assert.NoError(t, prefix.Validate())

	// The system tenant always resolves to the shared store, whatever its
	// isolation type says.
	system := &IAMProvisionTenantRequest{TenantID: qrdbsdk.SystemTenantID, AdminCSRPEM: testCSR}
	assert.NoError(t, system.Validate())
}
