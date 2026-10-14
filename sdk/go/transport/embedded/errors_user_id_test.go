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

package embedded

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestParseIAMUserID_AcceptsTheFullUint64Range(t *testing.T) {
	for _, want := range []uint64{1, 1 << 53, (1 << 53) + 1, math.MaxUint64} {
		got, err := parseIAMUserID("quicbaas.IAM.User", "Get", strconv.FormatUint(want, 10))
		if err != nil {
			t.Fatalf("parseIAMUserID(%d): %v", want, err)
		}
		if got != want {
			t.Errorf("parseIAMUserID = %d, want %d", got, want)
		}
	}
}

func TestParseIAMUserID_RefusesANonID(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "non-numeric", value: "user-1", wantErr: strconv.ErrSyntax},
		{name: "negative", value: "-1", wantErr: strconv.ErrSyntax},
		{name: "empty", value: "", wantErr: strconv.ErrSyntax},
		{name: "overflowing", value: "18446744073709551616", wantErr: strconv.ErrRange},
		{name: "zero", value: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIAMUserID("quicbaas.IAM.User", "Get", tt.value)
			if got != 0 {
				t.Errorf("parseIAMUserID = %d, want 0", got)
			}

			var idErr *InvalidUserIDError
			if !errors.As(err, &idErr) {
				t.Fatalf("error = %v, want *InvalidUserIDError", err)
			}
			if idErr.Service != "quicbaas.IAM.User" || idErr.Method != "Get" || idErr.Value != tt.value {
				t.Errorf("error fields = %+v, want the request's service, method and value", idErr)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want it to wrap %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && errors.Unwrap(err) != nil {
				t.Errorf("zero id error wraps %v, want nothing", errors.Unwrap(err))
			}
		})
	}
}

// TestEmbeddedWebAuthnRefusesANonNumericUserID proves the handler rejects a
// malformed id at the edge and never reaches the service.
func TestEmbeddedWebAuthnRefusesANonNumericUserID(t *testing.T) {
	stub := &stubWebAuthnCredentials{}
	svc := &mockQuicBaaSForWebAuthn{creds: stub}

	_, err := handleIAMWebAuthnCredentialsList(context.Background(), svc,
		&webAuthnListEnvelope{TenantID: "t1", UserID: "18446744073709551616"})

	var idErr *InvalidUserIDError
	if !errors.As(err, &idErr) {
		t.Fatalf("error = %v, want *InvalidUserIDError", err)
	}
	if stub.gotTenant != "" {
		t.Error("the service must not be called")
	}
}
