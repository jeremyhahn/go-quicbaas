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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// These tests cover go-quicbaas#335 on gRPC, and on Unix, which serves this
// same server over a domain socket: a field the identity store refuses is the
// caller's mistake and answers InvalidArgument, not Internal.

func TestGRPC_CreateUserRefusesAMalformedEmailAsInvalidArgument(t *testing.T) {
	srv, _ := newRealIAMGRPCStack(t)

	_, err := srv.CreateUser(context.Background(), &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "not-an-email",
		Name:      "Bob",
		Lifecycle: string(iam.LifecycleActive),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "err: %v", err)
	assert.Contains(t, status.Convert(err).Message(), "email")
}

func TestGRPC_UpdateUserRefusesAnEmailShapedNameAsInvalidArgument(t *testing.T) {
	srv, _ := newRealIAMGRPCStack(t)
	ctx := context.Background()

	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "kim@acme.test",
		Username:  "kim",
		Name:      "Kim",
		Lifecycle: string(iam.LifecycleActive),
	})
	require.NoError(t, err)

	_, err = srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: "acme",
		UserId:   created.Id,
		Name:     "kim@acme.test",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "err: %v", err)
}

func TestToGRPCError_CodedFieldValidationErrorIsInvalidArgument(t *testing.T) {
	err := api.NewInvalidInputCauseError("invalid email",
		&iam.ValidationError{Field: "email", Message: "invalid email format"})

	assert.Equal(t, codes.InvalidArgument, status.Code(ToGRPCError(err)))
}
