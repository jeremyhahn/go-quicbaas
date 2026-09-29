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
	"math"
	"testing"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestSessionCapToProto_CarriesTheRange(t *testing.T) {
	for _, v := range []int{0, 1, 5, math.MaxInt32} {
		got, err := sessionCapToProto("op", v)
		if err != nil || int(got) != v {
			t.Fatalf("sessionCapToProto(%d) = (%d, %v), want it carried exactly", v, got, err)
		}
	}
}

func TestSessionCapToProto_RefusesWhatTheWireCannotCarry(t *testing.T) {
	for _, v := range []int{-1, math.MinInt32 - 1, math.MaxInt32 + 1, 1 << 32} {
		if got, err := sessionCapToProto("op", v); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("sessionCapToProto(%d) = (%d, %v), want ErrInvalidInput, never a wrapped cap", v, got, err)
		}
	}
}

// TestAccountHandlers_RefuseAnOverflowingCapBeforeTheWire proves create and
// update fail on the client: 1<<32 would narrow to 0, which the server stores
// as unmetered. Refused before any RPC, so no connection is needed.
func TestAccountHandlers_RefuseAnOverflowingCapBeforeTheWire(t *testing.T) {
	for _, v := range []int{-1, 1 << 32} {
		if _, err := handleAccountCreate(context.Background(), nil, &account.CreateAccountRequest{
			TenantID: "t", Provider: "claude", MaxConcurrentSessions: v,
		}); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("create with cap %d: err = %v, want ErrInvalidInput", v, err)
		}
		capValue := v
		if _, err := handleAccountUpdate(context.Background(), nil, &account.UpdateAccountRequest{
			TenantID: "t", AccountID: "a", MaxConcurrentSessions: &capValue,
		}); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("update with cap %d: err = %v, want ErrInvalidInput", v, err)
		}
	}
}

func TestAccountFromProto_CarriesTheSessionCap(t *testing.T) {
	info := accountFromProto(&pb.Account{Id: "acct-1", MaxConcurrentSessions: 3})
	if info.MaxConcurrentSessions != 3 {
		t.Fatalf("MaxConcurrentSessions = %d, want 3", info.MaxConcurrentSessions)
	}
}
