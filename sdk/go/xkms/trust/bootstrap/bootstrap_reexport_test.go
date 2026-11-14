// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package bootstrap_test

import (
	"context"
	"errors"
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
)

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
