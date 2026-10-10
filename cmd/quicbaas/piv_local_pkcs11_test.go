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

package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestNewPKCS11PIVSession_RejectsIncompleteConfig verifies the PKCS#11 session
// refuses to start without a module or a PIN, before it seals anything or
// touches a token.
func TestNewPKCS11PIVSession_RejectsIncompleteConfig(t *testing.T) {
	t.Run("missing module", func(t *testing.T) {
		_, err := newPKCS11PIVSession(&localPIVConfig{backend: localPIVBackendPKCS11, PIN: "123456"})
		var sessionErr *PIVLocalSessionError
		if !errors.As(err, &sessionErr) || !errors.Is(err, errPKCS11ModuleRequired) {
			t.Fatalf("error = %T %v, want *PIVLocalSessionError wrapping errPKCS11ModuleRequired", err, err)
		}
	})
	t.Run("missing pin", func(t *testing.T) {
		_, err := newPKCS11PIVSession(&localPIVConfig{backend: localPIVBackendPKCS11, pkcs11Module: "/lib/p11.so"})
		var pinErr *PIVPINRequiredError
		if !errors.As(err, &pinErr) {
			t.Fatalf("error = %T %v, want *PIVPINRequiredError", err, err)
		}
	})
}

// TestNewPKCS11PIVSession_UnloadableModule verifies a module that cannot be
// loaded surfaces as a *PIVLocalSessionError from the certificate-storage
// step, after the PIN was sealed (and, on this failure, destroyed).
func TestNewPKCS11PIVSession_UnloadableModule(t *testing.T) {
	_, err := newPKCS11PIVSession(&localPIVConfig{
		backend:      localPIVBackendPKCS11,
		pkcs11Module: filepath.Join(t.TempDir(), "absent.so"),
		pkcs11Token:  "quicbaas-test",
		PIN:          "123456",
	})
	var sessionErr *PIVLocalSessionError
	if !errors.As(err, &sessionErr) {
		t.Fatalf("error = %T %v, want *PIVLocalSessionError", err, err)
	}
	if sessionErr.Op != "init pkcs11 cert storage" {
		t.Fatalf("PIVLocalSessionError.Op = %q, want init pkcs11 cert storage", sessionErr.Op)
	}
}
