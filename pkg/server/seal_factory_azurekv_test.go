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

//go:build azurekv

package server

import (
	"errors"
	"testing"

	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// Azure Key Vault note: a full seal -> unseal round-trip cannot be exercised in
// a unit test. The go-xkms azurekv backend stores symmetric key material as a
// Key Vault *secret* and retrieves it through a real azsecrets client built
// from an Azure credential at Seal time; that path is not served by the
// backend's injectable mock KeyVaultClient (which only mocks asymmetric key
// operations). The go-xkms backend's own symmetric tests skip for exactly this
// reason ("Symmetric encryption requires Azure Secrets API (not mocked)").
//
// These tests therefore cover the factory's config validation, the seal-key
// attribute construction, and the registration path's failure handling when
// Azure is unreachable. Because the factory now registers the wrapping key at
// build time (so the seal path can resolve it), a successful build and the
// end-to-end crypto round-trip both require a real Key Vault and are covered by
// an integration test, out of scope for the unit suite.

// azureKVConfig returns a *config.Config selecting the azurekv strategy with
// the supplied Azure sub-config, for exercising the factory.
func azureKVConfig(sub config.AzureSealConfig) *config.Config {
	cfg := &config.Config{}
	cfg.Seal.Strategy = string(xkmssdk.StrategyAzureKV)
	cfg.Seal.AzureKV = sub
	return cfg
}

// TestBuildAzureKVStrategy_MissingVaultURL asserts the factory rejects a config
// with no vault URL before attempting any backend construction.
func TestBuildAzureKVStrategy_MissingVaultURL(t *testing.T) {
	_, err := buildAzureKVStrategy(azureKVConfig(config.AzureSealConfig{KeyName: "root"}))
	if err == nil {
		t.Fatal("expected error for missing vault_url, got nil")
	}
	var cfgErr *AzureKVConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected wrapped *AzureKVConfigError, got %T: %v", err, err)
	}
	if cfgErr.Field != "vault_url" {
		t.Errorf("expected missing field vault_url, got %q", cfgErr.Field)
	}
}

// TestBuildAzureKVStrategy_MissingKeyName asserts the factory rejects a config
// with a vault URL but no key name.
func TestBuildAzureKVStrategy_MissingKeyName(t *testing.T) {
	_, err := buildAzureKVStrategy(azureKVConfig(config.AzureSealConfig{
		VaultURL: "https://my-vault.vault.azure.net/",
	}))
	if err == nil {
		t.Fatal("expected error for missing key_name, got nil")
	}
	var cfgErr *AzureKVConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected wrapped *AzureKVConfigError, got %T: %v", err, err)
	}
	if cfgErr.Field != "key_name" {
		t.Errorf("expected missing field key_name, got %q", cfgErr.Field)
	}
}

// TestBuildAzureKVStrategy_InvalidVaultURL asserts an unreachable / malformed
// vault URL is surfaced as a *StrategyBuildError for the azurekv backend. The
// failure now originates from the key-registration step (which reaches out to
// Azure) rather than backend construction, but the operator-facing error type
// and backend tag are unchanged.
func TestBuildAzureKVStrategy_InvalidVaultURL(t *testing.T) {
	_, err := buildAzureKVStrategy(azureKVConfig(config.AzureSealConfig{
		VaultURL: "http://not-a-vault.example.com/",
		KeyName:  "root",
	}))
	if err == nil {
		t.Fatal("expected error for invalid vault URL, got nil")
	}
	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected *StrategyBuildError, got %T: %v", err, err)
	}
	if buildErr.Backend != "azurekv" {
		t.Errorf("expected backend azurekv, got %q", buildErr.Backend)
	}
}

// TestBuildAzureKVStrategy_RegistrationRequiresAzure documents and pins the new
// behaviour: the factory registers the wrapping key at build time so the seal
// path can resolve it, which requires a reachable Key Vault. Against a vault URL
// that resolves to no real Azure endpoint, the factory therefore fails at the
// registration step, surfacing a *StrategyBuildError that wraps an
// *AzureKVKeyRegistrationError naming the configured key. A clean build and the
// crypto round-trip are validated by the integration suite against real Azure.
func TestBuildAzureKVStrategy_RegistrationRequiresAzure(t *testing.T) {
	const keyName = "barrier-root"

	_, err := buildAzureKVStrategy(azureKVConfig(config.AzureSealConfig{
		VaultURL: "https://nonexistent-vault.vault.azure.net/",
		KeyName:  keyName,
	}))
	if err == nil {
		t.Fatal("expected registration to fail without a reachable Key Vault, got nil")
	}

	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected *StrategyBuildError, got %T: %v", err, err)
	}
	if buildErr.Backend != "azurekv" {
		t.Errorf("expected backend azurekv, got %q", buildErr.Backend)
	}

	var regErr *AzureKVKeyRegistrationError
	if !errors.As(err, &regErr) {
		t.Fatalf("expected wrapped *AzureKVKeyRegistrationError, got %v", err)
	}
	if regErr.KeyName != keyName {
		t.Errorf("expected registration error key %q, got %q", keyName, regErr.KeyName)
	}
}

// TestAzureKVSealKeyAttributes verifies the seal-key attributes are a valid,
// symmetric, Azure-KV-resident AES-256-GCM key keyed on the configured KeyName.
func TestAzureKVSealKeyAttributes(t *testing.T) {
	attrs := azureKVSealKeyAttributes("barrier-root")

	if err := attrs.Validate(); err != nil {
		t.Fatalf("seal key attributes failed validation: %v", err)
	}
	if !attrs.IsSymmetric() {
		t.Error("seal key attributes must be symmetric")
	}
	if attrs.CN != "barrier-root" {
		t.Errorf("expected CN barrier-root, got %q", attrs.CN)
	}
	if attrs.StoreType != xkmstypes.StoreAzureKV {
		t.Errorf("expected store %s, got %s", xkmstypes.StoreAzureKV, attrs.StoreType)
	}
	if attrs.SymmetricAlgorithm != xkmstypes.SymmetricAES256GCM {
		t.Errorf("expected AES-256-GCM, got %s", attrs.SymmetricAlgorithm)
	}
}
