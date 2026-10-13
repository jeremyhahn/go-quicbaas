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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestDEKHandlers_ResponseOutOfRange proves that a DEK warning threshold too
// large for its int32 wire field becomes codes.Internal on every handler that
// returns DEK metadata, instead of a wrapped integer.
func TestDEKHandlers_ResponseOutOfRange(t *testing.T) {
	ctx := context.Background()
	hugeDEK := func() *service.DEKMetadata {
		return &service.DEKMetadata{Path: "/app/data", WarningPercent: math.MaxInt32 + 1}
	}

	tests := []struct {
		name string
		svc  *mockKMSService
		call func(srv *DEKServer) error
	}{
		{
			name: "CreateDEK",
			svc: &mockKMSService{
				createDEKFunc: func(context.Context, *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
					return &service.CreateDEKResponse{DEK: hugeDEK()}, nil
				},
			},
			call: func(srv *DEKServer) error {
				_, err := srv.CreateDEK(ctx, &pb.CreateDEKRequest{Path: "/app/data"})
				return err
			},
		},
		{
			name: "GetDEK",
			svc: &mockKMSService{
				getDEKFunc: func(context.Context, *service.GetDEKRequest) (*service.GetDEKResponse, error) {
					return &service.GetDEKResponse{DEK: hugeDEK()}, nil
				},
			},
			call: func(srv *DEKServer) error {
				_, err := srv.GetDEK(ctx, &pb.GetDEKRequest{Path: "/app/data"})
				return err
			},
		},
		{
			name: "ListDEKs",
			svc: &mockKMSService{
				listDEKsFunc: func(context.Context, *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
					return &service.ListDEKsResponse{DEKs: []*service.DEKMetadata{hugeDEK()}}, nil
				},
			},
			call: func(srv *DEKServer) error {
				_, err := srv.ListDEKs(ctx, &pb.ListDEKsRequest{})
				return err
			},
		},
		{
			name: "GetDEKStatus",
			svc: &mockKMSService{
				getDEKStatusFunc: func(context.Context, *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
					return &service.GetDEKStatusResponse{DEK: hugeDEK()}, nil
				},
			},
			call: func(srv *DEKServer) error {
				_, err := srv.GetDEKStatus(ctx, &pb.GetDEKStatusRequest{Path: "/app/data"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewDEKServer(tt.svc, newTestLogger())
			requireRangeStatus(t, tt.call(srv), codes.Internal, "warning_percent")
		})
	}
}
