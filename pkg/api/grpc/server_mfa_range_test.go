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
	"context"
	"math"
	"testing"

	"google.golang.org/grpc/codes"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestMFAHandlers_ResponseOutOfRange proves that a backup code count too
// large for its int32 wire field becomes codes.Internal instead of a wrapped
// integer.
func TestMFAHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	backupStatus := func(total, remaining int) *mockMFAService {
		return &mockMFAService{
			getBackupCodeStatusFn: func(context.Context, string, string) (*iam.BackupCodeStatus, error) {
				return &iam.BackupCodeStatus{Total: total, Remaining: remaining}, nil
			},
		}
	}
	getBackupCodeStatus := func(srv *MFAServer) error {
		_, err := srv.GetBackupCodeStatus(ctx, &iampb.GetBackupCodeStatusRequest{TenantId: "t1", UserId: "u1"})
		return err
	}

	tests := []struct {
		name  string
		field string
		svc   *mockMFAService
		call  func(srv *MFAServer) error
	}{
		{
			name:  "GetStatus backup codes remaining",
			field: "backup_codes_remaining",
			svc: &mockMFAService{
				getMFAStatusFn: func(context.Context, string, string) (*iam.MFAStatus, error) {
					return &iam.MFAStatus{BackupCodesRemaining: tooBig}, nil
				},
			},
			call: func(srv *MFAServer) error {
				_, err := srv.GetStatus(ctx, &iampb.GetMFAStatusRequest{TenantId: "t1", UserId: "u1"})
				return err
			},
		},
		{
			name:  "VerifyBackupCode remaining codes",
			field: "remaining_codes",
			svc:   backupStatus(10, tooBig),
			call: func(srv *MFAServer) error {
				_, err := srv.VerifyBackupCode(ctx, &iampb.VerifyBackupCodeRequest{TenantId: "t1", UserId: "u1", Code: "c"})
				return err
			},
		},
		{name: "GetBackupCodeStatus total codes", field: "total_codes", svc: backupStatus(tooBig, 5), call: getBackupCodeStatus},
		{name: "GetBackupCodeStatus remaining codes", field: "remaining_codes", svc: backupStatus(10, tooBig), call: getBackupCodeStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireRangeStatus(t, tt.call(newTestMFAServer(tt.svc)), codes.Internal, tt.field)
		})
	}
}
