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
	"errors"
	"path/filepath"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestBuildPKCS11ServiceBackend_Constructs verifies the registered PKCS#11
// factory builds a ServiceBackend from the operator's seal config. No token
// I/O happens before Login, so the stub library is enough.
func TestBuildPKCS11ServiceBackend_Constructs(t *testing.T) {
	cfg := fullPKCS11Config(t)

	sb, err := buildPKCS11ServiceBackend(cfg, BackendSpec{Backend: config.BackendTypePKCS11}, xkmssdk.NewStorageMemory())
	if err != nil {
		t.Fatalf("buildPKCS11ServiceBackend: %v", err)
	}
	if sb == nil {
		t.Fatal("buildPKCS11ServiceBackend returned a nil backend")
	}
}

// TestBuildPKCS11ServiceBackend_UnloadableLibrary verifies a construction
// failure surfaces as a *BackendConstructError naming the pkcs11 backend.
func TestBuildPKCS11ServiceBackend_UnloadableLibrary(t *testing.T) {
	cfg := fullPKCS11Config(t)
	cfg.Seal.PKCS11.Library = filepath.Join(t.TempDir(), "absent.so")

	sb, err := buildPKCS11ServiceBackend(cfg, BackendSpec{Backend: config.BackendTypePKCS11}, xkmssdk.NewStorageMemory())
	if err == nil {
		t.Fatalf("buildPKCS11ServiceBackend() = %v, want error for a missing library", sb)
	}
	var constructErr *BackendConstructError
	if !errors.As(err, &constructErr) || constructErr.Backend != config.BackendTypePKCS11 {
		t.Fatalf("error = %T %v, want *BackendConstructError for pkcs11", err, err)
	}
}
