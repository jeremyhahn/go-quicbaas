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

// Package database provides database credential generators.
package database

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// DefaultPasswordLength is the default length for generated passwords.
	DefaultPasswordLength = 32

	// DefaultUsernamePrefix is the default prefix for generated usernames.
	DefaultUsernamePrefix = "v-"
)

var (
	// ErrMissingConnectionURL is returned when connection_url is not configured.
	ErrMissingConnectionURL = stderrors.New("connection_url is required in role config")

	// ErrMissingCreationStatements is returned when creation_statements are not configured.
	ErrMissingCreationStatements = stderrors.New("creation_statements is required in role config")
)

// BaseGenerator provides common functionality for database credential generators.
// It handles username/password generation and configuration validation.
type BaseGenerator struct {
	logger interfaces.Logger
}

// NewBaseGenerator creates a new base database credential generator.
func NewBaseGenerator(logger interfaces.Logger) *BaseGenerator {
	return &BaseGenerator{
		logger: logger.WithField("component", "db_credential_generator"),
	}
}

// ValidateConfig validates common database configuration fields.
func (g *BaseGenerator) ValidateConfig(config map[string]string) error {
	if config == nil {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "BaseGenerator.ValidateConfig",
			Err:  stderrors.New("config cannot be nil"),
		}
	}

	if config["connection_url"] == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "BaseGenerator.ValidateConfig",
			Err:  ErrMissingConnectionURL,
		}
	}

	if config["creation_statements"] == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "BaseGenerator.ValidateConfig",
			Err:  ErrMissingCreationStatements,
		}
	}

	return nil
}

// GenerateUsername creates a random database username with a timestamp.
func (g *BaseGenerator) GenerateUsername(prefix string) (string, error) {
	if prefix == "" {
		prefix = DefaultUsernamePrefix
	}

	// Generate random suffix
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "BaseGenerator.GenerateUsername",
			Err:  &api.WrappedMessageError{Message: "failed to generate random bytes", Cause: err},
		}
	}

	// Include timestamp for uniqueness and debugging
	timestamp := time.Now().Unix()
	username := fmt.Sprintf("%s%d-%s", prefix, timestamp, hex.EncodeToString(bytes))

	return username, nil
}

// GeneratePassword creates a cryptographically secure random password.
func (g *BaseGenerator) GeneratePassword(length int) (string, error) {
	if length <= 0 {
		length = DefaultPasswordLength
	}

	// Generate random bytes
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "BaseGenerator.GeneratePassword",
			Err:  &api.WrappedMessageError{Message: "failed to generate random password", Cause: err},
		}
	}

	// Base64 encode for a readable password
	password := base64.URLEncoding.EncodeToString(bytes)

	// Trim to desired length
	if len(password) > length {
		password = password[:length]
	}

	return password, nil
}

// GenerateCredentialID creates a unique credential identifier.
func (g *BaseGenerator) GenerateCredentialID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "BaseGenerator.GenerateCredentialID",
			Err:  &api.WrappedMessageError{Message: "failed to generate credential ID", Cause: err},
		}
	}
	return "cred_" + hex.EncodeToString(bytes), nil
}

// SubstituteParameters replaces template variables in SQL statements.
// Supported variables: {{username}}, {{password}}, {{expiration}}.
func (g *BaseGenerator) SubstituteParameters(statement, username, password string, expiration time.Time) string {
	statement = strings.ReplaceAll(statement, "{{username}}", username)
	statement = strings.ReplaceAll(statement, "{{password}}", password)
	statement = strings.ReplaceAll(statement, "{{expiration}}", expiration.Format(time.RFC3339))
	return statement
}

// ParseStatements splits a multi-statement SQL string into individual statements.
func (g *BaseGenerator) ParseStatements(statements string) []string {
	// Split by semicolon and trim whitespace
	parts := strings.Split(statements, ";")
	result := make([]string, 0, len(parts))

	for _, stmt := range parts {
		stmt = strings.TrimSpace(stmt)
		if stmt != "" {
			result = append(result, stmt)
		}
	}

	return result
}

// BuildConnectionURL constructs a connection URL with credentials.
func (g *BaseGenerator) BuildConnectionURL(baseURL, username, password string) string {
	// This is a simplified implementation
	// Real implementations should properly parse and reconstruct the URL
	if strings.Contains(baseURL, "@") {
		// URL already has credentials, replace them
		parts := strings.SplitN(baseURL, "@", 2)
		if len(parts) == 2 {
			protoParts := strings.SplitN(parts[0], "//", 2)
			if len(protoParts) == 2 {
				return fmt.Sprintf("%s//%s:%s@%s", protoParts[0], username, password, parts[1])
			}
		}
	} else {
		// URL has no credentials, add them
		parts := strings.SplitN(baseURL, "//", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("%s//%s:%s@%s", parts[0], username, password, parts[1])
		}
	}

	return baseURL
}

// DBExecutor is an interface for executing database operations.
// This allows for testing without real database connections.
type DBExecutor interface {
	// ExecContext executes a SQL statement with context.
	ExecContext(ctx context.Context, query string, args ...interface{}) error

	// Close closes the database connection.
	Close() error
}

// DBConnector is a function type for creating database connections.
type DBConnector func(connectionURL string) (DBExecutor, error)
