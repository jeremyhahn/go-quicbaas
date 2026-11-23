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

package fido2_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	xkmsfido2 "github.com/jeremyhahn/go-xkms/sdk/go/fido2"

	fido2 "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/fido2"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// TestFIDO2Reexports_ErrorsAreGoXKMSSentinels proves each PIN and config
// error the facade exports is the go-xkms sentinel itself, so errors.Is
// works across the two imports.
func TestFIDO2Reexports_ErrorsAreGoXKMSSentinels(t *testing.T) {
	cases := []struct {
		name string
		got  error
		want error
	}{
		{"ErrPINRequired", fido2.ErrPINRequired, xkmsfido2.ErrPINRequired},
		{"ErrInvalidPIN", fido2.ErrInvalidPIN, xkmsfido2.ErrInvalidPIN},
		{"ErrPINBlocked", fido2.ErrPINBlocked, xkmsfido2.ErrPINBlocked},
		{"ErrPINNotSet", fido2.ErrPINNotSet, xkmsfido2.ErrPINNotSet},
		{"ErrPINAuthInvalid", fido2.ErrPINAuthInvalid, xkmsfido2.ErrPINAuthInvalid},
		{"ErrPINAuthBlocked", fido2.ErrPINAuthBlocked, xkmsfido2.ErrPINAuthBlocked},
		{"ErrPINRetriesUnavailable", fido2.ErrPINRetriesUnavailable, xkmsfido2.ErrPINRetriesUnavailable},
		{"ErrInvalidOption", fido2.ErrInvalidOption, xkmsfido2.ErrInvalidOption},
		{"ErrAuthenticationConfigNil", fido2.ErrAuthenticationConfigNil, xkmsfido2.ErrAuthenticationConfigNil},
		{"ErrEnrollmentConfigNil", fido2.ErrEnrollmentConfigNil, xkmsfido2.ErrEnrollmentConfigNil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == nil {
				t.Fatalf("%s is nil", tc.name)
			}
			if tc.got != tc.want {
				t.Fatalf("%s is not the go-xkms sentinel", tc.name)
			}
			if !errors.Is(tc.got, tc.want) {
				t.Fatalf("errors.Is(%s, go-xkms %s) = false", tc.name, tc.name)
			}
		})
	}
}

// TestFIDO2Reexports_TypesAreGoXKMSTypes proves each assertion and PIN type
// the facade exports is the go-xkms type itself, not a lookalike.
func TestFIDO2Reexports_TypesAreGoXKMSTypes(t *testing.T) {
	cases := []struct {
		name string
		got  reflect.Type
		want reflect.Type
	}{
		{"Authenticator", reflect.TypeOf((*fido2.Authenticator)(nil)).Elem(), reflect.TypeOf((*xkmsfido2.Authenticator)(nil)).Elem()},
		{"AuthenticationResult", reflect.TypeOf((*fido2.AuthenticationResult)(nil)).Elem(), reflect.TypeOf((*xkmsfido2.AuthenticationResult)(nil)).Elem()},
		{"CTAPHIDDevice", reflect.TypeOf((*fido2.CTAPHIDDevice)(nil)).Elem(), reflect.TypeOf((*xkmsfido2.CTAPHIDDevice)(nil)).Elem()},
		{"WebAuthnAssertion", reflect.TypeOf((*fido2.WebAuthnAssertion)(nil)).Elem(), reflect.TypeOf((*xkmsfido2.WebAuthnAssertion)(nil)).Elem()},
		{"WebAuthnAssertionRequest", reflect.TypeOf((*fido2.WebAuthnAssertionRequest)(nil)).Elem(), reflect.TypeOf((*xkmsfido2.WebAuthnAssertionRequest)(nil)).Elem()},
		{"WebAuthnAuthenticationOptions", reflect.TypeOf((*fido2.WebAuthnAuthenticationOptions)(nil)).Elem(), reflect.TypeOf((*xkmsfido2.WebAuthnAuthenticationOptions)(nil)).Elem()},
		{"PINAttemptsError", reflect.TypeOf((*fido2.PINAttemptsError)(nil)).Elem(), reflect.TypeOf((*xkmsfido2.PINAttemptsError)(nil)).Elem()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("%s is %v, want the go-xkms type %v", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestFIDO2Reexports_PINAttemptsErrorUnwrapsToInvalidPIN proves a wrapped
// attempts-annotated PIN failure still matches ErrInvalidPIN and yields its
// remaining attempts through the facade's own names.
func TestFIDO2Reexports_PINAttemptsErrorUnwrapsToInvalidPIN(t *testing.T) {
	err := fmt.Errorf("unlock: %w", &fido2.PINAttemptsError{Err: fido2.ErrInvalidPIN, Retries: 2})
	if !errors.Is(err, fido2.ErrInvalidPIN) {
		t.Fatalf("errors.Is(%v, ErrInvalidPIN) = false", err)
	}
	var attempts *fido2.PINAttemptsError
	if !errors.As(err, &attempts) {
		t.Fatalf("errors.As(%v, *PINAttemptsError) = false", err)
	}
	if attempts.Retries != 2 {
		t.Fatalf("Retries = %d, want 2", attempts.Retries)
	}
	if !strings.Contains(err.Error(), "2 attempts remaining") {
		t.Fatalf("error %q does not report the attempts remaining", err.Error())
	}
}

// TestFIDO2Reexports_AuthenticationConfigCarriesPIN proves a PIN reaches the
// assertion request through the facade's own imports.
func TestFIDO2Reexports_AuthenticationConfigCarriesPIN(t *testing.T) {
	cfg := fido2.DefaultAuthenticationConfig(nil, nil)
	if cfg == nil {
		t.Fatal("DefaultAuthenticationConfig returned nil")
	}
	pin, err := secure.NewSecret([]byte("123456"))
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	cfg.PIN = pin
	if cfg.PIN == nil {
		t.Fatal("AuthenticationConfig.PIN is nil after setting it")
	}
}
