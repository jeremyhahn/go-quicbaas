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

//go:build vault

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	vaultbackend "github.com/jeremyhahn/go-xkms/pkg/backend/vault"
	"github.com/jeremyhahn/go-xkms/pkg/storage"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// vaultConfig returns a *config.Config selecting the vault strategy with the
// supplied Vault sub-config, for exercising the factory's validation path.
func vaultConfig(sub config.VaultSealConfig) *config.Config {
	cfg := &config.Config{}
	cfg.Seal.Strategy = string(xkmssdk.StrategyVault)
	cfg.Seal.Vault = sub
	return cfg
}

// TestBuildVaultStrategy_MissingAddress asserts the factory rejects a config
// with no Vault address before attempting any backend construction.
func TestBuildVaultStrategy_MissingAddress(t *testing.T) {
	_, err := buildVaultStrategy(vaultConfig(config.VaultSealConfig{KeyName: "root", Token: "t"}))
	if err == nil {
		t.Fatal("expected error for missing address, got nil")
	}
	var cfgErr *VaultConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected wrapped *VaultConfigError, got %T: %v", err, err)
	}
	if cfgErr.Field != "address" {
		t.Errorf("expected missing field address, got %q", cfgErr.Field)
	}
}

// TestBuildVaultStrategy_MissingKeyName asserts the factory rejects a config
// with an address but no Transit key name.
func TestBuildVaultStrategy_MissingKeyName(t *testing.T) {
	_, err := buildVaultStrategy(vaultConfig(config.VaultSealConfig{
		Address: "https://vault:8200",
		Token:   "t",
	}))
	if err == nil {
		t.Fatal("expected error for missing key_name, got nil")
	}
	var cfgErr *VaultConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected wrapped *VaultConfigError, got %T: %v", err, err)
	}
	if cfgErr.Field != "key_name" {
		t.Errorf("expected missing field key_name, got %q", cfgErr.Field)
	}
}

// TestBuildVaultStrategy_MissingToken asserts an empty token is surfaced by the
// go-xkms backend's own config validation as a *StrategyBuildError.
func TestBuildVaultStrategy_MissingToken(t *testing.T) {
	_, err := buildVaultStrategy(vaultConfig(config.VaultSealConfig{
		Address: "https://vault:8200",
		KeyName: "root",
	}))
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected *StrategyBuildError, got %T: %v", err, err)
	}
	if buildErr.Backend != "vault" {
		t.Errorf("expected backend vault, got %q", buildErr.Backend)
	}
}

// TestVaultSealKeyAttributes verifies the seal-key attributes are a valid,
// symmetric, Vault-resident AES-256-GCM key keyed on the configured KeyName.
func TestVaultSealKeyAttributes(t *testing.T) {
	attrs := vaultSealKeyAttributes("barrier-root")

	if err := attrs.Validate(); err != nil {
		t.Fatalf("seal key attributes failed validation: %v", err)
	}
	if !attrs.IsSymmetric() {
		t.Error("seal key attributes must be symmetric")
	}
	if attrs.CN != "barrier-root" {
		t.Errorf("expected CN barrier-root, got %q", attrs.CN)
	}
	if attrs.StoreType != xkmstypes.StoreVault {
		t.Errorf("expected store %s, got %s", xkmstypes.StoreVault, attrs.StoreType)
	}
	if attrs.SymmetricAlgorithm != xkmstypes.SymmetricAES256GCM {
		t.Errorf("expected AES-256-GCM, got %s", attrs.SymmetricAlgorithm)
	}
}

// transitEmulator returns an httptest.Server that mimics the subset of the
// HashiCorp Vault Transit HTTP API exercised by a seal -> unseal round-trip:
// key creation, encrypt, and decrypt. The "encryption" is a reversible
// base64 wrapping (vault:v1:<base64(plaintext)>) which is sufficient to prove
// the go-xkms backend's request/response wiring end to end without a real
// Vault server, and round-trips the exact plaintext.
func transitEmulator(t *testing.T) *httptest.Server {
	t.Helper()
	const prefix = "vault:v1:"

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/transit/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/transit/")
		switch {
		case strings.HasPrefix(path, "keys/"):
			// Key create (POST) or read (GET): a 200 with an empty data object
			// is enough for GenerateSymmetricKey to succeed.
			writeJSON(t, w, map[string]any{"data": map[string]any{}})

		case strings.HasPrefix(path, "encrypt/"):
			var req struct {
				Plaintext string `json:"plaintext"`
			}
			decodeJSONRequest(t, r, &req)
			// req.Plaintext is base64(rootKey); wrap it as Vault ciphertext.
			writeJSON(t, w, map[string]any{
				"data": map[string]any{"ciphertext": prefix + req.Plaintext},
			})

		case strings.HasPrefix(path, "decrypt/"):
			var req struct {
				Ciphertext string `json:"ciphertext"`
			}
			decodeJSONRequest(t, r, &req)
			plaintext := strings.TrimPrefix(req.Ciphertext, prefix)
			writeJSON(t, w, map[string]any{
				"data": map[string]any{"plaintext": plaintext},
			})

		default:
			http.Error(w, "unexpected transit path: "+path, http.StatusNotFound)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
}

func decodeJSONRequest(t *testing.T, r *http.Request, dst any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		t.Fatalf("failed to decode request: %v", err)
	}
}

// TestVaultStrategy_SealUnsealRoundTrip drives the full
// SealRootKey -> UnsealRootKey crypto path through CloudKMSStrategy and a real
// vaultbackend.Backend pointed at an in-process Transit emulator, proving the
// strategy + backend composition seals and recovers a random 32-byte root key
// without a live Vault server.
func TestVaultStrategy_SealUnsealRoundTrip(t *testing.T) {
	const keyName = "barrier-root"

	rootKey := make([]byte, 32)
	if _, err := rand.Read(rootKey); err != nil {
		t.Fatalf("failed to generate root key: %v", err)
	}

	srv := transitEmulator(t)

	backendCfg := &vaultbackend.Config{
		Address:     srv.URL,
		Token:       "test-token",
		TransitPath: "transit",
		KeyStorage:  storage.New(),
		CertStorage: storage.New(),
	}
	backend, err := vaultbackend.NewBackend(backendCfg)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}

	attrs := vaultSealKeyAttributes(keyName)

	// Provision the Transit key + KeyStorage metadata through the backend's own
	// public API, which the sealer's SymmetricEncrypter relies on to resolve the
	// key name. This exercises the real metadata path rather than seeding it.
	if _, err := backend.GenerateSymmetricKey(context.Background(), attrs); err != nil {
		t.Fatalf("GenerateSymmetricKey failed: %v", err)
	}

	strategy := xkmssdk.NewCloudKMSStrategy(xkmssdk.StrategyVault, backend, attrs, nil)
	if !strategy.Available() {
		t.Fatal("strategy reported unavailable with a live-backed sealer")
	}

	ctx := context.Background()

	sealed, err := strategy.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey failed: %v", err)
	}
	if sealed.Strategy != xkmssdk.StrategyVault {
		t.Errorf("expected sealed strategy %s, got %s", xkmssdk.StrategyVault, sealed.Strategy)
	}

	// Sanity check the sealed payload carries a Vault-formatted ciphertext
	// (proving the emulator's encrypt path was actually traversed). The payload
	// is a JSON-encoded SealedData whose Ciphertext field holds the
	// "vault:v1:..." blob.
	var sd struct {
		Ciphertext []byte `json:"ciphertext"`
	}
	if err := json.Unmarshal(sealed.HardwarePayload, &sd); err != nil {
		t.Fatalf("failed to decode hardware payload: %v", err)
	}
	if !bytes.HasPrefix(sd.Ciphertext, []byte("vault:v1:")) {
		t.Errorf("sealed ciphertext is not Vault-formatted: %q", sd.Ciphertext)
	}

	recovered, err := strategy.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey failed: %v", err)
	}
	if !bytes.Equal(recovered, rootKey) {
		t.Error("recovered root key does not match the original")
	}
}

// TestBuildVaultStrategy_SelfRegistersSealKey drives the production factory
// buildVaultStrategy against the in-process Transit emulator and proves the
// factory now self-registers the Transit sealing key: a full
// SealRootKey -> UnsealRootKey round-trip succeeds with no test-side
// pre-registration, which is the behaviour the live daemon depends on. Before
// the registration wiring, the factory built a backend with an empty metadata
// store and the very first SealRootKey failed with ErrKeyNotFound.
func TestBuildVaultStrategy_SelfRegistersSealKey(t *testing.T) {
	const keyName = "barrier-root"

	srv := transitEmulator(t)

	cfg := vaultConfig(config.VaultSealConfig{
		Address:     srv.URL,
		Token:       "test-token",
		TransitPath: "transit",
		KeyName:     keyName,
	})

	strategy, err := buildVaultStrategy(cfg)
	if err != nil {
		t.Fatalf("buildVaultStrategy failed: %v", err)
	}
	if !strategy.Available() {
		t.Fatal("strategy reported unavailable after factory self-registration")
	}

	rootKey := make([]byte, 32)
	if _, err := rand.Read(rootKey); err != nil {
		t.Fatalf("failed to generate root key: %v", err)
	}

	ctx := context.Background()

	sealed, err := strategy.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey failed (factory did not register sealing key): %v", err)
	}

	recovered, err := strategy.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey failed: %v", err)
	}
	if !bytes.Equal(recovered, rootKey) {
		t.Error("recovered root key does not match the original")
	}
}

// TestEnsureVaultSealKey_Idempotent proves the registration helper is safe to
// run on every daemon boot. Two cases are covered:
//
//  1. A fresh-per-boot metadata store (production): a second backend built with
//     its own KeyStorage re-registers against the existing Transit key without
//     error and resolves the sealing key.
//  2. A populated local store: invoking the helper twice against the SAME
//     backend (whose local store now already holds the key) is tolerated — the
//     ErrKeyAlreadyExists path is swallowed, so a re-run never rotates or
//     destroys the key.
func TestEnsureVaultSealKey_Idempotent(t *testing.T) {
	srv := transitEmulator(t)

	attrs := vaultSealKeyAttributes("barrier-root")

	newBackend := func() *vaultbackend.Backend {
		t.Helper()
		b, err := vaultbackend.NewBackend(&vaultbackend.Config{
			Address:     srv.URL,
			Token:       "test-token",
			TransitPath: "transit",
			KeyStorage:  storage.New(),
			CertStorage: storage.New(),
		})
		if err != nil {
			t.Fatalf("NewBackend failed: %v", err)
		}
		return b
	}

	// Case 1: first boot.
	b1 := newBackend()
	if err := ensureVaultSealKey(b1, attrs); err != nil {
		t.Fatalf("first-boot ensureVaultSealKey failed: %v", err)
	}

	// Case 1: second boot with a fresh local store.
	b2 := newBackend()
	if err := ensureVaultSealKey(b2, attrs); err != nil {
		t.Fatalf("second-boot ensureVaultSealKey failed (not idempotent): %v", err)
	}
	if _, err := b2.SymmetricEncrypter(context.Background(), attrs); err != nil {
		t.Fatalf("sealing key not resolvable after re-registration: %v", err)
	}

	// Case 2: re-run against the SAME backend, whose local store already holds
	// the key — the ErrKeyAlreadyExists path must be tolerated.
	if err := ensureVaultSealKey(b2, attrs); err != nil {
		t.Fatalf("re-run against populated store failed (already-exists not tolerated): %v", err)
	}
}
