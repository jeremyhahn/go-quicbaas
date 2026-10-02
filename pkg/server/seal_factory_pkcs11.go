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
	"context"
	"crypto/x509"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the PKCS#11 HSM sealing strategy with the barrier
// composer. It is compiled only under the `pkcs11` build tag, which pulls in
// go-xkms/pkg/backend/pkcs11 (cgo + miekg/pkcs11). When the tag is absent the
// factory is not registered and buildHardwareSealStrategies returns a
// StrategyNotCompiledError instructing the operator to rebuild with -tags
// pkcs11.
//
// QuicBaaS composes go-xkms: a pkcs11backend.Backend (which implements
// xkmstypes.Sealer) is constructed from the operator config and wrapped by
// xkmssdk.NewPKCS11Strategy. No HSM crypto is reimplemented here.

// PKCS11ConfigError reports that the operator's PKCS#11 seal configuration is
// missing a required field. It is returned before any backend construction is
// attempted so the operator gets a precise, library-independent diagnostic.
// Field names the missing configuration key.
type PKCS11ConfigError struct {
	Field string
}

// Error implements the error interface.
func (e *PKCS11ConfigError) Error() string {
	return "seal strategy: pkcs11 config missing required field: " + e.Field
}

// PKCS11KeyRegistrationError reports that the factory could not log in to the
// token or ensure the HSM-resident sealing key is usable. KeyLabel names the
// configured wrapping key; Cause carries the underlying go-xkms failure.
type PKCS11KeyRegistrationError struct {
	KeyLabel string
	Cause    error
}

// Error implements the error interface.
func (e *PKCS11KeyRegistrationError) Error() string {
	return "seal strategy: pkcs11 sealing key registration failed for key " + e.KeyLabel + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *PKCS11KeyRegistrationError) Unwrap() error { return e.Cause }

// buildPKCS11Strategy constructs the PKCS#11 sealing strategy from
// cfg.Seal.PKCS11.
//
// It validates the operator config up front and surfaces any missing required
// field as a *StrategyBuildError wrapping a *PKCS11ConfigError, so the failure
// is distinguishable from a backend-construction failure. The required fields
// are:
//
//   - Library: path to the PKCS#11 provider shared object.
//   - TokenLabel: the token label used to locate the token. The go-xkms
//     pkcs11 backend addresses tokens by label; Slot, when set, is an
//     additional selector but does not substitute for the label.
//   - PIN: the user PIN authenticating to the token.
//   - KeyLabel: the label of the HSM-resident wrapping key.
//
// On success it builds a pkcs11backend.Backend from the config (with in-memory
// metadata stores — the actual key material stays in the HSM), logs in to the
// token and ensures the operator's wrapping key is usable by the backend (see
// ensurePKCS11SealKey), and wraps the backend plus the xkmstypes.KeyAttributes
// that identify the HSM-resident AES-256-GCM wrapping key by CKA_LABEL / CKA_ID
// in xkmssdk.NewPKCS11Strategy.
//
// Unlike the cloud factories, the PKCS#11 backend opens no token session until
// Login(); the factory performs that login so the key can be registered. This
// means the factory requires a reachable token at startup — there is no token
// emulator, so the registration is validated against a real HSM (or SoftHSM).
//
// The optional hardware SymmetricEncrypter is left nil: barrier data
// encryption then derives a software DEK from the HSM-protected root key,
// keeping the root key hardware-bound while avoiding an HSM round-trip per
// data operation.
func buildPKCS11Strategy(cfg *config.Config) (qrdbsdk.SealingStrategy, error) {
	p11 := cfg.Seal.PKCS11

	if p11.Library == "" {
		return nil, &StrategyBuildError{Backend: "pkcs11", Cause: &PKCS11ConfigError{Field: "library"}}
	}
	if p11.TokenLabel == "" {
		return nil, &StrategyBuildError{Backend: "pkcs11", Cause: &PKCS11ConfigError{Field: "token_label"}}
	}
	if p11.PIN == "" {
		return nil, &StrategyBuildError{Backend: "pkcs11", Cause: &PKCS11ConfigError{Field: "pin"}}
	}
	if p11.KeyLabel == "" {
		return nil, &StrategyBuildError{Backend: "pkcs11", Cause: &PKCS11ConfigError{Field: "key_label"}}
	}

	backendCfg, err := pkcs11BackendConfig(p11, xkmssdk.NewStorageMemory())
	if err != nil {
		return nil, &StrategyBuildError{Backend: "pkcs11", Cause: err}
	}

	backend, err := xkmssdk.NewPKCS11Backend(backendCfg)
	if err != nil {
		backendCfg.PIN.Clear()
		return nil, &StrategyBuildError{Backend: "pkcs11", Cause: err}
	}

	keyAttrs := pkcs11SealKeyAttributes(p11.KeyLabel)

	if err := ensurePKCS11SealKey(backend, keyAttrs); err != nil {
		return nil, &StrategyBuildError{Backend: "pkcs11", Cause: errors.Join(err, releasePKCS11Backend(backend))}
	}

	return xkmssdk.NewPKCS11Strategy(backend, keyAttrs, nil), nil
}

// pkcs11BackendConfig builds the go-xkms PKCS#11 backend config from the
// operator's PKCS#11 settings. keyStorage backs the backend's key-metadata
// store; the key material itself never leaves the token.
//
// The PIN is sealed into its own SecretValue enclave here, so the backend
// holds it only in protected memory. Custody of that SecretValue passes to the
// backend, which reads it on every login for its whole lifetime; a caller that
// abandons the config (construction failed) must Clear it, and a caller that
// abandons a constructed backend must release it with releasePKCS11Backend.
// A PIN that cannot be sealed fails the build rather than reaching the
// backend as an empty value it would read as "no PIN configured".
func pkcs11BackendConfig(p11 config.PKCS11SealConfig, keyStorage xkmssdk.StorageBackend) (*xkmssdk.PKCS11BackendConfig, error) {
	pin, err := sealPKCS11PIN(p11.PIN)
	if err != nil {
		return nil, err
	}
	return &xkmssdk.PKCS11BackendConfig{
		CN:          p11.KeyLabel,
		Library:     p11.Library,
		TokenLabel:  p11.TokenLabel,
		Slot:        p11.Slot,
		PIN:         pin,
		KeyStorage:  keyStorage,
		CertStorage: xkmssdk.NewStorageMemory(),
	}, nil
}

// sealPKCS11PIN seals pin into a SecretValue enclave and wipes the transient
// byte copy it made on the way in. An empty pin yields an empty SecretValue,
// which the backend treats as "no PIN configured".
func sealPKCS11PIN(pin string) (*xkmssdk.SecretValue, error) {
	b := []byte(pin)
	sv, err := xkmssdk.NewLiteralSecretValue(b)
	clear(b)
	return sv, err
}

// releasePKCS11Backend closes a backend the caller is abandoning and destroys
// the PIN enclave the backend held, so a failed build leaves neither an open
// PKCS#11 context nor the PIN behind. It returns the Close error, if any.
func releasePKCS11Backend(b *xkmssdk.PKCS11Backend) error {
	err := b.Close()
	b.Config().PIN.Clear()
	return err
}

// ensurePKCS11SealKey logs in to the token and ensures the operator's
// HSM-resident wrapping key is usable so the sealer's SymmetricEncrypter can
// resolve it.
//
// The PKCS#11 backend opens no session until Login(); both GetSymmetricKey and
// GenerateSymmetricKey return ErrNotInitialized beforehand. The factory
// therefore logs in first, then performs a check-and-skip on the token itself
// (the HSM, not an in-process cache, is the source of truth): GetSymmetricKey
// runs a CKA_LABEL lookup, and only if the key is genuinely absent
// (backend.ErrKeyNotFound) does it generate one with GenerateSymmetricKey — the
// first-init path. An operator who pre-provisions the key in the token has it
// detected and reused.
//
// Idempotency: an existing token key is detected and reused unchanged; the
// factory never rotates or destroys it, so previously-sealed data unseals after
// a restart. Login or any non-not-found lookup failure (and a generation
// failure) is wrapped in *PKCS11KeyRegistrationError.
func ensurePKCS11SealKey(b *xkmssdk.PKCS11Backend, keyAttrs *xkmssdk.KeyAttributes) error {
	if err := b.Login(); err != nil {
		return &PKCS11KeyRegistrationError{KeyLabel: keyAttrs.CN, Cause: err}
	}

	if _, err := b.GetSymmetricKey(context.Background(), keyAttrs); err == nil {
		return nil
	} else if !errors.Is(err, xkmssdk.ErrBackendKeyNotFound) {
		return &PKCS11KeyRegistrationError{KeyLabel: keyAttrs.CN, Cause: err}
	}

	if _, err := b.GenerateSymmetricKey(context.Background(), keyAttrs); err != nil {
		return &PKCS11KeyRegistrationError{KeyLabel: keyAttrs.CN, Cause: err}
	}
	return nil
}

// pkcs11SealKeyAttributes builds the xkmstypes.KeyAttributes that identify the
// HSM-resident symmetric wrapping key used to seal and unseal the barrier root
// key. The PKCS#11 backend resolves the key on the token by CKA_LABEL / CKA_ID
// derived from CN, so CN carries the operator-configured KeyLabel.
//
// The attributes describe a 256-bit AES-GCM symmetric key resident in the
// PKCS#11 store. SymmetricAlgorithm being non-empty is what makes the backend
// treat the request as a symmetric seal (Backend.Seal rejects asymmetric keys);
// KeyType KeyTypeEncryption marks the key's purpose.
func pkcs11SealKeyAttributes(keyLabel string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:                 keyLabel,
		StoreType:          xkmssdk.StorePKCS11,
		KeyType:            xkmssdk.KeyTypeEncryption,
		KeyAlgorithm:       x509.UnknownPublicKeyAlgorithm,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
}

// init registers the PKCS#11 strategy factory. Reached only when the binary is
// built with -tags pkcs11.
func init() {
	registerHardwareStrategyFactory(xkmssdk.StrategyPKCS11, buildPKCS11Strategy)
}
