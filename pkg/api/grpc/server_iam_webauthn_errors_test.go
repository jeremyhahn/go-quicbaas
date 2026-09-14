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

package grpc

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestConvertWebAuthnCredentialError_MapsEachCredentialIDShape pins the gRPC
// half of the go-quicbaas#574 contract: the three ways a credential id can be
// wrong must be distinguishable on this wire the same way they are over REST,
// because the CLI's default transport is gRPC and an operator debugging a
// stale passkey there gets only the status code and message.
func TestConvertWebAuthnCredentialError_MapsEachCredentialIDShape(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{
			name:     "truncated id is invalid argument naming the full id",
			err:      &iam.WebAuthnCredentialTruncatedIDError{CredentialID: "abcd", Matches: []string{"abcdefgh"}},
			wantCode: codes.InvalidArgument,
			wantMsg:  "the full id is abcdefgh",
		},
		{
			name:     "unknown id is not found",
			err:      &iam.WebAuthnCredentialNotFoundError{Email: "alice@example.com", CredentialID: "abcdefgh"},
			wantCode: codes.NotFound,
			wantMsg:  "no credential abcdefgh is registered to alice@example.com",
		},
		{
			name:     "malformed id is invalid argument",
			err:      &iam.WebAuthnCredentialIDError{CredentialID: "!!!"},
			wantCode: codes.InvalidArgument,
			wantMsg:  "not valid base64url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, ok := status.FromError(convertWebAuthnCredentialError(tc.err))
			if !ok {
				t.Fatalf("converted error is not a gRPC status: %v", tc.err)
			}
			if st.Code() != tc.wantCode {
				t.Errorf("code = %s, want %s", st.Code(), tc.wantCode)
			}
			if !strings.Contains(st.Message(), tc.wantMsg) {
				t.Errorf("message %q does not contain %q", st.Message(), tc.wantMsg)
			}
		})
	}
}

// TestConvertWebAuthnCredentialError_FallsThroughForOtherErrors covers the
// error-handling side: an error that is none of the credential-id shapes must
// reach the shared IAM conversion rather than being mislabelled as a bad id.
func TestConvertWebAuthnCredentialError_FallsThroughForOtherErrors(t *testing.T) {
	st, ok := status.FromError(convertWebAuthnCredentialError(webAuthnCredentialStoreFailureError{}))
	if !ok {
		t.Fatal("converted error is not a gRPC status")
	}
	if st.Code() == codes.NotFound || st.Code() == codes.InvalidArgument {
		t.Errorf("an unrelated store failure was mapped to %s", st.Code())
	}
	if st.Message() == "" {
		t.Error("converted error carries no message")
	}
}

// webAuthnCredentialStoreFailureError is a store failure unrelated to
// credential ids.
type webAuthnCredentialStoreFailureError struct{}

// Error implements error.
func (webAuthnCredentialStoreFailureError) Error() string {
	return "credential store is unreachable"
}
