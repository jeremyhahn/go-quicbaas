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

// Package main provides tests for the OAuth2 client CLI commands.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// writePEMCert writes a self-signed certificate to a temp file and returns the path.
func writePEMCert(t *testing.T) (string, *x509.Certificate) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "client.pem")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create PEM file: %v", err)
	}
	defer f.Close()

	if encErr := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); encErr != nil {
		t.Fatalf("encode PEM: %v", encErr)
	}

	return path, cert
}

// withMockIAM installs a mock IAM client factory and restores the original on cleanup.
func withMockIAM(t *testing.T, mock *testMockIAMClient) {
	t.Helper()
	orig := iamClientFactory
	t.Cleanup(func() { iamClientFactory = orig })
	iamClientFactory = func() (sdkiam.Client, error) { return mock, nil }
}

// withTenant sets and restores the global tenant ID.
func withTenant(t *testing.T, id string) {
	t.Helper()
	orig := globalTenantID
	t.Cleanup(func() { globalTenantID = orig })
	globalTenantID = id
}

// TestOAuth2ClientRegister_SecretBasic verifies successful registration with client_secret_basic.
func TestOAuth2ClientRegister_SecretBasic(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "acme")

	// Reset flag values.
	oauth2ClientName = "my-service"
	oauth2ClientAuthMethod = "client_secret_basic"
	oauth2ClientGrantType = "client_credentials"
	oauth2ClientCertPEM = ""
	oauth2ClientThumbprint = ""
	oauth2ClientScopes = nil

	cmd := newOAuth2ClientRegisterCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	_ = out // output goes to os.Stdout in the implementation; we verify no error
}

// TestOAuth2ClientRegister_SelfSignedMTLS verifies registration with a cert PEM.
func TestOAuth2ClientRegister_SelfSignedMTLS(t *testing.T) {
	certPath, _ := writePEMCert(t)

	var capturedReq *sdkiam.RegisterAppRequest
	mock := newTestMockIAMClient()
	mock.appClient.RegisterFunc = func(ctx context.Context, tenantID string, req *sdkiam.RegisterAppRequest) (*sdkiam.Application, error) {
		capturedReq = req
		return &sdkiam.Application{
			ClientID:           "app_mtls_001",
			TenantID:           tenantID,
			Name:               req.Name,
			MTLSCertThumbprint: req.MTLSCertThumbprint,
		}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	// Construct the command first; cobra's StringVar(default) writes to the
	// package-level flag globals during construction, so any pre-set values
	// must come AFTER newOAuth2ClientRegisterCommand returns.
	cmd := newOAuth2ClientRegisterCommand()
	oauth2ClientName = "gpu-agent"
	oauth2ClientAuthMethod = "self_signed_tls_client_auth"
	oauth2ClientGrantType = "client_credentials"
	oauth2ClientCertPEM = certPath
	oauth2ClientThumbprint = ""
	oauth2ClientScopes = nil

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("Register was not called")
	}
	if capturedReq.MTLSCertThumbprint == "" {
		t.Error("expected MTLSCertThumbprint to be set from PEM file")
	}
	if capturedReq.TokenEndpointAuthMethod != sdkiam.TokenEndpointAuthSelfSignedTLSClientAuth {
		t.Errorf("expected self_signed_tls_client_auth, got %v", capturedReq.TokenEndpointAuthMethod)
	}
}

// TestOAuth2ClientRegister_MissingCertForMTLS verifies that mTLS methods require a cert.
func TestOAuth2ClientRegister_MissingCertForMTLS(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientRegisterCommand()
	oauth2ClientName = "no-cert-agent"
	oauth2ClientAuthMethod = "self_signed_tls_client_auth"
	oauth2ClientGrantType = "client_credentials"
	oauth2ClientCertPEM = ""
	oauth2ClientThumbprint = ""
	oauth2ClientScopes = nil

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error when cert is missing for mTLS, got nil")
	}
	if !strings.Contains(err.Error(), "cert") {
		t.Errorf("expected cert-related error, got: %v", err)
	}
}

// TestOAuth2ClientRegister_MissingTenant verifies that --tenant is required.
func TestOAuth2ClientRegister_MissingTenant(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "")

	oauth2ClientName = "some-service"
	oauth2ClientAuthMethod = "client_secret_basic"

	cmd := newOAuth2ClientRegisterCommand()
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing tenant, got nil")
	}
}

// TestOAuth2ClientList_Success verifies that list returns results.
func TestOAuth2ClientList_Success(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientListCommand()
	err := cmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestOAuth2ClientList_MissingTenant verifies that --tenant is required.
func TestOAuth2ClientList_MissingTenant(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "")

	cmd := newOAuth2ClientListCommand()
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing tenant, got nil")
	}
}

// TestOAuth2ClientShow_Success verifies that show returns client details.
func TestOAuth2ClientShow_Success(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientShowCommand()
	err := cmd.RunE(cmd, []string{"app_abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestOAuth2ClientShow_Error verifies error propagation.
func TestOAuth2ClientShow_Error(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.GetFunc = func(ctx context.Context, tenantID, clientID string) (*sdkiam.Application, error) {
		return nil, &testError{msg: "client not found"}
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientShowCommand()
	err := cmd.RunE(cmd, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestOAuth2ClientDelete_WithForce verifies deletion with iamForce=true.
func TestOAuth2ClientDelete_WithForce(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "acme")

	origForce := iamForce
	iamForce = true
	t.Cleanup(func() { iamForce = origForce })

	cmd := newOAuth2ClientDeleteCommand()
	err := cmd.RunE(cmd, []string{"app_abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestOAuth2ClientDelete_Error verifies error propagation from Delete.
func TestOAuth2ClientDelete_Error(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.DeleteFunc = func(ctx context.Context, tenantID, clientID string) error {
		return &testError{msg: "delete failed"}
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	origForce := iamForce
	iamForce = true
	t.Cleanup(func() { iamForce = origForce })

	cmd := newOAuth2ClientDeleteCommand()
	err := cmd.RunE(cmd, []string{"app_abc123"})
	if err == nil {
		t.Fatal("expected error from Delete, got nil")
	}
}

// TestOAuth2ClientPinCert_WithPEM verifies pin-cert updates the thumbprint.
func TestOAuth2ClientPinCert_WithPEM(t *testing.T) {
	certPath, _ := writePEMCert(t)

	var capturedUpdate *sdkiam.UpdateAppRequest
	mock := newTestMockIAMClient()
	mock.appClient.GetFunc = func(ctx context.Context, tenantID, clientID string) (*sdkiam.Application, error) {
		return &sdkiam.Application{
			ClientID:                clientID,
			TenantID:                tenantID,
			TokenEndpointAuthMethod: sdkiam.TokenEndpointAuthSelfSignedTLSClientAuth,
		}, nil
	}
	mock.appClient.UpdateFunc = func(ctx context.Context, tenantID, clientID string, req *sdkiam.UpdateAppRequest) (*sdkiam.Application, error) {
		capturedUpdate = req
		return &sdkiam.Application{ClientID: clientID, MTLSCertThumbprint: req.MTLSCertThumbprint}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientPinCertCommand()
	oauth2ClientCertPEM = certPath
	oauth2ClientThumbprint = ""

	err := cmd.RunE(cmd, []string{"app_abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedUpdate == nil {
		t.Fatal("Update was not called")
	}
	if capturedUpdate.MTLSCertThumbprint == "" {
		t.Error("expected thumbprint to be set in update request")
	}
}

// TestOAuth2ClientPinCert_WrongAuthMethod verifies rejection for non-mTLS clients.
func TestOAuth2ClientPinCert_WrongAuthMethod(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.GetFunc = func(ctx context.Context, tenantID, clientID string) (*sdkiam.Application, error) {
		return &sdkiam.Application{
			ClientID:                clientID,
			TenantID:                tenantID,
			TokenEndpointAuthMethod: sdkiam.TokenEndpointAuthClientSecretBasic,
		}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	oauth2ClientCertPEM = ""
	oauth2ClientThumbprint = "some-thumb"

	cmd := newOAuth2ClientPinCertCommand()
	err := cmd.RunE(cmd, []string{"app_abc123"})
	if err == nil {
		t.Fatal("expected error for non-mTLS client, got nil")
	}
}

// TestOAuth2ClientPinCert_MissingCertAndThumbprint verifies that at least one is required.
func TestOAuth2ClientPinCert_MissingCertAndThumbprint(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "acme")

	oauth2ClientCertPEM = ""
	oauth2ClientThumbprint = ""

	cmd := newOAuth2ClientPinCertCommand()
	err := cmd.RunE(cmd, []string{"app_abc123"})
	if err == nil {
		t.Fatal("expected error when both cert and thumbprint are missing, got nil")
	}
}

// testError is a simple error type for test error injection.
type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
