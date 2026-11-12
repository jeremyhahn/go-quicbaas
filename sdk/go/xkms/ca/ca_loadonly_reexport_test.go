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

package ca_test

import (
	"context"
	"reflect"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/ca"
)

// Compile-time proof the load-only constructors keep the go-xkms signatures.
var (
	_ func(*ca.Params) (ca.TrustedCA, error)                                = ca.NewParentCA
	_ func(context.Context, *ca.Params, ca.TrustedCA) (ca.TrustedCA, error) = ca.NewIntermediateCA
)

// TestLoadOnlyConstructorsReexported proves the re-exports are the go-xkms
// functions themselves, not wrappers.
func TestLoadOnlyConstructorsReexported(t *testing.T) {
	if reflect.ValueOf(ca.NewParentCA).Pointer() != reflect.ValueOf(xkmssdk.NewParentCA).Pointer() {
		t.Fatalf("ca.NewParentCA is not xkmssdk.NewParentCA")
	}
	if reflect.ValueOf(ca.NewIntermediateCA).Pointer() != reflect.ValueOf(xkmssdk.NewIntermediateCA).Pointer() {
		t.Fatalf("ca.NewIntermediateCA is not xkmssdk.NewIntermediateCA")
	}
}
