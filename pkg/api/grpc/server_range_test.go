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

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestServerHandlers_OutOfRange proves that values outside their wire or
// service range fail with a status naming the field: InvalidArgument for
// request input, Internal for data the service reported.
func TestServerHandlers_OutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *testutil.MockQuicBaaS)
		call  func(s *Server) error
		code  codes.Code
		field string
	}{
		{
			name: "ListEntityTypes total_count",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("ListEntityTypes", mock.Anything, mock.Anything).
					Return(&service.ListEntityTypesResponse{TotalCount: -1}, nil)
			},
			call: func(s *Server) error {
				_, err := s.ListEntityTypes(context.Background(), &pb.ListEntityTypesRequest{})
				return err
			},
			code:  codes.Internal,
			field: "total_count",
		},
		{
			name: "CreateBackup size",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("CreateBackup", mock.Anything, mock.Anything).
					Return(&service.CreateBackupResponse{Size: math.MaxUint64}, nil)
			},
			call: func(s *Server) error {
				_, err := s.CreateBackup(context.Background(), &pb.CreateBackupRequest{})
				return err
			},
			code:  codes.Internal,
			field: "size",
		},
		{
			name: "ListBackups backups.size",
			setup: func(m *testutil.MockQuicBaaS) {
				m.On("ListBackups", mock.Anything, mock.Anything).
					Return(&service.ListBackupsResponse{
						Backups: []*service.BackupMetadata{{Size: math.MaxUint64}},
					}, nil)
			},
			call: func(s *Server) error {
				_, err := s.ListBackups(context.Background(), &pb.ListBackupsRequest{})
				return err
			},
			code:  codes.Internal,
			field: "backups.size",
		},
		{
			name:  "ListBackups limit",
			setup: func(*testutil.MockQuicBaaS) {},
			call: func(s *Server) error {
				_, err := s.ListBackups(context.Background(), &pb.ListBackupsRequest{Limit: -1})
				return err
			},
			code:  codes.InvalidArgument,
			field: "limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, m := newMockTestServer(t)
			tt.setup(m)

			requireRangeStatus(t, tt.call(srv), tt.code, tt.field)
		})
	}
}
