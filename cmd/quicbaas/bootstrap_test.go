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

// Package main provides flag-parsing tests for the canonical
// 'quicbaas bootstrap' command tree.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	trustbootstrap "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/bootstrap"
)

// =============================================================================
// Mock BootstrapClient
// =============================================================================

type fakeBootstrapClient struct {
	autoFunc func(ctx context.Context, req *BootstrapAutoRequest) (*BootstrapAutoResponse, error)
	lastReq  *BootstrapAutoRequest
}

func (f *fakeBootstrapClient) BootstrapAuto(ctx context.Context, req *BootstrapAutoRequest) (*BootstrapAutoResponse, error) {
	f.lastReq = req
	if f.autoFunc != nil {
		return f.autoFunc(ctx, req)
	}
	return &BootstrapAutoResponse{
		Method:      req.Method,
		CABundlePEM: "-----BEGIN CERTIFICATE-----\nstub\n-----END CERTIFICATE-----\n",
		SPKIPin:     "sha256/abcdef",
	}, nil
}

// withFakeBootstrap installs fc as the active bootstrap factory and
// returns a restore func.
func withFakeBootstrap(fc *fakeBootstrapClient) func() {
	orig := bootstrapClientFactory
	bootstrapClientFactory = func() (BootstrapClient, error) { return fc, nil }
	return func() { bootstrapClientFactory = orig }
}

// resetBootstrapFlags zeroes the package-level flag variables.
func resetBootstrapFlags() {
	bootstrapMethod = "auto"
	bootstrapServerURL = ""
	bootstrapBundleOut = ""
	bootstrapOutputFmt = "table"
	bootstrapHostname = ""
	bootstrapDNSServer = ""
	bootstrapNoiseAddr = ""
	bootstrapNoiseStaticKey = ""
	bootstrapNoiseP256StaticKey = ""
	bootstrapNoiseSuite = ""
	bootstrapSPKIPin = ""
}

// =============================================================================
// Command tree shape
// =============================================================================

// TestNewBootstrapCommand_Shape verifies the canonical surface.
func TestNewBootstrapCommand_Shape(t *testing.T) {
	cmd := newBootstrapCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "bootstrap", cmd.Use)

	expected := map[string]bool{
		"auto":   false,
		"dane":   false,
		"noise":  false,
		"spki":   false,
		"direct": false,
	}
	fetchFlags := []string{
		"server-url", "bundle-output",
		"hostname", "dns-server",
		"noise-addr", "noise-static-key", "noise-p256-static-key", "noise-suite",
		"spki-pin",
	}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
			for _, name := range fetchFlags {
				assert.NotNil(t, sub.Flags().Lookup(name), "'bootstrap %s' lacks --%s", sub.Name(), name)
			}
			if sub.Name() == "auto" {
				assert.NotNil(t, sub.Flags().Lookup("method"), "'bootstrap auto' lacks --method")
			} else {
				assert.Nil(t, sub.Flags().Lookup("method"), "'bootstrap %s' must not take --method", sub.Name())
			}
		}
	}
	for verb, present := range expected {
		assert.True(t, present, "expected 'bootstrap %s' subcommand", verb)
	}
}

// =============================================================================
// auto
// =============================================================================

// TestBootstrapAuto_RejectsInvalidMethod returns a typed error.
func TestBootstrapAuto_RejectsInvalidMethod(t *testing.T) {
	defer resetBootstrapFlags()
	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "telepathy"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BootstrapInvalidMethodError
	assert.ErrorAs(t, err, &typed)
}

// TestBootstrapAuto_AcceptsCanonicalMethods verifies all canonical methods.
func TestBootstrapAuto_AcceptsCanonicalMethods(t *testing.T) {
	for _, method := range []string{"auto", "dane", "noise", "spki", "direct"} {
		t.Run(method, func(t *testing.T) {
			defer resetBootstrapFlags()
			fc := &fakeBootstrapClient{}
			defer withFakeBootstrap(fc)()

			cmd := newBootstrapAutoCommand()
			cmd.SetArgs([]string{"--method", method})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)

			require.NoError(t, cmd.Execute())
			require.NotNil(t, fc.lastReq)
			assert.Equal(t, method, fc.lastReq.Method)
		})
	}
}

// TestBootstrapAuto_WritesBundleFile verifies --bundle-output.
func TestBootstrapAuto_WritesBundleFile(t *testing.T) {
	defer resetBootstrapFlags()
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "ca.pem")

	fc := &fakeBootstrapClient{}
	defer withFakeBootstrap(fc)()

	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "auto", "--bundle-output", bundlePath})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	got, err := os.ReadFile(bundlePath)
	require.NoError(t, err)
	assert.Contains(t, string(got), "BEGIN CERTIFICATE")
}

// TestBootstrapAuto_PrintsBundleToStdout when no --bundle-output is given.
func TestBootstrapAuto_PrintsBundleToStdout(t *testing.T) {
	defer resetBootstrapFlags()
	fc := &fakeBootstrapClient{}
	defer withFakeBootstrap(fc)()

	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "auto"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "BOOTSTRAP RESULT")
	assert.Contains(t, out.String(), "BEGIN CERTIFICATE")
}

// TestBootstrapAuto_FactoryError surfaces the typed pending-SDK error
// from the default factory when the SDK has no bootstrap surface.
func TestBootstrapAuto_FactoryError(t *testing.T) {
	defer resetBootstrapFlags()
	orig := bootstrapClientFactory
	defer func() { bootstrapClientFactory = orig }()

	wantErr := errors.New("factory failure")
	bootstrapClientFactory = func() (BootstrapClient, error) { return nil, wantErr }

	cmd := newBootstrapAutoCommand()
	cmd.SetArgs([]string{"--method", "auto"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory failure")
}

// =============================================================================
// SDK-backed client (client-side go-xkms Fetch)
// =============================================================================

// withFakeFetch swaps the SDK fetch seam for fn and restores it on cleanup.
func withFakeFetch(t *testing.T, fn func(context.Context, trustbootstrap.Config) (*trustbootstrap.CABundle, error)) {
	t.Helper()
	orig := bootstrapFetch
	bootstrapFetch = fn
	t.Cleanup(func() { bootstrapFetch = orig })
}

// captureFetch installs a fake fetch that records the config and returns a
// stub bundle; the returned pointer holds the last config.
func captureFetch(t *testing.T) *trustbootstrap.Config {
	t.Helper()
	captured := &trustbootstrap.Config{}
	withFakeFetch(t, func(_ context.Context, cfg trustbootstrap.Config) (*trustbootstrap.CABundle, error) {
		*captured = cfg
		return &trustbootstrap.CABundle{PEM: []byte(testBootstrapPEM)}, nil
	})
	return captured
}

// refuseFetch installs a fake fetch that fails the test if it is called.
func refuseFetch(t *testing.T) {
	t.Helper()
	withFakeFetch(t, func(context.Context, trustbootstrap.Config) (*trustbootstrap.CABundle, error) {
		t.Fatal("fetch must not be called")
		return nil, nil
	})
}

const testBootstrapPEM = "-----BEGIN CERTIFICATE-----\nfetched\n-----END CERTIFICATE-----\n"

// runBootstrapVerb runs 'bootstrap <args...>' through the real command tree
// and the production client factory.
func runBootstrapVerb(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Cleanup(resetBootstrapFlags)
	cmd := newBootstrapCommand()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

// TestBootstrapAuto_SDKClient_BuildsNoiseConfig: the noise verb maps the
// approved-suite flags onto the go-xkms Noise config.
func TestBootstrapAuto_SDKClient_BuildsNoiseConfig(t *testing.T) {
	// Obvious fake hex of the right shape: an uncompressed point tag and
	// P256PublicKeySize-1 filler bytes. The fetch seam never decodes it.
	p256Hex := "04" + strings.Repeat("ab", trustbootstrap.P256PublicKeySize-1)

	cfg := captureFetch(t)
	out, err := runBootstrapVerb(t, "noise",
		"--server-url", "https://kms.example.com:8443",
		"--noise-addr", "kms.example.com:8445",
		"--noise-p256-static-key", p256Hex,
		"--noise-suite", "p256_aesgcm")
	require.NoError(t, err)

	assert.Equal(t, trustbootstrap.ModeNoise, cfg.Mode)
	assert.Equal(t, "https://kms.example.com:8443", cfg.ServerURL)
	require.NotNil(t, cfg.Noise)
	assert.Equal(t, p256Hex, cfg.Noise.ServerP256StaticKey)
	assert.Empty(t, cfg.Noise.ServerStaticKey)
	assert.Equal(t, "kms.example.com:8445", cfg.Noise.ServerAddr)
	assert.Equal(t, trustbootstrap.SuiteP256AESGCM, cfg.Noise.Suite)
	assert.Nil(t, cfg.Direct, "a pinned noise run must not offer direct")
	assert.Contains(t, out, "BOOTSTRAP RESULT")
	assert.Contains(t, out, "fetched")
}

// TestBootstrapAuto_SDKClient_BuildsDANEAndSPKIConfig: the dane and spki
// verbs map their flags.
func TestBootstrapAuto_SDKClient_BuildsDANEAndSPKIConfig(t *testing.T) {
	t.Run("dane", func(t *testing.T) {
		cfg := captureFetch(t)
		_, err := runBootstrapVerb(t, "dane",
			"--server-url", "https://kms.example.com:8443",
			"--hostname", "kms.example.com",
			"--dns-server", "8.8.8.8:53")
		require.NoError(t, err)
		assert.Equal(t, trustbootstrap.ModeDANE, cfg.Mode)
		require.NotNil(t, cfg.DANE)
		assert.Equal(t, "kms.example.com", cfg.DANE.Hostname)
		assert.Equal(t, "8.8.8.8:53", cfg.DANE.DNSServer)
		assert.Nil(t, cfg.SPKI)
		assert.Nil(t, cfg.Noise)
	})
	t.Run("spki", func(t *testing.T) {
		pin := strings.Repeat("0f", 32)
		cfg := captureFetch(t)
		out, err := runBootstrapVerb(t, "spki",
			"--server-url", "https://kms.example.com:8443",
			"--spki-pin", pin)
		require.NoError(t, err)
		assert.Equal(t, trustbootstrap.ModeSPKI, cfg.Mode)
		require.NotNil(t, cfg.SPKI)
		assert.Equal(t, pin, cfg.SPKI.Pin)
		assert.Nil(t, cfg.DANE)
		assert.Contains(t, out, pin, "the verified pin is reported")
	})
}

// TestBootstrapAuto_SDKClient_DirectHasDirectConfig: the direct verb, and
// auto, offer the direct fetch.
func TestBootstrapAuto_SDKClient_DirectHasDirectConfig(t *testing.T) {
	cfg := captureFetch(t)
	_, err := runBootstrapVerb(t, "direct", "--server-url", "https://kms.example.com:8443")
	require.NoError(t, err)
	assert.Equal(t, trustbootstrap.ModeDirect, cfg.Mode)
	assert.NotNil(t, cfg.Direct)

	cfg = captureFetch(t)
	_, err = runBootstrapVerb(t, "auto", "--server-url", "https://kms.example.com:8443")
	require.NoError(t, err)
	assert.Equal(t, trustbootstrap.ModeAuto, cfg.Mode)
	assert.NotNil(t, cfg.Direct)
}

// TestBootstrapAuto_RequiresServerURL: no --server-url is refused before
// any fetch.
func TestBootstrapAuto_RequiresServerURL(t *testing.T) {
	refuseFetch(t)
	for _, verb := range []string{"auto", "dane", "noise", "spki", "direct"} {
		t.Run(verb, func(t *testing.T) {
			_, err := runBootstrapVerb(t, verb)
			var typed *BootstrapServerURLRequiredError
			require.ErrorAs(t, err, &typed)
		})
	}
}

// TestBootstrapAuto_RejectsUnknownNoiseSuite: an unknown --noise-suite is
// refused before any fetch.
func TestBootstrapAuto_RejectsUnknownNoiseSuite(t *testing.T) {
	refuseFetch(t)
	_, err := runBootstrapVerb(t, "noise",
		"--server-url", "https://kms.example.com:8443",
		"--noise-static-key", "deadbeef",
		"--noise-suite", "noise_ik")
	var typed *BootstrapInvalidNoiseSuiteError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "noise_ik", typed.Got)
}

// TestBootstrapAuto_FetchErrorSurfaces: a fetch error reaches the command
// error with its sentinel intact.
func TestBootstrapAuto_FetchErrorSurfaces(t *testing.T) {
	withFakeFetch(t, func(context.Context, trustbootstrap.Config) (*trustbootstrap.CABundle, error) {
		return nil, &trustbootstrap.FetchError{Mode: trustbootstrap.ModeNoise, Cause: trustbootstrap.ErrNoSuite}
	})
	_, err := runBootstrapVerb(t, "noise",
		"--server-url", "https://kms.example.com:8443",
		"--noise-static-key", "deadbeef")
	require.Error(t, err)
	assert.ErrorIs(t, err, trustbootstrap.ErrNoSuite)
	assert.ErrorIs(t, err, trustbootstrap.ErrFetch)
}

// TestBootstrapAuto_SDKClient_EmptyBundle: a nil bundle with a nil error is
// reported, never rendered as an empty success.
func TestBootstrapAuto_SDKClient_EmptyBundle(t *testing.T) {
	withFakeFetch(t, func(context.Context, trustbootstrap.Config) (*trustbootstrap.CABundle, error) {
		return nil, nil
	})
	_, err := runBootstrapVerb(t, "direct", "--server-url", "https://kms.example.com:8443")
	var typed *BootstrapEmptyBundleError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "direct", typed.Method)
}

// =============================================================================
// Errors
// =============================================================================

// TestBootstrapInvalidMethodError_Message verifies the error string.
func TestBootstrapInvalidMethodError_Message(t *testing.T) {
	err := &BootstrapInvalidMethodError{Got: "telepathy"}
	assert.Contains(t, err.Error(), "telepathy")
	assert.Contains(t, err.Error(), "auto, dane, noise, spki, direct")
}

// TestBootstrapServerURLRequiredError_Message verifies the error string.
func TestBootstrapServerURLRequiredError_Message(t *testing.T) {
	err := &BootstrapServerURLRequiredError{}
	assert.Contains(t, err.Error(), "--server-url")
	assert.Contains(t, err.Error(), "https")
}

// TestBootstrapInvalidNoiseSuiteError_Message verifies the error string.
func TestBootstrapInvalidNoiseSuiteError_Message(t *testing.T) {
	err := &BootstrapInvalidNoiseSuiteError{Got: "noise_ik"}
	assert.Contains(t, err.Error(), "noise_ik")
	assert.Contains(t, err.Error(), "noise_nk")
	assert.Contains(t, err.Error(), "p256_aesgcm")
}

// TestBootstrapEmptyBundleError_Message verifies the error string.
func TestBootstrapEmptyBundleError_Message(t *testing.T) {
	err := &BootstrapEmptyBundleError{Method: "spki"}
	assert.Contains(t, err.Error(), "spki")
	assert.Contains(t, err.Error(), "no CA bundle")
}
