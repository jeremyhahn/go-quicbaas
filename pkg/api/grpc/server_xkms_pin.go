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

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// PIN operations are not available via the QuicBaaS xkms bridge.
// QuicBaaS manages its own authentication and PIN lifecycle through
// its IAM service. These methods satisfy the KeystoreServiceServer
// interface and return codes.Unimplemented.

// SetSOPIN sets the Security Officer PIN.
func (s *XKMSServer) SetSOPIN(ctx context.Context, req *xkmssdk.ProtoSetSOPINRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}

// SetUserPIN sets the user PIN.
func (s *XKMSServer) SetUserPIN(ctx context.Context, req *xkmssdk.ProtoSetUserPINRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}

// ChangeSOPIN changes the Security Officer PIN.
func (s *XKMSServer) ChangeSOPIN(ctx context.Context, req *xkmssdk.ProtoChangeSOPINRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}

// ChangeUserPIN changes the user PIN.
func (s *XKMSServer) ChangeUserPIN(ctx context.Context, req *xkmssdk.ProtoChangeUserPINRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}

// VerifySOPIN verifies the provided Security Officer PIN.
func (s *XKMSServer) VerifySOPIN(ctx context.Context, req *xkmssdk.ProtoVerifySOPINRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}

// VerifyUserPIN verifies the provided user PIN.
func (s *XKMSServer) VerifyUserPIN(ctx context.Context, req *xkmssdk.ProtoVerifyUserPINRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}

// GetLockoutStatus returns the current PIN lockout status.
func (s *XKMSServer) GetLockoutStatus(ctx context.Context, req *emptypb.Empty) (*xkmssdk.ProtoLockoutStatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}

// ResetLockout resets the PIN lockout counter.
func (s *XKMSServer) ResetLockout(ctx context.Context, req *xkmssdk.ProtoResetLockoutRequest) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "PIN operations not available via QuicBaaS xkms bridge")
}
