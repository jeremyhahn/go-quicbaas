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

// Package main: factory-error and typed-error coverage for the DEK /
// dynamic-secrets / trust / ACME command trees.
//
// The SDK factories can only be exercised on their error path here: the
// package's fake SDK client returns a nil SecurityService, so the success
// path needs a live client and is covered by the integration suite. This
// mirrors ca_factory_test.go.
package main

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// SDK factory error propagation
// =============================================================================

func TestSecuritySDKFactories_PropagateFactoryError(t *testing.T) {
	sentinel := stderrors.New("factory boom")

	t.Run("DEK", func(t *testing.T) {
		withFakeSDKFactory(t, nil, sentinel)
		c, err := createSDKDEKServiceClient()
		require.ErrorIs(t, err, sentinel)
		assert.Nil(t, c)
	})

	t.Run("DynamicSecrets", func(t *testing.T) {
		withFakeSDKFactory(t, nil, sentinel)
		c, err := createSDKDynamicSecretsClient()
		require.ErrorIs(t, err, sentinel)
		assert.Nil(t, c)
	})

	t.Run("Trust", func(t *testing.T) {
		withFakeSDKFactory(t, nil, sentinel)
		c, err := createSDKTrustClient()
		require.ErrorIs(t, err, sentinel)
		assert.Nil(t, c)
	})

	t.Run("ACMEServer", func(t *testing.T) {
		withFakeSDKFactory(t, nil, sentinel)
		c, err := createSDKACMEServerClient()
		require.ErrorIs(t, err, sentinel)
		assert.Nil(t, c)
	})

	t.Run("ACMEClient", func(t *testing.T) {
		withFakeSDKFactory(t, nil, sentinel)
		c, err := createSDKACMEProtocolClient()
		require.ErrorIs(t, err, sentinel)
		assert.Nil(t, c)
	})
}

func TestResetFactories_RestoresSecurityDefaults(t *testing.T) {
	sentinel := stderrors.New("swapped")
	sdkDEKClientFactory = func() (quicbaas.DEKClient, error) { return nil, sentinel }
	t.Cleanup(resetFactories)

	resetFactories()

	// After the reset the factory must be the real SDK constructor again,
	// which surfaces the underlying sdkClientFactory error rather than the
	// swapped sentinel.
	withFakeSDKFactory(t, nil, stderrors.New("factory boom"))
	_, err := sdkDEKClientFactory()
	require.Error(t, err)
	assert.NotErrorIs(t, err, sentinel)
}

// =============================================================================
// Typed error rendering
// =============================================================================

func TestSecurityCLITypedErrors_Messages(t *testing.T) {
	underlying := stderrors.New("disk on fire")

	tests := []struct {
		name     string
		err      error
		contains []string
		unwraps  bool
	}{
		{
			name:     "acme flag required",
			err:      &ACMEFlagRequiredError{Verb: "issue", Flag: "csr"},
			contains: []string{"acme issue", "--csr"},
		},
		{
			name:     "acme arg required",
			err:      &ACMEArgRequiredError{Verb: "renew", Arg: "cert-url"},
			contains: []string{"acme renew", "<cert-url>"},
		},
		{
			name:     "acme unknown challenge",
			err:      &ACMEUnknownChallengeError{Name: "tls-alpn-01"},
			contains: []string{"tls-alpn-01", "http-01"},
		},
		{
			name:     "acme identifier format",
			err:      &ACMEIdentifierFormatError{Value: "example.com"},
			contains: []string{"example.com", "<type>:<value>"},
		},
		{
			name:     "acme read",
			err:      &ACMEReadError{Source: "/tmp/x.csr", Err: underlying},
			contains: []string{"/tmp/x.csr", "disk on fire"},
			unwraps:  true,
		},
		{
			name:     "acme write",
			err:      &ACMEWriteError{Path: "/tmp/out.crt", Err: underlying},
			contains: []string{"/tmp/out.crt", "disk on fire"},
			unwraps:  true,
		},
		{
			name:     "acme enroll CA unsupported",
			err:      &ACMEEnrollCAUnsupportedError{},
			contains: []string{"TCG-CSR-IDEVID"},
		},
		{
			name:     "trust flag required",
			err:      &TrustFlagRequiredError{Verb: "publish-dane", Flag: "name"},
			contains: []string{"trust publish-dane", "--name"},
		},
		{
			name:     "trust invalid port",
			err:      &TrustInvalidPortError{Port: 70000},
			contains: []string{"70000", "1-65535"},
		},
		{
			name:     "trust read",
			err:      &TrustReadError{Source: "chain.pem", Err: underlying},
			contains: []string{"chain.pem", "disk on fire"},
			unwraps:  true,
		},
		{
			name:     "trust write",
			err:      &TrustWriteError{Path: "roots.txt", Err: underlying},
			contains: []string{"roots.txt", "disk on fire"},
			unwraps:  true,
		},
		{
			name:     "trust no certificates",
			err:      &TrustNoCertificatesError{Source: "chain.pem"},
			contains: []string{"chain.pem", "CERTIFICATE"},
		},
		{
			name:     "trust certificate parse",
			err:      &TrustCertificateParseError{Source: "chain.pem", Index: 1, Err: underlying},
			contains: []string{"chain.pem", "#1", "disk on fire"},
			unwraps:  true,
		},
		{
			name:     "trust DANE publisher",
			err:      &TrustDANEPublisherError{Err: underlying},
			contains: []string{"disk on fire", "go-trusted-platform", "DANE"},
			unwraps:  true,
		},
		{
			name:     "dek arg required",
			err:      &DEKArgRequiredError{Verb: "get", Arg: "path"},
			contains: []string{"dek get", "<path>"},
		},
		{
			name:     "dek flag invalid",
			err:      &DEKFlagInvalidError{Verb: "list", Flag: "limit", Reason: "must not be negative"},
			contains: []string{"dek list", "--limit", "must not be negative"},
		},
		{
			name:     "dynamic-secrets arg required",
			err:      &DynamicSecretsArgRequiredError{Verb: "role get", Arg: "name"},
			contains: []string{"dynamic-secrets role get", "<name>"},
		},
		{
			name:     "dynamic-secrets flag invalid",
			err:      &DynamicSecretsFlagInvalidError{Verb: "generate", Flag: "ttl", Reason: "must not be negative"},
			contains: []string{"dynamic-secrets generate", "--ttl"},
		},
		{
			name:     "dynamic-secrets config format",
			err:      &DynamicSecretsConfigFormatError{Value: "hostdb"},
			contains: []string{"hostdb", "key=value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, want := range tt.contains {
				assert.Contains(t, msg, want)
			}
			if tt.unwraps {
				require.ErrorIs(t, tt.err, underlying, "the typed error must unwrap to its cause")
			}
		})
	}
}

// =============================================================================
// ACME challenge verifier adapters
// =============================================================================

func TestACMEVerifierAdapters(t *testing.T) {
	// endorse-01 performs no network verification by design (issuance is
	// gated by External Account Binding), so the adapter must succeed.
	require.NoError(t, acmeVerifyEndorse01(nil, nil, "example.com", "80", "token", "keyauth"))

	// dns-01 fails closed before any DNS I/O when no DNS service is
	// configured in this process.
	require.Error(t, acmeVerifyDNS01(nil, nil, "example.com", "80", "token", "keyauth"))

	// http-01 builds "http://<domain>:<port>/..."; a domain containing a
	// space makes the URL unparsable, so the adapter surfaces the error
	// without opening a socket.
	require.Error(t, acmeVerifyHTTP01(nil, nil, "invalid host", "80", "token", "keyauth"))
}
