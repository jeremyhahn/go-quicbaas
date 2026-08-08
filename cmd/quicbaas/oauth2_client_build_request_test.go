// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spf13/cobra"

	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// resetOAuth2RegisterFlags clears the package-level register flags and restores
// them after the test, so each case starts from a known state.
func resetOAuth2RegisterFlags(t *testing.T) {
	t.Helper()
	origName := oauth2ClientName
	origMethod := oauth2ClientAuthMethod
	origGrant := oauth2ClientGrantTypes
	origScopes := oauth2ClientScopes
	origCert := oauth2ClientCertPEM
	origThumb := oauth2ClientThumbprint
	t.Cleanup(func() {
		oauth2ClientName = origName
		oauth2ClientAuthMethod = origMethod
		oauth2ClientGrantTypes = origGrant
		oauth2ClientScopes = origScopes
		oauth2ClientCertPEM = origCert
		oauth2ClientThumbprint = origThumb
	})
	oauth2ClientName = "svc"
	oauth2ClientGrantTypes = []string{"client_credentials"}
	oauth2ClientScopes = nil
	oauth2ClientCertPEM = ""
	oauth2ClientThumbprint = ""
}

func TestBuildRegisterRequest_ClientSecretPost(t *testing.T) {
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "CLIENT_SECRET_POST" // exercises ToLower normalization

	method, req, err := buildRegisterRequest(&cobra.Command{})
	require.NoError(t, err)
	assert.Equal(t, "client_secret_post", method)
	assert.Equal(t, sdkiam.TokenEndpointAuthClientSecretPost, req.TokenEndpointAuthMethod)
	assert.Equal(t, "svc", req.Name)
}

func TestBuildRegisterRequest_TLSClientAuthWithThumbprint(t *testing.T) {
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "tls_client_auth"
	oauth2ClientThumbprint = "abc123"

	method, req, err := buildRegisterRequest(&cobra.Command{})
	require.NoError(t, err)
	assert.Equal(t, "tls_client_auth", method)
	assert.Equal(t, sdkiam.TokenEndpointAuthTLSClientAuth, req.TokenEndpointAuthMethod)
	assert.Equal(t, "abc123", req.MTLSCertThumbprint)
}

func TestBuildRegisterRequest_TLSClientAuthWithPEM(t *testing.T) {
	resetOAuth2RegisterFlags(t)
	certPath, _ := writePEMCert(t)
	oauth2ClientAuthMethod = "tls_client_auth"
	oauth2ClientCertPEM = certPath

	_, req, err := buildRegisterRequest(&cobra.Command{})
	require.NoError(t, err)
	assert.NotEmpty(t, req.MTLSCertThumbprint)
	// tls_client_auth (unlike self_signed) records the subject DN.
	assert.Contains(t, req.MTLSSubjectDN, "test-client")
}

func TestBuildRegisterRequest_TLSClientAuthMissingCertAndThumbprint(t *testing.T) {
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "tls_client_auth"

	_, _, err := buildRegisterRequest(&cobra.Command{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tls_client_auth")
}

func TestBuildRegisterRequest_SelfSignedWithThumbprint(t *testing.T) {
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "self_signed_tls_client_auth"
	oauth2ClientThumbprint = "deadbeef"

	_, req, err := buildRegisterRequest(&cobra.Command{})
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", req.MTLSCertThumbprint)
	// self_signed does not populate the subject DN.
	assert.Empty(t, req.MTLSSubjectDN)
}

func TestBuildRegisterRequest_BadPEMPath(t *testing.T) {
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "tls_client_auth"
	oauth2ClientCertPEM = "/nonexistent/path/cert.pem"

	_, _, err := buildRegisterRequest(&cobra.Command{})
	require.Error(t, err)
}

func TestBuildRegisterRequest_UnsupportedAuthMethod(t *testing.T) {
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "none"

	_, _, err := buildRegisterRequest(&cobra.Command{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported --auth-method")
}
