// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"log/slog"
	"reflect"
	"testing"

	xkmssignstore "github.com/jeremyhahn/go-xkms/pkg/signing/store"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms"
)

// Compile-time proof: NewSignerStore builds a SignerStorer on the
// StorageBackend quicbaas already re-exports.
var _ func(*slog.Logger, xkms.StorageBackend) xkms.SignerStorer = xkms.NewSignerStore

// Compile-time proof: the alias is the go-xkms interface itself.
var _ xkmssignstore.SignerStorer = xkms.SignerStorer(nil)

func TestSignerStore_IsTheGoXKMSStore(t *testing.T) {
	got := reflect.ValueOf(xkms.NewSignerStore).Pointer()
	want := reflect.ValueOf(xkmssignstore.NewSignerStore).Pointer()
	if got != want {
		t.Fatalf("xkms.NewSignerStore is not go-xkms store.NewSignerStore")
	}

	s := xkms.NewSignerStore(nil, xkms.NewStorageMemory())
	if s == nil {
		t.Fatal("NewSignerStore returned nil")
	}
}
