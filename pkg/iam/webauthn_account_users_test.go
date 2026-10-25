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
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
)

var errAccountRead = errors.New("account read failed")

type accountBindKey struct{}

// accountBinder marks the context it binds.
type accountBinder struct{}

func (accountBinder) ServiceContext(ctx context.Context) (context.Context, error) {
	return context.WithValue(ctx, accountBindKey{}, true), nil
}

// accountUsers is a user client holding one stored record. It records the
// updates that reach it and whether its reads arrived bound.
type accountUsers struct {
	qrdbtransport.UserClient
	stored    qrdbtransport.User
	readErr   error
	boundRead bool
	updates   []*qrdbtransport.UpdateUserRequest
}

func (a *accountUsers) GetUser(ctx context.Context, _ uint64) (*qrdbtransport.User, error) {
	a.boundRead = ctx.Value(accountBindKey{}) != nil
	if a.readErr != nil {
		return nil, a.readErr
	}
	stored := a.stored
	return &stored, nil
}

func (a *accountUsers) UpdateUser(_ context.Context, req *qrdbtransport.UpdateUserRequest) (*qrdbtransport.User, error) {
	a.updates = append(a.updates, req)
	updated := a.stored
	updated.Name = req.Name
	return &updated, nil
}

func TestUnchangedNameUsers_SkipsAnUnchangedName(t *testing.T) {
	users := &accountUsers{stored: qrdbtransport.User{ID: 7, Name: "Alice"}}
	got, err := webAuthnAccountUsers(users, nil, qrdbsdk.SystemTenantID).
		UpdateUser(context.Background(), &qrdbtransport.UpdateUserRequest{ID: 7, Name: "Alice"})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if len(users.updates) != 0 {
		t.Fatalf("an unchanged name was written: %+v", users.updates)
	}
	if got.ID != 7 || got.Name != "Alice" {
		t.Fatalf("UpdateUser returned %+v, want the stored record", got)
	}
}

func TestUnchangedNameUsers_WritesAChangedName(t *testing.T) {
	users := &accountUsers{stored: qrdbtransport.User{ID: 7, Name: "Alice"}}
	got, err := webAuthnAccountUsers(users, nil, qrdbsdk.SystemTenantID).
		UpdateUser(context.Background(), &qrdbtransport.UpdateUserRequest{ID: 7, Name: "Alicia"})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if len(users.updates) != 1 || got.Name != "Alicia" {
		t.Fatalf("a changed name was not written: updates %+v, returned %+v", users.updates, got)
	}
}

func TestUnchangedNameUsers_PassesOtherUpdatesThrough(t *testing.T) {
	requests := map[string]*qrdbtransport.UpdateUserRequest{
		"lifecycle":     {ID: 7, Name: "Alice", Lifecycle: string(qrdbsdk.LifecycleActive)},
		"email":         {ID: 7, Email: "alice@example.com"},
		"no name":       {ID: 7},
		"auth methods":  {ID: 7, Name: "Alice", AuthMethods: []string{}},
		"role bindings": {ID: 7, Name: "Alice", RoleIDs: []string{"admin"}},
	}
	for name, req := range requests {
		t.Run(name, func(t *testing.T) {
			users := &accountUsers{stored: qrdbtransport.User{ID: 7, Name: "Alice"}}
			_, err := (&unchangedNameUsers{UserClient: users}).UpdateUser(context.Background(), req)
			if err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
			if len(users.updates) != 1 || users.updates[0] != req {
				t.Fatalf("update was not passed through: %+v", users.updates)
			}
		})
	}
}

func TestUnchangedNameUsers_ReturnsTheReadError(t *testing.T) {
	users := &accountUsers{readErr: errAccountRead}
	_, err := webAuthnAccountUsers(users, nil, qrdbsdk.SystemTenantID).
		UpdateUser(context.Background(), &qrdbtransport.UpdateUserRequest{ID: 7, Name: "Alice"})
	if !errors.Is(err, errAccountRead) {
		t.Fatalf("UpdateUser error = %v, want %v", err, errAccountRead)
	}
	if len(users.updates) != 0 {
		t.Fatalf("a name was written after a failed read: %+v", users.updates)
	}
}

func TestIsNameOnlyUpdate(t *testing.T) {
	cases := map[string]struct {
		req  *qrdbtransport.UpdateUserRequest
		want bool
	}{
		"name only":     {req: &qrdbtransport.UpdateUserRequest{ID: 7, Name: "Alice"}, want: true},
		"nil request":   {req: nil, want: false},
		"empty name":    {req: &qrdbtransport.UpdateUserRequest{ID: 7}, want: false},
		"name and more": {req: &qrdbtransport.UpdateUserRequest{ID: 7, Name: "Alice", Username: "alice"}, want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := isNameOnlyUpdate(tc.req); got != tc.want {
				t.Fatalf("isNameOnlyUpdate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWebAuthnAccountUsers_BindsTheSystemTenantOnly(t *testing.T) {
	cases := map[string]struct {
		service qrdbservice.Binder
		tenant  string
		bound   bool
	}{
		"system tenant":       {service: accountBinder{}, tenant: qrdbsdk.SystemTenantID, bound: true},
		"other tenant":        {service: accountBinder{}, tenant: "acme", bound: false},
		"remote client, none": {service: nil, tenant: qrdbsdk.SystemTenantID, bound: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			users := &accountUsers{stored: qrdbtransport.User{ID: 7}}
			if _, err := webAuthnAccountUsers(users, tc.service, tc.tenant).GetUser(context.Background(), 7); err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			if users.boundRead != tc.bound {
				t.Fatalf("read bound = %v, want %v", users.boundRead, tc.bound)
			}
		})
	}
}
