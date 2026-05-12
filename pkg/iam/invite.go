//go:build iam

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

// Package iam provides Identity and Access Management functionality for QuicBaaS.
package iam

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// InviteRecord stores the state of a single invite token.
type InviteRecord struct {
	// TokenHash is the SHA-256 hex of the plaintext token.
	TokenHash string

	// UserID is the user this invite activates.
	UserID string

	// TenantID is the tenant the user belongs to.
	TenantID string

	// ExpiresAt is when the invite becomes invalid.
	ExpiresAt time.Time

	// Used indicates the invite has been consumed.
	Used bool
}

// InviteStore defines the interface for managing invite tokens.
type InviteStore interface {
	// Create stores a new invite record.
	Create(ctx context.Context, record *InviteRecord) error

	// GetByToken looks up an invite by the plaintext token.
	// Returns ErrInviteNotFound when not present.
	GetByToken(ctx context.Context, token string) (*InviteRecord, error)

	// MarkUsed atomically marks the invite as consumed.
	// Returns ErrInviteNotFound when not present, ErrInviteUsed when already consumed.
	MarkUsed(ctx context.Context, token string) error

	// GC removes expired or used records.
	GC(ctx context.Context) error
}

// MemoryInviteStore is a thread-safe in-memory InviteStore implementation.
type MemoryInviteStore struct {
	mu      sync.RWMutex
	records map[string]*InviteRecord // keyed by TokenHash
}

// NewMemoryInviteStore creates a new in-memory InviteStore.
func NewMemoryInviteStore() *MemoryInviteStore {
	return &MemoryInviteStore{
		records: make(map[string]*InviteRecord),
	}
}

// Create stores a new invite record.
func (s *MemoryInviteStore) Create(_ context.Context, record *InviteRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.TokenHash] = record
	return nil
}

// GetByToken looks up an invite by the plaintext token.
func (s *MemoryInviteStore) GetByToken(_ context.Context, token string) (*InviteRecord, error) {
	hash := tokenHash(token)
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[hash]
	if !ok {
		return nil, ErrInviteNotFound
	}
	// Return a copy to avoid external mutation.
	copy := *r
	return &copy, nil
}

// MarkUsed atomically marks the invite as consumed.
func (s *MemoryInviteStore) MarkUsed(_ context.Context, token string) error {
	hash := tokenHash(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[hash]
	if !ok {
		return ErrInviteNotFound
	}
	if r.Used {
		return ErrInviteUsed
	}
	r.Used = true
	return nil
}

// GC removes expired or already-used invite records.
func (s *MemoryInviteStore) GC(_ context.Context) error {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, r := range s.records {
		if r.Used || now.After(r.ExpiresAt) {
			delete(s.records, k)
		}
	}
	return nil
}

// GenerateInviteToken generates a cryptographically random 32-byte invite token
// encoded as a hex string (64 characters).
func GenerateInviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", &InviteTokenGenerationError{Cause: err}
	}
	return hex.EncodeToString(b), nil
}

// tokenHash returns the SHA-256 hex of the plaintext token.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokenHashExported returns the SHA-256 hex of the plaintext token.
// This is the exported form of tokenHash, intended for callers outside the iam package
// that need to construct an InviteRecord manually (e.g., REST handlers).
func TokenHashExported(token string) string {
	return tokenHash(token)
}

// InviteTokenGenerationError is returned when secure random generation fails.
type InviteTokenGenerationError struct {
	Cause error
}

// Error implements the error interface.
func (e *InviteTokenGenerationError) Error() string {
	return "failed to generate invite token: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *InviteTokenGenerationError) Unwrap() error {
	return e.Cause
}
