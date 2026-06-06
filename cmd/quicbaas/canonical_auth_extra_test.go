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

// Package main: typed-error coverage for canonical_auth.go and
// canonical_auth_login.go. Each Error()/Unwrap() pair is exercised here
// to lock in the human-readable diagnostics and the errors.Is/As chain.
package main

import (
	stderrors "errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// canonical_auth.go: typed errors
// =============================================================================

func TestMissingFactorIDError_Message(t *testing.T) {
	e := &MissingFactorIDError{Op: "auth factor remove"}
	assert.Contains(t, e.Error(), "auth factor remove")
	assert.Contains(t, e.Error(), "factor id")
}

func TestMissingLabelError_Message(t *testing.T) {
	e := &MissingLabelError{Op: "auth factor relabel"}
	assert.Contains(t, e.Error(), "auth factor relabel")
	assert.Contains(t, e.Error(), "--label")
}

func TestInvalidFactorTypeError_Message(t *testing.T) {
	e := &InvalidFactorTypeError{Type: "totp"}
	assert.Contains(t, e.Error(), "totp")
	assert.Contains(t, e.Error(), "mtls")
	assert.Contains(t, e.Error(), "fido2")
	assert.Contains(t, e.Error(), "piv")
}

func TestInvalidAuthMethodError_Message(t *testing.T) {
	e := &InvalidAuthMethodError{Method: "saml"}
	assert.Contains(t, e.Error(), "saml")
	assert.Contains(t, e.Error(), "mtls")
}

func TestMissingCertPathError_Message(t *testing.T) {
	e := &MissingCertPathError{}
	assert.Contains(t, e.Error(), "mtls")
	assert.Contains(t, e.Error(), "--cert")
}

func TestCertReadError_WithPath(t *testing.T) {
	inner := stderrors.New("permission denied")
	e := &CertReadError{Path: "/etc/ca.pem", Cause: inner}
	assert.Contains(t, e.Error(), "/etc/ca.pem")
	assert.Contains(t, e.Error(), "permission denied")
	assert.ErrorIs(t, e, inner)
}

func TestCertReadError_NoPath(t *testing.T) {
	inner := stderrors.New("io: EOF")
	e := &CertReadError{Cause: inner}
	assert.Contains(t, e.Error(), "cert read error")
	assert.Contains(t, e.Error(), "io: EOF")
	assert.False(t, strings.Contains(e.Error(), "()"),
		"empty Path should not produce '()' in message")
}

func TestSecuritySurfaceUnavailableError_Message(t *testing.T) {
	e := &SecuritySurfaceUnavailableError{Op: "auth factor list"}
	assert.Contains(t, e.Error(), "auth factor list")
	assert.Contains(t, e.Error(), "Security()")
}

// =============================================================================
// canonical_auth.go: command tree shape
// =============================================================================

// TestNewCanonicalAuthFactorCommands_AllPresent confirms each builder
// returns a non-nil *cobra.Command with the expected Use string.
func TestNewCanonicalAuthFactorCommands_AllPresent(t *testing.T) {
	cases := []struct {
		name string
		want string
		cmd  any
	}{
		{"list", "list", newCanonicalAuthFactorListCmd()},
		{"add", "add", newCanonicalAuthFactorAddCmd()},
		{"remove", "remove", newCanonicalAuthFactorRemoveCmd()},
		{"rotate", "rotate", newCanonicalAuthFactorRotateCmd()},
		{"relabel", "relabel", newCanonicalAuthFactorRelabelCmd()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotNil(t, tc.cmd)
		})
	}
}

// TestCanonicalAuthFactorRemove_MissingArg verifies the typed
// MissingFactorIDError is returned when the positional arg is omitted.
func TestCanonicalAuthFactorRemove_MissingArg(t *testing.T) {
	cmd := newCanonicalAuthFactorRemoveCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected MissingFactorIDError, got nil")
	}
	var typed *MissingFactorIDError
	if !stderrors.As(err, &typed) {
		t.Fatalf("expected *MissingFactorIDError, got %T", err)
	}
	assert.Contains(t, typed.Op, "auth factor remove")
}

// TestCanonicalAuthFactorRotate_MissingArg verifies the typed
// MissingFactorIDError is returned for rotate when the positional arg
// is omitted.
func TestCanonicalAuthFactorRotate_MissingArg(t *testing.T) {
	cmd := newCanonicalAuthFactorRotateCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected MissingFactorIDError, got nil")
	}
	var typed *MissingFactorIDError
	if !stderrors.As(err, &typed) {
		t.Fatalf("expected *MissingFactorIDError, got %T", err)
	}
}

// TestCanonicalAuthFactorRelabel_MissingArg verifies the typed
// MissingFactorIDError is returned for relabel when the positional arg
// is omitted.
func TestCanonicalAuthFactorRelabel_MissingArg(t *testing.T) {
	cmd := newCanonicalAuthFactorRelabelCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected MissingFactorIDError, got nil")
	}
}
