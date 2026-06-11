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
	"context"
	"errors"
	"testing"
)

// A resolver built from a server with no embedded qrdb node fails closed with a
// typed configuration error rather than returning a nil servicer.
func TestPlatformUserServicerResolver_NoNodeFailsClosed(t *testing.T) {
	r := newPlatformUserServicerResolver(&Server{})
	svc, err := r.UserServicer(context.Background())
	if err == nil {
		t.Fatal("expected a typed error when no qrdb node is present")
	}
	if svc != nil {
		t.Fatalf("expected nil servicer, got %T", svc)
	}
	var notInit *QRDBClientNotInitializedError
	if !errors.As(err, &notInit) {
		t.Fatalf("expected QRDBClientNotInitializedError, got %T: %v", err, err)
	}
}

// A nil resolver / nil server also fails closed rather than panicking.
func TestPlatformUserServicerResolver_NilFailsClosed(t *testing.T) {
	var r *platformUserServicerResolver
	if _, err := r.UserServicer(context.Background()); err == nil {
		t.Fatal("expected a typed error from a nil resolver")
	}
	r2 := newPlatformUserServicerResolver(nil)
	if _, err := r2.UserServicer(context.Background()); err == nil {
		t.Fatal("expected a typed error from a resolver with a nil server")
	}
}
