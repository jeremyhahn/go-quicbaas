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

package xkms_test

import (
	"reflect"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms"
)

// TestTPMAttributeBuilders_AreTheGoXKMSFunctions proves the EK and SRK
// attribute builders the SDK re-exports are the go-xkms functions themselves,
// not wrappers or lookalikes (go-quicbaas#1777).
func TestTPMAttributeBuilders_AreTheGoXKMSFunctions(t *testing.T) {
	cases := []struct {
		name     string
		got      any
		upstream any
	}{
		{"EKAttributesFromConfig", xkms.EKAttributesFromConfig, xkmssdk.EKAttributesFromConfig},
		{"SRKAttributesFromConfig", xkms.SRKAttributesFromConfig, xkmssdk.SRKAttributesFromConfig},
	}
	for _, tc := range cases {
		got := reflect.ValueOf(tc.got)
		want := reflect.ValueOf(tc.upstream)
		if got.Kind() != reflect.Func || got.IsNil() {
			t.Fatalf("%s is not a function value", tc.name)
		}
		if got.Type() != want.Type() {
			t.Fatalf("%s type %v, want %v", tc.name, got.Type(), want.Type())
		}
		if got.Pointer() != want.Pointer() {
			t.Fatalf("%s is not the go-xkms function", tc.name)
		}
	}
}
