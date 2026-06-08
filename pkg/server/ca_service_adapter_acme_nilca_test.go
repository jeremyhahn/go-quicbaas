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
	"errors"
	"testing"
)

// CertificateBySerial and RevokeBySerial fail closed with the nil-CA sentinel
// when the adapter's backing CA has been cleared (e.g. before SetCA wires the
// real CA at startup). The serial parse succeeds first, so this exercises the
// post-parse nil-CA guard specifically.
func TestCAServiceAdapter_ACMESerial_NilCA(t *testing.T) {
	t.Run("CertificateBySerial nil CA", func(t *testing.T) {
		adapter, _ := newCAServiceAdapter(newFakeTrustedCA(t))
		adapter.ca = nil
		if _, err := adapter.CertificateBySerial("ff"); !errors.Is(err, ErrCAServiceAdapterNilCA) {
			t.Fatalf("want ErrCAServiceAdapterNilCA, got %v", err)
		}
	})

	t.Run("RevokeBySerial nil CA", func(t *testing.T) {
		adapter, _ := newCAServiceAdapter(newFakeTrustedCA(t))
		adapter.ca = nil
		if err := adapter.RevokeBySerial("ff", 0); !errors.Is(err, ErrCAServiceAdapterNilCA) {
			t.Fatalf("want ErrCAServiceAdapterNilCA, got %v", err)
		}
	})
}
