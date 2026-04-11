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
	stderrors "errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// MySQLGeneratorType is the type identifier for MySQL generators.
	MySQLGeneratorType = "mysql"

	// DefaultMySQLCreationStatements are the default SQL statements for user creation.
	DefaultMySQLCreationStatements = `
		CREATE USER '{{username}}'@'%' IDENTIFIED BY '{{password}}';
		GRANT SELECT ON *.* TO '{{username}}'@'%';
		FLUSH PRIVILEGES;
	`

	// DefaultMySQLRevocationStatements are the default SQL statements for user revocation.
	DefaultMySQLRevocationStatements = `
		REVOKE ALL PRIVILEGES ON *.* FROM '{{username}}'@'%';
		DROP USER IF EXISTS '{{username}}'@'%';
		FLUSH PRIVILEGES;
	`
)

// MySQLGenerator generates credentials for MySQL databases.
type MySQLGenerator struct {
	base      *BaseGenerator
	connector DBConnector
	logger    interfaces.Logger
}

// NewMySQLGenerator creates a new MySQL credential generator.
func NewMySQLGenerator(connector DBConnector, logger interfaces.Logger) *MySQLGenerator {
	return &MySQLGenerator{
		base:      NewBaseGenerator(logger),
		connector: connector,
		logger:    logger.WithField("generator_type", "mysql"),
	}
}

// Type returns the generator type identifier.
func (g *MySQLGenerator) Type() string {
	return MySQLGeneratorType
}

// ValidateConfig validates the MySQL role configuration.
func (g *MySQLGenerator) ValidateConfig(config map[string]string) error {
	return g.base.ValidateConfig(config)
}

// Generate creates a new MySQL user with the specified role configuration.
func (g *MySQLGenerator) Generate(ctx context.Context, role *credentials.Role) (*credentials.Credential, error) {
	if role == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.Generate",
			Err:  stderrors.New("role cannot be nil"),
		}
	}

	if err := g.ValidateConfig(role.Config); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.Generate",
			Err:  &api.WrappedMessageError{Message: "invalid role config", Cause: err},
		}
	}

	if ctx.Err() != nil {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "MySQLGenerator.Generate",
			Err:  ctx.Err(),
		}
	}

	// Generate username and password
	username, err := g.base.GenerateUsername("myuser_")
	if err != nil {
		return nil, err
	}

	password, err := g.base.GeneratePassword(DefaultPasswordLength)
	if err != nil {
		return nil, err
	}

	credID, err := g.base.GenerateCredentialID()
	if err != nil {
		return nil, err
	}

	// Calculate expiration
	now := time.Now()
	ttl := role.DefaultTTL
	expiration := now.Add(ttl)

	// Get creation statements
	creationStatements := role.Config["creation_statements"]
	if creationStatements == "" {
		creationStatements = DefaultMySQLCreationStatements
	}

	// Substitute parameters
	stmts := g.base.ParseStatements(creationStatements)
	for i, stmt := range stmts {
		stmts[i] = g.base.SubstituteParameters(stmt, username, password, expiration)
	}

	// Connect to database
	connectionURL := role.Config["connection_url"]
	db, err := g.connector(connectionURL)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "MySQLGenerator.Generate",
			Err:  &api.WrappedMessageError{Message: "failed to connect to database", Cause: err},
		}
	}
	defer db.Close()

	// Execute creation statements
	for _, stmt := range stmts {
		if err := db.ExecContext(ctx, stmt); err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "MySQLGenerator.Generate",
				Err:  &api.WrappedMessageError{Message: "failed to execute creation statement", Cause: err},
			}
		}
	}

	g.logger.WithFields(map[string]interface{}{
		"credential_id": credID,
		"username":      username,
		"role":          role.Name,
	}).Info("MySQL credential generated")

	// Build credential
	credential := &credentials.Credential{
		CredentialID: credID,
		Type:         credentials.CredentialTypeDatabase,
		Data: map[string]string{
			"username": username,
			"password": password,
			"db_type":  "mysql",
		},
		CreatedAt: now,
		ExpiresAt: expiration,
	}

	// Add connection URL if requested
	if role.Config["include_connection_url"] == "true" {
		credential.Data["connection_url"] = g.base.BuildConnectionURL(connectionURL, username, password)
	}

	return credential, nil
}

// Revoke removes a MySQL user from the database.
func (g *MySQLGenerator) Revoke(ctx context.Context, credential *credentials.Credential) error {
	if credential == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.Revoke",
			Err:  stderrors.New("credential cannot be nil"),
		}
	}

	if ctx.Err() != nil {
		return &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "MySQLGenerator.Revoke",
			Err:  ctx.Err(),
		}
	}

	username := credential.Data["username"]
	if username == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.Revoke",
			Err:  stderrors.New("username not found in credential data"),
		}
	}

	return &errors.QBError{
		Code: errors.ErrNotImplemented,
		Op:   "MySQLGenerator.Revoke",
		Err:  stderrors.New("revocation requires role configuration - use service layer"),
	}
}

// revokeWithRole revokes a credential using the role configuration.
func (g *MySQLGenerator) revokeWithRole(ctx context.Context, credential *credentials.Credential, role *credentials.Role) error {
	if credential == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.revokeWithRole",
			Err:  stderrors.New("credential cannot be nil"),
		}
	}

	if role == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.revokeWithRole",
			Err:  stderrors.New("role cannot be nil"),
		}
	}

	username := credential.Data["username"]
	if username == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.revokeWithRole",
			Err:  stderrors.New("username not found in credential data"),
		}
	}

	// Get revocation statements
	revocationStatements := role.Config["revocation_statements"]
	if revocationStatements == "" {
		revocationStatements = DefaultMySQLRevocationStatements
	}

	// Substitute parameters
	stmts := g.base.ParseStatements(revocationStatements)
	for i, stmt := range stmts {
		stmts[i] = g.base.SubstituteParameters(stmt, username, "", time.Time{})
	}

	// Connect to database
	connectionURL := role.Config["connection_url"]
	db, err := g.connector(connectionURL)
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "MySQLGenerator.revokeWithRole",
			Err:  &api.WrappedMessageError{Message: "failed to connect to database", Cause: err},
		}
	}
	defer db.Close()

	// Execute revocation statements
	for _, stmt := range stmts {
		if err := db.ExecContext(ctx, stmt); err != nil {
			g.logger.WithFields(map[string]interface{}{
				"username": username,
				"error":    err,
			}).Warn("Failed to execute revocation statement")
		}
	}

	g.logger.WithFields(map[string]interface{}{
		"credential_id": credential.CredentialID,
		"username":      username,
	}).Info("MySQL credential revoked")

	return nil
}

// Rotate creates a new credential and revokes the old one.
func (g *MySQLGenerator) Rotate(ctx context.Context, old *credentials.Credential, role *credentials.Role) (*credentials.Credential, error) {
	if old == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.Rotate",
			Err:  stderrors.New("old credential cannot be nil"),
		}
	}

	if role == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "MySQLGenerator.Rotate",
			Err:  stderrors.New("role cannot be nil"),
		}
	}

	// Generate new credential
	newCred, err := g.Generate(ctx, role)
	if err != nil {
		return nil, err
	}

	// Revoke old credential
	if err := g.revokeWithRole(ctx, old, role); err != nil {
		g.logger.WithFields(map[string]interface{}{
			"old_credential_id": old.CredentialID,
			"error":             err,
		}).Warn("Failed to revoke old credential during rotation")
	}

	g.logger.WithFields(map[string]interface{}{
		"old_credential_id": old.CredentialID,
		"new_credential_id": newCred.CredentialID,
	}).Info("MySQL credential rotated")

	return newCred, nil
}
