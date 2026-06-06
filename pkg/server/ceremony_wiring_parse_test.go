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

//go:build !integration

// ceremony_wiring_parse_test.go covers the x509-parse error branch of
// SOCertSPKIThumbprint: a well-formed CERTIFICATE PEM block whose DER body is
// not a valid certificate must surface a typed SOCertSPKIThumbprintError.

package server

import (
	"encoding/pem"
	"errors"
	"testing"
)

// TestSOCertSPKIThumbprint_ParseError covers the ParseCertificate error branch:
// pem.Decode succeeds (the block is well-formed) but x509.ParseCertificate
// rejects the garbage DER, yielding a typed SOCertSPKIThumbprintError.
func TestSOCertSPKIThumbprint_ParseError(t *testing.T) {
	garbage := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("not-valid-der-bytes"),
	})

	_, err := SOCertSPKIThumbprint(garbage)
	if err == nil {
		t.Fatal("expected a parse error for a CERTIFICATE block with garbage DER")
	}
	var spkiErr *SOCertSPKIThumbprintError
	if !errors.As(err, &spkiErr) {
		t.Fatalf("expected *SOCertSPKIThumbprintError, got %T: %v", err, err)
	}
}
