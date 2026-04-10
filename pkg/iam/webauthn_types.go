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
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Credential represents a WebAuthn credential registered to a user.
// It contains all the information needed to verify WebAuthn assertions.
type WebAuthnCredential struct {
	// ID is the base64url-encoded credential ID
	ID string `json:"id" yaml:"id"`

	// UserID is the ID of the user who owns this credential
	UserID string `json:"user_id" yaml:"user_id"`

	// TenantID is the tenant this credential belongs to
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Name is a user-friendly name for the credential (e.g., "iPhone TouchID")
	Name string `json:"name" yaml:"name"`

	// PublicKey is the COSE-encoded public key
	PublicKey []byte `json:"public_key" yaml:"public_key"`

	// AttestationType is the attestation type used during registration
	// (none, indirect, direct)
	AttestationType string `json:"attestation_type" yaml:"attestation_type"`

	// AAGUID is the Authenticator Attestation GUID
	AAGUID []byte `json:"aaguid,omitempty" yaml:"aaguid,omitempty"`

	// SignCount is the signature counter value (for cloned device detection)
	SignCount uint32 `json:"sign_count" yaml:"sign_count"`

	// Transports indicates how the authenticator communicates
	// (usb, nfc, ble, internal)
	Transports []string `json:"transports,omitempty" yaml:"transports,omitempty"`

	// CreatedAt is when the credential was registered
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`

	// LastUsedAt is when the credential was last used for authentication
	LastUsedAt *time.Time `json:"last_used_at,omitempty" yaml:"last_used_at,omitempty"`
}

// WebAuthnChallenge represents a temporary challenge for registration or authentication.
// Challenges are stored with a TTL and deleted after use or expiration.
type WebAuthnChallenge struct {
	// ID is the unique challenge identifier
	ID string `json:"id" yaml:"id"`

	// UserID is the user this challenge is for
	UserID string `json:"user_id" yaml:"user_id"`

	// TenantID is the tenant this challenge belongs to
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Challenge is the random challenge bytes
	Challenge []byte `json:"challenge" yaml:"challenge"`

	// Type is the challenge type (registration, authentication)
	Type ChallengeType `json:"type" yaml:"type"`

	// SessionData contains additional session data for the challenge
	SessionData []byte `json:"session_data,omitempty" yaml:"session_data,omitempty"`

	// CreatedAt is when the challenge was created
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`

	// ExpiresAt is when the challenge expires
	ExpiresAt time.Time `json:"expires_at" yaml:"expires_at"`
}

// ChallengeType represents the type of WebAuthn challenge.
type ChallengeType string

const (
	// ChallengeTypeRegistration is for credential registration.
	ChallengeTypeRegistration ChallengeType = "registration"

	// ChallengeTypeAuthentication is for user authentication.
	ChallengeTypeAuthentication ChallengeType = "authentication"
)

// webauthnUser implements the webauthn.User interface required by the go-webauthn library.
// It wraps our User type and associated credentials.
type webauthnUser struct {
	user        *User
	credentials []*WebAuthnCredential
}

// WebAuthnID returns the user's ID as bytes for WebAuthn.
func (u *webauthnUser) WebAuthnID() []byte {
	return []byte(u.user.ID)
}

// WebAuthnName returns the user's email as the WebAuthn name.
func (u *webauthnUser) WebAuthnName() string {
	return u.user.Email
}

// WebAuthnDisplayName returns the user's display name.
func (u *webauthnUser) WebAuthnDisplayName() string {
	if u.user.Name != "" {
		return u.user.Name
	}
	return u.user.Email
}

// WebAuthnCredentials converts our credentials to the webauthn library format.
func (u *webauthnUser) WebAuthnCredentials() []webauthn.Credential {
	creds := make([]webauthn.Credential, len(u.credentials))
	for i, cred := range u.credentials {
		creds[i] = webauthn.Credential{
			ID:              []byte(cred.ID),
			PublicKey:       cred.PublicKey,
			AttestationType: cred.AttestationType,
			Authenticator: webauthn.Authenticator{
				AAGUID:       cred.AAGUID,
				SignCount:    cred.SignCount,
				CloneWarning: false,
			},
		}

		// Convert transport strings to protocol.AuthenticatorTransport
		if len(cred.Transports) > 0 {
			transports := make([]protocol.AuthenticatorTransport, 0, len(cred.Transports))
			for _, t := range cred.Transports {
				transports = append(transports, protocol.AuthenticatorTransport(t))
			}
			creds[i].Transport = transports
		}
	}
	return creds
}

// WebAuthnIcon returns an icon URL for the user (optional).
func (u *webauthnUser) WebAuthnIcon() string {
	return ""
}

// IsExpired checks if a challenge has expired.
func (c *WebAuthnChallenge) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

// Validate validates a WebAuthnCredential for correctness.
func (c *WebAuthnCredential) Validate() error {
	if c.ID == "" {
		return NewIAMError(ErrCredentialInvalid, "iam.credential.validate", "credential ID is required", nil)
	}
	if c.UserID == "" {
		return NewIAMError(ErrCredentialInvalid, "iam.credential.validate", "user ID is required", nil)
	}
	if c.TenantID == "" {
		return NewIAMError(ErrCredentialInvalid, "iam.credential.validate", "tenant ID is required", nil)
	}
	if len(c.PublicKey) == 0 {
		return NewIAMError(ErrCredentialInvalid, "iam.credential.validate", "public key is required", nil)
	}
	return nil
}

// Validate validates a WebAuthnChallenge for correctness.
func (c *WebAuthnChallenge) Validate() error {
	if c.ID == "" {
		return NewIAMError(ErrChallengeInvalid, "iam.challenge.validate", "challenge ID is required", nil)
	}
	if c.UserID == "" {
		return NewIAMError(ErrChallengeInvalid, "iam.challenge.validate", "user ID is required", nil)
	}
	if c.TenantID == "" {
		return NewIAMError(ErrChallengeInvalid, "iam.challenge.validate", "tenant ID is required", nil)
	}
	if len(c.Challenge) == 0 {
		return NewIAMError(ErrChallengeInvalid, "iam.challenge.validate", "challenge bytes are required", nil)
	}
	if c.Type != ChallengeTypeRegistration && c.Type != ChallengeTypeAuthentication {
		return NewIAMError(ErrChallengeInvalid, "iam.challenge.validate", "invalid challenge type", nil)
	}
	return nil
}
