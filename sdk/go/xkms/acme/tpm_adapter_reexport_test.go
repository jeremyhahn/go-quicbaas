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

package acme_test

import (
	"errors"
	"testing"

	acmeclient "github.com/jeremyhahn/go-xkms/pkg/acme/client"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/acme"
)

// Compile-time proof the ACME TPM adapter types are the go-xkms ones,
// assignable both ways, so an adapter built through the SDK is the TPM module
// the go-xkms ACME client enrolls with.
var (
	_ xkmsacme.ACMEXKMSTPMAdapter = acme.ACMEXKMSTPMAdapter{}
	_ acme.ACMEXKMSTPMAdapter     = xkmsacme.ACMEXKMSTPMAdapter{}
	_ xkmsacme.ACMETPMModule      = acme.ACMETPMModule(nil)
	_ acme.ACMETPMModule          = xkmsacme.ACMETPMModule(nil)
	_ acme.ACMETPMModule          = (*acme.ACMEXKMSTPMAdapter)(nil)

	_ func(xkmssdk.TrustedPlatformModule, xkmssdk.TPMKeyBackend) *xkmsacme.ACMEXKMSTPMAdapter = acme.NewACMEXKMSTPMAdapter
)

// TestNewACMEXKMSTPMAdapter_BuildsTheGoXKMSAdapter proves the re-exported
// constructor builds an adapter from no TPM at all, as go-xkms's does, so a
// consumer may construct it before the TPM is opened.
func TestNewACMEXKMSTPMAdapter_BuildsTheGoXKMSAdapter(t *testing.T) {
	adapter := acme.NewACMEXKMSTPMAdapter(nil, nil)
	if adapter == nil {
		t.Fatal("NewACMEXKMSTPMAdapter returned nil")
	}
}

// TestNewACMEXKMSTPMAdapter_RefusesBadInput proves the adapter keeps the
// go-xkms input refusals: an unnamed attested key, and a key with no key
// backend to store it in, fail with the upstream typed errors.
func TestNewACMEXKMSTPMAdapter_RefusesBadInput(t *testing.T) {
	adapter := acme.NewACMEXKMSTPMAdapter(nil, nil)

	key, err := adapter.CreateAttestedKey("", "key-authorization")
	if !errors.Is(err, acmeclient.ErrAttestedKeyNameRequired) {
		t.Fatalf("empty name: err = %v, want ErrAttestedKeyNameRequired", err)
	}
	if key != nil {
		t.Fatal("empty name: got a key, want nil")
	}

	key, err = adapter.CreateAttestedKey("device-cert", "key-authorization")
	if !errors.Is(err, acmeclient.ErrKeyBackendNotConfigured) {
		t.Fatalf("no key backend: err = %v, want ErrKeyBackendNotConfigured", err)
	}
	if key != nil {
		t.Fatal("no key backend: got a key, want nil")
	}
}
