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
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestBuildHardwareSealStrategies_SoftwarePassthrough verifies that the
// software/none/shamir IDs (and the empty default) are NOT dispatched through
// the hardware registry: buildHardwareSealStrategies must return (nil, nil) so
// the caller falls through to buildSealStrategies.
func TestBuildHardwareSealStrategies_SoftwarePassthrough(t *testing.T) {
	cases := []string{
		"", // empty defaults to software
		string(qrdbsdk.StrategyPassphrase),
		string(qrdbsdk.StrategyNone),
		string(qrdbsdk.StrategyShamir),
	}
	for _, start := range cases {
		t.Run("strategy="+start, func(t *testing.T) {
			cfg := config.TestConfig()
			cfg.Seal.Strategy = start

			strategies, err := buildHardwareSealStrategies(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strategies != nil {
				t.Fatalf("expected nil strategies for non-hardware id %q, got %d", start, len(strategies))
			}
		})
	}
}

// TestBuildHardwareSealStrategies_NilConfig verifies the nil-config guard.
func TestBuildHardwareSealStrategies_NilConfig(t *testing.T) {
	strategies, err := buildHardwareSealStrategies(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategies != nil {
		t.Fatalf("expected nil strategies for nil config, got %v", strategies)
	}
}

// TestBuildHardwareSealStrategies_NotCompiled verifies that selecting a
// hardware/cloud strategy whose build tag was not compiled in yields a
// StrategyNotCompiledError naming the strategy. awskms/gcpkms/azurekv/vault/
// pkcs11 are build-tagged and absent from the default test build, so they all
// exercise this path.
func TestBuildHardwareSealStrategies_NotCompiled(t *testing.T) {
	buildTagged := []qrdbsdk.StrategyID{
		xkmssdk.StrategyAWSKMS,
		xkmssdk.StrategyGCPKMS,
		xkmssdk.StrategyAzureKV,
		xkmssdk.StrategyVault,
		xkmssdk.StrategyPKCS11,
	}
	for _, id := range buildTagged {
		if _, registered := hardwareStrategyFactories[id]; registered {
			// A follow-up build may have compiled this backend in; skip the
			// not-compiled assertion for it since the premise no longer holds.
			continue
		}
		t.Run("strategy="+string(id), func(t *testing.T) {
			cfg := config.TestConfig()
			cfg.Seal.Strategy = string(id)

			_, err := buildHardwareSealStrategies(cfg)
			if err == nil {
				t.Fatalf("expected StrategyNotCompiledError for %q, got nil", id)
			}
			var notCompiled *StrategyNotCompiledError
			if !errors.As(err, &notCompiled) {
				t.Fatalf("expected *StrategyNotCompiledError, got %T: %v", err, err)
			}
			if notCompiled.Strategy != id {
				t.Fatalf("StrategyNotCompiledError.Strategy = %q, want %q", notCompiled.Strategy, id)
			}
		})
	}
}

// TestTPM2FactoryRegistered verifies the build-tag-free TPM2 factory is wired
// into the registry at init time and is dispatched as a hardware strategy.
func TestTPM2FactoryRegistered(t *testing.T) {
	if _, ok := hardwareStrategyFactories[xkmssdk.StrategyTPM2]; !ok {
		t.Fatal("TPM2 factory not registered in hardwareStrategyFactories")
	}
	if !isHardwareStrategy(xkmssdk.StrategyTPM2) {
		t.Fatal("TPM2 is not classified as a hardware strategy")
	}
}

// TestStrategyBuildError_Format verifies the StrategyBuildError message and
// Unwrap behavior.
func TestStrategyBuildError_Format(t *testing.T) {
	cause := errors.New("boom")
	err := &StrategyBuildError{Backend: "tpm2", Cause: cause}
	if !errors.Is(err, cause) {
		t.Fatal("StrategyBuildError does not unwrap to its cause")
	}
	if got := err.Error(); got == "" {
		t.Fatal("empty error message")
	}

	nilCause := &StrategyBuildError{Backend: "tpm2"}
	if nilCause.Error() == "" {
		t.Fatal("empty error message for nil cause")
	}
}

// TestStrategyNotCompiledError_Format verifies the not-compiled error names the
// -tags value to rebuild with.
func TestStrategyNotCompiledError_Format(t *testing.T) {
	err := &StrategyNotCompiledError{Strategy: xkmssdk.StrategyAWSKMS}
	msg := err.Error()
	if msg == "" {
		t.Fatal("empty error message")
	}
	// The message must name the strategy so the operator knows what to enable.
	if want := string(xkmssdk.StrategyAWSKMS); !contains(msg, want) {
		t.Fatalf("error message %q does not mention strategy %q", msg, want)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestRegisterHardwareStrategyFactory_Duplicate verifies that re-registering a
// strategy ID panics (a duplicate registration is a programming error).
func TestRegisterHardwareStrategyFactory_Duplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	// TPM2 is already registered by init(); re-registering must panic.
	registerHardwareStrategyFactory(xkmssdk.StrategyTPM2, buildTPM2Strategy)
}

// TestRegisterHardwareStrategyFactory_Nil verifies that registering a nil
// factory panics.
func TestRegisterHardwareStrategyFactory_Nil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil factory registration")
		}
	}()
	registerHardwareStrategyFactory(qrdbsdk.StrategyID("test-nil-factory"), nil)
}

// withTestHardwareStrategy registers id into the hardware ID set and factory
// map for the duration of the test, restoring both afterwards. This lets the
// factory-dispatch paths of buildHardwareSealStrategies be exercised without a
// real hardware backend (TPM2/PKCS11/cloud KMS), which is the irreducible
// hardware floor and is covered only by hardware/integration tests.
func withTestHardwareStrategy(t *testing.T, id qrdbsdk.StrategyID, factory hardwareStrategyFactory) {
	t.Helper()
	hardwareStrategyIDs[id] = struct{}{}
	hardwareStrategyFactories[id] = factory
	t.Cleanup(func() {
		delete(hardwareStrategyIDs, id)
		delete(hardwareStrategyFactories, id)
	})
}

// TestBuildHardwareSealStrategies_FactorySuccess drives the success branch of
// buildHardwareSealStrategies: an in-set hardware ID with a registered factory
// returns a single-element strategy slice. A None strategy stands in for the
// real hardware-backed strategy so the dispatch contract is exercised without
// touching a TPM/HSM.
func TestBuildHardwareSealStrategies_FactorySuccess(t *testing.T) {
	id := qrdbsdk.StrategyID("test-hw-success")
	withTestHardwareStrategy(t, id, func(*config.Config) (qrdbsdk.SealingStrategy, error) {
		return qrdbsdk.NewNoneStrategy(), nil
	})

	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(id)

	strategies, err := buildHardwareSealStrategies(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(strategies) != 1 {
		t.Fatalf("expected exactly 1 strategy, got %d", len(strategies))
	}
	if strategies[0] == nil {
		t.Fatal("dispatched strategy is nil")
	}
}

// TestBuildHardwareSealStrategies_FactoryError drives the factory-error branch:
// when the registered factory fails, buildHardwareSealStrategies must surface
// the error verbatim (no wrapping at this layer — the factory owns its typed
// error).
func TestBuildHardwareSealStrategies_FactoryError(t *testing.T) {
	id := qrdbsdk.StrategyID("test-hw-error")
	wantErr := &StrategyBuildError{Backend: "test-hw", Cause: errors.New("factory boom")}
	withTestHardwareStrategy(t, id, func(*config.Config) (qrdbsdk.SealingStrategy, error) {
		return nil, wantErr
	})

	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(id)

	strategies, err := buildHardwareSealStrategies(cfg)
	if err == nil {
		t.Fatal("expected factory error, got nil")
	}
	if strategies != nil {
		t.Fatalf("expected nil strategies on factory error, got %v", strategies)
	}
	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected *StrategyBuildError, got %T: %v", err, err)
	}
}

// rootKeyRoundTrip exercises a SealingStrategy end-to-end: it seals a 32-byte
// root key and unseals it, asserting the recovered bytes match. This is the
// core proof that a strategy is functional.
func rootKeyRoundTrip(t *testing.T, strategy qrdbsdk.SealingStrategy) {
	t.Helper()
	ctx := context.Background()
	rootKey := []byte("0123456789abcdef0123456789abcdef")

	if !strategy.Available() {
		t.Fatal("strategy reports Available() == false; cannot seal")
	}

	sealed, err := strategy.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey: %v", err)
	}
	recovered, err := strategy.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey: %v", err)
	}
	if string(recovered) != string(rootKey) {
		t.Fatalf("recovered root key mismatch: got %x want %x", recovered, rootKey)
	}
}
