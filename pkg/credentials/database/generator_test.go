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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockLogger implements interfaces.Logger for testing.
type mockLogger struct {
	fields map[string]interface{}
}

func newMockLogger() *mockLogger {
	return &mockLogger{
		fields: make(map[string]interface{}),
	}
}

func (m *mockLogger) Debug(msg string)                          {}
func (m *mockLogger) Debugf(format string, args ...interface{}) {}
func (m *mockLogger) Info(msg string)                           {}
func (m *mockLogger) Infof(format string, args ...interface{})  {}
func (m *mockLogger) Warn(msg string)                           {}
func (m *mockLogger) Warnf(format string, args ...interface{})  {}
func (m *mockLogger) Error(msg string)                          {}
func (m *mockLogger) Errorf(format string, args ...interface{}) {}
func (m *mockLogger) Fatal(msg string)                          {}
func (m *mockLogger) Fatalf(format string, args ...interface{}) {}
func (m *mockLogger) WithError(err error) interfaces.Logger     { return m }

func (m *mockLogger) WithField(key string, value interface{}) interfaces.Logger {
	newLogger := &mockLogger{
		fields: make(map[string]interface{}),
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (m *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	newLogger := &mockLogger{
		fields: make(map[string]interface{}),
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

func TestNewBaseGenerator(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "creates base generator successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			require.NotNil(t, gen)
			assert.NotNil(t, gen.logger)
		})
	}
}

func TestBaseGenerator_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]string
		wantErr bool
		errCode errors.ErrorCode
		errMsg  string
	}{
		{
			name: "success - valid config",
			config: map[string]string{
				"connection_url":      "postgres://localhost:5432/testdb",
				"creation_statements": "CREATE USER {{username}}",
			},
			wantErr: false,
		},
		{
			name:    "error - nil config",
			config:  nil,
			wantErr: true,
			errCode: errors.ErrInvalidInput,
			errMsg:  "config cannot be nil",
		},
		{
			name: "error - missing connection_url",
			config: map[string]string{
				"creation_statements": "CREATE USER {{username}}",
			},
			wantErr: true,
			errCode: errors.ErrInvalidInput,
			errMsg:  "connection_url is required",
		},
		{
			name: "error - empty connection_url",
			config: map[string]string{
				"connection_url":      "",
				"creation_statements": "CREATE USER {{username}}",
			},
			wantErr: true,
			errCode: errors.ErrInvalidInput,
			errMsg:  "connection_url is required",
		},
		{
			name: "error - missing creation_statements",
			config: map[string]string{
				"connection_url": "postgres://localhost:5432/testdb",
			},
			wantErr: true,
			errCode: errors.ErrInvalidInput,
			errMsg:  "creation_statements is required",
		},
		{
			name: "error - empty creation_statements",
			config: map[string]string{
				"connection_url":      "postgres://localhost:5432/testdb",
				"creation_statements": "",
			},
			wantErr: true,
			errCode: errors.ErrInvalidInput,
			errMsg:  "creation_statements is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			err := gen.ValidateConfig(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				var dragonErr *errors.DragonError
				require.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, tt.errCode, dragonErr.Code)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBaseGenerator_GenerateUsername_Uniqueness(t *testing.T) {
	t.Run("generates unique usernames", func(t *testing.T) {
		logger := newMockLogger()
		gen := NewBaseGenerator(logger)

		usernames := make(map[string]bool)
		count := 100

		for i := 0; i < count; i++ {
			username, err := gen.GenerateUsername("test_")
			require.NoError(t, err)
			assert.False(t, usernames[username], "duplicate username generated: %s", username)
			usernames[username] = true
		}

		assert.Len(t, usernames, count, "should generate unique usernames")
	})
}

func TestBaseGenerator_GenerateUsername_Format(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		wantPrefix string
	}{
		{
			name:       "success - with custom prefix",
			prefix:     "dbuser_",
			wantPrefix: "dbuser_",
		},
		{
			name:       "success - with postgres prefix",
			prefix:     "pguser_",
			wantPrefix: "pguser_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			username, err := gen.GenerateUsername(tt.prefix)

			require.NoError(t, err)
			assert.NotEmpty(t, username)
			assert.True(t, strings.HasPrefix(username, tt.wantPrefix))

			// Verify format: prefix + timestamp + dash + hex
			// For custom prefixes with underscore, we can safely split by dash
			if strings.HasSuffix(tt.prefix, "_") {
				parts := strings.Split(username, "-")
				assert.Len(t, parts, 2, "username should have format: prefix+timestamp-hex")

				// Verify timestamp part exists
				timestampPart := strings.TrimPrefix(parts[0], tt.wantPrefix)
				assert.NotEmpty(t, timestampPart)

				// Verify hex part (should be 16 chars for 8 bytes)
				assert.Len(t, parts[1], 16)
			}

			// Verify it contains a timestamp (numeric) and hex suffix
			assert.Contains(t, username, tt.wantPrefix)
		})
	}
}

func TestBaseGenerator_GenerateUsername_DefaultPrefix(t *testing.T) {
	t.Run("uses default prefix when empty", func(t *testing.T) {
		logger := newMockLogger()
		gen := NewBaseGenerator(logger)

		username, err := gen.GenerateUsername("")

		require.NoError(t, err)
		assert.NotEmpty(t, username)
		assert.True(t, strings.HasPrefix(username, DefaultUsernamePrefix))

		// The default prefix is "v-" which ends with dash
		// Format is: v-timestamp-hex, so splitting by "-" gives us ["v", "timestamp", "hex"]
		parts := strings.Split(username, "-")
		assert.GreaterOrEqual(t, len(parts), 3, "username should have format: v-timestamp-hex")

		// Verify hex part (last element should be 16 chars for 8 bytes)
		assert.Len(t, parts[len(parts)-1], 16)
	})
}

func TestBaseGenerator_GeneratePassword_Length(t *testing.T) {
	tests := []struct {
		name       string
		length     int
		wantLength int
	}{
		{
			name:       "success - default length",
			length:     0,
			wantLength: DefaultPasswordLength,
		},
		{
			name:       "success - negative length uses default",
			length:     -1,
			wantLength: DefaultPasswordLength,
		},
		{
			name:       "success - custom length 16",
			length:     16,
			wantLength: 16,
		},
		{
			name:       "success - custom length 64",
			length:     64,
			wantLength: 64,
		},
		{
			name:       "success - custom length 128",
			length:     128,
			wantLength: 128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			password, err := gen.GeneratePassword(tt.length)

			require.NoError(t, err)
			assert.NotEmpty(t, password)
			assert.Len(t, password, tt.wantLength)

			// Password should be URL-safe base64
			// Valid characters: A-Z, a-z, 0-9, -, _
			for _, ch := range password {
				assert.True(t,
					(ch >= 'A' && ch <= 'Z') ||
						(ch >= 'a' && ch <= 'z') ||
						(ch >= '0' && ch <= '9') ||
						ch == '-' || ch == '_',
					"invalid character in password: %c", ch)
			}
		})
	}
}

func TestBaseGenerator_GeneratePassword_Uniqueness(t *testing.T) {
	t.Run("generates unique passwords", func(t *testing.T) {
		logger := newMockLogger()
		gen := NewBaseGenerator(logger)

		passwords := make(map[string]bool)
		count := 100

		for i := 0; i < count; i++ {
			password, err := gen.GeneratePassword(32)
			require.NoError(t, err)
			assert.False(t, passwords[password], "duplicate password generated: %s", password)
			passwords[password] = true
		}

		assert.Len(t, passwords, count, "should generate unique passwords")
	})
}

func TestBaseGenerator_GenerateCredentialID(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "success - generates valid credential ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			credID, err := gen.GenerateCredentialID()

			require.NoError(t, err)
			assert.NotEmpty(t, credID)
			assert.True(t, strings.HasPrefix(credID, "cred_"))

			// Should have format: cred_ + 32 hex chars (16 bytes)
			hexPart := strings.TrimPrefix(credID, "cred_")
			assert.Len(t, hexPart, 32)

			// Verify it's valid hex
			for _, ch := range hexPart {
				assert.True(t,
					(ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'),
					"invalid hex character: %c", ch)
			}
		})
	}
}

func TestBaseGenerator_GenerateCredentialID_Uniqueness(t *testing.T) {
	t.Run("generates unique credential IDs", func(t *testing.T) {
		logger := newMockLogger()
		gen := NewBaseGenerator(logger)

		credIDs := make(map[string]bool)
		count := 100

		for i := 0; i < count; i++ {
			credID, err := gen.GenerateCredentialID()
			require.NoError(t, err)
			assert.False(t, credIDs[credID], "duplicate credential ID generated: %s", credID)
			credIDs[credID] = true
		}

		assert.Len(t, credIDs, count, "should generate unique credential IDs")
	})
}

func TestBaseGenerator_SubstituteParameters_AllCases(t *testing.T) {
	expiration := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	tests := []struct {
		name       string
		statement  string
		username   string
		password   string
		expiration time.Time
		want       string
	}{
		{
			name:       "success - substitute all parameters",
			statement:  "CREATE USER {{username}} WITH PASSWORD '{{password}}' VALID UNTIL '{{expiration}}'",
			username:   "testuser",
			password:   "testpass",
			expiration: expiration,
			want:       fmt.Sprintf("CREATE USER testuser WITH PASSWORD 'testpass' VALID UNTIL '%s'", expiration.Format(time.RFC3339)),
		},
		{
			name:       "success - substitute username only",
			statement:  "GRANT SELECT ON DATABASE testdb TO {{username}}",
			username:   "dbuser",
			password:   "pass123",
			expiration: expiration,
			want:       "GRANT SELECT ON DATABASE testdb TO dbuser",
		},
		{
			name:       "success - substitute password only",
			statement:  "ALTER USER admin WITH PASSWORD '{{password}}'",
			username:   "admin",
			password:   "newpass",
			expiration: expiration,
			want:       "ALTER USER admin WITH PASSWORD 'newpass'",
		},
		{
			name:       "success - no substitution needed",
			statement:  "GRANT ALL PRIVILEGES ON DATABASE testdb TO admin",
			username:   "admin",
			password:   "pass",
			expiration: expiration,
			want:       "GRANT ALL PRIVILEGES ON DATABASE testdb TO admin",
		},
		{
			name:       "success - multiple occurrences",
			statement:  "{{username}} {{username}} {{password}}",
			username:   "user",
			password:   "pass",
			expiration: expiration,
			want:       "user user pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			result := gen.SubstituteParameters(tt.statement, tt.username, tt.password, tt.expiration)

			assert.Equal(t, tt.want, result)
		})
	}
}

func TestBaseGenerator_ParseStatements_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		statements string
		want       []string
	}{
		{
			name:       "success - single statement",
			statements: "CREATE USER testuser",
			want:       []string{"CREATE USER testuser"},
		},
		{
			name:       "success - multiple statements",
			statements: "CREATE USER testuser; GRANT SELECT ON DATABASE testdb TO testuser; GRANT CONNECT ON DATABASE testdb TO testuser",
			want: []string{
				"CREATE USER testuser",
				"GRANT SELECT ON DATABASE testdb TO testuser",
				"GRANT CONNECT ON DATABASE testdb TO testuser",
			},
		},
		{
			name:       "success - statements with whitespace",
			statements: "  CREATE USER testuser  ;  GRANT SELECT ON DATABASE testdb TO testuser  ",
			want: []string{
				"CREATE USER testuser",
				"GRANT SELECT ON DATABASE testdb TO testuser",
			},
		},
		{
			name:       "success - statements with newlines",
			statements: "CREATE USER testuser;\nGRANT SELECT ON DATABASE testdb TO testuser;\nGRANT CONNECT ON DATABASE testdb TO testuser",
			want: []string{
				"CREATE USER testuser",
				"GRANT SELECT ON DATABASE testdb TO testuser",
				"GRANT CONNECT ON DATABASE testdb TO testuser",
			},
		},
		{
			name:       "success - empty statements ignored",
			statements: "CREATE USER testuser;;;GRANT SELECT",
			want: []string{
				"CREATE USER testuser",
				"GRANT SELECT",
			},
		},
		{
			name:       "success - empty string",
			statements: "",
			want:       []string{},
		},
		{
			name:       "success - only semicolons",
			statements: ";;;",
			want:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			result := gen.ParseStatements(tt.statements)

			assert.Equal(t, tt.want, result)
		})
	}
}

func TestBaseGenerator_BuildConnectionURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		username string
		password string
		want     string
	}{
		{
			name:     "success - add credentials to URL without existing creds",
			baseURL:  "postgres://localhost:5432/testdb",
			username: "user",
			password: "pass",
			want:     "postgres://user:pass@localhost:5432/testdb",
		},
		{
			name:     "success - replace existing credentials",
			baseURL:  "postgres://olduser:oldpass@localhost:5432/testdb",
			username: "newuser",
			password: "newpass",
			want:     "postgres://newuser:newpass@localhost:5432/testdb",
		},
		{
			name:     "success - mysql URL without creds",
			baseURL:  "mysql://localhost:3306/testdb",
			username: "dbuser",
			password: "dbpass",
			want:     "mysql://dbuser:dbpass@localhost:3306/testdb",
		},
		{
			name:     "success - complex URL without creds",
			baseURL:  "postgres://localhost:5432/testdb?sslmode=require&pool_max_conns=10",
			username: "admin",
			password: "secret",
			want:     "postgres://admin:secret@localhost:5432/testdb?sslmode=require&pool_max_conns=10",
		},
		{
			name:     "success - replace creds in complex URL",
			baseURL:  "postgres://old:old@localhost:5432/testdb?sslmode=require",
			username: "new",
			password: "new",
			want:     "postgres://new:new@localhost:5432/testdb?sslmode=require",
		},
		{
			name:     "fallback - malformed URL returns original",
			baseURL:  "invalid-url",
			username: "user",
			password: "pass",
			want:     "invalid-url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newMockLogger()
			gen := NewBaseGenerator(logger)

			result := gen.BuildConnectionURL(tt.baseURL, tt.username, tt.password)

			assert.Equal(t, tt.want, result)
		})
	}
}

func TestBaseGenerator_BuildConnectionURL_CredentialsPresent(t *testing.T) {
	t.Run("verifies credentials are in the URL", func(t *testing.T) {
		logger := newMockLogger()
		gen := NewBaseGenerator(logger)

		baseURL := "postgres://localhost:5432/testdb"
		username := "testuser"
		password := "testpass"

		result := gen.BuildConnectionURL(baseURL, username, password)

		assert.Contains(t, result, username)
		assert.Contains(t, result, password)
		assert.Contains(t, result, "@")
	})
}
