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
	"sync"
	"testing"
	"time"
)

// errInjectedStorage is the failure faultyTokenStorage returns.
var errInjectedStorage = errors.New("injected storage failure")

// faultyTokenStorage is the real MemoryStorage with one write armed to fail.
// It records every token record a rotation tries to store, so a test can
// prove that nothing a failed rotation minted is left live.
type faultyTokenStorage struct {
	*MemoryStorage

	mu sync.Mutex
	// failStoreOf fails StoreToken for records of this type ("" = never).
	failStoreOf string
	// failRevokeOf fails RevokeToken for this JTI ("" = never).
	failRevokeOf string
	attempted    []string
}

func (f *faultyTokenStorage) StoreToken(ctx context.Context, token *Token) error {
	f.mu.Lock()
	f.attempted = append(f.attempted, token.JTI)
	fail := f.failStoreOf != "" && token.TokenType == f.failStoreOf
	f.mu.Unlock()
	if fail {
		return errInjectedStorage
	}
	return f.MemoryStorage.StoreToken(ctx, token)
}

func (f *faultyTokenStorage) RevokeToken(ctx context.Context, jti string) error {
	f.mu.Lock()
	fail := f.failRevokeOf != "" && jti == f.failRevokeOf
	f.mu.Unlock()
	if fail {
		return errInjectedStorage
	}
	return f.MemoryStorage.RevokeToken(ctx, jti)
}

// disarm stops injecting failures and returns the JTIs whose records were
// written while it was armed.
func (f *faultyTokenStorage) disarm() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failStoreOf, f.failRevokeOf = "", ""
	attempted := f.attempted
	f.attempted = nil
	return attempted
}

// TestRefreshSession_AFailedRotationLeavesTheChainRedeemable is #282. A
// renewal that fails at any step must leave the presented refresh credential
// able to renew the session, so a transient storage failure costs the client
// one retry and not its login. It used to revoke the presented credential
// first, so a failure at any later step left the chain revoked with no
// successor.
//
// It also asserts that rotation never leaves two live credentials. Nothing a
// failed rotation stored (the renewed access token's record, the successor
// refresh record) may stay live, and a successful retry still revokes the
// presented credential.
func TestRefreshSession_AFailedRotationLeavesTheChainRedeemable(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(f *faultyTokenStorage, presentedJTI string)
	}{
		{
			name: "storing the renewed access token fails",
			arm:  func(f *faultyTokenStorage, _ string) { f.failStoreOf = TokenTypeAccess },
		},
		{
			name: "storing the successor refresh credential fails",
			arm:  func(f *faultyTokenStorage, _ string) { f.failStoreOf = TokenTypeRefresh },
		},
		{
			name: "revoking the presented credential fails",
			arm:  func(f *faultyTokenStorage, presented string) { f.failRevokeOf = presented },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, memory := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
			faulty := &faultyTokenStorage{MemoryStorage: memory}
			provider.tokenService.storage = faulty
			ctx := context.Background()

			value, _, err := provider.IssueSessionRefresh(ctx, loginSession())
			if err != nil {
				t.Fatalf("IssueSessionRefresh: %v", err)
			}
			presented, err := memory.GetTokenByValue(ctx, value)
			if err != nil {
				t.Fatalf("GetTokenByValue: %v", err)
			}
			faulty.disarm()

			tc.arm(faulty, presented.JTI)
			if _, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value}); err == nil {
				t.Fatal("RefreshSession succeeded with a storage failure injected")
			}
			minted := faulty.disarm()
			if len(minted) == 0 {
				t.Fatal("control failed: the failed rotation attempted no write, so the fault was not reached")
			}

			// Nothing the failed rotation stored may remain live.
			for _, jti := range minted {
				if _, err := memory.GetToken(ctx, jti); err != nil {
					continue // never persisted
				}
				revoked, err := memory.IsTokenRevoked(ctx, jti)
				if err != nil {
					t.Fatalf("IsTokenRevoked(%s): %v", jti, err)
				}
				if !revoked {
					t.Errorf("token %s minted by the failed rotation is still live", jti)
				}
			}

			// The chain is not stranded: the presented credential still renews.
			tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
			if err != nil {
				t.Fatalf("the presented credential no longer renews after a failed rotation: %v", err)
			}
			if tokens.RefreshToken == "" || tokens.RefreshToken == value {
				t.Fatal("the retried renewal did not rotate the credential")
			}

			// And the retry spent it: exactly one credential stays redeemable.
			if revoked, err := memory.IsTokenRevoked(ctx, presented.JTI); err != nil || !revoked {
				t.Fatalf("presented credential not revoked by the successful retry (revoked=%v, err=%v)", revoked, err)
			}
			if _, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: tokens.RefreshToken}); err != nil {
				t.Fatalf("the successor credential does not renew: %v", err)
			}
		})
	}
}
