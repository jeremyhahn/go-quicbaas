//go:build pkcs11

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

// piv_local_pkcs11.go builds the PKCS#11-backed local PIV session. Physical
// smart cards (e.g. YubiKey PIV via ykcs11) are reached through the go-xkms
// pkcs11 backend; this mirrors xkey's PKCS#11 PIV wiring (storage via
// NewPIVPKCS11Backend, keys via the xkms pkcs11 key backend + PIV backend
// resolver).
package main

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// pkcs11PIVSession is the PKCS#11-backed local PIV session.
type pkcs11PIVSession struct {
	store xkmssdk.PIVCertificateStorage

	// pin is the token PIN sealed in a memguard enclave. The certificate
	// store's session pool reads it whenever it opens a session, so the
	// session owns it for its lifetime and destroys it on Close.
	pin *secure.Secret
}

// newPKCS11PIVSession assembles the PKCS#11 session: certificate storage on
// the token, the xkms pkcs11 key backend for signing, and the PIV manager +
// backend resolver, mirroring xkey's ensurePIVInitialized/ensurePKCS11Backend.
func newPKCS11PIVSession(cfg *localPIVConfig) (localPIVSession, error) {
	if cfg.pkcs11Module == "" {
		return nil, &PIVLocalSessionError{Op: "init pkcs11", Cause: errPKCS11ModuleRequired}
	}
	if cfg.PIN == "" {
		return nil, &PIVPINRequiredError{}
	}

	// NewSecret wipes the byte copy it seals, so only the enclave holds the
	// PIN from here on.
	pin, err := secure.NewSecret([]byte(cfg.PIN))
	if err != nil {
		return nil, &PIVLocalSessionError{Op: "seal pkcs11 pin", Cause: err}
	}

	store, err := xkmssdk.NewPIVPKCS11Backend(&xkmssdk.PIVPKCS11StorageConfig{
		LibraryPath: cfg.pkcs11Module,
		TokenLabel:  cfg.pkcs11Token,
		SlotID:      -1, // resolve by token label
		PIN:         pin,
		RequireSO:   strings.Contains(strings.ToLower(cfg.pkcs11Module), "ykcs11"),
	})
	if err != nil {
		pin.Destroy()
		return nil, &PIVLocalSessionError{Op: "init pkcs11 cert storage", Cause: err}
	}
	session := &pkcs11PIVSession{store: store, pin: pin}

	// Refuse to clobber an xkms singleton something else initialized.
	if xkmssdk.ServiceIsInitialized() && !localPIVOwnsXKMS {
		return nil, &PIVLocalSessionError{Op: "init pkcs11 key backend", Cause: errors.Join(errXKMSServiceInUse, session.Close())}
	}
	if !xkmssdk.ServiceIsInitialized() {
		// The xkms pkcs11 backend registration reads the PIN from this
		// string-keyed map; a plain string is the only shape it accepts.
		backendConfig := map[string]interface{}{
			"library": cfg.pkcs11Module,
			"label":   cfg.pkcs11Token,
			"pin":     cfg.PIN,
		}
		if err := xkmssdk.AutoInitialize(&xkmssdk.AutoConfig{
			DefaultBackend: string(xkmssdk.BackendPKCS11),
			BackendConfigs: map[xkmssdk.ServiceBackendType]map[string]interface{}{
				xkmssdk.BackendPKCS11: backendConfig,
			},
		}); err != nil {
			return nil, &PIVLocalSessionError{Op: "init pkcs11 key backend", Cause: errors.Join(err, session.Close())}
		}
		localPIVOwnsXKMS = true
	}

	if err := initLocalPIVManager(localPIVBackendPKCS11, store); err != nil {
		return nil, errors.Join(err, session.Close())
	}
	return session, nil
}

// Certificate reads the slot certificate from the token.
func (s *pkcs11PIVSession) Certificate(slot string) (*x509.Certificate, error) {
	return localPIVCertificate(s.store, slot)
}

// SignNonce signs SHA-256(nonce) with the slot key on the token, resolved
// through the go-xkms PIV backend resolver.
func (s *pkcs11PIVSession) SignNonce(ctx context.Context, slot string, nonce []byte) ([]byte, error) {
	return localPIVSignNonce(ctx, localPIVBackendPKCS11, slot, nonce)
}

// Close releases the token session and destroys the PIN enclave.
func (s *pkcs11PIVSession) Close() error {
	err := s.store.Close()
	s.pin.Destroy()
	return err
}
