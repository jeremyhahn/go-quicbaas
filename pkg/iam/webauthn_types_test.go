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
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

func TestWebAuthnCredential_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cred    *WebAuthnCredential
		wantErr bool
	}{
		{
			name: "valid credential",
			cred: &WebAuthnCredential{
				ID:        "cred123",
				UserID:    "user123",
				TenantID:  "tenant123",
				PublicKey: []byte("publickey"),
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			cred: &WebAuthnCredential{
				UserID:    "user123",
				TenantID:  "tenant123",
				PublicKey: []byte("publickey"),
			},
			wantErr: true,
		},
		{
			name: "missing UserID",
			cred: &WebAuthnCredential{
				ID:        "cred123",
				TenantID:  "tenant123",
				PublicKey: []byte("publickey"),
			},
			wantErr: true,
		},
		{
			name: "missing TenantID",
			cred: &WebAuthnCredential{
				ID:        "cred123",
				UserID:    "user123",
				PublicKey: []byte("publickey"),
			},
			wantErr: true,
		},
		{
			name: "missing PublicKey",
			cred: &WebAuthnCredential{
				ID:       "cred123",
				UserID:   "user123",
				TenantID: "tenant123",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cred.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("WebAuthnCredential.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWebAuthnChallenge_Validate(t *testing.T) {
	tests := []struct {
		name      string
		challenge *WebAuthnChallenge
		wantErr   bool
	}{
		{
			name: "valid registration challenge",
			challenge: &WebAuthnChallenge{
				ID:        "challenge123",
				UserID:    "user123",
				TenantID:  "tenant123",
				Challenge: []byte("challenge"),
				Type:      ChallengeTypeRegistration,
			},
			wantErr: false,
		},
		{
			name: "valid authentication challenge",
			challenge: &WebAuthnChallenge{
				ID:        "challenge123",
				UserID:    "user123",
				TenantID:  "tenant123",
				Challenge: []byte("challenge"),
				Type:      ChallengeTypeAuthentication,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			challenge: &WebAuthnChallenge{
				UserID:    "user123",
				TenantID:  "tenant123",
				Challenge: []byte("challenge"),
				Type:      ChallengeTypeRegistration,
			},
			wantErr: true,
		},
		{
			name: "missing UserID",
			challenge: &WebAuthnChallenge{
				ID:        "challenge123",
				TenantID:  "tenant123",
				Challenge: []byte("challenge"),
				Type:      ChallengeTypeRegistration,
			},
			wantErr: true,
		},
		{
			name: "missing TenantID",
			challenge: &WebAuthnChallenge{
				ID:        "challenge123",
				UserID:    "user123",
				Challenge: []byte("challenge"),
				Type:      ChallengeTypeRegistration,
			},
			wantErr: true,
		},
		{
			name: "missing Challenge",
			challenge: &WebAuthnChallenge{
				ID:       "challenge123",
				UserID:   "user123",
				TenantID: "tenant123",
				Type:     ChallengeTypeRegistration,
			},
			wantErr: true,
		},
		{
			name: "invalid Type",
			challenge: &WebAuthnChallenge{
				ID:        "challenge123",
				UserID:    "user123",
				TenantID:  "tenant123",
				Challenge: []byte("challenge"),
				Type:      "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.challenge.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("WebAuthnChallenge.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWebAuthnChallenge_IsExpired(t *testing.T) {
	tests := []struct {
		name      string
		challenge *WebAuthnChallenge
		want      bool
	}{
		{
			name: "not expired",
			challenge: &WebAuthnChallenge{
				ExpiresAt: time.Now().Add(1 * time.Hour),
			},
			want: false,
		},
		{
			name: "expired",
			challenge: &WebAuthnChallenge{
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			},
			want: true,
		},
		{
			name: "expires exactly now",
			challenge: &WebAuthnChallenge{
				ExpiresAt: time.Now(),
			},
			// This may be flaky due to timing, but generally should be expired
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.challenge.IsExpired(); got != tt.want {
				t.Errorf("WebAuthnChallenge.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebAuthnUser_Implementation(t *testing.T) {
	user := &User{
		ID:    "user123",
		Email: "test@example.com",
		Name:  "Test User",
	}

	credentials := []*WebAuthnCredential{
		{
			ID:              "cred123",
			PublicKey:       []byte("publickey"),
			AttestationType: "none",
			AAGUID:          []byte("aaguid"),
			SignCount:       0,
			Transports:      []string{"usb", "nfc"},
		},
	}

	waUser := &webauthnUser{
		user:        user,
		credentials: credentials,
	}

	// Test WebAuthnID
	if string(waUser.WebAuthnID()) != user.ID {
		t.Errorf("WebAuthnID() = %s, want %s", string(waUser.WebAuthnID()), user.ID)
	}

	// Test WebAuthnName
	if waUser.WebAuthnName() != user.Email {
		t.Errorf("WebAuthnName() = %s, want %s", waUser.WebAuthnName(), user.Email)
	}

	// Test WebAuthnDisplayName
	if waUser.WebAuthnDisplayName() != user.Name {
		t.Errorf("WebAuthnDisplayName() = %s, want %s", waUser.WebAuthnDisplayName(), user.Name)
	}

	// Test WebAuthnDisplayName when name is empty
	userNoName := &User{
		ID:    "user123",
		Email: "test@example.com",
	}
	waUserNoName := &webauthnUser{
		user: userNoName,
	}
	if waUserNoName.WebAuthnDisplayName() != userNoName.Email {
		t.Errorf("WebAuthnDisplayName() = %s, want %s (fallback to email)", waUserNoName.WebAuthnDisplayName(), userNoName.Email)
	}

	// Test WebAuthnCredentials
	waCreds := waUser.WebAuthnCredentials()
	if len(waCreds) != len(credentials) {
		t.Errorf("WebAuthnCredentials() returned %d credentials, want %d", len(waCreds), len(credentials))
	}

	if len(waCreds) > 0 {
		// Verify credential conversion
		if string(waCreds[0].ID) != credentials[0].ID {
			t.Errorf("WebAuthnCredentials()[0].ID = %s, want %s", string(waCreds[0].ID), credentials[0].ID)
		}
		if string(waCreds[0].PublicKey) != string(credentials[0].PublicKey) {
			t.Errorf("WebAuthnCredentials()[0].PublicKey = %v, want %v", waCreds[0].PublicKey, credentials[0].PublicKey)
		}
		if waCreds[0].AttestationType != credentials[0].AttestationType {
			t.Errorf("WebAuthnCredentials()[0].AttestationType = %s, want %s", waCreds[0].AttestationType, credentials[0].AttestationType)
		}
		if len(waCreds[0].Transport) != len(credentials[0].Transports) {
			t.Errorf("WebAuthnCredentials()[0].Transport has %d items, want %d", len(waCreds[0].Transport), len(credentials[0].Transports))
		}
		if len(waCreds[0].Transport) == 2 {
			if waCreds[0].Transport[0] != protocol.AuthenticatorTransport(credentials[0].Transports[0]) {
				t.Errorf("WebAuthnCredentials()[0].Transport[0] = %s, want %s", waCreds[0].Transport[0], credentials[0].Transports[0])
			}
		}
	}

	// Test WebAuthnIcon
	if waUser.WebAuthnIcon() != "" {
		t.Errorf("WebAuthnIcon() = %s, want empty string", waUser.WebAuthnIcon())
	}
}

func TestWebAuthnUser_CredentialsConversion(t *testing.T) {
	tests := []struct {
		name        string
		credentials []*WebAuthnCredential
		wantCount   int
	}{
		{
			name:        "no credentials",
			credentials: []*WebAuthnCredential{},
			wantCount:   0,
		},
		{
			name: "single credential with transports",
			credentials: []*WebAuthnCredential{
				{
					ID:         "cred1",
					PublicKey:  []byte("pk1"),
					Transports: []string{"usb", "nfc"},
				},
			},
			wantCount: 1,
		},
		{
			name: "multiple credentials",
			credentials: []*WebAuthnCredential{
				{
					ID:        "cred1",
					PublicKey: []byte("pk1"),
				},
				{
					ID:        "cred2",
					PublicKey: []byte("pk2"),
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			waUser := &webauthnUser{
				user: &User{
					ID:    "user123",
					Email: "test@example.com",
				},
				credentials: tt.credentials,
			}

			waCreds := waUser.WebAuthnCredentials()
			if len(waCreds) != tt.wantCount {
				t.Errorf("WebAuthnCredentials() returned %d credentials, want %d", len(waCreds), tt.wantCount)
			}
		})
	}
}
