//go:build rest

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

package rest

import (
	"bytes"
	"context"
	stderrors "errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// crlOnlyCA is a contract double for the one TrustedCA method the servicer's
// CRL verb reaches. The embedded interface is nil, so a call to any other
// method panics and exposes a servicer that strayed outside that contract.
type crlOnlyCA struct {
	xkmssdk.TrustedCA
	der []byte
	err error
}

func (c *crlOnlyCA) CRL(context.Context) ([]byte, error) {
	return c.der, c.err
}

// errCRLUnavailable is the CA failure the error-path test injects.
var errCRLUnavailable = stderrors.New("crl unavailable")

// TestXKMSCAServicer_CRL_ReturnsCurrentDER verifies the servicer hands the
// CA's current CRL through unchanged — no re-encoding, since go-xkms's
// HandleGetCRL writes these bytes as application/pkix-crl.
func TestXKMSCAServicer_CRL_ReturnsCurrentDER(t *testing.T) {
	want := []byte{0x30, 0x82, 0x01, 0x0a}
	a := newXKMSCAServicer(&crlOnlyCA{der: want}, "")

	got, err := a.CRL(context.Background())
	if err != nil {
		t.Fatalf("CRL: unexpected error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("CRL: got %x, want %x", got, want)
	}
}

// TestXKMSCAServicer_CRL_WrapsCAFailure verifies a CA failure surfaces as the
// typed XKMSCAOperationError, still matchable against the underlying cause,
// and that no partial CRL escapes alongside it.
func TestXKMSCAServicer_CRL_WrapsCAFailure(t *testing.T) {
	a := newXKMSCAServicer(&crlOnlyCA{der: []byte{0x30}, err: errCRLUnavailable}, "")

	got, err := a.CRL(context.Background())
	if got != nil {
		t.Fatalf("CRL: expected no bytes on failure, got %x", got)
	}
	var opErr *XKMSCAOperationError
	if !stderrors.As(err, &opErr) {
		t.Fatalf("CRL: expected *XKMSCAOperationError, got %T (%v)", err, err)
	}
	if opErr.Op != "get crl" {
		t.Fatalf("CRL: unexpected op label %q", opErr.Op)
	}
	if !stderrors.Is(err, errCRLUnavailable) {
		t.Fatalf("CRL: underlying cause lost: %v", err)
	}
}
