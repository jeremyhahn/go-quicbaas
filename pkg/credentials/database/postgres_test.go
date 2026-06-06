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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// testLogger is a mock logger that implements interfaces.Logger.
type testLogger struct{}

func newTestLogger() interfaces.Logger {
	return &testLogger{}
}

func (l *testLogger) Debug(msg string, args ...any)                              {}
func (l *testLogger) Debugf(format string, args ...interface{})                  {}
func (l *testLogger) Info(msg string, args ...any)                               {}
func (l *testLogger) Infof(format string, args ...interface{})                   {}
func (l *testLogger) Warn(msg string, args ...any)                               {}
func (l *testLogger) Warnf(format string, args ...interface{})                   {}
func (l *testLogger) Error(msg string, args ...any)                              {}
func (l *testLogger) Errorf(format string, args ...interface{})                  {}
func (l *testLogger) Fatal(msg string)                                           {}
func (l *testLogger) Fatalf(format string, args ...interface{})                  {}
func (l *testLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *testLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *testLogger) WithError(err error) interfaces.Logger                      { return l }

// MockDBExecutor is a mock database executor for testing.
type MockDBExecutor struct {
	execCalls   []string
	execError   error
	closeCalled bool
}

func (m *MockDBExecutor) ExecContext(ctx context.Context, query string, args ...interface{}) error {
	m.execCalls = append(m.execCalls, query)
	return m.execError
}

func (m *MockDBExecutor) Close() error {
	m.closeCalled = true
	return nil
}

func TestPostgreSQLGenerator_Type(t *testing.T) {
	log := newTestLogger()

	connector := func(connectionURL string) (DBExecutor, error) {
		return &MockDBExecutor{}, nil
	}

	gen := NewPostgreSQLGenerator(connector, log)
	assert.Equal(t, PostgreSQLGeneratorType, gen.Type())
}

func TestPostgreSQLGenerator_ValidateConfig(t *testing.T) {
	log := newTestLogger()

	connector := func(connectionURL string) (DBExecutor, error) {
		return &MockDBExecutor{}, nil
	}

	gen := NewPostgreSQLGenerator(connector, log)

	t.Run("valid config", func(t *testing.T) {
		config := map[string]string{
			"connection_url":        "postgresql://localhost:5432/mydb",
			"creation_statements":   "CREATE USER {{username}}",
			"revocation_statements": "DROP USER {{username}}",
		}

		err := gen.ValidateConfig(config)
		assert.NoError(t, err)
	})

	t.Run("missing connection_url", func(t *testing.T) {
		config := map[string]string{
			"creation_statements": "CREATE USER {{username}}",
		}

		err := gen.ValidateConfig(config)
		assert.Error(t, err)
	})

	t.Run("missing creation_statements", func(t *testing.T) {
		config := map[string]string{
			"connection_url": "postgresql://localhost:5432/mydb",
		}

		err := gen.ValidateConfig(config)
		assert.Error(t, err)
	})

	t.Run("nil config", func(t *testing.T) {
		err := gen.ValidateConfig(nil)
		assert.Error(t, err)
	})
}

func TestPostgreSQLGenerator_Generate(t *testing.T) {
	log := newTestLogger()

	t.Run("successful generation", func(t *testing.T) {
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "postgresql://admin:pass@localhost:5432/postgres",
				"creation_statements": "CREATE USER \"{{username}}\" WITH PASSWORD '{{password}}';\nGRANT CONNECT ON DATABASE postgres TO \"{{username}}\";",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		credential, err := gen.Generate(ctx, role)

		require.NoError(t, err)
		assert.NotEmpty(t, credential.CredentialID)
		assert.Equal(t, credentials.CredentialTypeDatabase, credential.Type)
		assert.NotEmpty(t, credential.Data["username"])
		assert.NotEmpty(t, credential.Data["password"])
		assert.Equal(t, "postgres", credential.Data["db_type"])
		assert.True(t, mockDB.closeCalled)
		assert.GreaterOrEqual(t, len(mockDB.execCalls), 2)
	})

	t.Run("with connection URL", func(t *testing.T) {
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":         "postgresql://admin:pass@localhost:5432/postgres",
				"creation_statements":    "CREATE USER \"{{username}}\" WITH PASSWORD '{{password}}';",
				"include_connection_url": "true",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		credential, err := gen.Generate(ctx, role)

		require.NoError(t, err)
		assert.NotEmpty(t, credential.Data["connection_url"])
	})

	t.Run("database connection error", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return nil, stderrors.New("connection failed")
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		_, err := gen.Generate(ctx, role)
		assert.Error(t, err)
	})

	t.Run("database execution error", func(t *testing.T) {
		mockDB := &MockDBExecutor{
			execError: stderrors.New("execution failed"),
		}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		_, err := gen.Generate(ctx, role)
		assert.Error(t, err)
	})

	t.Run("nil role error", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)
		ctx := context.Background()

		_, err := gen.Generate(ctx, nil)
		assert.Error(t, err)
	})

	t.Run("invalid config error", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config:        map[string]string{
				// Missing required fields
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		_, err := gen.Generate(ctx, role)
		assert.Error(t, err)
	})

	t.Run("context cancelled error", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := gen.Generate(ctx, role)
		assert.Error(t, err)
	})
}

func TestPostgreSQLGenerator_Revoke(t *testing.T) {
	log := newTestLogger()

	t.Run("error - not implemented without role", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Type:         credentials.CredentialTypeDatabase,
			Data: map[string]string{
				"username": "testuser",
				"password": "testpass",
				"db_type":  "postgres",
			},
		}

		ctx := context.Background()
		err := gen.Revoke(ctx, credential)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "revocation requires role configuration")
	})

	t.Run("error - nil credential", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		ctx := context.Background()
		err := gen.Revoke(ctx, nil)

		require.Error(t, err)
	})

	t.Run("error - context cancelled", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

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
	})

	t.Run("error - missing username", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data:         map[string]string{},
		}

		ctx := context.Background()
		err := gen.Revoke(ctx, credential)

		require.Error(t, err)
	})
}

func TestPostgreSQLGenerator_revokeWithRole(t *testing.T) {
	log := newTestLogger()

	t.Run("successful revocation with default statements", func(t *testing.T) {
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "old_user",
			},
		}

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
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

	t.Run("successful revocation with custom statements", func(t *testing.T) {
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "test_user",
			},
		}

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":        "postgresql://localhost:5432/postgres",
				"creation_statements":   "CREATE USER {{username}}",
				"revocation_statements": "DROP USER IF EXISTS \"{{username}}\";",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		require.NoError(t, err)
		assert.True(t, mockDB.closeCalled)
	})

	t.Run("error - nil credential", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name: "test-role",
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, nil, role)

		require.Error(t, err)
	})

	t.Run("error - nil role", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "testuser",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, nil)

		require.Error(t, err)
	})

	t.Run("error - missing username", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data:         map[string]string{},
		}

		role := &credentials.Role{
			Name: "test-role",
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		require.Error(t, err)
	})

	t.Run("error - connection failed", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return nil, stderrors.New("connection failed")
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "testuser",
			},
		}

		role := &credentials.Role{
			Name: "test-role",
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		require.Error(t, err)
	})

	t.Run("partial success - statement execution errors logged", func(t *testing.T) {
		mockDB := &MockDBExecutor{
			execError: stderrors.New("user does not exist"),
		}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		credential := &credentials.Credential{
			CredentialID: "cred_12345",
			Data: map[string]string{
				"username": "testuser",
			},
		}

		role := &credentials.Role{
			Name: "test-role",
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
		}

		ctx := context.Background()
		err := gen.revokeWithRole(ctx, credential, role)

		// Should not return error - errors are logged
		require.NoError(t, err)
		assert.True(t, mockDB.closeCalled)
	})
}

func TestPostgreSQLGenerator_Rotate(t *testing.T) {
	log := newTestLogger()

	t.Run("successful rotation", func(t *testing.T) {
		mockDB := &MockDBExecutor{}
		connector := func(connectionURL string) (DBExecutor, error) {
			return mockDB, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":        "postgresql://localhost:5432/postgres",
				"creation_statements":   "CREATE USER {{username}}",
				"revocation_statements": "DROP USER {{username}}",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old",
			Type:         credentials.CredentialTypeDatabase,
			Data: map[string]string{
				"username": "old_user",
				"password": "old_pass",
			},
		}

		ctx := context.Background()
		newCredential, err := gen.Rotate(ctx, oldCredential, role)

		require.NoError(t, err)
		assert.NotEqual(t, oldCredential.CredentialID, newCredential.CredentialID)
		assert.NotEqual(t, oldCredential.Data["username"], newCredential.Data["username"])
	})

	t.Run("nil old credential error", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		ctx := context.Background()
		_, err := gen.Rotate(ctx, nil, role)
		assert.Error(t, err)
	})

	t.Run("nil role error", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old",
			Data: map[string]string{
				"username": "old_user",
			},
		}

		ctx := context.Background()
		_, err := gen.Rotate(ctx, oldCredential, nil)
		assert.Error(t, err)
	})

	t.Run("generate new credential fails", func(t *testing.T) {
		connector := func(connectionURL string) (DBExecutor, error) {
			return &MockDBExecutor{}, nil
		}

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config:        map[string]string{
				// Missing required fields
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old",
			Data: map[string]string{
				"username": "old_user",
			},
		}

		ctx := context.Background()
		_, err := gen.Rotate(ctx, oldCredential, role)
		assert.Error(t, err)
	})

	t.Run("revoke old credential fails but rotation succeeds", func(t *testing.T) {
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

		gen := NewPostgreSQLGenerator(connector, log)

		role := &credentials.Role{
			Name:          "test-role",
			GeneratorType: PostgreSQLGeneratorType,
			Config: map[string]string{
				"connection_url":      "postgresql://localhost:5432/postgres",
				"creation_statements": "CREATE USER {{username}}",
			},
			DefaultTTL: 1 * time.Hour,
			MaxTTL:     24 * time.Hour,
		}

		oldCredential := &credentials.Credential{
			CredentialID: "cred_old",
			Data: map[string]string{
				"username": "old_user",
			},
		}

		ctx := context.Background()
		newCredential, err := gen.Rotate(ctx, oldCredential, role)

		// Should succeed even if revoke fails
		require.NoError(t, err)
		assert.NotNil(t, newCredential)
		assert.NotEqual(t, oldCredential.CredentialID, newCredential.CredentialID)
	})
}

func TestBaseGenerator_GenerateUsername(t *testing.T) {
	log := newTestLogger()

	gen := NewBaseGenerator(log)

	t.Run("with custom prefix", func(t *testing.T) {
		username, err := gen.GenerateUsername("test_")
		require.NoError(t, err)
		assert.NotEmpty(t, username)
		assert.Contains(t, username, "test_")
	})

	t.Run("with default prefix", func(t *testing.T) {
		username, err := gen.GenerateUsername("")
		require.NoError(t, err)
		assert.NotEmpty(t, username)
		assert.Contains(t, username, DefaultUsernamePrefix)
	})
}

func TestBaseGenerator_GeneratePassword(t *testing.T) {
	log := newTestLogger()

	gen := NewBaseGenerator(log)

	t.Run("with custom length", func(t *testing.T) {
		password, err := gen.GeneratePassword(64)
		require.NoError(t, err)
		assert.Len(t, password, 64)
	})

	t.Run("with default length", func(t *testing.T) {
		password, err := gen.GeneratePassword(0)
		require.NoError(t, err)
		assert.Len(t, password, DefaultPasswordLength)
	})

	t.Run("uniqueness", func(t *testing.T) {
		password1, err := gen.GeneratePassword(32)
		require.NoError(t, err)

		password2, err := gen.GeneratePassword(32)
		require.NoError(t, err)

		assert.NotEqual(t, password1, password2)
	})
}

func TestBaseGenerator_ParseStatements(t *testing.T) {
	log := newTestLogger()

	gen := NewBaseGenerator(log)

	t.Run("multiple statements", func(t *testing.T) {
		sql := "CREATE USER test; GRANT SELECT ON *.* TO test; FLUSH PRIVILEGES;"
		statements := gen.ParseStatements(sql)
		assert.Len(t, statements, 3)
		assert.Equal(t, "CREATE USER test", statements[0])
		assert.Equal(t, "GRANT SELECT ON *.* TO test", statements[1])
		assert.Equal(t, "FLUSH PRIVILEGES", statements[2])
	})

	t.Run("single statement", func(t *testing.T) {
		sql := "CREATE USER test"
		statements := gen.ParseStatements(sql)
		assert.Len(t, statements, 1)
		assert.Equal(t, "CREATE USER test", statements[0])
	})

	t.Run("empty string", func(t *testing.T) {
		statements := gen.ParseStatements("")
		assert.Len(t, statements, 0)
	})
}

func TestBaseGenerator_SubstituteParameters(t *testing.T) {
	log := newTestLogger()

	gen := NewBaseGenerator(log)

	t.Run("substitute all parameters", func(t *testing.T) {
		template := "CREATE USER {{username}} WITH PASSWORD '{{password}}' VALID UNTIL '{{expiration}}'"
		expiration := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

		result := gen.SubstituteParameters(template, "testuser", "testpass", expiration)

		assert.Contains(t, result, "testuser")
		assert.Contains(t, result, "testpass")
		assert.Contains(t, result, "2024-12-31")
	})

	t.Run("no parameters", func(t *testing.T) {
		template := "CREATE TABLE test"
		result := gen.SubstituteParameters(template, "user", "pass", time.Time{})
		assert.Equal(t, template, result)
	})
}
