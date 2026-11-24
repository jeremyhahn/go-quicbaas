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
	"bytes"
	"context"
	"sync"

	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// The in-memory WebAuthn user and credential stores the pkg/iam tests run
// against. go-xkms ships only the qrdb-backed pair, which needs a live node;
// these keep the unit tests self-contained while honouring the same
// UserStore / CredentialStore contracts (not-found and duplicate sentinels,
// the fixed credential owner).

// memoryUserStore is an in-memory xkmswebauthn.UserStore keyed by the
// WebAuthn user handle.
type memoryUserStore struct {
	mu    sync.RWMutex
	users map[string]xkmswebauthn.User
}

func newMemoryUserStore() *memoryUserStore {
	return &memoryUserStore{users: make(map[string]xkmswebauthn.User)}
}

func (s *memoryUserStore) GetByID(_ context.Context, userID []byte) (xkmswebauthn.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[string(userID)]
	if !ok {
		return nil, xkmswebauthn.ErrUserNotFound
	}
	return u, nil
}

func (s *memoryUserStore) GetByEmail(_ context.Context, email string) (xkmswebauthn.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Email() == email {
			return u, nil
		}
	}
	return nil, xkmswebauthn.ErrUserNotFound
}

func (s *memoryUserStore) Create(_ context.Context, email, displayName string) (xkmswebauthn.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Email() == email {
			return nil, xkmswebauthn.ErrUserAlreadyExists
		}
	}
	u := xkmswebauthn.NewDefaultUserFromEmail(email, displayName)
	s.users[string(u.WebAuthnID())] = u
	return u, nil
}

func (s *memoryUserStore) Save(_ context.Context, user xkmswebauthn.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[string(user.WebAuthnID())] = user
	return nil
}

func (s *memoryUserStore) Delete(_ context.Context, userID []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[string(userID)]; !ok {
		return xkmswebauthn.ErrUserNotFound
	}
	delete(s.users, string(userID))
	return nil
}

// memoryCredentialStore is an in-memory xkmswebauthn.CredentialStore keyed
// by credential ID. It stores copies, so a caller mutating a credential it
// holds does not change the stored record without an Update.
type memoryCredentialStore struct {
	mu    sync.RWMutex
	creds map[string]*xkmswebauthn.Credential
}

func newMemoryCredentialStore() *memoryCredentialStore {
	return &memoryCredentialStore{creds: make(map[string]*xkmswebauthn.Credential)}
}

func copyCredential(c *xkmswebauthn.Credential) *xkmswebauthn.Credential {
	cp := *c
	return &cp
}

func (s *memoryCredentialStore) Save(_ context.Context, cred *xkmswebauthn.Credential) error {
	if cred == nil || len(cred.ID) == 0 {
		return xkmswebauthn.ErrInvalidCredential
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.creds[string(cred.ID)]; ok {
		return xkmswebauthn.ErrCredentialAlreadyExists
	}
	s.creds[string(cred.ID)] = copyCredential(cred)
	return nil
}

func (s *memoryCredentialStore) GetByUserID(_ context.Context, userID []byte) ([]*xkmswebauthn.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*xkmswebauthn.Credential{}
	for _, c := range s.creds {
		if bytes.Equal(c.UserID, userID) {
			out = append(out, copyCredential(c))
		}
	}
	return out, nil
}

func (s *memoryCredentialStore) GetByCredentialID(_ context.Context, credID []byte) (*xkmswebauthn.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.creds[string(credID)]
	if !ok {
		return nil, xkmswebauthn.ErrCredentialNotFound
	}
	return copyCredential(c), nil
}

func (s *memoryCredentialStore) Update(_ context.Context, cred *xkmswebauthn.Credential) error {
	if cred == nil {
		return xkmswebauthn.ErrInvalidCredential
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.creds[string(cred.ID)]
	if !ok {
		return xkmswebauthn.ErrCredentialNotFound
	}
	if !bytes.Equal(stored.UserID, cred.UserID) {
		return xkmswebauthn.ErrCredentialOwnerMismatch
	}
	s.creds[string(cred.ID)] = copyCredential(cred)
	return nil
}

func (s *memoryCredentialStore) Delete(_ context.Context, credID []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.creds[string(credID)]; !ok {
		return xkmswebauthn.ErrCredentialNotFound
	}
	delete(s.creds, string(credID))
	return nil
}

func (s *memoryCredentialStore) DeleteByUserID(_ context.Context, userID []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.creds {
		if bytes.Equal(c.UserID, userID) {
			delete(s.creds, id)
		}
	}
	return nil
}
