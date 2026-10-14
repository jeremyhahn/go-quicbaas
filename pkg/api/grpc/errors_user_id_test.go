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

package grpc

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

func TestParseUserID_AcceptsTheFullUint64Range(t *testing.T) {
	for _, want := range []uint64{1, 1 << 53, (1 << 53) + 1, math.MaxUint64} {
		got, err := parseUserID("user_id", strconv.FormatUint(want, 10))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseUserID_RefusesANonID(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "non-numeric", value: "user-1", wantErr: strconv.ErrSyntax},
		{name: "negative", value: "-1", wantErr: strconv.ErrSyntax},
		{name: "overflowing", value: "18446744073709551616", wantErr: strconv.ErrRange},
		{name: "empty", value: "", wantErr: strconv.ErrSyntax},
		{name: "zero", value: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUserID("user_id", tt.value)
			assert.Zero(t, got)

			var idErr *UserIDError
			require.ErrorAs(t, err, &idErr)
			assert.Equal(t, "user_id", idErr.Field)
			assert.Equal(t, tt.value, idErr.Value)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, errors.Unwrap(err))
			}
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestParseOptionalUserID_TreatsEmptyAsUnset(t *testing.T) {
	got, err := parseOptionalUserID("user_id", "")
	require.NoError(t, err)
	assert.Zero(t, got)

	got, err = parseOptionalUserID("user_id", "42")
	require.NoError(t, err)
	assert.Equal(t, uint64(42), got)
}

func TestParseOptionalUserID_RefusesANonID(t *testing.T) {
	for _, value := range []string{"user-1", "18446744073709551616", "0"} {
		_, err := parseOptionalUserID("user_id", value)
		var idErr *UserIDError
		require.ErrorAs(t, err, &idErr, "value %q", value)
		assert.Equal(t, value, idErr.Value)
	}
}

func TestFormatUserID_RendersZeroAsUnset(t *testing.T) {
	assert.Empty(t, formatUserID(0))
	assert.Equal(t, "18446744073709551615", formatUserID(math.MaxUint64))
}

// TestIAMServer_RefusesANonNumericUserIDBeforeTheService proves the edge
// rejects a malformed id with InvalidArgument and never reaches the service.
func TestIAMServer_RefusesANonNumericUserIDBeforeTheService(t *testing.T) {
	srv, svc := newTestIAMServer(t)

	_, err := srv.UpdateUser(callerContext("t-1"), &iampb.UpdateUserRequest{
		TenantId: "t-1", UserId: "18446744073709551616", Email: "a@b.c",
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	svc.AssertNotCalled(t, "UpdateUser")
}
