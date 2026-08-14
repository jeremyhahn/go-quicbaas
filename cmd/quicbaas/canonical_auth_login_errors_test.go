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

// Package main: typed-error coverage for canonical_auth_login.go. Each
// Error/Unwrap pair is exercised here to pin both the human-readable
// diagnostic and the errors.Is/As chain.
package main

import (
	stderrors "errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMissingClientCertError_Message(t *testing.T) {
	e := &MissingClientCertError{}
	assert.Contains(t, e.Error(), "mtls")
	assert.Contains(t, e.Error(), "--client-cert")
}

func TestNoFactorsAvailableError_Message(t *testing.T) {
	e := &NoFactorsAvailableError{}
	assert.Contains(t, e.Error(), "no auth method")
}

func TestAmbiguousAutoDiscoveryError_Message(t *testing.T) {
	e := &AmbiguousAutoDiscoveryError{Methods: []string{"mtls", "piv"}}
	got := e.Error()
	assert.Contains(t, got, "mtls")
	assert.Contains(t, got, "piv")
	assert.Contains(t, got, "--method")
}

func TestInvalidPIVBackendError_Message(t *testing.T) {
	e := &InvalidPIVBackendError{Backend: "opgp"}
	got := e.Error()
	assert.Contains(t, got, "opgp")
	assert.Contains(t, got, "file | pkcs11 | tpm2")
}

func TestPIVLocalBackendUnsupportedError_Message(t *testing.T) {
	e := &PIVLocalBackendUnsupportedError{Backend: "tpm2", Reason: "no client-local TPM2 PIV storage"}
	assert.Contains(t, e.Error(), "tpm2")
	assert.Contains(t, e.Error(), "no client-local TPM2 PIV storage")
}

func TestPIVLocalSessionError_Message(t *testing.T) {
	cause := stderrors.New("boom")
	e := &PIVLocalSessionError{Op: "read certificate", Cause: cause}
	assert.Contains(t, e.Error(), "read certificate")
	assert.Contains(t, e.Error(), "boom")
	assert.ErrorIs(t, e, cause)
}

func TestPIVPINRequiredError_Message(t *testing.T) {
	e := &PIVPINRequiredError{}
	assert.Contains(t, e.Error(), "--pin")
	assert.Contains(t, e.Error(), "QUICBAAS_USER_PIN")
}

func TestFIDO2OperationError_Message(t *testing.T) {
	inner := stderrors.New("device removed")
	e := &FIDO2OperationError{Op: "assert", Cause: inner}
	assert.Contains(t, e.Error(), "assert")
	assert.Contains(t, e.Error(), "device removed")
	assert.ErrorIs(t, e, inner)
}

func TestPIVOperationError_Message(t *testing.T) {
	inner := stderrors.New("smartcard fault")
	e := &PIVOperationError{Op: "sign", Cause: inner}
	assert.Contains(t, e.Error(), "sign")
	assert.Contains(t, e.Error(), "smartcard fault")
	assert.ErrorIs(t, e, inner)
}

func TestCanonicalCertReadError_WithPath(t *testing.T) {
	inner := stderrors.New("EOF")
	e := &CanonicalCertReadError{Path: "/etc/cert.pem", Cause: inner}
	got := e.Error()
	assert.Contains(t, got, "/etc/cert.pem")
	assert.Contains(t, got, "EOF")
	assert.ErrorIs(t, e, inner)
}

func TestCanonicalCertReadError_NoPath(t *testing.T) {
	inner := stderrors.New("io: EOF")
	e := &CanonicalCertReadError{Cause: inner}
	got := e.Error()
	assert.Contains(t, got, "cert read error")
	assert.Contains(t, got, "io: EOF")
	assert.False(t, strings.Contains(got, "()"),
		"empty Path should not produce '()' in message")
}

func TestTokenWriteError_Message(t *testing.T) {
	inner := stderrors.New("disk full")
	e := &TokenWriteError{Path: "/tmp/tok", Cause: inner}
	assert.Contains(t, e.Error(), "/tmp/tok")
	assert.Contains(t, e.Error(), "disk full")
	assert.ErrorIs(t, e, inner)
}
