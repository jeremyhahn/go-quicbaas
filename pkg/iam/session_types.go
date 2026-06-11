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
)

// Session represents an authenticated user session with enhanced security features.
type SessionExt struct {
	// ID is the unique session identifier
	ID string `json:"id"`

	// UserID is the authenticated user
	UserID string `json:"user_id"`

	// TenantID is the session tenant
	TenantID string `json:"tenant_id"`

	// TokenHash is the SHA256 hash of the access token
	TokenHash string `json:"token_hash"`

	// RefreshHash is the SHA256 hash of the refresh token
	RefreshHash string `json:"refresh_hash"`

	// MFAVerified indicates whether MFA was verified for this session
	MFAVerified bool `json:"mfa_verified"`

	// DeviceInfo contains information about the device used
	DeviceInfo *DeviceInfo `json:"device_info,omitempty"`

	// IPAddress is the client IP address
	IPAddress string `json:"ip_address"`

	// UserAgent is the client user agent
	UserAgent string `json:"user_agent"`

	// Created is the session creation timestamp
	Created time.Time `json:"created"`

	// LastActive is the last activity timestamp
	LastActive time.Time `json:"last_active"`

	// ExpiresAt is the absolute session expiration time
	ExpiresAt time.Time `json:"expires_at"`

	// Metadata contains additional session metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DeviceInfo contains information about the authenticated device.
type DeviceInfo struct {
	// ID is the unique device identifier
	ID string `json:"id"`

	// Name is the human-readable device name
	Name string `json:"name"`

	// Type is the device type (browser, mobile, api)
	Type string `json:"type"`

	// Fingerprint is a unique device fingerprint
	Fingerprint string `json:"fingerprint"`

	// Trusted indicates whether the device is trusted
	Trusted bool `json:"trusted"`

	// TrustedUntil is when the trusted status expires
	TrustedUntil *time.Time `json:"trusted_until,omitempty"`
}

// DeviceType constants for device types.
const (
	DeviceTypeBrowser = "browser"
	DeviceTypeMobile  = "mobile"
	DeviceTypeAPI     = "api"
	DeviceTypeDesktop = "desktop"
)

// SessionPolicy defines tenant-level session requirements.
type SessionPolicy struct {
	// MaxConcurrent is the maximum number of concurrent sessions per user (0 = unlimited)
	MaxConcurrent int `json:"max_concurrent" yaml:"max_concurrent"`

	// IdleTimeout is the session idle timeout duration
	IdleTimeout time.Duration `json:"idle_timeout" yaml:"idle_timeout"`

	// AbsoluteTimeout is the absolute session timeout duration
	AbsoluteTimeout time.Duration `json:"absolute_timeout" yaml:"absolute_timeout"`

	// RequireMFA indicates whether MFA is required for sessions
	RequireMFA bool `json:"require_mfa" yaml:"require_mfa"`

	// RememberDeviceDays allows skipping MFA for trusted devices
	RememberDeviceDays int `json:"remember_device_days,omitempty" yaml:"remember_device_days,omitempty"`
}

// IsExpired checks if a session has expired.
func (s *SessionExt) IsExpired() bool {
	now := time.Now()
	return now.After(s.ExpiresAt)
}

// IsIdle checks if a session is idle based on policy.
func (s *SessionExt) IsIdle(policy *SessionPolicy) bool {
	if policy == nil || policy.IdleTimeout == 0 {
		return false
	}
	now := time.Now()
	return now.Sub(s.LastActive) > policy.IdleTimeout
}

// Validate validates a SessionExt for correctness.
func (s *SessionExt) Validate() error {
	if s.ID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.validate", "session ID is required", nil)
	}
	if s.UserID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.validate", "user ID is required", nil)
	}
	if s.TenantID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.validate", "tenant ID is required", nil)
	}
	if s.TokenHash == "" {
		return NewIAMError(ErrSessionInvalid, "iam.session.validate", "token hash is required", nil)
	}
	return nil
}

// Validate validates a SessionPolicy for correctness.
func (p *SessionPolicy) Validate() error {
	if p.MaxConcurrent < 0 {
		return NewIAMError(ErrSessionInvalid, "iam.session_policy.validate", "max_concurrent cannot be negative", nil)
	}
	if p.IdleTimeout < 0 {
		return NewIAMError(ErrSessionInvalid, "iam.session_policy.validate", "idle_timeout cannot be negative", nil)
	}
	if p.AbsoluteTimeout < 0 {
		return NewIAMError(ErrSessionInvalid, "iam.session_policy.validate", "absolute_timeout cannot be negative", nil)
	}
	if p.RememberDeviceDays < 0 {
		return NewIAMError(ErrSessionInvalid, "iam.session_policy.validate", "remember_device_days cannot be negative", nil)
	}
	return nil
}

// ApplyDefaults applies default values to the session policy.
func (p *SessionPolicy) ApplyDefaults() {
	if p.IdleTimeout == 0 {
		p.IdleTimeout = 30 * time.Minute
	}
	if p.AbsoluteTimeout == 0 {
		p.AbsoluteTimeout = 24 * time.Hour
	}
	if p.MaxConcurrent == 0 {
		p.MaxConcurrent = 5 // Default: 5 concurrent sessions
	}
}

// Validate validates a DeviceInfo for correctness.
func (d *DeviceInfo) Validate() error {
	if d.ID == "" {
		return NewIAMError(ErrSessionInvalid, "iam.device_info.validate", "device ID is required", nil)
	}
	if d.Type == "" {
		return NewIAMError(ErrSessionInvalid, "iam.device_info.validate", "device type is required", nil)
	}
	switch d.Type {
	case DeviceTypeBrowser, DeviceTypeMobile, DeviceTypeAPI, DeviceTypeDesktop:
		// Valid types
	default:
		return NewIAMError(ErrSessionInvalid, "iam.device_info.validate", "invalid device type", nil)
	}
	return nil
}
