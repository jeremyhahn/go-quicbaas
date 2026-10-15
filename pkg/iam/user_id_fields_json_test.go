// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"encoding/json"
	"strings"
	"testing"
)

// aboveFloat64Range is 2^53 + 1: the smallest id a float64-based JSON decoder
// would round, so it proves the field travels as a decimal string.
const aboveFloat64Range uint64 = 1<<53 + 1

// assertUserIDFieldRoundTrips marshals value, checks that key carries the id
// as a decimal string, decodes it back, and checks that a bare JSON number
// under the same key is refused.
func assertUserIDFieldRoundTrips[T any](t *testing.T, value T, key string, get func(*T) uint64) {
	t.Helper()

	data, err := json.Marshal(&value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	quoted := `"` + key + `":"9007199254740993"`
	if !strings.Contains(string(data), quoted) {
		t.Fatalf("encoded %s, want %s", data, quoted)
	}

	var back T
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := get(&back); got != aboveFloat64Range {
		t.Errorf("round-tripped %s = %d, want %d", key, got, aboveFloat64Range)
	}

	bare := strings.Replace(string(data), quoted, `"`+key+`":9007199254740993`, 1)
	var refused T
	if err := json.Unmarshal([]byte(bare), &refused); err == nil {
		t.Errorf("a bare-number %s decoded without error; want it refused", key)
	}
}

func TestUserIDFields_JSONCarryTheIDAsADecimalString(t *testing.T) {
	id := aboveFloat64Range

	t.Run("AdminAssignment.UserID", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, AdminAssignment{UserID: id}, "user_id",
			func(v *AdminAssignment) uint64 { return v.UserID })
	})
	t.Run("AdminCheckRequest.UserID", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, AdminCheckRequest{UserID: id}, "user_id",
			func(v *AdminCheckRequest) uint64 { return v.UserID })
	})
	t.Run("ImpersonationSession.AdminUserID", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, ImpersonationSession{AdminUserID: id, TargetUserID: 1}, "admin_user_id",
			func(v *ImpersonationSession) uint64 { return v.AdminUserID })
	})
	t.Run("ImpersonationSession.TargetUserID", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, ImpersonationSession{AdminUserID: 1, TargetUserID: id}, "target_user_id",
			func(v *ImpersonationSession) uint64 { return v.TargetUserID })
	})
	t.Run("CreateAdminAssignmentOptions.UserID", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, CreateAdminAssignmentOptions{UserID: id}, "user_id",
			func(v *CreateAdminAssignmentOptions) uint64 { return v.UserID })
	})
	t.Run("ListAdminAssignmentsOptions.UserID", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, ListAdminAssignmentsOptions{UserID: id}, "user_id",
			func(v *ListAdminAssignmentsOptions) uint64 { return v.UserID })
	})
	t.Run("StartImpersonationOptions.TargetUserID", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, StartImpersonationOptions{TargetUserID: id}, "target_user_id",
			func(v *StartImpersonationOptions) uint64 { return v.TargetUserID })
	})
	t.Run("DataExportRequest.RequestedBy", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, DataExportRequest{UserID: 1, RequestedBy: id}, "requested_by",
			func(v *DataExportRequest) uint64 { return v.RequestedBy })
	})
	t.Run("DataDeletionRequest.RequestedBy", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, DataDeletionRequest{UserID: 1, RequestedBy: id}, "requested_by",
			func(v *DataDeletionRequest) uint64 { return v.RequestedBy })
	})
	t.Run("CreateDataExportOptions.RequestedBy", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, CreateDataExportOptions{UserID: 1, RequestedBy: id}, "requested_by",
			func(v *CreateDataExportOptions) uint64 { return v.RequestedBy })
	})
	t.Run("CreateDeletionRequestOptions.RequestedBy", func(t *testing.T) {
		assertUserIDFieldRoundTrips(t, CreateDeletionRequestOptions{UserID: 1, RequestedBy: id}, "requested_by",
			func(v *CreateDeletionRequestOptions) uint64 { return v.RequestedBy })
	})
}
