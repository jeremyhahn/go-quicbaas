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

package database

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestNewMySQLGenerator(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "creates MySQL generator successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			connector := func(connectionURL string) (DBExecutor, error) {
				return &MockDBExecutor{}, nil
			}

			gen := NewMySQLGenerator(connector, logger)

			require.NotNil(t, gen)
			assert.NotNil(t, gen.base)
			assert.NotNil(t, gen.connector)
			assert.NotNil(t, gen.logger)
		})
	}
}

func TestMySQLGenerator_Type(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "returns correct type identifier",
			want: MySQLGeneratorType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			connector := func(connectionURL string) (DBExecutor, error) {
				return &MockDBExecutor{}, nil
			}

			gen := NewMySQLGenerator(connector, logger)
			result := gen.Type()

			assert.Equal(t, tt.want, result)
			assert.Equal(t, "mysql", result)
		})
	}
}

func TestMySQLGenerator_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]string
		wantErr bool
		errCode errors.ErrorCode
	}{
		{
			name: "success - valid config",
			config: map[string]string{
				"connection_url":      "mysql://localhost:3306/testdb",
				"creation_statements": "CREATE USER '{{username}}'@'%'",
			},
			wantErr: false,
		},
		{
			name:    "error - nil config",
			config:  nil,
			wantErr: true,
			errCode: errors.ErrInvalidInput,
		},
		{
			name: "error - missing connection_url",
			config: map[string]string{
				"creation_statements": "CREATE USER '{{username}}'@'%'",
			},
			wantErr: true,
			errCode: errors.ErrInvalidInput,
		},
		{
			name: "error - missing creation_statements",
			config: map[string]string{
				"connection_url": "mysql://localhost:3306/testdb",
			},
			wantErr: true,
			errCode: errors.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			connector := func(connectionURL string) (DBExecutor, error) {
				return &MockDBExecutor{}, nil
			}

			gen := NewMySQLGenerator(connector, logger)
			err := gen.ValidateConfig(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				var dragonErr *errors.DragonError
				require.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, tt.errCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMySQLGenerator_Generate(t *testing.T) {
	t.Run("success - generates credential with default statements", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name:          "mysql-readonly",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "mysql://admin:secret@localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'%' IDENTIFIED BY '{{password}}'; GRANT SELECT ON *.* TO '{{username}}'@'%'; FLUSH PRIVILEGES;",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		credential, err := gen.Generate(ctx, role)

		require.NoError(t, err)
		require.NotNil(t, credential)
		assert.NotEmpty(t, credential.CredentialID)
		assert.True(t, len(credential.CredentialID) > len("cred_"))
		assert.Equal(t, credentials.CredentialTypeDatabase, credential.Type)
		assert.NotEmpty(t, credential.Data["username"])
		assert.NotEmpty(t, credential.Data["password"])
		assert.Equal(t, "mysql", credential.Data["db_type"])
		assert.False(t, credential.CreatedAt.IsZero())
		assert.False(t, credential.ExpiresAt.IsZero())
		assert.True(t, credential.ExpiresAt.After(credential.CreatedAt))
		assert.True(t, mockDB.closeCalled)
		assert.GreaterOrEqual(t, len(mockDB.execCalls), 3)
	})

	t.Run("success - generates credential with custom statements", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name:          "mysql-admin",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "mysql://root:rootpass@localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'localhost' IDENTIFIED BY '{{password}}'; GRANT ALL PRIVILEGES ON mydb.* TO '{{username}}'@'localhost';",
			},
			DefaultTTL: 2 * time.Hour,
			MaxTTL:     48 * time.Hour,
		}

		ctx := context.Background()
		credential, err := gen.Generate(ctx, role)

		require.NoError(t, err)
		require.NotNil(t, credential)
		assert.NotEmpty(t, credential.CredentialID)
		assert.Equal(t, 2, len(mockDB.execCalls))
	})

	t.Run("success - includes connection URL when requested", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name:          "mysql-app",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":         "mysql://localhost:3306/appdb",
				"creation_statements":    "CREATE USER '{{username}}'@'%' IDENTIFIED BY '{{password}}';",
				"include_connection_url": "true",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     12 * time.Hour,
		}

		ctx := context.Background()
		credential, err := gen.Generate(ctx, role)

		require.NoError(t, err)
		require.NotNil(t, credential)
		assert.NotEmpty(t, credential.Data["connection_url"])
		assert.Contains(t, credential.Data["connection_url"], credential.Data["username"])
		assert.Contains(t, credential.Data["connection_url"], credential.Data["password"])
	})

	t.Run("error - nil role", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)
		ctx := context.Background()

		_, err := gen.Generate(ctx, nil)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - invalid config", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name:          "mysql-test",
			GeneratorType: MySQLGeneratorType,
			Config:        map[string]string{
				// Missing required fields
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		_, err := gen.Generate(ctx, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - context cancelled", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name:          "mysql-test",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/testdb",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := gen.Generate(ctx, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrTimeout, dragonErr.Code)
	})

	t.Run("error - database connection failed", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return nil, stderrors.New("connection refused")
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name:          "mysql-test",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/testdb",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		_, err := gen.Generate(ctx, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInternal, dragonErr.Code)
	})

	t.Run("error - statement execution failed", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{
			execError: stderrors.New("access denied"),
		}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name:          "mysql-test",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/testdb",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		_, err := gen.Generate(ctx, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInternal, dragonErr.Code)
	})
}

func TestMySQLGenerator_Revoke(t *testing.T) {
	t.Run("error - not implemented without role", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Type:         credentials.CredentialTypeDatabase,
			Data: map[string]string{
				"username": "testuser",
				"password": "testpass",
				"db_type":  "mysql",
			},
		}

		ctx := context.Background()
		err := gen.Revoke(ctx, credential)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrNotImplemented, dragonErr.Code)
	})

	t.Run("error - nil credential", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		ctx := context.Background()
		err := gen.Revoke(ctx, nil)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - context cancelled", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "testuser",
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := gen.Revoke(ctx, credential)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrTimeout, dragonErr.Code)
	})

	t.Run("error - missing username", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data:         map[string]string{},
		}

		ctx := context.Background()
		err := gen.Revoke(ctx, credential)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})
}

func TestMySQLGenerator_revokeWithRole(t *testing.T) {
	t.Run("success - revokes credential with default statements", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Type:         credentials.CredentialTypeDatabase,
			Data: map[string]string{
				"username": "old_user",
				"password": "old_pass",
				"db_type":  "mysql",
			},
		}

		role := &credentials.Role{
			Name:          "mysql-role",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":        "mysql://admin:secret@localhost:3306/mysql",
				"creation_statements":   "CREATE USER '{{username}}'@'%';",
				"revocation_statements": "REVOKE ALL PRIVILEGES ON *.* FROM '{{username}}'@'%'; DROP USER IF EXISTS '{{username}}'@'%';",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		require.NoError(t, err)
		assert.True(t, mockDB.closeCalled)
		assert.GreaterOrEqual(t, len(mockDB.execCalls), 2)
	})

	t.Run("success - uses default revocation statements when not provided", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_67890",
			Data: map[string]string{
				"username": "temp_user",
			},
		}

		role := &credentials.Role{
			Name:          "mysql-role",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
				// No revocation_statements - should use defaults
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		require.NoError(t, err)
		assert.True(t, mockDB.closeCalled)
		assert.GreaterOrEqual(t, len(mockDB.execCalls), 1)
	})

	t.Run("error - nil credential", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name: "mysql-role",
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, nil, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - nil role", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "testuser",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, nil)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - missing username", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data:         map[string]string{},
		}

		role := &credentials.Role{
			Name: "mysql-role",
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - connection failed", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return nil, stderrors.New("connection refused")
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "testuser",
			},
		}

		role := &credentials.Role{
			Name: "mysql-role",
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInternal, dragonErr.Code)
	})

	t.Run("partial success - statement execution errors are logged but not fatal", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{
			execError: stderrors.New("user does not exist"),
		}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "testuser",
			},
		}

		role := &credentials.Role{
			Name: "mysql-role",
			Config: map[string]string{
				"connection_url":        "mysql://localhost:3306/mysql",
				"creation_statements":   "CREATE USER '{{username}}'@'%';",
				"revocation_statements": "DROP USER '{{username}}'@'%';",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		// Errors are logged but don't fail the operation
		require.NoError(t, err)
		assert.True(t, mockDB.closeCalled)
	})
}

func TestMySQLGenerator_Rotate(t *testing.T) {
	t.Run("success - rotates credential", func(t *testing.T) {
		logger := newMockLogger()
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old_12345",
			Type:         credentials.CredentialTypeDatabase,
			Data: map[string]string{
				"username": "old_user",
				"password": "old_pass",
				"db_type":  "mysql",
			},
		}

		role := &credentials.Role{
			Name:          "mysql-role",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":        "mysql://admin:secret@localhost:3306/mysql",
				"creation_statements":   "CREATE USER '{{username}}'@'%' IDENTIFIED BY '{{password}}';",
				"revocation_statements": "DROP USER IF EXISTS '{{username}}'@'%';",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		newCredential, err := gen.Rotate(ctx, oldCredential, role)

		require.NoError(t, err)
		require.NotNil(t, newCredential)
		assert.NotEqual(t, oldCredential.CredentialID, newCredential.CredentialID)
		assert.NotEqual(t, oldCredential.Data["username"], newCredential.Data["username"])
		assert.NotEqual(t, oldCredential.Data["password"], newCredential.Data["password"])
	})

	t.Run("error - nil old credential", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		role := &credentials.Role{
			Name: "mysql-role",
			Config: map[string]string{
				"connection_url":      "mysql://localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
		}

		ctx := context.Background()
		_, err := gen.Rotate(ctx, nil, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - nil role", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old",
			Data: map[string]string{
				"username": "old_user",
			},
		}

		ctx := context.Background()
		_, err := gen.Rotate(ctx, oldCredential, nil)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("error - generate new credential fails", func(t *testing.T) {
		logger := newMockLogger()
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewMySQLGenerator(connector, logger)

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old",
			Data: map[string]string{
				"username": "old_user",
			},
		}

		role := &credentials.Role{
			Name:   "mysql-role",
			Config: map[string]string{
				// Missing required fields - will cause Generate to fail
			},
		}

		ctx := context.Background()
		_, err := gen.Rotate(ctx, oldCredential, role)

		require.Error(t, err)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	})

	t.Run("partial success - revoke fails but new credential created", func(t *testing.T) {
		logger := newMockLogger()
		callCount := 0
		connector := func(connectionURL string) (DBExecutor, error) {
			callCount++
			if callCount == 1 {
				// First call for Generate - success
				return &MockDBExecutor{}, nil
			}
			// Second call for revoke - fail
			return nil, stderrors.New("connection failed")
		}

		gen := NewMySQLGenerator(connector, logger)

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old",
			Data: map[string]string{
				"username": "old_user",
			},
		}

		role := &credentials.Role{
			Name:          "mysql-role",
			GeneratorType: MySQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "mysql://admin:secret@localhost:3306/mysql",
				"creation_statements": "CREATE USER '{{username}}'@'%';",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		newCredential, err := gen.Rotate(ctx, oldCredential, role)

		// Should succeed even if revoke fails
		require.NoError(t, err)
		require.NotNil(t, newCredential)
		assert.NotEqual(t, oldCredential.CredentialID, newCredential.CredentialID)
	})
}
