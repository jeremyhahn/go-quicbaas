//go:build iam

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

package iam

import (
	"encoding/json"
	"testing"
	"time"
)

// Test UnixTime UnmarshalJSON and MarshalJSON
func TestUnixTime_MarshalJSON(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC)
	ut := NewUnixTime(now)

	data, err := json.Marshal(ut)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Should marshal to Unix timestamp (number)
	expectedUnix := now.Unix()
	var result int64
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result != expectedUnix {
		t.Errorf("MarshalJSON() = %d, want %d", result, expectedUnix)
	}
}

func TestUnixTime_UnmarshalJSON_Timestamp(t *testing.T) {
	// Test unmarshaling from Unix timestamp (number)
	timestamp := int64(1705319445) // 2024-01-15 12:30:45 UTC
	jsonData := []byte(`1705319445`)

	var ut UnixTime
	if err := json.Unmarshal(jsonData, &ut); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	if ut.Unix() != timestamp {
		t.Errorf("UnmarshalJSON() Unix() = %d, want %d", ut.Unix(), timestamp)
	}
}

func TestUnixTime_UnmarshalJSON_RFC3339(t *testing.T) {
	// Test unmarshaling from RFC3339 string
	timeStr := "2024-01-15T12:30:45Z"
	jsonData := []byte(`"` + timeStr + `"`)

	var ut UnixTime
	if err := json.Unmarshal(jsonData, &ut); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	expected, _ := time.Parse(time.RFC3339, timeStr)
	if ut.Unix() != expected.Unix() {
		t.Errorf("UnmarshalJSON() Unix() = %d, want %d", ut.Unix(), expected.Unix())
	}
}

func TestUnixTime_UnmarshalJSON_InvalidString(t *testing.T) {
	// Test with invalid RFC3339 string
	jsonData := []byte(`"not-a-valid-time"`)

	var ut UnixTime
	err := json.Unmarshal(jsonData, &ut)
	if err == nil {
		t.Error("UnmarshalJSON() expected error for invalid time string, got nil")
	}
}

func TestUnixTime_UnmarshalJSON_InvalidFormat(t *testing.T) {
	// Test with invalid JSON format (not number or string)
	jsonData := []byte(`true`)

	var ut UnixTime
	err := json.Unmarshal(jsonData, &ut)
	if err == nil {
		t.Error("UnmarshalJSON() expected error for invalid format, got nil")
	}
}

func TestUnixTime_UnmarshalJSON_Zero(t *testing.T) {
	// Test with zero timestamp
	jsonData := []byte(`0`)

	var ut UnixTime
	if err := json.Unmarshal(jsonData, &ut); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	if ut.Unix() != 0 {
		t.Errorf("UnmarshalJSON() Unix() = %d, want 0", ut.Unix())
	}
}

func TestUnixTime_RoundTrip(t *testing.T) {
	// Test marshaling and unmarshaling produces the same value
	now := time.Date(2024, 3, 15, 10, 20, 30, 0, time.UTC)
	original := NewUnixTime(now)

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal
	var result UnixTime
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Compare Unix timestamps (ignore nanoseconds as they're not preserved)
	if result.Unix() != original.Unix() {
		t.Errorf("Round-trip Unix() = %d, want %d", result.Unix(), original.Unix())
	}
}

func TestAuthCodeData_IsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "not expired",
			expiresAt: time.Now().Add(10 * time.Minute),
			want:      false,
		},
		{
			name:      "expired",
			expiresAt: time.Now().Add(-10 * time.Minute),
			want:      true,
		},
		{
			name:      "just expired",
			expiresAt: time.Now().Add(-1 * time.Second),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authCode := &AuthCodeData{
				ExpiresAt: tt.expiresAt,
			}
			if got := authCode.IsExpired(); got != tt.want {
				t.Errorf("AuthCodeData.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthCodeData_ValidatePKCE(t *testing.T) {
	validChallenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	validVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	tests := []struct {
		name                string
		codeChallenge       string
		codeChallengeMethod string
		verifier            string
		wantErr             bool
		checkErrType        bool // Whether to check for specific error type
		expectedErr         error
	}{
		{
			name:                "no PKCE required",
			codeChallenge:       "",
			codeChallengeMethod: "",
			verifier:            "",
			wantErr:             false,
		},
		{
			name:                "valid S256",
			codeChallenge:       validChallenge,
			codeChallengeMethod: CodeChallengeMethodS256,
			verifier:            validVerifier,
			wantErr:             false,
		},
		{
			name:                "plain method rejected per OAuth 2.1",
			codeChallenge:       "test-challenge",
			codeChallengeMethod: CodeChallengeMethodPlain,
			verifier:            "test-challenge",
			wantErr:             true,
			checkErrType:        true,
			expectedErr:         ErrPKCEPlainMethodNotAllowed,
		},
		{
			name:                "invalid verifier for S256",
			codeChallenge:       validChallenge,
			codeChallengeMethod: CodeChallengeMethodS256,
			verifier:            "wrong-verifier",
			wantErr:             true,
		},
		{
			name:                "plain method rejected even with wrong verifier",
			codeChallenge:       "test-challenge",
			codeChallengeMethod: CodeChallengeMethodPlain,
			verifier:            "wrong-verifier",
			wantErr:             true,
			checkErrType:        true,
			expectedErr:         ErrPKCEPlainMethodNotAllowed,
		},
		{
			name:                "missing verifier when required",
			codeChallenge:       validChallenge,
			codeChallengeMethod: CodeChallengeMethodS256,
			verifier:            "",
			wantErr:             true,
		},
		{
			name:                "unsupported method",
			codeChallenge:       "some-challenge",
			codeChallengeMethod: "unknown",
			verifier:            "some-verifier",
			wantErr:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authCode := &AuthCodeData{
				CodeChallenge:       tt.codeChallenge,
				CodeChallengeMethod: tt.codeChallengeMethod,
			}
			err := authCode.ValidatePKCE(tt.verifier)
			if (err != nil) != tt.wantErr {
				t.Errorf("AuthCodeData.ValidatePKCE() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.checkErrType && err != tt.expectedErr {
				t.Errorf("AuthCodeData.ValidatePKCE() error = %v, want %v", err, tt.expectedErr)
			}
		})
	}
}

// Test that plain method is consistently rejected in ValidatePKCE
func TestAuthCodeData_ValidatePKCE_PlainMethodRejected(t *testing.T) {
	authCode := &AuthCodeData{
		CodeChallenge:       "test-challenge",
		CodeChallengeMethod: CodeChallengeMethodPlain,
	}

	err := authCode.ValidatePKCE("test-challenge")

	if err == nil {
		t.Fatal("ValidatePKCE() should reject plain method per OAuth 2.1")
	}

	if err != ErrPKCEPlainMethodNotAllowed {
		t.Errorf("ValidatePKCE() error = %v, want ErrPKCEPlainMethodNotAllowed", err)
	}

	// Verify the error message is informative
	oauthErr, ok := err.(*OAuthError)
	if !ok {
		t.Fatalf("Error should be *OAuthError, got %T", err)
	}

	if oauthErr.ErrorCode != ErrOAuthInvalidRequest {
		t.Errorf("Error code = %s, want %s", oauthErr.ErrorCode, ErrOAuthInvalidRequest)
	}
}

func TestOAuthError_Error(t *testing.T) {
	tests := []struct {
		name             string
		errorCode        string
		errorDescription string
		want             string
	}{
		{
			name:             "with description",
			errorCode:        ErrOAuthInvalidRequest,
			errorDescription: "Missing client_id parameter",
			want:             "invalid_request: Missing client_id parameter",
		},
		{
			name:             "without description",
			errorCode:        ErrOAuthInvalidClient,
			errorDescription: "",
			want:             "invalid_client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &OAuthError{
				ErrorCode:        tt.errorCode,
				ErrorDescription: tt.errorDescription,
			}
			if got := e.Error(); got != tt.want {
				t.Errorf("OAuthError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewOAuthError(t *testing.T) {
	code := ErrOAuthInvalidGrant
	description := "Authorization code expired"

	err := NewOAuthError(code, description)

	if err.ErrorCode != code {
		t.Errorf("NewOAuthError().ErrorCode = %v, want %v", err.ErrorCode, code)
	}
	if err.ErrorDescription != description {
		t.Errorf("NewOAuthError().ErrorDescription = %v, want %v", err.ErrorDescription, description)
	}
}

func TestOAuthError_WithState(t *testing.T) {
	state := "test-state-123"
	err := NewOAuthError(ErrOAuthAccessDenied, "User denied access").WithState(state)

	if err.State != state {
		t.Errorf("WithState() State = %v, want %v", err.State, state)
	}
}

func TestOAuthError_WithURI(t *testing.T) {
	uri := "https://docs.example.com/errors#access_denied"
	err := NewOAuthError(ErrOAuthAccessDenied, "User denied access").WithURI(uri)

	if err.ErrorURI != uri {
		t.Errorf("WithURI() ErrorURI = %v, want %v", err.ErrorURI, uri)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Field:   "email",
		Message: "email is required",
	}

	want := "validation error: email: email is required"
	if got := err.Error(); got != want {
		t.Errorf("ValidationError.Error() = %v, want %v", got, want)
	}
}

// Test Claims marshaling/unmarshaling
func TestClaims_MarshalUnmarshal(t *testing.T) {
	now := time.Now()
	original := &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-123",
		Audience:  []string{"client-123"},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "token-jti-123",
		TenantID:  "tenant-1",
		Roles:     []string{"admin", "user"},
		Groups:    []string{"developers"},
		Email:     "test@example.com",
		Name:      "Test User",
		Scope:     "openid profile email",
		ClientID:  "client-123",
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal
	var result Claims
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Compare fields
	if result.Issuer != original.Issuer {
		t.Errorf("Issuer = %v, want %v", result.Issuer, original.Issuer)
	}
	if result.Subject != original.Subject {
		t.Errorf("Subject = %v, want %v", result.Subject, original.Subject)
	}
	if result.ExpiresAt.Unix() != original.ExpiresAt.Unix() {
		t.Errorf("ExpiresAt = %v, want %v", result.ExpiresAt.Unix(), original.ExpiresAt.Unix())
	}
}

// Test IDTokenClaims marshaling/unmarshaling
func TestIDTokenClaims_MarshalUnmarshal(t *testing.T) {
	now := time.Now()
	original := &IDTokenClaims{
		Issuer:        "https://test.example.com",
		Subject:       "user-123",
		Audience:      []string{"client-123"},
		ExpiresAt:     NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:      NewUnixTime(now),
		AuthTime:      NewUnixTime(now),
		Nonce:         "nonce-123",
		Email:         "test@example.com",
		EmailVerified: true,
		Name:          "Test User",
		TenantID:      "tenant-1",
		Roles:         []string{"admin"},
		Groups:        []string{"developers"},
		MFAVerified:   true,
		AuthMethod:    "webauthn",
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal
	var result IDTokenClaims
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Compare fields
	if result.Nonce != original.Nonce {
		t.Errorf("Nonce = %v, want %v", result.Nonce, original.Nonce)
	}
	if result.EmailVerified != original.EmailVerified {
		t.Errorf("EmailVerified = %v, want %v", result.EmailVerified, original.EmailVerified)
	}
	if result.MFAVerified != original.MFAVerified {
		t.Errorf("MFAVerified = %v, want %v", result.MFAVerified, original.MFAVerified)
	}
}

// Test NewUnixTime
func TestNewUnixTime(t *testing.T) {
	now := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	ut := NewUnixTime(now)

	if ut.Unix() != now.Unix() {
		t.Errorf("NewUnixTime() Unix() = %d, want %d", ut.Unix(), now.Unix())
	}
}

// Test UnixTime edge cases
func TestUnixTime_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
		wantErr  bool
	}{
		{
			name:     "negative timestamp",
			jsonData: "-1",
			wantErr:  false,
		},
		{
			name:     "very large timestamp",
			jsonData: "9999999999",
			wantErr:  false,
		},
		{
			name:     "object instead of number or string",
			jsonData: `{"time":"2024-01-15"}`,
			wantErr:  true,
		},
		{
			name:     "array instead of number or string",
			jsonData: `[1705319445]`,
			wantErr:  true,
		},
		{
			name:     "null value",
			jsonData: `null`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ut UnixTime
			err := json.Unmarshal([]byte(tt.jsonData), &ut)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
