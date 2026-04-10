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

package secrets

import (
	"testing"
)

// TestParseDEKID_Valid tests parsing valid DEKID strings.
func TestParseDEKID_Valid(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected DEKID
	}{
		{
			name:     "global DEKID",
			input:    "dek:global",
			expected: DEKIDGlobal,
		},
		{
			name:     "tenant DEKID with simple ID",
			input:    "dek:tenant:abc123",
			expected: DEKID("dek:tenant:abc123"),
		},
		{
			name:     "tenant DEKID with UUID",
			input:    "dek:tenant:550e8400-e29b-41d4-a716-446655440000",
			expected: DEKID("dek:tenant:550e8400-e29b-41d4-a716-446655440000"),
		},
		{
			name:     "tenant DEKID with complex ID",
			input:    "dek:tenant:org-123-prod",
			expected: DEKID("dek:tenant:org-123-prod"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDEKID(tt.input)
			if err != nil {
				t.Fatalf("ParseDEKID(%q) returned unexpected error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("ParseDEKID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestParseDEKID_Invalid tests parsing invalid DEKID strings.
func TestParseDEKID_Invalid(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr error
	}{
		{
			name:        "empty string",
			input:       "",
			expectedErr: ErrEmptyDEKID,
		},
		{
			name:        "whitespace only",
			input:       "   ",
			expectedErr: ErrEmptyDEKID,
		},
		{
			name:        "missing dek prefix",
			input:       "global",
			expectedErr: ErrInvalidDEKIDFormat,
		},
		{
			name:        "wrong prefix",
			input:       "kek:global",
			expectedErr: ErrInvalidDEKIDFormat,
		},
		{
			name:        "global with extra parts",
			input:       "dek:global:extra",
			expectedErr: ErrInvalidDEKIDFormat,
		},
		{
			name:        "tenant without ID",
			input:       "dek:tenant:",
			expectedErr: ErrInvalidTenantID,
		},
		{
			name:        "tenant with empty ID after trim",
			input:       "dek:tenant:   ",
			expectedErr: ErrInvalidTenantID,
		},
		{
			name:        "invalid scope",
			input:       "dek:invalid",
			expectedErr: ErrInvalidDEKIDFormat,
		},
		{
			name:        "only dek prefix",
			input:       "dek:",
			expectedErr: ErrInvalidDEKIDFormat,
		},
		{
			name:        "malformed tenant format",
			input:       "dek:tenant",
			expectedErr: ErrInvalidDEKIDFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDEKID(tt.input)
			if err == nil {
				t.Fatalf("ParseDEKID(%q) expected error %v, but got result: %q", tt.input, tt.expectedErr, result)
			}
			if err != tt.expectedErr {
				t.Errorf("ParseDEKID(%q) error = %v, want %v", tt.input, err, tt.expectedErr)
			}
		})
	}
}

// TestIsDEKID tests the IsDEKID validation function.
func TestIsDEKID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid global DEKID",
			input:    "dek:global",
			expected: true,
		},
		{
			name:     "valid tenant DEKID",
			input:    "dek:tenant:abc123",
			expected: true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "no dek prefix",
			input:    "global",
			expected: false,
		},
		{
			name:     "wrong prefix",
			input:    "kek:global",
			expected: false,
		},
		{
			name:     "partial prefix",
			input:    "de:global",
			expected: false,
		},
		{
			name:     "dek in middle",
			input:    "prefix:dek:global",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDEKID(tt.input)
			if result != tt.expected {
				t.Errorf("IsDEKID(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestNewTenantDEKID tests creating tenant DEKIDs.
func TestNewTenantDEKID(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		expected  DEKID
		shouldErr bool
	}{
		{
			name:      "valid tenant ID",
			tenantID:  "tenant-123",
			expected:  DEKID("dek:tenant:tenant-123"),
			shouldErr: false,
		},
		{
			name:      "UUID tenant ID",
			tenantID:  "550e8400-e29b-41d4-a716-446655440000",
			expected:  DEKID("dek:tenant:550e8400-e29b-41d4-a716-446655440000"),
			shouldErr: false,
		},
		{
			name:      "empty tenant ID",
			tenantID:  "",
			expected:  "",
			shouldErr: true,
		},
		{
			name:      "whitespace tenant ID",
			tenantID:  "   ",
			expected:  "",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Using defer/recover to catch panics if implementation uses panic
			// But we expect proper error handling
			result := NewTenantDEKID(tt.tenantID)

			if tt.shouldErr {
				// Empty result indicates error condition
				if result != "" {
					t.Errorf("NewTenantDEKID(%q) expected empty result for invalid input, got %q", tt.tenantID, result)
				}
			} else {
				if result != tt.expected {
					t.Errorf("NewTenantDEKID(%q) = %q, want %q", tt.tenantID, result, tt.expected)
				}
			}
		})
	}
}

// TestGetTenantID tests extracting tenant ID from DEKID.
func TestGetTenantID(t *testing.T) {
	tests := []struct {
		name       string
		dekid      DEKID
		expectedID string
		expectedOK bool
	}{
		{
			name:       "tenant DEKID",
			dekid:      DEKID("dek:tenant:abc123"),
			expectedID: "abc123",
			expectedOK: true,
		},
		{
			name:       "tenant DEKID with UUID",
			dekid:      DEKID("dek:tenant:550e8400-e29b-41d4-a716-446655440000"),
			expectedID: "550e8400-e29b-41d4-a716-446655440000",
			expectedOK: true,
		},
		{
			name:       "global DEKID",
			dekid:      DEKIDGlobal,
			expectedID: "",
			expectedOK: false,
		},
		{
			name:       "invalid format",
			dekid:      DEKID("invalid"),
			expectedID: "",
			expectedOK: false,
		},
		{
			name:       "empty DEKID",
			dekid:      DEKID(""),
			expectedID: "",
			expectedOK: false,
		},
		{
			name:       "malformed tenant DEKID",
			dekid:      DEKID("dek:tenant:"),
			expectedID: "",
			expectedOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := GetTenantID(tt.dekid)
			if ok != tt.expectedOK {
				t.Errorf("GetTenantID(%q) ok = %v, want %v", tt.dekid, ok, tt.expectedOK)
			}
			if id != tt.expectedID {
				t.Errorf("GetTenantID(%q) id = %q, want %q", tt.dekid, id, tt.expectedID)
			}
		})
	}
}

// TestIsGlobal tests checking if DEKID is global.
func TestIsGlobal(t *testing.T) {
	tests := []struct {
		name     string
		dekid    DEKID
		expected bool
	}{
		{
			name:     "global DEKID",
			dekid:    DEKIDGlobal,
			expected: true,
		},
		{
			name:     "global DEKID uppercase",
			dekid:    DEKID("dek:global"),
			expected: true,
		},
		{
			name:     "tenant DEKID",
			dekid:    DEKID("dek:tenant:abc123"),
			expected: false,
		},
		{
			name:     "invalid DEKID",
			dekid:    DEKID("invalid"),
			expected: false,
		},
		{
			name:     "empty DEKID",
			dekid:    DEKID(""),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsGlobal(tt.dekid)
			if result != tt.expected {
				t.Errorf("IsGlobal(%q) = %v, want %v", tt.dekid, result, tt.expected)
			}
		})
	}
}

// TestIsTenant tests checking if DEKID is a tenant DEKID.
func TestIsTenant(t *testing.T) {
	tests := []struct {
		name     string
		dekid    DEKID
		expected bool
	}{
		{
			name:     "tenant DEKID",
			dekid:    DEKID("dek:tenant:abc123"),
			expected: true,
		},
		{
			name:     "tenant DEKID with UUID",
			dekid:    DEKID("dek:tenant:550e8400-e29b-41d4-a716-446655440000"),
			expected: true,
		},
		{
			name:     "global DEKID",
			dekid:    DEKIDGlobal,
			expected: false,
		},
		{
			name:     "invalid DEKID",
			dekid:    DEKID("invalid"),
			expected: false,
		},
		{
			name:     "empty DEKID",
			dekid:    DEKID(""),
			expected: false,
		},
		{
			name:     "tenant prefix but malformed",
			dekid:    DEKID("dek:tenant"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTenant(tt.dekid)
			if result != tt.expected {
				t.Errorf("IsTenant(%q) = %v, want %v", tt.dekid, result, tt.expected)
			}
		})
	}
}

// TestDEKID_String tests the String method.
func TestDEKID_String(t *testing.T) {
	tests := []struct {
		name     string
		dekid    DEKID
		expected string
	}{
		{
			name:     "global DEKID",
			dekid:    DEKIDGlobal,
			expected: "dek:global",
		},
		{
			name:     "tenant DEKID",
			dekid:    DEKID("dek:tenant:abc123"),
			expected: "dek:tenant:abc123",
		},
		{
			name:     "empty DEKID",
			dekid:    DEKID(""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.dekid.String()
			if result != tt.expected {
				t.Errorf("DEKID.String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestDEKID_RoundTrip tests parsing and converting back to string.
func TestDEKID_RoundTrip(t *testing.T) {
	tests := []string{
		"dek:global",
		"dek:tenant:abc123",
		"dek:tenant:550e8400-e29b-41d4-a716-446655440000",
		"dek:tenant:org-prod-123",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			dekid, err := ParseDEKID(tt)
			if err != nil {
				t.Fatalf("ParseDEKID(%q) error: %v", tt, err)
			}
			result := dekid.String()
			if result != tt {
				t.Errorf("Round trip: %q -> %q, want %q", tt, result, tt)
			}
		})
	}
}

// TestDEKID_EdgeCases tests edge cases and boundary conditions.
func TestDEKID_EdgeCases(t *testing.T) {
	t.Run("tenant ID with colons", func(t *testing.T) {
		// Tenant IDs with colons should work as the format is dek:tenant:{everything-else}
		input := "dek:tenant:org:team:id"
		dekid, err := ParseDEKID(input)
		if err != nil {
			t.Fatalf("ParseDEKID(%q) error: %v", input, err)
		}

		tenantID, ok := GetTenantID(dekid)
		if !ok {
			t.Errorf("GetTenantID(%q) should return true for tenant DEKID", dekid)
		}
		expected := "org:team:id"
		if tenantID != expected {
			t.Errorf("GetTenantID(%q) = %q, want %q", dekid, tenantID, expected)
		}
	})

	t.Run("case sensitivity", func(t *testing.T) {
		// DEKIDs should be case-sensitive
		_, err := ParseDEKID("DEK:GLOBAL")
		if err == nil {
			t.Error("ParseDEKID(\"DEK:GLOBAL\") should return error for uppercase prefix")
		}
	})

	t.Run("special characters in tenant ID", func(t *testing.T) {
		specialChars := []string{
			"dek:tenant:id-with-dashes",
			"dek:tenant:id_with_underscores",
			"dek:tenant:id.with.dots",
		}

		for _, input := range specialChars {
			dekid, err := ParseDEKID(input)
			if err != nil {
				t.Errorf("ParseDEKID(%q) should accept special characters, got error: %v", input, err)
			}
			if !IsTenant(dekid) {
				t.Errorf("IsTenant(%q) should be true", dekid)
			}
		}
	})
}

// TestDEKIDGlobal_Constant tests the global constant is correctly defined.
func TestDEKIDGlobal_Constant(t *testing.T) {
	if DEKIDGlobal != DEKID("dek:global") {
		t.Errorf("DEKIDGlobal = %q, want %q", DEKIDGlobal, "dek:global")
	}

	if !IsGlobal(DEKIDGlobal) {
		t.Error("IsGlobal(DEKIDGlobal) should be true")
	}

	if IsTenant(DEKIDGlobal) {
		t.Error("IsTenant(DEKIDGlobal) should be false")
	}
}

// TestDatabaseDEKID tests creating database-specific DEKIDs.
func TestDatabaseDEKID(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		dbName   string
		expected DEKID
	}{
		{
			name:     "valid database DEKID",
			tenantID: "tenant-123",
			dbName:   "mydb",
			expected: DEKID("dek:tenant:tenant-123:db:mydb"),
		},
		{
			name:     "UUID tenant with database",
			tenantID: "550e8400-e29b-41d4-a716-446655440000",
			dbName:   "analytics",
			expected: DEKID("dek:tenant:550e8400-e29b-41d4-a716-446655440000:db:analytics"),
		},
		{
			name:     "empty tenant ID returns empty",
			tenantID: "",
			dbName:   "mydb",
			expected: "",
		},
		{
			name:     "empty db name returns empty",
			tenantID: "tenant-123",
			dbName:   "",
			expected: "",
		},
		{
			name:     "whitespace tenant ID returns empty",
			tenantID: "   ",
			dbName:   "mydb",
			expected: "",
		},
		{
			name:     "whitespace db name returns empty",
			tenantID: "tenant-123",
			dbName:   "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DatabaseDEKID(tt.tenantID, tt.dbName)
			if result != tt.expected {
				t.Errorf("DatabaseDEKID(%q, %q) = %q, want %q", tt.tenantID, tt.dbName, result, tt.expected)
			}
		})
	}
}

// TestParseDatabaseDEKID tests extracting tenantID and dbName from database DEK IDs.
func TestParseDatabaseDEKID(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantTenantID string
		wantDBName   string
		wantErr      error
	}{
		{
			name:         "valid database DEKID",
			input:        "dek:tenant:tenant-123:db:mydb",
			wantTenantID: "tenant-123",
			wantDBName:   "mydb",
		},
		{
			name:         "UUID tenant with database",
			input:        "dek:tenant:550e8400-e29b-41d4-a716-446655440000:db:analytics",
			wantTenantID: "550e8400-e29b-41d4-a716-446655440000",
			wantDBName:   "analytics",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: ErrEmptyDEKID,
		},
		{
			name:    "global DEKID is not database",
			input:   "dek:global",
			wantErr: ErrInvalidDatabaseDEKIDFormat,
		},
		{
			name:    "tenant DEKID without db scope",
			input:   "dek:tenant:tenant-123",
			wantErr: ErrInvalidDatabaseDEKIDFormat,
		},
		{
			name:    "wrong prefix",
			input:   "kek:tenant:tenant-123:db:mydb",
			wantErr: ErrInvalidDatabaseDEKIDFormat,
		},
		{
			name:    "empty tenant ID in database DEKID",
			input:   "dek:tenant::db:mydb",
			wantErr: ErrInvalidTenantID,
		},
		{
			name:    "empty db name in database DEKID",
			input:   "dek:tenant:tenant-123:db:",
			wantErr: ErrInvalidDBName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenantID, dbName, err := ParseDatabaseDEKID(tt.input)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("ParseDatabaseDEKID(%q) expected error %v, got nil", tt.input, tt.wantErr)
				}
				if err != tt.wantErr {
					t.Errorf("ParseDatabaseDEKID(%q) error = %v, want %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDatabaseDEKID(%q) unexpected error: %v", tt.input, err)
			}
			if tenantID != tt.wantTenantID {
				t.Errorf("ParseDatabaseDEKID(%q) tenantID = %q, want %q", tt.input, tenantID, tt.wantTenantID)
			}
			if dbName != tt.wantDBName {
				t.Errorf("ParseDatabaseDEKID(%q) dbName = %q, want %q", tt.input, dbName, tt.wantDBName)
			}
		})
	}
}

// TestIsDatabase tests checking if DEKID is a database-specific DEKID.
func TestIsDatabase(t *testing.T) {
	tests := []struct {
		name     string
		dekid    DEKID
		expected bool
	}{
		{
			name:     "valid database DEKID",
			dekid:    DEKID("dek:tenant:tenant-123:db:mydb"),
			expected: true,
		},
		{
			name:     "tenant DEKID is not database",
			dekid:    DEKID("dek:tenant:tenant-123"),
			expected: false,
		},
		{
			name:     "global DEKID is not database",
			dekid:    DEKIDGlobal,
			expected: false,
		},
		{
			name:     "empty DEKID",
			dekid:    DEKID(""),
			expected: false,
		},
		{
			name:     "invalid format",
			dekid:    DEKID("invalid"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDatabase(tt.dekid)
			if result != tt.expected {
				t.Errorf("IsDatabase(%q) = %v, want %v", tt.dekid, result, tt.expected)
			}
		})
	}
}

// TestGetDatabaseInfo tests extracting database info from database DEKIDs.
func TestGetDatabaseInfo(t *testing.T) {
	tests := []struct {
		name         string
		dekid        DEKID
		wantTenantID string
		wantDBName   string
		wantOK       bool
	}{
		{
			name:         "valid database DEKID",
			dekid:        DEKID("dek:tenant:tenant-123:db:mydb"),
			wantTenantID: "tenant-123",
			wantDBName:   "mydb",
			wantOK:       true,
		},
		{
			name:   "tenant DEKID returns false",
			dekid:  DEKID("dek:tenant:tenant-123"),
			wantOK: false,
		},
		{
			name:   "global DEKID returns false",
			dekid:  DEKIDGlobal,
			wantOK: false,
		},
		{
			name:   "empty DEKID returns false",
			dekid:  DEKID(""),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenantID, dbName, ok := GetDatabaseInfo(tt.dekid)
			if ok != tt.wantOK {
				t.Errorf("GetDatabaseInfo(%q) ok = %v, want %v", tt.dekid, ok, tt.wantOK)
			}
			if ok {
				if tenantID != tt.wantTenantID {
					t.Errorf("GetDatabaseInfo(%q) tenantID = %q, want %q", tt.dekid, tenantID, tt.wantTenantID)
				}
				if dbName != tt.wantDBName {
					t.Errorf("GetDatabaseInfo(%q) dbName = %q, want %q", tt.dekid, dbName, tt.wantDBName)
				}
			}
		})
	}
}

// TestParseDEKID_DatabaseFormat tests parsing database-format DEKIDs.
func TestParseDEKID_DatabaseFormat(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    DEKID
		wantErr     bool
		expectedErr error
	}{
		{
			name:     "valid database DEKID",
			input:    "dek:tenant:tenant-123:db:mydb",
			expected: DEKID("dek:tenant:tenant-123:db:mydb"),
		},
		{
			name:     "database DEKID with UUID tenant",
			input:    "dek:tenant:550e8400-e29b-41d4-a716-446655440000:db:analytics",
			expected: DEKID("dek:tenant:550e8400-e29b-41d4-a716-446655440000:db:analytics"),
		},
		{
			name:        "database DEKID with empty tenant",
			input:       "dek:tenant::db:mydb",
			wantErr:     true,
			expectedErr: ErrInvalidTenantID,
		},
		{
			name:        "database DEKID with empty db name",
			input:       "dek:tenant:tenant-123:db:",
			wantErr:     true,
			expectedErr: ErrInvalidDBName,
		},
		{
			name:        "trailing db without name",
			input:       "dek:tenant:tenant-123:db",
			wantErr:     true,
			expectedErr: ErrInvalidDBName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDEKID(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDEKID(%q) expected error, got result: %q", tt.input, result)
				}
				if tt.expectedErr != nil && err != tt.expectedErr {
					t.Errorf("ParseDEKID(%q) error = %v, want %v", tt.input, err, tt.expectedErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDEKID(%q) unexpected error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("ParseDEKID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestGetTenantID_DatabaseDEKID tests extracting tenant ID from a database DEKID.
func TestGetTenantID_DatabaseDEKID(t *testing.T) {
	dekid := DEKID("dek:tenant:tenant-123:db:mydb")
	tenantID, ok := GetTenantID(dekid)
	if !ok {
		t.Fatal("GetTenantID should return true for database DEKID")
	}
	if tenantID != "tenant-123" {
		t.Errorf("GetTenantID(%q) = %q, want %q", dekid, tenantID, "tenant-123")
	}
}

// TestIsTenant_DatabaseDEKID tests that database DEKIDs are not classified as tenant-only.
func TestIsTenant_DatabaseDEKID(t *testing.T) {
	dekid := DEKID("dek:tenant:tenant-123:db:mydb")
	if IsTenant(dekid) {
		t.Errorf("IsTenant(%q) should be false for database DEKID", dekid)
	}
}

// TestDatabaseDEKID_RoundTrip tests creating and parsing database DEKIDs.
func TestDatabaseDEKID_RoundTrip(t *testing.T) {
	tenantID := "tenant-456"
	dbName := "userdata"

	dekid := DatabaseDEKID(tenantID, dbName)
	if dekid == "" {
		t.Fatal("DatabaseDEKID returned empty DEKID")
	}

	// Parse it back
	parsedTenantID, parsedDBName, err := ParseDatabaseDEKID(string(dekid))
	if err != nil {
		t.Fatalf("ParseDatabaseDEKID(%q) error: %v", dekid, err)
	}

	if parsedTenantID != tenantID {
		t.Errorf("Round trip tenantID = %q, want %q", parsedTenantID, tenantID)
	}
	if parsedDBName != dbName {
		t.Errorf("Round trip dbName = %q, want %q", parsedDBName, dbName)
	}

	// Also validate via ParseDEKID
	parsed, err := ParseDEKID(string(dekid))
	if err != nil {
		t.Fatalf("ParseDEKID(%q) error: %v", dekid, err)
	}
	if parsed != dekid {
		t.Errorf("ParseDEKID round trip = %q, want %q", parsed, dekid)
	}

	// Should be database, not tenant-only
	if !IsDatabase(dekid) {
		t.Error("IsDatabase should return true")
	}
	if IsTenant(dekid) {
		t.Error("IsTenant should return false for database DEKID")
	}
}
