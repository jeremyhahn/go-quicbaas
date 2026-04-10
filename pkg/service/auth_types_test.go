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

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// MFAType.String Tests
// =============================================================================

func TestMFAType_String(t *testing.T) {
	tests := []struct {
		name    string
		mfaType MFAType
		want    string
	}{
		{
			name:    "TOTP type",
			mfaType: MFATypeTOTP,
			want:    "TOTP",
		},
		{
			name:    "WebAuthn type",
			mfaType: MFATypeWebAuthn,
			want:    "WEBAUTHN",
		},
		{
			name:    "SMS type",
			mfaType: MFATypeSMS,
			want:    "SMS",
		},
		{
			name:    "Email type",
			mfaType: MFATypeEmail,
			want:    "EMAIL",
		},
		{
			name:    "BackupCode type",
			mfaType: MFATypeBackupCode,
			want:    "BACKUP_CODE",
		},
		{
			name:    "unknown type returns UNKNOWN",
			mfaType: MFAType(999),
			want:    "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.mfaType.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// ProviderType.String Tests
// =============================================================================

func TestProviderType_String(t *testing.T) {
	tests := []struct {
		name         string
		providerType ProviderType
		want         string
	}{
		{
			name:         "OIDC provider",
			providerType: ProviderTypeOIDC,
			want:         "OIDC",
		},
		{
			name:         "SAML provider",
			providerType: ProviderTypeSAML,
			want:         "SAML",
		},
		{
			name:         "OAuth2 provider",
			providerType: ProviderTypeOAuth2,
			want:         "OAUTH2",
		},
		{
			name:         "unspecified provider returns UNSPECIFIED",
			providerType: ProviderTypeUnspecified,
			want:         "UNSPECIFIED",
		},
		{
			name:         "unknown provider returns UNSPECIFIED",
			providerType: ProviderType(999),
			want:         "UNSPECIFIED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.providerType.String()
			assert.Equal(t, tt.want, got)
		})
	}
}
