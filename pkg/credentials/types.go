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

// Package credentials provides dynamic credential generation for databases and services.
package credentials

import (
	"time"
)

// Credential represents a dynamically generated credential.
// Credentials are time-limited and associated with a lease.
type Credential struct {
	// CredentialID is the unique identifier for this credential.
	CredentialID string `json:"credential_id"`

	// Type identifies the credential type (e.g., "database", "certificate").
	Type string `json:"type"`

	// Data contains the actual credential information (username, password, token, etc.).
	// The structure varies by credential type.
	Data map[string]string `json:"data"`

	// LeaseID is the ID of the lease associated with this credential.
	LeaseID string `json:"lease_id"`

	// CreatedAt is the timestamp when the credential was generated.
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt is the timestamp when the credential expires.
	ExpiresAt time.Time `json:"expires_at"`
}

// Role defines a template for generating credentials.
// Roles specify the permissions, TTL, and configuration for generated credentials.
type Role struct {
	// Name is the unique identifier for this role.
	Name string `json:"name"`

	// GeneratorType identifies the credential generator to use (e.g., "postgres", "mysql", "certificate").
	GeneratorType string `json:"generator_type"`

	// Config contains generator-specific configuration.
	// For database generators: connection_url, creation_statements, revocation_statements
	// For certificate generators: ca_cert, key_type, key_bits, ttl
	Config map[string]string `json:"config"`

	// DefaultTTL is the default time-to-live for credentials generated from this role.
	DefaultTTL time.Duration `json:"default_ttl"`

	// MaxTTL is the maximum allowed time-to-live for credentials from this role.
	MaxTTL time.Duration `json:"max_ttl"`
}

// DatabaseCredential represents database-specific credential data.
type DatabaseCredential struct {
	// Username is the database username.
	Username string `json:"username"`

	// Password is the database password.
	Password string `json:"password"`

	// ConnectionURL is the complete connection URL (optional).
	ConnectionURL string `json:"connection_url,omitempty"`

	// Database is the database name (optional).
	Database string `json:"database,omitempty"`

	// Host is the database host (optional).
	Host string `json:"host,omitempty"`

	// Port is the database port (optional).
	Port int `json:"port,omitempty"`
}

// CertificateCredential represents certificate-based credential data.
type CertificateCredential struct {
	// Certificate is the PEM-encoded certificate.
	Certificate string `json:"certificate"`

	// PrivateKey is the PEM-encoded private key.
	PrivateKey string `json:"private_key"`

	// CACertificate is the PEM-encoded CA certificate (optional).
	CACertificate string `json:"ca_certificate,omitempty"`

	// SerialNumber is the certificate serial number.
	SerialNumber string `json:"serial_number,omitempty"`
}

// CredentialType constants for common credential types.
const (
	CredentialTypeDatabase    = "database"
	CredentialTypeCertificate = "certificate"
)
