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

//go:build pkcs11

package server

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// fullPKCS11Config returns a server config whose Seal section selects the
// pkcs11 strategy and fills every required field. The library path points at a
// real (empty) file under t.TempDir() so pkcs11backend.Config.Validate — which
// stats the library path — passes without touching the host's PKCS#11 stack.
// No token I/O happens until Login(), so NewBackend succeeds here.
func fullPKCS11Config(t *testing.T) *config.Config {
	t.Helper()

	lib := filepath.Join(t.TempDir(), "libsofthsm2.so")
	if err := os.WriteFile(lib, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub library: %v", err)
	}

	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(xkmssdk.StrategyPKCS11)
	cfg.Seal.PKCS11 = config.PKCS11SealConfig{
		Library:    lib,
		TokenLabel: "quicbaas-test-token",
		PIN:        "1234",
		KeyLabel:   "quicbaas-seal-key",
	}
	return cfg
}

// TestBuildPKCS11Strategy_MissingFields asserts that each required PKCS#11
// config field is validated before any backend construction is attempted, and
// that the failure surfaces as a *StrategyBuildError wrapping a
// *PKCS11ConfigError naming the missing field.
func TestBuildPKCS11Strategy_MissingFields(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(c *config.PKCS11SealConfig)
		wantField string
	}{
		{
			name:      "missing library",
			mutate:    func(c *config.PKCS11SealConfig) { c.Library = "" },
			wantField: "library",
		},
		{
			name:      "missing token label",
			mutate:    func(c *config.PKCS11SealConfig) { c.TokenLabel = "" },
			wantField: "token_label",
		},
		{
			name:      "missing pin",
			mutate:    func(c *config.PKCS11SealConfig) { c.PIN = "" },
			wantField: "pin",
		},
		{
			name:      "missing key label",
			mutate:    func(c *config.PKCS11SealConfig) { c.KeyLabel = "" },
			wantField: "key_label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fullPKCS11Config(t)
			tt.mutate(&cfg.Seal.PKCS11)

			strategy, err := buildPKCS11Strategy(cfg)
			if err == nil {
				t.Fatalf("buildPKCS11Strategy() returned nil error for %s; strategy=%v", tt.name, strategy)
			}
			if strategy != nil {
				t.Fatalf("buildPKCS11Strategy() returned non-nil strategy alongside error")
			}

			var buildErr *StrategyBuildError
			if !errors.As(err, &buildErr) {
				t.Fatalf("error is not *StrategyBuildError: %T: %v", err, err)
			}
			if buildErr.Backend != "pkcs11" {
				t.Fatalf("StrategyBuildError.Backend = %q, want pkcs11", buildErr.Backend)
			}

			var cfgErr *PKCS11ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("error does not wrap *PKCS11ConfigError: %v", err)
			}
			if cfgErr.Field != tt.wantField {
				t.Fatalf("PKCS11ConfigError.Field = %q, want %q", cfgErr.Field, tt.wantField)
			}
		})
	}
}

// TestBuildPKCS11Strategy_SlotSelector verifies that supplying an explicit Slot
// alongside the required TokenLabel passes config validation: Slot is an
// optional additional token selector threaded into the go-xkms backend config,
// not a substitute for the label (which the backend requires). Validation runs
// before any token I/O, so the factory advances past it to the registration
// step; against the stub library (no real token) that step fails while logging
// in, surfacing a *StrategyBuildError wrapping a *PKCS11KeyRegistrationError.
// A clean build over a real token is exercised by the integration suite.
func TestBuildPKCS11Strategy_SlotSelector(t *testing.T) {
	cfg := fullPKCS11Config(t)
	slot := 0
	cfg.Seal.PKCS11.Slot = &slot

	_, err := buildPKCS11Strategy(cfg)
	if err == nil {
		t.Fatal("expected registration to fail without a real token, got nil")
	}

	var cfgErr *PKCS11ConfigError
	if errors.As(err, &cfgErr) {
		t.Fatalf("slot selector must pass config validation, got config error: %v", err)
	}

	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("error is not *StrategyBuildError: %T: %v", err, err)
	}
	if buildErr.Backend != "pkcs11" {
		t.Fatalf("StrategyBuildError.Backend = %q, want pkcs11", buildErr.Backend)
	}

	var regErr *PKCS11KeyRegistrationError
	if !errors.As(err, &regErr) {
		t.Fatalf("error does not wrap *PKCS11KeyRegistrationError: %v", err)
	}
	if regErr.KeyLabel != cfg.Seal.PKCS11.KeyLabel {
		t.Fatalf("PKCS11KeyRegistrationError.KeyLabel = %q, want %q", regErr.KeyLabel, cfg.Seal.PKCS11.KeyLabel)
	}
}

// TestBuildPKCS11Strategy_FactoryDispatch drives the registered factory through
// buildHardwareSealStrategies for a fully populated config. It proves the
// dispatch registry routes the pkcs11 strategy ID to buildPKCS11Strategy and
// the factory reaches the registration step. Because the factory now logs in to
// the token to register the sealing key, and no real token backs the stub
// library, dispatch surfaces the registration failure as a *StrategyBuildError
// for the pkcs11 backend. The successful single-strategy build over a real token
// is exercised by the integration suite.
func TestBuildPKCS11Strategy_FactoryDispatch(t *testing.T) {
	cfg := fullPKCS11Config(t)

	_, err := buildHardwareSealStrategies(cfg)
	if err == nil {
		t.Fatal("expected dispatch to fail registration without a real token, got nil")
	}

	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("error is not *StrategyBuildError: %T: %v", err, err)
	}
	if buildErr.Backend != "pkcs11" {
		t.Fatalf("StrategyBuildError.Backend = %q, want pkcs11", buildErr.Backend)
	}

	var regErr *PKCS11KeyRegistrationError
	if !errors.As(err, &regErr) {
		t.Fatalf("dispatch error does not wrap *PKCS11KeyRegistrationError: %v", err)
	}
}

// TestPKCS11SealKeyAttributes verifies the KeyAttributes the factory hands to
// the strategy describe the HSM-resident AES-256-GCM wrapping key: the
// configured KeyLabel as CN (used by the backend for CKA_LABEL / CKA_ID
// resolution), the PKCS#11 store, and a symmetric algorithm — the latter is
// what makes pkcs11backend.Backend.Seal treat the request as a symmetric seal.
func TestPKCS11SealKeyAttributes(t *testing.T) {
	const label = "quicbaas-seal-key"
	attrs := pkcs11SealKeyAttributes(label)

	if attrs == nil {
		t.Fatal("pkcs11SealKeyAttributes returned nil")
	}
	if attrs.CN != label {
		t.Fatalf("CN = %q, want %q", attrs.CN, label)
	}
	if attrs.StoreType != xkmstypes.StorePKCS11 {
		t.Fatalf("StoreType = %q, want %q", attrs.StoreType, xkmstypes.StorePKCS11)
	}
	if attrs.KeyType != xkmstypes.KeyTypeEncryption {
		t.Fatalf("KeyType = %v, want KeyTypeEncryption", attrs.KeyType)
	}
	if attrs.SymmetricAlgorithm != xkmstypes.SymmetricAES256GCM {
		t.Fatalf("SymmetricAlgorithm = %q, want %q", attrs.SymmetricAlgorithm, xkmstypes.SymmetricAES256GCM)
	}
	if !attrs.IsSymmetric() {
		t.Fatal("KeyAttributes must report IsSymmetric() == true so the backend takes the symmetric seal path")
	}
}

// aesGCMSealer is a faithful, in-test xkmstypes.Sealer that performs the exact
// AES-256-GCM seal/unseal the PKCS#11 HSM backend performs, but with the AES
// key held in process instead of in an HSM. It exists because go-xkms exposes
// no in-memory PKCS#11 backend (unlike the TPM2 simulator) and no mechanism to
// inject a mock PKCS#11 context into the symmetric seal path: that path runs
// entirely through a real *pkcs11.Ctx + session pool reached only via Login()
// against a real token. The MockPKCS11Context in go-xkms covers only
// asymmetric signers, not C_GenerateKey/C_Encrypt/C_Decrypt for AES.
//
// aesGCMSealer therefore validates exactly the contract the production factory
// depends on from the strategy wrapper: that xkmssdk.NewPKCS11Strategy passes
// the configured KeyAttributes through on BOTH Seal and Unseal (the backend
// rejects either without them), and that a 32-byte root key survives a full
// seal -> unseal round trip. The HSM crypto itself is owned and tested by
// go-xkms; this guards the quicbaas compose contract.
type aesGCMSealer struct {
	key       []byte
	wantLabel string
	t         *testing.T
}

func newAESGCMSealer(t *testing.T, wantLabel string) *aesGCMSealer {
	t.Helper()
	key := make([]byte, 32) // AES-256
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate AES key: %v", err)
	}
	return &aesGCMSealer{key: key, wantLabel: wantLabel, t: t}
}

func (s *aesGCMSealer) CanSeal() bool { return true }

func (s *aesGCMSealer) gcm() cipher.AEAD {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		s.t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		s.t.Fatalf("cipher.NewGCM: %v", err)
	}
	return gcm
}

// Seal mirrors pkcs11backend.Backend.Seal: it requires opts.KeyAttributes
// identifying the symmetric wrapping key, then AES-256-GCM-encrypts the data.
func (s *aesGCMSealer) Seal(ctx context.Context, data []byte, opts *xkmstypes.SealOptions) (*xkmstypes.SealedData, error) {
	if opts == nil || opts.KeyAttributes == nil {
		return nil, errors.New("seal options with KeyAttributes required")
	}
	if opts.KeyAttributes.CN != s.wantLabel {
		return nil, errors.New("key label mismatch on seal")
	}
	if !opts.KeyAttributes.IsSymmetric() {
		return nil, errors.New("seal requires a symmetric key")
	}

	gcm := s.gcm()
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, data, opts.AAD)

	return &xkmstypes.SealedData{
		Backend:    xkmstypes.BackendTypePKCS11,
		Ciphertext: ct,
		Nonce:      nonce,
		KeyID:      opts.KeyAttributes.ID(),
	}, nil
}

// Unseal mirrors pkcs11backend.Backend.Unseal: it requires opts.KeyAttributes
// identifying the same wrapping key, then AES-256-GCM-decrypts.
func (s *aesGCMSealer) Unseal(ctx context.Context, sealed *xkmstypes.SealedData, opts *xkmstypes.UnsealOptions) ([]byte, error) {
	if sealed == nil {
		return nil, errors.New("sealed data required")
	}
	if opts == nil || opts.KeyAttributes == nil {
		return nil, errors.New("unseal options with KeyAttributes required")
	}
	if opts.KeyAttributes.CN != s.wantLabel {
		return nil, errors.New("key label mismatch on unseal")
	}

	gcm := s.gcm()
	return gcm.Open(nil, sealed.Nonce, sealed.Ciphertext, opts.AAD)
}

// TestPKCS11Strategy_SealUnsealRoundTrip performs a full root-key seal ->
// unseal round trip through xkmssdk.NewPKCS11Strategy — the exact wrapper the
// production factory uses — over a faithful in-test AES-256-GCM Sealer keyed by
// the same KeyAttributes the factory builds. It proves the compose contract the
// factory relies on: the strategy threads the configured KeyAttributes through
// to both Seal and Unseal, and a 32-byte root key is recovered byte-for-byte.
//
// See aesGCMSealer for why a real / mock-context pkcs11backend.Backend cannot
// drive this round trip in a unit test today.
func TestPKCS11Strategy_SealUnsealRoundTrip(t *testing.T) {
	const label = "quicbaas-seal-key"
	keyAttrs := pkcs11SealKeyAttributes(label)
	sealer := newAESGCMSealer(t, label)

	strategy := xkmssdk.NewPKCS11Strategy(sealer, keyAttrs, nil)

	if strategy.ID() != xkmssdk.StrategyPKCS11 {
		t.Fatalf("strategy.ID() = %q, want %q", strategy.ID(), xkmssdk.StrategyPKCS11)
	}
	if !strategy.HardwareBacked() {
		t.Fatal("PKCS11 strategy must report HardwareBacked() == true")
	}
	if !strategy.Available() {
		t.Fatal("PKCS11 strategy reports Available() == false over a seal-capable sealer")
	}

	ctx := context.Background()
	rootKey := make([]byte, 32)
	if _, err := rand.Read(rootKey); err != nil {
		t.Fatalf("generate root key: %v", err)
	}

	sealed, err := strategy.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey: %v", err)
	}
	if sealed == nil || len(sealed.HardwarePayload) == 0 {
		t.Fatal("SealRootKey produced empty HardwarePayload")
	}
	if sealed.Strategy != xkmssdk.StrategyPKCS11 {
		t.Fatalf("sealed.Strategy = %q, want %q", sealed.Strategy, xkmssdk.StrategyPKCS11)
	}

	recovered, err := strategy.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey: %v", err)
	}
	if !bytes.Equal(recovered, rootKey) {
		t.Fatalf("recovered root key mismatch: got %x want %x", recovered, rootKey)
	}
}

// TestPKCS11Strategy_RoundTripRequiresKeyAttributes asserts the negative half
// of the compose contract: a strategy built without KeyAttributes (nil) cannot
// seal or unseal, because the PKCS#11 backend requires the sealing-key identity
// on every operation. xkmssdk.PKCS11Strategy returns ErrNoSealingKey rather
// than delegating to a backend that would reject the request.
func TestPKCS11Strategy_RoundTripRequiresKeyAttributes(t *testing.T) {
	sealer := newAESGCMSealer(t, "unused")
	strategy := xkmssdk.NewPKCS11Strategy(sealer, nil, nil)

	ctx := context.Background()
	if _, err := strategy.SealRootKey(ctx, make([]byte, 32), qrdbsdk.Credentials{}); err == nil {
		t.Fatal("SealRootKey without KeyAttributes should fail")
	}
}

// TestPKCS11BackendConfig_SealsPIN verifies the operator PIN reaches the
// go-xkms backend config as a SecretValue whose enclave holds exactly the
// configured PIN, alongside the rest of the operator's token selection.
func TestPKCS11BackendConfig_SealsPIN(t *testing.T) {
	p11 := fullPKCS11Config(t).Seal.PKCS11
	keyStorage := xkmssdk.NewStorageMemory()

	got, err := pkcs11BackendConfig(p11, keyStorage)
	if err != nil {
		t.Fatalf("pkcs11BackendConfig: %v", err)
	}
	t.Cleanup(got.PIN.Clear)

	pin, err := xkmssdk.SecretValueBytes(got.PIN)
	if err != nil {
		t.Fatalf("SecretValueBytes(PIN): %v", err)
	}
	if string(pin) != p11.PIN {
		t.Fatalf("PIN enclave = %q, want %q", pin, p11.PIN)
	}
	if got.CN != p11.KeyLabel || got.Library != p11.Library || got.TokenLabel != p11.TokenLabel {
		t.Fatalf("token selection not threaded: %+v", got)
	}
	if got.KeyStorage != keyStorage {
		t.Fatal("KeyStorage is not the caller's key store")
	}
	if got.CertStorage == nil {
		t.Fatal("CertStorage is nil")
	}
}

// TestPKCS11BackendConfig_EmptyPIN verifies an unset PIN seals to an empty
// SecretValue, which the backend reads as "no PIN configured" rather than as
// an unresolved reference.
func TestPKCS11BackendConfig_EmptyPIN(t *testing.T) {
	p11 := fullPKCS11Config(t).Seal.PKCS11
	p11.PIN = ""

	got, err := pkcs11BackendConfig(p11, xkmssdk.NewStorageMemory())
	if err != nil {
		t.Fatalf("pkcs11BackendConfig: %v", err)
	}

	pin, err := xkmssdk.SecretValueBytes(got.PIN)
	if err != nil {
		t.Fatalf("SecretValueBytes(empty PIN): %v", err)
	}
	if pin != nil {
		t.Fatalf("empty PIN sealed to %q, want no bytes", pin)
	}
}

// TestReleasePKCS11Backend_DestroysPIN verifies that abandoning a constructed
// backend closes it and destroys the PIN enclave it held, so a failed build
// leaves no PIN in memory.
func TestReleasePKCS11Backend_DestroysPIN(t *testing.T) {
	backendCfg, err := pkcs11BackendConfig(fullPKCS11Config(t).Seal.PKCS11, xkmssdk.NewStorageMemory())
	if err != nil {
		t.Fatalf("pkcs11BackendConfig: %v", err)
	}
	backend, err := xkmssdk.NewPKCS11Backend(backendCfg)
	if err != nil {
		t.Fatalf("NewPKCS11Backend: %v", err)
	}

	if err := releasePKCS11Backend(backend); err != nil {
		t.Fatalf("releasePKCS11Backend: %v", err)
	}

	var sawBytes bool
	if err := backendCfg.PIN.WithBytes(func(b []byte) error {
		sawBytes = len(b) > 0
		return nil
	}); err != nil {
		t.Fatalf("WithBytes after release: %v", err)
	}
	if sawBytes {
		t.Fatal("PIN enclave still holds plaintext after release")
	}
}

// TestBuildPKCS11Strategy_UnloadableLibrary verifies a backend construction
// failure (the library path does not exist) surfaces as a *StrategyBuildError
// for the pkcs11 backend rather than as a config or registration error.
func TestBuildPKCS11Strategy_UnloadableLibrary(t *testing.T) {
	cfg := fullPKCS11Config(t)
	cfg.Seal.PKCS11.Library = filepath.Join(t.TempDir(), "absent.so")

	strategy, err := buildPKCS11Strategy(cfg)
	if err == nil {
		t.Fatalf("buildPKCS11Strategy() = %v, want error for a missing library", strategy)
	}
	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) || buildErr.Backend != "pkcs11" {
		t.Fatalf("error = %T %v, want *StrategyBuildError for pkcs11", err, err)
	}
	var regErr *PKCS11KeyRegistrationError
	if errors.As(err, &regErr) {
		t.Fatalf("construction failure reported as registration failure: %v", err)
	}
}
