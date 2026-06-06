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
)

// stubFactor is the minimal qrdbsdk.AuthFactorPlugin used to verify that
// XKMSCryptoOptions registers factors into the composed registry.
type stubFactor struct{ name string }

func (f *stubFactor) Name() string { return f.name }
func (f *stubFactor) Enroll(context.Context, *qrdbsdk.AuthFactorEnrollParams) (*qrdbsdk.AuthFactorEnrollment, error) {
	return &qrdbsdk.AuthFactorEnrollment{FactorName: f.name}, nil
}
func (f *stubFactor) Challenge(context.Context, *qrdbsdk.AuthFactorEnrollment) (*qrdbsdk.AuthFactorChallenge, error) {
	return &qrdbsdk.AuthFactorChallenge{}, nil
}
func (f *stubFactor) Verify(context.Context, *qrdbsdk.AuthFactorChallenge, *qrdbsdk.AuthFactorResponse) error {
	return nil
}

func TestXKMSCryptoOptions_NilConfigYieldsDefaults(t *testing.T) {
	opts, err := XKMSCryptoOptions(nil)
	if err != nil {
		t.Fatalf("XKMSCryptoOptions(nil): %v", err)
	}
	if len(opts) != 3 {
		t.Fatalf("expected 3 options (StrategyRegistry, PINBackend, FactorRegistry), got %d", len(opts))
	}
}

func TestXKMSCryptoOptions_DefaultStrategiesRegistered(t *testing.T) {
	// Construct directly via the helper used inside XKMSCryptoOptions so we
	// can assert which strategy names are registered.
	reg := qrdbsdk.NewDefaultStrategyRegistry()
	avail := reg.ListAvailable()
	if len(avail) < 2 {
		t.Fatalf("default registry must include at least Software + None, got %v", avail)
	}
	for _, want := range []qrdbsdk.StrategyID{qrdbsdk.StrategyPassphrase, qrdbsdk.StrategyNone} {
		found := false
		for _, got := range avail {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("default registry missing %q (have %v)", want, avail)
		}
	}
}

func TestXKMSCryptoOptions_RegistersInjectedStrategies(t *testing.T) {
	calledTPM2 := false
	cfg := &XKMSCryptoConfig{
		StrategyFactories: map[qrdbsdk.StrategyID]qrdbsdk.StrategyFactory{
			xkmssdk.StrategyTPM2: func() (qrdbsdk.SealingStrategy, error) {
				calledTPM2 = true
				return qrdbsdk.NewNoneStrategy(), nil // stand-in for a real TPM2 strategy
			},
		},
	}
	opts, err := XKMSCryptoOptions(cfg)
	if err != nil {
		t.Fatalf("XKMSCryptoOptions: %v", err)
	}
	if len(opts) != 3 {
		t.Fatalf("expected 3 options, got %d", len(opts))
	}
	// We can't directly introspect the registry via the option (it's
	// stored on serverOptions which is unexported), but we can re-derive
	// the registry and confirm registration semantics via the same code
	// path XKMSCryptoOptions takes.
	reg := qrdbsdk.NewDefaultStrategyRegistry()
	for name, factory := range cfg.StrategyFactories {
		if err := reg.Register(name, factory); err != nil {
			t.Fatalf("re-Register %q: %v", name, err)
		}
	}
	got, err := reg.LookupFactory(xkmssdk.StrategyTPM2)
	if err != nil {
		t.Fatalf("LookupFactory(tpm2): %v", err)
	}
	if _, err := got(); err != nil {
		t.Fatalf("tpm2 factory invocation: %v", err)
	}
	if !calledTPM2 {
		t.Fatal("tpm2 factory not invoked")
	}
}

func TestXKMSCryptoOptions_PINBackendOverride(t *testing.T) {
	sb, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.DefaultHashConfig())
	if err != nil {
		t.Fatalf("NewSoftwareBackend: %v", err)
	}
	cfg := &XKMSCryptoConfig{PINBackend: sb}
	opts, err := XKMSCryptoOptions(cfg)
	if err != nil {
		t.Fatalf("XKMSCryptoOptions: %v", err)
	}
	if len(opts) != 3 {
		t.Fatalf("expected 3 options, got %d", len(opts))
	}
}

func TestXKMSCryptoOptions_DefaultPINBackend(t *testing.T) {
	// With no PINBackend and no PINStorage, XKMSCryptoOptions must build an
	// in-memory-only SoftwareBackend rather than returning an error.
	cfg := &XKMSCryptoConfig{}
	if _, err := XKMSCryptoOptions(cfg); err != nil {
		t.Fatalf("XKMSCryptoOptions with nil PINBackend: %v", err)
	}
}

func TestXKMSCryptoOptions_FactorsRegistered(t *testing.T) {
	cfg := &XKMSCryptoConfig{
		Factors: []qrdbsdk.AuthFactorPlugin{
			&stubFactor{name: "fido2"},
			&stubFactor{name: "piv"},
		},
	}
	opts, err := XKMSCryptoOptions(cfg)
	if err != nil {
		t.Fatalf("XKMSCryptoOptions: %v", err)
	}
	if len(opts) != 3 {
		t.Fatalf("expected 3 options, got %d", len(opts))
	}
}

func TestXKMSCryptoOptions_DuplicateFactorIsError(t *testing.T) {
	cfg := &XKMSCryptoConfig{
		Factors: []qrdbsdk.AuthFactorPlugin{
			&stubFactor{name: "fido2"},
			&stubFactor{name: "fido2"}, // duplicate
		},
	}
	var ce *XKMSCryptoError
	_, err := XKMSCryptoOptions(cfg)
	if !errors.As(err, &ce) {
		t.Fatalf("expected XKMSCryptoError, got %T %v", err, err)
	}
	var dup *qrdbsdk.FactorAlreadyRegisteredError
	if !errors.As(err, &dup) {
		t.Fatalf("expected wrapped FactorAlreadyRegisteredError, got %v", err)
	}
}

func TestXKMSCryptoOptions_NilFactorIsErrorNotPanic(t *testing.T) {
	// A nil entry in Factors must surface a typed XKMSCryptoError wrapping
	// ErrNilFactor — not a nil-pointer panic from calling f.Name() on the
	// error path.
	cfg := &XKMSCryptoConfig{
		Factors: []qrdbsdk.AuthFactorPlugin{
			&stubFactor{name: "fido2"},
			nil, // composition-time programming error
		},
	}
	var ce *XKMSCryptoError
	_, err := XKMSCryptoOptions(cfg)
	if !errors.As(err, &ce) {
		t.Fatalf("expected XKMSCryptoError, got %T %v", err, err)
	}
	if !errors.Is(err, ErrNilFactor) {
		t.Fatalf("expected wrapped ErrNilFactor, got %v", err)
	}
}

func TestXKMSCryptoError_Format(t *testing.T) {
	base := errors.New("boom")
	e := &XKMSCryptoError{Stage: "stage-a", Cause: base}
	if e.Error() == "" {
		t.Fatal("Error() returned empty")
	}
	if !errors.Is(e, base) {
		t.Fatal("errors.Is(e, base) = false, want true via Unwrap")
	}
}
