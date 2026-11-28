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

package kdf_test

import (
	"bytes"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/kdf"
)

func TestErrorAliases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		got, want error
	}{
		{"InvalidVersion", kdf.ErrInvalidVersion, xkmssdk.ErrPasswordHashInvalidVersion},
		{"InvalidPassword", kdf.ErrInvalidPassword, xkmssdk.ErrPasswordHashInvalidPassword},
		{"InvalidSalt", kdf.ErrInvalidSalt, xkmssdk.ErrPasswordHashInvalidSalt},
		{"ParamsMismatch", kdf.ErrParamsMismatch, xkmssdk.ErrPasswordHashParamsMismatch},
	}
	for _, c := range cases {
		if !errors.Is(c.got, c.want) {
			t.Errorf("%s: kdf alias is not the xkmssdk sentinel", c.name)
		}
	}
}

func TestHashDeriveKeyRoundTrip(t *testing.T) {
	t.Parallel()
	h := kdf.NewFIPSAwareHasher()
	key, salt, v, err := h.Hash([]byte("correct horse"))
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	again, err := h.DeriveKey([]byte("correct horse"), salt, v)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if !bytes.Equal(again, key) {
		t.Fatal("DeriveKey with the same password did not reproduce the key")
	}
	wrong, err := h.DeriveKey([]byte("wrong"), salt, v)
	if err != nil {
		t.Fatalf("DeriveKey(wrong): %v", err)
	}
	if bytes.Equal(wrong, key) {
		t.Fatal("DeriveKey with a wrong password reproduced the key")
	}
}

func TestHasherErrors(t *testing.T) {
	t.Parallel()
	h := kdf.NewFIPSAwareHasher()
	pw := []byte("correct horse")
	v := h.Version()

	if _, _, _, err := h.Hash(nil); !errors.Is(err, kdf.ErrInvalidPassword) {
		t.Errorf("Hash(nil): got %v, want ErrInvalidPassword", err)
	}
	if _, err := h.DeriveKey(pw, make([]byte, 4), v); !errors.Is(err, kdf.ErrInvalidSalt) {
		t.Errorf("DeriveKey(short salt): got %v, want ErrInvalidSalt", err)
	}
	if _, err := h.DeriveKey(pw, make([]byte, 32), kdf.HashVersion(99)); !errors.Is(err, kdf.ErrInvalidVersion) {
		t.Errorf("DeriveKey(version 99): got %v, want ErrInvalidVersion", err)
	}
}
