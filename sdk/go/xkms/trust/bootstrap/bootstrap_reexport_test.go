// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package bootstrap_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	xkmsbootstrap "github.com/jeremyhahn/go-xkms/sdk/go/bootstrap"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/bootstrap"
)

// The re-exported trust-bootstrap types are go-xkms's own.
var (
	_ xkmsbootstrap.Config          = bootstrap.Config{}
	_ *xkmsbootstrap.CABundle       = (*bootstrap.CABundle)(nil)
	_ xkmsbootstrap.Mode            = bootstrap.Mode("")
	_ xkmsbootstrap.PinStore        = bootstrap.PinStore(nil)
	_ *xkmsbootstrap.NoiseStaticKey = (*bootstrap.NoiseStaticKey)(nil)
	_ *xkmsbootstrap.P256StaticKey  = (*bootstrap.P256StaticKey)(nil)
	_ *xkmsbootstrap.SuiteError     = (*bootstrap.SuiteError)(nil)
	_ *xkmsbootstrap.NoSuiteError   = (*bootstrap.NoSuiteError)(nil)
)

// TestP256Suite_ReExportsCompose: the P-256 key helpers, the size and suite
// constants and the suite errors are go-xkms's and work together.
func TestP256Suite_ReExportsCompose(t *testing.T) {
	key, err := bootstrap.GenerateP256StaticKey()
	if err != nil {
		t.Fatalf("GenerateP256StaticKey: %v", err)
	}
	if key == nil {
		t.Fatal("GenerateP256StaticKey returned a nil key")
	}
	key.Destroy()

	// The server's pinned P-256 key is an uncompressed point: 65 bytes, so
	// 130 hex characters on the wire.
	if got := bootstrap.P256PublicKeySize * 2; got != 130 {
		t.Fatalf("P256PublicKeySize*2 = %d, want 130", got)
	}
	if bootstrap.P256HexKeySize != xkmsbootstrap.P256HexKeySize {
		t.Fatalf("P256HexKeySize = %d, want go-xkms's %d", bootstrap.P256HexKeySize, xkmsbootstrap.P256HexKeySize)
	}
	if bootstrap.SuiteNoiseNK == bootstrap.SuiteP256AESGCM {
		t.Fatal("the two suite identifiers are equal")
	}

	for name, pair := range map[string][2]any{
		"GenerateP256StaticKey": {bootstrap.GenerateP256StaticKey, xkmsbootstrap.GenerateP256StaticKey},
		"NewP256StaticKey":      {bootstrap.NewP256StaticKey, xkmsbootstrap.NewP256StaticKey},
		"EncodeP256StaticKey":   {bootstrap.EncodeP256StaticKey, xkmsbootstrap.EncodeP256StaticKey},
		"DecodeP256StaticKey":   {bootstrap.DecodeP256StaticKey, xkmsbootstrap.DecodeP256StaticKey},
		"EnsureSuiteAvailable":  {bootstrap.EnsureSuiteAvailable, xkmsbootstrap.EnsureSuiteAvailable},
	} {
		if reflect.ValueOf(pair[0]).Pointer() != reflect.ValueOf(pair[1]).Pointer() {
			t.Errorf("%s is not go-xkms's", name)
		}
	}

	if err := bootstrap.EnsureSuiteAvailable(bootstrap.SuiteP256AESGCM); err != nil {
		t.Fatalf("EnsureSuiteAvailable(SuiteP256AESGCM) = %v, want nil", err)
	}

	suiteErr := &bootstrap.SuiteError{Suite: bootstrap.SuiteNoiseNK, Cause: bootstrap.ErrSuiteKeyMissing}
	if !errors.Is(suiteErr, bootstrap.ErrSuiteKeyMissing) {
		t.Fatalf("errors.Is(SuiteError{ErrSuiteKeyMissing}, ErrSuiteKeyMissing) = false: %v", suiteErr)
	}
	for _, pair := range [][2]error{
		{bootstrap.ErrSuiteUnavailable, xkmsbootstrap.ErrSuiteUnavailable},
		{bootstrap.ErrSuiteKeyMissing, xkmsbootstrap.ErrSuiteKeyMissing},
		{bootstrap.ErrSuiteKeyInvalid, xkmsbootstrap.ErrSuiteKeyInvalid},
		{bootstrap.ErrNoSuite, xkmsbootstrap.ErrNoSuite},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%v is not go-xkms's sentinel", pair[0])
		}
	}
}

// TestFetch_ReExportCallsGoXKMS: the SDK's Fetch is go-xkms's; with no mode
// it refuses with the re-exported ErrInvalidMode.
func TestFetch_ReExportCallsGoXKMS(t *testing.T) {
	_, err := bootstrap.Fetch(context.Background(), bootstrap.Config{})
	if !errors.Is(err, bootstrap.ErrInvalidMode) {
		t.Fatalf("Fetch(Config{}) = %v, want ErrInvalidMode", err)
	}
	if !errors.Is(err, xkmsbootstrap.ErrInvalidMode) {
		t.Fatalf("bootstrap.ErrInvalidMode is not go-xkms's: %v", err)
	}
}
