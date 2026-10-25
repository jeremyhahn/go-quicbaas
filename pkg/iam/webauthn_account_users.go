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
	"reflect"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
)

// webAuthnAccountUsers returns the user client a tenant's xkms WebAuthn
// adapter resolves accounts through.
//
// A passkey login and an invite registration resolve their account before
// anyone is authenticated, and the embedded node refuses an unauthenticated
// read. For the system tenant the reads therefore run as the node's service
// principal (qrdbservice.UserReads). go-qrdb lets that principal read its own
// tenant only; another tenant's accounts are reached through a delegation that
// tenant issues, so its client is left as is. A nil service means users is a
// remote client carrying the server's own mTLS identity.
func webAuthnAccountUsers(users qrdbtransport.UserClient, service qrdbservice.Binder, tenantID string) qrdbtransport.UserClient {
	if service != nil && tenantID == qrdbsdk.SystemTenantID {
		users = qrdbservice.UserReads(service, users)
	}
	return &unchangedNameUsers{UserClient: users}
}

// unchangedNameUsers answers a display-name update that changes nothing from
// the stored record instead of writing it. The xkms adapter's Save rewrites
// the account's display name at the end of every ceremony, login included, so
// without this a ceremony nobody is authenticated for yet would need
// users:update to rewrite the name it just read.
type unchangedNameUsers struct {
	qrdbtransport.UserClient
}

func (u *unchangedNameUsers) UpdateUser(ctx context.Context, req *qrdbtransport.UpdateUserRequest) (*qrdbtransport.User, error) {
	if !isNameOnlyUpdate(req) {
		return u.UserClient.UpdateUser(ctx, req)
	}
	current, err := u.GetUser(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current.Name == req.Name {
		return current, nil
	}
	return u.UserClient.UpdateUser(ctx, req)
}

// isNameOnlyUpdate reports whether req sets the display name and nothing
// else. It compares against a request built from the id and name alone, so a
// field added to UpdateUserRequest later counts as a change, not as nothing.
func isNameOnlyUpdate(req *qrdbtransport.UpdateUserRequest) bool {
	if req == nil || req.Name == "" {
		return false
	}
	return reflect.DeepEqual(*req, qrdbtransport.UpdateUserRequest{ID: req.ID, Name: req.Name})
}
