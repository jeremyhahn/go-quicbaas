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
	"errors"
	"testing"
)

func TestSystemDatabaseName(t *testing.T) {
	got := SystemDatabaseName("tenant-abc")
	want := "tenant-abc_system"
	if got != want {
		t.Errorf("SystemDatabaseName() = %q, want %q", got, want)
	}
}

func TestSystemDatabaseName_Empty(t *testing.T) {
	got := SystemDatabaseName("")
	want := "_system"
	if got != want {
		t.Errorf("SystemDatabaseName(\"\") = %q, want %q", got, want)
	}
}

func TestTenantDatabaseName(t *testing.T) {
	got := TenantDatabaseName("tenant-abc", "my_app")
	want := "tenant-abc_my_app"
	if got != want {
		t.Errorf("TenantDatabaseName() = %q, want %q", got, want)
	}
}

func TestTenantDatabaseName_Empty(t *testing.T) {
	got := TenantDatabaseName("", "db")
	want := "_db"
	if got != want {
		t.Errorf("TenantDatabaseName(\"\", \"db\") = %q, want %q", got, want)
	}
}

func TestRecordKey(t *testing.T) {
	got := RecordKey("my_app")
	want := "sys:db:my_app"
	if got != want {
		t.Errorf("RecordKey() = %q, want %q", got, want)
	}
}

func TestRecordKey_Empty(t *testing.T) {
	got := RecordKey("")
	want := "sys:db:"
	if got != want {
		t.Errorf("RecordKey(\"\") = %q, want %q", got, want)
	}
}

func TestPlatformTenantKey(t *testing.T) {
	got := PlatformTenantKey("tenant-123")
	want := "platform:tenant:tenant-123"
	if got != want {
		t.Errorf("PlatformTenantKey() = %q, want %q", got, want)
	}
}

func TestPlatformTenantKey_Empty(t *testing.T) {
	got := PlatformTenantKey("")
	want := "platform:tenant:"
	if got != want {
		t.Errorf("PlatformTenantKey(\"\") = %q, want %q", got, want)
	}
}

// ShardAllocator was removed in Phase D: shard allocation is now owned by
// qrdb's DatabaseServicer. Tests for the removed type are deleted here.

func TestDatabaseRecord_Fields(t *testing.T) {
	r := DatabaseRecord{
		Name:     "my_app",
		TenantID: "tenant-abc",
		Engine:   "kv",
		Backend:  "pebble",
		Purpose:  PurposeUser,
	}
	if r.Name != "my_app" {
		t.Errorf("Name = %q, want %q", r.Name, "my_app")
	}
	if r.Purpose != PurposeUser {
		t.Errorf("Purpose = %q, want %q", r.Purpose, PurposeUser)
	}
}

func TestDatabaseRecord_ZeroValue(t *testing.T) {
	var r DatabaseRecord
	if r.Name != "" {
		t.Errorf("zero Name = %q, want empty", r.Name)
	}
	if r.Purpose != "" {
		t.Errorf("zero Purpose = %q, want empty", r.Purpose)
	}
}

// Error type tests
func TestTenantProvisionError(t *testing.T) {
	inner := errors.New("storage failure")
	err := &TenantProvisionError{TenantID: "t1", Err: inner}
	if got := err.Error(); got == "" {
		t.Error("Error() is empty")
	}
	if err.Unwrap() != inner {
		t.Error("Unwrap() did not return inner error")
	}
}

func TestTenantProvisionError_Message(t *testing.T) {
	inner := errors.New("disk full")
	err := &TenantProvisionError{TenantID: "tenant-xyz", Err: inner}
	got := err.Error()
	want := `database: failed to provision tenant "tenant-xyz": disk full`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestTenantDeprovisionError(t *testing.T) {
	inner := errors.New("cleanup failure")
	err := &TenantDeprovisionError{TenantID: "t1", Err: inner}
	if got := err.Error(); got == "" {
		t.Error("Error() is empty")
	}
	if err.Unwrap() != inner {
		t.Error("Unwrap() did not return inner error")
	}
}

func TestTenantDeprovisionError_Message(t *testing.T) {
	inner := errors.New("timeout")
	err := &TenantDeprovisionError{TenantID: "tenant-xyz", Err: inner}
	got := err.Error()
	want := `database: failed to deprovision tenant "tenant-xyz": timeout`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestDatabaseLimitExceededError(t *testing.T) {
	err := &DatabaseLimitExceededError{TenantID: "t1", Max: 10, Current: 10}
	if got := err.Error(); got == "" {
		t.Error("Error() is empty")
	}
}

func TestDatabaseLimitExceededError_Message(t *testing.T) {
	err := &DatabaseLimitExceededError{TenantID: "t1", Max: 50, Current: 50}
	got := err.Error()
	want := `database: tenant "t1" has reached the maximum of 50 databases`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestTenantAlreadyProvisionedError(t *testing.T) {
	err := &TenantAlreadyProvisionedError{TenantID: "t1"}
	if got := err.Error(); got == "" {
		t.Error("Error() is empty")
	}
}

func TestTenantAlreadyProvisionedError_Message(t *testing.T) {
	err := &TenantAlreadyProvisionedError{TenantID: "tenant-abc"}
	got := err.Error()
	want := `database: tenant "tenant-abc" is already provisioned`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestInvalidDatabasePurposeError(t *testing.T) {
	err := &InvalidDatabasePurposeError{Purpose: "bad"}
	if got := err.Error(); got == "" {
		t.Error("Error() is empty")
	}
}

func TestInvalidDatabasePurposeError_Message(t *testing.T) {
	err := &InvalidDatabasePurposeError{Purpose: "unknown"}
	got := err.Error()
	want := `database: invalid database purpose: "unknown"`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestConstants(t *testing.T) {
	if PurposeSystem != "system" {
		t.Errorf("PurposeSystem = %q, want %q", PurposeSystem, "system")
	}
	if PurposeUser != "user" {
		t.Errorf("PurposeUser = %q, want %q", PurposeUser, "user")
	}
	if SystemDatabaseSuffix != "_system" {
		t.Errorf("SystemDatabaseSuffix = %q, want %q", SystemDatabaseSuffix, "_system")
	}
}

func TestConstants_Prefixes(t *testing.T) {
	if RecordKeyPrefix != "sys:db:" {
		t.Errorf("RecordKeyPrefix = %q, want %q", RecordKeyPrefix, "sys:db:")
	}
	if PlatformTenantKeyPrefix != "platform:tenant:" {
		t.Errorf("PlatformTenantKeyPrefix = %q, want %q", PlatformTenantKeyPrefix, "platform:tenant:")
	}
}
